package studio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	// Platform is the container platform (e.g., "linux/amd64", "linux/arm64")
	// Used to determine which arch-specific libs to download and mount.
	// If empty, defaults to linux/amd64.
	Platform string
	// MountUserHome indicates whether to mount the user's home directory
	MountUserHome bool
	// SkipSSHMounts disables mounting SSH key files into the container
	SkipSSHMounts bool
	// SkipFileMounts disables mounting files into the container (directories only)
	SkipFileMounts bool
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
// 1. Downloads GPU client libraries for Linux (using target platform arch)
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

	// Parse target arch from platform string (e.g., "linux/amd64" → "amd64")
	targetArch := "amd64" // default
	if config.Platform != "" {
		if parts := strings.SplitN(config.Platform, "/", 2); len(parts) == 2 {
			targetArch = parts[1]
		}
	}
	// Arch-specific libs directory (e.g., ~/.gpugo/cache/libs/linux-amd64/)
	libsDir := paths.LibsDirForPlatform("linux", targetArch)

	// Step 1: Download GPU client libraries for Linux (container target)
	// Libraries are downloaded for Linux with the target CPU architecture
	if config.GPUWorkerURL != "" {
		if err := ensureGPUClientLibraries(ctx, vendor, targetArch); err != nil {
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
			LibsPath:      libsDir,
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

		if config.SkipFileMounts {
			result.EnvVars["LD_LIBRARY_PATH"] = "/opt/gpugo/libs"
			if preload := buildContainerLDPreload(libsDir, vendor); preload != "" {
				result.EnvVars["LD_PRELOAD"] = preload
			}
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

	// Step 4: Setup SSH volume mounts for container SSH access
	if !config.SkipSSHMounts {
		var sshMounts []VolumeMount
		if config.SkipFileMounts {
			sshMounts = getSSHDirectoryMounts(paths, config.StudioName)
		} else {
			sshMounts = getSSHVolumeMounts(paths, config.StudioName)
		}
		result.VolumeMounts = append(result.VolumeMounts, sshMounts...)
		if len(sshMounts) > 0 {
			klog.V(2).Infof("SSH mounts configured for studio %s: %d mounts", config.StudioName, len(sshMounts))
		}
	}

	// Step 5: Download and mount GPU binary (like nvidia-smi) to /usr/local/bin/
	if !config.SkipFileMounts && config.GPUWorkerURL != "" && config.HardwareVendor != "" {
		gpuBinMount, err := ensureAndMountGPUBinary(ctx, paths, config.HardwareVendor, targetArch)
		if err != nil {
			klog.Warningf("Failed to setup GPU binary mount: %v (continuing without it)", err)
		} else if gpuBinMount != nil {
			result.VolumeMounts = append(result.VolumeMounts, *gpuBinMount)
			klog.Infof("GPU binary mount configured: %s -> %s", gpuBinMount.HostPath, gpuBinMount.ContainerPath)
		}
	}

	if config.SkipFileMounts {
		result.VolumeMounts = filterDirectoryMounts(result.VolumeMounts)
	}

	return result, nil
}

// ensureAndMountGPUBinary downloads GPU binary (like nvidia-smi) and returns a volume mount
// The binary is mounted to /usr/local/bin/ in the container
func ensureAndMountGPUBinary(ctx context.Context, paths *platform.Paths, vendorSlug, targetArch string) (*VolumeMount, error) {
	// Studios run in Linux containers, so download Linux binary with target CPU arch
	binPath, err := deps.EnsureGPUBinaryForPlatform(ctx, paths, vendorSlug, "linux", targetArch)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure GPU binary: %w", err)
	}
	if binPath == "" {
		// No binary available for this vendor/platform combination
		return nil, nil
	}

	// Get the binary name (e.g., "nvidia-smi")
	binaryName := deps.GetGPUBinaryName(vendorSlug)
	if binaryName == "" {
		return nil, nil
	}

	// Mount the binary to /usr/local/bin/
	containerPath := fmt.Sprintf("/usr/local/bin/%s", binaryName)

	return &VolumeMount{
		HostPath:      binPath,
		ContainerPath: containerPath,
		ReadOnly:      true,
	}, nil
}

// ensureGPUClientLibraries downloads GPU client libraries for Linux containers
// Libraries are downloaded for Linux platform with the specified target CPU architecture
func ensureGPUClientLibraries(ctx context.Context, vendor GPUVendor, targetArch string) error {
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

	klog.Infof("Downloading GPU client libraries for %s (linux/%s)...", vendorSlug, targetArch)

	// Studios run in Linux containers, so we always download Linux libraries
	// Use the specified target architecture from the platform flag
	_, err := depsMgr.EnsureLibrariesByTypesForPlatform(ctx, targetTypes, vendorSlug, "linux", targetArch, nil)
	if err != nil {
		return fmt.Errorf("failed to ensure GPU client libraries: %w", err)
	}

	klog.V(2).Info("GPU client libraries downloaded successfully")
	return nil
}

func buildContainerLDPreload(libsPath string, vendor GPUVendor) string {
	libNames := FindActualLibraryFiles(libsPath, vendor)
	if len(libNames) == 0 {
		return ""
	}

	preloadPaths := make([]string, 0, len(libNames))
	for _, lib := range libNames {
		preloadPaths = append(preloadPaths, filepath.Join("/opt/gpugo/libs", lib))
	}

	return strings.Join(preloadPaths, ":")
}

func filterDirectoryMounts(mounts []VolumeMount) []VolumeMount {
	filtered := make([]VolumeMount, 0, len(mounts))
	for _, mount := range mounts {
		info, err := os.Stat(mount.HostPath)
		if err != nil {
			klog.V(2).Infof("Skipping mount %s: %v", mount.HostPath, err)
			continue
		}
		if !info.IsDir() {
			klog.V(2).Infof("Skipping file mount %s", mount.HostPath)
			continue
		}
		filtered = append(filtered, mount)
	}
	return filtered
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

// SSH public key file names to search for (in priority order)
var sshPublicKeyNames = []string{
	"id_ed25519.pub",
	"id_ecdsa.pub",
	"id_rsa.pub",
	"id_dsa.pub",
}

// findUserSSHPublicKey finds the user's SSH public key
// Returns the public key content and the path to the key file
func findUserSSHPublicKey() (string, string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get user home directory: %w", err)
	}

	sshDir := filepath.Join(homeDir, ".ssh")
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		return "", "", fmt.Errorf("SSH directory not found: %s", sshDir)
	}

	// Search for public key files in priority order
	for _, keyName := range sshPublicKeyNames {
		keyPath := filepath.Join(sshDir, keyName)
		if content, err := os.ReadFile(keyPath); err == nil {
			klog.V(2).Infof("Found SSH public key: %s", keyPath)
			return string(content), keyPath, nil
		}
	}

	return "", "", fmt.Errorf("no SSH public key found in %s", sshDir)
}

