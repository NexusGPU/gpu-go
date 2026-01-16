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

func (b *WSLBackend) IsAvailable(ctx context.Context) bool {
	if runtime.GOOS != "windows" {
		return false
	}

	// Check if WSL is available
	cmd := exec.CommandContext(ctx, "wsl", "--status")
	return cmd.Run() == nil
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
		return nil, fmt.Errorf("Docker not available in WSL: %s", string(output))
	}

	// Create container using Docker in WSL
	containerName := fmt.Sprintf("ggo-%s", opts.Name)

	// Build docker run command
	args := []string{"docker", "run", "-d", "--name", containerName}

	// Add labels
	args = append(args, "--label", "ggo.managed=true")
	args = append(args, "--label", fmt.Sprintf("ggo.name=%s", opts.Name))
	args = append(args, "--label", "ggo.mode=wsl")

	// Find available SSH port
	sshPort := findAvailablePort(2222)
	args = append(args, "-p", fmt.Sprintf("%d:22", sshPort))

	// Add environment variables
	if opts.GPUWorkerURL != "" {
		args = append(args, "-e", fmt.Sprintf("GPU_GO_CONNECTION_URL=%s", opts.GPUWorkerURL))
		args = append(args, "-e", "CUDA_VISIBLE_DEVICES=0")
	}

	for k, v := range opts.Envs {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	// Add volume mounts (convert Windows paths to WSL paths)
	for _, vol := range opts.Volumes {
		hostPath := b.windowsToWSLPath(vol.HostPath)
		mountOpt := fmt.Sprintf("%s:%s", hostPath, vol.ContainerPath)
		if vol.ReadOnly {
			mountOpt += ":ro"
		}
		args = append(args, "-v", mountOpt)
	}

	// Add image
	image := opts.Image
	if image == "" {
		image = "tensorfusion/studio-torch:latest"
	}
	args = append(args, image)

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
		GPUWorkerURL: opts.GPUWorkerURL,
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
