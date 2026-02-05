package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/NexusGPU/gpu-go/internal/platform"
	"k8s.io/klog/v2"
)

// DockerBackend implements the Backend interface using Docker
type DockerBackend struct {
	dockerCmd  string // docker or podman
	dockerHost string // Custom docker host (e.g., unix:///path/to/docker.sock)
}

// NewDockerBackend creates a new Docker backend
func NewDockerBackend() *DockerBackend {
	return &DockerBackend{
		dockerCmd: "docker",
	}
}

// NewDockerBackendWithHost creates a new Docker backend with custom host
func NewDockerBackendWithHost(dockerHost string) *DockerBackend {
	return &DockerBackend{
		dockerCmd:  "docker",
		dockerHost: dockerHost,
	}
}

// NewPodmanBackend creates a new Podman backend (uses Docker interface)
func NewPodmanBackend() *DockerBackend {
	return &DockerBackend{
		dockerCmd: "podman",
	}
}

func (b *DockerBackend) Name() string {
	return "docker"
}

func (b *DockerBackend) Mode() Mode {
	return ModeDocker
}

func (b *DockerBackend) IsAvailable(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, b.dockerCmd, "info")
	b.setDockerEnv(cmd)
	return cmd.Run() == nil
}

// setDockerEnv sets DOCKER_HOST environment variable if configured
func (b *DockerBackend) setDockerEnv(cmd *exec.Cmd) {
	if b.dockerHost != "" {
		cmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", b.dockerHost))
	}
}

// pullImageWithProgress pulls a Docker image with progress output to stderr
func (b *DockerBackend) pullImageWithProgress(ctx context.Context, image string) error {
	// Check if image already exists locally
	checkCmd := exec.CommandContext(ctx, b.dockerCmd, "image", "inspect", image)
	b.setDockerEnv(checkCmd)
	if err := checkCmd.Run(); err == nil {
		klog.V(2).Infof("Image %s already exists locally", image)
		return nil // Image exists
	}

	// Image doesn't exist, pull it with progress
	fmt.Fprintf(os.Stderr, "\n   Pulling image: %s\n", image)
	fmt.Fprintf(os.Stderr, "   This may take a few minutes for large images...\n\n")

	pullCmd := exec.CommandContext(ctx, b.dockerCmd, "pull", image)
	b.setDockerEnv(pullCmd)
	// Stream output to stderr so user can see progress
	pullCmd.Stdout = os.Stderr
	pullCmd.Stderr = os.Stderr

	if err := pullCmd.Run(); err != nil {
		return fmt.Errorf("failed to pull image %s: %w", image, err)
	}

	fmt.Fprintf(os.Stderr, "\n   Image pulled successfully!\n\n")
	return nil
}

