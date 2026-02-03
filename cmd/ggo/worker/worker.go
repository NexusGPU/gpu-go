package worker

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/NexusGPU/gpu-go/cmd/ggo/auth"
	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
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
	cmdutil.AddOutputFlag(cmd, &outputFormat)

	cmd.AddCommand(newWorkerListCmd())
	cmd.AddCommand(newWorkerCreateCmd())
	cmd.AddCommand(newWorkerGetCmd())
	cmd.AddCommand(newWorkerUpdateCmd())
	cmd.AddCommand(newWorkerDeleteCmd())
	cmd.AddCommand(newWorkerShareCmd())

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
	return cmdutil.NewOutput(outputFormat)
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
				cmd.SilenceUsage = true
				klog.Errorf("Failed to list workers: error=%v", err)
				return err
			}

			return out.Render(&workerListResult{workers: resp.Workers})
		},
	}

	cmd.Flags().StringVar(&agentID, "agent-id", "", "Filter by agent ID")
	cmd.Flags().StringVar(&hostname, "hostname", "", "Filter by hostname")

	return cmd
}

// workerListResult implements Renderable for worker list
type workerListResult struct {
	workers []api.WorkerInfo
}

func (r *workerListResult) RenderJSON() any {
	return tui.NewListResult(r.workers)
}

func (r *workerListResult) RenderTUI(out *tui.Output) {
	if len(r.workers) == 0 {
		out.Info("No workers found")
		return
	}

	styles := tui.DefaultStyles()
	var rows [][]string
	for _, w := range r.workers {
		statusIcon := tui.StatusIcon(w.Status)
		statusStyled := styles.StatusStyle(w.Status).Render(statusIcon + " " + w.Status)

		enabledIcon := tui.StatusIcon(boolToYesNo(w.Enabled))
		enabledStyled := styles.StatusStyle(boolToYesNo(w.Enabled)).Render(enabledIcon)

		// Format PID
		pid := "-"
		if w.PID > 0 {
			pid = fmt.Sprintf("%d", w.PID)
		}

		rows = append(rows, []string{
			w.WorkerID,
			w.Name,
			statusStyled,
			fmt.Sprintf("%d", w.ListenPort),
			enabledStyled,
			pid,
			fmt.Sprintf("%d", w.Restarts),
		})
	}

	table := tui.NewTable().
		Headers("WORKER ID", "NAME", "STATUS", "PORT", "ENABLED", "PID", "RESTARTS").
		Rows(rows)

	out.Println(table.String())
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
				cmd.SilenceUsage = true
				klog.Errorf("Failed to create worker: error=%v", err)
				return err
			}

			return out.Render(&workerCreateResult{worker: resp})
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

// workerCreateResult implements Renderable for worker create
type workerCreateResult struct {
	worker *api.WorkerInfo
}

func (r *workerCreateResult) RenderJSON() any {
	return tui.NewActionResult(true, "Worker created successfully", r.worker.WorkerID)
}

func (r *workerCreateResult) RenderTUI(out *tui.Output) {
	out.Println()
	out.Success("Worker created successfully!")
	out.Println()

	status := tui.NewStatusTable().
		Add("Worker ID", r.worker.WorkerID).
		Add("Name", r.worker.Name).
		AddWithStatus("Status", r.worker.Status, r.worker.Status)

	out.Println(status.String())
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
				cmd.SilenceUsage = true
				klog.Errorf("Failed to get worker: error=%v", err)
				return err
			}

			return out.Render(&workerDetailResult{worker: resp})
		},
	}

	return cmd
}

// workerDetailResult implements Renderable for worker detail
type workerDetailResult struct {
	worker *api.WorkerInfo
}

func (r *workerDetailResult) RenderJSON() any {
	return tui.NewDetailResult(r.worker)
}

