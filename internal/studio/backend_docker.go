package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"
)

// SSH port range constants
const (
	SSHPortRangeMin = 12000
	SSHPortRangeMax = 18000
)

// Environment variable names
const (
	EnvLDPreload     = "LD_PRELOAD"
	EnvLDLibraryPath = "LD_LIBRARY_PATH"
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

// SocketPath implements BackendSocketPath. Returns the effective Docker socket path.
func (b *DockerBackend) SocketPath(ctx context.Context) string {
	if b.dockerHost != "" {
		return b.dockerHost
	}
	if v := os.Getenv("DOCKER_HOST"); v != "" {
		return v
	}
	return "unix:///var/run/docker.sock"
}

// GetHostArch returns the architecture of the Docker host
// Returns "amd64", "arm64", or empty string if detection fails
func (b *DockerBackend) GetHostArch(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, b.dockerCmd, "info", "--format", "{{.Architecture}}")
	b.setDockerEnv(cmd)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	arch := strings.TrimSpace(string(output))
	return NormalizeArch(arch)
}

// pullImageWithProgress pulls a Docker image with progress output to stderr
// If platform is specified, it will use --platform flag
func (b *DockerBackend) pullImageWithProgress(ctx context.Context, image, platform string) error {
	// Check if image already exists locally with the correct platform
	imageExistsLocally := false
	checkCmd := exec.CommandContext(ctx, b.dockerCmd, "image", "inspect", image)
	b.setDockerEnv(checkCmd)
	if err := checkCmd.Run(); err == nil {
		imageExistsLocally = true
		if platform == "" {
			klog.V(2).Infof("Image %s already exists locally, skipping pull", image)
			return nil
		}
		// Image exists and platform is specified — check if it already matches
		inspectCmd := exec.CommandContext(ctx, b.dockerCmd, "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", image)
		b.setDockerEnv(inspectCmd)
		if out, inspectErr := inspectCmd.Output(); inspectErr == nil {
			localPlatform := strings.TrimSpace(string(out))
			if localPlatform == platform {
				klog.V(2).Infof("Image %s already exists locally with matching platform %s, skipping pull", image, platform)
				return nil
			}
			klog.V(2).Infof("Image %s exists locally as %s but need %s, will pull", image, localPlatform, platform)
		}
	}

	// Image doesn't exist or platform doesn't match, pull it with progress
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

	pullCmd := exec.CommandContext(ctx, b.dockerCmd, pullArgs...)
	b.setDockerEnv(pullCmd)
	// Stream output to stderr so user can see progress
	pullCmd.Stdout = os.Stderr
	pullCmd.Stderr = os.Stderr

	if err := pullCmd.Run(); err != nil {
		if imageExistsLocally {
			// Pull failed but image exists locally (e.g. local-only custom image
			// with a different platform) — use the local image as-is
			klog.V(2).Infof("Pull failed but image %s exists locally, using local image", image)
			fmt.Fprintf(os.Stderr, "   Pull failed, using local image %s\n\n", image)
			return nil
		}
		return fmt.Errorf("failed to pull image %s: %w", image, err)
	}

	fmt.Fprintf(os.Stderr, "\n   Image pulled successfully!\n\n")
	return nil
}

