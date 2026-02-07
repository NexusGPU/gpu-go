package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ColimaBackend implements the Backend interface using Colima (macOS/Linux)
type ColimaBackend struct {
	dockerBackend *DockerBackend
	profile       string // Colima profile name (default: "default")
	dockerHost    string // Custom docker host socket path
}

// NewColimaBackend creates a new Colima backend
func NewColimaBackend() *ColimaBackend {
	return NewColimaBackendWithProfile("")
}

// NewColimaBackendWithProfile creates a new Colima backend with specific profile
func NewColimaBackendWithProfile(profile string) *ColimaBackend {
	if profile == "" {
		profile = "default"
	}

	// Detect Docker socket path for this Colima profile
	homeDir, _ := os.UserHomeDir()
	dockerHost := fmt.Sprintf("unix://%s/.colima/%s/docker.sock", homeDir, profile)

	return &ColimaBackend{
		dockerBackend: NewDockerBackend(),
		profile:       profile,
		dockerHost:    dockerHost,
	}
}

// SetDockerHost sets a custom Docker socket path (overrides default Colima socket).
func (b *ColimaBackend) SetDockerHost(dockerHost string) {
	if dockerHost == "" {
		return
	}
	b.dockerHost = dockerHost
	b.dockerBackend = NewDockerBackendWithHost(dockerHost)
}

// GetVMArch returns the architecture of the Colima VM
// Returns "amd64", "arm64", or empty string if detection fails
func (b *ColimaBackend) GetVMArch(ctx context.Context) string {
	// Use colima status to get VM info
	cmd := exec.CommandContext(ctx, "colima", "status", "-p", b.profile, "--json")
	output, err := cmd.Output()
	if err != nil {
		// Fallback: try running uname -m inside docker
		unameCmd := exec.CommandContext(ctx, "docker", "run", "--rm", "alpine", "uname", "-m")
		unameCmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", b.dockerHost))
		unameOutput, unameErr := unameCmd.Output()
		if unameErr == nil {
			arch := strings.TrimSpace(string(unameOutput))
			return NormalizeArch(arch)
		}
		return ""
	}

	var status struct {
		Arch string `json:"arch"`
	}

	if err := json.Unmarshal(output, &status); err != nil {
		return ""
	}

	return NormalizeArch(status.Arch)
}

// pullImageWithProgress pulls a Docker image with progress output to stderr
// If platform is specified, it will use --platform flag
func (b *ColimaBackend) pullImageWithProgress(ctx context.Context, image, platform string) error {
	// Check if image already exists locally with correct platform
	checkCmd := exec.CommandContext(ctx, "docker", "image", "inspect", image)
	checkCmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", b.dockerHost))
	if err := checkCmd.Run(); err == nil {
		// Image exists, but we should re-pull if platform is specified
		// to ensure we get the right architecture
		if platform == "" {
			return nil // Image exists and no specific platform requested
		}
	}

	// Image doesn't exist or we need to ensure correct platform, pull it with progress
	fmt.Fprintf(os.Stderr, "\n   Pulling image: %s\n", image)
	if platform != "" {
		fmt.Fprintf(os.Stderr, "   Platform: %s\n", platform)
	}
	fmt.Fprintf(os.Stderr, "   This may take a few minutes for large images...\n\n")

	pullArgs := []string{"pull"}
	if platform != "" {
		pullArgs = append(pullArgs, "--platform", platform)
	}
	pullArgs = append(pullArgs, image)

	pullCmd := exec.CommandContext(ctx, "docker", pullArgs...)
	pullCmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", b.dockerHost))
	// Stream output to stderr so user can see progress
	pullCmd.Stdout = os.Stderr
	pullCmd.Stderr = os.Stderr

	if err := pullCmd.Run(); err != nil {
		return fmt.Errorf("failed to pull image %s: %w", image, err)
	}

	fmt.Fprintf(os.Stderr, "\n   Image pulled successfully!\n\n")
	return nil
}

