#!/bin/sh
# GPU Go (ggo) Installation Script
# Usage: curl -sfL https://get.tensor-fusion.ai | sh -s -
#
# Environment variables:
#   - GGO_VERSION: Specific version to install (default: latest)
#   - GGO_INSTALL_DIR: Installation directory (default: /usr/local/bin)
#   - GGO_CONFIG_DIR: Config directory (default: platform-specific)
#   - GGO_NO_MODIFY_PATH: If set, don't add to PATH
#
# Examples:
#   # Install latest version
#   curl -sfL https://cdn.tensor-fusion.ai/gpugo/install.sh | sh -s -
#
#   # Install specific version
#   curl -sfL https://cdn.tensor-fusion.ai/gpugo/install.sh | GGO_VERSION=v1.0.0 sh -s -
#
#   # Install to custom directory
#   curl -sfL https://cdn.tensor-fusion.ai/gpugo/install.sh | GGO_INSTALL_DIR=/opt/bin sh -s -
#
#   # Agent mode (register and start agent)
#   curl -sfL https://cdn.tensor-fusion.ai/gpugo/install.sh | sh -s - agent --token <token>

set -e

# --- Configuration ---
GITHUB_REPO="NexusGPU/gpu-go"
BINARY_NAME="ggo"
INSTALL_DIR="${GGO_INSTALL_DIR:-/usr/local/bin}"

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

# --- Version detection ---
get_latest_version() {
    # Try using curl
    if command -v curl >/dev/null 2>&1; then
        curl -sL "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | \
            grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
    # Fall back to wget
    elif command -v wget >/dev/null 2>&1; then
        wget -qO- "https://api.github.com/repos/${GITHUB_REPO}/releases/latest" | \
            grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/'
    else
        fatal "Neither curl nor wget found. Please install one of them."
    fi
}

# --- Download function ---
download() {
    url="$1"
    dest="$2"
    
    info "Downloading ${url}"
    
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "${dest}" "${url}"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "${dest}" "${url}"
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

# --- Main installation ---
main() {
    OS=$(detect_os)
    ARCH=$(detect_arch)
    
    info "Detected OS: ${OS}"
    info "Detected architecture: ${ARCH}"
    
    # Get version
    VERSION="${GGO_VERSION:-$(get_latest_version)}"
    if [ -z "${VERSION}" ]; then
        fatal "Could not determine version to install"
    fi
    info "Installing version: ${VERSION}"
    
    # Determine binary suffix
    SUFFIX=""
    if [ "${OS}" = "windows" ]; then
        SUFFIX=".exe"
    fi
    
    # Construct download URL
    BINARY_FILENAME="${BINARY_NAME}-${OS}-${ARCH}${SUFFIX}"
    DOWNLOAD_URL="https://github.com/${GITHUB_REPO}/releases/download/${VERSION}/${BINARY_FILENAME}"
    CHECKSUM_URL="${DOWNLOAD_URL}.sha256"
    
    # Create temp directory
    TMP_DIR=$(mktemp -d)
    trap 'rm -rf "${TMP_DIR}"' EXIT
    
    TMP_BINARY="${TMP_DIR}/${BINARY_FILENAME}"
    TMP_CHECKSUM="${TMP_DIR}/${BINARY_FILENAME}.sha256"
    
    # Download binary and checksum
    download "${DOWNLOAD_URL}" "${TMP_BINARY}"
    download "${CHECKSUM_URL}" "${TMP_CHECKSUM}"
    
    # Verify checksum
    EXPECTED_CHECKSUM=$(cat "${TMP_CHECKSUM}" | awk '{print $1}')
    verify_checksum "${TMP_BINARY}" "${EXPECTED_CHECKSUM}"
    
    # Make executable
    chmod +x "${TMP_BINARY}"
    
    # Install binary
    DEST_PATH="${INSTALL_DIR}/${BINARY_NAME}${SUFFIX}"
    
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
    
    # Handle additional arguments (for agent mode, etc.)
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
    info "  ggo use <short-code>"
    info ""
}

# Run main with all arguments
main "$@"
