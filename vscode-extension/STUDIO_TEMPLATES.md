# Studio Image Templates

The VSCode extension now includes pre-configured image templates for common AI/ML frameworks, making it easier to create studio environments without memorizing Docker image names.

## Available Templates

### 🔥 PyTorch
- **PyTorch (Official)** - `pytorch/pytorch:latest`
  - Features: PyTorch, CUDA, Python 3, NumPy, SciPy
  - Default ports: 8888 (Jupyter), 6006 (TensorBoard)

- **PyTorch + Jupyter** - `jupyter/pytorch-notebook:latest`
  - Features: PyTorch, Jupyter Lab, CUDA, Python 3
  - Default ports: 8888, 6006
  - Auto-configured with JupyterLab enabled

- **TensorFusion PyTorch** - `tensorfusion/studio-torch:latest`
  - Features: PyTorch, CUDA, SSH, Jupyter, TensorBoard, VS Code Server
  - Default ports: 8888, 6006
  - Optimized for remote development

### 🧠 TensorFlow
- **TensorFlow GPU** - `tensorflow/tensorflow:latest-gpu`
  - Features: TensorFlow, CUDA, Python 3, Keras
  - Default ports: 8888, 6006

- **TensorFlow + Jupyter** - `jupyter/tensorflow-notebook:latest`
  - Features: TensorFlow, Jupyter Lab, CUDA, Python 3
  - Default ports: 8888, 6006
  - Auto-configured with JupyterLab

- **TensorFusion TensorFlow** - `tensorfusion/studio-tensorflow:latest`
  - Features: TensorFlow, CUDA, SSH, Jupyter, TensorBoard
  - Default ports: 8888, 6006

### 📊 Jupyter / Data Science
- **Jupyter SciPy** - `jupyter/scipy-notebook:latest`
  - Features: Jupyter Lab, NumPy, Pandas, Matplotlib, SciPy, scikit-learn
  - Default port: 8888
  - Perfect for data analysis and scientific computing

- **Jupyter Data Science** - `jupyter/datascience-notebook:latest`
  - Features: Jupyter Lab, Python, R, Julia, Pandas, scikit-learn
  - Default port: 8888
  - Multi-language support

- **Jupyter All-Spark** - `jupyter/all-spark-notebook:latest`
  - Features: Jupyter Lab, Python, R, Apache Spark, PySpark
  - Default ports: 8888, 4040 (Spark UI)
  - For big data processing

### 📈 RStudio
- **RStudio** - `rocker/rstudio:latest`
  - Features: RStudio Server, R, tidyverse
  - Default port: 8787
  - Default credentials: user=rstudio, pass=rstudio
  - Access at: http://localhost:8787

- **RStudio Verse** - `rocker/verse:latest`
  - Features: RStudio Server, R, tidyverse, LaTeX, Pandoc
  - Default port: 8787
  - Includes publishing tools

- **RStudio ML** - `rocker/ml:latest`
  - Features: RStudio Server, R, tidyverse, keras, tensorflow
  - Default port: 8787
  - Machine learning packages included

### 🐍 General Purpose
- **Miniconda** - `continuumio/miniconda3:latest`
  - Features: Python 3, conda, pip
  - Minimal conda installation
  - Build your own environment

- **Anaconda** - `continuumio/anaconda3:latest`
  - Features: Python 3, conda, NumPy, Pandas, Matplotlib, scikit-learn
  - Full distribution with 250+ packages

- **Python 3.11** - `python:3.11-slim`
  - Features: Python 3.11, pip
  - Minimal Python environment

- **Ubuntu + CUDA** - `nvidia/cuda:12.2.0-devel-ubuntu22.04`
  - Features: Ubuntu 22.04, CUDA 12.2, cuDNN, Development Tools
  - For custom CUDA development

- **TensorFusion Full Stack** - `tensorfusion/studio-full:latest`
  - Features: PyTorch, TensorFlow, Jupyter, SSH, VS Code Server, CUDA
  - Complete environment with all frameworks

### 🔧 Custom Image
- Enter your own Docker image name
- Useful for custom or proprietary images
- Supports any public or private Docker registry

## Usage in VSCode Extension

1. Open GPU Go sidebar
2. Click "Create Studio Environment" (+ icon)
3. Enter a name for your environment
4. Select a template from the dropdown
5. (Optional) Configure GPU worker URL for remote GPU
6. (Optional) Expand "Advanced Options" to customize:
   - Port mappings (defaults are pre-filled)
   - Volume mounts (e.g., `~/projects:/workspace`)
   - Environment variables
