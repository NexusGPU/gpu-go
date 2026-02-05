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

// sendTermSignal sends a termination signal to the process.
// On Unix, this sends SIGTERM.
func sendTermSignal(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}
