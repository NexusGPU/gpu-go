//go:build windows

package worker

import (
	"os"
	"os/exec"
)

// setSysProcAttr configures platform-specific process attributes for the command.
// On Windows, process groups work differently; no special setup is needed.
func setSysProcAttr(_ *exec.Cmd) {
	// On Windows, process group handling is different.
	// No special SysProcAttr configuration is needed for basic process management.
}

// sendTermSignal sends a termination signal to the process.
// On Windows, we use os.Interrupt which is the closest equivalent to SIGTERM.
func sendTermSignal(process *os.Process) error {
	return process.Signal(os.Interrupt)
}
