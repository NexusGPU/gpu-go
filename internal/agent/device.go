package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/log"
)

// DeviceDiscovery handles GPU device discovery using the AcceleratorInterface
type DeviceDiscovery struct {
	accelerator *AcceleratorInterface
	libPath     string
	log         *log.Logger
}

// NewDeviceDiscovery creates a new device discovery instance
func NewDeviceDiscovery(libPath string) (*DeviceDiscovery, error) {
	logger := log.Default.WithComponent("device-discovery")

	// If no library path specified, try to find one automatically
	if libPath == "" {
		libPath = findAcceleratorLibrary()
		if libPath == "" {
			return nil, fmt.Errorf("accelerator library not found, please specify --accelerator-lib path")
		}
		logger.Info().Str("lib_path", libPath).Msg("auto-detected accelerator library")
	}

	// Use local AcceleratorInterface (tensor-fusion's internal package cannot be imported)
	accel, err := NewAcceleratorInterface(libPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load accelerator library from %s: %w", libPath, err)
	}

	return &DeviceDiscovery{
		accelerator: accel,
		libPath:     libPath,
		log:         logger,
	}, nil
}

// Close releases the accelerator resources
func (d *DeviceDiscovery) Close() error {
	if d.accelerator != nil {
		return d.accelerator.Close()
	}
	return nil
}

// DiscoverGPUs discovers all available GPUs using the accelerator library
func (d *DeviceDiscovery) DiscoverGPUs() ([]api.GPUInfo, error) {
	devices, err := d.accelerator.GetAllDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to discover devices: %w", err)
	}

	if len(devices) == 0 {
		d.log.Warn().Msg("no GPU devices discovered")
		return nil, nil
	}

	gpuInfos := make([]api.GPUInfo, len(devices))
	for i, dev := range devices {
		// Convert UUID to lowercase for consistency
		uuid := strings.ToLower(dev.UUID)

		// Extract driver/CUDA version from properties if available
		driverVersion := ""
		cudaVersion := ""
		if dev.Properties != nil {
			if v, ok := dev.Properties["driverVersion"]; ok {
				driverVersion = v
			}
			if v, ok := dev.Properties["cudaVersion"]; ok {
				cudaVersion = v
			}
		}

		gpuInfos[i] = api.GPUInfo{
			GPUID:         uuid,
			Vendor:        dev.Vendor,
			Model:         dev.Model,
			VRAMMb:        int64(dev.TotalMemoryBytes / (1024 * 1024)),
			DriverVersion: driverVersion,
			CUDAVersion:   cudaVersion,
		}

		d.log.Debug().
			Str("uuid", uuid).
			Str("vendor", dev.Vendor).
			Str("model", dev.Model).
			Int64("vram_mb", gpuInfos[i].VRAMMb).
			Msg("discovered GPU")
	}

	d.log.Info().Int("count", len(gpuInfos)).Msg("GPU discovery complete")
	return gpuInfos, nil
}

// GetDeviceMetrics retrieves current metrics for the specified device UUIDs
func (d *DeviceDiscovery) GetDeviceMetrics(deviceUUIDs []string) (map[string]*api.GPUMetrics, error) {
	if len(deviceUUIDs) == 0 {
		return nil, nil
	}

	metrics, err := d.accelerator.GetDeviceMetrics(deviceUUIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get device metrics: %w", err)
	}

	result := make(map[string]*api.GPUMetrics, len(metrics))
	for _, m := range metrics {
		result[m.DeviceUUID] = &api.GPUMetrics{
			GPUID:       m.DeviceUUID,
			Utilization: m.ComputePercentage,
			VRAMUsedMb:  int64(m.MemoryBytes / (1024 * 1024)),
			VRAMTotalMb: 0, // Total would need to be fetched from device info
			Temperature: m.Temperature,
			PowerUsageW: float64(m.PowerUsage),
		}
	}

	return result, nil
}

// GetLibraryPath returns the path to the loaded accelerator library
func (d *DeviceDiscovery) GetLibraryPath() string {
	return d.libPath
}

// findAcceleratorLibrary attempts to find an accelerator library in common locations
func findAcceleratorLibrary() string {
	// Determine library suffix based on OS
	suffix := ".so"
	if runtime.GOOS == "darwin" {
		suffix = ".dylib"
	} else if runtime.GOOS == "windows" {
		suffix = ".dll"
	}

	// Common library names in priority order
	libNames := []string{
		"libaccelerator_nvidia" + suffix,
		"libaccelerator_amd" + suffix,
		"libaccelerator_example" + suffix,
		"libaccelerator_stub" + suffix,
	}

	// Common search paths
	searchPaths := []string{
		"/usr/lib/tensor-fusion",
		"/usr/local/lib/tensor-fusion",
		"/opt/tensor-fusion/lib",
	}

	// Add home directory paths
	if home, err := os.UserHomeDir(); err == nil {
		searchPaths = append(searchPaths,
			filepath.Join(home, ".tensor-fusion", "libs"),
			filepath.Join(home, ".local", "lib", "tensor-fusion"),
		)
	}

	// Add current working directory
	if cwd, err := os.Getwd(); err == nil {
		searchPaths = append(searchPaths, cwd)
	}

	// Add paths from environment
	if tfLibPath := os.Getenv("TENSOR_FUSION_LIB_PATH"); tfLibPath != "" {
		searchPaths = append([]string{tfLibPath}, searchPaths...)
	}

	// Search for libraries
	for _, dir := range searchPaths {
		for _, name := range libNames {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}

	return ""
}

// GetExampleLibraryPath returns the path to the example/stub accelerator library
// This is useful for testing without real GPU hardware
func GetExampleLibraryPath() string {
	suffix := ".so"
	if runtime.GOOS == "darwin" {
		suffix = ".dylib"
	}

	name := "libaccelerator_example" + suffix

	// Search in common locations
	searchPaths := []string{}

	// Check TENSOR_FUSION_OPERATOR_PATH for development
	if opPath := os.Getenv("TENSOR_FUSION_OPERATOR_PATH"); opPath != "" {
		searchPaths = append(searchPaths, filepath.Join(opPath, "provider", "build"))
	}

	// Add home directory
	if home, err := os.UserHomeDir(); err == nil {
		searchPaths = append(searchPaths,
			filepath.Join(home, ".tensor-fusion", "libs"),
		)
	}

	// Standard locations
	searchPaths = append(searchPaths,
		"/usr/lib/tensor-fusion",
		"/usr/local/lib/tensor-fusion",
	)

	for _, dir := range searchPaths {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}