// SSHAuthorizedKeysPath returns the path to store authorized_keys for a studio
func SSHAuthorizedKeysPath(paths *platform.Paths, studioName string) string {
	normalizedName := platform.NormalizeName(studioName)
	return filepath.Join(paths.StudioConfigDir(normalizedName), "authorized_keys")
}

// setupSSHAuthorizedKeys creates the authorized_keys file for a studio
// Returns the path to the created file, or empty string if no public key found
func setupSSHAuthorizedKeys(paths *platform.Paths, studioName string) (string, error) {
	publicKey, _, err := findUserSSHPublicKey()
	if err != nil {
		return "", err
	}

	authorizedKeysPath := SSHAuthorizedKeysPath(paths, studioName)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(authorizedKeysPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory for authorized_keys: %w", err)
	}

	// Write authorized_keys file with proper permissions
	if err := os.WriteFile(authorizedKeysPath, []byte(publicKey), 0600); err != nil {
		return "", fmt.Errorf("failed to write authorized_keys: %w", err)
	}

	klog.V(2).Infof("Created authorized_keys for studio %s: %s", studioName, authorizedKeysPath)
	return authorizedKeysPath, nil
}

// setupSSHAuthorizedKeysDir creates an authorized_keys file inside a dedicated directory
func setupSSHAuthorizedKeysDir(paths *platform.Paths, studioName string) (string, error) {
	publicKey, _, err := findUserSSHPublicKey()
	if err != nil {
		return "", err
	}

	normalizedName := platform.NormalizeName(studioName)
	sshDir := filepath.Join(paths.StudioConfigDir(normalizedName), "ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create directory for authorized_keys: %w", err)
	}

	authorizedKeysPath := filepath.Join(sshDir, "authorized_keys")
	if err := os.WriteFile(authorizedKeysPath, []byte(publicKey), 0600); err != nil {
		return "", fmt.Errorf("failed to write authorized_keys: %w", err)
	}

	klog.V(2).Infof("Created authorized_keys directory for studio %s: %s", studioName, authorizedKeysPath)
	return authorizedKeysPath, nil
}

