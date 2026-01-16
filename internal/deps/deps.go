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

	"github.com/NexusGPU/gpu-go/internal/platform"
)

const (
	// DefaultCDNBaseURL is the default CDN for downloading dependencies
	DefaultCDNBaseURL = "https://cdn.tensor-fusion.ai"

	// ManifestPath is the path to the version manifest on CDN
	ManifestPath = "/vgpu/manifest.json"
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
}

// Manifest represents the version manifest from CDN
type Manifest struct {
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	Libraries []Library `json:"libraries"`
}

// LocalManifest represents locally installed dependencies
type LocalManifest struct {
	InstalledAt time.Time           `json:"installed_at"`
	Libraries   map[string]Library  `json:"libraries"` // name -> library
}

// Manager manages dependency downloads and versions
type Manager struct {
	cdnBaseURL string
	paths      *platform.Paths
	httpClient *http.Client
	mu         sync.RWMutex
}

// NewManager creates a new dependency manager
func NewManager(opts ...ManagerOption) *Manager {
	m := &Manager{
		cdnBaseURL: DefaultCDNBaseURL,
		paths:      platform.DefaultPaths(),
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
	for _, opt := range opts {
		opt(m)
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

// FetchManifest fetches the version manifest from CDN
func (m *Manager) FetchManifest(ctx context.Context) (*Manifest, error) {
	url := m.cdnBaseURL + ManifestPath
	
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch manifest: status %d", resp.StatusCode)
	}

	var manifest Manifest
	if err := json.NewDecoder(resp.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %w", err)
	}

	return &manifest, nil
}

// GetLibrariesForPlatform returns libraries matching the current platform
func (m *Manager) GetLibrariesForPlatform(manifest *Manifest) []Library {
	var result []Library
	for _, lib := range manifest.Libraries {
		if lib.Platform == runtime.GOOS && lib.Arch == runtime.GOARCH {
			result = append(result, lib)
		}
	}
	return result
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

	// Check if already downloaded with correct hash
	if m.verifyLibrary(destPath, lib.SHA256) {
		return nil // Already downloaded
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

	// Verify hash
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if actualHash != lib.SHA256 {
		return fmt.Errorf("hash mismatch: expected %s, got %s", lib.SHA256, actualHash)
	}

	// Move to final destination
	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to move file: %w", err)
	}

	return nil
}

// InstallLibrary installs a downloaded library to the lib directory
func (m *Manager) InstallLibrary(lib Library) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	cacheDir := m.paths.CacheDir()
	libDir := m.paths.LibDir()

	// Ensure lib directory exists
	if err := os.MkdirAll(libDir, 0755); err != nil {
		return fmt.Errorf("failed to create lib directory: %w", err)
	}

	srcPath := filepath.Join(cacheDir, lib.Name)
	destPath := filepath.Join(libDir, lib.Name)

	// Copy file (don't move, keep in cache)
	if err := copyFile(srcPath, destPath); err != nil {
		return fmt.Errorf("failed to install library: %w", err)
	}

	// Make executable on Unix
	if runtime.GOOS != "windows" {
		if err := os.Chmod(destPath, 0755); err != nil {
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

	manifestPath := filepath.Join(m.paths.ConfigDir(), "deps-manifest.json")
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

// CheckUpdates checks if any installed libraries have updates available
func (m *Manager) CheckUpdates(ctx context.Context) ([]Library, error) {
	manifest, err := m.FetchManifest(ctx)
	if err != nil {
		return nil, err
	}

	installed, err := m.GetInstalledLibraries()
	if err != nil {
		return nil, err
	}

	var updates []Library
	for _, lib := range m.GetLibrariesForPlatform(manifest) {
		installedLib, exists := installed.Libraries[lib.Name]
		if !exists || installedLib.Version != lib.Version {
			updates = append(updates, lib)
		}
	}

	return updates, nil
}

// GetLibraryPath returns the path to an installed library
func (m *Manager) GetLibraryPath(name string) string {
	return filepath.Join(m.paths.LibDir(), name)
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

	return nil
}

// verifyLibrary checks if a file exists and has the expected hash
func (m *Manager) verifyLibrary(path, expectedHash string) bool {
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
	manifestPath := filepath.Join(m.paths.ConfigDir(), "deps-manifest.json")

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

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
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
