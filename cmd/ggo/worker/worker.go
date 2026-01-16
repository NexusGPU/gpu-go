package worker

import (
	"context"
	"fmt"
	"os"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	serverURL string
	userToken string
)

// NewWorkerCmd creates the worker command
func NewWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage GPU workers",
		Long:  `The worker command manages GPU workers on remote servers.`,
	}

	cmd.PersistentFlags().StringVar(&serverURL, "server", "https://api.gpu.tf", "Server URL")
	cmd.PersistentFlags().StringVar(&userToken, "token", "", "User authentication token")

	cmd.AddCommand(newWorkerListCmd())
	cmd.AddCommand(newWorkerCreateCmd())
	cmd.AddCommand(newWorkerGetCmd())
	cmd.AddCommand(newWorkerUpdateCmd())
	cmd.AddCommand(newWorkerDeleteCmd())

	return cmd
}

func getClient() *api.Client {
	token := userToken
	if token == "" {
		token = os.Getenv("GPU_GO_USER_TOKEN")
	}
	return api.NewClient(
		api.WithBaseURL(serverURL),
		api.WithUserToken(token),
	)
}

func newWorkerListCmd() *cobra.Command {
	var agentID string
	var hostname string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all workers",
		Long:  `List all GPU workers for the current user.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := getClient()
			ctx := context.Background()

			resp, err := client.ListWorkers(ctx, agentID, hostname)
			if err != nil {
				log.Error().Err(err).Msg("Failed to list workers")
				return err
			}

			if len(resp.Workers) == 0 {
				log.Info().Msg("No workers found")
				return nil
			}

			fmt.Printf("%-20s %-20s %-15s %-10s %-10s\n", "WORKER ID", "NAME", "STATUS", "PORT", "ENABLED")
			fmt.Println("--------------------------------------------------------------------------------")
			for _, w := range resp.Workers {
				enabled := "no"
				if w.Enabled {
					enabled = "yes"
				}
				fmt.Printf("%-20s %-20s %-15s %-10d %-10s\n", w.WorkerID, w.Name, w.Status, w.ListenPort, enabled)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&agentID, "agent-id", "", "Filter by agent ID")
	cmd.Flags().StringVar(&hostname, "hostname", "", "Filter by hostname")

	return cmd
}

func newWorkerCreateCmd() *cobra.Command {
	var agentID string
	var name string
	var gpuIDs []string
	var listenPort int
	var enabled bool

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new worker",
		Long:  `Create a new GPU worker on a remote server.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := getClient()
			ctx := context.Background()

			req := &api.WorkerCreateRequest{
				AgentID:    agentID,
				Name:       name,
				GPUIDs:     gpuIDs,
				ListenPort: listenPort,
				Enabled:    enabled,
			}

			resp, err := client.CreateWorker(ctx, req)
			if err != nil {
				log.Error().Err(err).Msg("Failed to create worker")
				return err
			}

			log.Info().
				Str("worker_id", resp.WorkerID).
				Str("name", resp.Name).
				Str("status", resp.Status).
				Msg("Worker created successfully")

			return nil
		},
	}

	cmd.Flags().StringVar(&agentID, "agent-id", "", "Agent ID (required)")
	cmd.Flags().StringVar(&name, "name", "", "Worker name (required)")
	cmd.Flags().StringSliceVar(&gpuIDs, "gpu-ids", nil, "GPU IDs to allocate (required)")
	cmd.Flags().IntVar(&listenPort, "port", 9001, "Listen port")
	cmd.Flags().BoolVar(&enabled, "enabled", true, "Enable worker")

	_ = cmd.MarkFlagRequired("agent-id")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("gpu-ids")

	return cmd
}

func newWorkerGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <worker-id>",
		Short: "Get worker details",
		Long:  `Get detailed information about a specific worker.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workerID := args[0]
			client := getClient()
			ctx := context.Background()

			resp, err := client.GetWorker(ctx, workerID)
			if err != nil {
				log.Error().Err(err).Msg("Failed to get worker")
				return err
			}

			fmt.Printf("Worker ID:    %s\n", resp.WorkerID)
			fmt.Printf("Name:         %s\n", resp.Name)
			fmt.Printf("Agent ID:     %s\n", resp.AgentID)
			fmt.Printf("Status:       %s\n", resp.Status)
			fmt.Printf("Listen Port:  %d\n", resp.ListenPort)
			fmt.Printf("Enabled:      %v\n", resp.Enabled)
			fmt.Printf("GPU IDs:      %v\n", resp.GPUIDs)

			if len(resp.Connections) > 0 {
				fmt.Printf("\nActive Connections:\n")
				for _, conn := range resp.Connections {
					fmt.Printf("  - %s (connected at %s)\n", conn.ClientIP, conn.ConnectedAt.Format("2006-01-02 15:04:05"))
				}
			}

			return nil
		},
	}

	return cmd
}

func newWorkerUpdateCmd() *cobra.Command {
	var name string
	var gpuIDs []string
	var listenPort int
	var enabled bool
	var disabled bool

	cmd := &cobra.Command{
		Use:   "update <worker-id>",
		Short: "Update a worker",
		Long:  `Update configuration of an existing worker.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workerID := args[0]
			client := getClient()
			ctx := context.Background()

			req := &api.WorkerUpdateRequest{}

			if cmd.Flags().Changed("name") {
				req.Name = &name
			}
			if cmd.Flags().Changed("gpu-ids") {
				req.GPUIDs = gpuIDs
			}
			if cmd.Flags().Changed("port") {
				req.ListenPort = &listenPort
			}
			if cmd.Flags().Changed("enabled") {
				req.Enabled = &enabled
			}
			if cmd.Flags().Changed("disabled") {
				disabled := !disabled
				req.Enabled = &disabled
			}

			resp, err := client.UpdateWorker(ctx, workerID, req)
			if err != nil {
				log.Error().Err(err).Msg("Failed to update worker")
				return err
			}

			log.Info().
				Str("worker_id", resp.WorkerID).
				Str("status", resp.Status).
				Msg("Worker updated successfully")

			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Worker name")
	cmd.Flags().StringSliceVar(&gpuIDs, "gpu-ids", nil, "GPU IDs")
	cmd.Flags().IntVar(&listenPort, "port", 0, "Listen port")
	cmd.Flags().BoolVar(&enabled, "enabled", false, "Enable worker")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "Disable worker")

	return cmd
}

func newWorkerDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <worker-id>",
		Short: "Delete a worker",
		Long:  `Delete a GPU worker.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			workerID := args[0]
			client := getClient()
			ctx := context.Background()

			if !force {
				fmt.Printf("Are you sure you want to delete worker %s? [y/N]: ", workerID)
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "y" && confirm != "Y" {
					log.Info().Msg("Cancelled")
					return nil
				}
			}

			if err := client.DeleteWorker(ctx, workerID); err != nil {
				log.Error().Err(err).Msg("Failed to delete worker")
				return err
			}

			log.Info().Str("worker_id", workerID).Msg("Worker deleted successfully")
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")

	return cmd
}

// For local worker status (used by agent)
func LoadLocalWorkers(configDir string) ([]config.WorkerConfig, error) {
	configMgr := config.NewManager(configDir, "")
	return configMgr.LoadWorkers()
}
