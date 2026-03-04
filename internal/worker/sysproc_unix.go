//go:build unix

package worker

import (
	"os"
	"os/exec"
	"syscall"
)

// setSysProcAttr configures platform-specific process attributes for the command.
// On Unix, this sets up process group for proper cleanup.
func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

// postStart performs any platform-specific post-start setup.
// On Unix, no additional setup is needed; Setpgid handles process group management.
func postStart(_ *exec.Cmd) {}

// sendTermSignal sends a termination signal to the process.
// On Unix, this sends SIGTERM.
func sendTermSignal(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}
