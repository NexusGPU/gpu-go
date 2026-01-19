package worker

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/NexusGPU/gpu-go/cmd/ggo/auth"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	serverURL    string
	userToken    string
	outputFormat string
)

// NewWorkerCmd creates the worker command
func NewWorkerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage GPU workers",
		Long:  `The worker command manages GPU workers on remote servers.`,
	}

	cmd.PersistentFlags().StringVar(&serverURL, "server", api.GetDefaultBaseURL(), "Server URL (or set GPU_GO_ENDPOINT env var)")
	cmd.PersistentFlags().StringVar(&userToken, "token", "", "User authentication token")
	cmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json)")

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
		token = os.Getenv("GPU_GO_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GPU_GO_USER_TOKEN")
	}
	// Fall back to token from ~/.gpugo/token.json
	if token == "" {
		if savedToken, err := auth.GetToken(); err == nil && savedToken != "" {
			token = savedToken
		}
	}
	return api.NewClient(
		api.WithBaseURL(serverURL),
		api.WithUserToken(token),
	)
}

func getOutput() *tui.Output {
	return tui.NewOutputWithFormat(tui.ParseOutputFormat(outputFormat))
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
			out := getOutput()

			resp, err := client.ListWorkers(ctx, agentID, hostname)
			if err != nil {
				log.Error().Err(err).Msg("Failed to list workers")
				return err
			}

			if len(resp.Workers) == 0 {
				if out.IsJSON() {
					return out.PrintJSON(tui.NewListResult([]api.WorkerInfo{}))
				}
				out.Info("No workers found")
				return nil
			}

			// JSON output
			if out.IsJSON() {
				return out.PrintJSON(tui.NewListResult(resp.Workers))
			}

			// Table output
			styles := tui.DefaultStyles()
			var rows [][]string
			for _, w := range resp.Workers {
				statusIcon := tui.StatusIcon(w.Status)
				statusStyled := styles.StatusStyle(w.Status).Render(statusIcon + " " + w.Status)

				enabledIcon := tui.StatusIcon(boolToYesNo(w.Enabled))
				enabledStyled := styles.StatusStyle(boolToYesNo(w.Enabled)).Render(enabledIcon)

				rows = append(rows, []string{
					w.WorkerID,
					w.Name,
					statusStyled,
					fmt.Sprintf("%d", w.ListenPort),
					enabledStyled,
				})
			}

			table := tui.NewTable().
				Headers("WORKER ID", "NAME", "STATUS", "PORT", "ENABLED").
				Rows(rows)

			fmt.Println(table.String())
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
			out := getOutput()

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

			if out.IsJSON() {
				return out.PrintJSON(tui.NewActionResult(true, "Worker created successfully", resp.WorkerID))
			}

			// Styled output
			fmt.Println()
			fmt.Println(tui.SuccessMessage("Worker created successfully!"))
			fmt.Println()

			status := tui.NewStatusTable().
				Add("Worker ID", resp.WorkerID).
				Add("Name", resp.Name).
				AddWithStatus("Status", resp.Status, resp.Status)

			fmt.Println(status.String())
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
			out := getOutput()

			resp, err := client.GetWorker(ctx, workerID)
			if err != nil {
				log.Error().Err(err).Msg("Failed to get worker")
				return err
			}

			if out.IsJSON() {
				return out.PrintJSON(tui.NewDetailResult(resp))
			}

			// Styled output
			styles := tui.DefaultStyles()

			fmt.Println()
			fmt.Println(styles.Title.Render("Worker Details"))
			fmt.Println()

			status := tui.NewStatusTable().
				Add("Worker ID", resp.WorkerID).
				Add("Name", resp.Name).
				Add("Agent ID", resp.AgentID).
				AddWithStatus("Status", resp.Status, resp.Status).
				Add("Listen Port", fmt.Sprintf("%d", resp.ListenPort)).
				AddWithStatus("Enabled", boolToYesNo(resp.Enabled), boolToYesNo(resp.Enabled)).
				Add("GPU IDs", strings.Join(resp.GPUIDs, ", "))

			fmt.Println(status.String())

			if len(resp.Connections) > 0 {
				fmt.Println()
				fmt.Println(styles.Subtitle.Render("Active Connections"))
				fmt.Println()

				var rows [][]string
				for _, conn := range resp.Connections {
					rows = append(rows, []string{
						conn.ClientIP,
						conn.ConnectedAt.Format("2006-01-02 15:04:05"),
					})
				}

				connTable := tui.NewTable().
					Headers("CLIENT IP", "CONNECTED AT").
					Rows(rows)

				fmt.Println(connTable.String())
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
			out := getOutput()

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

			if out.IsJSON() {
				return out.PrintJSON(tui.NewActionResult(true, "Worker updated successfully", resp.WorkerID))
			}

			fmt.Println()
			fmt.Println(tui.SuccessMessage("Worker updated successfully!"))
			fmt.Println()

			status := tui.NewStatusTable().
				Add("Worker ID", resp.WorkerID).
				AddWithStatus("Status", resp.Status, resp.Status)

			fmt.Println(status.String())
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
			out := getOutput()

			if !force && !out.IsJSON() {
				styles := tui.DefaultStyles()
				fmt.Printf("%s Are you sure you want to delete worker %s? [y/N]: ",
					styles.Warning.Render("!"),
					styles.Bold.Render(workerID))
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "y" && confirm != "Y" {
					out.Info("Cancelled")
					return nil
				}
			}

			if err := client.DeleteWorker(ctx, workerID); err != nil {
				log.Error().Err(err).Msg("Failed to delete worker")
				return err
			}

			if out.IsJSON() {
				return out.PrintJSON(tui.NewActionResult(true, "Worker deleted successfully", workerID))
			}

			fmt.Println()
			fmt.Println(tui.SuccessMessage(fmt.Sprintf("Worker %s deleted successfully!", workerID)))
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

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
