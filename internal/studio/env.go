package studio

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/NexusGPU/gpu-go/internal/platform"
)

// GPUVendor represents supported GPU vendors
type GPUVendor string

const (
	VendorNvidia  GPUVendor = "nvidia"
	VendorAMD     GPUVendor = "amd"
	VendorHygon   GPUVendor = "hygon"
	VendorUnknown GPUVendor = "unknown"
)

// ParseVendor parses a vendor string to GPUVendor
func ParseVendor(vendor string) GPUVendor {
	switch strings.ToLower(vendor) {
	case "nvidia":
		return VendorNvidia
	case "amd":
		return VendorAMD
	case "hygon":
		return VendorHygon
	default:
		return VendorUnknown
	}
}

// GPUEnvConfig holds configuration for GPU environment setup
type GPUEnvConfig struct {
	Vendor        GPUVendor
	ConnectionURL string // TENSOR_FUSION_OPERATOR_CONNECTION_INFO value
	CachePath     string // Path to gpugo cache directory
	LogPath       string // Path to logs directory
	StudioName    string // Name of the studio (for creating config files)
	IsContainer   bool   // Whether this is for a container (affects paths)
}

// GPUEnvResult holds the result of GPU environment setup
type GPUEnvResult struct {
	EnvVars         map[string]string
	LDSoConfPath    string        // Host path to ld.so.conf file
	LDSoPreloadPath string        // Host path to ld.so.preload file
	VolumeMounts    []VolumeMount // Additional volume mounts needed
}

// GetLibraryNames returns the library names to preload for a vendor (Linux/macOS)
func GetLibraryNames(vendor GPUVendor) []string {
	switch vendor {
	case VendorNvidia:
		return []string{"libcuda.so", "libnvidia-ml.so"}
	case VendorAMD, VendorHygon:
		return []string{"libamdhip64.so"}
	default:
		return []string{}
	}
}

// GetWindowsLibraryNames returns the DLL names for a vendor (Windows)
func GetWindowsLibraryNames(vendor GPUVendor) []string {
	switch vendor {
	case VendorNvidia:
		// Windows DLL names are different from Linux .so names
		// nvcuda.dll - CUDA driver stub
		// nvml.dll - NVIDIA ML stub
		// teleport.dll - TensorFusion remote transport layer
		return []string{"nvcuda.dll", "nvml.dll", "teleport.dll"}
	case VendorAMD, VendorHygon:
		return []string{"amdhip64.dll"}
	default:
		return []string{}
	}
}

