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
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/NexusGPU/gpu-go/internal/deps"
	"github.com/NexusGPU/gpu-go/internal/platform"
	hvapi "github.com/NexusGPU/tensor-fusion/pkg/hypervisor/api"
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
		vendor = "stub" // Fallback to stub if detection fails
	}

	// Step 2: Try to find library locally
	suffix := getLibSuffix()
	libName := fmt.Sprintf("libaccelerator_%s%s", vendor, suffix)

	searchPaths := buildSearchPaths()
	for _, dir := range searchPaths {
		if path := filepath.Join(dir, libName); fileExists(path) {
			return path, nil
		}
	}

	// Step 3: Library not found locally, try to download from CDN
	paths := platform.DefaultPaths()

	// Check config for accelerator library version (version already detected from env/config in detectVendor)
	// TODO: Add AcceleratorVersion field to config.Config struct for persistent config

	// Use deps manager to download
	depsMgr := deps.NewManager(deps.WithPaths(paths))
	ctx := context.Background()

	// Fetch manifest to get latest version or use configured version
	manifest, err := depsMgr.FetchManifest(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest from CDN: %w", err)
	}

	// Find library in manifest matching platform and vendor
	var targetLib *deps.Library
	var candidateLibs []deps.Library
	platformLibs := depsMgr.GetLibrariesForPlatform(manifest)

	// Match library name pattern: contains "accelerator_{vendor}"
	vendorPattern := "accelerator_" + vendor
	for i := range platformLibs {
		lib := platformLibs[i]
		libNameLower := strings.ToLower(lib.Name)
		// Match if library name contains the vendor pattern
		if strings.Contains(libNameLower, vendorPattern) {
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
		// Library not in manifest, return error (could fallback to stub)
		availableNames := getLibraryNames(platformLibs)
		return "", fmt.Errorf("accelerator library for vendor %s (name: %s) not found in CDN manifest. Available libraries: %v",
			vendor, libName, availableNames)
	}

	// Download the library
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

	// Priority 2: Check config file
	// Note: Config struct doesn't currently have accelerator_vendor/version fields
	// Environment variables ACCELERATOR_VENDOR and ACCELERATOR_VERSION are used for now
	// TODO: Add AcceleratorVendor and AcceleratorVersion fields to config.Config struct
	paths := platform.DefaultPaths()
	cfgMgr := config.NewManagerWithPaths(paths)
	_, err := cfgMgr.LoadConfig()
	if err == nil {
		// Config loaded successfully, but vendor/version fields not yet in Config struct
		// For now, rely on environment variables set above
	}

	// Priority 3: System detection
	vendor := detectVendorFromSystem()
	if vendor != "" {
		return vendor, ""
	}

	// Fallback: return empty to use stub
	return "", ""
}

// detectVendorFromSystem detects GPU vendor from system information
func detectVendorFromSystem() string {
	// Check for NVIDIA
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		// nvidia-smi exists, try to run it
		cmd := exec.Command("nvidia-smi", "--query-gpu=vendor", "--format=csv,noheader")
		if output, err := cmd.Output(); err == nil {
			vendor := strings.TrimSpace(strings.ToLower(string(output)))
			if strings.Contains(vendor, "nvidia") {
				return "nvidia"
			}
		}
		// If nvidia-smi exists, assume NVIDIA even if query fails
		return "nvidia"
	}

	// Check for AMD/ROCm
	if _, err := exec.LookPath("rocm-smi"); err == nil {
		return "amd"
	}
	if _, err := exec.LookPath("rocm-info"); err == nil {
		return "amd"
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
							return "nvidia"
						}
						if strings.Contains(vendorID, "1002") {
							return "amd"
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
	if strings.Contains(outputStr, "nvidia") {
		return "nvidia"
	}
	if strings.Contains(outputStr, "amd") || strings.Contains(outputStr, "radeon") {
		return "amd"
	}

	return ""
}

// DetectVendorFromLibPath extracts vendor name from library path
func DetectVendorFromLibPath(libPath string) string {
	lower := strings.ToLower(filepath.Base(libPath))
	switch {
	case strings.Contains(lower, "nvidia"):
		return "nvidia"
	case strings.Contains(lower, "amd"):
		return "amd"
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
			Vendor:        "nvidia",
			Model:         "RTX 4090",
			VRAMMb:        24576,
			DriverVersion: "535.104.05",
			CUDAVersion:   "12.2",
		}
	}
	return gpus
}

func getLibSuffix() string {
	switch runtime.GOOS {
	case "darwin":
		return ".dylib"
	case "windows":
		return ".dll"
	default:
		return ".so"
	}
}

func buildSearchPaths() []string {
	paths := []string{
		"/usr/lib/tensor-fusion",
		"/usr/local/lib/tensor-fusion",
		"/opt/tensor-fusion/lib",
	}

	// Add platform lib directory (where deps manager installs libraries)
	platformPaths := platform.DefaultPaths()
	paths = append(paths, platformPaths.LibDir())

	// Environment variable takes priority
	if tfLibPath := os.Getenv("TENSOR_FUSION_LIB_PATH"); tfLibPath != "" {
		paths = append([]string{tfLibPath}, paths...)
	}

	// Add home directory paths
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths,
			filepath.Join(home, ".tensor-fusion", "libs"),
			filepath.Join(home, ".local", "lib", "tensor-fusion"),
		)
	}

	// Add cwd for development
	if cwd, err := os.Getwd(); err == nil {
		paths = append(paths, cwd)
	}

	return paths
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
