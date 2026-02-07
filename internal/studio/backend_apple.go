package studio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/NexusGPU/gpu-go/internal/errors"
	"github.com/NexusGPU/gpu-go/internal/platform"
)

const appleContainerInstallHint = "Apple Container is not installed. Download the signed installer package from https://github.com/apple/container/releases"

// AppleContainerBackend implements the Backend interface using Apple native containers.
type AppleContainerBackend struct {
	containerCmd string
}

// NewAppleContainerBackend creates a new Apple container backend.
func NewAppleContainerBackend() *AppleContainerBackend {
	return &AppleContainerBackend{
		containerCmd: "container",
	}
}

func (b *AppleContainerBackend) Name() string {
	return "apple-container"
}

func (b *AppleContainerBackend) Mode() Mode {
	return ModeAppleContainer
}

func (b *AppleContainerBackend) IsAvailable(ctx context.Context) bool {
	if !b.isSupportedOS() {
		return false
	}
	if _, err := exec.LookPath(b.containerCmd); err != nil {
		return false
	}

	cmd := exec.CommandContext(ctx, b.containerCmd, "system", "status")
	return cmd.Run() == nil
}

func (b *AppleContainerBackend) IsInstalled(ctx context.Context) bool {
	if !b.isSupportedOS() {
		return false
	}
	_, err := exec.LookPath(b.containerCmd)
	return err == nil
}

func (b *AppleContainerBackend) EnsureRunning(ctx context.Context) error {
	if !b.isSupportedOS() {
		return errors.Unavailable("Apple Container requires macOS 26 or newer. Please upgrade your macOS version.")
	}
	if !b.IsInstalled(ctx) {
		return errors.Unavailable(appleContainerInstallHint)
	}
	if b.IsAvailable(ctx) {
		return nil
	}

	fmt.Fprintf(os.Stderr, "\n   Starting Apple Container services...\n")
	cmd := exec.CommandContext(ctx, b.containerCmd, "system", "start")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start Apple Container services: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\n   Apple Container services started!\n\n")
	return nil
}

func (b *AppleContainerBackend) Create(ctx context.Context, opts *CreateOptions) (*Environment, error) {
	if err := b.EnsureRunning(ctx); err != nil {
		return nil, err
	}

	// Generate container name with random suffix to avoid conflicts
	containerName := GenerateContainerName(opts.Name)

	// Build container run command
	args := []string{"run", "--detach", "--name", containerName}

	// Add platform if specified
	if opts.Platform != "" {
		args = append(args, "--platform", opts.Platform)
	}

	// Add labels
	args = append(args, "--label", "ggo.managed=true")
	args = append(args, "--label", fmt.Sprintf("ggo.name=%s", opts.Name))
	args = append(args, "--label", fmt.Sprintf("ggo.mode=%s", b.Mode()))

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
	setupConfig := &ContainerSetupConfig{
		StudioName:     opts.Name,
		GPUWorkerURL:   gpuWorkerURL,
		HardwareVendor: opts.HardwareVendor,
		MountUserHome:  !opts.NoUserVolume,
		SkipFileMounts: true,
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
		cpus := int64(math.Ceil(opts.Resources.CPUs))
		if cpus < 1 {
			cpus = 1
		}
		args = append(args, "--cpus", strconv.FormatInt(cpus, 10))
	}

	// Set default memory to 1/4 of system memory if not specified
	memoryLimit := opts.Resources.Memory
	if memoryLimit == "" {
		memGB, err := getSystemMemoryGB()
		if err == nil && memGB > 0 {
			memAllocated := (memGB + 3) / 4
			if memAllocated < 1 {
				memAllocated = 1
			}
			memoryLimit = fmt.Sprintf("%dG", memAllocated)
		}
	}
	memoryLimit = normalizeContainerMemory(memoryLimit)
	if memoryLimit != "" {
		args = append(args, "--memory", memoryLimit)
	}

	// Add working directory
	if opts.WorkDir != "" {
		args = append(args, "--workdir", opts.WorkDir)
	}

	// Add image
	image := opts.Image
	if image == "" {
		image = DefaultImageStudioTorch
	}
	args = append(args, image)

	// Add command args (supplements ENTRYPOINT or overrides CMD)
	if formattedCmd := FormatContainerCommand(opts.Command); len(formattedCmd) > 0 {
		args = append(args, formattedCmd...)
	}

	cmd := exec.CommandContext(ctx, b.containerCmd, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create container: %w, output: %s", err, string(output))
	}

	containerID := strings.TrimSpace(string(output))

	env := &Environment{
		ID:           containerID,
		Name:         opts.Name,
		Mode:         ModeAppleContainer,
		Image:        image,
		Status:       StatusRunning,
		SSHHost:      DefaultHostLocalhost,
		SSHPort:      sshPort,
		SSHUser:      "root",
		GPUWorkerURL: gpuWorkerURL,
		CreatedAt:    time.Now(),
		Labels:       opts.Labels,
	}

	return env, nil
}

