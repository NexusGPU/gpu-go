package studio

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseImageReference(t *testing.T) {
	tests := []struct {
		image    string
		wantReg  string
		wantRepo string
	}{
		{
			image:    "nginx",
			wantReg:  "registry-1.docker.io",
			wantRepo: "library/nginx",
		},
		{
			image:    "nginx:latest",
			wantReg:  "registry-1.docker.io",
			wantRepo: "library/nginx",
		},
		{
			image:    "tensorfusion/studio-torch",
			wantReg:  "registry-1.docker.io",
			wantRepo: "tensorfusion/studio-torch",
		},
		{
			image:    "tensorfusion/studio-torch:v1.0",
			wantReg:  "registry-1.docker.io",
			wantRepo: "tensorfusion/studio-torch",
		},
		{
			image:    "gcr.io/my-project/my-image",
			wantReg:  "gcr.io",
			wantRepo: "my-project/my-image",
		},
		{
			image:    "gcr.io/my-project/my-image:v2",
			wantReg:  "gcr.io",
			wantRepo: "my-project/my-image",
		},
		{
			image:    "localhost:5555/test/nginx",
			wantReg:  "localhost:5555",
			wantRepo: "test/nginx",
		},
		{
			image:    "localhost:5555/test/nginx:v1",
			wantReg:  "localhost:5555",
			wantRepo: "test/nginx",
		},
		{
			image:    "myregistry.example.com/myapp",
			wantReg:  "myregistry.example.com",
			wantRepo: "myapp",
		},
		{
			image:    "myregistry.example.com:8443/org/repo:sha-abc123",
			wantReg:  "myregistry.example.com:8443",
			wantRepo: "org/repo",
		},
		{
			image:    "localhost/myimage",
			wantReg:  "localhost",
			wantRepo: "myimage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.image, func(t *testing.T) {
			reg, repo := parseImageReference(tt.image)
			if reg != tt.wantReg {
				t.Errorf("registry = %q, want %q", reg, tt.wantReg)
			}
			if repo != tt.wantRepo {
				t.Errorf("repository = %q, want %q", repo, tt.wantRepo)
			}
		})
	}
}

func TestReadDockerConfig(t *testing.T) {
	// Create a temp docker config
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".docker")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}

	config := dockerConfig{
		Auths: map[string]dockerAuth{
			"https://index.docker.io/v1/": {Auth: "dXNlcjpwYXNz"}, // base64("user:pass")
			"gcr.io":                      {Auth: "Z2NyOnRva2Vu"}, // base64("gcr:token")
		},
		CredHelpers: map[string]string{
			"123456789.dkr.ecr.us-east-1.amazonaws.com": "ecr-login",
		},
		CredsStore: "desktop",
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Parse the config directly (readDockerConfig uses real home dir, so test the parsing)
	var parsed dockerConfig
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse config: %v", err)
	}

	if len(parsed.Auths) != 2 {
		t.Errorf("expected 2 auths entries, got %d", len(parsed.Auths))
	}

	if parsed.CredHelpers["123456789.dkr.ecr.us-east-1.amazonaws.com"] != "ecr-login" {
		t.Error("expected ecr-login credential helper")
	}

	if parsed.CredsStore != "desktop" {
		t.Errorf("expected credsStore=desktop, got %s", parsed.CredsStore)
	}

	// Test getRegistryAuth with auths
	user, pass, err := getRegistryAuth(&parsed, "gcr.io")
	if err != nil {
		t.Fatalf("getRegistryAuth failed: %v", err)
	}
	if user != "gcr" || pass != "token" {
		t.Errorf("expected gcr:token, got %s:%s", user, pass)
	}

	// Test Docker Hub auth resolution via index.docker.io key
	user, pass, err = getRegistryAuth(&parsed, dockerHubRegistry)
	if err != nil {
		t.Fatalf("getRegistryAuth for Docker Hub failed: %v", err)
	}
	if user != "user" || pass != "pass" {
		t.Errorf("expected user:pass for Docker Hub, got %s:%s", user, pass)
	}
}

func TestListRemoteTags(t *testing.T) {
	// Create a mock V2 registry
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Expect /v2/<repo>/tags/list
		if !strings.HasPrefix(r.URL.Path, "/v2/") || !strings.HasSuffix(r.URL.Path, "/tags/list") {
			http.NotFound(w, r)
			return
		}

		resp := v2TagsResponse{
			Name: "test/nginx",
			Tags: []string{"v2", "v1", "latest", "alpine"},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Extract host:port from server URL (strip "http://")
	registry := strings.TrimPrefix(server.URL, "http://")

	ctx := context.Background()
	tags, err := ListRemoteTags(ctx, registry+"/test/nginx", 0)
	if err != nil {
		t.Fatalf("ListRemoteTags failed: %v", err)
	}

	// Tags should be sorted
	expected := []string{"alpine", "latest", "v1", "v2"}
	if len(tags) != len(expected) {
		t.Fatalf("expected %d tags, got %d: %v", len(expected), len(tags), tags)
	}
	for i, tag := range tags {
		if tag != expected[i] {
			t.Errorf("tag[%d] = %q, want %q", i, tag, expected[i])
		}
	}
}

func TestListRemoteTagsWithLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := v2TagsResponse{
			Name: "test/app",
			Tags: []string{"v3", "v2", "v1", "latest", "beta", "alpha"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	registry := strings.TrimPrefix(server.URL, "http://")

	ctx := context.Background()
	tags, err := ListRemoteTags(ctx, registry+"/test/app", 3)
	if err != nil {
		t.Fatalf("ListRemoteTags failed: %v", err)
	}

	if len(tags) != 3 {
		t.Fatalf("expected 3 tags, got %d: %v", len(tags), tags)
	}
}

func TestIsLocalRegistry(t *testing.T) {
	tests := []struct {
		registry string
		want     bool
	}{
		{"localhost:5555", true},
		{"localhost", true},
		{"127.0.0.1:5000", true},
		{"gcr.io", false},
		{"registry-1.docker.io", false},
		{"myregistry.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.registry, func(t *testing.T) {
			if got := isLocalRegistry(tt.registry); got != tt.want {
				t.Errorf("isLocalRegistry(%q) = %v, want %v", tt.registry, got, tt.want)
			}
		})
	}
}
