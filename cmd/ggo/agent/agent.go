package agent

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/NexusGPU/gpu-go/internal/agent"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/NexusGPU/gpu-go/internal/deps"
	"github.com/NexusGPU/gpu-go/internal/hypervisor"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/NexusGPU/gpu-go/internal/tui"
	tfv1 "github.com/NexusGPU/tensor-fusion/api/v1"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var (
	configDir      string
	stateDir       string
	serverURL      string
	outputFormat   string
	acceleratorLib string
	isolationMode  string
	paths          = platform.DefaultPaths()

	// Hypervisor singleton
	hypervisorOnce    sync.Once
	hypervisorManager *hypervisor.Manager
	hypervisorErr     error
)

// NewAgentCmd creates the agent command
func NewAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Manage GPU agent on the server side",
		Long:  `The agent command manages the GPU agent that runs on GPU servers to sync with the cloud platform.`,
	}

	cmd.PersistentFlags().StringVar(&configDir, "config-dir", paths.ConfigDir(), "Configuration directory")
	cmd.PersistentFlags().StringVar(&stateDir, "state-dir", paths.StateDir(), "State directory for tensor-fusion")
	cmd.PersistentFlags().StringVar(&serverURL, "server", api.GetDefaultBaseURL(), "Server URL (or set GPU_GO_ENDPOINT env var)")
	cmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json)")
	cmd.PersistentFlags().StringVar(&acceleratorLib, "accelerator-lib", "", "Path to accelerator library (auto-detected if not specified)")
	cmd.PersistentFlags().StringVar(&isolationMode, "isolation-mode", "shared", "Worker isolation mode (shared, soft, partitioned)")

	cmd.AddCommand(newRegisterCmd())
	cmd.AddCommand(newStartCmd())
	cmd.AddCommand(newStatusCmd())

	return cmd
}

func getOutput() *tui.Output {
	return tui.NewOutputWithFormat(tui.ParseOutputFormat(outputFormat))
}

func getIsolationMode() tfv1.IsolationModeType {
	switch isolationMode {
	case "soft":
		return tfv1.IsolationModeSoft
	case "partitioned":
		return tfv1.IsolationModePartitioned
	default:
		return tfv1.IsolationModeShared
	}
}

// getHypervisorManager returns the singleton hypervisor manager, initializing it if needed
func getHypervisorManager() (*hypervisor.Manager, error) {
	hypervisorOnce.Do(func() {
		if os.Getenv("GPU_GO_MOCK_GPUS") != "" {
			hypervisorErr = fmt.Errorf("mock mode enabled, hypervisor not available")
			return
		}

		libPath := acceleratorLib
		if libPath == "" {
			var err error
			libPath, err = agent.DownloadOrFindAccelerator()
			if err != nil {
				hypervisorErr = fmt.Errorf("failed to find or download accelerator library: %w", err)
				return
			}
			if libPath == "" {
				hypervisorErr = fmt.Errorf("accelerator library not found")
				return
			}
		}

		hypervisorManager, hypervisorErr = hypervisor.NewManager(hypervisor.Config{
			LibPath:       libPath,
			Vendor:        agent.DetectVendorFromLibPath(libPath),
			IsolationMode: getIsolationMode(),
			StateDir:      stateDir,
		})
		if hypervisorErr != nil {
			return
		}
		hypervisorErr = hypervisorManager.Start()
	})
	return hypervisorManager, hypervisorErr
}

// stopHypervisorManager stops the singleton hypervisor manager if running
func stopHypervisorManager() {
	if hypervisorManager != nil {
		hypervisorManager.Stop()
	}
}

