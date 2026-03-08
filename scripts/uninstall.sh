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

    # Refresh sudo timestamp to prevent password prompts during removal
    if [ -n "${SUDO}" ]; then
        ${SUDO} -v 2>/dev/null || true
    fi

    # Refresh sudo timestamp to prevent password prompts during removal
    if [ -n "${SUDO}" ]; then
        ${SUDO} -v 2>/dev/null || true
    fi
    
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

    # Refresh sudo timestamp to prevent password prompts during removal
    if [ -n "${SUDO}" ]; then
        ${SUDO} -v 2>/dev/null || true
    fi
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

    # Refresh sudo timestamp to prevent password prompts during removal
    if [ -n "${SUDO}" ]; then
        ${SUDO} -v 2>/dev/null || true
    fi
    
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

    # Refresh sudo timestamp to prevent password prompts during removal
    if [ -n "${SUDO}" ]; then
        ${SUDO} -v 2>/dev/null || true
    fi
    SYSTEM_DIRS="/var/lib/ggo /etc/ggo"
    
    for dir in ${SYSTEM_DIRS}; do
        if [ -d "${dir}" ]; then
            info "Removing ${dir}..."
            ${SUDO} rm -rf "${dir}"
        fi
    done
    
    # Root config (if running as root or with sudo)
    local root_cleanup_failed=false
    if [ "$(id -u)" -eq 0 ] || [ -n "$(get_sudo)" ]; then
        if [ -d "/root/.config/ggo" ]; then
            if ${SUDO} rm -rf "/root/.config/ggo" 2>/dev/null; then
                info "Removed /root/.config/ggo"
            else
                warn "Failed to remove /root/.config/ggo (permission denied)"
                root_cleanup_failed=true
            fi
        fi
        if [ -d "/root/.ggo" ]; then
            if ${SUDO} rm -rf "/root/.ggo" 2>/dev/null; then
                info "Removed /root/.ggo"
            else
                warn "Failed to remove /root/.ggo (permission denied)"
                root_cleanup_failed=true
            fi
        fi
        if [ -d "/root/.gpugo" ]; then
            if ${SUDO} rm -rf "/root/.gpugo" 2>/dev/null; then
                info "Removed /root/.gpugo"
            else
                warn "Failed to remove /root/.gpugo (permission denied)"
                root_cleanup_failed=true
            fi
        fi
    else
        # Check if root directories exist but we can't remove them
        if [ -d "/root/.config/ggo" ] || [ -d "/root/.ggo" ] || [ -d "/root/.gpugo" ]; then
            warn "Root configuration directories exist but sudo is not available"
            root_cleanup_failed=true
        fi
    fi

    info "Configuration directories removed!"

    # Warn about manual cleanup if root cleanup failed
    if [ "${root_cleanup_failed}" = "true" ]; then
        echo ""
        warn "==============================================="
        warn "  MANUAL CLEANUP REQUIRED"
        warn "==============================================="
        warn "Some configuration directories could not be removed automatically."
        warn "Please run the following commands with appropriate permissions:"
        warn ""
        if [ -d "/root/.config/ggo" ]; then
            warn "  sudo rm -rf /root/.config/ggo"
        fi
        if [ -d "/root/.ggo" ]; then
            warn "  sudo rm -rf /root/.ggo"
        fi
        if [ -d "/root/.gpugo" ]; then
            warn "  sudo rm -rf /root/.gpugo"
        fi
        warn "==============================================="
        echo ""
    fi
}

# --- Kill running processes ---
kill_processes() {
    info "Stopping any running ggo processes..."
    if command -v pkill >/dev/null 2>&1; then
        pkill -9 "${BINARY_NAME}" 2>/dev/null || true
    elif command -v killall >/dev/null 2>&1; then
        killall -9 "${BINARY_NAME}" 2>/dev/null || true
    fi

    info "Stopping any running tensor-fusion-worker processes..."
    if command -v pkill >/dev/null 2>&1; then
        pkill -9 "tensor-fusion-worker" 2>/dev/null || true
    elif command -v killall >/dev/null 2>&1; then
        killall -9 "tensor-fusion-worker" 2>/dev/null || true
    fi
}

# --- Unregister from server ---
unregister_from_server() {
    # Search for ggo binary in multiple locations
    local binary_path=""
    local search_paths="${GGO_INSTALL_DIR:-/usr/local/bin}/${BINARY_NAME} /usr/local/bin/${BINARY_NAME} /usr/bin/${BINARY_NAME} /opt/bin/${BINARY_NAME} ${HOME}/.local/bin/${BINARY_NAME} ${HOME}/bin/${BINARY_NAME}"

    for path in ${search_paths}; do
        if [ -f "${path}" ] && [ -x "${path}" ]; then
            binary_path="${path}"
            break
        fi
    done

    if [ -z "${binary_path}" ]; then
        warn "ggo binary not found in common locations, skipping server unregistration"
        warn "Searched: ${search_paths}"
        return 0
    fi

    info "Unregistering agent from server (using binary at ${binary_path})..."

    # Check if agent config exists in root directory (agent mode)
    # If so, we need to run unregister with sudo
    local needs_sudo=false
    if [ -f "/root/.gpugo/config/agent.yaml" ] || [ -f "/root/.config/ggo/agent.yaml" ]; then
        needs_sudo=true
        info "Agent config found in root directory, using sudo for unregistration"
    fi

    # Run unregister command
    local unregister_result=0
    if [ "${needs_sudo}" = "true" ]; then
        SUDO=$(get_sudo)

    # Refresh sudo timestamp to prevent password prompts during removal
    if [ -n "${SUDO}" ]; then
        ${SUDO} -v 2>/dev/null || true
    fi
        if [ -n "${SUDO}" ]; then
            ${SUDO} "${binary_path}" agent unregister --force 2>/dev/null || unregister_result=$?
        else
            warn "Sudo not available, attempting unregister without sudo (may fail)"
            "${binary_path}" agent unregister --force 2>/dev/null || unregister_result=$?
        fi
    else
        "${binary_path}" agent unregister --force 2>/dev/null || unregister_result=$?
    fi

    if [ ${unregister_result} -eq 0 ]; then
        info "Agent unregistered from server"
    else
        warn "Server unregistration failed (exit code: ${unregister_result})"
        warn "Local config will still be removed"
        warn "You may need to manually remove the agent from the server dashboard"
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

    # Unregister from server (before removing binary and config)
    unregister_from_server

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
