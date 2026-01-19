package studio

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// CDN base URL for TensorFusion libraries
	DefaultCDNURL = "https://cdn.tensor-fusion.ai"

	// Library version - this would normally come from config
	DefaultLibVersion = "v1.0.0"
)

// LibraryDownloader handles downloading versioned GPU libraries from CDN
type LibraryDownloader struct {
	cdnURL   string
	version  string
	cacheDir string
}

// NewLibraryDownloader creates a new library downloader
func NewLibraryDownloader(cdnURL, version string) *LibraryDownloader {
	if cdnURL == "" {
		cdnURL = DefaultCDNURL
	}
	if version == "" {
		version = DefaultLibVersion
	}

	// Default cache directory
	cacheDir := "/tmp/tensor-fusion/libs"
	if home, err := os.UserHomeDir(); err == nil {
		cacheDir = filepath.Join(home, ".tensor-fusion", "libs")
	}

	return &LibraryDownloader{
		cdnURL:   cdnURL,
		version:  version,
		cacheDir: cacheDir,
	}
}

// Architecture detection
type Architecture struct {
	OS   string // darwin, linux, windows
	Arch string // arm64, amd64, 386
}

// DetectArchitecture detects the current system architecture
func DetectArchitecture() Architecture {
	arch := Architecture{
		OS:   runtime.GOOS,
		Arch: runtime.GOARCH,
	}

	// Normalize architecture names
	switch arch.Arch {
	case "amd64":
		arch.Arch = "amd64"
	case "arm64":
		arch.Arch = "arm64"
	case "386":
		arch.Arch = "386"
	default:
		// Keep as-is for other architectures
	}

	return arch
}

// String returns the architecture string for CDN lookup
func (a Architecture) String() string {
	return fmt.Sprintf("%s-%s", a.OS, a.Arch)
}

// LibraryInfo contains information about a library to download
type LibraryInfo struct {
	Name     string
	Version  string
	Arch     Architecture
	Checksum string // SHA256 checksum
}

// DownloadURL constructs the CDN download URL for a library
func (d *LibraryDownloader) DownloadURL(lib LibraryInfo) string {
	// Format: https://cdn.tensor-fusion.ai/{version}/{os}-{arch}/lib{name}.so
	var ext string
	switch lib.Arch.OS {
	case "darwin":
		ext = "dylib"
	case "windows":
		ext = "dll"
	default:
		ext = "so"
	}

	return fmt.Sprintf("%s/%s/%s/lib%s.%s",
		d.cdnURL,
		lib.Version,
		lib.Arch.String(),
		lib.Name,
		ext,
	)
}

// Download downloads a library from the CDN
func (d *LibraryDownloader) Download(lib LibraryInfo) (string, error) {
	url := d.DownloadURL(lib)

	// Create cache directory
	if err := os.MkdirAll(d.cacheDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Determine library filename
	var ext string
	switch lib.Arch.OS {
	case "darwin":
		ext = "dylib"
	case "windows":
		ext = "dll"
	default:
		ext = "so"
	}

	filename := fmt.Sprintf("lib%s.%s", lib.Name, ext)
	destPath := filepath.Join(d.cacheDir, filename)

	// Check if already downloaded and verified
	if d.isValidCache(destPath, lib.Checksum) {
		fmt.Printf("Library %s already cached at %s\n", lib.Name, destPath)
		return destPath, nil
	}

	// Download the library
	fmt.Printf("Downloading %s from %s...\n", lib.Name, url)

	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download library: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download library: HTTP %d", resp.StatusCode)
	}

	// Create temporary file
	tmpFile := destPath + ".tmp"
	out, err := os.Create(tmpFile)
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %w", err)
	}
	defer func() { _ = out.Close() }()

	// Copy with progress
	hasher := sha256.New()
	writer := io.MultiWriter(out, hasher)

	_, err = io.Copy(writer, resp.Body)
	if err != nil {
		_ = os.Remove(tmpFile)
		return "", fmt.Errorf("failed to download library: %w", err)
	}

	// Verify checksum if provided
	if lib.Checksum != "" {
		downloadedChecksum := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(downloadedChecksum, lib.Checksum) {
			_ = os.Remove(tmpFile)
			return "", fmt.Errorf("checksum mismatch: expected %s, got %s", lib.Checksum, downloadedChecksum)
		}
	}

	// Move to final location
	if err := os.Rename(tmpFile, destPath); err != nil {
		_ = os.Remove(tmpFile)
		return "", fmt.Errorf("failed to move library to final location: %w", err)
	}

	// Set permissions
	if err := os.Chmod(destPath, 0755); err != nil {
		return "", fmt.Errorf("failed to set permissions: %w", err)
	}

	fmt.Printf("Successfully downloaded %s to %s\n", lib.Name, destPath)
	return destPath, nil
}

// isValidCache checks if a cached library is valid
func (d *LibraryDownloader) isValidCache(path, expectedChecksum string) bool {
	// Check if file exists
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return false
	}

	// If no checksum provided, assume valid
	if expectedChecksum == "" {
		return true
	}

	// Verify checksum
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return false
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))
	return strings.EqualFold(checksum, expectedChecksum)
}

// DownloadDefaultLibraries downloads the default set of GPU libraries
func (d *LibraryDownloader) DownloadDefaultLibraries() ([]string, error) {
	arch := DetectArchitecture()

	// List of libraries to download
	libraries := []LibraryInfo{
		{
			Name:    "cuda-vgpu",
			Version: d.version,
			Arch:    arch,
		},
		{
			Name:    "cudart",
			Version: d.version,
			Arch:    arch,
		},
		{
			Name:    "tensor-fusion-runtime",
			Version: d.version,
			Arch:    arch,
		},
	}

	var paths []string
	for _, lib := range libraries {
		path, err := d.Download(lib)
		if err != nil {
			// Log error but continue with other libraries
			fmt.Printf("Warning: failed to download %s: %v\n", lib.Name, err)
			continue
		}
		paths = append(paths, path)
	}

	if len(paths) == 0 {
		return nil, fmt.Errorf("failed to download any libraries")
	}

	return paths, nil
}

// GetCacheDir returns the cache directory path
func (d *LibraryDownloader) GetCacheDir() string {
	return d.cacheDir
}

// SetCacheDir sets a custom cache directory
func (d *LibraryDownloader) SetCacheDir(dir string) {
	d.cacheDir = dir
}