func (b *ColimaBackend) Name() string {
	return "colima"
}

func (b *ColimaBackend) Mode() Mode {
	return ModeColima
}

// SocketPath implements BackendSocketPath.
func (b *ColimaBackend) SocketPath(ctx context.Context) string {
	return b.dockerHost
}

func (b *ColimaBackend) IsAvailable(ctx context.Context) bool {
	if runtime.GOOS != OSDarwin && runtime.GOOS != OSLinux {
		return false
	}

	// First, check if colima command exists
	if _, err := exec.LookPath("colima"); err != nil {
		return false
	}

	// Use colima list to check if the profile is running
	// This is more reliable than colima status -p which may fail
	cmd := exec.CommandContext(ctx, "colima", "list")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: check if Docker socket exists
		homeDir, _ := os.UserHomeDir()
		sockPath := fmt.Sprintf("%s/.colima/%s/docker.sock", homeDir, b.profile)
		if _, err := os.Stat(sockPath); err == nil {
			// Socket exists, try to verify with docker
			testCmd := exec.CommandContext(ctx, "docker", "info")
			testCmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=unix://%s", sockPath))
			if err := testCmd.Run(); err == nil {
				return true
			}
		}
		return false
	}

	// Parse colima list output to find the profile
	outputStr := string(output)
	lines := strings.SplitSeq(outputStr, "\n")
	for line := range lines {
		// Skip header line
		if strings.Contains(line, "PROFILE") || strings.Contains(line, "STATUS") {
			continue
		}
		// Check if this line contains our profile and "Running" status
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			profile := fields[0]
			status := strings.ToLower(fields[1])
			if profile == b.profile && (status == string(StatusRunning) || strings.Contains(status, string(StatusRunning))) {
				return true
			}
		}
	}

	return false
}

// IsInstalled checks if colima is installed (but not necessarily running)
func (b *ColimaBackend) IsInstalled(ctx context.Context) bool {
	if runtime.GOOS != OSDarwin && runtime.GOOS != OSLinux {
		return false
	}
	_, err := exec.LookPath("colima")
	return err == nil
}

// getSystemMemoryGB detects system total memory in GB
func getSystemMemoryGB() (int, error) {
	switch runtime.GOOS {
	case OSDarwin:
		// macOS: use sysctl
		cmd := exec.Command("sysctl", "-n", "hw.memsize")
		output, err := cmd.Output()
		if err != nil {
			return 0, err
		}
		memBytes, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
		if err != nil {
			return 0, err
		}
		// Convert bytes to GB (round up)
		memGB := int((memBytes + (1024*1024*1024 - 1)) / (1024 * 1024 * 1024))
		return memGB, nil
	case OSLinux:
		// Linux: read from /proc/meminfo
		cmd := exec.Command("grep", "MemTotal", "/proc/meminfo")
		output, err := cmd.Output()
		if err != nil {
			return 0, err
		}
		// Parse "MemTotal:       16384000 kB"
		parts := strings.Fields(string(output))
		if len(parts) < 2 {
			return 0, fmt.Errorf("unexpected meminfo format")
		}
		memKB, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, err
		}
		// Convert KB to GB (round up)
		memGB := int((memKB*1024 + (1024*1024*1024 - 1)) / (1024 * 1024 * 1024))
		return memGB, nil
	}
	// Fallback: return 8GB if detection fails
	return 8, nil
}

