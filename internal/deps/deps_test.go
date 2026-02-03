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

func TestLibraryKey(t *testing.T) {
	lib := Library{
		Name:     "libcuda.so.1",
		Version:  "1.0.0",
		Platform: "linux",
		Arch:     "amd64",
	}

	key := lib.Key()
	assert.Equal(t, "libcuda.so.1:linux:amd64", key)

	// Same name, different version should have same key (we track one version per key)
	lib2 := Library{
		Name:     "libcuda.so.1",
		Version:  "2.0.0",
		Platform: "linux",
		Arch:     "amd64",
	}
	assert.Equal(t, lib.Key(), lib2.Key())

	// Different platform should have different key
	lib3 := Library{
		Name:     "libcuda.so.1",
		Version:  "1.0.0",
		Platform: "darwin",
		Arch:     "amd64",
	}
	assert.NotEqual(t, lib.Key(), lib3.Key())
}

func TestSelectRequiredDeps(t *testing.T) {
	manifest := &ReleaseManifest{
		Libraries: []Library{
			// vgpu-library type - two versions
			{Name: "libaccel-nvidia.so", Version: "2.0.0", Platform: "linux", Arch: "amd64", Type: LibraryTypeVGPULibrary},
			{Name: "libaccel-nvidia.so", Version: "1.0.0", Platform: "linux", Arch: "amd64", Type: LibraryTypeVGPULibrary},
			// remote-gpu-worker type - multiple artifacts in same version
			{Name: "tensor-fusion-worker", Version: "2.6.3", Platform: "linux", Arch: "amd64", Type: LibraryTypeRemoteGPUWorker},
			{Name: "tensor-fusion-client", Version: "2.6.3", Platform: "linux", Arch: "amd64", Type: LibraryTypeRemoteGPUClient},
			// Older version of worker - should not be selected
			{Name: "tensor-fusion-worker", Version: "2.5.0", Platform: "linux", Arch: "amd64", Type: LibraryTypeRemoteGPUWorker},
		},
	}

	mgr := NewManager()
	deps := mgr.SelectRequiredDeps(manifest)

	// Should select latest version for each type
	assert.Len(t, deps.Libraries, 3)

	// Check vgpu-library - should be 2.0.0 (latest)
	vgpuKey := "libaccel-nvidia.so:linux:amd64"
	vgpuLib, exists := deps.Libraries[vgpuKey]
	assert.True(t, exists)
	assert.Equal(t, "2.0.0", vgpuLib.Version)

	// Check remote-gpu-worker - should be 2.6.3 (latest)
	workerKey := "tensor-fusion-worker:linux:amd64"
	workerLib, exists := deps.Libraries[workerKey]
	assert.True(t, exists)
	assert.Equal(t, "2.6.3", workerLib.Version)

	// Check remote-gpu-client - should be included (same version group)
	clientKey := "tensor-fusion-client:linux:amd64"
	clientLib, exists := deps.Libraries[clientKey]
	assert.True(t, exists)
	assert.Equal(t, "2.6.3", clientLib.Version)
}

func TestFetchReleaseManifest(t *testing.T) {
	// Use temp directory to avoid cached manifests
	tmpDir := t.TempDir()
	paths := platform.DefaultPaths().WithConfigDir(tmpDir)

	// Create mock API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ecosystem/releases" {
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
									"type": LibraryTypeVGPULibrary,
									"size": "1024",
								},
							},
						},
					},
				},
				Count: 1,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
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

	result, _, err := mgr.FetchReleaseManifest(ctx)
	require.NoError(t, err)
	// Version should be in format "api-{timestamp}"
	assert.True(t, strings.HasPrefix(result.Version, "api-"), "version should start with 'api-'")
	assert.Len(t, result.Libraries, 1)
	assert.Equal(t, "libcuda.so.1", result.Libraries[0].Name)
	assert.Equal(t, LibraryTypeVGPULibrary, result.Libraries[0].Type)
}

