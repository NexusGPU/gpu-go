package agent

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/NexusGPU/gpu-go/internal/agent"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	configDir      string
	stateDir       string
	serverURL      string
	outputFormat   string
	acceleratorLib string
	workerBinary   string
	singleNode     bool
	paths          = platform.DefaultPaths()
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
	cmd.PersistentFlags().StringVar(&workerBinary, "worker-binary", "tensor-fusion-worker", "Path to worker binary")
	cmd.PersistentFlags().BoolVar(&singleNode, "single-node", false, "Enable single-node mode with local worker management")

	cmd.AddCommand(newRegisterCmd())
	cmd.AddCommand(newStartCmd())
	cmd.AddCommand(newStatusCmd())

	return cmd
}

func getOutput() *tui.Output {
	return tui.NewOutputWithFormat(tui.ParseOutputFormat(outputFormat))
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
				// Show help for missing required parameter
				return fmt.Errorf("token is required")
			}

			configMgr := config.NewManager(configDir, stateDir)
			client := api.NewClient(api.WithBaseURL(serverURL))

			// Discover GPUs (placeholder - in real implementation, use nvml or similar)
			gpus := discoverGPUs()
			if len(gpus) == 0 {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				if !out.IsJSON() {
					fmt.Println(tui.ErrorMessage("No GPUs found. Please check your GPU configuration"))
				}
				return fmt.Errorf("no GPUs found")
			}

			agentInstance := agent.NewAgent(client, configMgr)
			if err := agentInstance.Register(token, gpus); err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				log.Error().Err(err).Msg("Failed to register agent")
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
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				if !out.IsJSON() {
					fmt.Println(tui.ErrorMessage("Agent is not registered. Please run 'ggo agent register' first"))
				}
				return agent.ErrNotRegistered
			}

			cfg, err := configMgr.LoadConfig()
			if err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				log.Error().Err(err).Msg("Failed to load config")
				return err
			}

			client := api.NewClient(
				api.WithBaseURL(serverURL),
				api.WithAgentSecret(cfg.AgentSecret),
			)

			// Create agent with single-node options
			agentInstance := agent.NewAgentWithOptions(client, configMgr, singleNode, workerBinary)
			if err := agentInstance.Start(); err != nil {
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				log.Error().Err(err).Msg("Failed to start agent")
				return err
			}

			if !out.IsJSON() {
				styles := tui.DefaultStyles()
				modeStr := ""
				if singleNode {
					modeStr = " [single-node]"
				}
				fmt.Printf("%s Agent started (ID: %s)%s\n",
					styles.Success.Render("●"),
					styles.Bold.Render(cfg.AgentID),
					modeStr)
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
				// Runtime error - don't show help
				cmd.SilenceUsage = true
				log.Error().Err(err).Msg("Failed to load config")
				return err
			}

			if cfg == nil {
				if out.IsJSON() {
					return out.PrintJSON(map[string]interface{}{
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
				return out.PrintJSON(map[string]interface{}{
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
					statusIcon := tui.StatusIcon(w.Status)
					statusStyled := styles.StatusStyle(w.Status).Render(statusIcon + " " + w.Status)

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

// discoverGPUs discovers GPUs on the system using the Hypervisor's AcceleratorInterface
func discoverGPUs() []api.GPUInfo {
	// Check for mock mode first (for testing without real GPUs)
	mockEnv := os.Getenv("GPU_GO_MOCK_GPUS")
	if mockEnv != "" {
		return getMockGPUs(mockEnv)
	}

	// Try to use the accelerator library for real GPU discovery
	libPath := acceleratorLib
	if libPath == "" {
		libPath = os.Getenv("TENSOR_FUSION_ACCELERATOR_LIB")
	}

	// If no library specified and no path found, use mock GPUs or return empty
	if libPath == "" {
		libPath = agent.GetExampleLibraryPath()
	}

	if libPath == "" {
		log.Debug().Msg("no accelerator library found, use --accelerator-lib or set GPU_GO_MOCK_GPUS env var")
		return nil
	}

	// Try to initialize device discovery
	discovery, err := agent.NewDeviceDiscovery(libPath)
	if err != nil {
		log.Debug().Err(err).Str("lib_path", libPath).Msg("failed to initialize device discovery")
		return nil
	}
	defer discovery.Close()

	gpus, err := discovery.DiscoverGPUs()
	if err != nil {
		log.Error().Err(err).Msg("failed to discover GPUs")
		return nil
	}

	return gpus
}

// getMockGPUs returns mock GPU information based on the mock env value
// Value can be a number (e.g., "2" for 2 GPUs) or "1" for a single default GPU
func getMockGPUs(mockEnv string) []api.GPUInfo {
	count := 1
	var n int
	if _, err := fmt.Sscanf(mockEnv, "%d", &n); err == nil && n > 0 {
		count = n
	}

	gpus := make([]api.GPUInfo, count)
	for i := 0; i < count; i++ {
		gpus[i] = api.GPUInfo{
			GPUID:         fmt.Sprintf("GPU-%d", i),
			Vendor:        "nvidia",
			Model:         "RTX 4090",
			VRAMMb:        24576,
			DriverVersion: "535.104.05",
			CUDAVersion:   "12.2",
		}
	}
	return gpus
}

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
