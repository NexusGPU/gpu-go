# Dependency Management

The `ggo deps` command manages vGPU library dependencies required for GPU Go to function. This includes the remote-gpu-worker binary, accelerator libraries, and other artifacts.

## Architecture

### Manifest Files

The dependency system uses three manifest files stored in `~/.gpugo/config/`:

| File | Purpose |
|------|---------|
| `releases-manifest.json` | Global releases from API (synced via `deps sync`) |
| `deps-manifest.json` | Required dependencies for current environment |
| `downloaded-manifest.json` | Tracks what has been downloaded |

### Library Types

| Type | Description |
|------|-------------|
| `vgpu-library` | Accelerator libraries (libaccel_*.so) |
| `remote-gpu-worker` | Worker binary for GPU server |
| `remote-gpu-client` | Client library for remote GPU access |

## Commands

### `ggo deps sync`

Fetches latest releases from the API and updates the local release manifest.

```bash
ggo deps sync                  # Sync for current platform
ggo deps sync --os linux       # Sync for specific OS
ggo deps sync -v               # Verbose output
```

### `ggo deps list`

Lists available and installed dependencies with their status.

```bash
ggo deps list                  # List for current platform
ggo deps list --os linux       # Filter by OS
ggo deps list -o json          # JSON output
```

Status meanings:
- **Installed**: Required and downloaded with correct version
- **Downloaded**: Downloaded but not in deps-manifest
- **Available**: In release manifest but not downloaded
- **Update: X → Y**: New version available
- **Missing**: In deps-manifest but file not found

### `ggo deps update`

Syncs releases, updates deps-manifest, and downloads required dependencies.

```bash
ggo deps update                # Interactive mode (prompts for confirmation)
ggo deps update -y             # Auto-confirm and download
```

The update flow:
1. Syncs release-manifest from API
2. Selects latest version of each library type
3. Updates deps-manifest
4. Shows diff between deps-manifest and downloaded-manifest
5. Prompts for download confirmation (unless `-y` flag)
6. Downloads missing/outdated dependencies

### `ggo deps download`

Downloads all dependencies in deps-manifest that need downloading.

```bash
ggo deps download              # Download all pending
ggo deps download --name lib   # Download specific library
ggo deps download -f           # Force re-download
```

### `ggo deps install`

Downloads and marks libraries as required (adds to deps-manifest).

```bash
ggo deps install               # Install all available for platform
ggo deps install libcuda.so.1  # Install specific library
```

### `ggo deps clean`

Removes all cached downloads.

```bash
ggo deps clean
```

## Auto-sync Behavior

The release manifest auto-syncs when:
- Running `deps update`
- Manifest is older than 7 days
- Manifest doesn't exist and a library is needed

## On-demand Download

When code needs a library (e.g., remote-gpu-worker), it will:
1. Check deps-manifest for the library type
2. If not found, sync releases and create deps-manifest
3. Check if file exists with correct version
4. Download if missing or outdated

This ensures libraries are always available when needed.

## Example Workflow

```bash
# Initial setup - sync and download dependencies
ggo deps update -y

# Check what's installed
ggo deps list

# Later - check for updates
ggo deps update

# Clean and re-download if issues
ggo deps clean
ggo deps update -y
```

## Configuration

| Environment Variable | Description |
|---------------------|-------------|
| `GPU_GO_ENDPOINT` | API base URL (default: https://go.gpu.tf) |

| Flag | Description |
|------|-------------|
| `--api` | Override API base URL |
| `--cdn` | Override CDN base URL |
| `-o, --output` | Output format (table, json) |
