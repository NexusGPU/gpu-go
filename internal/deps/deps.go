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
	"sort"
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

	// ReleaseManifestFile is the filename for the cached releases manifest (from API sync)
	ReleaseManifestFile = "releases-manifest.json"

	// DepsManifestFile is the filename for the required dependencies manifest
	DepsManifestFile = "deps-manifest.json"

	// DownloadedManifestFile is the filename for the downloaded dependencies manifest
	DownloadedManifestFile = "downloaded-manifest.json"

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

// Key returns a unique identifier for this library (name + platform + arch)
// Note: We don't include version in the key because we want to track a single
// version per library name/platform/arch combination
func (l Library) Key() string {
	return fmt.Sprintf("%s:%s:%s", l.Name, l.Platform, l.Arch)
}

// ReleaseManifest represents the global releases manifest synced from API
type ReleaseManifest struct {
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Libraries []Library `json:"libraries"`
}

// DepsManifest represents the required dependencies for current environment
type DepsManifest struct {
	UpdatedAt time.Time          `json:"updated_at"`
	Libraries map[string]Library `json:"libraries"` // key -> library
}

// DownloadedManifest represents locally downloaded dependencies
type DownloadedManifest struct {
	UpdatedAt time.Time          `json:"updated_at"`
	Libraries map[string]Library `json:"libraries"` // key -> library
}

// DownloadStatus represents the status of a library during download
type DownloadStatus string

const (
	DownloadStatusNew      DownloadStatus = "new"
	DownloadStatusUpdated  DownloadStatus = "updated"
	DownloadStatusExisting DownloadStatus = "existing"
	DownloadStatusFailed   DownloadStatus = "failed"
)

// DownloadResult represents the result of downloading a library
type DownloadResult struct {
	Library Library        `json:"library"`
	Status  DownloadStatus `json:"status"`
	Error   string         `json:"error,omitempty"`
}

