//go:build !darwin

package platform

// MacOSMajorVersion returns 0 on non-macOS platforms.
func MacOSMajorVersion() int {
	return 0
}
