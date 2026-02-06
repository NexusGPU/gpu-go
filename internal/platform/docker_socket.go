package platform

import (
	"os"
	"path/filepath"
	"strings"
)

// HasDockerSocket checks common Docker socket locations for macOS runtimes.
func HasDockerSocket() bool {
	if socketPath := dockerHostSocketPath(os.Getenv("DOCKER_HOST")); socketPath != "" {
		if fileExists(socketPath) {
			return true
		}
	}

	if fileExists("/var/run/docker.sock") {
		return true
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	colimaGlob := filepath.Join(homeDir, ".colima", "*", "docker.sock")
	if matches, err := filepath.Glob(colimaGlob); err == nil {
		for _, match := range matches {
			if fileExists(match) {
				return true
			}
		}
	}

	orbstackSock := filepath.Join(homeDir, ".orbstack", "run", "docker.sock")
	return fileExists(orbstackSock)
}

func dockerHostSocketPath(dockerHost string) string {
	if dockerHost == "" {
		return ""
	}
	if !strings.HasPrefix(dockerHost, "unix://") {
		return ""
	}
	return strings.TrimPrefix(dockerHost, "unix://")
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
