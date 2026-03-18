#!/bin/sh
# GPU Go (ggo) Uninstallation Script
#
# When called from `ggo uninstall`, this script only handles service and binary
# removal. Agent unregistration and config directory cleanup are done by the
# Go code before/after this script runs.
#
# When called standalone (curl | sh), it still works but won't unregister the
# agent from the server — users should run `ggo uninstall` instead.
#
# Environment variables:
#   - GGO_INSTALL_DIR: Installation directory (default: /usr/local/bin)

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

    ${SUDO} systemctl stop "${SYSTEMD_SERVICE_NAME}" 2>/dev/null || true

    info "Disabling systemd service..."
    ${SUDO} systemctl disable "${SYSTEMD_SERVICE_NAME}" 2>/dev/null || true

    info "Removing systemd service file..."
    ${SUDO} rm -f "${SERVICE_FILE}"

    info "Reloading systemd daemon..."
    ${SUDO} systemctl daemon-reload

    info "Systemd service removed successfully!"
}

# --- Stop launchd service (macOS) ---
remove_launchd_service() {
    PLIST_FILE="$HOME/Library/LaunchAgents/ai.tensor-fusion.ggo-agent.plist"
    PLIST_FILE_SYSTEM="/Library/LaunchDaemons/ai.tensor-fusion.ggo-agent.plist"

    if [ -f "${PLIST_FILE}" ]; then
        info "Stopping launchd user agent..."
        launchctl unload "${PLIST_FILE}" 2>/dev/null || true
        rm -f "${PLIST_FILE}"
        info "User launch agent removed!"
    fi

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

    if [ -w "$(dirname "${BINARY_PATH}")" ]; then
        if rm -f "${BINARY_PATH}" 2>/dev/null; then
            info "Binary removed!"
        else
            warn "Failed to remove binary at ${BINARY_PATH}"
            return 1
        fi
    else
        if ${SUDO} rm -f "${BINARY_PATH}" 2>/dev/null; then
            info "Binary removed!"
        else
            warn "Failed to remove binary at ${BINARY_PATH} (permission denied or sudo not available)"
            warn "Please run: sudo rm -f ${BINARY_PATH}"
            return 1
        fi
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

    # Refresh sudo credentials once at the start (if needed)
    SUDO=$(get_sudo)
    if [ -n "${SUDO}" ]; then
        ${SUDO} -v 2>/dev/null || true
    fi

    # Stop and remove service
    if [ "${OS}" = "linux" ]; then
        remove_systemd_service
    elif [ "${OS}" = "darwin" ]; then
        remove_launchd_service
    fi

    # Remove binary
    remove_binary

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
