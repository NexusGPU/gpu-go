// Package studio provides one-click AI development environment management
// across various container and VM platforms (WSL, Colima, AppleContainer, K8S, Docker).
package studio

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"
)

// Mode represents the container/VM runtime mode
type Mode string

const (
	ModeWSL            Mode = "wsl"    // Windows Subsystem for Linux
	ModeColima         Mode = "colima" // Colima (macOS/Linux)
	ModeAppleContainer Mode = "apple"  // Apple Virtualization Framework
	ModeDocker         Mode = "docker" // Native Docker
	ModeKubernetes     Mode = "k8s"    // Kubernetes (kind, minikube, etc.)
	ModeAuto           Mode = "auto"   // Auto-detect best option
)

// StudioImage represents a pre-configured AI development image
type StudioImage struct {
	Name        string            `json:"name"`
	Tag         string            `json:"tag"`
	Description string            `json:"description"`
	Features    []string          `json:"features"` // torch, cuda, ssh, jupyter, etc.
	Size        string            `json:"size"`
	Registry    string            `json:"registry"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// DefaultImages returns the list of available studio images
func DefaultImages() []StudioImage {
	return []StudioImage{
		{
			Name:        "tensorfusion/studio-base",
			Tag:         "latest",
			Description: "Basic AI development environment with Python, CUDA support",
			Features:    []string{"python", "cuda", "ssh"},
			Registry:    "docker.io",
		},
		{
			Name:        "tensorfusion/studio-torch",
			Tag:         "latest",
			Description: "PyTorch environment with CUDA support",
			Features:    []string{"python", "cuda", "ssh", "torch", "jupyter"},
			Registry:    "docker.io",
		},
		{
			Name:        "tensorfusion/studio-tensorflow",
			Tag:         "latest",
			Description: "TensorFlow environment with CUDA support",
			Features:    []string{"python", "cuda", "ssh", "tensorflow", "jupyter"},
			Registry:    "docker.io",
		},
		{
			Name:        "tensorfusion/studio-full",
			Tag:         "latest",
			Description: "Full AI development environment with PyTorch, TensorFlow, and tools",
			Features:    []string{"python", "cuda", "ssh", "torch", "tensorflow", "jupyter", "vscode-server"},
			Registry:    "docker.io",
		},
	}
}

// Environment represents a running studio environment
type Environment struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Mode          Mode              `json:"mode"`
	Image         string            `json:"image"`
	Status        EnvironmentStatus `json:"status"`
	ConnectionURL string            `json:"connection_url,omitempty"`
	SSHHost       string            `json:"ssh_host,omitempty"`
	SSHPort       int               `json:"ssh_port,omitempty"`
	SSHUser       string            `json:"ssh_user,omitempty"`
	WorkDir       string            `json:"work_dir,omitempty"`
	GPUWorkerURL  string            `json:"gpu_worker_url,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// EnvironmentStatus represents the status of an environment
type EnvironmentStatus string

const (
	StatusPending    EnvironmentStatus = "pending"
	StatusPulling    EnvironmentStatus = "pulling"
	StatusStarting   EnvironmentStatus = "starting"
	StatusRunning    EnvironmentStatus = "running"
	StatusStopped    EnvironmentStatus = "stopped"
	StatusError      EnvironmentStatus = "error"
	StatusTerminated EnvironmentStatus = "terminated"
	StatusDeleted    EnvironmentStatus = "deleted"
	StatusUnknown    EnvironmentStatus = "unknown"
)

// Platform constants for runtime.GOOS comparisons
const (
	OSDarwin  = "darwin"
	OSLinux   = "linux"
	OSWindows = "windows"
)

// Architecture constants for CPU architecture detection
const (
	ArchX86_64  = "x86_64"
	ArchAarch64 = "aarch64"
	ArchAmd64   = "amd64"
	ArchArm64   = "arm64"
)

// NormalizeArch normalizes architecture names to Docker/OCI format (amd64, arm64)
func NormalizeArch(arch string) string {
	switch arch {
	case ArchX86_64:
		return ArchAmd64
	case ArchAarch64:
		return ArchArm64
	default:
		return arch
	}
}

// Docker-related constants
const (
	DefaultProtocolTCP      = "tcp"
	MountOptionReadOnly     = ":ro"
	DefaultImageStudioTorch = "tensorfusion/studio-torch:latest"
	DockerStateRunning      = "running"
	DockerStateExited       = "exited"
	DockerStateCreated      = "created"
	DefaultHostLocalhost    = "localhost"
)

// CreateOptions contains options for creating an environment
type CreateOptions struct {
	Name           string            `json:"name"`
	Mode           Mode              `json:"mode"`
	Image          string            `json:"image"`
	GPUWorkerURL   string            `json:"gpu_worker_url,omitempty"`  // TENSOR_FUSION_OPERATOR_CONNECTION_INFO
	HardwareVendor string            `json:"hardware_vendor,omitempty"` // nvidia, amd, hygon
	SSHPublicKey   string            `json:"ssh_public_key,omitempty"`
	WorkDir        string            `json:"work_dir,omitempty"`
	Ports          []PortMapping     `json:"ports,omitempty"`
	Envs           map[string]string `json:"envs,omitempty"`
	Volumes        []VolumeMount     `json:"volumes,omitempty"`
	Resources      ResourceSpec      `json:"resources,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	// NoUserVolume disables automatic mounting of user's home directory
	NoUserVolume bool `json:"no_user_volume,omitempty"`
	// Command specifies the container startup command or supplements ENTRYPOINT args
	Command []string `json:"command,omitempty"`
	// Endpoint overrides the GPU worker endpoint URL
	Endpoint string `json:"endpoint,omitempty"`
	// Platform specifies the container image platform (e.g., linux/amd64, linux/arm64)
	// If empty, auto-detect from VM/host architecture
	Platform string `json:"platform,omitempty"`
}

// PortMapping represents a port mapping
type PortMapping struct {
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol,omitempty"` // tcp, udp
}

// VolumeMount represents a volume mount
type VolumeMount struct {
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
	ReadOnly      bool   `json:"read_only,omitempty"`
}

// ResourceSpec represents resource limits
type ResourceSpec struct {
	CPUs   float64 `json:"cpus,omitempty"`
	Memory string  `json:"memory,omitempty"` // e.g., "8Gi"
}

// Backend is the interface for container/VM runtime backends
type Backend interface {
	// Name returns the backend name
	Name() string

	// Mode returns the mode this backend handles
	Mode() Mode

	// IsAvailable checks if this backend is available on the system
	IsAvailable(ctx context.Context) bool

	// Create creates a new environment
	Create(ctx context.Context, opts *CreateOptions) (*Environment, error)

	// Start starts a stopped environment
	Start(ctx context.Context, envID string) error

	// Stop stops a running environment
	Stop(ctx context.Context, envID string) error

	// Remove removes an environment
	Remove(ctx context.Context, envID string) error

	// List lists all environments
	List(ctx context.Context) ([]*Environment, error)

	// Get gets an environment by ID or name
	Get(ctx context.Context, idOrName string) (*Environment, error)

	// Exec executes a command in an environment
	Exec(ctx context.Context, envID string, cmd []string) ([]byte, error)

	// Logs returns logs from an environment
	Logs(ctx context.Context, envID string, follow bool) (<-chan string, error)
}

// AutoStartableBackend is an optional interface for backends that can be auto-started.
// Backends implementing this interface can be selected even if not currently running,
// as long as they are installed and can be started automatically.
type AutoStartableBackend interface {
	Backend
	// IsInstalled checks if the backend software is installed (but not necessarily running)
	IsInstalled(ctx context.Context) bool
	// EnsureRunning ensures the backend is running, starting it if necessary
	EnsureRunning(ctx context.Context) error
}

// SSHConfig represents an SSH configuration entry
type SSHConfig struct {
	Host         string
	HostName     string
	User         string
	Port         int
	IdentityFile string
	ExtraOptions map[string]string
}

// GenerateSSHConfigEntry generates an SSH config entry string
func (c *SSHConfig) GenerateSSHConfigEntry() string {
	entry := "Host " + c.Host + "\n"
	entry += "    HostName " + c.HostName + "\n"
	entry += "    User " + c.User + "\n"
	if c.Port != 0 && c.Port != 22 {
		entry += "    Port " + string(rune(c.Port)) + "\n"
	}
	if c.IdentityFile != "" {
		entry += "    IdentityFile " + c.IdentityFile + "\n"
	}
	for k, v := range c.ExtraOptions {
		entry += "    " + k + " " + v + "\n"
	}
	return entry
}

// GenerateContainerName generates a unique container name with random suffix
func GenerateContainerName(name string) string {
	suffix := generateRandomSuffix(4)
	return "ggo-" + name + "-" + suffix
}

// generateRandomSuffix generates a random hex string of specified length
func generateRandomSuffix(length int) string {
	bytes := make([]byte, (length+1)/2)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based suffix if random fails
		return hex.EncodeToString([]byte{byte(time.Now().UnixNano() & 0xFF)})[:length]
	}
	return hex.EncodeToString(bytes)[:length]
}