func (r *workerDetailResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()

	out.Println()
	out.Println(styles.Title.Render("Worker Details"))
	out.Println()

	// Format PID
	pid := "-"
	if r.worker.PID > 0 {
		pid = fmt.Sprintf("%d", r.worker.PID)
	}

	status := tui.NewStatusTable().
		Add("Worker ID", r.worker.WorkerID).
		Add("Name", r.worker.Name).
		Add("Agent ID", r.worker.AgentID).
		AddWithStatus("Status", r.worker.Status, r.worker.Status).
		Add("Listen Port", fmt.Sprintf("%d", r.worker.ListenPort)).
		AddWithStatus("Enabled", boolToYesNo(r.worker.Enabled), boolToYesNo(r.worker.Enabled)).
		Add("PID", pid).
		Add("Restarts", fmt.Sprintf("%d", r.worker.Restarts)).
		Add("GPU IDs", strings.Join(r.worker.GPUIDs, ", "))

	out.Println(status.String())

	if len(r.worker.Connections) > 0 {
		out.Println()
		out.Println(styles.Subtitle.Render("Active Connections"))
		out.Println()

		var rows [][]string
		for _, conn := range r.worker.Connections {
			rows = append(rows, []string{
				conn.ClientIP,
				conn.ConnectedAt.Format("2006-01-02 15:04:05"),
			})
		}

		connTable := tui.NewTable().
			Headers("CLIENT IP", "CONNECTED AT").
			Rows(rows)

		out.Println(connTable.String())
	}
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
				notDisabled := !disabled
				req.Enabled = &notDisabled
			}

			resp, err := client.UpdateWorker(ctx, workerID, req)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to update worker: error=%v", err)
				return err
			}

			return out.Render(&workerUpdateResult{worker: resp})
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Worker name")
	cmd.Flags().StringSliceVar(&gpuIDs, "gpu-ids", nil, "GPU IDs")
	cmd.Flags().IntVar(&listenPort, "port", 0, "Listen port")
	cmd.Flags().BoolVar(&enabled, "enabled", false, "Enable worker")
	cmd.Flags().BoolVar(&disabled, "disabled", false, "Disable worker")

	return cmd
}

// workerUpdateResult implements Renderable for worker update
type workerUpdateResult struct {
	worker *api.WorkerInfo
}

func (r *workerUpdateResult) RenderJSON() any {
	return tui.NewActionResult(true, "Worker updated successfully", r.worker.WorkerID)
}

func (r *workerUpdateResult) RenderTUI(out *tui.Output) {
	out.Println()
	out.Success("Worker updated successfully!")
	out.Println()

	status := tui.NewStatusTable().
		Add("Worker ID", r.worker.WorkerID).
		AddWithStatus("Status", r.worker.Status, r.worker.Status)

	out.Println(status.String())
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
				cmd.SilenceUsage = true
				klog.Errorf("Failed to delete worker: error=%v", err)
				return err
			}

			return out.Render(&cmdutil.ActionData{
				Success: true,
				Message: fmt.Sprintf("Worker %s deleted successfully!", workerID),
				ID:      workerID,
			})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation")

	return cmd
}

func newWorkerShareCmd() *cobra.Command {
	var connectionIP string
	var expiresIn string
	var maxUses int

	cmd := &cobra.Command{
		Use:   "share [worker-name]",
		Short: "Create a share link for a worker",
		Long: `Create a shareable link that allows others to connect to your GPU worker.

The command will:
  1. Select a worker (from argument or interactive selection)
  2. Select an IP address (from worker's network IPs or custom input)
  3. Create a share link via the API
  4. Display the link with usage instructions

Examples:
  # Interactive selection of worker and IP
  ggo worker share

  # Share a specific worker with interactive IP selection
  ggo worker share my-worker

  # Share with specific IP
  ggo worker share my-worker --connection-ip 192.168.1.100

  # Share with expiration
  ggo worker share my-worker --expires-in 24h`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := getClient()
			ctx := context.Background()
			out := getOutput()

			// Step 1: Get worker (from arg or selection)
			workerID, workerName, agentID, err := selectWorker(ctx, client, args, out)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			// Step 2: Get connection IP (from flag, agent IPs, or manual input)
			if connectionIP == "" {
				selectedIP, err := selectConnectionIP(ctx, client, agentID, out)
				if err != nil {
					cmd.SilenceUsage = true
					return err
				}
				connectionIP = selectedIP
			}

			// Step 3: Create share link
			req := &api.ShareCreateRequest{
				WorkerID:     workerID,
				ConnectionIP: connectionIP,
			}

			if expiresIn != "" {
				duration, err := time.ParseDuration(expiresIn)
				if err != nil {
					return fmt.Errorf("invalid expiration duration: %w", err)
				}
				expiresAt := time.Now().Add(duration)
				req.ExpiresAt = &expiresAt
			}

			if maxUses > 0 {
				req.MaxUses = &maxUses
			}

			resp, err := client.CreateShare(ctx, req)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to create share: error=%v", err)
				return err
			}

			return out.Render(&workerShareResult{
				share:      resp,
				workerName: workerName,
			})
		},
	}

	cmd.Flags().StringVar(&connectionIP, "connection-ip", "", "Connection IP address (skip interactive selection)")
	cmd.Flags().StringVar(&expiresIn, "expires-in", "", "Expiration duration (e.g., 24h, 7d)")
	cmd.Flags().IntVar(&maxUses, "max-uses", 0, "Maximum number of uses (0 = unlimited)")

	return cmd
}

