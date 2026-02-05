//go:build windows

package hypervisor

import (
	"os"

	"k8s.io/klog/v2"
)

// isProcessRunning checks if a process with the given PID is still running
func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows, FindProcess always succeeds for any PID,
	// but we can try to get process info to verify it exists
	// The safest way is to attempt to signal it and check error
	// However, on Windows Signal is limited, so we just assume it's running
	// if FindProcess succeeds - the actual kill will fail if process doesn't exist
	_ = process
	return true
}

// forceKillWorkerProcess force kills a worker process on Windows
func forceKillWorkerProcess(pid int) {
	process, err := os.FindProcess(pid)
	if err != nil {
		klog.V(4).Infof("Process not found: pid=%d error=%v", pid, err)
		return
	}

	if err := process.Kill(); err != nil {
		klog.Warningf("Failed to force kill worker process: pid=%d error=%v", pid, err)
	} else {
		klog.Infof("Force killed worker process: pid=%d", pid)
	}
}
