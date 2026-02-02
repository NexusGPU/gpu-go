#!/bin/sh
# GPU Go (ggo) Installation Script
# Usage: curl -sfL https://cdn.tensor-fusion.ai/archive/gpugo/install.sh | GPU_GO_TOKEN="xxx" sh
#
# Environment variables:
#   - GPU_GO_VERSION: Specific version to install (default: latest)
#   - GPU_GO_TOKEN: Agent registration token (Linux only, will auto-register and setup systemd service)
#   - GPU_GO_ENDPOINT: Custom API endpoint (optional, used with GPU_GO_TOKEN for agent registration)
#   - GGO_INSTALL_DIR: Installation directory (default: /usr/local/bin)
#   - GGO_CONFIG_DIR: Config directory (default: platform-specific)
#   - GGO_NO_MODIFY_PATH: If set, don't add to PATH
#
# Examples:
#   # Install latest version (client mode)
#   curl -sfL https://cdn.tensor-fusion.ai/archive/gpugo/install.sh | sh
#
#   # Install specific version
#   curl -sfL https://cdn.tensor-fusion.ai/archive/gpugo/install.sh | GPU_GO_VERSION=v1.0.0 sh
#
#   # Install to custom directory
#   curl -sfL https://cdn.tensor-fusion.ai/archive/gpugo/install.sh | GGO_INSTALL_DIR=/opt/bin sh
#
#   # Agent mode (Linux only): install, register agent, and setup systemd service
#   curl -sfL https://cdn.tensor-fusion.ai/archive/gpugo/install.sh | GPU_GO_TOKEN="your-token" sh
#
#   # Agent mode with custom endpoint
#   curl -sfL https://cdn.tensor-fusion.ai/archive/gpugo/install.sh | GPU_GO_TOKEN="your-token" GPU_GO_ENDPOINT="https://api.example.com" sh

set -e

# --- Configuration ---
CDN_BASE_URL="https://cdn.tensor-fusion.ai/archive/gpugo"
BINARY_NAME="ggo"
INSTALL_DIR="${GGO_INSTALL_DIR:-/usr/local/bin}"
VERSION="${GPU_GO_VERSION:-latest}"
TOKEN="${GPU_GO_TOKEN:-}"
ENDPOINT="${GPU_GO_ENDPOINT:-}"
SYSTEMD_SERVICE_NAME="ggo-agent"

# --- Helper functions ---
info() {
    printf '[INFO] %s\n' "$@"
}

warn() {
    printf '[WARN] %s\n' "$@" >&2
}

fatal() {
    printf '[ERROR] %s\n' "$@" >&2
    exit 1
}

# --- Platform detection ---
detect_os() {
    case "$(uname -s)" in
        Linux*)     echo "linux";;
        Darwin*)    echo "darwin";;
        CYGWIN*|MINGW*|MSYS*) echo "windows";;
        *)          fatal "Unsupported OS: $(uname -s)";;
    esac
}

detect_arch() {
    case "$(uname -m)" in
        x86_64|amd64)   echo "amd64";;
        aarch64|arm64)  echo "arm64";;
        armv7l)         echo "arm";;
        *)              fatal "Unsupported architecture: $(uname -m)";;
    esac
}

# --- Build download URL ---
build_download_url() {
    os="$1"
    arch="$2"
    version="$3"
    
    # CDN URL format: https://cdn.tensor-fusion.ai/archive/gpugo/{version}/gpugo-{os}-{arch}
    # For Linux, the format might be: https://cdn.tensor-fusion.ai/archive/gpugo/{version}/gpugo-{arch}
    # We'll try both formats for compatibility
    
    if [ "${os}" = "linux" ]; then
        # Linux uses simplified naming: gpugo-{arch}
        echo "${CDN_BASE_URL}/${version}/gpugo-${arch}"
    else
        # Other OS use: gpugo-{os}-{arch}
        echo "${CDN_BASE_URL}/${version}/gpugo-${os}-${arch}"
    fi
}

