package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NexusGPU/gpu-go/internal/api"
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

// FindAcceleratorLibrary attempts to find an accelerator library in common locations
func FindAcceleratorLibrary() string {
	suffix := getLibSuffix()

	// Library names in priority order
	libNames := []string{
		"libaccelerator_nvidia" + suffix,
		"libaccelerator_amd" + suffix,
		"libaccelerator_example" + suffix,
	}

	// Build search paths
	searchPaths := buildSearchPaths()

	// Search for libraries
	for _, dir := range searchPaths {
		for _, name := range libNames {
			if path := filepath.Join(dir, name); fileExists(path) {
				return path
			}
		}
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
