package studio

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"k8s.io/klog/v2"
)

// setupSSHInContainer installs and configures SSH server in a running container
// This allows any Docker image to be used, not just images with SSH pre-installed
// dockerHost can be empty for default Docker socket, or custom (e.g., for Colima)
// envVars are the TensorFusion environment variables that need to be available in SSH sessions
func setupSSHInContainer(ctx context.Context, dockerCmd, containerID, sshPublicKey, dockerHost string, envVars map[string]string) error {
	klog.V(2).Infof("Setting up SSH in container %s", containerID)

	// Helper to create docker exec command with proper environment
	execCmd := func(args ...string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, dockerCmd, args...)
		if dockerHost != "" {
			cmd.Env = append(os.Environ(), fmt.Sprintf("DOCKER_HOST=%s", dockerHost))
		}
		return cmd
	}

	// Install script that works across different base images (Debian/Ubuntu, Alpine, RHEL/CentOS)
	// Smart detection: skip installation if SSH is already present
	installScript := `#!/bin/sh
set -e

# Check if SSH is already installed
if command -v sshd >/dev/null 2>&1 && [ -f /usr/sbin/sshd ]; then
    echo "   ✓ SSH server already installed, skipping installation"
else
    # Detect package manager and install openssh-server
    if command -v apt-get >/dev/null 2>&1; then
        export DEBIAN_FRONTEND=noninteractive
        printf "   Installing SSH (30-60s): "

        # Create temp files for error messages
        ERR_FILE=$(mktemp)

        # Update packages in background with progress indicator
        apt-get update -qq >/dev/null 2>"$ERR_FILE" &
        update_pid=$!

        # Show progress dots while apt-get update runs
        while kill -0 $update_pid 2>/dev/null; do
            printf "."
            sleep 2
        done

        # Wait for apt-get update and check exit code
        wait $update_pid
        update_exit=$?

        if [ $update_exit -ne 0 ]; then
            printf "\n   ✗ apt-get update failed (exit code: $update_exit)\n"
            echo "==================== Error Output ===================="
            cat "$ERR_FILE" 2>/dev/null || echo "No error output captured"
            echo "======================================================"
            rm -f "$ERR_FILE"
            exit $update_exit
        fi

        # Install packages in background with progress indicator
        apt-get install -y -qq openssh-server sudo >/dev/null 2>"$ERR_FILE" &
        install_pid=$!

        # Show progress dots while apt-get install runs
        while kill -0 $install_pid 2>/dev/null; do
            printf "."
            sleep 2
        done

        # Wait for apt-get install and check exit code
        wait $install_pid
        install_exit=$?

        if [ $install_exit -ne 0 ]; then
            printf "\n   ✗ apt-get install failed (exit code: $install_exit)\n"
            echo "==================== Error Output ===================="
            cat "$ERR_FILE" 2>/dev/null || echo "No error output captured"
            echo "======================================================"
            rm -f "$ERR_FILE"
            exit $install_exit
        fi

        rm -f "$ERR_FILE"
        printf " done\n"
        mkdir -p /run/sshd
    elif command -v apk >/dev/null 2>&1; then
        printf "   Installing SSH: "
        apk add --no-cache openssh sudo >/dev/null 2>&1
        ssh-keygen -A >/dev/null 2>&1
        printf "done\n"
    elif command -v yum >/dev/null 2>&1; then
        printf "   Installing SSH: "
        yum install -y -q openssh-server sudo >/dev/null 2>&1
        ssh-keygen -A >/dev/null 2>&1
        printf "done\n"
    elif command -v dnf >/dev/null 2>&1; then
        printf "   Installing SSH: "
        dnf install -y -q openssh-server sudo >/dev/null 2>&1
        ssh-keygen -A >/dev/null 2>&1
        printf "done\n"
    else
        echo "Error: Unsupported package manager. Please use an image based on Debian, Ubuntu, Alpine, RHEL, or CentOS."
        exit 1
    fi
fi

# Configure SSH server
mkdir -p /root/.ssh
chmod 700 /root/.ssh
mkdir -p /run/sshd

# Generate host keys if missing
if [ ! -f /etc/ssh/ssh_host_rsa_key ]; then
    ssh-keygen -A >/dev/null 2>&1
fi

# Configure sshd_config for secure access
cat > /etc/ssh/sshd_config << 'SSHD_EOF'
# Basic configuration
Port 22
AddressFamily any
ListenAddress 0.0.0.0

# Host keys
HostKey /etc/ssh/ssh_host_rsa_key
HostKey /etc/ssh/ssh_host_ecdsa_key
HostKey /etc/ssh/ssh_host_ed25519_key

# Authentication
PermitRootLogin prohibit-password
PubkeyAuthentication yes
AuthorizedKeysFile .ssh/authorized_keys
PasswordAuthentication no
PermitEmptyPasswords no
ChallengeResponseAuthentication no

# Logging
SyslogFacility AUTH
LogLevel INFO

# Session settings
X11Forwarding yes
PrintMotd no
AcceptEnv LANG LC_*
TCPKeepAlive yes
ClientAliveInterval 60
ClientAliveCountMax 3
UsePAM yes

# Subsystems
Subsystem sftp /usr/lib/openssh/sftp-server
SSHD_EOF

printf "   ✓ SSH configured\n"
`

	// Execute install script in container as root user
	// Many images (e.g., Jupyter) run as non-root by default, but apt-get needs root
	cmd := execCmd("exec", "--user", "root", containerID, "sh", "-c", installScript)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		klog.Errorf("Failed to install SSH in container: %v", err)
		return fmt.Errorf("failed to install SSH: %w", err)
	}

	klog.V(2).Infof("SSH packages installed successfully")

	// Get the container's original PATH from docker inspect
	// This preserves conda/venv paths that are set in the container image
	inspectCmd := execCmd("inspect", "--format", "{{range .Config.Env}}{{println .}}{{end}}", containerID)
	inspectOutput, err := inspectCmd.CombinedOutput()
	originalPath := ""
	if err == nil {
		// Parse environment variables to find PATH
		for _, line := range strings.Split(string(inspectOutput), "\n") {
			if strings.HasPrefix(line, "PATH=") {
				originalPath = strings.TrimPrefix(line, "PATH=")
				break
			}
		}
	}
	if originalPath == "" {
		klog.Warningf("Failed to get container PATH from inspect, using default")
		originalPath = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	klog.V(2).Infof("Container original PATH: %s", originalPath)

	// Write environment variables to /etc/environment
	// This makes them available to SSH login sessions
	// Always write PATH to preserve conda/venv paths from container image
	// LD_PRELOAD is included here (not as docker -e) to prevent loading into sshd
	klog.V(2).Infof("Writing environment variables to /etc/environment")
	var envLines []string

	// First, write PATH using the container's original PATH (preserves conda/venv)
	envLines = append(envLines, fmt.Sprintf(`PATH="%s"`, originalPath))

	// Then write TensorFusion environment variables if present
	for k, v := range envVars {
		// Skip PATH since we already added it above
		if k == "PATH" {
			continue
		}
		// Write all TensorFusion and LD_PRELOAD environment variables
		// LD_PRELOAD is written here to only affect user shells, not system daemons
		if strings.HasPrefix(k, "TENSOR_FUSION_") || strings.HasPrefix(k, "TF_") || k == EnvLDPreload || k == EnvLDLibraryPath {
			// Escape quotes in value
			escapedValue := strings.ReplaceAll(v, `"`, `\"`)
			envLines = append(envLines, fmt.Sprintf(`%s="%s"`, k, escapedValue))
		}
	}

	if len(envLines) > 0 {
		envContent := strings.Join(envLines, "\n")
		// First backup and remove existing PATH line, then append all variables
		// Execute as root since /etc/environment requires elevated permissions
		writeEnvCmd := execCmd("exec", "--user", "root", containerID, "sh", "-c",
			fmt.Sprintf("grep -v '^PATH=' /etc/environment > /etc/environment.tmp 2>/dev/null || touch /etc/environment.tmp; echo '%s' >> /etc/environment.tmp && mv /etc/environment.tmp /etc/environment", envContent))
		if output, err := writeEnvCmd.CombinedOutput(); err != nil {
			klog.Warningf("Failed to write environment variables (non-fatal): %v, output: %s", err, string(output))
		} else {
			klog.V(2).Infof("Successfully wrote environment variables to /etc/environment")
		}
	}

	// Add SSH public key if provided
	if sshPublicKey != "" {
		klog.V(2).Infof("Adding SSH public key to container")
		// Execute as root to write to /root/.ssh/authorized_keys
		addKeyCmd := execCmd("exec", "--user", "root", containerID, "sh", "-c",
			fmt.Sprintf("echo '%s' >> /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys && chown root:root /root/.ssh/authorized_keys", sshPublicKey))
		if output, err := addKeyCmd.CombinedOutput(); err != nil {
			klog.Warningf("Failed to add SSH key (non-fatal): %v, output: %s", err, string(output))
		}
	}

	// Start SSH daemon in background
	// LD_PRELOAD is now set in /etc/environment (not /etc/ld.so.preload)
	// so it only affects user shells, not the sshd daemon itself
	// Execute as root since sshd requires elevated permissions
	klog.V(2).Infof("Starting SSH daemon in container")
	startSSHCmd := execCmd("exec", "-d", "--user", "root", containerID, "/usr/sbin/sshd", "-D")
	if output, err := startSSHCmd.CombinedOutput(); err != nil {
		klog.Errorf("Failed to start SSH daemon: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to start SSH daemon: %w\nOutput: %s", err, string(output))
	}

	klog.Infof("SSH server successfully configured and started in container %s", containerID)
	return nil
}
