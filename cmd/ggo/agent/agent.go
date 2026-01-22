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
	configDir    string
	stateDir     string
	serverURL    string
	outputFormat string
	paths        = platform.DefaultPaths()
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
				return fmt.Errorf("token is required")
			}

			configMgr := config.NewManager(configDir, stateDir)
			client := api.NewClient(api.WithBaseURL(serverURL))

			// Discover GPUs (placeholder - in real implementation, use nvml or similar)
			gpus := discoverGPUs()

			agentInstance := agent.NewAgent(client, configMgr)
			if err := agentInstance.Register(token, gpus); err != nil {
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
				if !out.IsJSON() {
					fmt.Println(tui.ErrorMessage("Agent is not registered. Please run 'ggo agent register' first"))
				}
				return agent.ErrNotRegistered
			}

			cfg, err := configMgr.LoadConfig()
			if err != nil {
				log.Error().Err(err).Msg("Failed to load config")
				return err
			}

			client := api.NewClient(
				api.WithBaseURL(cfg.ServerURL),
				api.WithAgentSecret(cfg.AgentSecret),
			)

			agentInstance := agent.NewAgent(client, configMgr)
			if err := agentInstance.Start(); err != nil {
				log.Error().Err(err).Msg("Failed to start agent")
				return err
			}

			if !out.IsJSON() {
				styles := tui.DefaultStyles()
				fmt.Printf("%s Agent started (ID: %s)\n",
					styles.Success.Render("●"),
					styles.Bold.Render(cfg.AgentID))
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

// discoverGPUs discovers GPUs on the system (placeholder implementation)
func discoverGPUs() []api.GPUInfo {
	// In real implementation, use NVML or similar to discover GPUs
	// For now, return empty list or mock data based on environment
	if os.Getenv("GPU_GO_MOCK_GPUS") != "" {
		return []api.GPUInfo{
			{
				GPUID:         "GPU-0",
				Vendor:        "nvidia",
				Model:         "RTX 4090",
				VRAMMb:        24576,
				DriverVersion: "535.104.05",
				CUDAVersion:   "12.2",
			},
		}
	}
	return nil
}

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
