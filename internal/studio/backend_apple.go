package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// AppleContainerBackend implements the Backend interface using Apple native containers
// This uses Docker CLI with the assumption that Docker Desktop or similar is running
// on macOS using Apple Virtualization Framework
type AppleContainerBackend struct {
	dockerCmd string
}

// NewAppleContainerBackend creates a new Apple container backend
func NewAppleContainerBackend() *AppleContainerBackend {
	return &AppleContainerBackend{
		dockerCmd: "docker",
	}
}

func (b *AppleContainerBackend) Name() string {
	return "apple-container"
}

func (b *AppleContainerBackend) Mode() Mode {
	return ModeAppleContainer
}

func (b *AppleContainerBackend) IsAvailable(ctx context.Context) bool {
	// Only available on macOS
	if runtime.GOOS != OSDarwin {
		return false
	}

	// Check if Docker is available
	cmd := exec.CommandContext(ctx, b.dockerCmd, "info")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}

	// Check if it's using Apple Virtualization Framework or native macOS
	outputStr := string(output)
	return strings.Contains(outputStr, "Operating System") &&
		(strings.Contains(outputStr, "macOS") || strings.Contains(outputStr, "Darwin"))
}

func (b *AppleContainerBackend) Create(ctx context.Context, opts *CreateOptions) (*Environment, error) {
	// Generate container name with random suffix to avoid conflicts
	containerName := GenerateContainerName(opts.Name)

	// Build docker run command
	args := []string{"run", "-d", "--name", containerName}

	// Add labels
	args = append(args, "--label", "ggo.managed=true")
	args = append(args, "--label", fmt.Sprintf("ggo.name=%s", opts.Name))
	args = append(args, "--label", "ggo.backend=apple-container")

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

	// Add default SSH port if not specified
	if sshPort == 0 {
		sshPort = findAvailablePort(2222)
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

	// Add resource limits (macOS-specific optimizations)
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

	// Add working directory
	if opts.WorkDir != "" {
		args = append(args, "-w", opts.WorkDir)
	}

	// Restart policy for better reliability
	args = append(args, "--restart", "unless-stopped")

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

	// Run container
	fmt.Printf("Creating Apple container with command: %s %s\n", b.dockerCmd, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, b.dockerCmd, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w, output: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))

	// Get container info
	env := &Environment{
		ID:           containerID[:12],
		Name:         opts.Name,
		Mode:         ModeAppleContainer,
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

func (b *AppleContainerBackend) Start(ctx context.Context, envID string) error {
	cmd := exec.CommandContext(ctx, b.dockerCmd, "start", envID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *AppleContainerBackend) Stop(ctx context.Context, envID string) error {
	cmd := exec.CommandContext(ctx, b.dockerCmd, "stop", envID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stop container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *AppleContainerBackend) Remove(ctx context.Context, envID string) error {
	// Stop first
	_ = b.Stop(ctx, envID)

	cmd := exec.CommandContext(ctx, b.dockerCmd, "rm", "-f", envID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *AppleContainerBackend) List(ctx context.Context) ([]*Environment, error) {
	cmd := exec.CommandContext(ctx, b.dockerCmd, "ps", "-a",
		"--filter", "label=ggo.managed=true",
		"--filter", "label=ggo.backend=apple-container",
		"--format", "{{json .}}")

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
			Mode:    ModeAppleContainer,
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

func (b *AppleContainerBackend) Get(ctx context.Context, idOrName string) (*Environment, error) {
	// Try to find by container ID or name
	containerName := idOrName
	if !strings.HasPrefix(idOrName, "ggo-") {
		containerName = "ggo-" + idOrName
	}

	cmd := exec.CommandContext(ctx, b.dockerCmd, "inspect", containerName)
	output, err := cmd.Output()
	if err != nil {
		// Try with original name/ID
		cmd = exec.CommandContext(ctx, b.dockerCmd, "inspect", idOrName)
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
		if strings.HasPrefix(env, "GPU_WORKER_URL=") {
			gpuWorkerURL = strings.TrimPrefix(env, "GPU_WORKER_URL=")
			break
		}
	}

	// Parse status
	status := StatusStopped
	switch c.State.Status {
	case DockerStateRunning:
		status = StatusRunning
	case DockerStateExited:
		status = StatusStopped
	case DockerStateCreated:
		status = StatusPending
	}

	env := &Environment{
		ID:           c.ID[:12],
		Name:         name,
		Mode:         ModeAppleContainer,
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

func (b *AppleContainerBackend) Exec(ctx context.Context, envID string, cmd []string) ([]byte, error) {
	args := append([]string{"exec", envID}, cmd...)
	execCmd := exec.CommandContext(ctx, b.dockerCmd, args...)
	return execCmd.CombinedOutput()
}

func (b *AppleContainerBackend) Logs(ctx context.Context, envID string, follow bool) (<-chan string, error) {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, envID)

	cmd := exec.CommandContext(ctx, b.dockerCmd, args...)

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

var _ Backend = (*AppleContainerBackend)(nil)

func init() {
	_ = bytes.Buffer{} // silence import
}