// UpdateDiff represents the difference between deps-manifest and downloaded-manifest
type UpdateDiff struct {
	ToDownload []Library `json:"to_download"` // new or version mismatch
	UpToDate   []Library `json:"up_to_date"`  // already downloaded with correct version
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

// SyncReleases fetches releases from the API and caches them locally as release-manifest
// If os and arch are empty strings, uses the current platform
// Returns the synced manifest for verbose output
func (m *Manager) SyncReleases(ctx context.Context, osStr, arch string) (*ReleaseManifest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Fetch all releases (max 500)
	releasesResp, err := m.apiClient.GetReleases(ctx, "", 500)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch releases from API: %w", err)
	}

	// Convert API releases to internal manifest format
	manifest := &ReleaseManifest{
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
		// Find matching artifacts for target platform
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
	if err := m.saveReleaseManifest(manifest); err != nil {
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

// LoadReleaseManifest loads the release manifest from local storage
func (m *Manager) LoadReleaseManifest() (*ReleaseManifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	manifestPath := filepath.Join(m.paths.ConfigDir(), ReleaseManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No cached manifest
		}
		return nil, fmt.Errorf("failed to read release manifest: %w", err)
	}

	var manifest ReleaseManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to decode release manifest: %w", err)
	}

	return &manifest, nil
}

// saveReleaseManifest saves the release manifest to local cache
func (m *Manager) saveReleaseManifest(manifest *ReleaseManifest) error {
	manifestPath := filepath.Join(m.paths.ConfigDir(), ReleaseManifestFile)

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

// FetchReleaseManifest loads the release manifest, and syncs from API if not available or outdated
// Returns (manifest, synced, error) where synced is true if auto-sync was performed
func (m *Manager) FetchReleaseManifest(ctx context.Context) (*ReleaseManifest, bool, error) {
	// Try to load from cache first
	manifest, err := m.LoadReleaseManifest()
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
		klog.Infof("Release manifest missing or outdated (last sync: %s), syncing from API...", lastSync)
		newManifest, err := m.SyncReleases(ctx, "", "")
		if err != nil {
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
// If type is empty string, matches any type
func (m *Manager) GetLibrariesForPlatform(manifest *ReleaseManifest, osStr, arch, libType string) []Library {
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

// GetAllLibraries returns all libraries from the manifest
func (m *Manager) GetAllLibraries(manifest *ReleaseManifest) []Library {
	return manifest.Libraries
}

// SelectRequiredDeps selects the required dependencies from release manifest
// For each library type, it selects all artifacts from the latest version
// This ensures that types with multiple files (like remote-gpu-client) get all files
func (m *Manager) SelectRequiredDeps(manifest *ReleaseManifest) *DepsManifest {
	deps := &DepsManifest{
		UpdatedAt: time.Now(),
		Libraries: make(map[string]Library),
	}

	// Group libraries by type and version
	typeVersionLibs := make(map[string]map[string][]Library) // type -> version -> []Library

	for _, lib := range manifest.Libraries {
		if lib.Type == "" {
			continue
		}
		if _, ok := typeVersionLibs[lib.Type]; !ok {
			typeVersionLibs[lib.Type] = make(map[string][]Library)
		}
		typeVersionLibs[lib.Type][lib.Version] = append(typeVersionLibs[lib.Type][lib.Version], lib)
	}

	// For each type, find the latest version and include ALL its artifacts
	for libType, versionLibs := range typeVersionLibs {
		// Get all versions and sort them (newest first)
		versions := make([]string, 0, len(versionLibs))
		for v := range versionLibs {
			versions = append(versions, v)
		}
		// Sort versions in descending order (simple string comparison works for semver-like versions)
		sort.Slice(versions, func(i, j int) bool {
			return versions[i] > versions[j]
		})

		if len(versions) == 0 {
			continue
		}

		// Take the latest version and include all its artifacts
		latestVersion := versions[0]
		libs := versionLibs[latestVersion]

		klog.V(4).Infof("Selected latest version for type %s: %s (%d artifacts)", libType, latestVersion, len(libs))

		for _, lib := range libs {
			deps.Libraries[lib.Key()] = lib
		}
	}

	return deps
}

// LoadDepsManifest loads the deps manifest from local storage
func (m *Manager) LoadDepsManifest() (*DepsManifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.loadDepsManifestUnsafe()
}

func (m *Manager) loadDepsManifestUnsafe() (*DepsManifest, error) {
	manifestPath := filepath.Join(m.paths.ConfigDir(), DepsManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read deps manifest: %w", err)
	}

	var manifest DepsManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to decode deps manifest: %w", err)
	}

	return &manifest, nil
}

// SaveDepsManifest saves the deps manifest to local storage
func (m *Manager) SaveDepsManifest(manifest *DepsManifest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.saveDepsManifestUnsafe(manifest)
}

func (m *Manager) saveDepsManifestUnsafe(manifest *DepsManifest) error {
	manifestPath := filepath.Join(m.paths.ConfigDir(), DepsManifestFile)

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode deps manifest: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return os.WriteFile(manifestPath, data, 0644)
}

// LoadDownloadedManifest loads the downloaded manifest from local storage
func (m *Manager) LoadDownloadedManifest() (*DownloadedManifest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.loadDownloadedManifestUnsafe()
}

func (m *Manager) loadDownloadedManifestUnsafe() (*DownloadedManifest, error) {
	manifestPath := filepath.Join(m.paths.ConfigDir(), DownloadedManifestFile)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &DownloadedManifest{Libraries: make(map[string]Library)}, nil
		}
		return nil, fmt.Errorf("failed to read downloaded manifest: %w", err)
	}

	var manifest DownloadedManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to decode downloaded manifest: %w", err)
	}

	if manifest.Libraries == nil {
		manifest.Libraries = make(map[string]Library)
	}

	return &manifest, nil
}

func (m *Manager) saveDownloadedManifestUnsafe(manifest *DownloadedManifest) error {
	manifestPath := filepath.Join(m.paths.ConfigDir(), DownloadedManifestFile)

	manifest.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode downloaded manifest: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	return os.WriteFile(manifestPath, data, 0644)
}

// ComputeUpdateDiff computes the difference between deps manifest and downloaded manifest
func (m *Manager) ComputeUpdateDiff() (*UpdateDiff, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	deps, err := m.loadDepsManifestUnsafe()
	if err != nil {
		return nil, err
	}
	if deps == nil {
		return &UpdateDiff{}, nil
	}

	downloaded, err := m.loadDownloadedManifestUnsafe()
	if err != nil {
		return nil, err
	}

	diff := &UpdateDiff{
		ToDownload: []Library{},
		UpToDate:   []Library{},
	}

	for key, lib := range deps.Libraries {
		downloadedLib, exists := downloaded.Libraries[key]
		if !exists || downloadedLib.Version != lib.Version {
			diff.ToDownload = append(diff.ToDownload, lib)
		} else {
			// Also verify the file actually exists
			filePath := filepath.Join(m.paths.CacheDir(), lib.Name)
			if _, err := os.Stat(filePath); os.IsNotExist(err) {
				diff.ToDownload = append(diff.ToDownload, lib)
			} else {
				diff.UpToDate = append(diff.UpToDate, lib)
			}
		}
	}

	return diff, nil
}

// DownloadLibrary downloads a library to the cache directory
func (m *Manager) DownloadLibrary(ctx context.Context, lib Library, progressFn func(downloaded, total int64)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.downloadLibraryUnsafe(ctx, lib, progressFn)
}

func (m *Manager) downloadLibraryUnsafe(ctx context.Context, lib Library, progressFn func(downloaded, total int64)) error {
	// Ensure cache directory exists
	cacheDir := m.paths.CacheDir()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Destination path
	destPath := filepath.Join(cacheDir, lib.Name)
	tmpPath := destPath + ".tmp"

	// Check if already downloaded with correct version
	downloaded, _ := m.loadDownloadedManifestUnsafe()
	if downloaded != nil {
		if existingLib, exists := downloaded.Libraries[lib.Key()]; exists {
			if existingLib.Version == lib.Version {
				// Check if file actually exists
				if info, err := os.Stat(destPath); err == nil {
					// File exists and version matches - verify hash if available
					if lib.SHA256 == "" || m.verifyLibraryUnsafe(destPath, lib.SHA256) {
						// Update size from actual file if needed
						if lib.Size == 0 {
							lib.Size = info.Size()
						}
						return nil // Already downloaded
					}
				}
			}
		}
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
	var downloadedBytes int64
	reader := io.TeeReader(resp.Body, hash)

	buf := make([]byte, 32*1024)
	for {
		n, readErr := reader.Read(buf)
		if n > 0 {
			if _, writeErr := tmpFile.Write(buf[:n]); writeErr != nil {
				_ = tmpFile.Close()
				return fmt.Errorf("failed to write file: %w", writeErr)
			}
			downloadedBytes += int64(n)
			if progressFn != nil {
				progressFn(downloadedBytes, lib.Size)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("failed to read response: %w", readErr)
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
	if !platform.IsWindows() {
		if err := os.Chmod(destPath, 0755); err != nil {
			return fmt.Errorf("failed to set permissions: %w", err)
		}
	}

	// Update size if it was zero (discovered during download)
	if lib.Size == 0 {
		lib.Size = downloadedBytes
	}

	// Update downloaded manifest
	if err := m.updateDownloadedManifestUnsafe(lib); err != nil {
		klog.Warningf("Failed to update downloaded manifest: %v", err)
	}

	return nil
}

// updateDownloadedManifestUnsafe updates the downloaded manifest with a library
// Caller must hold m.mu lock
func (m *Manager) updateDownloadedManifestUnsafe(lib Library) error {
	manifest, err := m.loadDownloadedManifestUnsafe()
	if err != nil {
		manifest = &DownloadedManifest{Libraries: make(map[string]Library)}
	}

	manifest.Libraries[lib.Key()] = lib
	return m.saveDownloadedManifestUnsafe(manifest)
}

// DownloadAllRequired downloads all libraries in deps manifest that need downloading
// Returns the download results for each library
func (m *Manager) DownloadAllRequired(ctx context.Context, progressFn func(lib Library, downloaded, total int64)) ([]DownloadResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	deps, err := m.loadDepsManifestUnsafe()
	if err != nil {
		return nil, err
	}
	if deps == nil || len(deps.Libraries) == 0 {
		return nil, nil
	}

	downloaded, err := m.loadDownloadedManifestUnsafe()
	if err != nil {
		downloaded = &DownloadedManifest{Libraries: make(map[string]Library)}
	}

	var results []DownloadResult

	for key, lib := range deps.Libraries {
		result := DownloadResult{Library: lib}

		// Check if already downloaded with correct version
		downloadedLib, exists := downloaded.Libraries[key]
		filePath := filepath.Join(m.paths.CacheDir(), lib.Name)
		var fileInfo os.FileInfo
		fileInfo, statErr := os.Stat(filePath)
		fileExists := statErr == nil

		if exists && downloadedLib.Version == lib.Version && fileExists {
			result.Status = DownloadStatusExisting
			// Use actual file size if available (API may return 0)
			if fileInfo != nil && fileInfo.Size() > 0 {
				result.Library.Size = fileInfo.Size()
			} else if downloadedLib.Size > 0 {
				result.Library.Size = downloadedLib.Size
			}
			results = append(results, result)
			continue
		}

		// Determine if this is new or updated
		if exists && fileExists {
			result.Status = DownloadStatusUpdated
		} else {
			result.Status = DownloadStatusNew
		}

		// Download the library
		libProgressFn := func(d, t int64) {
			if progressFn != nil {
				progressFn(lib, d, t)
			}
		}

		if err := m.downloadLibraryUnsafe(ctx, lib, libProgressFn); err != nil {
			result.Status = DownloadStatusFailed
			result.Error = err.Error()
			klog.Errorf("Failed to download library: name=%s error=%v", lib.Name, err)
		}

		results = append(results, result)
	}

	return results, nil
}

// VerifyLibrary checks if a file exists and has the expected hash
func (m *Manager) VerifyLibrary(path, expectedHash string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.verifyLibraryUnsafe(path, expectedHash)
}

func (m *Manager) verifyLibraryUnsafe(path, expectedHash string) bool {
	// Skip verification if hash is empty
	if expectedHash == "" {
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

// GetLibraryPath returns the path to a library in cache
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

// EnsureLibraryByType ensures a library of the specified type exists and is downloaded
// If not available, it will auto-sync and download on demand
// Returns the path to the library
func (m *Manager) EnsureLibraryByType(ctx context.Context, libType string, vendorSlug string) (string, error) {
	// First, try to find in deps manifest
	deps, err := m.LoadDepsManifest()
	if err != nil {
		return "", err
	}

	// If deps manifest doesn't exist or doesn't have the library, sync and create
	if deps == nil {
		if err := m.ensureDepsManifest(ctx); err != nil {
			return "", err
		}
		deps, err = m.LoadDepsManifest()
		if err != nil {
			return "", err
		}
	}

	// Find library of the specified type
	var targetLib *Library
	for _, lib := range deps.Libraries {
		if lib.Type == libType {
			if vendorSlug == "" || lib.VendorSlug == vendorSlug {
				libCopy := lib
				targetLib = &libCopy
				break
			}
		}
	}

	// If not found in deps, sync and try again
	if targetLib == nil {
		klog.Infof("Library type %s not found in deps manifest, syncing releases...", libType)
		if err := m.ensureDepsManifest(ctx); err != nil {
			return "", err
		}
		deps, err = m.LoadDepsManifest()
		if err != nil {
			return "", err
		}

		for _, lib := range deps.Libraries {
			if lib.Type == libType {
				if vendorSlug == "" || lib.VendorSlug == vendorSlug {
					libCopy := lib
					targetLib = &libCopy
					break
				}
			}
		}
	}

	if targetLib == nil {
		return "", fmt.Errorf("library of type %s (vendor: %s) not found in releases", libType, vendorSlug)
	}

	// Check if downloaded
	filePath := m.GetLibraryPath(targetLib.Name)
	if _, err := os.Stat(filePath); err == nil {
		// Verify if needed
		if targetLib.SHA256 == "" || m.VerifyLibrary(filePath, targetLib.SHA256) {
			return filePath, nil
		}
	}

	// Download on demand
	klog.Infof("Downloading library on demand: name=%s version=%s type=%s", targetLib.Name, targetLib.Version, libType)
	if err := m.DownloadLibrary(ctx, *targetLib, nil); err != nil {
		return "", fmt.Errorf("failed to download library: %w", err)
	}

	return filePath, nil
}

// ensureDepsManifest ensures deps manifest exists by syncing releases and selecting required deps
func (m *Manager) ensureDepsManifest(ctx context.Context) error {
	// Sync releases first
	releaseManifest, _, err := m.FetchReleaseManifest(ctx)
	if err != nil {
		return err
	}

	// Select required deps
	deps := m.SelectRequiredDeps(releaseManifest)

	// Save deps manifest
	return m.SaveDepsManifest(deps)
}

// GetRemoteGPUWorkerPath returns the path to the remote-gpu-worker binary
// It ensures the library exists, downloading if necessary
func (m *Manager) GetRemoteGPUWorkerPath(ctx context.Context) (string, error) {
	return m.EnsureLibraryByType(ctx, LibraryTypeRemoteGPUWorker, "")
}

// EnsureLibrariesByTypes ensures ALL libraries of the specified types exist and are downloaded
// This is different from EnsureLibraryByType which only returns one library
// Returns the list of all libraries that were checked/downloaded
func (m *Manager) EnsureLibrariesByTypes(ctx context.Context, libTypes []string, progressFn func(lib Library, downloaded, total int64)) ([]Library, error) {
	// Ensure deps manifest exists and is up to date
	if err := m.ensureDepsManifest(ctx); err != nil {
		return nil, err
	}

	deps, err := m.LoadDepsManifest()
	if err != nil {
		return nil, err
	}
	if deps == nil {
		return nil, fmt.Errorf("deps manifest is empty after sync")
	}

	// Create a set of target types for quick lookup
	typeSet := make(map[string]bool)
	for _, t := range libTypes {
		typeSet[t] = true
	}

	// Find all libraries matching the specified types
	var targetLibs []Library
	for _, lib := range deps.Libraries {
		if typeSet[lib.Type] {
			targetLibs = append(targetLibs, lib)
		}
	}

	if len(targetLibs) == 0 {
		klog.Warningf("No libraries found for types: %v", libTypes)
		return nil, nil
	}

	// Check which ones need to be downloaded
	downloaded, _ := m.LoadDownloadedManifest()
	if downloaded == nil {
		downloaded = &DownloadedManifest{Libraries: make(map[string]Library)}
	}

	var toDownload []Library

	for _, lib := range targetLibs {
		filePath := m.GetLibraryPath(lib.Name)
		downloadedLib, exists := downloaded.Libraries[lib.Key()]

		needsDownload := false
		if !exists {
			needsDownload = true
		} else if downloadedLib.Version != lib.Version {
			needsDownload = true
		} else if _, err := os.Stat(filePath); os.IsNotExist(err) {
			needsDownload = true
		} else if lib.SHA256 != "" && !m.VerifyLibrary(filePath, lib.SHA256) {
			needsDownload = true
		}

		if needsDownload {
			toDownload = append(toDownload, lib)
		}
	}

	// Download missing libraries
	for _, lib := range toDownload {
		libProgressFn := func(d, t int64) {
			if progressFn != nil {
				progressFn(lib, d, t)
			}
		}

		klog.Infof("Downloading library: name=%s version=%s type=%s", lib.Name, lib.Version, lib.Type)
		if err := m.DownloadLibrary(ctx, lib, libProgressFn); err != nil {
			return nil, fmt.Errorf("failed to download library %s: %w", lib.Name, err)
		}
	}

	// Return all target libraries
	return targetLibs, nil
}

// CheckUpdates checks if deps manifest has updates compared to downloaded manifest
// Returns the libraries that need to be downloaded
func (m *Manager) CheckUpdates(ctx context.Context) ([]Library, error) {
	// Ensure we have latest release manifest
	_, _, err := m.FetchReleaseManifest(ctx)
	if err != nil {
		return nil, err
	}

	// Compute diff
	diff, err := m.ComputeUpdateDiff()
	if err != nil {
		return nil, err
	}

	return diff.ToDownload, nil
}

// UpdateDepsManifest syncs releases and updates the deps manifest with latest versions
// Returns (updated deps manifest, list of changes from old deps)
func (m *Manager) UpdateDepsManifest(ctx context.Context) (*DepsManifest, []Library, error) {
	// Load old deps manifest to compare
	oldDeps, _ := m.LoadDepsManifest()

	// Sync releases
	releaseManifest, _, err := m.FetchReleaseManifest(ctx)
	if err != nil {
		return nil, nil, err
	}

	// Select new required deps
	newDeps := m.SelectRequiredDeps(releaseManifest)

	// Compare and find changes
	var changes []Library
	if oldDeps != nil {
		for key, newLib := range newDeps.Libraries {
			if oldLib, exists := oldDeps.Libraries[key]; !exists || oldLib.Version != newLib.Version {
				changes = append(changes, newLib)
			}
		}
	} else {
		// All are new
		for _, lib := range newDeps.Libraries {
			changes = append(changes, lib)
		}
	}

	// Save new deps manifest
	if err := m.SaveDepsManifest(newDeps); err != nil {
		return nil, nil, err
	}

	return newDeps, changes, nil
}

// ========== Backward compatibility aliases ==========

// Manifest is an alias for ReleaseManifest for backward compatibility
type Manifest = ReleaseManifest

// LocalManifest is an alias for DepsManifest for backward compatibility
type LocalManifest = DepsManifest

// LoadCachedManifest is an alias for LoadReleaseManifest
func (m *Manager) LoadCachedManifest() (*ReleaseManifest, error) {
	return m.LoadReleaseManifest()
}

// FetchManifest is an alias for FetchReleaseManifest
func (m *Manager) FetchManifest(ctx context.Context) (*ReleaseManifest, bool, error) {
	return m.FetchReleaseManifest(ctx)
}

// GetInstalledLibraries returns the deps manifest (renamed from installed)
func (m *Manager) GetInstalledLibraries() (*DepsManifest, error) {
	deps, err := m.LoadDepsManifest()
	if err != nil {
		return nil, err
	}
	if deps == nil {
		return &DepsManifest{Libraries: make(map[string]Library)}, nil
	}
	return deps, nil
}

// GetDownloadedLibraries returns the downloaded manifest
func (m *Manager) GetDownloadedLibraries() (*DownloadedManifest, error) {
	return m.LoadDownloadedManifest()
}

// InstallLibrary marks a library as required (adds to deps manifest)
func (m *Manager) InstallLibrary(lib Library) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	deps, err := m.loadDepsManifestUnsafe()
	if err != nil {
		deps = &DepsManifest{Libraries: make(map[string]Library)}
	}
	if deps == nil {
		deps = &DepsManifest{Libraries: make(map[string]Library)}
	}

	deps.Libraries[lib.Key()] = lib
	deps.UpdatedAt = time.Now()

	return m.saveDepsManifestUnsafe(deps)
}