# --- Download function ---
download() {
    url="$1"
    dest="$2"
    silent="${3:-}"
    
    if [ -z "${silent}" ]; then
        info "Downloading ${url}"
    fi
    
    if command -v curl >/dev/null 2>&1; then
        if [ -n "${silent}" ]; then
            curl -fsSL -o "${dest}" "${url}" 2>/dev/null || return 1
        else
            curl -fsSL -o "${dest}" "${url}" || return 1
        fi
    elif command -v wget >/dev/null 2>&1; then
        if [ -n "${silent}" ]; then
            wget -qO "${dest}" "${url}" 2>/dev/null || return 1
        else
            wget -qO "${dest}" "${url}" || return 1
        fi
    else
        fatal "Neither curl nor wget found. Please install one of them."
    fi
}

# --- Verify checksum ---
verify_checksum() {
    file="$1"
    expected="$2"
    
    if command -v sha256sum >/dev/null 2>&1; then
        actual=$(sha256sum "${file}" | awk '{print $1}')
    elif command -v shasum >/dev/null 2>&1; then
        actual=$(shasum -a 256 "${file}" | awk '{print $1}')
    else
        warn "No sha256 tool found, skipping checksum verification"
        return 0
    fi
    
    if [ "${actual}" != "${expected}" ]; then
        fatal "Checksum verification failed. Expected: ${expected}, Got: ${actual}"
    fi
    
    info "Checksum verified"
}

# --- Check for root/sudo ---
can_install_to_dir() {
    dir="$1"
    if [ -w "${dir}" ]; then
        return 0
    fi
    return 1
}

get_sudo() {
    if [ "$(id -u)" -eq 0 ]; then
        echo ""
    elif command -v sudo >/dev/null 2>&1; then
        echo "sudo"
    else
        echo ""
    fi
}

# --- Check sudo for systemd operations ---
require_sudo_for_systemd() {
    if [ "$(id -u)" -eq 0 ]; then
        return 0
    fi
    
    if ! command -v sudo >/dev/null 2>&1; then
        fatal "sudo is required for systemd service management but not found. Please run as root or install sudo."
    fi
    
    # Test if sudo works
    if ! sudo -n true 2>/dev/null; then
        info "sudo access is required for systemd service management"
        if ! sudo true; then
            fatal "Failed to obtain sudo access"
        fi
    fi
    
    return 0
}