// extractLabelValue extracts a label value from docker ps Labels format (comma-separated key=value pairs)
func extractLabelValue(labels, key string) string {
	for _, label := range strings.Split(labels, ",") {
		parts := strings.SplitN(strings.TrimSpace(label), "=", 2)
		if len(parts) == 2 && parts[0] == key {
			return parts[1]
		}
	}
	return ""
}

// FormatContainerCommand formats the command slice for docker/container execution.
// If a single command string is passed that looks like a shell command (contains spaces
// or shell metacharacters), it wraps with "sh -c" to ensure proper shell interpretation.
// This handles cases like: -c "sh -c 'sleep 1d'" or -c "sleep 1d && echo done"
func FormatContainerCommand(command []string) []string {
	if len(command) == 0 {
		return nil
	}

	// If multiple arguments, pass as-is (user specified proper arguments)
	if len(command) > 1 {
		return command
	}

	// Single argument - check if it needs shell interpretation
	cmd := command[0]

	// If the single command contains shell metacharacters, wrap with sh -c
	// This handles: "sh -c 'sleep 1d'", "sleep 1d && echo done", etc.
	shellMetaChars := []string{" ", ";", "&", "|", ">", "<", "$", "`", "(", ")", "{", "}", "'", "\""}
	needsShell := false
	for _, meta := range shellMetaChars {
		if strings.Contains(cmd, meta) {
			needsShell = true
			break
		}
	}

	if needsShell {
		return []string{"sh", "-c", cmd}
	}

	// Simple command without shell metacharacters, pass as-is
	return command
}
