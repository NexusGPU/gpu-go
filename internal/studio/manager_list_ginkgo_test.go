package studio

import (
	"context"
	"os"

	"github.com/NexusGPU/gpu-go/internal/platform"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Manager List", func() {
	var (
		mgr    *Manager
		tmpDir string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "ggo-studio-list-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = os.RemoveAll(tmpDir)
		})

		mgr = &Manager{
			paths:    platform.DefaultPaths().WithConfigDir(tmpDir),
			backends: make(map[Mode]Backend),
		}
	})

	It("marks state-only envs as deleted when runtime is available", func() {
		runtimeEnv := &Environment{
			ID:     "env-runtime",
			Name:   "runtime",
			Mode:   ModeDocker,
			Status: StatusRunning,
		}
		backend := &MockBackend{
			mode:      ModeDocker,
			available: true,
			listFunc: func(ctx context.Context) ([]*Environment, error) {
				return []*Environment{runtimeEnv}, nil
			},
		}
		mgr.RegisterBackend(backend)

		stateEnv := &Environment{
			ID:     "env-deleted",
			Name:   "deleted",
			Mode:   ModeDocker,
			Status: StatusStopped,
		}
		Expect(mgr.saveState(map[string]*Environment{stateEnv.ID: stateEnv})).To(Succeed())

		envs, err := mgr.List(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(envs).To(HaveLen(2))

		var foundRuntime *Environment
		var foundDeleted *Environment
		for _, env := range envs {
			switch env.ID {
			case runtimeEnv.ID:
				foundRuntime = env
			case stateEnv.ID:
				foundDeleted = env
			}
		}

		Expect(foundRuntime).NotTo(BeNil())
		Expect(foundRuntime.Status).To(Equal(StatusRunning))
		Expect(foundDeleted).NotTo(BeNil())
		Expect(foundDeleted.Status).To(Equal(EnvironmentStatus("deleted")))
	})

	It("marks state envs as unknown when runtime is offline", func() {
		backend := &MockBackend{
			mode:      ModeDocker,
			available: false,
		}
		mgr.RegisterBackend(backend)

		stateEnv := &Environment{
			ID:     "env-offline",
			Name:   "offline",
			Mode:   ModeDocker,
			Status: StatusStopped,
		}
		Expect(mgr.saveState(map[string]*Environment{stateEnv.ID: stateEnv})).To(Succeed())

		envs, err := mgr.List(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(envs).To(HaveLen(1))
		Expect(envs[0].ID).To(Equal(stateEnv.ID))
		Expect(envs[0].Status).To(Equal(EnvironmentStatus("unknown")))
	})
})
