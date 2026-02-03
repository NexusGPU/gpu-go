<p align="center"><a href="https://tensor-fusion.ai" target="_blank" rel="noreferrer"><img width="100%" src="https://cdn.tensor-fusion.ai/logo-banner.png" alt="Logo"></a></p>

# GPU Go VSCode Extension

[![VS Code Marketplace](https://img.shields.io/visual-studio-marketplace/v/nexusgpu.gpu-go?style=flat-square&label=VS%20Code%20Marketplace&logo=visual-studio-code)](https://marketplace.visualstudio.com/items?itemName=nexusgpu.gpu-go)
[![Installs](https://img.shields.io/visual-studio-marketplace/i/nexusgpu.gpu-go?style=flat-square)](https://marketplace.visualstudio.com/items?itemName=nexusgpu.gpu-go)
[![Rating](https://img.shields.io/visual-studio-marketplace/r/nexusgpu.gpu-go?style=flat-square)](https://marketplace.visualstudio.com/items?itemName=nexusgpu.gpu-go)

> **Use GPU Like NFS** - Seamlessly manage AI/ML development environments with remote GPU access directly from VS Code.

This extension is part of the [TensorFusion](https://github.com/NexusGPU/tensor-fusion) ecosystem, allowing you to manage your GPU resources and development environments directly from your IDE.

## 🚀 Quick Start

### Step 1: Register & Setup Agent
Before using the VS Code extension, you need a TensorFusion account and a running GPU agent.

1.  Go to the [TensorFusion Dashboard](https://tensor-fusion.ai/auth/login?callbackUrl=%2Fdashboard).
2.  Register for an account.
3.  Follow the dashboard instructions to install and start the **GPUGo Agent** on your GPU machine (Server).

### Step 2: Install Extension
Install this extension from the VS Code Marketplace.

### Step 3: Login
1.  Click on the **GPU Go** icon in the Activity Bar.
2.  Click **"Login to GPU Go"**.
3.  Go to the [Dashboard](https://tensor-fusion.ai/auth/login?callbackUrl=%2Fdashboard) to generate a **Personal Access Token (PAT)**.
4.  Paste the token into the VS Code prompt.

### Step 4: Create Studio
Once logged in, you can create a new **Studio** environment:
1.  Click the **"Create Studio"** button or use the command palette (`GPU Go: Create Studio Environment`).
2.  Select your preferred environment (e.g., PyTorch, TensorFlow).
3.  The extension will automatically set up the local container and connect it to your remote GPU.

## ✨ Features

### 🖥️ Studio Environments
- **One-Click Creation**: Spin up GPU-powered environments instantly.
- **SSH Integration**: Connect directly from VS Code terminal.
- **Pre-configured Images**: PyTorch, TensorFlow, Jupyter, and more.
- **Automatic SSH Config**: No manual configuration needed.

### 🔧 GPU Workers & Devices
- View and manage GPU workers across your infrastructure.
- Monitor active connections and device utilization in real-time.
- View GPU specifications (model, VRAM, driver version).

## 🔗 Links

- 📖 [Documentation](https://docs.tensor-fusion.ai)
- 🐛 [Report Issues](https://github.com/NexusGPU/gpu-go/issues)
- 💬 [Discord Community](https://discord.gg/2bybv9yQNk)
- 🌐 [Website](https://tensor-fusion.ai)

## 📄 License

Proprietary - Copyright © 2026 NexusGPU PTE. LTD. All rights reserved.
See [LICENSE](../LICENSE) for details.
