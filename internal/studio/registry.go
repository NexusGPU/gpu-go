package studio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// dockerConfig represents the structure of ~/.docker/config.json
type dockerConfig struct {
	Auths       map[string]dockerAuth `json:"auths"`
	CredHelpers map[string]string     `json:"credHelpers"`
	CredsStore  string                `json:"credsStore"`
}

type dockerAuth struct {
	Auth string `json:"auth"` // base64-encoded "username:password"
}

// credHelperResponse represents the JSON output from docker-credential-* helpers
type credHelperResponse struct {
	Username string `json:"Username"`
	Secret   string `json:"Secret"`
}

// v2TagsResponse represents the Docker V2 API tags/list response
type v2TagsResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// v2TokenResponse represents the Docker Hub token endpoint response
type v2TokenResponse struct {
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
}

// dockerHubRegistry is the canonical Docker Hub registry host used for V2 API calls.
const dockerHubRegistry = "registry-1.docker.io"

// dockerHubIndexServer is the legacy index host that appears in docker config auths.
const dockerHubIndexServer = "https://index.docker.io/v1/"

// parseImageReference parses a Docker image reference into registry and repository.
// Examples:
//
//	"nginx"                       → ("registry-1.docker.io", "library/nginx")
//	"tensorfusion/studio-torch"   → ("registry-1.docker.io", "tensorfusion/studio-torch")
//	"gcr.io/my-project/my-image" → ("gcr.io", "my-project/my-image")
//	"localhost:5555/test/nginx"   → ("localhost:5555", "test/nginx")
func parseImageReference(image string) (registry, repository string) {
	// Strip tag or digest
	ref := image
	if idx := strings.Index(ref, "@"); idx != -1 {
		ref = ref[:idx]
	}
	if idx := strings.LastIndex(ref, ":"); idx != -1 {
		// Only strip if the part after : looks like a tag (no slashes)
		afterColon := ref[idx+1:]
		if !strings.Contains(afterColon, "/") {
			ref = ref[:idx]
		}
	}

	parts := strings.SplitN(ref, "/", 2)
	if len(parts) == 1 {
		// Single name like "nginx" → Docker Hub library image
		return dockerHubRegistry, "library/" + parts[0]
	}

	// Check if the first part looks like a registry host (contains "." or ":" or is "localhost")
	firstPart := parts[0]
	if strings.Contains(firstPart, ".") || strings.Contains(firstPart, ":") || firstPart == "localhost" {
		return firstPart, parts[1]
	}

	// No registry prefix, assume Docker Hub (e.g., "tensorfusion/studio-torch")
	return dockerHubRegistry, ref
}

// readDockerConfig reads and parses ~/.docker/config.json
func readDockerConfig() (*dockerConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine home directory: %w", err)
	}

	configPath := filepath.Join(home, ".docker", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &dockerConfig{}, nil
		}
		return nil, fmt.Errorf("cannot read docker config: %w", err)
	}

	var config dockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("cannot parse docker config: %w", err)
	}
	return &config, nil
}

// getRegistryAuth resolves credentials for a registry from Docker config.
// It checks credHelpers, then auths, then credsStore.
func getRegistryAuth(config *dockerConfig, registry string) (username, password string, err error) {
	if config == nil {
		return "", "", nil
	}

	// 1. Check credHelpers for registry-specific credential helper
	if helper, ok := config.CredHelpers[registry]; ok {
		return execCredHelper(helper, registry)
	}

	// 2. Check auths for base64-encoded credentials
	// Try exact match first, then common Docker Hub variants
	authKeys := []string{registry}
	if registry == dockerHubRegistry {
		authKeys = append(authKeys, dockerHubIndexServer, "https://index.docker.io/v2/", "docker.io")
	}

	for _, key := range authKeys {
		if auth, ok := config.Auths[key]; ok && auth.Auth != "" {
			decoded, err := base64.StdEncoding.DecodeString(auth.Auth)
			if err != nil {
				continue
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				return parts[0], parts[1], nil
			}
		}
	}

	// 3. Check credsStore for default credential store
	if config.CredsStore != "" {
		u, p, err := execCredHelper(config.CredsStore, registry)
		if err == nil && u != "" {
			return u, p, nil
		}
	}

	return "", "", nil
}

// execCredHelper runs docker-credential-<helper> get and parses the result.
func execCredHelper(helper, registry string) (string, string, error) {
	helperBin := "docker-credential-" + helper
	cmd := exec.Command(helperBin, "get")
	cmd.Stdin = strings.NewReader(registry)

	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("credential helper %s failed: %w", helperBin, err)
	}

	var resp credHelperResponse
	if err := json.Unmarshal(output, &resp); err != nil {
		return "", "", fmt.Errorf("cannot parse credential helper output: %w", err)
	}

	return resp.Username, resp.Secret, nil
}

// getRegistryToken obtains a Bearer token for registries that use token-based auth (e.g., Docker Hub).
func getRegistryToken(ctx context.Context, registry, repo, username, password string) (string, error) {
	// Docker Hub token endpoint
	tokenURL := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull", repo)

	// For non-Docker Hub registries, try the WWW-Authenticate challenge flow
	if registry != dockerHubRegistry {
		return "", nil // Non-Docker Hub registries typically use Basic auth
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL, nil)
	if err != nil {
		return "", err
	}

	if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var tokenResp v2TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("cannot parse token response: %w", err)
	}

	if tokenResp.Token != "" {
		return tokenResp.Token, nil
	}
	return tokenResp.AccessToken, nil
}

// isLocalRegistry returns true if the registry looks like a local/insecure registry.
func isLocalRegistry(registry string) bool {
	return strings.HasPrefix(registry, "localhost") || strings.HasPrefix(registry, "127.0.0.1")
}

// ListRemoteTags fetches available tags for an image from the Docker V2 Registry API.
// It uses credentials from ~/.docker/config.json when available.
func ListRemoteTags(ctx context.Context, image string, limit int) ([]string, error) {
	registry, repo := parseImageReference(image)

	// Read Docker config for auth
	config, err := readDockerConfig()
	if err != nil {
		// Continue without auth — may still work for public repos
		config = &dockerConfig{}
	}

	username, password, _ := getRegistryAuth(config, registry)

	// Build the V2 tags/list URL
	scheme := "https"
	if isLocalRegistry(registry) {
		scheme = "http"
	}
	tagsURL := fmt.Sprintf("%s://%s/v2/%s/tags/list", scheme, registry, repo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	if err != nil {
		return nil, err
	}

	// Set auth header
	if registry == dockerHubRegistry {
		// Docker Hub requires Bearer token auth
		token, err := getRegistryToken(ctx, registry, repo, username, password)
		if err != nil {
			return nil, fmt.Errorf("failed to get registry token: %w", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	} else if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("registry request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("authentication required for %s/%s — run 'docker login %s'", registry, repo, registry)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	var tagsResp v2TagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, fmt.Errorf("cannot parse tags response: %w", err)
	}

	tags := tagsResp.Tags
	sort.Strings(tags)

	if limit > 0 && len(tags) > limit {
		tags = tags[:limit]
	}

	return tags, nil
}