// selectWorker selects a worker from argument or interactive prompt
func selectWorker(ctx context.Context, client *api.Client, args []string, out *tui.Output) (workerID, workerName, agentID string, err error) {
	resp, err := client.ListWorkers(ctx, "", "")
	if err != nil {
		klog.Errorf("Failed to list workers: error=%v", err)
		return "", "", "", err
	}

	if len(resp.Workers) == 0 {
		return "", "", "", fmt.Errorf("no workers found. Create a worker first with 'ggo worker create'")
	}

	// If worker name provided as argument, find it
	if len(args) > 0 {
		workerNameArg := args[0]
		for _, w := range resp.Workers {
			if w.Name == workerNameArg {
				return w.WorkerID, w.Name, w.AgentID, nil
			}
		}
		return "", "", "", fmt.Errorf("worker '%s' not found", workerNameArg)
	}

	// Interactive selection if running in TUI mode
	if out.IsJSON() {
		return "", "", "", fmt.Errorf("worker name is required in JSON output mode")
	}

	// Build worker options for selection
	var workerItems []tui.WorkerSelectItem
	for _, w := range resp.Workers {
		workerItems = append(workerItems, tui.WorkerSelectItem{
			Name:     w.Name,
			WorkerID: w.WorkerID,
			Status:   w.Status,
		})
	}

	options := tui.FormatWorkerOptions(workerItems)
	selectedWorkerID, err := tui.SelectPrompt("Select a worker to share:", options)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to select worker: %w", err)
	}

	// Find the selected worker's details
	for _, w := range resp.Workers {
		if w.WorkerID == selectedWorkerID {
			return w.WorkerID, w.Name, w.AgentID, nil
		}
	}

	return "", "", "", fmt.Errorf("selected worker not found")
}

// selectConnectionIP selects an IP from agent's network IPs or manual input
func selectConnectionIP(ctx context.Context, client *api.Client, agentID string, out *tui.Output) (string, error) {
	if out.IsJSON() {
		return "", fmt.Errorf("--connection-ip is required in JSON output mode")
	}

	// Get agent info to retrieve network IPs
	var networkIPs []string
	if agentID != "" {
		agent, err := client.GetAgent(ctx, agentID)
		if err != nil {
			klog.Warningf("Failed to get agent info: error=%v", err)
		} else if len(agent.NetworkIPs) > 0 {
			networkIPs = agent.NetworkIPs
		}
	}

	if len(networkIPs) == 0 {
		// No IPs available, ask for manual input
		return tui.InputPrompt("Enter connection IP address")
	}

	// Show IP selection
	options := tui.FormatIPOptions(networkIPs)
	return tui.SelectPrompt("Select connection IP address:", options)
}

// workerShareResult implements Renderable for worker share command
type workerShareResult struct {
	share      *api.ShareInfo
	workerName string
}

func (r *workerShareResult) RenderJSON() any {
	return r.share
}

func (r *workerShareResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()

	out.Println()
	out.Success("Share link created successfully!")
	out.Println()

	status := tui.NewStatusTable().
		Add("Worker", styles.Bold.Render(r.workerName)).
		Add("Short Code", styles.Bold.Render(r.share.ShortCode)).
		Add("Short Link", tui.URL(r.share.ShortLink)).
		Add("Connection URL", r.share.ConnectionURL)

	if r.share.ExpiresAt != nil {
		status.Add("Expires At", r.share.ExpiresAt.Format("2006-01-02 15:04:05"))
	}
	if r.share.MaxUses != nil {
		status.Add("Max Uses", fmt.Sprintf("%d", *r.share.MaxUses))
	}

	out.Println(status.String())

	out.Println()
	out.Println(styles.Title.Render("📋 Share this link with others:"))
	out.Println()

	// Usage instruction box
	out.Println(styles.Subtitle.Render("Option 1: Update client environment"))
	out.Println()
	out.Println("  " + tui.Code(fmt.Sprintf("ggo use %s", r.share.ShortCode)))
	out.Println()
	out.Println(styles.Muted.Render("  This sets up the remote GPU environment for the current session."))

	out.Println()
	out.Println(styles.Subtitle.Render("Option 2: Create an AI Studio with this GPU"))
	out.Println()
	out.Println("  " + tui.Code(fmt.Sprintf("ggo studio create <name> -s %s", r.share.ConnectionURL)))
	out.Println()
	out.Println(styles.Muted.Render("  This creates a containerized development environment with remote GPU access."))
	out.Println()
}

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