func (b *DockerBackend) Create(ctx context.Context, opts *CreateOptions) (*Environment, error) {
	paths := platform.DefaultPaths()

	// Generate container name
	containerName := fmt.Sprintf("ggo-%s", opts.Name)
	normalizedName := platform.NormalizeName(opts.Name)

	// Build docker run command
	args := []string{"run", "-d", "--name", containerName}

	// Add labels
	args = append(args, "--label", "ggo.managed=true")
	args = append(args, "--label", fmt.Sprintf("ggo.name=%s", opts.Name))
	args = append(args, "--label", "ggo.mode=docker")

	// Add port mappings
	sshPort := 0
	for _, port := range opts.Ports {
		protocol := port.Protocol
		if protocol == "" {
			protocol = "tcp"
		}
		args = append(args, "-p", fmt.Sprintf("%d:%d/%s", port.HostPort, port.ContainerPort, protocol))
	}

	// Add default SSH port if not specified
	if sshPort == 0 {
		sshPort = findAvailablePort(2222)
		args = append(args, "-p", fmt.Sprintf("%d:22/tcp", sshPort))
	}

	// Setup GPU environment if GPU worker URL is provided
	if opts.GPUWorkerURL != "" {
		vendor := ParseVendor(opts.HardwareVendor)

		// Create GPU environment config
		gpuConfig := &GPUEnvConfig{
			Vendor:        vendor,
			ConnectionURL: opts.GPUWorkerURL,
			CachePath:     paths.CacheDir(),
			LogPath:       paths.StudioLogsDir(normalizedName),
			StudioName:    normalizedName,
			IsContainer:   true,
		}

		// Setup GPU environment (creates config files)
		envResult, err := SetupGPUEnv(paths, gpuConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to setup GPU environment: %w", err)
		}

		klog.Infof("Setting up GPU environment for studio %s: vendor=%s connection_url=%s",
			opts.Name, vendor, opts.GPUWorkerURL)

		// Add environment variables
		for k, v := range envResult.EnvVars {
			args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
		}

		// Add CUDA_VISIBLE_DEVICES for NVIDIA
		if vendor == VendorNvidia {
			args = append(args, "-e", "CUDA_VISIBLE_DEVICES=0")
		}

		// Add volume mounts from GPU env setup
		for _, vol := range envResult.VolumeMounts {
			mountOpt := fmt.Sprintf("%s:%s", vol.HostPath, vol.ContainerPath)
			if vol.ReadOnly {
				mountOpt += MountOptionReadOnly
			}
			args = append(args, "-v", mountOpt)
		}
	}

	// Add user-specified environment variables
	for k, v := range opts.Envs {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Add user-specified volume mounts
	for _, vol := range opts.Volumes {
		mountOpt := fmt.Sprintf("%s:%s", vol.HostPath, vol.ContainerPath)
		if vol.ReadOnly {
			mountOpt += ":ro"
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

	// Add working directory
	if opts.WorkDir != "" {
		args = append(args, "-w", opts.WorkDir)
	}

	// Add image
	image := opts.Image
	if image == "" {
		image = "tensorfusion/studio-torch:latest"
	}
	args = append(args, image)

	klog.V(2).Infof("Running docker command: %s %v", b.dockerCmd, args)

	// Pull image first with progress visible to user
	if err := b.pullImageWithProgress(ctx, image); err != nil {
		return nil, fmt.Errorf("failed to pull image: %w", err)
	}

	// Run container
	cmd := exec.CommandContext(ctx, b.dockerCmd, args...)
	b.setDockerEnv(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w, output: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))

	// Get container info
	env := &Environment{
		ID:           containerID[:12],
		Name:         opts.Name,
		Mode:         ModeDocker,
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

func (b *DockerBackend) Start(ctx context.Context, envID string) error {
	cmd := exec.CommandContext(ctx, b.dockerCmd, "start", envID)
	b.setDockerEnv(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *DockerBackend) Stop(ctx context.Context, envID string) error {
	cmd := exec.CommandContext(ctx, b.dockerCmd, "stop", envID)
	b.setDockerEnv(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stop container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *DockerBackend) Remove(ctx context.Context, envID string) error {
	// Stop first
	_ = b.Stop(ctx, envID)

	cmd := exec.CommandContext(ctx, b.dockerCmd, "rm", "-f", envID)
	b.setDockerEnv(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *DockerBackend) List(ctx context.Context) ([]*Environment, error) {
	// Filter by both ggo.managed=true and ggo.mode=docker
	// This ensures we only list containers created by the docker backend, not colima/wsl
	cmd := exec.CommandContext(ctx, b.dockerCmd, "ps", "-a",
		"--filter", "label=ggo.managed=true",
		"--filter", "label=ggo.mode=docker",
		"--format", "{{json .}}")
	b.setDockerEnv(cmd)

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
			ID      string `json:"ID"`
			Names   string `json:"Names"`
			Image   string `json:"Image"`
			State   string `json:"State"`
			Status  string `json:"Status"`
			Ports   string `json:"Ports"`
			Labels  string `json:"Labels"`
			Created string `json:"CreatedAt"`
		}

		if err := json.Unmarshal([]byte(line), &container); err != nil {
			continue
		}

		// Parse name
		name := strings.TrimPrefix(container.Names, "ggo-")

		// Parse SSH port from ports
		sshPort := parseSSHPort(container.Ports)

		// Parse status
		status := StatusStopped
		switch container.State {
		case DockerStateRunning:
			status = StatusRunning
		case DockerStateExited:
			status = StatusStopped
		case DockerStateCreated:
			status = StatusPending
		}

		env := &Environment{
			ID:      container.ID,
			Name:    name,
			Mode:    ModeDocker,
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

func (b *DockerBackend) Get(ctx context.Context, idOrName string) (*Environment, error) {
	// Try to find by container ID or name
	containerName := idOrName
	if !strings.HasPrefix(idOrName, "ggo-") {
		containerName = "ggo-" + idOrName
	}

	cmd := exec.CommandContext(ctx, b.dockerCmd, "inspect", containerName)
	b.setDockerEnv(cmd)
	output, err := cmd.Output()
	if err != nil {
		// Try with original name/ID
		cmd = exec.CommandContext(ctx, b.dockerCmd, "inspect", idOrName)
		b.setDockerEnv(cmd)
		output, err = cmd.Output()
		if err != nil {
			return nil, fmt.Errorf("environment not found: %s", idOrName)
		}
	}

	var containers []struct {
		ID    string `json:"Id"`
		Name  string `json:"Name"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		Config struct {
			Image  string            `json:"Image"`
			Labels map[string]string `json:"Labels"`
			Env    []string          `json:"Env"`
		} `json:"Config"`
		NetworkSettings struct {
			Ports map[string][]struct {
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
	}

	if err := json.Unmarshal(output, &containers); err != nil {
		return nil, fmt.Errorf("failed to parse container info: %w", err)
	}

	if len(containers) == 0 {
		return nil, fmt.Errorf("environment not found: %s", idOrName)
	}

	c := containers[0]

	// Parse name
	name := strings.TrimPrefix(strings.TrimPrefix(c.Name, "/"), "ggo-")
	if labelName, ok := c.Config.Labels["ggo.name"]; ok {
		name = labelName
	}

	// Parse SSH port
	sshPort := 22
	if ports, ok := c.NetworkSettings.Ports["22/tcp"]; ok && len(ports) > 0 {
		if p, err := strconv.Atoi(ports[0].HostPort); err == nil {
			sshPort = p
		}
	}

	// Parse GPU worker URL from env
	gpuWorkerURL := ""
	for _, env := range c.Config.Env {
		if strings.HasPrefix(env, "GPU_GO_CONNECTION_URL=") {
			gpuWorkerURL = strings.TrimPrefix(env, "GPU_GO_CONNECTION_URL=")
			break
		}
	}

	// Parse status
	status := StatusStopped
	switch c.State.Status {
	case "running":
		status = StatusRunning
	case "exited":
		status = StatusStopped
	case "created":
		status = StatusPending
	}

	env := &Environment{
		ID:           c.ID[:12],
		Name:         name,
		Mode:         ModeDocker,
		Image:        c.Config.Image,
		Status:       status,
		SSHHost:      "localhost",
		SSHPort:      sshPort,
		SSHUser:      "root",
		GPUWorkerURL: gpuWorkerURL,
		Labels:       c.Config.Labels,
	}

	return env, nil
}

func (b *DockerBackend) Exec(ctx context.Context, envID string, cmd []string) ([]byte, error) {
	args := append([]string{"exec", envID}, cmd...)
	execCmd := exec.CommandContext(ctx, b.dockerCmd, args...)
	b.setDockerEnv(execCmd)
	return execCmd.CombinedOutput()
}

func (b *DockerBackend) Logs(ctx context.Context, envID string, follow bool) (<-chan string, error) {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, envID)

	cmd := exec.CommandContext(ctx, b.dockerCmd, args...)
	b.setDockerEnv(cmd)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	logCh := make(chan string, 100)
	go func() {
		defer close(logCh)
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				logCh <- string(buf[:n])
			}
			if err != nil {
				break
			}
		}
		_ = cmd.Wait()
	}()

	return logCh, nil
}

// Helper functions

func parseSSHPort(ports string) int {
	// Parse "0.0.0.0:2222->22/tcp, ..." format
	for _, part := range strings.Split(ports, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "->22/tcp") {
			// Extract host port
			parts := strings.Split(part, "->")
			if len(parts) > 0 {
				hostPart := parts[0]
				colonIdx := strings.LastIndex(hostPart, ":")
				if colonIdx >= 0 {
					if port, err := strconv.Atoi(hostPart[colonIdx+1:]); err == nil {
						return port
					}
				}
			}
		}
	}
	return 22
}

func findAvailablePort(start int) int {
	// Simple implementation - in production, actually check if port is available
	return start
}

var _ Backend = (*DockerBackend)(nil)

func init() {
	_ = bytes.Buffer{} // silence import
}
