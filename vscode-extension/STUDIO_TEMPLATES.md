# Studio Image Templates

The VS Code extension ships with a minimal image template list for studio creation.

## Available Templates

### 🔥 PyTorch
- **PyTorch + Jupyter CUDA 12** - `quay.io/jupyter/pytorch-notebook:cuda12-python-3.11.8`
  - Features: PyTorch, Jupyter Lab, CUDA 12, Python 3.11.8
  - Default port: 8888
  - Auto-configured with JupyterLab enabled

### 🔧 Custom Image
- Enter your own Docker image name
- Useful for internal, private, or locally-built images
- Supports any public or private registry

## Usage in VS Code Extension

1. Open the GPUGo sidebar.
2. Click `Create Studio Environment`.
3. Enter a name for your environment.
4. Select the official PyTorch image, or `Custom Image`.
5. Enter a remote GPU share link.
6. Optionally adjust ports, volumes, or environment variables.
7. Click `Create Studio`.

## Automatic Configuration

When you select an official PyTorch template, the extension automatically:

- Pre-fills port `8888:8888`
- Sets `JUPYTER_ENABLE_LAB=yes`
- Sets an empty `JUPYTER_TOKEN`
- Shows the full image name and included features

## After Studio Creation

- Jupyter Lab: `http://localhost:8888`
- VS Code Remote-SSH host: `ggo-<your-studio-name>`

## Custom Template Example

```text
Template: Custom Image
Name: my-custom-studio
Image: ghcr.io/my-org/my-image:latest
Ports: 8888:8888
```
