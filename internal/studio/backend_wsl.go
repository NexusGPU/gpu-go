package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// WSLBackend implements the Backend interface using Windows Subsystem for Linux
type WSLBackend struct {
	dockerBackend *DockerBackend
	distro        string // WSL distribution name
}

// NewWSLBackend creates a new WSL backend
func NewWSLBackend() *WSLBackend {
	return NewWSLBackendWithDistro("")
}

// NewWSLBackendWithDistro creates a new WSL backend with specific distro
func NewWSLBackendWithDistro(distro string) *WSLBackend {
	return &WSLBackend{
		dockerBackend: NewDockerBackend(),
		distro:        distro, // empty means use default
	}
}

// SetDistro sets the WSL distribution name
func (b *WSLBackend) SetDistro(distro string) {
	b.distro = distro
}

// GetDistro returns the configured or default WSL distribution
func (b *WSLBackend) GetDistro(ctx context.Context) (string, error) {
	if b.distro != "" {
		return b.distro, nil
	}
	return b.getDefaultDistro(ctx)
}

func (b *WSLBackend) Name() string {
	return "wsl"
}

func (b *WSLBackend) Mode() Mode {
	return ModeWSL
}

// SocketPath implements BackendSocketPath. Returns the Docker socket path inside the WSL distro.
func (b *WSLBackend) SocketPath(ctx context.Context) string {
	distro, err := b.GetDistro(ctx)
	if err != nil || distro == "" {
		distro = "default"
	}
	return fmt.Sprintf("WSL (%s): /var/run/docker.sock (inside distro)", distro)
}

func (b *WSLBackend) IsAvailable(ctx context.Context) bool {
	if runtime.GOOS != OSWindows {
		return false
	}

	status := b.GetWSLStatus(ctx)
	return status.IsReady()
}

// WSLStatus represents the detailed status of WSL
type WSLStatus struct {
	WSLInstalled     bool
	HasDistribution  bool
	DistributionName string
	DockerInstalled  bool
	DockerRunning    bool
	ErrorMessage     string
}

// IsReady returns true if WSL is ready to run containers
func (s *WSLStatus) IsReady() bool {
	return s.WSLInstalled && s.HasDistribution && s.DockerInstalled && s.DockerRunning
}

// GetInstallGuidance returns installation guidance based on the current status
func (s *WSLStatus) GetInstallGuidance() string {
	switch {
	case !s.WSLInstalled:
		return `WSL is not installed. Please run the following in PowerShell (as Administrator):

  wsl --install

Then restart your computer and run 'ggo studio backends' again.`

	case !s.HasDistribution:
		return `WSL is installed but no Linux distribution found. Please run:

  wsl --install -d Ubuntu

Then restart your computer and run 'ggo studio backends' again.`

	case !s.DockerInstalled:
		return fmt.Sprintf(`WSL distribution '%s' found but Docker is not installed.
Please open WSL terminal and run:

  curl -fsSL https://get.docker.com | sh
  sudo usermod -aG docker $USER

Then log out and back in to WSL, and run 'ggo studio backends' again.`, s.DistributionName)

	case !s.DockerRunning:
		return fmt.Sprintf(`Docker is installed in WSL '%s' but not running.
Please open WSL terminal and run:

  sudo service docker start

Or to start Docker automatically, add to your ~/.bashrc:

  if service docker status 2>&1 | grep -q "is not running"; then
    sudo service docker start
  fi`, s.DistributionName)
	}
	return ""
}

