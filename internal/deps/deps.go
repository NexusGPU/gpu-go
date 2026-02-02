// Package deps manages external dependencies for GPU Go, including
// downloading and managing vgpu libraries from the CDN.
package deps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"k8s.io/klog/v2"
)

const (
	// DefaultCDNBaseURL is the default CDN for downloading dependencies
	DefaultCDNBaseURL = "https://cdn.tensor-fusion.ai"

	// ManifestPath is the path to the version manifest on CDN
	ManifestPath = "/vgpu/manifest.json"

	// CachedManifestFile is the filename for the cached releases manifest
	CachedManifestFile = "releases-manifest.json"

	// DownloadedManifestFile is the filename for the downloaded dependencies manifest
	DownloadedManifestFile = "downloaded-manifest.json"

	// InstalledManifestFile is the filename for the installed dependencies manifest
	InstalledManifestFile = "deps-manifest.json"

	// AutoSyncInterval is the interval for auto-syncing the manifest
	AutoSyncInterval = 7 * 24 * time.Hour
)

// Library type constants
const (
	LibraryTypeVGPULibrary     = "vgpu-library"
	LibraryTypeRemoteGPUWorker = "remote-gpu-worker"
	LibraryTypeRemoteGPUClient = "remote-gpu-client"
)

// Library represents a downloadable library
type Library struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Platform string `json:"platform"` // linux, darwin, windows
	Arch     string `json:"arch"`     // amd64, arm64
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Type     string `json:"type,omitempty"` // e.g., "vgpu-library", "remote-gpu-worker", "remote-gpu-client"
	// Vendor information from release
	VendorSlug string `json:"vendorSlug,omitempty"` // e.g., "stub", "nvidia", "amd"
	VendorName string `json:"vendorName,omitempty"` // e.g., "STUB", "NVIDIA", "AMD"
}

// Manifest represents the version manifest from CDN
type Manifest struct {
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Libraries []Library `json:"libraries"`
}

// LocalManifest represents locally installed dependencies
type LocalManifest struct {
	InstalledAt time.Time          `json:"installed_at"`
	Libraries   map[string]Library `json:"libraries"` // name -> library
}

// DownloadedManifest represents locally downloaded dependencies
type DownloadedManifest struct {
	UpdatedAt time.Time          `json:"updated_at"`
	Libraries map[string]Library `json:"libraries"` // name -> library
}

// Manager manages dependency downloads and versions
type Manager struct {
	cdnBaseURL string
	apiBaseURL string
	apiClient  *api.Client
	paths      *platform.Paths
	httpClient *http.Client
	mu         sync.RWMutex
}

// NewManager creates a new dependency manager
func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		cdnBaseURL: DefaultCDNBaseURL,
		apiBaseURL: api.GetDefaultBaseURL(),
		paths:      platform.DefaultPaths(),
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
	for _, opt := range opts {
		opt(m)
	}
	// Initialize API client if not set
	if m.apiClient == nil {
		m.apiClient = api.NewClient(api.WithBaseURL(m.apiBaseURL))
	}
	return m
}

// ManagerOption configures the dependency manager
type ManagerOption func(*Manager)

// WithCDNBaseURL sets a custom CDN base URL
func WithCDNBaseURL(url string) ManagerOption {
	return func(m *Manager) {
		m.cdnBaseURL = strings.TrimSuffix(url, "/")
	}
}

// WithPaths sets custom paths
func WithPaths(paths *platform.Paths) ManagerOption {
	return func(m *Manager) {
		m.paths = paths
	}
}

// WithAPIBaseURL sets a custom API base URL
func WithAPIBaseURL(url string) ManagerOption {
	return func(m *Manager) {
		m.apiBaseURL = strings.TrimSuffix(url, "/")
		if m.apiClient != nil {
			m.apiClient.SetBaseURL(m.apiBaseURL)
		} else {
			m.apiClient = api.NewClient(api.WithBaseURL(m.apiBaseURL))
		}
	}
}

// WithAPIClient sets a custom API client
func WithAPIClient(client *api.Client) ManagerOption {
	return func(m *Manager) {
		m.apiClient = client
		if client != nil {
			m.apiBaseURL = client.GetBaseURL()
		}
	}
}

