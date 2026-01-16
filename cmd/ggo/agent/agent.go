package agent

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/NexusGPU/gpu-go/internal/agent"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	configDir string
	stateDir  string
	serverURL string
	paths     = platform.DefaultPaths()
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
	cmd.PersistentFlags().StringVar(&serverURL, "server", "https://api.gpu.tf", "Server URL")

	cmd.AddCommand(newRegisterCmd())
	cmd.AddCommand(newStartCmd())
	cmd.AddCommand(newStatusCmd())

	return cmd
}

func newRegisterCmd() *cobra.Command {
	var token string

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register agent with the server",
		Long:  `Register this GPU server as an agent with the GPU Go platform.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				token = os.Getenv("GPU_GO_TOKEN")
			}
			if token == "" {
				log.Error().Msg("Token is required. Use --token flag or GPU_GO_TOKEN environment variable")
				return nil
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

			log.Info().Msg("Agent registered successfully")
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
			configMgr := config.NewManager(configDir, stateDir)
			
			// Check if agent is registered
			if !configMgr.ConfigExists() {
				log.Error().Msg("Agent is not registered. Please run 'ggo agent register' first")
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

			// Wait for interrupt signal
			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh

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
			configMgr := config.NewManager(configDir, stateDir)

			cfg, err := configMgr.LoadConfig()
			if err != nil {
				log.Error().Err(err).Msg("Failed to load config")
				return err
			}

			if cfg == nil {
				log.Info().Msg("Agent is not registered")
				return nil
			}

			log.Info().
				Str("agent_id", cfg.AgentID).
				Int("config_version", cfg.ConfigVersion).
				Str("server_url", cfg.ServerURL).
				Msg("Agent configuration")

			gpus, err := configMgr.LoadGPUs()
			if err == nil && len(gpus) > 0 {
				log.Info().Int("count", len(gpus)).Msg("GPUs")
				for _, gpu := range gpus {
					log.Info().
						Str("id", gpu.GPUID).
						Str("vendor", gpu.Vendor).
						Str("model", gpu.Model).
						Int64("vram_mb", gpu.VRAMMb).
						Msg("  GPU")
				}
			}

			workers, err := configMgr.LoadWorkers()
			if err == nil && len(workers) > 0 {
				log.Info().Int("count", len(workers)).Msg("Workers")
				for _, w := range workers {
					log.Info().
						Str("id", w.WorkerID).
						Int("port", w.ListenPort).
						Bool("enabled", w.Enabled).
						Str("status", w.Status).
						Msg("  Worker")
				}
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