// GetWSLStatus checks the detailed status of WSL
func (b *WSLBackend) GetWSLStatus(ctx context.Context) *WSLStatus {
	status := &WSLStatus{}

	// Check if WSL command exists
	cmd := exec.CommandContext(ctx, "wsl", "--status")
	if err := cmd.Run(); err != nil {
		status.WSLInstalled = false
		status.ErrorMessage = "WSL is not installed or not accessible"
		return status
	}
	status.WSLInstalled = true

	// Check for distributions
	distro, err := b.getDefaultDistro(ctx)
	if err != nil {
		status.HasDistribution = false
		status.ErrorMessage = "No WSL distribution found"
		return status
	}
	status.HasDistribution = true
	status.DistributionName = distro

	// Check if Docker is installed in WSL
	output, err := b.runInWSL(ctx, distro, "which", "docker")
	if err != nil || strings.TrimSpace(string(output)) == "" {
		status.DockerInstalled = false
		status.ErrorMessage = "Docker is not installed in WSL"
		return status
	}
	status.DockerInstalled = true

	// Check if Docker daemon is running
	output, err = b.runInWSL(ctx, distro, "docker", "info")
	if err != nil {
		status.DockerRunning = false
		// Check if it's a permission issue
		if strings.Contains(string(output), "permission denied") {
			status.ErrorMessage = "Docker permission denied. Run: sudo usermod -aG docker $USER"
		} else if strings.Contains(string(output), "Cannot connect") {
			status.ErrorMessage = "Docker daemon is not running"
		} else {
			status.ErrorMessage = fmt.Sprintf("Docker error: %s", string(output))
		}
		return status
	}
	status.DockerRunning = true

	return status
}

// getDefaultDistro returns the default WSL distribution
func (b *WSLBackend) getDefaultDistro(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "wsl", "--list", "--quiet")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to list WSL distributions: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Remove null characters that Windows adds
		line = strings.ReplaceAll(line, "\x00", "")
		if line != "" {
			return line, nil
		}
	}

	return "", fmt.Errorf("no WSL distribution found")
}

// runInWSL runs a command in WSL
func (b *WSLBackend) runInWSL(ctx context.Context, distro string, cmdArgs ...string) ([]byte, error) {
	args := []string{"-d", distro, "--"}
	args = append(args, cmdArgs...)

	cmd := exec.CommandContext(ctx, "wsl", args...)
	return cmd.CombinedOutput()
}

func (b *WSLBackend) Create(ctx context.Context, opts *CreateOptions) (*Environment, error) {
	distro, err := b.GetDistro(ctx)
	if err != nil {
		return nil, err
	}

	// Check if Docker is available in WSL
	output, err := b.runInWSL(ctx, distro, "docker", "info")
	if err != nil {
		return nil, fmt.Errorf("docker not available in WSL: %s", string(output))
	}

	// Create container using Docker in WSL with random suffix to avoid conflicts
	containerName := GenerateContainerName(opts.Name)

	// Build docker run command
	args := []string{"docker", "run", "-d", "--name", containerName}

	// Add labels
	args = append(args, "--label", "ggo.managed=true")
	args = append(args, "--label", fmt.Sprintf("ggo.name=%s", opts.Name))
	args = append(args, "--label", "ggo.mode=wsl")

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
	// Convert Windows paths to WSL paths for volume mounts
	mergedVolumes := MergeVolumeMounts(setupResult.VolumeMounts, opts.Volumes)
	for _, vol := range mergedVolumes {
		hostPath := b.windowsToWSLPath(vol.HostPath)
		mountOpt := fmt.Sprintf("%s:%s", hostPath, vol.ContainerPath)
		if vol.ReadOnly {
			mountOpt += MountOptionReadOnly
		}
		args = append(args, "-v", mountOpt)
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

	// Run in WSL
	output, err = b.runInWSL(ctx, distro, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w, output: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))

	env := &Environment{
		ID:           containerID[:12],
		Name:         opts.Name,
		Mode:         ModeWSL,
		Image:        image,
		Status:       StatusRunning,
		SSHHost:      "localhost",
		SSHPort:      sshPort,
		SSHUser:      "root",
		GPUWorkerURL: gpuWorkerURL,
		CreatedAt:    time.Now(),
		Labels: map[string]string{
			"wsl.distro": distro,
		},
	}

	return env, nil
}

func (b *WSLBackend) Start(ctx context.Context, envID string) error {
	distro, err := b.GetDistro(ctx)
	if err != nil {
		return err
	}

	_, err = b.runInWSL(ctx, distro, "docker", "start", envID)
	return err
}

func (b *WSLBackend) Stop(ctx context.Context, envID string) error {
	distro, err := b.GetDistro(ctx)
	if err != nil {
		return err
	}

	_, err = b.runInWSL(ctx, distro, "docker", "stop", envID)
	return err
}