// getSystemDiskGB detects available disk space in GB for the home directory
func getSystemDiskGB() (int, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return 0, err
	}

	switch runtime.GOOS {
	case OSDarwin:
		// macOS: use df command
		cmd := exec.Command("df", "-g", homeDir)
		output, err := cmd.Output()
		if err != nil {
			return 0, err
		}
		// Parse df output: "Filesystem   1024-blocks      Used Available Capacity  Mounted on"
		lines := strings.Split(string(output), "\n")
		if len(lines) < 2 {
			return 0, fmt.Errorf("unexpected df output format")
		}
		fields := strings.Fields(lines[1])
		if len(fields) < 4 {
			return 0, fmt.Errorf("unexpected df output format")
		}
		// Available is the 4th field (0-indexed: 3)
		availGB, err := strconv.Atoi(fields[3])
		if err != nil {
			return 0, err
		}
		return availGB, nil
	case OSLinux:
		// Linux: use df command
		cmd := exec.Command("df", "-BG", homeDir)
		output, err := cmd.Output()
		if err != nil {
			return 0, err
		}
		// Parse df output
		lines := strings.Split(string(output), "\n")
		if len(lines) < 2 {
			return 0, fmt.Errorf("unexpected df output format")
		}
		fields := strings.Fields(lines[1])
		if len(fields) < 4 {
			return 0, fmt.Errorf("unexpected df output format")
		}
		// Available is the 4th field, remove 'G' suffix
		availStr := strings.TrimSuffix(fields[3], "G")
		availGB, err := strconv.Atoi(availStr)
		if err != nil {
			return 0, err
		}
		return availGB, nil
	}
	// Fallback: return 60GB if detection fails
	return 60, nil
}

// EnsureRunning ensures Colima is running
func (b *ColimaBackend) EnsureRunning(ctx context.Context) error {
	// Check if already running
	if b.IsAvailable(ctx) {
		return nil
	}

	// Detect system resources and calculate 1/4 allocation for VM
	memGB, err := getSystemMemoryGB()
	if err != nil {
		// Fallback to 8GB if detection fails
		memGB = 8
	}
	// Calculate 1/4 of system memory (round up)
	memAllocated := (memGB + 3) / 4
	if memAllocated < 2 {
		memAllocated = 2 // Minimum 2GB
	}

	diskGB, err := getSystemDiskGB()
	if err != nil {
		// Fallback to 60GB if detection fails
		diskGB = 60
	}
	// Calculate 60% and round up, but ensure we don't exceed available space
	diskAllocated := int(float64(diskGB)*0.6 + 0.5)
	if diskAllocated > diskGB {
		diskAllocated = diskGB // Don't exceed available space
	}
	if diskAllocated < 200 {
		diskAllocated = 200 // Minimum 200GB
	}

	// Get CPU count (don't limit CPU, use all available cores)
	cpuCount := runtime.NumCPU()
	if cpuCount < 1 {
		cpuCount = 1 // Minimum 1 CPU
	}

	// Inform user that Colima is starting (this can take several minutes for first time)
	fmt.Fprintf(os.Stderr, "\n   Starting Colima VM (profile: %s)...\n", b.profile)
	fmt.Fprintf(os.Stderr, "   Resources: %d CPUs, %dGB memory, %dGB disk\n", cpuCount, memAllocated, diskAllocated)
	fmt.Fprintf(os.Stderr, "   This may take a few minutes on first run...\n\n")

	// Start Colima with the specified profile
	// - CPU: use all available cores (no limit)
	// - Memory: 1/4 of system memory (minimum 2GB)
	// - Disk: 60% of available disk space (minimum 200GB)
	// - DNS: configure DNS servers to fix Docker pull issues
	cmd := exec.CommandContext(ctx, "colima", "start",
		"-p", b.profile,
		"--cpu", strconv.Itoa(cpuCount),
		"--memory", fmt.Sprintf("%d", memAllocated),
		"--disk", fmt.Sprintf("%d", diskAllocated),
		"--dns", "1.1.1.1",
	)
	// Stream output to stderr so user can see progress
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start Colima profile '%s': %w", b.profile, err)
	}

	fmt.Fprintf(os.Stderr, "\n   Colima VM started successfully!\n\n")
	return nil
}

