# GPU Go (ggo)

A cross-platform command-line tool for managing remote GPU environments, designed to work with the [Tensor Fusion](https://github.com/NexusGPU/tensor-fusion) platform.

## Features

- **Agent Mode**: Run on GPU servers to sync with the cloud platform
- **Worker Management**: Create, list, update, and delete GPU workers
- **Share Links**: Generate shareable links for GPU access
- **Remote Environment**: Set up temporary or long-term remote GPU environments
- **AI Studio**: One-click AI development environments (WSL/Colima/Docker/K8S)
- **Dependency Management**: Download and manage vGPU libraries
- **Cross-Platform**: Windows, macOS, and Linux support

## Installation

### Quick Install (Recommended)

**Linux/macOS:**
```bash
curl -sfL https://get.tensor-fusion.ai | sh -s -
```

**Windows (PowerShell):**
```powershell
iwr -useb https://get.tensor-fusion.ai/install.ps1 | iex
```

### Install with Go

```bash
go install github.com/NexusGPU/gpu-go/cmd/ggo@latest
```

### Build from Source

```bash
git clone https://github.com/NexusGPU/gpu-go
cd gpu-go
go build -o ggo ./cmd/ggo
```

## Quick Start

### Agent Side (GPU Server)

1. Register the agent with your account token:

```bash
# Using environment variable
export GPU_GO_TOKEN="tmp_xxxxxxxxxxxx"
ggo agent register

# Or using flag
ggo agent register --token tmp_xxxxxxxxxxxx
```

2. Start the agent daemon:

```bash
ggo agent start
```

3. Check agent status:

```bash
ggo agent status
```

### Client Side (Manage Workers)

```bash
# Set your user token
export GPU_GO_USER_TOKEN="your_user_token"

# List all workers
ggo worker list

# Create a new worker
ggo worker create --agent-id agent_xxx --name "My Worker" --gpu-ids GPU-0 --port 9001

# Get worker details
ggo worker get worker_xxx

# Update worker
ggo worker update worker_xxx --name "Updated Name" --enabled

# Delete worker
ggo worker delete worker_xxx
```

### Share GPU Workers

```bash
# Create a share link
ggo share create "My Worker"
# or
ggo share create --worker-id worker_xxx --expires-in 24h --max-uses 10

# List all shares
ggo share list

# Delete a share
ggo share delete share_xxx
```

### Use Shared GPU

```bash
# Set up temporary GPU environment
ggo use abc123

# Set up long-term GPU environment
ggo use abc123 --long-term

# Clean up
ggo clean
# or
ggo clean --all
```

### AI Studio (One-Click Development Environment)

```bash
# Create a studio environment with remote GPU
ggo studio create my-env --gpu-url "https://worker:9001"

# Create with specific platform
ggo studio create my-env --mode wsl     # Windows
ggo studio create my-env --mode colima  # macOS
ggo studio create my-env --mode docker  # Linux/Any

# List environments
ggo studio list

# Connect via SSH (adds to VS Code SSH config)
ggo studio ssh my-env

# Stop/Start environments
ggo studio stop my-env
ggo studio start my-env

# Remove environment
ggo studio rm my-env

# List available images
ggo studio images

# List available backends
ggo studio backends
```

### Dependency Management

```bash
# List available vGPU libraries
ggo deps list

# Download and install libraries
ggo deps install

# Check for updates
ggo deps update

# Clean cache
ggo deps clean
```

## Configuration

### Platform-Specific Paths

| Platform | Config Directory | State Directory |
|----------|-----------------|-----------------|
| Linux    | `/etc/gpugo` or `~/.config/gpugo` | `/tmp/tensor-fusion-state` |
| macOS    | `~/Library/Application Support/gpugo` | `/tmp/tensor-fusion-state` |
| Windows  | `%ProgramData%\gpugo` | `%ProgramData%\gpugo\state` |

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `GPU_GO_TOKEN` | Temporary installation token | - |
| `GPU_GO_USER_TOKEN` | User authentication token | - |
| `TENSOR_FUSION_STATE_DIR` | State directory for hypervisor | Platform-specific |
| `GGO_CONFIG_DIR` | Override config directory | Platform-specific |
| `GGO_CACHE_DIR` | Override cache directory | Platform-specific |
| `GPU_GO_MOCK_GPUS` | Enable mock GPU discovery (dev) | - |

## Architecture

```
┌─────────────────┐       ┌─────────────────┐       ┌─────────────────┐
│   GPU Go CLI    │◀─────▶│  GPU Go Backend │◀─────▶│   Dashboard     │
│    (ggo)        │       │   (API Server)  │       │   (Web UI)      │
└────────┬────────┘       └─────────────────┘       └─────────────────┘
         │
         │ File sync
         ▼
┌─────────────────┐
│  Tensor Fusion  │
│   Hypervisor    │
│ (single_node)   │
└─────────────────┘
```

### Project Structure

```
gpu-go/
├── cmd/ggo/                 # CLI commands
│   ├── agent/               # Agent management
│   ├── deps/                # Dependency management
│   ├── share/               # Share link management
│   ├── studio/              # AI studio environments
│   ├── use/                 # Use shared GPU
│   └── worker/              # Worker management
├── internal/
│   ├── agent/               # Agent implementation
│   ├── api/                 # API client
│   ├── config/              # Configuration management
│   ├── deps/                # Dependency management
│   ├── platform/            # Cross-platform utilities
│   └── studio/              # Studio backend implementations
├── .github/workflows/       # CI/CD
├── install.sh               # Unix install script
├── install.ps1              # Windows install script
└── go.mod
```

## Studio Backends

| Backend | Platform | Description |
|---------|----------|-------------|
| `wsl` | Windows | Windows Subsystem for Linux with Docker |
| `colima` | macOS/Linux | Colima container runtime |
| `docker` | All | Native Docker |
| `k8s` | All | Kubernetes (kind, minikube) |
| `auto` | All | Auto-detect best available |

## Development

### Prerequisites

- Go 1.24+
- Access to GPU Go backend API

### Running Tests

```bash
go test ./... -v
```

### Building

```bash
# Current platform
go build -o ggo ./cmd/ggo

# Cross-compile
GOOS=linux GOARCH=amd64 go build -o ggo-linux-amd64 ./cmd/ggo
GOOS=darwin GOARCH=arm64 go build -o ggo-darwin-arm64 ./cmd/ggo
GOOS=windows GOARCH=amd64 go build -o ggo-windows-amd64.exe ./cmd/ggo
```

### Linting

```bash
golangci-lint run
```

## API Reference

See the [API Specification](docs/api.md) for detailed API documentation.

## Integration with Tensor Fusion

GPU Go integrates with Tensor Fusion's hypervisor in `single_node` mode:

1. **Agent syncs configuration** from the backend API to local files
2. **Hypervisor watches** the state directory for worker changes
3. **Workers are automatically** started/stopped based on file state
4. **Agent reports status** back to the backend

### State File Format

Workers are synced to `workers.json` in Tensor Fusion format:

```json
[
  {
    "WorkerUID": "worker_xxx",
    "AllocatedDevices": ["GPU-0"],
    "Status": "Running"
  }
]
```

## Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

Apache License 2.0