func (b *WSLBackend) Remove(ctx context.Context, envID string) error {
	distro, err := b.GetDistro(ctx)
	if err != nil {
		return err
	}

	_ = b.Stop(ctx, envID)
	_, err = b.runInWSL(ctx, distro, "docker", "rm", "-f", envID)
	return err
}

func (b *WSLBackend) List(ctx context.Context) ([]*Environment, error) {
	distro, err := b.GetDistro(ctx)
	if err != nil {
		return nil, err
	}

	output, err := b.runInWSL(ctx, distro, "docker", "ps", "-a",
		"--filter", "label=ggo.mode=wsl",
		"--format", "{{json .}}")
	if err != nil {
		return nil, err
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
		if container.State == "running" {
			status = StatusRunning
		}

		env := &Environment{
			ID:      container.ID,
			Name:    name,
			Mode:    ModeWSL,
			Image:   container.Image,
			Status:  status,
			SSHHost: "localhost",
			SSHPort: sshPort,
			SSHUser: "root",
			Labels: map[string]string{
				"wsl.distro": distro,
			},
		}

		envs = append(envs, env)
	}

	return envs, nil
}

func (b *WSLBackend) Get(ctx context.Context, idOrName string) (*Environment, error) {
	envs, err := b.List(ctx)
	if err != nil {
		return nil, err
	}

	for _, env := range envs {
		if env.ID == idOrName || env.Name == idOrName {
			return env, nil
		}
	}

	return nil, fmt.Errorf("environment not found: %s", idOrName)
}

func (b *WSLBackend) Exec(ctx context.Context, envID string, cmd []string) ([]byte, error) {
	distro, err := b.GetDistro(ctx)
	if err != nil {
		return nil, err
	}

	args := []string{"docker", "exec", envID}
	args = append(args, cmd...)

	return b.runInWSL(ctx, distro, args...)
}

func (b *WSLBackend) Logs(ctx context.Context, envID string, follow bool) (<-chan string, error) {
	distro, err := b.GetDistro(ctx)
	if err != nil {
		return nil, err
	}

	args := []string{"-d", distro, "--", "docker", "logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, envID)

	cmd := exec.CommandContext(ctx, "wsl", args...)
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

// windowsToWSLPath converts a Windows path to a WSL path
func (b *WSLBackend) windowsToWSLPath(windowsPath string) string {
	// Convert C:\Users\... to /mnt/c/Users/...
	if len(windowsPath) >= 2 && windowsPath[1] == ':' {
		drive := strings.ToLower(string(windowsPath[0]))
		rest := strings.ReplaceAll(windowsPath[2:], "\\", "/")
		return fmt.Sprintf("/mnt/%s%s", drive, rest)
	}
	return windowsPath
}

// GetWSLIP returns the IP address of the WSL instance
func (b *WSLBackend) GetWSLIP(ctx context.Context) (string, error) {
	distro, err := b.GetDistro(ctx)
	if err != nil {
		return "", err
	}

	output, err := b.runInWSL(ctx, distro, "hostname", "-I")
	if err != nil {
		return "", err
	}

	ips := strings.Fields(string(output))
	if len(ips) > 0 {
		return ips[0], nil
	}

	return "localhost", nil
}

// EnsureSSHServer ensures SSH server is running in the container
func (b *WSLBackend) EnsureSSHServer(ctx context.Context, envID string) error {
	setupScript := `
#!/bin/bash
set -e

# Install SSH if not present
if ! command -v sshd &> /dev/null; then
    apt-get update && apt-get install -y openssh-server
fi

# Configure SSH
mkdir -p /var/run/sshd
sed -i 's/#PermitRootLogin prohibit-password/PermitRootLogin yes/' /etc/ssh/sshd_config
sed -i 's/PermitRootLogin prohibit-password/PermitRootLogin yes/' /etc/ssh/sshd_config

# Start SSH
/usr/sbin/sshd

echo "SSH server started"
`

	_, err := b.Exec(ctx, envID, []string{"bash", "-c", setupScript})
	return err
}

var _ Backend = (*WSLBackend)(nil)

func init() {
	_ = os.PathSeparator
	_ = filepath.Join
}
