<p align="center"><a href="https://tensor-fusion.ai" target="_blank" rel="noreferrer"><img width="100%" src="https://cdn.tensor-fusion.ai/logo-banner.png" alt="Logo"></a></p>

<p align="center">
    <br /><strong><a href="https://tensor-fusion.ai" target="_blank">GPUGo (ggo)</a></strong><br/><b>Use Remote GPUs Like They Are Local</b>
    <br />
    <a href="https://tensor-fusion.ai/guide/overview"><strong>Explore the docs »</strong></a>
    <br />
    <a href="https://tensor-fusion.ai/guide/overview">View Demo</a>
    |
    <a href="https://github.com/NexusGPU/tensor-fusion/issues/new?labels=bug&template=bug-report---.md">Report Bug</a>
    |
    <a href="https://github.com/NexusGPU/tensor-fusion/issues/new?labels=enhancement&template=feature-request---.md">Request Feature</a>
</p>

[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/NexusGPU/gpu-go)](https://goreportcard.com/report/github.com/NexusGPU/gpu-go)
[![Release](https://img.shields.io/github/v/release/NexusGPU/gpu-go)](https://github.com/NexusGPU/gpu-go/releases)
[![Discord](https://img.shields.io/discord/1234567890?color=7289da&label=Discord&logo=discord&logoColor=ffffff)](https://discord.gg/2bybv9yQNk)

**GPUGo** is the official CLI and client tool for [TensorFusion GPU Go](https://tensor-fusion.ai/products/gpu-go), designed to transform how you access and manage GPU resources. 

It allows developers to treat remote GPUs (on servers, cloud instances, or colleagues' machines) as if they were attached to their local development environment.

Think of it as **"NFS for GPUs"**: seamless, low-latency, and easy to manage.

## 🌟 Highlights

- **Zero Friction**: Spin up a local-first studio environment with one command and get instant access to remote GPUs.
- **Cost Effective**: Share powerful GPU servers among multiple developers using [TensorFusion](https://github.com/NexusGPU/tensor-fusion)'s virtualization technology.
- **Cross-Platform**: Works on macOS (Apple Silicon or Intel), Windows (WSL), and Linux.
- **Flexible Backends**: Supports Docker, Colima, WSL, and Kubernetes for  for studio environments.
- **VS Code Integration**: Full GUI management via the official extension.

## 🚀 Quick Start

### 1. Register & Get Started

[Register and follow dashboard instructions](https://tensor-fusion.ai/auth/login?callbackUrl=%2Fdashboard) to get your account and access tokens.

### 2. Install GPUGo Agent on GPU Host

Copy the command from dashboard and run. You can see real-time onboarding progress on dashboard.

**Optionally**, you can run in manual way

```bash
curl -fsSL https://cdn.tensor-fusion.ai/gpugo/install.sh | sh
```
```bash
# 1. Register the agent using the token from the Dashboard
ggo agent register -t "<token-from-dashboard>"

# 2. Start agent service
ggo agent start
```

### 4. Client Side: Use a Remote GPU

Create a **Studio** environment locally that is connected to the remote GPU.

```bash
# Login first, copy personal access token (PAT) from dashboard
ggo auth login

# Create a studio environment connected to a remote GPU
ggo studio create my-project -s "https://go.gpu.tf/s/share-code"

# Connect via SSH (automatically configures your ~/.ssh/config)
ggo studio ssh my-project
```

## 🧩 VS Code Extension (Recommended)

Prefer a GUI? The **GPU Go VS Code Extension** provides a beautiful interface to manage your studios, agents, and workers.

- [Open VSX Registry](https://open-vsx.org/extension/nexusgpu/gpu-go)
- [VSCode Marketplace](https://marketplace.visualstudio.com/items?itemName=nexusgpu.gpu-go)

👉 [Check out the VS Code Extension README](./vscode-extension/README.md)

## 💬 Community & Contact

- Discord channel: [https://discord.gg/2bybv9yQNk](https://discord.gg/2bybv9yQNk)
- Discuss anything about TensorFusion & GPUGo: [Github Discussions](https://github.com/NexusGPU/tensor-fusion/discussions)
- Contact us with WeCom for Greater China region: [企业微信](https://work.weixin.qq.com/ca/cawcde42751d9f6a29)
- Email us: [support@tensor-fusion.com](mailto:support@tensor-fusion.com)
- Schedule [1:1 meeting with TensorFusion founders](https://tensor-fusion.ai/book-demo)

## 📄 License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
