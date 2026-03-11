# Changelog

All notable changes to the "GPUGo" extension will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
