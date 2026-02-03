# GPU Go Studio 使用指南

本文档介绍 `ggo use` 和 `ggo studio create` 命令的使用方法和最佳实践。

## 概览

GPU Go 提供两种使用远程 GPU 的方式：

1. **`ggo use`** - 在当前环境（Linux/macOS）直接使用远程 GPU
2. **`ggo studio create`** - 创建容器化的 AI 开发环境并连接远程 GPU

## `ggo use` 命令

### 基本用法

```bash
# 使用短链接或短代码
ggo use abc123

# 使用完整链接
ggo use https://go.gpu.tf/s/abc123

# 直接激活，无需确认 (-y)
ggo use abc123 -y

# 长期配置（添加到 shell 配置文件）
ggo use abc123 --long-term
```

### 环境变量设置

`ggo use` 会自动配置以下环境变量：

| 环境变量 | 说明 |
|---------|------|
| `TENSOR_FUSION_OPERATOR_CONNECTION_INFO` | 远程 GPU 连接信息（必需） |
| `TF_LOG_LEVEL` | 日志级别，默认 `info` |
| `TF_ENABLE_LOG` | 启用日志，默认 `1` |
| `TF_LOG_PATH` | 日志输出路径 |

### Linux 库配置

在 Linux 环境下，`ggo use` 会自动创建：

- `/etc/ld.so.conf.d/zz_tensor-fusion.conf` - 配置 LD_LIBRARY_PATH
- `/etc/ld.so.preload` - 配置预加载库

根据 GPU 厂商类型：
- **NVIDIA**: 预加载 `libcuda.so`, `libnvidia-ml.so`
- **AMD/Hygon**: 预加载 `libamdhip64.so`

### Python/venv 环境支持

对于 Python 虚拟环境，建议使用长期配置：

```bash
# 设置长期配置
ggo use abc123 --long-term -y

# 在 venv 中也会自动生效
python -m venv myenv
source myenv/bin/activate
# GPU 环境变量已自动可用
```

### 清理

```bash
# 清理当前配置
ggo clean

# 清理所有配置
ggo clean --all
```

## `ggo studio create` 命令

### 基本用法

```bash
# 创建 studio 环境（需要 share link）
ggo studio create my-studio -s abc123

# 使用完整链接
ggo studio create my-studio -s https://go.gpu.tf/s/abc123

# 指定镜像
ggo studio create my-studio -s abc123 --image tensorfusion/studio-torch:latest

# 选择运行模式
ggo studio create my-studio -s abc123 --mode docker
```

### 支持的模式

| 模式 | 说明 | 平台 |
|------|------|------|
| `auto` | 自动检测最佳后端 | 所有 |
| `docker` | 原生 Docker | 所有 |
| `colima` | Colima 容器运行时 | macOS/Linux |
| `wsl` | Windows Subsystem for Linux | Windows |
| `apple` | Apple Virtualization Framework | macOS |

### 卷挂载（Volume Mounts）

**最佳实践**：使用 `-v` 挂载用户数据目录，防止 studio 重建时数据丢失。

```bash
# 挂载项目目录（推荐）
ggo studio create my-studio -s abc123 -v ~/projects:/workspace

# 挂载数据目录
ggo studio create my-studio -s abc123 -v ~/data:/data

# 只读挂载
ggo studio create my-studio -s abc123 -v ~/config:/config:ro

# 多个挂载
ggo studio create my-studio -s abc123 \
  -v ~/projects:/workspace \
  -v ~/data:/data \
  -v ~/models:/models
```

### 自动挂载

Studio 会自动挂载以下目录：

| Host 路径 | 容器路径 | 说明 |
|----------|---------|------|
| `~/.gpugo/cache` | `/opt/gpugo/cache` | GPU 库文件（只读） |
| `~/.gpugo/studio/{name}/logs` | `/var/log/tensor-fusion` | 日志目录 |
| `~/.gpugo/studio/{name}/config/ld.so.conf.d/zz_tensor-fusion.conf` | `/etc/ld.so.conf.d/zz_tensor-fusion.conf` | LD 配置 |
| `~/.gpugo/studio/{name}/config/ld.so.preload` | `/etc/ld.so.preload` | 预加载配置 |