func (b *AppleContainerBackend) Start(ctx context.Context, envID string) error {
	cmd := exec.CommandContext(ctx, b.containerCmd, "start", envID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to start container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *AppleContainerBackend) Stop(ctx context.Context, envID string) error {
	cmd := exec.CommandContext(ctx, b.containerCmd, "stop", envID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stop container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *AppleContainerBackend) Remove(ctx context.Context, envID string) error {
	cmd := exec.CommandContext(ctx, b.containerCmd, "delete", "--force", envID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove container: %w, output: %s", err, string(output))
	}
	return nil
}

func (b *AppleContainerBackend) List(ctx context.Context) ([]*Environment, error) {
	cmd := exec.CommandContext(ctx, b.containerCmd, "list", "--all", "--format", "json")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	return parseAppleContainerList(output)
}

func (b *AppleContainerBackend) Get(ctx context.Context, idOrName string) (*Environment, error) {
	cmd := exec.CommandContext(ctx, b.containerCmd, "inspect", idOrName)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("environment not found: %s", idOrName)
	}

	var snapshots []appleContainerSnapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return nil, fmt.Errorf("failed to parse container info: %w", err)
	}
	if len(snapshots) == 0 {
		return nil, fmt.Errorf("environment not found: %s", idOrName)
	}

	env, ok := appleContainerToEnvironment(snapshots[0])
	if !ok {
		return nil, errors.NotFound("environment", idOrName)
	}

	return env, nil
}

func (b *AppleContainerBackend) Exec(ctx context.Context, envID string, cmd []string) ([]byte, error) {
	args := append([]string{"exec", envID}, cmd...)
	execCmd := exec.CommandContext(ctx, b.containerCmd, args...)
	return execCmd.CombinedOutput()
}