// SyncReleases fetches releases from the API and caches them locally
// If os and arch are empty strings, uses the current platform
// Returns the synced manifest for verbose output
func (m *Manager) SyncReleases(ctx context.Context, osStr, arch string) (*Manifest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Fetch all releases (max 500)
	releasesResp, err := m.apiClient.GetReleases(ctx, "", 500)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases from API: %w", err)
	}

	// Convert API releases to internal manifest format
	manifest := &Manifest{
		Version:   fmt.Sprintf("api-%d", time.Now().Unix()),
		UpdatedAt: time.Now(),
		Libraries: []Library{},
	}

	// Determine target platform
	targetOS := osStr
	targetArch := arch
	if targetOS == "" {
		targetOS = runtime.GOOS
	}
	if targetArch == "" {
		targetArch = runtime.GOARCH
	}

	for _, release := range releasesResp.Releases {
		// Find matching artifact for target platform
		for _, artifact := range release.Artifacts {
			// Map OS names: linux, darwin, windows
			artifactOS := strings.ToLower(artifact.OS)
			if artifactOS == "macos" {
				artifactOS = "darwin"
			}

			// Map arch names: amd64, arm64
			artifactArch := strings.ToLower(artifact.CPUArch)
			if artifactArch == "x86_64" || artifactArch == "x64" {
				artifactArch = "amd64"
			}

			if artifactOS == targetOS && artifactArch == targetArch {
				// Extract library name from URL
				libName := m.extractLibraryName(release.Vendor.Slug, artifact.URL)
				// Skip if no valid library name extracted
				if libName == "" {
					continue
				}

				// Get size from metadata if available, otherwise 0
				size := int64(0)
				if sizeStr, ok := artifact.Metadata["size"]; ok {
					_, _ = fmt.Sscanf(sizeStr, "%d", &size)
				}

				// Get type from metadata if available
				libType := ""
				if typeStr, ok := artifact.Metadata["type"]; ok {
					libType = typeStr
				}

				lib := Library{
					Name:       libName,
					Version:    release.Version,
					Platform:   artifactOS,
					Arch:       artifactArch,
					URL:        artifact.URL,
					SHA256:     artifact.SHA256,
					Size:       size,
					Type:       libType,
					VendorSlug: strings.ToLower(release.Vendor.Slug),
					VendorName: release.Vendor.Name,
				}
				manifest.Libraries = append(manifest.Libraries, lib)
			}
		}
	}

	// Save to cache
	if err := m.saveCachedManifest(manifest); err != nil {
		return nil, err
	}

	klog.Infof("Synced %d libraries for platform %s/%s", len(manifest.Libraries), targetOS, targetArch)

	return manifest, nil
}

// extractLibraryName extracts library name from URL
func (m *Manager) extractLibraryName(vendorSlug, url string) string {
	if url == "" {
		klog.Warningf("Empty URL, cannot extract library name: vendor=%s", vendorSlug)
		return ""
	}

	// Extract the last part of URL
	parts := strings.Split(url, "/")
	if len(parts) == 0 {
		klog.Warningf("Invalid URL, cannot extract library name: vendor=%s url=%s", vendorSlug, url)
		return ""
	}

	filename := parts[len(parts)-1]
	// Remove query params if any
	if idx := strings.Index(filename, "?"); idx >= 0 {
		filename = filename[:idx]
	}
	// Remove fragment if any
	if idx := strings.Index(filename, "#"); idx >= 0 {
		filename = filename[:idx]
	}

	if filename == "" {
		klog.Warningf("Empty filename extracted from URL: vendor=%s url=%s", vendorSlug, url)
		return ""
	}

	return filename
}

// LoadCachedManifest loads the cached manifest from local storage
func (m *Manager) LoadCachedManifest() (*Manifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	manifestPath := filepath.Join(m.paths.ConfigDir(), CachedManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No cached manifest
		}
		return nil, fmt.Errorf("failed to read cached manifest: %w", err)
	}

	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to decode cached manifest: %w", err)
	}

	return &manifest, nil
}

