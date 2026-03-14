# Changelog

All notable changes to the "GPUGo" extension will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- Simplified studio image templates in the VS Code extension
  - Keep only `quay.io/jupyter/pytorch-notebook:cuda12-python-3.13.12`
  - Keep only `quay.io/jupyter/pytorch-notebook:cuda13-python-3.13.12`
  - Keep `Custom Image`
- Removed TensorBoard actions and references from the VS Code extension studio flow

## [0.1.11] - 2026-03-12

### Added
- Share link validation before creating studio
  - Prevents accidental studio creation without remote GPU
  - Shows clear error message if share link is missing

### Changed
- Use GPU-enabled Jupyter Docker images from Quay.io registry
  - PyTorch: `quay.io/jupyter/pytorch-notebook:cuda12-python-3.11.8` (CUDA 12)
  - TensorFlow: `quay.io/jupyter/tensorflow-notebook:cuda-latest`
  - All Jupyter images migrated from Docker Hub to Quay.io
- Auto-fill image version tag when selecting template
  - Automatically sets correct CUDA tag (cuda12-python-3.11.8, cuda-latest)
  - Users can still manually modify version if needed
- Improve studio creation progress logging
  - Real-time stdout/stderr streaming to output window
  - Docker pull progress now shows as INFO instead of ERROR
  - Better visibility of creation steps

### Fixed
- Null pointer errors in list commands
  - Added null checks for studioList, workerList, shareList, agentList
  - Safely returns empty arrays instead of crashing
  - Fixes "Cannot read properties of null (reading 'map')" error

## [0.1.10] - 2026-03-11

### Changed
- Show template default ports in Advanced Options when creating studio
  - Default ports are now pre-filled and visible in Port Mappings field
  - Advanced Options auto-expands when template has default ports
  - Users can see and modify ports before creating studio
  - Prevents port conflicts when creating multiple studios with same template

## [0.1.9] - 2026-03-11

### Changed
- Automatically show GPUGo output window when creating studio
  - Users can now see real-time creation progress and logs
  - Helps with debugging creation issues

## [0.1.8] - 2026-03-11

### Fixed
- Improved Windows CLI error detection and handling
  - Better detection of "command not found" errors (exit code 9009, 1)
  - Support for Chinese error messages (不是内部或外部命令)
  - Clearer error messages with install guide link
- Improved UTF-8 encoding handling for Windows CLI output

## [0.1.2] - 2026-02-02

### Added
- Enhanced logging system with dedicated output channel handling
- Improved CLI dependency management and downloading logic

### Fixed
- Agent and worker status reporting improvements
- Authentication token storage handling
- CLI interaction and output parsing optimizations
- Agent startup and connection reliability fixes

## [0.1.0] - 2026-01-16

### Added

- **Authentication**
  - Login/Logout with Personal Access Token (PAT)
  - Secure token storage using VS Code SecretStorage API

- **Studio Environment Management**
  - Create new studio environments with pre-configured templates
  - Start, stop, and remove environments
  - Connect to environments via SSH directly from VS Code
  - View container logs

- **Workers View**
  - List all available GPU workers
  - View worker details and active connections
  - Create worker guidance with CLI commands

- **Devices View**
  - List all agents and connected GPUs
  - View GPU specifications (model, VRAM, driver version)
  - Monitor device status

- **General**
  - Auto-refresh for real-time status updates
  - Open Dashboard command for quick web access
  - Auto-download of ggo CLI binary
  - Configurable API endpoints
