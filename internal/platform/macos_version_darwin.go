//go:build darwin

package platform

import (
	"strconv"
	"strings"
	"syscall"
)

// MacOSMajorVersion returns the macOS major version (e.g., 26).
// Returns 0 when the version cannot be determined.
func MacOSMajorVersion() int {
	version, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return 0
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return 0
	}
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return 0
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return major
}
