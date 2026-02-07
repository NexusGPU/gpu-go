package studio

import (
	"context"
	"os"

	"github.com/NexusGPU/gpu-go/internal/platform"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Manager RemoveAll", func() {
	var (
		mgr    *Manager
		tmpDir string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "ggo-studio-remove-all-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = os.RemoveAll(tmpDir)
		})

		mgr = &Manager{
			paths:    platform.DefaultPaths().WithConfigDir(tmpDir),
			backends: make(map[Mode]Backend),
		}
	})

	It("removes runtime environments and stale offline state in one batch", func() {
		dockerBackend := &MockBackend{
			mode:      ModeDocker,
			available: true,
			envs: map[string]*Environment{
				"env-runtime": {
					ID:     "env-runtime",
					Name:   "runtime",
					Mode:   ModeDocker,
					Status: StatusRunning,
				},
			},
		}
		mgr.RegisterBackend(dockerBackend)
		mgr.RegisterBackend(&MockBackend{mode: ModeColima, available: false})

		stateEnvRuntime := &Environment{ID: "env-runtime", Name: "runtime", Mode: ModeDocker, Status: StatusRunning}
		stateEnvOffline := &Environment{ID: "env-offline", Name: "offline", Mode: ModeColima, Status: StatusStopped}
		Expect(mgr.saveState(map[string]*Environment{
			stateEnvRuntime.ID: stateEnvRuntime,
			stateEnvOffline.ID: stateEnvOffline,
		})).To(Succeed())

		removed, err := mgr.RemoveAll(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(removed).To(ConsistOf("runtime", "offline"))

		_, runtimeExists := dockerBackend.envs["env-runtime"]
		Expect(runtimeExists).To(BeFalse())

		state, err := mgr.loadState()
		Expect(err).NotTo(HaveOccurred())
		Expect(state).To(BeEmpty())
	})
})
