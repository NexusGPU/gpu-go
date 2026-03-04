//go:build windows

package worker

import (
	"os"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"k8s.io/klog/v2"
)

var (
	workerJobOnce   sync.Once
	workerJobHandle windows.Handle
)

// initWorkerJobObject creates a Windows Job Object with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE.
// All worker processes assigned to this job are automatically killed when ggo.exe exits,
// regardless of how ggo.exe terminates (graceful shutdown, taskkill /f, crash, etc.).
func initWorkerJobObject() windows.Handle {
	workerJobOnce.Do(func() {
		handle, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			klog.Warningf("Failed to create Windows Job Object for workers: %v", err)
			return
		}

		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE

		if _, err := windows.SetInformationJobObject(
			handle,
			windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		); err != nil {
			klog.Warningf("Failed to configure Job Object limits: %v", err)
			_ = windows.CloseHandle(handle)
			return
		}

		workerJobHandle = handle
		klog.V(4).Info("Worker Job Object created with KILL_ON_JOB_CLOSE")
	})
	return workerJobHandle
}

// setSysProcAttr configures platform-specific process attributes for the command.
// On Windows, process group handling differs from Unix; Job Object assignment is
// performed after process start via postStart instead.
func setSysProcAttr(_ *exec.Cmd) {}

// postStart assigns the newly started worker process to the shared Job Object so
// it is automatically killed when ggo.exe exits for any reason.
func postStart(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}

	job := initWorkerJobObject()
	if job == 0 {
		klog.Warning("Worker Job Object unavailable; worker process may outlive ggo.exe on unexpected exit")
		return
	}

	handle, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(cmd.Process.Pid),
	)
	if err != nil {
		klog.Warningf("Failed to open worker process handle for Job Object: pid=%d error=%v", cmd.Process.Pid, err)
		return
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	if err := windows.AssignProcessToJobObject(job, handle); err != nil {
		klog.Warningf("Failed to assign worker process to Job Object: pid=%d error=%v", cmd.Process.Pid, err)
		return
	}

	klog.V(4).Infof("Worker process assigned to Job Object: pid=%d", cmd.Process.Pid)
}

// sendTermSignal sends a termination signal to the process.
// On Windows, os.Interrupt is the closest equivalent to SIGTERM.
func sendTermSignal(process *os.Process) error {
	return process.Signal(os.Interrupt)
}
