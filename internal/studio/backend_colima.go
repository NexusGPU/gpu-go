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

// SetDockerHost sets a custom Docker socket path
func (b *ColimaBackend) SetDockerHost(dockerHost string) {
	b.dockerHost = dockerHost
	b.dockerBackend = NewDockerBackend()
}

// pullImageWithProgress pulls a Docker image with progress output to stderr
func (b *ColimaBackend) pullImageWithProgress(ctx context.Context, image string) error {
	// Check if image already exists locally
	checkCmd := exec.CommandContext(ctx, "docker", "image", "inspect", image)
	checkCmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", b.dockerHost))
	if err := checkCmd.Run(); err == nil {
		return nil // Image exists
	}

	// Image doesn't exist, pull it with progress
	fmt.Fprintf(os.Stderr, "\n   Pulling image: %s\n", image)
	fmt.Fprintf(os.Stderr, "   This may take a few minutes for large images...\n\n")

	pullCmd := exec.CommandContext(ctx, "docker", "pull", image)
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

	// Detect system resources and calculate 60% allocation
	memGB, err := getSystemMemoryGB()
	if err != nil {
		// Fallback to 8GB if detection fails
		memGB = 8
	}
	// Calculate 60% and round up to ensure sufficient memory
	memAllocated := int(float64(memGB)*0.6 + 0.5)
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
	// - Memory: 60% of system memory (minimum 2GB)
	// - Disk: 60% of available disk space (minimum 500GB)
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

	// Generate container name
	containerName := fmt.Sprintf("ggo-%s", opts.Name)

	// Build docker run command
	args := []string{"run", "-d", "--name", containerName}

	// Add labels
	args = append(args, "--label", "ggo.managed=true")
	args = append(args, "--label", fmt.Sprintf("ggo.name=%s", opts.Name))
	args = append(args, "--label", "ggo.mode=colima")

	// Find available SSH port
	sshPort := findAvailablePort(2222)
	args = append(args, "-p", fmt.Sprintf("%d:22", sshPort))

	// Add additional port mappings
	for _, port := range opts.Ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		args = append(args, "-p", fmt.Sprintf("%d:%d/%s", port.HostPort, port.ContainerPort, protocol))
	}

	// Add environment variables
	if opts.GPUWorkerURL != "" {
		args = append(args, "-e", fmt.Sprintf("GPU_GO_CONNECTION_URL=%s", opts.GPUWorkerURL))
		args = append(args, "-e", "CUDA_VISIBLE_DEVICES=0")
	}

	for k, v := range opts.Envs {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Add volume mounts
	for _, vol := range opts.Volumes {
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
	if opts.Resources.Memory != "" {
		args = append(args, "--memory", opts.Resources.Memory)
	}

	// Add image
	image := opts.Image
	if image == "" {
		image = DefaultImageStudioTorch
	}
	args = append(args, image)

	// Pull image first with progress visible to user
	if err := b.pullImageWithProgress(ctx, image); err != nil {
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
		GPUWorkerURL: opts.GPUWorkerURL,
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
			ID    string `json:"ID"`
			Names string `json:"Names"`
			Image string `json:"Image"`
			State string `json:"State"`
			Ports string `json:"Ports"`
		}

		if err := json.Unmarshal([]byte(line), &container); err != nil {
			continue
		}

		name := strings.TrimPrefix(container.Names, "ggo-")
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
