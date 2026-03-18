//go:build unix

package hypervisor

import (
	"os"
	"strings"

	"k8s.io/klog/v2"
)

// cudaLibSearchPaths lists common directories where CUDA libraries are installed.
var cudaLibSearchPaths = []string{
	"/usr/local/cuda/lib64",
	"/usr/local/cuda/lib",
	"/usr/lib/x86_64-linux-gnu",
	"/usr/lib/aarch64-linux-gnu",
	"/usr/lib64",
}

// ensureCUDALibraryPath appends known CUDA library directories to LD_LIBRARY_PATH
// when they exist and contain libcuda.so. This is necessary for non-interactive
// environments such as systemd services where the user's shell profile is not sourced.
func ensureCUDALibraryPath() {
	current := os.Getenv("LD_LIBRARY_PATH")
	existing := make(map[string]bool)
	for _, p := range strings.Split(current, ":") {
		if p != "" {
			existing[p] = true
		}
	}

	var added []string
	for _, dir := range cudaLibSearchPaths {
		if existing[dir] {
			continue
		}
		// Check if the directory contains CUDA libraries
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), "libcuda.so") || strings.HasPrefix(e.Name(), "libnvidia") {
				added = append(added, dir)
				break
			}
		}
	}

	if len(added) == 0 {
		return
	}

	newPath := current
	for _, dir := range added {
		if newPath == "" {
			newPath = dir
		} else {
			newPath = newPath + ":" + dir
		}
	}
	if err := os.Setenv("LD_LIBRARY_PATH", newPath); err != nil {
		klog.Warningf("Failed to set LD_LIBRARY_PATH: %v", err)
		return
	}
	klog.Infof("Updated LD_LIBRARY_PATH with CUDA library paths: added=%v", added)
}