func (b *DockerBackend) Create(ctx context.Context, opts *CreateOptions) (*Environment, error) {
	// Determine the platform to use
	// If user specified platform, use it; otherwise detect from host
	platform := opts.Platform
	if platform == "" {
		// Auto-detect host architecture
		hostArch := b.GetHostArch(ctx)
		if hostArch != "" {
			platform = "linux/" + hostArch
			fmt.Fprintf(os.Stderr, "   Auto-detected host architecture: %s\n", platform)
		}
	}

	// Generate container name with random suffix to avoid conflicts
	containerName := GenerateContainerName(opts.Name)

	// Build docker run command
	args := []string{"run", "-d", "--name", containerName}

	// Add --init flag to use tini init process
	// This prevents zombie processes and allows proper signal handling
	// Required for SSH daemon to properly fork child processes
	args = append(args, "--init")

	// On macOS, disable any built-in image healthcheck.
	// Under Rosetta (amd64 on ARM Mac) healthcheck processes run slowly and
	// accumulate, overwhelming the service they probe (e.g. Jupyter's port 8888).
	if IsDarwin() {
		args = append(args, "--no-healthcheck")
	}

	// Add security options for SSH to work in containers
	// SSH's privilege separation requires certain capabilities that Docker's
	// default seccomp profile blocks, causing "mm_request_receive: bad msg_len" errors
	// See: https://github.com/moby/moby/issues/42866
	args = append(args, "--cap-add", "AUDIT_WRITE")
	// Also add SYS_ADMIN to allow unshare for isolating /etc/ld.so.preload in SSH processes
	// This prevents TensorFusion GPU libraries from interfering with SSH privilege separation
	args = append(args, "--cap-add", "SYS_ADMIN")

	// Add platform flag if specified
	if platform != "" {
		args = append(args, "--platform", platform)
	}

	// Add local GPU passthrough if requested
	if opts.UseLocalGPU {
		args = append(args, "--gpus", "all")
	}

	// Add labels
	args = append(args, "--label", "ggo.managed=true")
	args = append(args, "--label", fmt.Sprintf("ggo.name=%s", opts.Name))
	args = append(args, "--label", "ggo.mode=docker")

	// Add port mappings and find SSH port
	ports := resolvePortMappings(opts.Ports, opts.Image)
	sshPort, err := addPortMappings(&args, ports)
	if err != nil {
		return nil, err
	}

	// Use endpoint override if specified
	gpuWorkerURL := opts.GPUWorkerURL
	if opts.Endpoint != "" {
		gpuWorkerURL = opts.Endpoint
	}

	// On macOS with OrbStack, containers can typically reach LAN IPs directly.
	// No URL rewriting or relay is needed — the GPU worker URL is passed as-is.

	// Setup container GPU environment using common abstraction
	// This downloads GPU client libraries and sets up env vars, volumes
	setupConfig := &ContainerSetupConfig{
		StudioName:     opts.Name,
		GPUWorkerURL:   gpuWorkerURL,
		HardwareVendor: opts.HardwareVendor,
		Platform:       opts.Platform,
		MountUserHome:  !opts.NoUserVolume,
	}

	setupResult, err := SetupContainerGPUEnv(ctx, setupConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to setup container environment: %w", err)
	}

	// Merge setup env vars with user env vars (user takes precedence)
	mergedEnvs := MergeEnvVars(setupResult.EnvVars, opts.Envs)
	for k, v := range mergedEnvs {
		// Don't pass LD_PRELOAD/LD_LIBRARY_PATH as container env vars - they'll be written to /etc/environment
		// This prevents them from being inherited by system daemons like sshd
		if k == EnvLDPreload || k == EnvLDLibraryPath {
			continue
		}
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

	// Add working directory
	if opts.WorkDir != "" {
		args = append(args, "-w", opts.WorkDir)
	}

	// Add image
	image := opts.Image
	if image == "" {
		image = DefaultImageStudioTorch
	}
	args = append(args, image)

	klog.V(2).Infof("Running docker command: %s %v", b.dockerCmd, args)

	// Pull image if not already cached locally
	if err := b.pullImageWithProgress(ctx, image, platform); err != nil {
		return nil, fmt.Errorf("failed to pull image: %w", err)
	}

	// Check if image has a default CMD or ENTRYPOINT
	// Only use "sleep infinity" if image has no CMD and user provided no command
	cmdToUse := opts.Command
	if len(cmdToUse) == 0 {
		hasDefaultCmd := ImageHasDefaultCommand(ctx, b.dockerCmd, image)
		if !hasDefaultCmd {
			cmdToUse = []string{"sleep", "infinity"}
			klog.V(2).Infof("Image has no default CMD/ENTRYPOINT, using sleep infinity")
		} else {
			klog.V(2).Infof("Image has default CMD/ENTRYPOINT, using it")
		}
	}
	if len(cmdToUse) > 0 {
		if formattedCmd := FormatContainerCommand(cmdToUse); len(formattedCmd) > 0 {
			args = append(args, formattedCmd...)
		}
	}

	// Run container
	cmd := exec.CommandContext(ctx, b.dockerCmd, args...)
	b.setDockerEnv(cmd)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w, output: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))

	// Automatically install and configure SSH in the container
	// This allows any Docker image to be used, not just images with SSH pre-installed
	klog.Infof("Configuring SSH in container %s...", containerID[:12])
	fmt.Fprintf(os.Stderr, "\n   Configuring SSH server...\n")

	if err := setupSSHInContainer(ctx, b.dockerCmd, containerID, opts.SSHPublicKey, b.dockerHost, mergedEnvs); err != nil {
		// SSH setup failed, clean up container
		klog.Errorf("Failed to configure SSH, removing container: %v", err)
		_ = b.Remove(ctx, containerID)
		return nil, fmt.Errorf("failed to configure SSH in container: %w", err)
	}

	fmt.Fprintf(os.Stderr, "   SSH server configured successfully!\n\n")

	// Get container info
	// Use container name without ggo- prefix to match List() behavior
	// This ensures SSH config uses the full name with suffix
	env := &Environment{
		ID:           containerID[:12],
		Name:         strings.TrimPrefix(containerName, "ggo-"), // e.g., "andy-studio-0086"
		Mode:         ModeDocker,
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

		// Parse ports
		ports := parsePortMappings(container.Ports)

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
			Ports:   ports,
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
	sshPort := 0
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
	for part := range strings.SplitSeq(ports, ",") {
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
	return 0
}

// parsePortMappings parses Docker port mappings into "hostPort:containerPort" format
// Input format: "0.0.0.0:8888->8888/tcp, [::]:8888->8888/tcp, 0.0.0.0:6006->6006/tcp"
// Output format: ["8888:8888", "6006:6006"] (deduplicated)
func parsePortMappings(ports string) []string {
	seen := make(map[string]bool)
	var result []string

	for part := range strings.SplitSeq(ports, ",") {
		part = strings.TrimSpace(part)
		if !strings.Contains(part, "->") {
			continue
		}

		// Split by "->" to get host and container parts
		// e.g., "0.0.0.0:8888" -> "8888/tcp"
		parts := strings.Split(part, "->")
		if len(parts) != 2 {
			continue
		}

		// Extract host port from "0.0.0.0:8888" or "[::]:8888"
		hostPart := parts[0]
		colonIdx := strings.LastIndex(hostPart, ":")
		if colonIdx < 0 {
			continue
		}
		hostPort := hostPart[colonIdx+1:]

		// Extract container port from "8888/tcp" or "8888/udp"
		containerPart := parts[1]
		slashIdx := strings.Index(containerPart, "/")
		if slashIdx < 0 {
			continue
		}
		containerPort := containerPart[:slashIdx]

		mapping := hostPort + ":" + containerPort
		if !seen[mapping] {
			seen[mapping] = true
			result = append(result, mapping)
		}
	}
	return result
}

// isPortAvailable checks if a port is available on localhost
func isPortAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false // Port is in use or unavailable
	}
	_ = listener.Close()
	return true
}