func (b *AppleContainerBackend) Logs(ctx context.Context, envID string, follow bool) (<-chan string, error) {
	args := []string{"logs"}
	if follow {
		args = append(args, "--follow")
	}
	args = append(args, envID)

	cmd := exec.CommandContext(ctx, b.containerCmd, args...)
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

func (b *AppleContainerBackend) isSupportedOS() bool {
	if runtime.GOOS != OSDarwin {
		return false
	}
	major := platform.MacOSMajorVersion()
	return major >= 26
}

type appleContainerSnapshot struct {
	Status        string                  `json:"status"`
	Configuration appleContainerConfig    `json:"configuration"`
	Networks      []appleContainerNetwork `json:"networks"`
}

type appleContainerConfig struct {
	ID             string                        `json:"id"`
	Image          appleContainerImage           `json:"image"`
	Labels         map[string]string             `json:"labels"`
	InitProcess    appleContainerProcess         `json:"initProcess"`
	PublishedPorts []appleContainerPublishedPort `json:"publishedPorts"`
}

type appleContainerImage struct {
	Reference string `json:"reference"`
}

type appleContainerProcess struct {
	Environment []string `json:"environment"`
}

type appleContainerPublishedPort struct {
	HostPort      uint16 `json:"hostPort"`
	ContainerPort uint16 `json:"containerPort"`
	Proto         string `json:"proto"`
	Count         uint16 `json:"count"`
}

type appleContainerNetwork struct {
	IPv4Address string `json:"ipv4Address"`
}

func parseAppleContainerList(output []byte) ([]*Environment, error) {
	var snapshots []appleContainerSnapshot
	if err := json.Unmarshal(output, &snapshots); err != nil {
		return nil, fmt.Errorf("failed to parse container list: %w", err)
	}

	var envs []*Environment
	for _, snapshot := range snapshots {
		env, ok := appleContainerToEnvironment(snapshot)
		if !ok {
			continue
		}
		envs = append(envs, env)
	}

	return envs, nil
}

func appleContainerToEnvironment(snapshot appleContainerSnapshot) (*Environment, bool) {
	labels := snapshot.Configuration.Labels
	if labels["ggo.managed"] != "true" {
		return nil, false
	}
	if modeLabel, ok := labels["ggo.mode"]; ok && modeLabel != string(ModeAppleContainer) {
		if modeLabel != "apple-container" {
			return nil, false
		}
	}

	name := strings.TrimPrefix(snapshot.Configuration.ID, "ggo-")
	if labelName := labels["ggo.name"]; labelName != "" {
		name = labelName
	}

	sshPort := extractSSHPort(snapshot.Configuration.PublishedPorts)
	gpuWorkerURL := extractGPUWorkerURL(snapshot.Configuration.InitProcess.Environment)

	var status EnvironmentStatus
	switch snapshot.Status {
	case "running":
		status = StatusRunning
	case "stopped":
		status = StatusStopped
	case "stopping":
		status = StatusPending
	default:
		status = StatusPending
	}

	env := &Environment{
		ID:           snapshot.Configuration.ID,
		Name:         name,
		Mode:         ModeAppleContainer,
		Image:        snapshot.Configuration.Image.Reference,
		Status:       status,
		SSHHost:      DefaultHostLocalhost,
		SSHPort:      sshPort,
		SSHUser:      "root",
		GPUWorkerURL: gpuWorkerURL,
		Labels:       labels,
	}

	return env, true
}

func extractGPUWorkerURL(envs []string) string {
	prefixes := []string{
		"TENSOR_FUSION_OPERATOR_CONNECTION_INFO=",
		"GPU_GO_CONNECTION_URL=",
		"GPU_WORKER_URL=",
	}
	for _, env := range envs {
		for _, prefix := range prefixes {
			if strings.HasPrefix(env, prefix) {
				return strings.TrimPrefix(env, prefix)
			}
		}
	}
	return ""
}

func extractSSHPort(published []appleContainerPublishedPort) int {
	for _, port := range published {
		if port.Proto != "" && !strings.EqualFold(port.Proto, DefaultProtocolTCP) {
			continue
		}
		count := port.Count
		if count == 0 {
			count = 1
		}
		for offset := uint16(0); offset < count; offset++ {
			if port.ContainerPort+offset == 22 {
				return int(port.HostPort + offset)
			}
		}
	}
	return 0
}

func normalizeContainerMemory(memory string) string {
	if memory == "" {
		return ""
	}
	replacements := map[string]string{
		"Ki": "K",
		"Mi": "M",
		"Gi": "G",
		"Ti": "T",
		"Pi": "P",
		"ki": "K",
		"mi": "M",
		"gi": "G",
		"ti": "T",
		"pi": "P",
	}
	for oldSuffix, newSuffix := range replacements {
		if strings.HasSuffix(memory, oldSuffix) {
			return strings.TrimSuffix(memory, oldSuffix) + newSuffix
		}
	}
	return memory
}

var _ Backend = (*AppleContainerBackend)(nil)
var _ AutoStartableBackend = (*AppleContainerBackend)(nil)

func init() {
	_ = bytes.Buffer{} // silence import
}
