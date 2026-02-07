package studio

import (
	"context"
	"net"
	"os"
	"time"

	"github.com/NexusGPU/gpu-go/internal/platform"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Manager Create", func() {
	var (
		mgr    *Manager
		tmpDir string
	)

	BeforeEach(func() {
		var err error
		tmpDir, err = os.MkdirTemp("", "ggo-studio-create-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = os.RemoveAll(tmpDir)
		})

		mgr = &Manager{
			paths:    platform.DefaultPaths().WithConfigDir(tmpDir),
			backends: make(map[Mode]Backend),
		}

		oldStable := createStabilityWindow
		oldPoll := createStabilityPollInterval
		oldProbe := createSSHProbeTimeout
		createStabilityWindow = 60 * time.Millisecond
		createStabilityPollInterval = 10 * time.Millisecond
		createSSHProbeTimeout = 5 * time.Millisecond
		DeferCleanup(func() {
			createStabilityWindow = oldStable
			createStabilityPollInterval = oldPoll
			createSSHProbeTimeout = oldProbe
		})
	})

	It("fails when environment leaves running state during startup window", func() {
		backend := &MockBackend{
			mode:      ModeDocker,
			available: true,
			createFunc: func(ctx context.Context, opts *CreateOptions) (*Environment, error) {
				return &Environment{ID: "env-1", Name: opts.Name, Mode: ModeDocker, Status: StatusRunning}, nil
			},
			getFunc: func(ctx context.Context, idOrName string) (*Environment, error) {
				return &Environment{ID: "env-1", Name: "my-studio", Mode: ModeDocker, Status: StatusStopped}, nil
			},
		}
		mgr.RegisterBackend(backend)

		_, err := mgr.Create(context.Background(), &CreateOptions{Name: "my-studio", Mode: ModeDocker})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("status=stopped"))
		Expect(err.Error()).To(ContainSubstring("ggo studio logs my-studio"))
	})

	It("waits for container to transition from pending to running", func() {
		getCalls := 0
		backend := &MockBackend{
			mode:      ModeDocker,
			available: true,
			createFunc: func(ctx context.Context, opts *CreateOptions) (*Environment, error) {
				return &Environment{ID: "env-pending", Name: opts.Name, Mode: ModeDocker, Status: StatusRunning}, nil
			},
			getFunc: func(ctx context.Context, idOrName string) (*Environment, error) {
				getCalls++
				status := StatusPending
				if getCalls > 2 {
					status = StatusRunning
				}
				return &Environment{ID: "env-pending", Name: "pending-studio", Mode: ModeDocker, Status: status}, nil
			},
		}
		mgr.RegisterBackend(backend)

		_, err := mgr.Create(context.Background(), &CreateOptions{Name: "pending-studio", Mode: ModeDocker})
		Expect(err).NotTo(HaveOccurred())
		Expect(getCalls).To(BeNumerically(">", 2))
	})

	It("keeps SSH info only when the published SSH port is reachable", func() {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			_ = listener.Close()
		})

		port := listener.Addr().(*net.TCPAddr).Port
		backend := &MockBackend{
			mode:      ModeDocker,
			available: true,
			createFunc: func(ctx context.Context, opts *CreateOptions) (*Environment, error) {
				return &Environment{ID: "env-2", Name: opts.Name, Mode: ModeDocker, Status: StatusRunning, SSHHost: "127.0.0.1", SSHPort: port, SSHUser: "root"}, nil
			},
			getFunc: func(ctx context.Context, idOrName string) (*Environment, error) {
				return &Environment{
					ID:      "env-2",
					Name:    "with-ssh",
					Mode:    ModeDocker,
					Status:  StatusRunning,
					SSHHost: "127.0.0.1",
					SSHPort: port,
					SSHUser: "root",
				}, nil
			},
		}
		mgr.RegisterBackend(backend)

		env, err := mgr.Create(context.Background(), &CreateOptions{Name: "with-ssh", Mode: ModeDocker})
		Expect(err).NotTo(HaveOccurred())
		Expect(env.SSHPort).To(Equal(port))

		_ = listener.Close()

		envNoSSH, err := mgr.Create(context.Background(), &CreateOptions{Name: "with-ssh", Mode: ModeDocker})
		Expect(err).NotTo(HaveOccurred())
		Expect(envNoSSH.SSHPort).To(BeZero())
		Expect(envNoSSH.SSHHost).To(BeEmpty())
		Expect(envNoSSH.SSHUser).To(BeEmpty())
	})
})
