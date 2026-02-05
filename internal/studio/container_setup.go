package studio

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/NexusGPU/gpu-go/internal/deps"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"k8s.io/klog/v2"
)

// ContainerSetupConfig holds configuration for container GPU environment setup
type ContainerSetupConfig struct {
	// StudioName is the name of the studio (used for config file paths)
	StudioName string
	// GPUWorkerURL is the connection URL to the GPU worker
	GPUWorkerURL string
	// HardwareVendor is the GPU vendor (nvidia, amd, hygon)
	HardwareVendor string
	// MountUserHome indicates whether to mount the user's home directory
	MountUserHome bool
	// UserHomeContainerPath is the path to mount user home in container (default: /home/user/host)
	UserHomeContainerPath string
}

// ContainerSetupResult holds the result of container setup
type ContainerSetupResult struct {
	// EnvVars are environment variables to set in the container
	EnvVars map[string]string
	// VolumeMounts are volumes to mount in the container
	VolumeMounts []VolumeMount
	// LibrariesDownloaded indicates if libraries were downloaded during setup
	LibrariesDownloaded bool
}

// SetupContainerGPUEnv sets up the GPU environment for a container
// This performs the same setup as `ggo use` does for Linux:
// 1. Downloads GPU client libraries for Linux (using host CPU arch)
// 2. Sets up GPU environment (env vars, ld.so.preload, ld.so.conf.d)
// 3. Optionally mounts user home directory
func SetupContainerGPUEnv(ctx context.Context, config *ContainerSetupConfig) (*ContainerSetupResult, error) {
	paths := platform.DefaultPaths()
	result := &ContainerSetupResult{
		EnvVars:      make(map[string]string),
		VolumeMounts: []VolumeMount{},
	}

	normalizedName := platform.NormalizeName(config.StudioName)
	vendor := ParseVendor(config.HardwareVendor)

	// Step 1: Download GPU client libraries for Linux (container target)
	// Libraries are downloaded for Linux with host CPU architecture
	if config.GPUWorkerURL != "" {
		if err := ensureGPUClientLibraries(ctx, vendor); err != nil {
			klog.Warningf("Failed to download GPU client libraries: %v (continuing anyway)", err)
		} else {
			result.LibrariesDownloaded = true
		}
	}

	// Step 2: Setup GPU environment using SetupGPUEnv
	if config.GPUWorkerURL != "" {
		gpuConfig := &GPUEnvConfig{
			Vendor:        vendor,
			ConnectionURL: config.GPUWorkerURL,
			CachePath:     paths.CacheDir(),
			LogPath:       paths.StudioLogsDir(normalizedName),
			StudioName:    normalizedName,
			IsContainer:   true,
		}

		envResult, err := SetupGPUEnv(paths, gpuConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to setup GPU environment: %w", err)
		}

		// Copy env vars from GPU setup
		for k, v := range envResult.EnvVars {
			result.EnvVars[k] = v
		}

		// Copy volume mounts from GPU setup
		result.VolumeMounts = append(result.VolumeMounts, envResult.VolumeMounts...)

		// Add CUDA_VISIBLE_DEVICES for NVIDIA
		if vendor == VendorNvidia {
			result.EnvVars["CUDA_VISIBLE_DEVICES"] = "0"
		}

		klog.Infof("GPU environment setup complete for studio %s: vendor=%s connection_url=%s",
			config.StudioName, vendor, config.GPUWorkerURL)
	}

	// Step 3: Mount user home directory if enabled
	if config.MountUserHome {
		userHomeMount, err := getUserHomeMount(config.UserHomeContainerPath)
		if err != nil {
			klog.Warningf("Failed to get user home directory: %v (skipping mount)", err)
		} else if userHomeMount != nil {
			result.VolumeMounts = append(result.VolumeMounts, *userHomeMount)
			klog.V(2).Infof("User home directory will be mounted: %s -> %s",
				userHomeMount.HostPath, userHomeMount.ContainerPath)
		}
	}

	return result, nil
}

// ensureGPUClientLibraries downloads GPU client libraries for Linux containers
// Libraries are downloaded for Linux platform with the current host CPU architecture
func ensureGPUClientLibraries(ctx context.Context, vendor GPUVendor) error {
	depsMgr := deps.NewManager()

	// Target library types needed for GPU client functionality
	targetTypes := []string{deps.LibraryTypeRemoteGPUClient, deps.LibraryTypeVGPULibrary}

	// Determine vendor slug for filtering
	vendorSlug := ""
	switch vendor {
	case VendorNvidia:
		vendorSlug = "nvidia"
	case VendorAMD:
		vendorSlug = "amd"
	case VendorHygon:
		vendorSlug = "hygon"
	}

	klog.Infof("Downloading GPU client libraries for %s (linux/%s)...", vendorSlug, runtime.GOARCH)

	// Studios run in Linux containers, so we always download Linux libraries
	// CPU architecture matches the host (arm64 on Apple Silicon, amd64 on Intel/AMD)
	_, err := depsMgr.EnsureLibrariesByTypesForPlatform(ctx, targetTypes, vendorSlug, "linux", "", nil)
	if err != nil {
		return fmt.Errorf("failed to ensure GPU client libraries: %w", err)
	}

	klog.V(2).Info("GPU client libraries downloaded successfully")
	return nil
}

// getUserHomeMount returns the volume mount for user's home directory
func getUserHomeMount(containerPath string) (*VolumeMount, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	// Default container path for user home
	if containerPath == "" {
		containerPath = "/home/user/host"
	}

	return &VolumeMount{
		HostPath:      homeDir,
		ContainerPath: containerPath,
		ReadOnly:      false,
	}, nil
}

// BuildContainerArgs builds common Docker/container run arguments from setup result
// This is a helper for backends that use docker-style CLI
func BuildContainerArgs(result *ContainerSetupResult) []string {
	var args []string

	// Add environment variables
	for k, v := range result.EnvVars {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Add volume mounts
	for _, vol := range result.VolumeMounts {
		mountOpt := fmt.Sprintf("%s:%s", vol.HostPath, vol.ContainerPath)
		if vol.ReadOnly {
			mountOpt += MountOptionReadOnly
		}
		args = append(args, "-v", mountOpt)
	}

	return args
}

// MergeEnvVars merges user-provided env vars with setup result env vars
// User-provided vars take precedence
func MergeEnvVars(setupEnvs, userEnvs map[string]string) map[string]string {
	merged := make(map[string]string)

	// Copy setup env vars
	for k, v := range setupEnvs {
		merged[k] = v
	}

	// Override with user env vars
	for k, v := range userEnvs {
		merged[k] = v
	}

	return merged
}

// MergeVolumeMounts merges user-provided volumes with setup result volumes
// If the same container path exists, user-provided volume takes precedence
func MergeVolumeMounts(setupVolumes, userVolumes []VolumeMount) []VolumeMount {
	// Track container paths to avoid duplicates
	containerPaths := make(map[string]bool)
	var merged []VolumeMount

	// Add user volumes first (they take precedence)
	for _, vol := range userVolumes {
		merged = append(merged, vol)
		containerPaths[vol.ContainerPath] = true
	}

	// Add setup volumes that don't conflict
	for _, vol := range setupVolumes {
		if !containerPaths[vol.ContainerPath] {
			merged = append(merged, vol)
		}
	}

	return merged
}
