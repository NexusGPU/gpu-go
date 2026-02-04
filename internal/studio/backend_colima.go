package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
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

// EnsureRunning ensures Colima is running
func (b *ColimaBackend) EnsureRunning(ctx context.Context) error {
	// Check if already running
	if b.IsAvailable(ctx) {
		return nil
	}

	// Start Colima with the specified profile
	cmd := exec.CommandContext(ctx, "colima", "start",
		"-p", b.profile,
		"--cpu", "4",
		"--memory", "8",
		"--disk", "60",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start Colima profile '%s': %w, output: %s", b.profile, err, string(output))
	}

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