### 端口映射

```bash
# 映射 Jupyter 端口
ggo studio create my-studio -s abc123 -p 8888:8888

# 映射多个端口
ggo studio create my-studio -s abc123 -p 8888:8888 -p 6006:6006
```

### 环境变量

```bash
# 设置环境变量
ggo studio create my-studio -s abc123 -e MY_VAR=value

# 多个环境变量
ggo studio create my-studio -s abc123 -e KEY1=val1 -e KEY2=val2
```

### 资源限制

```bash
# 限制 CPU
ggo studio create my-studio -s abc123 --cpus 4

# 限制内存
ggo studio create my-studio -s abc123 --memory 8Gi

# 同时限制
ggo studio create my-studio -s abc123 --cpus 4 --memory 8Gi
```

### Studio 管理

```bash
# 列出所有 studio
ggo studio list

# 查看 studio 详情
ggo studio logs my-studio

# 停止 studio
ggo studio stop my-studio

# 启动 studio
ggo studio start my-studio

# 删除 studio
ggo studio rm my-studio

# 强制删除
ggo studio rm my-studio -f
```

### SSH 连接

Studio 创建后会自动配置 SSH：

```bash
# 直接 SSH 连接
ssh ggo-my-studio

# VS Code Remote 连接
# 1. 安装 Remote - SSH 扩展
# 2. F1 → Remote-SSH: Connect to Host...
# 3. 选择 ggo-my-studio
```

## 最佳实践

### 1. 数据持久化

**重要**：Studio 容器重建后内部数据会丢失，务必挂载重要数据目录：

```bash
# 推荐的挂载结构
ggo studio create ml-dev -s abc123 \
  -v ~/workspace:/workspace \      # 代码目录
  -v ~/datasets:/datasets:ro \     # 数据集（只读）
  -v ~/models:/models \            # 模型输出
  -v ~/checkpoints:/checkpoints    # 训练检查点
```

### 2. 开发环境配置

```bash
# 完整的开发环境配置示例
ggo studio create ml-dev -s abc123 \
  --image tensorfusion/studio-full:latest \
  -v ~/projects:/workspace \
  -p 8888:8888 \      # Jupyter
  -p 6006:6006 \      # TensorBoard
  --cpus 8 \
  --memory 16Gi
```

### 3. 多项目隔离

为不同项目创建独立的 studio：

```bash
# 项目 A
ggo studio create project-a -s abc123 \
  -v ~/projects/project-a:/workspace

# 项目 B
ggo studio create project-b -s def456 \
  -v ~/projects/project-b:/workspace
```

### 4. 日志查看

```bash
# 查看实时日志
ggo studio logs my-studio -f

# 查看 TensorFusion 日志
cat ~/.gpugo/studio/my-studio/logs/*.log
```

## 故障排除

### GPU 库未找到

确保先下载 GPU 依赖：

```bash
ggo deps sync
ggo deps download
```

### 容器启动失败

检查 Docker/Colima 是否运行：

```bash
# 查看可用后端
ggo studio backends

# 检查 Docker
docker info
```

### 环境变量未生效

重新 source 配置文件：

```bash
source ~/.gpugo/studio/current-os/config/env.sh
```

### 清理旧配置

```bash
ggo clean --all
rm -rf ~/.gpugo/studio/current-os
```

## 文件结构

```
~/.gpugo/
├── cache/                    # GPU 库缓存
│   ├── libcuda.so
│   ├── libnvidia-ml.so
│   └── tensor-fusion-worker
├── config/                   # 全局配置
│   ├── config.json
│   └── deps-manifest.json
└── studio/                   # Studio 配置
    ├── current-os/           # ggo use 配置
    │   ├── config/
    │   │   ├── env.sh
    │   │   ├── ld.so.conf.d/
    │   │   │   └── zz_tensor-fusion.conf
    │   │   └── ld.so.preload
    │   └── logs/
    └── my-studio/            # Studio 配置
        ├── config/
        │   ├── ld.so.conf.d/
        │   │   └── zz_tensor-fusion.conf
        │   └── ld.so.preload
        └── logs/
```