// saveCachedManifest saves the manifest to local cache
func (m *Manager) saveCachedManifest(manifest *Manifest) error {
	manifestPath := filepath.Join(m.paths.ConfigDir(), CachedManifestFile)

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode manifest: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	tmpPath := manifestPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write manifest: %w", err)
	}

	return os.Rename(tmpPath, manifestPath)
}

// FetchManifest loads the cached manifest, and syncs from API if not available or outdated
// Returns true as second argument if an auto-sync was performed and updates were found
func (m *Manager) FetchManifest(ctx context.Context) (*Manifest, bool, error) {
	// Try to load from cache first
	manifest, err := m.LoadCachedManifest()
	if err != nil {
		return nil, false, err
	}

	synced := false
	// If no cached manifest, or it's outdated, sync from API
	if manifest == nil || time.Since(manifest.UpdatedAt) > AutoSyncInterval {
		lastSync := "never"
		if manifest != nil {
			lastSync = manifest.UpdatedAt.Format(time.RFC3339)
		}
		klog.Infof("Manifest missing or outdated (last sync: %s), syncing from API...", lastSync)
		newManifest, err := m.SyncReleases(ctx, "", "")
		if err != nil {
			// If sync fails but we have a stale manifest, return it with a warning?
			// For now, return error to be safe, or just return the stale one if available?
			if manifest != nil {
				klog.Warningf("Failed to sync releases, using stale manifest: %v", err)
				return manifest, false, nil
			}
			return nil, false, fmt.Errorf("failed to sync releases: %w", err)
		}
		manifest = newManifest
		synced = true
	}

	return manifest, synced, nil
}

// GetLibrariesForPlatform returns libraries matching the specified platform and type
// If both os and arch are empty strings, uses the current platform
// If only os is provided, matches any arch for that os
// If only arch is provided, matches any os for that arch
// If type is empty string, matches any type
func (m *Manager) GetLibrariesForPlatform(manifest *Manifest, osStr, arch, libType string) []Library {
	var result []Library
	targetOS := osStr
	targetArch := arch

	// If both are empty, use current platform
	if targetOS == "" && targetArch == "" {
		targetOS = runtime.GOOS
		targetArch = runtime.GOARCH
	}

	for _, lib := range manifest.Libraries {
		osMatch := targetOS == "" || lib.Platform == targetOS
		archMatch := targetArch == "" || lib.Arch == targetArch
		typeMatch := libType == "" || lib.Type == libType
		if osMatch && archMatch && typeMatch {
			result = append(result, lib)
		}
	}
	return result
}

// GetAllLibraries returns all libraries from the manifest, grouped by platform/arch
func (m *Manager) GetAllLibraries(manifest *Manifest) []Library {
	return manifest.Libraries
}

// DownloadLibrary downloads a library to the cache directory
func (m *Manager) DownloadLibrary(ctx context.Context, lib Library, progressFn func(downloaded, total int64)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Ensure cache directory exists
	cacheDir := m.paths.CacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Destination path
	destPath := filepath.Join(cacheDir, lib.Name)
	tmpPath := destPath + ".tmp"

	// Check if already downloaded
	if info, err := os.Stat(destPath); err == nil {
		// File exists - verify hash if available, otherwise trust it
		if lib.SHA256 == "" || m.VerifyLibrary(destPath, lib.SHA256) {
			// Update size from actual file
			lib.Size = info.Size()
			// Update downloaded manifest
			if err := m.updateDownloadedManifest(lib); err != nil {
				klog.Warningf("Failed to update downloaded manifest: %v", err)
			}
			return nil // Already downloaded
		}
		// Hash mismatch - will re-download below
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lib.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download library: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download library: status %d", resp.StatusCode)
	}

	// Create temp file
	tmpFile, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	// Download with progress and hash verification
	hash := sha256.New()
	var downloaded int64
	reader := io.TeeReader(resp.Body, hash)

	buf := make([]byte, 32*1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			if _, writeErr := tmpFile.Write(buf[:n]); writeErr != nil {
				_ = tmpFile.Close()
				return fmt.Errorf("failed to write file: %w", writeErr)
			}
			downloaded += int64(n)
			if progressFn != nil {
				progressFn(downloaded, lib.Size)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to read response: %w", err)
		}
	}
	_ = tmpFile.Close()

	// Verify hash (skip if SHA256 is empty)
	if lib.SHA256 != "" {
		actualHash := hex.EncodeToString(hash.Sum(nil))
		if actualHash != lib.SHA256 {
			return fmt.Errorf("hash mismatch: expected %s, got %s", lib.SHA256, actualHash)
		}
	}

	// Move to final destination
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to move file: %w", err)
	}

	// Set executable permission on Unix
	if runtime.GOOS != "windows" {
		if err := os.Chmod(destPath, 0755); err != nil {
			return fmt.Errorf("failed to set permissions: %w", err)
		}
	}

	// Update size if it was zero (discovered during download)
	if lib.Size == 0 {
		lib.Size = downloaded
	}

	// Update downloaded manifest
	if err := m.updateDownloadedManifest(lib); err != nil {
		klog.Warningf("Failed to update downloaded manifest: %v", err)
	}

	return nil
}