7. Click "Create Studio"

## Automatic Configuration

When you select a template, the extension automatically:
- **Pre-fills port mappings** - Common ports for Jupyter (8888), TensorBoard (6006), RStudio (8787)
- **Sets environment variables** - Like `JUPYTER_ENABLE_LAB=yes` for Jupyter
- **Shows features** - Visual display of included tools and libraries
- **Provides guidance** - Information about accessing the studio after creation

## After Studio Creation

Once your studio is created:

1. **SSH Connection**
   - VS Code Remote-SSH: Connect to host `ggo-<your-studio-name>`
   - Terminal: `ggo studio ssh <your-studio-name>`

2. **Web Access**
   - Jupyter Lab: http://localhost:8888
   - TensorBoard: http://localhost:6006
   - RStudio: http://localhost:8787 (user: rstudio, pass: rstudio)
   - Spark UI: http://localhost:4040

3. **Management Commands**
   ```bash
   ggo studio list           # List all studios
   ggo studio start <name>   # Start a stopped studio
   ggo studio stop <name>    # Stop a running studio
   ggo studio rm <name>      # Remove a studio
   ggo studio logs <name>    # View studio logs
   ```

## Template Features Explained

### Default Ports
- **8888**: Jupyter Notebook/Lab
- **6006**: TensorBoard
- **8787**: RStudio Server
- **4040**: Apache Spark UI

### SSH Configuration
All studios automatically configure SSH for VS Code Remote development:
- Host: `ggo-<studio-name>`
- Port: 2222 (or auto-assigned)
- User: root
- Added to `~/.ssh/config` automatically

### Volume Mounts
Recommended volume mounts:
- `~/projects:/workspace` - Your project files
- `~/datasets:/data` - Training datasets
- `~/models:/models` - Saved models

### Environment Variables
Common environment variables:
- `JUPYTER_ENABLE_LAB=yes` - Enable JupyterLab
- `PASSWORD=rstudio` - RStudio password
- `GPU_GO_CONNECTION_URL=...` - Remote GPU worker URL
- `CUDA_VISIBLE_DEVICES=0` - GPU device selection

## Adding Custom Templates

To add your own templates, edit `src/config/studioTemplates.ts`:

```typescript
{
    id: 'my-custom-template',
    name: 'My Custom Environment',
    image: 'myorg/my-image:latest',
    description: 'Description of your environment',
    category: 'general',
    features: ['Feature1', 'Feature2'],
    defaultPorts: ['8080:8080'],
    defaultEnv: {
        'MY_VAR': 'value'
    },
    icon: '🚀'
}
```

## Examples

### Example 1: PyTorch Development with Project Volume
```
Template: PyTorch + Jupyter
Name: pytorch-dev
Ports: 8888:8888, 6006:6006 (pre-filled)
Volumes: ~/my-ml-project:/workspace
```

### Example 2: RStudio for Data Analysis
```
Template: RStudio Verse
Name: rstudio-analysis
Ports: 8787:8787 (pre-filled)
Volumes: ~/datasets:/data
Access: http://localhost:8787 (user: rstudio, pass: rstudio)
```

### Example 3: Jupyter Data Science with Multiple Volumes
```
Template: Jupyter Data Science
Name: data-science
Ports: 8888:8888 (pre-filled)
Volumes: ~/notebooks:/home/jovyan/work, ~/datasets:/data:ro
Environment: JUPYTER_ENABLE_LAB=yes (pre-set)
```

### Example 4: Custom TensorFlow Container
```
Template: Custom Image
Name: custom-tf
Image: tensorflow/tensorflow:2.15.0-gpu-jupyter
Ports: 8888:8888, 6006:6006
Volumes: ~/tf-projects:/tf
```

## Tips

1. **Leave ports empty** to use template defaults
2. **Mount your workspace** for persistent code
3. **Use read-only volumes** (`:ro`) for datasets
4. **Check template info** before creating to see included features
5. **GPU Worker URL is optional** - leave empty for local testing
6. **Multiple studios** can run simultaneously with different ports

## Troubleshooting

**Port already in use?**
- Change the host port: Instead of `8888:8888`, use `8889:8888`

**Volume mount permission issues?**
- Use full paths: `~/projects` or `/Users/you/projects`
- Check directory exists before mounting

**RStudio won't connect?**
- Default password is `rstudio`
- Access at `http://localhost:8787`
- Wait 10-15 seconds after creation for server to start

**Jupyter token required?**
- Check logs: `ggo studio logs <name>`
- Look for token in output
- Or set password with env var: `JUPYTER_TOKEN=your-token`