# --- Setup systemd service ---
setup_systemd_service() {
    binary_path="$1"
    
    info ""
    info "Setting up systemd service..."
    
    # Check if systemd is available
    if ! command -v systemctl >/dev/null 2>&1; then
        warn "systemctl not found, skipping systemd service setup"
        warn "You can manually start the agent with: ${binary_path} agent start"
        return 1
    fi
    
    # Check if systemd is running (PID 1)
    if [ ! -d /run/systemd/system ]; then
        warn "systemd is not running, skipping systemd service setup"
        warn "You can manually start the agent with: ${binary_path} agent start"
        return 1
    fi
    
    # Require sudo for systemd operations
    require_sudo_for_systemd
    
    SUDO=$(get_sudo)
    
    # Build environment variables section
    ENV_VARS=""
    if [ -n "${ENDPOINT}" ]; then
        ENV_VARS="Environment=\"GPU_GO_ENDPOINT=${ENDPOINT}\""
        info "Systemd service will use API endpoint: ${ENDPOINT}"
    fi
    
    # Create systemd service file
    SERVICE_FILE="/etc/systemd/system/${SYSTEMD_SERVICE_NAME}.service"
    
    info "Creating systemd service: ${SERVICE_FILE}"
    
    # Create service file content
    SERVICE_CONTENT="[Unit]
Description=GPU Go Agent - GPU Sharing Service
Documentation=https://tensor-fusion.ai/docs
After=network-online.target nvidia-persistenced.service
Wants=network-online.target

[Service]
Type=simple
ExecStart=${binary_path} agent start
Restart=always
RestartSec=10
TimeoutStartSec=0
TimeoutStopSec=30

# Run as root for GPU access
User=root
Group=root

# Security hardening (relaxed for GPU access)
NoNewPrivileges=false
ProtectSystem=full
ProtectHome=read-only
PrivateTmp=true
PrivateDevices=false

# Allow access to GPU devices and config
ReadWritePaths=/var/lib/ggo /tmp /root/.config/ggo

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096
LimitMEMLOCK=infinity

# Environment
${ENV_VARS}

[Install]
WantedBy=multi-user.target
"

    # Write service file
    echo "${SERVICE_CONTENT}" | ${SUDO} tee "${SERVICE_FILE}" > /dev/null
    
    # Create data and config directories if needed
    if [ ! -d /var/lib/ggo ]; then
        ${SUDO} mkdir -p /var/lib/ggo
    fi
    if [ ! -d /root/.config/ggo ]; then
        ${SUDO} mkdir -p /root/.config/ggo
    fi
    
    # Reload systemd daemon
    info "Reloading systemd daemon..."
    ${SUDO} systemctl daemon-reload
    
    # Enable and start service
    info "Enabling ${SYSTEMD_SERVICE_NAME} service..."
    ${SUDO} systemctl enable "${SYSTEMD_SERVICE_NAME}"
    
    info "Starting ${SYSTEMD_SERVICE_NAME} service..."
    if ${SUDO} systemctl start "${SYSTEMD_SERVICE_NAME}"; then
        info "Service started successfully!"
        info ""
        info "Service management commands:"
        info "  sudo systemctl status ${SYSTEMD_SERVICE_NAME}   # Check status"
        info "  sudo systemctl stop ${SYSTEMD_SERVICE_NAME}     # Stop service"
        info "  sudo systemctl restart ${SYSTEMD_SERVICE_NAME}  # Restart service"
        info "  sudo journalctl -u ${SYSTEMD_SERVICE_NAME} -f   # View logs"
    else
        warn "Failed to start service. Check logs with: sudo journalctl -u ${SYSTEMD_SERVICE_NAME}"
        return 1
    fi
    
    return 0
}

# --- Register agent ---
register_agent() {
    binary_path="$1"
    token="$2"
    
    info ""
    info "Registering GPU Go agent..."
    
    # Get sudo command (empty if already root)
    SUDO=$(get_sudo)
    
    # Build register command
    # Use sudo to ensure config is saved to root's home directory (~/.gpugo/config)
    # This matches the systemd service which runs as root
    REGISTER_CMD="${SUDO} ${binary_path} agent register -t ${token}"
    
    if [ -n "${ENDPOINT}" ]; then
        REGISTER_CMD="${REGISTER_CMD} --server ${ENDPOINT}"
    fi
    
    info "Running: ${SUDO:+sudo }ggo agent register"
    
    if ${REGISTER_CMD}; then
        info "Agent registered successfully!"
        return 0
    else
        fatal "Agent registration failed. Please check your token and try again."
    fi
}

