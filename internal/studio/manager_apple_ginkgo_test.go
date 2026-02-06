//go:build darwin

package studio

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Apple container selection", func() {
	It("prefers apple-container on macOS 26+ when no Docker socket is present", func() {
		if runtime.GOOS != OSDarwin {
			Skip("darwin only")
		}
		if currentMacOSMajorVersion() < 26 {
			Skip("macOS 26+ only")
		}
		if testDockerSocketExists() {
			Skip("docker socket present on host")
		}
		Expect(os.Unsetenv("DOCKER_HOST")).To(Succeed())

		m := NewManager()
		m.RegisterBackend(&MockBackend{mode: ModeAppleContainer, available: true})
		m.RegisterBackend(&MockBackend{mode: ModeDocker, available: true})

		backend, err := m.GetBackend(ModeAuto)
		Expect(err).NotTo(HaveOccurred())
		Expect(backend.Mode()).To(Equal(ModeAppleContainer))
	})

	It("prefers docker when Docker socket exists on macOS 26+", func() {
		if runtime.GOOS != OSDarwin {
			Skip("darwin only")
		}
		if currentMacOSMajorVersion() < 26 {
			Skip("macOS 26+ only")
		}

		tempDir := GinkgoT().TempDir()
		sockPath := filepath.Join(tempDir, "docker.sock")
		listener, err := net.Listen("unix", sockPath)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = listener.Close()
			_ = os.Remove(sockPath)
		})
		Expect(os.Setenv("DOCKER_HOST", "unix://"+sockPath)).To(Succeed())
		DeferCleanup(func() {
			_ = os.Unsetenv("DOCKER_HOST")
		})

		m := NewManager()
		m.RegisterBackend(&MockBackend{mode: ModeAppleContainer, available: true})
		m.RegisterBackend(&MockBackend{mode: ModeDocker, available: true})

		backend, err := m.GetBackend(ModeAuto)
		Expect(err).NotTo(HaveOccurred())
		Expect(backend.Mode()).To(Equal(ModeDocker))
	})

	It("rejects apple-container on macOS < 26 when explicitly requested", func() {
		if runtime.GOOS != OSDarwin {
			Skip("darwin only")
		}
		if currentMacOSMajorVersion() >= 26 {
			Skip("macOS < 26 only")
		}

		m := NewManager()
		m.RegisterBackend(&MockBackend{mode: ModeAppleContainer, available: true})

		_, err := m.GetBackend(ModeAppleContainer)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("macOS 26"))
	})
})

func currentMacOSMajorVersion() int {
	version, err := syscall.Sysctl("kern.osproductversion")
	if err != nil {
		return 0
	}
	parts := strings.Split(strings.TrimSpace(version), ".")
	if len(parts) == 0 {
		return 0
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return major
}

func testDockerSocketExists() bool {
	if dockerHost := os.Getenv("DOCKER_HOST"); dockerHost != "" {
		if strings.HasPrefix(dockerHost, "unix://") {
			if _, err := os.Stat(strings.TrimPrefix(dockerHost, "unix://")); err == nil {
				return true
			}
		}
	}
	if _, err := os.Stat("/var/run/docker.sock"); err == nil {
		return true
	}
	homeDir, err := os.UserHomeDir()
	if err == nil {
		colimaGlob := filepath.Join(homeDir, ".colima", "*", "docker.sock")
		if matches, err := filepath.Glob(colimaGlob); err == nil {
			for _, match := range matches {
				if _, err := os.Stat(match); err == nil {
					return true
				}
			}
		}
		orbstackSock := filepath.Join(homeDir, ".orbstack", "run", "docker.sock")
		if _, err := os.Stat(orbstackSock); err == nil {
			return true
		}
	}
	return false
}
