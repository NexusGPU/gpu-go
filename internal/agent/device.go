package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/deps"
	"github.com/NexusGPU/gpu-go/internal/platform"
	hvapi "github.com/NexusGPU/tensor-fusion/pkg/hypervisor/api"
	"k8s.io/klog/v2"
)

const (
	vendorNVIDIA = "nvidia"
	vendorAMD    = "amd"
)

// ConvertDevicesToGPUInfo converts hypervisor DeviceInfo to API GPUInfo
func ConvertDevicesToGPUInfo(devices []*hvapi.DeviceInfo) []api.GPUInfo {
	gpus := make([]api.GPUInfo, len(devices))
	for i, dev := range devices {
		driverVersion, cudaVersion := "", ""
		if dev.Properties != nil {
			driverVersion = dev.Properties["driverVersion"]
			cudaVersion = dev.Properties["cudaVersion"]
		}

		gpus[i] = api.GPUInfo{
			GPUID:         strings.ToLower(dev.UUID),
			GPUIndex:      int(dev.Index),
			Vendor:        dev.Vendor,
			Model:         dev.Model,
			VRAMMb:        int64(dev.TotalMemoryBytes / (1024 * 1024)),
			DriverVersion: driverVersion,
			CUDAVersion:   cudaVersion,
		}
	}
	return gpus
}

// ConvertMetricsToGPUMetrics converts hypervisor metrics to API metrics
func ConvertMetricsToGPUMetrics(metrics map[string]*hvapi.GPUUsageMetrics) map[string]*api.GPUMetrics {
	result := make(map[string]*api.GPUMetrics, len(metrics))
	for uuid, m := range metrics {
		result[uuid] = &api.GPUMetrics{
			GPUID:       m.DeviceUUID,
			Utilization: m.ComputePercentage,
			VRAMUsedMb:  int64(m.MemoryBytes / (1024 * 1024)),
			Temperature: m.Temperature,
			PowerUsageW: float64(m.PowerUsage),
		}
	}
	return result
}

// DownloadOrFindAccelerator detects vendor, finds or downloads the accelerator library
// Priority: 1) Config 2) System detection 3) Download from CDN if not found
func DownloadOrFindAccelerator() (string, error) {
	// Step 1: Detect vendor (config has highest priority)
	vendor, version := detectVendor()
	if vendor == "" {
		vendor = "stub" // Fallback to stub if detection fails (use lowercase for slug matching)
	}
	// Normalize vendor to lowercase for slug matching
	vendorSlug := strings.ToLower(vendor)

	// Step 2: Initialize deps manager and fetch manifest
	// This will auto-sync on first use if manifest doesn't exist
	paths := platform.DefaultPaths()
	depsMgr := deps.NewManager(deps.WithPaths(paths))
	ctx := context.Background()

	// Fetch manifest (auto-syncs if not cached)
	manifest, _, err := depsMgr.FetchManifest(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}

	// Step 3: Find library in manifest matching platform, type, and vendor slug
	var targetLib *deps.Library
	var candidateLibs []deps.Library
	platformLibs := depsMgr.GetLibrariesForPlatform(manifest, "", "", deps.LibraryTypeVGPULibrary)

	// Match by vendor slug from manifest
	for i := range platformLibs {
		lib := platformLibs[i]
		libNameLower := strings.ToLower(lib.Name)
		if strings.Contains(libNameLower, "accelerator") && lib.VendorSlug == vendorSlug {
			candidateLibs = append(candidateLibs, lib)
		}
	}

	// If version is specified, find exact match
	if version != "" {
		for i := range candidateLibs {
			if candidateLibs[i].Version == version {
				targetLib = &candidateLibs[i]
				break
			}
		}
	} else {
		// Use first candidate (manifest should be sorted, latest first)
		if len(candidateLibs) > 0 {
			targetLib = &candidateLibs[0]
		}
	}

	if targetLib == nil {
		// Library not in manifest, return error with available libraries
		availableNames := getLibraryNames(platformLibs)
		return "", fmt.Errorf("accelerator library for vendor %s (slug: %s) not found in CDN manifest. Available libraries: %v",
			vendor, vendorSlug, availableNames)
	}

	// Step 4: Check if library is already downloaded in cache directory
	// Search cache directory based on manifest entries
	cacheDir := paths.CacheDir()
	cachedPath := filepath.Join(cacheDir, targetLib.Name)
	if fileExists(cachedPath) {
		// Verify the cached file matches the expected hash
		if targetLib.SHA256 != "" {
			// Use deps manager's VerifyLibrary method
			if depsMgr.VerifyLibrary(cachedPath, targetLib.SHA256) {
				// Library found in cache, check if installed
				installedPath := depsMgr.GetLibraryPath(targetLib.Name)
				if fileExists(installedPath) {
					return installedPath, nil
				}
				// Install from cache
				if err := depsMgr.InstallLibrary(*targetLib); err != nil {
					return "", fmt.Errorf("failed to install library from cache: %w", err)
				}
				return installedPath, nil
			}
		} else {
			// No hash to verify, assume cached file is valid
			installedPath := depsMgr.GetLibraryPath(targetLib.Name)
			if fileExists(installedPath) {
				return installedPath, nil
			}
			// Install from cache
			if err := depsMgr.InstallLibrary(*targetLib); err != nil {
				return "", fmt.Errorf("failed to install library from cache: %w", err)
			}
			return installedPath, nil
		}
	}

	// Step 5: Library not found locally, download from CDN
	progressFn := func(downloaded, total int64) {
		// Silent progress for library download
	}
	if err := depsMgr.DownloadLibrary(ctx, *targetLib, progressFn); err != nil {
		return "", fmt.Errorf("failed to download library: %w", err)
	}

	// Install to lib directory
	if err := depsMgr.InstallLibrary(*targetLib); err != nil {
		return "", fmt.Errorf("failed to install library: %w", err)
	}

	// Return installed path
	installedPath := depsMgr.GetLibraryPath(targetLib.Name)
	if fileExists(installedPath) {
		return installedPath, nil
	}

	return "", fmt.Errorf("library downloaded but not found at expected path: %s", installedPath)
}