func (b *ColimaBackend) Create(ctx context.Context, opts *CreateOptions) (*Environment, error) {
	// Ensure Colima is running
	if err := b.EnsureRunning(ctx); err != nil {
		return nil, err
	}

	// Determine the platform to use
	// If user specified platform, use it; otherwise detect from VM
	platform := opts.Platform
	if platform == "" {
		// Auto-detect VM architecture
		vmArch := b.GetVMArch(ctx)
		if vmArch != "" {
			platform = "linux/" + vmArch
			fmt.Fprintf(os.Stderr, "   Auto-detected VM architecture: %s\n", platform)
		}
	}

	// Generate container name with random suffix to avoid conflicts
	containerName := GenerateContainerName(opts.Name)

	// Build docker run command
	args := []string{"run", "-d", "--name", containerName}

	// Add platform flag if specified
	if platform != "" {
		args = append(args, "--platform", platform)
	}

	// Add labels
	args = append(args, "--label", "ggo.managed=true")
	args = append(args, "--label", fmt.Sprintf("ggo.name=%s", opts.Name))
	args = append(args, "--label", "ggo.mode=colima")

	// Add port mappings
	sshPort := 0
	for _, port := range opts.Ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = DefaultProtocolTCP
		}
		args = append(args, "-p", fmt.Sprintf("%d:%d/%s", port.HostPort, port.ContainerPort, protocol))
		if port.ContainerPort == 22 {
			sshPort = port.HostPort
		}
	}

	// Add default SSH port if not specified (use random port in 12000-18000 range)
	if sshPort == 0 {
		sshPort = findAvailablePort(0)
		args = append(args, "-p", fmt.Sprintf("%d:22/tcp", sshPort))
	}

	// Use endpoint override if specified
	gpuWorkerURL := opts.GPUWorkerURL
	if opts.Endpoint != "" {
		gpuWorkerURL = opts.Endpoint
	}

	// Setup container GPU environment using common abstraction
	// This downloads GPU client libraries and sets up env vars, volumes
	setupConfig := &ContainerSetupConfig{
		StudioName:     opts.Name,
		GPUWorkerURL:   gpuWorkerURL,
		HardwareVendor: opts.HardwareVendor,
		MountUserHome:  !opts.NoUserVolume,
	}

	setupResult, err := SetupContainerGPUEnv(ctx, setupConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to setup container environment: %w", err)
	}

	// Merge setup env vars with user env vars (user takes precedence)
	mergedEnvs := MergeEnvVars(setupResult.EnvVars, opts.Envs)
	for k, v := range mergedEnvs {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Merge setup volumes with user volumes (user takes precedence)
	mergedVolumes := MergeVolumeMounts(setupResult.VolumeMounts, opts.Volumes)
	for _, vol := range mergedVolumes {
		mountOpt := fmt.Sprintf("%s:%s", vol.HostPath, vol.ContainerPath)
		if vol.ReadOnly {
			mountOpt += MountOptionReadOnly
		}
		args = append(args, "-v", mountOpt)
	}

	// Add resource limits
	if opts.Resources.CPUs > 0 {
		args = append(args, "--cpus", fmt.Sprintf("%.2f", opts.Resources.CPUs))
	}

	// Set default memory to 1/4 of system memory if not specified
	memoryLimit := opts.Resources.Memory
	if memoryLimit == "" {
		memGB, err := getSystemMemoryGB()
		if err == nil && memGB > 0 {
			// Calculate 1/4 of system memory (round up)
			memAllocated := (memGB + 3) / 4 // Round up division
			if memAllocated < 1 {
				memAllocated = 1 // Minimum 1GB
			}
			memoryLimit = fmt.Sprintf("%dg", memAllocated)
		}
	}
	if memoryLimit != "" {
		args = append(args, "--memory", memoryLimit)
	}

	// Add image
	image := opts.Image
	if image == "" {
		image = DefaultImageStudioTorch
	}
	args = append(args, image)

	// Add command args (supplements ENTRYPOINT or overrides CMD)
	// FormatContainerCommand handles wrapping single shell commands with "sh -c"
	if formattedCmd := FormatContainerCommand(opts.Command); len(formattedCmd) > 0 {
		args = append(args, formattedCmd...)
	}

	// Pull image first with progress visible to user
	if err := b.pullImageWithProgress(ctx, image, platform); err != nil {
		return nil, fmt.Errorf("failed to pull image: %w", err)
	}

	// Run with Colima's docker context
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", b.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w, output: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))

	env := &Environment{
		ID:           containerID[:12],
		Name:         opts.Name,
		Mode:         ModeColima,
		Image:        image,
		Status:       StatusRunning,
		SSHHost:      "localhost",
		SSHPort:      sshPort,
		SSHUser:      "root",
		GPUWorkerURL: gpuWorkerURL,
		CreatedAt:    time.Now(),
		Labels:       opts.Labels,
	}

	return env, nil
}

