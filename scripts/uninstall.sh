#!/bin/sh
# GPU Go (ggo) Uninstallation Script
# Usage: curl -sfL https://cdn.tensor-fusion.ai/archive/gpugo/uninstall.sh | sh
#
# Environment variables:
#   - GGO_INSTALL_DIR: Installation directory (default: /usr/local/bin)
#
# This script will:
#   - Stop and remove the systemd service (Linux)
#   - Remove the ggo binary
#   - Clean up config directories

set -e

# --- Configuration ---
BINARY_NAME="ggo"
INSTALL_DIR="${GGO_INSTALL_DIR:-/usr/local/bin}"
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

# --- Check for root/sudo ---
get_sudo() {
    if [ "$(id -u)" -eq 0 ]; then
        echo ""
    elif command -v sudo >/dev/null 2>&1; then
        echo "sudo"
    else
        echo ""
    fi
}

# --- Stop and remove systemd service ---
remove_systemd_service() {
    if ! command -v systemctl >/dev/null 2>&1; then
        return 0
    fi
    
    if [ ! -d /run/systemd/system ]; then
        return 0
    fi
    
    SERVICE_FILE="/etc/systemd/system/${SYSTEMD_SERVICE_NAME}.service"
    
    if [ ! -f "${SERVICE_FILE}" ]; then
        info "Systemd service not found, skipping..."
        return 0
    fi
    
    info "Stopping systemd service..."
    SUDO=$(get_sudo)
    
    # Stop service
    ${SUDO} systemctl stop "${SYSTEMD_SERVICE_NAME}" 2>/dev/null || true
    
    # Disable service
    info "Disabling systemd service..."
    ${SUDO} systemctl disable "${SYSTEMD_SERVICE_NAME}" 2>/dev/null || true
    
    # Remove service file
    info "Removing systemd service file..."
    ${SUDO} rm -f "${SERVICE_FILE}"
    
    # Reload systemd daemon
    info "Reloading systemd daemon..."
    ${SUDO} systemctl daemon-reload
    
    info "Systemd service removed successfully!"
}

# --- Stop launchd service (macOS) ---
remove_launchd_service() {
    PLIST_FILE="$HOME/Library/LaunchAgents/ai.tensor-fusion.ggo-agent.plist"
    PLIST_FILE_SYSTEM="/Library/LaunchDaemons/ai.tensor-fusion.ggo-agent.plist"
    
    # Check user launch agent
    if [ -f "${PLIST_FILE}" ]; then
        info "Stopping launchd user agent..."
        launchctl unload "${PLIST_FILE}" 2>/dev/null || true
        rm -f "${PLIST_FILE}"
        info "User launch agent removed!"
    fi
    
    # Check system launch daemon
    if [ -f "${PLIST_FILE_SYSTEM}" ]; then
        info "Stopping launchd system daemon..."
        SUDO=$(get_sudo)
        ${SUDO} launchctl unload "${PLIST_FILE_SYSTEM}" 2>/dev/null || true
        ${SUDO} rm -f "${PLIST_FILE_SYSTEM}"
        info "System launch daemon removed!"
    fi
}

# --- Remove binary ---
remove_binary() {
    BINARY_PATH="${INSTALL_DIR}/${BINARY_NAME}"
    
    if [ ! -f "${BINARY_PATH}" ]; then
        info "Binary not found at ${BINARY_PATH}, checking common locations..."
        
        # Check common locations
        for dir in /usr/local/bin /usr/bin /opt/bin "$HOME/.local/bin" "$HOME/bin"; do
            if [ -f "${dir}/${BINARY_NAME}" ]; then
                BINARY_PATH="${dir}/${BINARY_NAME}"
                info "Found binary at ${BINARY_PATH}"
                break
            fi
        done
    fi
    
    if [ ! -f "${BINARY_PATH}" ]; then
        warn "ggo binary not found"
        return 0
    fi
    
    info "Removing binary at ${BINARY_PATH}..."
    
    SUDO=$(get_sudo)
    
    # Check if we need sudo
    if [ -w "$(dirname "${BINARY_PATH}")" ]; then
        rm -f "${BINARY_PATH}"
    else
        ${SUDO} rm -f "${BINARY_PATH}"
    fi
    
    info "Binary removed!"
}

# --- Remove config directories ---
remove_config() {
    info "Removing configuration directories..."
    
    # User config
    CONFIG_DIRS="$HOME/.config/ggo $HOME/.ggo $HOME/.gpugo"
    
    for dir in ${CONFIG_DIRS}; do
        if [ -d "${dir}" ]; then
            info "Removing ${dir}..."
            rm -rf "${dir}"
        fi
    done
    
    # System config (Linux)
    SUDO=$(get_sudo)
    SYSTEM_DIRS="/var/lib/ggo /etc/ggo"
    
    for dir in ${SYSTEM_DIRS}; do
        if [ -d "${dir}" ]; then
            info "Removing ${dir}..."
            ${SUDO} rm -rf "${dir}"
        fi
    done
    
    # Root config (if running as root or with sudo)
    if [ "$(id -u)" -eq 0 ] || [ -n "$(get_sudo)" ]; then
        if [ -d "/root/.config/ggo" ]; then
            ${SUDO} rm -rf "/root/.config/ggo"
        fi
        if [ -d "/root/.gpugo" ]; then
            ${SUDO} rm -rf "/root/.gpugo"
        fi
    fi
    
    info "Configuration directories removed!"
}

# --- Kill running processes ---
kill_processes() {
    info "Stopping any running ggo processes..."
    
    # Try to find and kill ggo processes
    if command -v pkill >/dev/null 2>&1; then
        pkill -9 "${BINARY_NAME}" 2>/dev/null || true
    elif command -v killall >/dev/null 2>&1; then
        killall -9 "${BINARY_NAME}" 2>/dev/null || true
    fi
}

# --- Main uninstallation ---
main() {
    OS=$(detect_os)
    
    if [ "${OS}" = "windows" ]; then
        fatal "Windows uninstallation is not supported by this script. Please use uninstall.ps1 or uninstall.bat instead."
    fi
    
    echo ""
    echo "=========================================="
    echo "  GPU Go (ggo) Uninstaller"
    echo "=========================================="
    echo ""
    
    info "Detected OS: ${OS}"
    
    # Kill any running processes first
    kill_processes
    
    # Remove services based on OS
    if [ "${OS}" = "linux" ]; then
        remove_systemd_service
    elif [ "${OS}" = "darwin" ]; then
        remove_launchd_service
    fi
    
    # Remove binary
    remove_binary
    
    # Remove config
    remove_config
    
    echo ""
    echo "=========================================="
    echo "  Uninstallation Complete!"
    echo "=========================================="
    echo ""
    info "GPU Go (ggo) has been removed from your system."
    echo ""
}

# Run main
main "$@"
