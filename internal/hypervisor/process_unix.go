//go:build unix

package hypervisor

import (
	"syscall"

	"k8s.io/klog/v2"
)

// isProcessRunning checks if a process with the given PID is still running
func isProcessRunning(pid int) bool {
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}

// forceKillWorkerProcess force kills a worker process and its process group
func forceKillWorkerProcess(pid int) {
	// First check if the main process is still running
	if err := syscall.Kill(pid, syscall.Signal(0)); err == nil {
		// Process is still running, force kill the entire process group
		// Using negative PID kills all processes in the process group
		if killErr := syscall.Kill(-pid, syscall.SIGKILL); killErr != nil {
			// If process group kill fails (e.g., not a process group leader), try killing just the process
			klog.V(4).Infof("Process group kill failed (pid=%d), trying single process kill: %v", pid, killErr)
			if killErr := syscall.Kill(pid, syscall.SIGKILL); killErr != nil {
				klog.Warningf("Failed to force kill worker process: pid=%d error=%v", pid, killErr)
			} else {
				klog.Infof("Force killed worker process: pid=%d", pid)
			}
		} else {
			klog.Infof("Force killed worker process group: pgid=%d", pid)
		}
	}
}
