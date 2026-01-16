package deps

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFetchManifest(t *testing.T) {
	// Create mock server
	manifest := Manifest{
		Version:   "1.0.0",
		UpdatedAt: time.Now(),
		Libraries: []Library{
			{
				Name:     "libcuda.so.1",
				Version:  "12.0",
				Platform: "linux",
				Arch:     "amd64",
				URL:      "https://example.com/libcuda.so.1",
				SHA256:   "abc123",
				Size:     1024,
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == ManifestPath {
			json.NewEncoder(w).Encode(manifest)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	mgr := NewManager(WithCDNBaseURL(server.URL))
	ctx := context.Background()

	result, err := mgr.FetchManifest(ctx)
	require.NoError(t, err)
	assert.Equal(t, "1.0.0", result.Version)
	assert.Len(t, result.Libraries, 1)
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
	libs := mgr.GetLibrariesForPlatform(manifest)

	// Should only return libraries matching current platform
	for _, lib := range libs {
		assert.Equal(t, runtime.GOOS, lib.Platform)
		assert.Equal(t, runtime.GOARCH, lib.Arch)
	}
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
	case "linux":
		assert.Contains(t, libs, "libcuda.so.1")
		assert.Contains(t, libs, "libnvidia-ml.so.1")
	case "windows":
		assert.Contains(t, libs, "nvcuda.dll")
		assert.Contains(t, libs, "nvml.dll")
	default:
		assert.Nil(t, libs)
	}
}
