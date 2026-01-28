# GPU Go - Remote GPU Management for VS Code

[![VS Code Marketplace](https://img.shields.io/visual-studio-marketplace/v/nexusgpu.gpu-go?style=flat-square&label=VS%20Code%20Marketplace&logo=visual-studio-code)](https://marketplace.visualstudio.com/items?itemName=nexusgpu.gpu-go)
[![Installs](https://img.shields.io/visual-studio-marketplace/i/nexusgpu.gpu-go?style=flat-square)](https://marketplace.visualstudio.com/items?itemName=nexusgpu.gpu-go)
[![Rating](https://img.shields.io/visual-studio-marketplace/r/nexusgpu.gpu-go?style=flat-square)](https://marketplace.visualstudio.com/items?itemName=nexusgpu.gpu-go)

> **Use GPU Like NFS** - Seamlessly manage AI/ML development environments with remote GPU access directly from VS Code.

---

## ✨ Features

### 🖥️ Studio Environments

Create and manage AI development studio environments with one-click remote GPU access:

- **One-Click Creation** - Spin up GPU-powered environments instantly
- **SSH Integration** - Connect directly from VS Code terminal
- **Pre-configured Images** - PyTorch, TensorFlow, Jupyter, and more
- **Automatic SSH Config** - No manual configuration needed

### 🔧 GPU Workers

View and manage GPU workers across your infrastructure:

- List all available workers and their status
- Monitor active connections in real-time
- Create new workers via CLI or web dashboard
- Quick access to worker details and logs

### 📊 Device Management

Comprehensive view of all your GPU devices:

- View GPU specifications (model, VRAM, driver version)
- Monitor device availability and utilization
- Quick access to detailed device information
- Multi-GPU support

---

## 🚀 Getting Started

### Step 1: Install the Extension

Install from the [VS Code Marketplace](https://marketplace.visualstudio.com/items?itemName=nexusgpu.gpu-go) or search for "GPU Go" in VS Code Extensions.

### Step 2: Login

1. Click on the **GPU Go** icon in the Activity Bar
2. Click **"Login to GPU Go"**
3. Generate a Personal Access Token (PAT) from the [dashboard](https://go.tensor-fusion.ai)
4. Paste the token in VS Code

### Step 3: Setup Your GPU Server

On your GPU server, install and start the ggo agent:

```bash
# Install ggo CLI
curl -fsSL https://cdn.tensor-fusion.ai/gpugo/install.sh | sh

# Login to your account
ggo login

# Start the agent
ggo agent start
```

### Step 4: Create a Worker

Create a worker to share GPU resources:

```bash
ggo worker create --agent-id <your-agent-id> --name my-worker --gpu-ids 0
```

### Step 5: Create a Studio Environment

Use the **"Create Studio"** button in the extension to create a new development environment with remote GPU access.

---

## 📋 Commands

| Command | Description |
|---------|-------------|
| `GPU Go: Login` | Login to GPU Go platform |
| `GPU Go: Logout` | Logout from GPU Go platform |
| `GPU Go: Create Studio Environment` | Create a new studio environment |
| `GPU Go: Refresh Studio` | Refresh studio list |
| `GPU Go: Refresh Workers` | Refresh workers list |
| `GPU Go: Refresh Devices` | Refresh devices list |
| `GPU Go: Open Dashboard` | Open GPU Go web dashboard |

---

## ⚙️ Configuration

| Setting | Default | Description |
|---------|---------|-------------|
| `gpugo.serverUrl` | `https://tensor-fusion.ai` | GPU Go API server URL |
| `gpugo.dashboardUrl` | `https://go.tensor-fusion.ai` | GPU Go dashboard URL |
| `gpugo.cliPath` | *(auto-detected)* | Path to ggo CLI binary |
| `gpugo.autoRefreshInterval` | `30` | Auto-refresh interval in seconds (0 to disable) |
| `gpugo.autoDownloadCli` | `true` | Automatically download CLI on first install |

---

## 📦 Requirements

- **VS Code** 1.85.0 or higher
- **ggo CLI** - Auto-downloaded or install manually
- **Internet connection** for API access

---

## 🔗 Links

- 📖 [Documentation](https://docs.tensor-fusion.ai)
- 🐛 [Report Issues](https://github.com/NexusGPU/gpu-go/issues)
- 💬 [Discord Community](https://discord.gg/tensor-fusion)
- 🌐 [Website](https://tensor-fusion.ai)

---

## 📄 License

Proprietary - Copyright © 2026 NexusGPU PTE. LTD. All rights reserved.

See [LICENSE](LICENSE) for details.