func (b *ColimaBackend) Start(ctx context.Context, envID string) error {
	cmd := exec.CommandContext(ctx, "docker", "start", envID)
	cmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", b.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *ColimaBackend) Stop(ctx context.Context, envID string) error {
	cmd := exec.CommandContext(ctx, "docker", "stop", envID)
	cmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", b.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stop container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *ColimaBackend) Remove(ctx context.Context, envID string) error {
	_ = b.Stop(ctx, envID)

	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", envID)
	cmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", b.dockerHost))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *ColimaBackend) List(ctx context.Context) ([]*Environment, error) {
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", "label=ggo.mode=colima",
		"--format", "{{json .}}")
	cmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", b.dockerHost))

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var envs []*Environment
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var container struct {
			ID     string `json:"ID"`
			Names  string `json:"Names"`
			Image  string `json:"Image"`
			State  string `json:"State"`
			Ports  string `json:"Ports"`
			Labels string `json:"Labels"`
		}

		if err := json.Unmarshal([]byte(line), &container); err != nil {
			continue
		}

		// Parse name from labels if available, otherwise strip prefix
		name := strings.TrimPrefix(container.Names, "ggo-")
		if labelName := extractLabelValue(container.Labels, "ggo.name"); labelName != "" {
			name = labelName
		}
		sshPort := parseSSHPort(container.Ports)

		status := StatusStopped
		if container.State == DockerStateRunning {
			status = StatusRunning
		}

		env := &Environment{
			ID:      container.ID,
			Name:    name,
			Mode:    ModeColima,
			Image:   container.Image,
			Status:  status,
			SSHHost: "localhost",
			SSHPort: sshPort,
			SSHUser: "root",
		}

		envs = append(envs, env)
	}

	return envs, nil
}

func (b *ColimaBackend) Get(ctx context.Context, idOrName string) (*Environment, error) {
	return b.dockerBackend.Get(ctx, idOrName)
}

func (b *ColimaBackend) Exec(ctx context.Context, envID string, cmd []string) ([]byte, error) {
	return b.dockerBackend.Exec(ctx, envID, cmd)
}

func (b *ColimaBackend) Logs(ctx context.Context, envID string, follow bool) (<-chan string, error) {
	return b.dockerBackend.Logs(ctx, envID, follow)
}

// GetColimaIP returns the IP address of the Colima VM
func (b *ColimaBackend) GetColimaIP(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "colima", "status", "-p", b.profile, "--json")
	output, err := cmd.Output()
	if err != nil {
		return DefaultHostLocalhost, nil
	}

	var status struct {
		Runtime struct {
			IPAddress string `json:"ip_address"`
		} `json:"runtime"`
	}

	if err := json.Unmarshal(output, &status); err != nil {
		return DefaultHostLocalhost, nil
	}

	if status.Runtime.IPAddress != "" {
		return status.Runtime.IPAddress, nil
	}

	return DefaultHostLocalhost, nil
}

// GetProfile returns the Colima profile name
func (b *ColimaBackend) GetProfile() string {
	return b.profile
}

var _ Backend = (*ColimaBackend)(nil)
var _ AutoStartableBackend = (*ColimaBackend)(nil)
