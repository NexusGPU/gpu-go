package deps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants for platform strings
const (
	testOSLinux   = "linux"
	testOSWindows = "windows"
)

func TestFetchManifest(t *testing.T) {
	// Use temp directory to avoid cached manifests
	tmpDir := t.TempDir()
	paths := platform.DefaultPaths().WithConfigDir(tmpDir)

	// Create mock API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ecosystem/releases" {
			// Return a mock releases response
			resp := api.ReleasesResponse{
				Releases: []api.ReleaseInfo{
					{
						ID:      "release-1",
						Vendor:  api.VendorInfo{Slug: "stub", Name: "STUB"},
						Version: "1.0.0",
						Artifacts: []api.ReleaseArtifact{
							{
								CPUArch: runtime.GOARCH,
								OS:      runtime.GOOS,
								URL:     "https://example.com/libcuda.so.1",
								SHA256:  "abc123",
								Metadata: map[string]string{
									"type": "vgpu-library",
									"size": "1024",
								},
							},
						},
					},
				},
				Count: 1,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create manager with mocked API client
	apiClient := api.NewClient(api.WithBaseURL(server.URL))
	mgr := NewManager(
		WithPaths(paths),
		WithAPIClient(apiClient),
	)
	ctx := context.Background()

	result, _, err := mgr.FetchManifest(ctx)
	require.NoError(t, err)
	// Version should be in format "api-{timestamp}"
	assert.True(t, strings.HasPrefix(result.Version, "api-"), "version should start with 'api-'")
	assert.Len(t, result.Libraries, 1)
	assert.Equal(t, "libcuda.so.1", result.Libraries[0].Name)
}

func TestGetLibrariesForPlatform(t *testing.T) {
	manifest := &Manifest{
		Libraries: []Library{
			{Name: "libcuda.so.1", Platform: "linux", Arch: "amd64"},
			{Name: "libcuda.so.1", Platform: "linux", Arch: "arm64"},
			{Name: "nvcuda.dll", Platform: "windows", Arch: "amd64"},
		},
	}

	mgr := NewManager()
	libs := mgr.GetLibrariesForPlatform(manifest, "", "", "")

	// Should only return libraries matching current platform
	for _, lib := range libs {
		assert.Equal(t, runtime.GOOS, lib.Platform)
		assert.Equal(t, runtime.GOARCH, lib.Arch)
	}

	// Test with specific platform
	linuxLibs := mgr.GetLibrariesForPlatform(manifest, "linux", "amd64", "")
	assert.Len(t, linuxLibs, 1)
	assert.Equal(t, "libcuda.so.1", linuxLibs[0].Name)
	assert.Equal(t, "linux", linuxLibs[0].Platform)
	assert.Equal(t, "amd64", linuxLibs[0].Arch)
}

func TestLocalManifest(t *testing.T) {
	tmpDir := t.TempDir()
	paths := platform.DefaultPaths().WithConfigDir(tmpDir)

	mgr := NewManager(WithPaths(paths))

	// Initially empty
	manifest, err := mgr.GetInstalledLibraries()
	require.NoError(t, err)
	assert.Empty(t, manifest.Libraries)
}

func TestCleanCache(t *testing.T) {
	tmpDir := t.TempDir()
	paths := platform.DefaultPaths().WithConfigDir(tmpDir)

	// Create some cache files
	cacheDir := paths.CacheDir()
	os.MkdirAll(cacheDir, 0755)
	os.WriteFile(filepath.Join(cacheDir, "test.so"), []byte("test"), 0644)

	mgr := NewManager(WithPaths(paths))
	err := mgr.CleanCache()
	require.NoError(t, err)

	// Cache should be empty
	entries, _ := os.ReadDir(cacheDir)
	assert.Empty(t, entries)
}

func TestVGPULibraries(t *testing.T) {
	libs := VGPULibraries()

	switch runtime.GOOS {
	case testOSLinux:
		assert.Contains(t, libs, "libcuda.so.1")
		assert.Contains(t, libs, "libnvidia-ml.so.1")
	case testOSWindows:
		assert.Contains(t, libs, "nvcuda.dll")
		assert.Contains(t, libs, "nvml.dll")
	default:
		assert.Nil(t, libs)
	}
}
