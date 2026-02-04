// Package studio provides one-click AI development environment management
// across various container and VM platforms (WSL, Colima, AppleContainer, K8S, Docker).
package studio

import (
	"context"
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
)

// Platform constants for runtime.GOOS comparisons
const (
	OSDarwin  = "darwin"
	OSLinux   = "linux"
	OSWindows = "windows"
)

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