// InstallLibrary installs a downloaded library (marks it as installed)
func (m *Manager) InstallLibrary(lib Library) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cacheDir := m.paths.CacheDir()
	srcPath := filepath.Join(cacheDir, lib.Name)

	// Verify file exists in cache
	if _, err := os.Stat(srcPath); err != nil {
		return fmt.Errorf("library not found in cache: %w", err)
	}

	// Make executable on Unix (in cache directly)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(srcPath, 0755); err != nil {
			return fmt.Errorf("failed to set permissions: %w", err)
		}
	}

	// Update local manifest
	return m.updateLocalManifest(lib)
}

// GetInstalledLibraries returns the locally installed libraries
func (m *Manager) GetInstalledLibraries() (*LocalManifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	manifestPath := filepath.Join(m.paths.ConfigDir(), InstalledManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &LocalManifest{Libraries: make(map[string]Library)}, nil
		}
		return nil, err
	}

	var manifest LocalManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// GetDownloadedLibraries returns the downloaded libraries
func (m *Manager) GetDownloadedLibraries() (*DownloadedManifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	manifestPath := filepath.Join(m.paths.ConfigDir(), DownloadedManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &DownloadedManifest{Libraries: make(map[string]Library)}, nil
		}
		return nil, err
	}

	var manifest DownloadedManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}

	return &manifest, nil
}

// CheckUpdates checks if any installed libraries have updates available
func (m *Manager) CheckUpdates(ctx context.Context) ([]Library, error) {
	manifest, _, err := m.FetchManifest(ctx)
	if err != nil {
		return nil, err
	}

	installed, err := m.GetInstalledLibraries()
	if err != nil {
		return nil, err
	}

	var updates []Library
	for _, lib := range m.GetLibrariesForPlatform(manifest, "", "", "") {
		installedLib, exists := installed.Libraries[lib.Name]
		if !exists || installedLib.Version != lib.Version {
			updates = append(updates, lib)
		}
	}

	return updates, nil
}

// GetLibraryPath returns the path to an installed library (in cache)
func (m *Manager) GetLibraryPath(name string) string {
	return filepath.Join(m.paths.CacheDir(), name)
}

// CleanCache removes all cached downloads
func (m *Manager) CleanCache() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cacheDir := m.paths.CacheDir()
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(cacheDir, entry.Name())); err != nil {
			return err
		}
	}

	// Also remove downloaded manifest
	manifestPath := filepath.Join(m.paths.ConfigDir(), DownloadedManifestFile)
	if err := os.Remove(manifestPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return nil
}

// VerifyLibrary checks if a file exists and has the expected hash
// Returns true if expectedHash is empty (verification skipped)
func (m *Manager) VerifyLibrary(path, expectedHash string) bool {
	// Skip verification if hash is empty
	if expectedHash == "" {
		// Just check if file exists
		_, err := os.Stat(path)
		return err == nil
	}

	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false
	}

	actualHash := hex.EncodeToString(hash.Sum(nil))
	return actualHash == expectedHash
}

