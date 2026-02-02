package utils

import (
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
)

// CheckPortAvailability checks if a port is available.
// If occupied, returns the PID using it (if resolvable) and an error indicating it's in use.
// If available, returns 0 and nil.
func CheckPortAvailability(port int) (int, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err == nil {
		_ = ln.Close()
		return 0, nil
	}

	// Port is likely in use, try to find PID
	pid := 0

	// Try lsof first (works on macOS and Linux with lsof installed)
	// -t: terse mode (PID only)
	cmd := exec.Command("lsof", "-i", fmt.Sprintf(":%d", port), "-t")
	out, err := cmd.Output()
	if err == nil {
		// Output might contain multiple PIDs if multiple threads/processes, take the first one
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 && lines[0] != "" {
			if p, err := strconv.Atoi(lines[0]); err == nil {
				pid = p
			}
		}
	}

	// If lsof failed or didn't find it, we could try other tools,
	// but lsof is the most reliable cross-platform (Unix) way if installed.
	// Returning 0 PID with error is acceptable fallback.

	return pid, fmt.Errorf("port %d is already in use", port)
}