// addPortMappings adds port mappings to docker run args and returns SSH port
// Returns the SSH port (either user-specified or auto-generated), or error if ports are occupied
func addPortMappings(args *[]string, ports []PortMapping) (int, error) {
	var occupiedPorts []int
	sshPort := 0

	// Check all requested ports and add mappings
	for _, port := range ports {
		// Check if host port is available
		if !isPortAvailable(port.HostPort) {
			occupiedPorts = append(occupiedPorts, port.HostPort)
		}

		protocol := port.Protocol
		if protocol == "" {
			protocol = DefaultProtocolTCP
		}
		*args = append(*args, "-p", fmt.Sprintf("%d:%d/%s", port.HostPort, port.ContainerPort, protocol))
		if port.ContainerPort == 22 {
			sshPort = port.HostPort
		}
	}

	// If any ports are occupied, return error
	if len(occupiedPorts) > 0 {
		portList := make([]string, len(occupiedPorts))
		for i, p := range occupiedPorts {
			portList[i] = fmt.Sprintf("%d", p)
		}
		return 0, fmt.Errorf("port(s) already in use: %s. Please choose different ports in the 'Port Mappings' field", strings.Join(portList, ", "))
	}

	// Add default SSH port if not specified
	if sshPort == 0 {
		sshPort = findAvailablePort(0)
		*args = append(*args, "-p", fmt.Sprintf("%d:22/tcp", sshPort))
	}

	return sshPort, nil
}

// findAvailablePort finds an available port in the SSH port range (12000-18000)
// The start parameter is ignored; a random port in the range is selected
func findAvailablePort(_ int) int {
	// Try to find an available port in the range
	for i := 0; i < 100; i++ {
		// Generate random port in range [SSHPortRangeMin, SSHPortRangeMax]
		port := SSHPortRangeMin + rand.Intn(SSHPortRangeMax-SSHPortRangeMin+1)

		// Check if port is available by trying to listen on it
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			_ = listener.Close()
			return port
		}
	}

	// Fallback: return a random port in range without checking availability
	return SSHPortRangeMin + rand.Intn(SSHPortRangeMax-SSHPortRangeMin+1)
}