// updateLocalManifest updates the local manifest with an installed library
func (m *Manager) updateLocalManifest(lib Library) error {
	manifestPath := filepath.Join(m.paths.ConfigDir(), InstalledManifestFile)

	// Load existing manifest
	var manifest LocalManifest
	data, err := os.ReadFile(manifestPath)
	if err == nil {
		_ = json.Unmarshal(data, &manifest)
	}
	if manifest.Libraries == nil {
		manifest.Libraries = make(map[string]Library)
	}

	manifest.InstalledAt = time.Now()
	manifest.Libraries[lib.Name] = lib

	// Save manifest
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(manifestPath, data, 0644)
}

// updateDownloadedManifest updates the downloaded manifest
func (m *Manager) updateDownloadedManifest(lib Library) error {
	// Note: We don't lock here as it's called from DownloadLibrary which already holds the lock
	// But since DownloadLibrary calls this, we should be careful if we call this from outside
	// However, updateDownloadedManifest is private and only called from DownloadLibrary.
	// But wait, DownloadLibrary holds the lock, so we can't lock again inside unless it's a recursive lock (sync.Mutex is not).
	// So we should expect the caller to hold the lock, or this function should not lock.
	// DownloadLibrary holds m.mu.Lock(). So this function should NOT lock m.mu.
	// But LoadDownloadedManifest locks. We should be careful.
	// Let's implement this without locking the Manager, but locking the file access?
	// Actually, DownloadLibrary holds the lock on the Manager, which protects the file operations too effectively.

	manifestPath := filepath.Join(m.paths.ConfigDir(), DownloadedManifestFile)

	// Load existing manifest
	var manifest DownloadedManifest
	data, err := os.ReadFile(manifestPath)
	if err == nil {
		_ = json.Unmarshal(data, &manifest)
	}
	if manifest.Libraries == nil {
		manifest.Libraries = make(map[string]Library)
	}

	manifest.UpdatedAt = time.Now()
	manifest.Libraries[lib.Name] = lib

	// Save manifest
	data, err = json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(manifestPath, data, 0644)
}

// VGPULibraries returns the standard vGPU library names for the current platform
func VGPULibraries() []string {
	switch runtime.GOOS {
	case "linux":
		return []string{
			"libcuda.so.1",
			"libnvidia-ml.so.1",
			"libcuda-vgpu.so",
		}
	case "windows":
		return []string{
			"nvcuda.dll",
			"nvml.dll",
		}
	default:
		return nil
	}
}

// GetRemoteGPUWorkerPath returns the path to the remote-gpu-worker binary
// It first checks if already installed, if not downloads it from the API
func (m *Manager) GetRemoteGPUWorkerPath(ctx context.Context) (string, error) {
	// Check if already installed
	installed, err := m.GetInstalledLibraries()
	if err != nil {
		return "", fmt.Errorf("failed to get installed libraries: %w", err)
	}

	// Look for existing remote-gpu-worker binary
	for name, lib := range installed.Libraries {
		if lib.Type == LibraryTypeRemoteGPUWorker {
			path := m.GetLibraryPath(name)
			if _, err := os.Stat(path); err == nil {
				return path, nil
			}
		}
	}

	// Not found, try to download from manifest
	manifest, _, err := m.FetchManifest(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}

	// Find remote-gpu-worker for current platform
	libs := m.GetLibrariesForPlatform(manifest, "", "", LibraryTypeRemoteGPUWorker)
	if len(libs) == 0 {
		return "", fmt.Errorf("remote-gpu-worker not found for platform %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	lib := libs[0]
	klog.Infof("Downloading remote-gpu-worker: name=%s version=%s", lib.Name, lib.Version)

	// Download and install
	if err := m.DownloadLibrary(ctx, lib, nil); err != nil {
		return "", fmt.Errorf("failed to download remote-gpu-worker: %w", err)
	}

	if err := m.InstallLibrary(lib); err != nil {
		return "", fmt.Errorf("failed to install remote-gpu-worker: %w", err)
	}

	return m.GetLibraryPath(lib.Name), nil
}
