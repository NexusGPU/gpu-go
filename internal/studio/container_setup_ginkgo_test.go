package studio

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Container setup SSH mounts", func() {
	It("skips SSH mounts when configured", func() {
		tempDir := GinkgoT().TempDir()
		sshDir := filepath.Join(tempDir, ".ssh")
		Expect(os.MkdirAll(sshDir, 0700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(sshDir, "id_ed25519.pub"), []byte("ssh-ed25519 AAAATEST"), 0600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("PRIVATE"), 0600)).To(Succeed())

		oldHome := os.Getenv("HOME")
		Expect(os.Setenv("HOME", tempDir)).To(Succeed())
		DeferCleanup(func() {
			_ = os.Setenv("HOME", oldHome)
		})

		result, err := SetupContainerGPUEnv(GinkgoT().Context(), &ContainerSetupConfig{
			StudioName:     "ssh-skip-test",
			MountUserHome:  false,
			SkipSSHMounts:  true,
			GPUWorkerURL:   "",
			HardwareVendor: "",
		})
		Expect(err).NotTo(HaveOccurred())

		for _, vol := range result.VolumeMounts {
			Expect(vol.ContainerPath).NotTo(HavePrefix("/root/.ssh/"))
		}
	})

	It("includes SSH mounts by default when keys are present", func() {
		tempDir := GinkgoT().TempDir()
		sshDir := filepath.Join(tempDir, ".ssh")
		Expect(os.MkdirAll(sshDir, 0700)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(sshDir, "id_ed25519.pub"), []byte("ssh-ed25519 AAAATEST"), 0600)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("PRIVATE"), 0600)).To(Succeed())

		oldHome := os.Getenv("HOME")
		Expect(os.Setenv("HOME", tempDir)).To(Succeed())
		DeferCleanup(func() {
			_ = os.Setenv("HOME", oldHome)
		})

		result, err := SetupContainerGPUEnv(GinkgoT().Context(), &ContainerSetupConfig{
			StudioName:     "ssh-default-test",
			MountUserHome:  false,
			GPUWorkerURL:   "",
			HardwareVendor: "",
		})
		Expect(err).NotTo(HaveOccurred())

		found := false
		for _, vol := range result.VolumeMounts {
			if strings.HasPrefix(vol.ContainerPath, "/root/.ssh/") {
				found = true
				break
			}
		}
		Expect(found).To(BeTrue())
	})
})