# --- Main installation ---
main() {
    OS=$(detect_os)
    ARCH=$(detect_arch)
    
    # Only support Mac and Linux
    if [ "${OS}" = "windows" ]; then
        fatal "Windows installation is not supported by this script. Please use install.ps1 instead."
    fi
    
    info "Detected OS: ${OS}"
    info "Detected architecture: ${ARCH}"
    info "Installing version: ${VERSION}"
    
    if [ -n "${TOKEN}" ]; then
        if [ "${OS}" = "linux" ]; then
            info "Agent mode: will register and setup systemd service after installation"
        else
            warn "GPU_GO_TOKEN is only used for agent mode on Linux, ignoring on ${OS}"
        fi
    fi
    
    if [ -n "${ENDPOINT}" ]; then
        info "Using custom API endpoint: ${ENDPOINT}"
    fi
    
    # Determine binary suffix (not needed for Mac/Linux, but keep for consistency)
    SUFFIX=""
    
    # Construct download URL from CDN
    DOWNLOAD_URL=$(build_download_url "${OS}" "${ARCH}" "${VERSION}")
    CHECKSUM_URL="${DOWNLOAD_URL}.sha256"
    
    info "Download URL: ${DOWNLOAD_URL}"
    
    # Create temp directory
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "${TMP_DIR}"' EXIT
    
    # Determine binary filename based on OS
    if [ "${OS}" = "linux" ]; then
        BINARY_FILENAME="gpugo-${ARCH}"
    else
        BINARY_FILENAME="gpugo-${OS}-${ARCH}"
    fi
    
    TMP_BINARY="${TMP_DIR}/${BINARY_NAME}"
    TMP_CHECKSUM="${TMP_DIR}/${BINARY_NAME}.sha256"
    
    # Download binary
    if ! download "${DOWNLOAD_URL}" "${TMP_BINARY}"; then
        fatal "Failed to download binary from ${DOWNLOAD_URL}"
    fi
    
    # Try to download checksum (optional, don't fail if not available)
    if download "${CHECKSUM_URL}" "${TMP_CHECKSUM}" "silent"; then
        EXPECTED_CHECKSUM=$(cat "${TMP_CHECKSUM}" | awk '{print $1}')
        verify_checksum "${TMP_BINARY}" "${EXPECTED_CHECKSUM}"
    else
        warn "Checksum file not available, skipping verification"
    fi
    
    # Make executable
    chmod +x "${TMP_BINARY}"
    
    # Install binary
    DEST_PATH="${INSTALL_DIR}/${BINARY_NAME}"
    
    info "Installing to ${DEST_PATH}"
    
    # Check if we need sudo
    SUDO=$(get_sudo)
    
    # Create install directory if needed
    if [ ! -d "${INSTALL_DIR}" ]; then
        ${SUDO} mkdir -p "${INSTALL_DIR}"
    fi
    
    # Move binary to install location
    if can_install_to_dir "${INSTALL_DIR}"; then
        mv "${TMP_BINARY}" "${DEST_PATH}"
    else
        ${SUDO} mv "${TMP_BINARY}" "${DEST_PATH}"
    fi
    
    info "Installation complete!"
    info ""
    info "ggo has been installed to ${DEST_PATH}"
    info ""
    
    # Check if install dir is in PATH
    case ":${PATH}:" in
        *":${INSTALL_DIR}:"*) ;;
        *)
            if [ -z "${GGO_NO_MODIFY_PATH}" ]; then
                warn "${INSTALL_DIR} is not in your PATH"
                info ""
                info "Add it to your PATH by running:"
                info "  export PATH=\"\$PATH:${INSTALL_DIR}\""
                info ""
                info "To make this permanent, add the above line to your shell profile:"
                info "  ~/.bashrc, ~/.zshrc, or ~/.profile"
            fi
            ;;
    esac
    
    # Show version
    if command -v "${DEST_PATH}" >/dev/null 2>&1; then
        info ""
        info "Installed version:"
        "${DEST_PATH}" --version 2>/dev/null || true
    fi
    
    # Agent mode: register and setup systemd service (Linux only)
    if [ -n "${TOKEN}" ] && [ "${OS}" = "linux" ]; then
        info ""
        info "=========================================="
        info "Agent Mode Setup"
        info "=========================================="
        
        # Register agent
        register_agent "${DEST_PATH}" "${TOKEN}"
        
        # Setup systemd service
        setup_systemd_service "${DEST_PATH}"
        
        info ""
        info "=========================================="
        info "GPU Go Agent installation complete!"
        info "=========================================="
        if [ -n "${ENDPOINT}" ]; then
            info "API Endpoint: ${ENDPOINT}"
        fi
        info "The agent is now running as a systemd service."
        info ""
        return 0
    fi
    
    # Handle additional arguments (for manual agent mode, etc.)
    if [ $# -gt 0 ]; then
        info ""
        info "Running: ${BINARY_NAME} $*"
        exec "${DEST_PATH}" "$@"
    fi
    
    info ""
    info "Quick start:"
    info "  # Show help"
    info "  ggo --help"
    info ""
    info "  # Register as agent (on GPU server)"
    info "  ggo agent register --token <your-token>"
    info ""
    info "  # Use a shared GPU"
    info "  ggo use <short-link>"
    info ""
}

# Run main with all arguments
main "$@"