// SetupGPUEnv creates and configures GPU environment files
func SetupGPUEnv(paths *platform.Paths, config *GPUEnvConfig) (*GPUEnvResult, error) {
	result := &GPUEnvResult{
		EnvVars:      make(map[string]string),
		VolumeMounts: []VolumeMount{},
	}

	// Ensure studio directories exist
	if err := paths.EnsureStudioDirs(config.StudioName); err != nil {
		return nil, fmt.Errorf("failed to create studio directories: %w", err)
	}

	cachePath := config.CachePath
	if cachePath == "" {
		cachePath = paths.CacheDir()
	}

	logPath := config.LogPath
	if logPath == "" {
		logPath = paths.StudioLogsDir(config.StudioName)
	}

	// Ensure log directory exists
	if err := os.MkdirAll(logPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}

	// Set up environment variables
	result.EnvVars["TENSOR_FUSION_OPERATOR_CONNECTION_INFO"] = config.ConnectionURL
	result.EnvVars["TF_LOG_PATH"] = logPath
	result.EnvVars["TF_LOG_LEVEL"] = getEnvDefault("TF_LOG_LEVEL", "info")
	result.EnvVars["TF_ENABLE_LOG"] = getEnvDefault("TF_ENABLE_LOG", "0")

	// Add cache path to PATH (for tensor-fusion-worker binary)
	if config.IsContainer {
		// In container, cache will be mounted to /opt/gpugo/cache
		result.EnvVars["PATH"] = "/opt/gpugo/cache:" + os.Getenv("PATH")
	} else {
		result.EnvVars["PATH"] = cachePath + ":" + os.Getenv("PATH")
	}

	// Create ld.so.conf file (contains cache directory for LD_LIBRARY_PATH effect)
	ldConfPath := paths.LDSoConfPath(config.StudioName)
	if err := os.MkdirAll(filepath.Dir(ldConfPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create ld.so.conf.d directory: %w", err)
	}

	// Content for ld.so.conf.d file
	ldConfContent := fmt.Sprintf("# TensorFusion GPU libraries\n%s\n", cachePath)
	if config.IsContainer {
		// In container, use the mounted path
		ldConfContent = "# TensorFusion GPU libraries\n/opt/gpugo/cache\n"
	}
	if err := os.WriteFile(ldConfPath, []byte(ldConfContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write ld.so.conf: %w", err)
	}
	result.LDSoConfPath = ldConfPath

	// Create ld.so.preload file based on vendor
	ldPreloadPath := paths.LDSoPreloadPath(config.StudioName)
	ldPreloadContent := generateLDPreloadContent(config.Vendor, cachePath, config.IsContainer)
	if err := os.WriteFile(ldPreloadPath, []byte(ldPreloadContent), 0644); err != nil {
		return nil, fmt.Errorf("failed to write ld.so.preload: %w", err)
	}
	result.LDSoPreloadPath = ldPreloadPath

	// Set up volume mounts for container mode
	if config.IsContainer {
		// Mount cache directory
		result.VolumeMounts = append(result.VolumeMounts, VolumeMount{
			HostPath:      cachePath,
			ContainerPath: "/opt/gpugo/cache",
			ReadOnly:      true,
		})

		// Mount logs directory
		result.VolumeMounts = append(result.VolumeMounts, VolumeMount{
			HostPath:      logPath,
			ContainerPath: "/var/log/tensor-fusion",
			ReadOnly:      false,
		})

		// Mount ld.so.conf.d file
		result.VolumeMounts = append(result.VolumeMounts, VolumeMount{
			HostPath:      ldConfPath,
			ContainerPath: "/etc/ld.so.conf.d/zz_tensor-fusion.conf",
			ReadOnly:      true,
		})

		// Mount ld.so.preload file
		result.VolumeMounts = append(result.VolumeMounts, VolumeMount{
			HostPath:      ldPreloadPath,
			ContainerPath: "/etc/ld.so.preload",
			ReadOnly:      true,
		})

		// Update env vars for container paths
		result.EnvVars["TF_LOG_PATH"] = "/var/log/tensor-fusion"
	}

	return result, nil
}

// generateLDPreloadContent generates the content for ld.so.preload based on vendor
func generateLDPreloadContent(vendor GPUVendor, cachePath string, isContainer bool) string {
	libNames := GetLibraryNames(vendor)
	if len(libNames) == 0 {
		return "# No GPU libraries to preload\n"
	}

	basePath := cachePath
	if isContainer {
		basePath = "/opt/gpugo/cache"
	}

	var lines []string
	lines = append(lines, "# TensorFusion GPU library preload")
	for _, lib := range libNames {
		lines = append(lines, filepath.Join(basePath, lib))
	}
	return strings.Join(lines, "\n") + "\n"
}

// GenerateEnvScript generates a shell script to set up the GPU environment
func GenerateEnvScript(config *GPUEnvConfig, paths *platform.Paths) (string, error) {
	result, err := SetupGPUEnv(paths, config)
	if err != nil {
		return "", err
	}

	cachePath := config.CachePath
	if cachePath == "" {
		cachePath = paths.CacheDir()
	}

	var script strings.Builder
	script.WriteString("#!/bin/bash\n")
	script.WriteString("# GPU Go environment setup script\n")
	script.WriteString("# Generated by ggo use\n\n")

	// Export environment variables
	for k, v := range result.EnvVars {
		script.WriteString(fmt.Sprintf("export %s=\"%s\"\n", k, v))
	}

	// Add LD_LIBRARY_PATH
	script.WriteString("\n# Add GPU libraries to library path\n")
	script.WriteString(fmt.Sprintf("export LD_LIBRARY_PATH=\"%s:$LD_LIBRARY_PATH\"\n", cachePath))

	// Add LD_PRELOAD based on vendor
	libNames := GetLibraryNames(config.Vendor)
	if len(libNames) > 0 {
		var preloadPaths []string
		for _, lib := range libNames {
			preloadPaths = append(preloadPaths, filepath.Join(cachePath, lib))
		}
		script.WriteString("\n# Preload GPU libraries\n")
		script.WriteString(fmt.Sprintf("export LD_PRELOAD=\"%s${LD_PRELOAD:+:$LD_PRELOAD}\"\n", strings.Join(preloadPaths, ":")))
	}

	script.WriteString("\n# GPU Go environment activated\n")
	script.WriteString(fmt.Sprintf("echo \"GPU Go environment activated for vendor: %s\"\n", config.Vendor))
	script.WriteString(fmt.Sprintf("echo \"Connection URL: %s\"\n", config.ConnectionURL))

	return script.String(), nil
}

// GeneratePowerShellScript generates a PowerShell script to set up the GPU environment
func GeneratePowerShellScript(config *GPUEnvConfig, paths *platform.Paths) (string, error) {
	result, err := SetupGPUEnv(paths, config)
	if err != nil {
		return "", err
	}

	cachePath := config.CachePath
	if cachePath == "" {
		cachePath = paths.CacheDir()
	}

	var script strings.Builder
	script.WriteString("# GPU Go environment setup script (PowerShell)\n")
	script.WriteString("# Generated by ggo use\n")
	script.WriteString("#\n")
	script.WriteString("# IMPORTANT: Windows DLL loading note\n")
	script.WriteString("# Setting PATH helps but System32 DLLs still take priority.\n")
	script.WriteString("# For reliable GPU library loading, use: ggo launch <program>\n")
	script.WriteString("# Example: ggo launch python train.py\n")
	script.WriteString("#\n\n")

	// Export environment variables
	for k, v := range result.EnvVars {
		script.WriteString(fmt.Sprintf("$env:%s = \"%s\"\n", k, v))
	}

	// Set GPU vendor for ggo launch to detect correct DLLs
	script.WriteString(fmt.Sprintf("$env:TF_GPU_VENDOR = \"%s\"\n", config.Vendor))

	// Add cache path to PATH at the FRONT (best effort for DLL loading)
	script.WriteString("\n# Add GPU libraries to PATH (prepend for priority)\n")
	script.WriteString(fmt.Sprintf("$env:PATH = \"%s;$env:PATH\"\n", cachePath))

	// Set CUDA_PATH for applications that check it
	script.WriteString("\n# Set CUDA_PATH for CUDA-aware applications\n")
	script.WriteString(fmt.Sprintf("$env:CUDA_PATH = \"%s\"\n", cachePath))
	script.WriteString(fmt.Sprintf("$env:CUDA_HOME = \"%s\"\n", cachePath))

	// List required DLLs for this vendor
	windowsDLLs := GetWindowsLibraryNames(config.Vendor)
	if len(windowsDLLs) > 0 {
		script.WriteString("\n# Required DLLs for this vendor (should be in cache directory)\n")
		for _, dll := range windowsDLLs {
			script.WriteString(fmt.Sprintf("# - %s\n", dll))
		}
	}

	script.WriteString("\n# GPU Go environment activated\n")
	script.WriteString(fmt.Sprintf("Write-Host \"GPU Go environment activated for vendor: %s\" -ForegroundColor Green\n", config.Vendor))
	script.WriteString(fmt.Sprintf("Write-Host \"Connection URL: %s\"\n", config.ConnectionURL))
	script.WriteString("Write-Host \"\"\n")
	script.WriteString("Write-Host \"TIP: For reliable DLL loading, use 'ggo launch <program>'\" -ForegroundColor Yellow\n")
	script.WriteString("Write-Host \"Example: ggo launch python train.py\"\n")

	return script.String(), nil
}

// GenerateBatchScript generates a CMD batch script to set up the GPU environment
func GenerateBatchScript(config *GPUEnvConfig, paths *platform.Paths) (string, error) {
	result, err := SetupGPUEnv(paths, config)
	if err != nil {
		return "", err
	}

	cachePath := config.CachePath
	if cachePath == "" {
		cachePath = paths.CacheDir()
	}

	var script strings.Builder
	script.WriteString("@echo off\n")
	script.WriteString("REM GPU Go environment setup script (CMD)\n")
	script.WriteString("REM Generated by ggo use\n")
	script.WriteString("REM\n")
	script.WriteString("REM IMPORTANT: Windows DLL loading note\n")
	script.WriteString("REM Setting PATH helps but System32 DLLs still take priority.\n")
	script.WriteString("REM For reliable GPU library loading, use: ggo launch <program>\n")
	script.WriteString("REM Example: ggo launch python train.py\n")
	script.WriteString("REM\n\n")

	// Export environment variables
	for k, v := range result.EnvVars {
		script.WriteString(fmt.Sprintf("set %s=%s\n", k, v))
	}

	// Set GPU vendor for ggo launch to detect correct DLLs
	script.WriteString(fmt.Sprintf("set TF_GPU_VENDOR=%s\n", config.Vendor))

	// Add cache path to PATH at the FRONT
	script.WriteString("\nREM Add GPU libraries to PATH (prepend for priority)\n")
	script.WriteString(fmt.Sprintf("set PATH=%s;%%PATH%%\n", cachePath))

	// Set CUDA_PATH
	script.WriteString("\nREM Set CUDA_PATH for CUDA-aware applications\n")
	script.WriteString(fmt.Sprintf("set CUDA_PATH=%s\n", cachePath))
	script.WriteString(fmt.Sprintf("set CUDA_HOME=%s\n", cachePath))

	// List required DLLs for this vendor
	windowsDLLs := GetWindowsLibraryNames(config.Vendor)
	if len(windowsDLLs) > 0 {
		script.WriteString("\nREM Required DLLs for this vendor (should be in cache directory)\n")
		for _, dll := range windowsDLLs {
			script.WriteString(fmt.Sprintf("REM - %s\n", dll))
		}
	}

	script.WriteString("\necho GPU Go environment activated!\n")
	script.WriteString(fmt.Sprintf("echo Connection URL: %s\n", config.ConnectionURL))
	script.WriteString("echo.\n")
	script.WriteString("echo TIP: For reliable DLL loading, use 'ggo launch ^<program^>'\n")
	script.WriteString("echo Example: ggo launch python train.py\n")

	return script.String(), nil
}

// getEnvDefault gets an environment variable or returns a default value
func getEnvDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// IsLinux returns true if running on Linux
func IsLinux() bool {
	return runtime.GOOS == OSLinux
}

// IsDarwin returns true if running on macOS
func IsDarwin() bool {
	return runtime.GOOS == OSDarwin
}
