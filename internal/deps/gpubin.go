// Package deps manages GPU binary tools like nvidia-smi, amdsmi
package deps

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NexusGPU/gpu-go/internal/platform"
	"k8s.io/klog/v2"
)

// OS constants for GPU binary downloads
const (
	osWindows = "windows"
	osLinux   = "linux"
)

// GPUBinaryInfo contains the download URL and expected binary name for a GPU tool
type GPUBinaryInfo struct {
	URL        string // CDN download URL (zip file)
	BinaryName string // Expected binary name (e.g., "nvidia-smi", "amdsmi")
}

// GPUBinaryRegistry maps vendor -> os -> arch -> GPUBinaryInfo
// TODO: Fill in actual CDN URLs
var GPUBinaryRegistry = map[string]map[string]map[string]GPUBinaryInfo{
	"nvidia": {
		"linux": {
			"amd64": {URL: "", BinaryName: "nvidia-smi"},
			"arm64": {URL: "", BinaryName: "nvidia-smi"},
		},
		"windows": {
			"amd64": {URL: "", BinaryName: "nvidia-smi"},
		},
	},
	"amd": {
		"linux": {
			"amd64": {URL: "", BinaryName: "amdsmi"},
			"arm64": {URL: "", BinaryName: "amdsmi"},
		},
	},
}

// GetGPUBinaryInfo returns the download URL and binary name for a GPU vendor/os/arch combination
// Returns nil if no binary is available for this combination
func GetGPUBinaryInfo(vendor, osName, arch string) *GPUBinaryInfo {
	vendor = strings.ToLower(vendor)
	osName = strings.ToLower(osName)
	arch = strings.ToLower(arch)

	// Map common arch aliases
	if arch == "x86_64" || arch == "x64" {
		arch = "amd64"
	}
	if arch == "aarch64" {
		arch = "arm64"
	}

	if osMap, ok := GPUBinaryRegistry[vendor]; ok {
		if archMap, ok := osMap[osName]; ok {
			if info, ok := archMap[arch]; ok {
				if info.URL != "" {
					return &info
				}
			}
		}
	}
	return nil
}

// GetGPUBinaryPath returns the expected path for a GPU binary in the cache
// The binary will be stored in ~/.gpugo/cache/bin/
func GetGPUBinaryPath(paths *platform.Paths, binaryName string) string {
	binDir := filepath.Join(paths.CacheDir(), "bin")
	name := binaryName
	if runtime.GOOS == osWindows {
		name += ".exe"
	}
	return filepath.Join(binDir, name)
}

// EnsureGPUBinary ensures the GPU binary (like nvidia-smi) exists in the cache
// Downloads and extracts if not present
// Returns the path to the binary, or empty string if not available for this platform
func EnsureGPUBinary(ctx context.Context, paths *platform.Paths, vendor string) (string, error) {
	return EnsureGPUBinaryForPlatform(ctx, paths, vendor, runtime.GOOS, runtime.GOARCH)
}

// EnsureGPUBinaryForPlatform ensures the GPU binary exists for a specific platform
// Used when preparing binaries for containers (e.g., Linux containers on macOS)
func EnsureGPUBinaryForPlatform(ctx context.Context, paths *platform.Paths, vendor, osName, arch string) (string, error) {
	info := GetGPUBinaryInfo(vendor, osName, arch)
	if info == nil {
		klog.V(2).Infof("No GPU binary available for vendor=%s os=%s arch=%s", vendor, osName, arch)
		return "", nil
	}

	binDir := filepath.Join(paths.CacheDir(), "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bin directory: %w", err)
	}

	binaryName := info.BinaryName
	if osName == osWindows {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(binDir, binaryName)

	// Check if binary already exists
	if _, err := os.Stat(binaryPath); err == nil {
		klog.V(2).Infof("GPU binary already exists: %s", binaryPath)
		return binaryPath, nil
	}

	// Download and extract
	klog.Infof("Downloading GPU binary: vendor=%s os=%s arch=%s url=%s", vendor, osName, arch, info.URL)

	if err := downloadAndExtractGPUBinary(ctx, info.URL, binDir, binaryName); err != nil {
		return "", fmt.Errorf("failed to download GPU binary: %w", err)
	}

	klog.Infof("GPU binary downloaded successfully: %s", binaryPath)
	return binaryPath, nil
}

// downloadAndExtractGPUBinary downloads a ZIP file and extracts the binary
func downloadAndExtractGPUBinary(ctx context.Context, url, destDir, expectedBinaryName string) error {
	// Create temporary file for download
	tmpFile, err := os.CreateTemp("", "gpubin-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
	}()

	// Download the ZIP file
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	// Write to temp file
	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		return fmt.Errorf("failed to save download: %w", err)
	}
	_ = tmpFile.Close()

	// Extract the ZIP file
	return extractZipBinary(tmpPath, destDir, expectedBinaryName)
}

// extractZipBinary extracts a binary from a ZIP file
// If the ZIP contains a directory, it takes the first file from that directory
func extractZipBinary(zipPath, destDir, expectedBinaryName string) error {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer func() { _ = reader.Close() }()

	if len(reader.File) == 0 {
		return fmt.Errorf("zip file is empty")
	}

	// Find the binary file to extract
	var targetFile *zip.File
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		// Use the first non-directory file
		targetFile = file
		break
	}

	if targetFile == nil {
		return fmt.Errorf("no file found in zip")
	}

	klog.V(2).Infof("Extracting %s from zip", targetFile.Name)

	// Open the file in the ZIP
	rc, err := targetFile.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer func() { _ = rc.Close() }()

	// Create destination file with expected name
	destPath := filepath.Join(destDir, expectedBinaryName)
	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = destFile.Close() }()

	// Copy contents
	if _, err := io.Copy(destFile, rc); err != nil {
		_ = os.Remove(destPath)
		return fmt.Errorf("failed to extract file: %w", err)
	}

	// Set executable permission on Unix
	if runtime.GOOS != osWindows {
		if err := os.Chmod(destPath, 0755); err != nil {
			return fmt.Errorf("failed to set permissions: %w", err)
		}
	}

	return nil
}

// GetGPUBinaryName returns the expected binary name for a vendor (without extension)
func GetGPUBinaryName(vendor string) string {
	vendor = strings.ToLower(vendor)
	switch vendor {
	case "nvidia":
		return "nvidia-smi"
	case "amd", "hygon":
		return "amdsmi"
	default:
		return ""
	}
}
