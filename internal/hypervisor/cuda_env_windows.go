//go:build windows

package hypervisor

// ensureCUDALibraryPath is a no-op on Windows.
// Windows finds CUDA DLLs via the system PATH which is typically set by the NVIDIA installer.
func ensureCUDALibraryPath() {}