// EnsureSSHServer ensures SSH server is running in the container
func (b *DockerBackend) EnsureSSHServer(ctx context.Context, envID string) error {
	setupScript := `
#!/bin/bash
set -e

# Install SSH if not present
if ! command -v sshd &> /dev/null; then
    if command -v apt-get &> /dev/null; then
        apt-get update -qq && apt-get install -y -qq openssh-server
    elif command -v yum &> /dev/null; then
        yum install -y -q openssh-server
    elif command -v apk &> /dev/null; then
        apk add --no-cache -q openssh-server
    fi
fi

# Configure SSH directories
mkdir -p /var/run/sshd /root/.ssh
chmod 700 /root/.ssh
chmod 755 /var/run/sshd

# Set root password (required for password auth)
echo "root:gpugo" | chpasswd

# Generate SSH host keys if they don't exist
if [ ! -f /etc/ssh/ssh_host_rsa_key ]; then
    ssh-keygen -A
fi

# Configure sshd - modify main config file for compatibility
# Remove or comment out conflicting settings
sed -i 's/^PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config
sed -i 's/^#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config
sed -i 's/^PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config
sed -i 's/^#PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config
sed -i 's/^PubkeyAuthentication.*/PubkeyAuthentication yes/' /etc/ssh/sshd_config
sed -i 's/^#PubkeyAuthentication.*/PubkeyAuthentication yes/' /etc/ssh/sshd_config
sed -i 's/^UsePAM.*/UsePAM no/' /etc/ssh/sshd_config
sed -i 's/^#UsePAM.*/UsePAM no/' /etc/ssh/sshd_config

# Append our settings if not already present
grep -q "^Port 22" /etc/ssh/sshd_config || echo "Port 22" >> /etc/ssh/sshd_config
grep -q "^ListenAddress 0.0.0.0" /etc/ssh/sshd_config || echo "ListenAddress 0.0.0.0" >> /etc/ssh/sshd_config

# Kill existing sshd processes
pkill sshd 2>/dev/null || true
sleep 1

# Start SSH server
/usr/sbin/sshd

# Verify SSH is running
sleep 2
if pgrep sshd > /dev/null; then
    echo "SSH server started successfully"
else
    echo "ERROR: SSH server failed to start"
    exit 1
fi
`

	output, err := b.Exec(ctx, envID, []string{"bash", "-c", setupScript})
	if err != nil {
		klog.Warningf("SSH setup output: %s", string(output))
		return err
	}
	klog.V(2).Infof("SSH server setup completed: %s", string(output))
	return nil
}

var _ Backend = (*DockerBackend)(nil)

// resolvePortMappings merges user-specified ports with auto-detected default ports
// for the given image. If the user already mapped a well-known port, it is not overridden.
func resolvePortMappings(userPorts []PortMapping, image string) []PortMapping {
	ports := userPorts
	if hasContainerPort(ports, 8888) {
		return ports
	}
	for _, dp := range getDefaultPortsForImage(image) {
		if hasContainerPort(ports, dp.ContainerPort) {
			continue
		}
		hostPort := dp.HostPort
		if !isPortAvailable(hostPort) {
			hostPort = findAvailablePort(0)
			klog.V(2).Infof("Default port %d in use, using %d for container port %d", dp.HostPort, hostPort, dp.ContainerPort)
		}
		ports = append(ports, PortMapping{HostPort: hostPort, ContainerPort: dp.ContainerPort})
	}
	return ports
}

// getDefaultPortsForImage returns default port mappings based on the image name.
// This auto-exposes well-known service ports (Jupyter, TensorBoard, etc.) so users
// don't have to specify -p flags for common images.
func getDefaultPortsForImage(image string) []PortMapping {
	img := strings.ToLower(image)
	var ports []PortMapping

	// Jupyter-based images
	if strings.Contains(img, "jupyter") || strings.Contains(img, "notebook") {
		ports = append(ports, PortMapping{HostPort: 8888, ContainerPort: 8888})
	}

	// PyTorch/TensorFlow images often have TensorBoard + Jupyter
	if strings.Contains(img, "tensorflow") || strings.Contains(img, "torch") || strings.Contains(img, "pytorch") {
		if !hasContainerPort(ports, 8888) {
			ports = append(ports, PortMapping{HostPort: 8888, ContainerPort: 8888})
		}
		ports = append(ports, PortMapping{HostPort: 6006, ContainerPort: 6006})
	}

	// TensorFusion studio images
	if strings.Contains(img, "tensorfusion") || strings.Contains(img, "studio") {
		if !hasContainerPort(ports, 8888) {
			ports = append(ports, PortMapping{HostPort: 8888, ContainerPort: 8888})
		}
		if !hasContainerPort(ports, 6006) {
			ports = append(ports, PortMapping{HostPort: 6006, ContainerPort: 6006})
		}
	}

	return ports
}

// hasContainerPort checks if a container port is already in the port mappings
func hasContainerPort(ports []PortMapping, containerPort int) bool {
	for _, p := range ports {
		if p.ContainerPort == containerPort {
			return true
		}
	}
	return false
}

func init() {
	_ = bytes.Buffer{} // silence import
}