func newRegisterCmd() *cobra.Command {
	var token string

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register agent with the server",
		Long:  `Register this GPU server as an agent with the GPU Go platform.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := getOutput()

			if token == "" {
				token = os.Getenv("GPU_GO_TOKEN")
			}
			if token == "" {
				if !out.IsJSON() {
					fmt.Println(tui.ErrorMessage("Token is required. Use --token flag or GPU_GO_TOKEN environment variable"))
				}
				return fmt.Errorf("token is required")
			}

			configMgr := config.NewManager(configDir, stateDir)
			client := api.NewClient(api.WithBaseURL(serverURL))

			// Discover GPUs using hypervisor or mock
			gpus := discoverGPUs()
			if len(gpus) == 0 {
				cmd.SilenceUsage = true
				if !out.IsJSON() {
					fmt.Println(tui.ErrorMessage("No GPUs found. Please check your GPU configuration or use GPU_GO_MOCK_GPUS for testing"))
				}
				return fmt.Errorf("no GPUs found")
			}

			agentInstance := agent.NewAgent(client, configMgr)
			if err := agentInstance.Register(token, gpus); err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to register agent: error=%v", err)
				return err
			}

			if out.IsJSON() {
				return out.PrintJSON(tui.NewActionResult(true, "Agent registered successfully", ""))
			}

			fmt.Println()
			fmt.Println(tui.SuccessMessage("Agent registered successfully!"))
			fmt.Println()
			return nil
		},
	}

	cmd.Flags().StringVarP(&token, "token", "t", "", "Temporary installation token")

	return cmd
}

func newStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the agent daemon",
		Long:  `Start the GPU agent daemon to sync with the cloud platform.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := getOutput()
			configMgr := config.NewManager(configDir, stateDir)

			// Check if agent is registered
			if !configMgr.ConfigExists() {
				cmd.SilenceUsage = true
				if !out.IsJSON() {
					fmt.Println(tui.ErrorMessage("Agent is not registered. Please run 'ggo agent register' first"))
				}
				return agent.ErrNotRegistered
			}

			cfg, err := configMgr.LoadConfig()
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to load config: error=%v", err)
				return err
			}

			client := api.NewClient(
				api.WithBaseURL(serverURL),
				api.WithAgentSecret(cfg.AgentSecret),
			)

			// Get singleton hypervisor manager
			hvMgr, err := getHypervisorManager()
			if err != nil {
				// Log warning but continue - agent can work without hypervisor for some operations
				klog.Fatal("Failed to initialize hypervisor manager, worker management will be limited: error=%v", err)
			}

			// Get remote-gpu-worker binary path
			var workerBinaryPath string
			if hvMgr != nil {
				depsMgr := deps.NewManager(
					deps.WithPaths(paths),
					deps.WithAPIClient(client),
				)
				workerBinaryPath, err = depsMgr.GetRemoteGPUWorkerPath(context.Background())
				if err != nil {
					cmd.SilenceUsage = true
					if !out.IsJSON() {
						fmt.Println(tui.ErrorMessage(fmt.Sprintf("Fatal: Failed to get remote-gpu-worker binary: %v", err)))
						fmt.Println(tui.Muted("The agent requires remote-gpu-worker to manage workers. Please ensure the binary is available for your platform."))
					}
					klog.Fatalf("Fatal: Failed to get remote-gpu-worker path: error=%v", err)
				}
				klog.V(4).Infof("Using remote-gpu-worker binary: path=%s", workerBinaryPath)
			}

			// Create agent with hypervisor integration
			var agentInstance *agent.Agent
			if hvMgr != nil {
				agentInstance = agent.NewAgentWithHypervisor(client, configMgr, hvMgr, workerBinaryPath)
			} else {
				agentInstance = agent.NewAgent(client, configMgr)
			}

			if err := agentInstance.Start(); err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to start agent: error=%v", err)
				return err
			}

			if !out.IsJSON() {
				styles := tui.DefaultStyles()
				fmt.Printf("%s Agent started (ID: %s)\n",
					styles.Success.Render("●"),
					styles.Bold.Render(cfg.AgentID))
				if hvMgr != nil {
					fmt.Printf("%s Hypervisor integration enabled (vendor: %s)\n",
						styles.Success.Render("●"),
						hvMgr.GetVendor())
				}
				fmt.Println(tui.Muted("Press Ctrl+C to stop..."))
			}

			// Wait for interrupt signal
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh

			if !out.IsJSON() {
				fmt.Println()
				fmt.Println(tui.InfoMessage("Shutting down..."))
			}

			agentInstance.Stop()
			stopHypervisorManager()
			return nil
		},
	}

	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show agent status",
		Long:  `Show the current status of the GPU agent.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := getOutput()
			configMgr := config.NewManager(configDir, stateDir)

			cfg, err := configMgr.LoadConfig()
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to load config: error=%v", err)
				return err
			}

			if cfg == nil {
				if out.IsJSON() {
					return out.PrintJSON(map[string]any{
						"registered": false,
						"message":    "Agent is not registered",
					})
				}
				fmt.Println(tui.WarningMessage("Agent is not registered"))
				return nil
			}

			// Load GPUs and workers for status
			gpus, _ := configMgr.LoadGPUs()
			workers, _ := configMgr.LoadWorkers()

			if out.IsJSON() {
				return out.PrintJSON(map[string]any{
					"registered":     true,
					"agent_id":       cfg.AgentID,
					"config_version": cfg.ConfigVersion,
					"server_url":     cfg.ServerURL,
					"gpus":           gpus,
					"workers":        workers,
				})
			}

			// Styled output
			styles := tui.DefaultStyles()

			fmt.Println()
			fmt.Println(styles.Title.Render("Agent Status"))
			fmt.Println()

			status := tui.NewStatusTable().
				Add("Agent ID", cfg.AgentID).
				Add("Config Version", fmt.Sprintf("%d", cfg.ConfigVersion)).
				Add("Server URL", cfg.ServerURL)

			fmt.Println(status.String())

			if len(gpus) > 0 {
				fmt.Println()
				fmt.Println(styles.Subtitle.Render(fmt.Sprintf("GPUs (%d)", len(gpus))))
				fmt.Println()

				var rows [][]string
				for _, gpu := range gpus {
					vram := fmt.Sprintf("%.1f GB", float64(gpu.VRAMMb)/1024)
					rows = append(rows, []string{
						gpu.GPUID,
						gpu.Vendor,
						gpu.Model,
						vram,
					})
				}

				table := tui.NewTable().
					Headers("ID", "VENDOR", "MODEL", "VRAM").
					Rows(rows)

				fmt.Println(table.String())
			}

			if len(workers) > 0 {
				fmt.Println()
				fmt.Println(styles.Subtitle.Render(fmt.Sprintf("Workers (%d)", len(workers))))
				fmt.Println()

				var rows [][]string
				for _, w := range workers {
					status := w.Status
					if status == "" {
						status = "unknown"
					}
					statusIcon := tui.StatusIcon(status)
					statusStyled := styles.StatusStyle(status).Render(statusIcon + " " + status)

					enabledIcon := tui.StatusIcon(boolToYesNo(w.Enabled))
					enabledStyled := styles.StatusStyle(boolToYesNo(w.Enabled)).Render(enabledIcon)

					rows = append(rows, []string{
						w.WorkerID,
						fmt.Sprintf("%d", w.ListenPort),
						enabledStyled,
						statusStyled,
					})
				}

				table := tui.NewTable().
					Headers("ID", "PORT", "ENABLED", "STATUS").
					Rows(rows)

				fmt.Println(table.String())
			}

			return nil
		},
	}

	return cmd
}

// discoverGPUs discovers GPUs using hypervisor or returns mock GPUs
func discoverGPUs() []api.GPUInfo {
	// Check for mock GPUs environment variable
	if mockCount := os.Getenv("GPU_GO_MOCK_GPUS"); mockCount != "" {
		count, err := strconv.Atoi(mockCount)
		if err != nil || count <= 0 {
			count = 1
		}
		klog.Infof("Using mock GPUs for testing: count=%d", count)
		return agent.CreateMockGPUs(count)
	}

	// Use singleton hypervisor manager
	hvMgr, err := getHypervisorManager()
	if err != nil {
		klog.Warningf("Failed to get hypervisor manager: error=%v", err)
		return nil
	}

	devices, err := hvMgr.ListDevices()
	if err != nil {
		klog.Errorf("Failed to list devices: error=%v", err)
		return nil
	}

	return agent.ConvertDevicesToGPUInfo(devices)
}

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