// detectVendor detects GPU vendor with priority: config > system detection
// Returns (vendor, version) where version may be empty
func detectVendor() (string, string) {
	// Priority 1: Check environment variable
	if vendor := os.Getenv("ACCELERATOR_VENDOR"); vendor != "" {
		version := os.Getenv("ACCELERATOR_VERSION")
		return strings.ToLower(vendor), version
	}

	// Priority 2: System detection
	vendor := detectVendorFromSystem()
	if vendor != "" {
		return vendor, ""
	}

	// Fallback: return empty to use stub
	return "", ""
}

// detectVendorFromSystem detects GPU vendor from system information
func detectVendorFromSystem() string {
	if runtime.GOOS == "windows" {
		vendor, driverVersion, verified := detectWindowsGPUVendor()
		if vendor != "" {
			if vendor == vendorNVIDIA && driverVersion != "" {
				warnIfNvidiaDriverOutdated(driverVersion)
			}
			switch {
			case driverVersion != "" && verified:
				klog.Infof("Detected Windows GPU vendor: %s (driver_version=%s verified)", vendor, driverVersion)
			case driverVersion != "":
				klog.Infof("Detected Windows GPU vendor: %s (driver_version=%s)", vendor, driverVersion)
			default:
				klog.Infof("Detected Windows GPU vendor: %s", vendor)
			}
			return vendor
		}
	}

	// Check for NVIDIA
	if smiPath, err := exec.LookPath("nvidia-smi"); err == nil {
		if driverVersion := queryNvidiaSMIDriverVersion(smiPath); driverVersion != "" {
			warnIfNvidiaDriverOutdated(driverVersion)
		}
		// nvidia-smi exists, try to run it
		cmd := exec.Command(smiPath, "--query-gpu=vendor", "--format=csv,noheader")
		if output, err := cmd.Output(); err == nil {
			vendor := strings.TrimSpace(strings.ToLower(string(output)))
			if strings.Contains(vendor, vendorNVIDIA) {
				return vendorNVIDIA
			}
		}
		// If nvidia-smi exists, assume NVIDIA even if query fails
		return vendorNVIDIA
	}

	// Check for AMD/ROCm
	if _, err := exec.LookPath("rocm-smi"); err == nil {
		return vendorAMD
	}
	if _, err := exec.LookPath("rocm-info"); err == nil {
		return vendorAMD
	}

	// Check PCI devices (Linux)
	if runtime.GOOS == "linux" {
		if vendor := detectVendorFromPCI(); vendor != "" {
			return vendor
		}
	}

	// Check for vendor-specific driver files
	if fileExists("/sys/class/drm") {
		// Check DRM devices
		entries, err := os.ReadDir("/sys/class/drm")
		if err == nil {
			for _, entry := range entries {
				name := entry.Name()
				if strings.HasPrefix(name, "card") {
					// Check vendor
					vendorPath := filepath.Join("/sys/class/drm", name, "device", "vendor")
					if data, err := os.ReadFile(vendorPath); err == nil {
						vendorID := strings.TrimSpace(strings.ToLower(string(data)))
						// NVIDIA: 0x10de (hex: 10de), AMD: 0x1002 (hex: 1002)
						// Vendor ID format: 0x10de or 10de
						if strings.Contains(vendorID, "10de") {
							return vendorNVIDIA
						}
						if strings.Contains(vendorID, "1002") {
							return vendorAMD
						}
					}
				}
			}
		}
	}

	return ""
}

// detectVendorFromPCI detects vendor from lspci output (Linux)
func detectVendorFromPCI() string {
	cmd := exec.Command("lspci")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	outputStr := strings.ToLower(string(output))
	if strings.Contains(outputStr, vendorNVIDIA) {
		return vendorNVIDIA
	}
	if strings.Contains(outputStr, vendorAMD) || strings.Contains(outputStr, "radeon") {
		return vendorAMD
	}

	return ""
}

// DetectVendorFromLibPath extracts vendor name from library path
func DetectVendorFromLibPath(libPath string) string {
	lower := strings.ToLower(filepath.Base(libPath))
	switch {
	case strings.Contains(lower, vendorNVIDIA):
		return vendorNVIDIA
	case strings.Contains(lower, vendorAMD):
		return vendorAMD
	case strings.Contains(lower, "example"), strings.Contains(lower, "stub"):
		return "stub"
	default:
		return "unknown"
	}
}

// CreateMockGPUs creates mock GPU info for testing without real hardware
func CreateMockGPUs(count int) []api.GPUInfo {
	gpus := make([]api.GPUInfo, count)
	for i := range count {
		gpus[i] = api.GPUInfo{
			GPUID:         fmt.Sprintf("GPU-%d", i),
			GPUIndex:      i,
			Vendor:        vendorNVIDIA,
			Model:         "RTX 4090",
			VRAMMb:        24576,
			DriverVersion: "535.104.05",
			CUDAVersion:   "12.2",
		}
	}
	return gpus
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// getLibraryNames returns a list of library names from the platform libraries
func getLibraryNames(libs []deps.Library) []string {
	names := make([]string, len(libs))
	for i, lib := range libs {
		names[i] = lib.Name
	}
	return names
}
