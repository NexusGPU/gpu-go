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
func setupSSHInContainer(ctx context.Context, dockerCmd, containerID, sshPublicKey, dockerHost string) error {
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
	installScript := `#!/bin/sh
set -e

# Detect package manager and install openssh-server
if command -v apt-get >/dev/null 2>&1; then
    echo "Detected Debian/Ubuntu, installing openssh-server..."
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq openssh-server sudo > /dev/null 2>&1
    mkdir -p /run/sshd
elif command -v apk >/dev/null 2>&1; then
    echo "Detected Alpine, installing openssh..."
    apk add --no-cache openssh sudo > /dev/null 2>&1
    ssh-keygen -A > /dev/null 2>&1  # Generate host keys
elif command -v yum >/dev/null 2>&1; then
    echo "Detected RHEL/CentOS, installing openssh-server..."
    yum install -y -q openssh-server sudo > /dev/null 2>&1
    ssh-keygen -A > /dev/null 2>&1  # Generate host keys
elif command -v dnf >/dev/null 2>&1; then
    echo "Detected Fedora, installing openssh-server..."
    dnf install -y -q openssh-server sudo > /dev/null 2>&1
    ssh-keygen -A > /dev/null 2>&1  # Generate host keys
else
    echo "Error: Unsupported package manager. Please use an image based on Debian, Ubuntu, Alpine, RHEL, or CentOS."
    exit 1
fi

# Configure SSH server
mkdir -p /root/.ssh
chmod 700 /root/.ssh

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

echo "SSH setup completed successfully"
`

	// Execute install script in container
	cmd := execCmd("exec", containerID, "sh", "-c", installScript)
	output, err := cmd.CombinedOutput()
	if err != nil {
		klog.Errorf("Failed to install SSH in container: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to install SSH: %w\nOutput: %s", err, string(output))
	}

	klog.V(2).Infof("SSH packages installed: %s", strings.TrimSpace(string(output)))

	// Add SSH public key if provided
	if sshPublicKey != "" {
		klog.V(2).Infof("Adding SSH public key to container")
		addKeyCmd := execCmd("exec", containerID, "sh", "-c",
			fmt.Sprintf("echo '%s' >> /root/.ssh/authorized_keys && chmod 600 /root/.ssh/authorized_keys && chown root:root /root/.ssh/authorized_keys", sshPublicKey))
		if output, err := addKeyCmd.CombinedOutput(); err != nil {
			klog.Warningf("Failed to add SSH key (non-fatal): %v, output: %s", err, string(output))
		}
	}

	// Start SSH daemon in background
	// Note: With CAP_SYS_ADMIN capability (added in backend_docker.go), SSH works correctly
	// even with TensorFusion GPU libraries loaded via /etc/ld.so.preload
	klog.V(2).Infof("Starting SSH daemon in container")
	startSSHCmd := execCmd("exec", "-d", containerID, "/usr/sbin/sshd", "-D")
	if output, err := startSSHCmd.CombinedOutput(); err != nil {
		klog.Errorf("Failed to start SSH daemon: %v, output: %s", err, string(output))
		return fmt.Errorf("failed to start SSH daemon: %w\nOutput: %s", err, string(output))
	}

	klog.Infof("SSH server successfully configured and started in container %s", containerID)
	return nil
}