func TestGetLibrariesForPlatform(t *testing.T) {
	manifest := &ReleaseManifest{
		Libraries: []Library{
			{Name: "libcuda.so.1", Platform: "linux", Arch: "amd64", Type: LibraryTypeVGPULibrary},
			{Name: "libcuda.so.1", Platform: "linux", Arch: "arm64", Type: LibraryTypeVGPULibrary},
			{Name: "nvcuda.dll", Platform: "windows", Arch: "amd64", Type: LibraryTypeVGPULibrary},
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

	// Test with type filter
	vgpuLibs := mgr.GetLibrariesForPlatform(manifest, "linux", "amd64", LibraryTypeVGPULibrary)
	assert.Len(t, vgpuLibs, 1)
	assert.Equal(t, LibraryTypeVGPULibrary, vgpuLibs[0].Type)
}

func TestDepsManifest(t *testing.T) {
	tmpDir := t.TempDir()
	paths := platform.DefaultPaths().WithConfigDir(tmpDir)

	mgr := NewManager(WithPaths(paths))

	// Initially nil
	manifest, err := mgr.LoadDepsManifest()
	require.NoError(t, err)
	assert.Nil(t, manifest)

	// Save a deps manifest
	deps := &DepsManifest{
		Libraries: map[string]Library{
			"libcuda.so.1:linux:amd64": {
				Name:     "libcuda.so.1",
				Version:  "1.0.0",
				Platform: "linux",
				Arch:     "amd64",
				Type:     LibraryTypeVGPULibrary,
			},
		},
	}
	err = mgr.SaveDepsManifest(deps)
	require.NoError(t, err)

	// Load again
	loaded, err := mgr.LoadDepsManifest()
	require.NoError(t, err)
	assert.NotNil(t, loaded)
	assert.Len(t, loaded.Libraries, 1)
	assert.Equal(t, "1.0.0", loaded.Libraries["libcuda.so.1:linux:amd64"].Version)
}

func TestComputeUpdateDiff(t *testing.T) {
	tmpDir := t.TempDir()
	paths := platform.DefaultPaths().WithConfigDir(tmpDir)
	mgr := NewManager(WithPaths(paths))

	// Create deps manifest
	deps := &DepsManifest{
		Libraries: map[string]Library{
			"lib1:linux:amd64": {Name: "lib1", Version: "2.0.0", Platform: "linux", Arch: "amd64"},
			"lib2:linux:amd64": {Name: "lib2", Version: "1.0.0", Platform: "linux", Arch: "amd64"},
		},
	}
	err := mgr.SaveDepsManifest(deps)
	require.NoError(t, err)

	// Create downloaded manifest (lib1 is outdated, lib2 is up to date but file missing)
	downloaded := &DownloadedManifest{
		Libraries: map[string]Library{
			"lib1:linux:amd64": {Name: "lib1", Version: "1.0.0", Platform: "linux", Arch: "amd64"},
			"lib2:linux:amd64": {Name: "lib2", Version: "1.0.0", Platform: "linux", Arch: "amd64"},
		},
	}
	downloadedPath := filepath.Join(tmpDir, DownloadedManifestFile)
	data, _ := json.Marshal(downloaded)
	_ = os.WriteFile(downloadedPath, data, 0644)

	// Compute diff
	diff, err := mgr.ComputeUpdateDiff()
	require.NoError(t, err)

	// Both should need download (lib1: version mismatch, lib2: file doesn't exist)
	assert.Len(t, diff.ToDownload, 2)
	assert.Empty(t, diff.UpToDate)
}

func TestCleanCache(t *testing.T) {
	tmpDir := t.TempDir()
	paths := platform.DefaultPaths().WithConfigDir(tmpDir)

	// Create some cache files
	cacheDir := paths.CacheDir()
	_ = os.MkdirAll(cacheDir, 0755)
	_ = os.WriteFile(filepath.Join(cacheDir, "test.so"), []byte("test"), 0644)

	mgr := NewManager(WithPaths(paths))
	err := mgr.CleanCache()
	require.NoError(t, err)

	// Cache should be empty
	entries, _ := os.ReadDir(cacheDir)
	assert.Empty(t, entries)
}

func TestUpdateDepsManifest(t *testing.T) {
	tmpDir := t.TempDir()
	paths := platform.DefaultPaths().WithConfigDir(tmpDir)

	// Create mock API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ecosystem/releases" {
			resp := api.ReleasesResponse{
				Releases: []api.ReleaseInfo{
					{
						ID:      "release-1",
						Vendor:  api.VendorInfo{Slug: "nvidia", Name: "NVIDIA"},
						Version: "2.0.0",
						Artifacts: []api.ReleaseArtifact{
							{
								CPUArch: runtime.GOARCH,
								OS:      runtime.GOOS,
								URL:     "https://example.com/worker",
								Metadata: map[string]string{
									"type": LibraryTypeRemoteGPUWorker,
								},
							},
						},
					},
				},
				Count: 1,
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	apiClient := api.NewClient(api.WithBaseURL(server.URL))
	mgr := NewManager(
		WithPaths(paths),
		WithAPIClient(apiClient),
	)
	ctx := context.Background()

	// First update - should be all new
	deps, changes, err := mgr.UpdateDepsManifest(ctx)
	require.NoError(t, err)
	assert.NotNil(t, deps)
	assert.Len(t, changes, 1)
	assert.Equal(t, "2.0.0", changes[0].Version)

	// Second update - no changes
	deps2, changes2, err := mgr.UpdateDepsManifest(ctx)
	require.NoError(t, err)
	assert.NotNil(t, deps2)
	assert.Empty(t, changes2) // No changes since same version
}

func TestInstallLibrary(t *testing.T) {
	tmpDir := t.TempDir()
	paths := platform.DefaultPaths().WithConfigDir(tmpDir)
	mgr := NewManager(WithPaths(paths))

	lib := Library{
		Name:     "test-lib",
		Version:  "1.0.0",
		Platform: "linux",
		Arch:     "amd64",
		Type:     LibraryTypeVGPULibrary,
	}

	err := mgr.InstallLibrary(lib)
	require.NoError(t, err)

	// Verify it was added to deps manifest
	deps, err := mgr.LoadDepsManifest()
	require.NoError(t, err)
	assert.NotNil(t, deps)

	installed, exists := deps.Libraries[lib.Key()]
	assert.True(t, exists)
	assert.Equal(t, "1.0.0", installed.Version)
}