// getSSHDirectoryMounts returns volume mounts for SSH access using directory mounts only
func getSSHDirectoryMounts(paths *platform.Paths, studioName string) []VolumeMount {
	var mounts []VolumeMount

	authorizedKeysPath, err := setupSSHAuthorizedKeysDir(paths, studioName)
	if err != nil {
		klog.V(2).Infof("Could not setup authorized_keys: %v (SSH access to container may require password)", err)
		return mounts
	}

	mounts = append(mounts, VolumeMount{
		HostPath:      filepath.Dir(authorizedKeysPath),
		ContainerPath: "/root/.ssh",
		ReadOnly:      false,
	})

	return mounts
}

// getSSHVolumeMounts returns volume mounts for SSH access to the container
// This includes:
// 1. Individual SSH key files mounted to /root/.ssh/ (for git operations etc.)
// 2. Generated authorized_keys file mounted to /root/.ssh/authorized_keys
//
// Note: We mount individual files instead of the entire .ssh directory to avoid
// conflicts when also mounting authorized_keys (can't mount a file inside a read-only directory mount)
func getSSHVolumeMounts(paths *platform.Paths, studioName string) []VolumeMount {
	var mounts []VolumeMount

	homeDir, err := os.UserHomeDir()
	if err != nil {
		klog.Warningf("Failed to get user home directory for SSH mounts: %v", err)
		return mounts
	}

	sshDir := filepath.Join(homeDir, ".ssh")
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		klog.V(2).Infof("SSH directory not found, skipping SSH mounts: %s", sshDir)
		return mounts
	}

	// Mount individual SSH key files (for git operations etc.)
	// These are mounted as read-only for security
	sshKeyFiles := []string{
		"id_ed25519",
		"id_ecdsa",
		"id_rsa",
		"id_dsa",
		"config",
		"known_hosts",
	}

	for _, keyFile := range sshKeyFiles {
		keyPath := filepath.Join(sshDir, keyFile)
		if _, err := os.Stat(keyPath); err == nil {
			mounts = append(mounts, VolumeMount{
				HostPath:      keyPath,
				ContainerPath: "/root/.ssh/" + keyFile,
				ReadOnly:      true,
			})
			klog.V(2).Infof("Mounting SSH file: %s", keyFile)
		}
	}

	// Create and mount authorized_keys for SSH access to the container
	authorizedKeysPath, err := setupSSHAuthorizedKeys(paths, studioName)
	if err != nil {
		klog.V(2).Infof("Could not setup authorized_keys: %v (SSH access to container may require password)", err)
	} else {
		mounts = append(mounts, VolumeMount{
			HostPath:      authorizedKeysPath,
			ContainerPath: "/root/.ssh/authorized_keys",
			ReadOnly:      false, // needs to be writable for SSH daemon to read
		})
	}

	return mounts
}
