package worker

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/NexusGPU/gpu-go/cmd/ggo/auth"
	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/NexusGPU/gpu-go/internal/utils"
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
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Initialize klog flags if not already initialized
			klog.InitFlags(nil)
			// Disable logtostderr so that stderrthreshold takes effect
			// When logtostderr=true (default), ALL logs go to stderr ignoring stderrthreshold
			flag.Set("logtostderr", "false")
			// Set stderrthreshold to WARNING level - only WARNING and ERROR will be shown
			flag.Set("stderrthreshold", "WARNING")
		},
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
	// Priority: 1. CLI flag, 2. Env vars, 3. Agent secret, 4. PAT token
	token := userToken
	if token == "" {
		token = os.Getenv("GPU_GO_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GPU_GO_USER_TOKEN")
	}

	// Try agent config secret before PAT token
	if token == "" {
		cfgMgr := config.NewManager("", "")
		if agentCfg, err := cfgMgr.LoadConfig(); err == nil && agentCfg != nil && agentCfg.AgentSecret != "" {
			klog.V(2).Infof("Using agent secret for authentication")
			token = agentCfg.AgentSecret
		}
	}

	// Fall back to PAT token
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
		Long: `Create a new GPU worker on a remote server.

If required parameters (--agent-id, --name, --gpu-ids) are not provided,
the command enters interactive TUI mode to guide you through the setup.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := getClient()
			ctx := context.Background()
			out := getOutput()

			// Check if we need interactive mode (missing required params)
			needsInteractive := !cmd.Flags().Changed("agent-id") ||
				!cmd.Flags().Changed("name") ||
				!cmd.Flags().Changed("gpu-ids")

			if needsInteractive {
				if out.IsJSON() {
					return fmt.Errorf("required flags not provided: --agent-id, --name, --gpu-ids are required in JSON mode")
				}

				// Enter interactive TUI mode
				var err error
				agentID, name, gpuIDs, listenPort, enabled, err = interactiveWorkerCreate(ctx, client, cmd)
				if err != nil {
					cmd.SilenceUsage = true
					return err
				}
			}

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

	cmd.Flags().StringVar(&agentID, "agent-id", "", "Agent ID (required, or use interactive mode)")
	cmd.Flags().StringVar(&name, "name", "", "Worker name (required, or use interactive mode)")
	cmd.Flags().StringSliceVar(&gpuIDs, "gpu-ids", nil, "GPU IDs to allocate (required, or use interactive mode)")
	cmd.Flags().IntVar(&listenPort, "port", 9001, "Listen port")
	cmd.Flags().BoolVar(&enabled, "enabled", true, "Enable worker")

	return cmd
}

// interactiveWorkerCreate guides user through worker creation via TUI
func interactiveWorkerCreate(ctx context.Context, client *api.Client, cmd *cobra.Command) (
	agentID, name string, gpuIDs []string, port int, enabled bool, err error,
) {
	styles := tui.DefaultStyles()
	totalSteps := 5

	fmt.Println()
	fmt.Println(styles.Title.Render("🚀 Create GPU Worker"))
	fmt.Println(styles.Muted.Render("Follow the steps below to configure your new worker"))

	// Step 1: Enter worker name
	tui.StepHeader(1, totalSteps, "Worker Name")
	name, err = tui.InputPrompt("Enter worker name")
	if err != nil {
		return "", "", nil, 0, false, fmt.Errorf("failed to get worker name: %w", err)
	}

	// Step 2: Select agent
	tui.StepHeader(2, totalSteps, "Select Agent")
	agentsResp, err := client.ListAgents(ctx)
	if err != nil {
		return "", "", nil, 0, false, fmt.Errorf("failed to list agents: %w", err)
	}
	if len(agentsResp.Agents) == 0 {
		return "", "", nil, 0, false, fmt.Errorf("no agents found. Register an agent first with 'ggo agent register'")
	}

	var agentItems []tui.AgentSelectItem
	for _, a := range agentsResp.Agents {
		agentItems = append(agentItems, tui.AgentSelectItem{
			AgentID:  a.AgentID,
			Hostname: a.Hostname,
			Status:   a.Status,
			GPUCount: len(a.GPUs),
		})
	}

	agentOptions := tui.FormatAgentOptions(agentItems)
	// Default to first agent
	agentID, err = tui.SelectPromptWithDefault("Select an agent:", agentOptions, 0, false)
	if err != nil {
		return "", "", nil, 0, false, fmt.Errorf("failed to select agent: %w", err)
	}

	// Step 3: Select GPUs from agent
	tui.StepHeader(3, totalSteps, "Select GPUs")

	// Find selected agent and its GPUs
	var selectedAgent *api.AgentInfo
	for i := range agentsResp.Agents {
		if agentsResp.Agents[i].AgentID == agentID {
			selectedAgent = &agentsResp.Agents[i]
			break
		}
	}

	if selectedAgent == nil || len(selectedAgent.GPUs) == 0 {
		return "", "", nil, 0, false, fmt.Errorf("selected agent has no GPUs available")
	}

	var gpuItems []tui.GPUSelectItem
	for _, g := range selectedAgent.GPUs {
		gpuItems = append(gpuItems, tui.GPUSelectItem{
			GPUID:  g.GPUID,
			Vendor: g.Vendor,
			Model:  g.Model,
			VRAMMb: g.VRAMMb,
		})
	}

	gpuOptions := tui.FormatGPUOptions(gpuItems)
	gpuIDs, err = tui.MultiSelectPrompt("Select GPU(s) to allocate:", gpuOptions)
	if err != nil {
		return "", "", nil, 0, false, fmt.Errorf("failed to select GPUs: %w", err)
	}

	// Step 4: Set port (random or user input)
	tui.StepHeader(4, totalSteps, "Listen Port")

	// Get a random available port as default
	randomPort, _ := utils.GetRandomAvailablePort()
	if randomPort == 0 {
		randomPort = 9001 // fallback
	}

	portOptions := []tui.SelectOption{
		{Label: fmt.Sprintf("Use random available port (%d)", randomPort), Value: "random"},
		{Label: "Enter custom port", Value: "custom"},
	}

	portChoice, err := tui.SelectPromptWithDefault("Choose port configuration:", portOptions, 0, false)
	if err != nil {
		return "", "", nil, 0, false, fmt.Errorf("failed to select port option: %w", err)
	}

	if portChoice == "random" {
		port = randomPort
	} else {
		portStr, err := tui.InputPromptWithDefault("Enter listen port", "9001")
		if err != nil {
			return "", "", nil, 0, false, fmt.Errorf("failed to get port: %w", err)
		}
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return "", "", nil, 0, false, fmt.Errorf("invalid port number: %s", portStr)
		}
	}

	// Default enabled to true
	enabled = true

	// Step 5: Confirmation
	tui.StepHeader(5, totalSteps, "Confirm Configuration")
	fmt.Println()

	status := tui.NewStatusTable().
		Add("Name", styles.Bold.Render(name)).
		Add("Agent", fmt.Sprintf("%s (%s)", selectedAgent.Hostname, agentID[:12]+"...")).
		Add("GPUs", strings.Join(gpuIDs, ", ")).
		Add("Port", fmt.Sprintf("%d", port)).
		Add("Enabled", "yes")

	fmt.Println(status.String())
	fmt.Println()

	confirmed, err := tui.ConfirmPrompt("Create this worker?")
	if err != nil {
		return "", "", nil, 0, false, fmt.Errorf("failed to confirm: %w", err)
	}
	if !confirmed {
		return "", "", nil, 0, false, fmt.Errorf("operation cancelled")
	}

	return agentID, name, gpuIDs, port, enabled, nil
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
		Use:   "update [worker-id]",
		Short: "Update a worker",
		Long: `Update configuration of an existing worker.

If worker-id is not provided or no update flags are specified,
the command enters interactive TUI mode.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := getClient()
			ctx := context.Background()
			out := getOutput()

			var workerID string
			if len(args) > 0 {
				workerID = args[0]
			}

			// Check if we need interactive mode
			hasUpdateFlags := cmd.Flags().Changed("name") ||
				cmd.Flags().Changed("gpu-ids") ||
				cmd.Flags().Changed("port") ||
				cmd.Flags().Changed("enabled") ||
				cmd.Flags().Changed("disabled")

			needsInteractive := workerID == "" || !hasUpdateFlags

			if needsInteractive {
				if out.IsJSON() {
					return fmt.Errorf("worker-id and at least one update flag are required in JSON mode")
				}

				var req *api.WorkerUpdateRequest
				var err error
				workerID, req, err = interactiveWorkerUpdate(ctx, client, workerID)
				if err != nil {
					cmd.SilenceUsage = true
					return err
				}

				resp, err := client.UpdateWorker(ctx, workerID, req)
				if err != nil {
					cmd.SilenceUsage = true
					klog.Errorf("Failed to update worker: error=%v", err)
					return err
				}

				return out.Render(&workerUpdateResult{worker: resp})
			}

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

// interactiveWorkerUpdate guides user through worker update via TUI
func interactiveWorkerUpdate(ctx context.Context, client *api.Client, workerID string) (
	selectedWorkerID string, req *api.WorkerUpdateRequest, err error,
) {
	styles := tui.DefaultStyles()

	fmt.Println()
	fmt.Println(styles.Title.Render("✏️  Update GPU Worker"))
	fmt.Println(styles.Muted.Render("Follow the steps below to update your worker"))

	// Step 1: Select worker if not provided
	if workerID == "" {
		tui.StepHeader(1, 3, "Select Worker")

		workersResp, err := client.ListWorkers(ctx, "", "")
		if err != nil {
			return "", nil, fmt.Errorf("failed to list workers: %w", err)
		}
		if len(workersResp.Workers) == 0 {
			return "", nil, fmt.Errorf("no workers found. Create a worker first with 'ggo worker create'")
		}

		var workerItems []tui.WorkerSelectItem
		for _, w := range workersResp.Workers {
			workerItems = append(workerItems, tui.WorkerSelectItem{
				Name:     w.Name,
				WorkerID: w.WorkerID,
				Status:   w.Status,
			})
		}

		workerOptions := tui.FormatWorkerOptions(workerItems)
		workerID, err = tui.SelectPromptWithDefault("Select a worker to update:", workerOptions, 0, false)
		if err != nil {
			return "", nil, fmt.Errorf("failed to select worker: %w", err)
		}
	}

	selectedWorkerID = workerID

	// Get current worker details
	worker, err := client.GetWorker(ctx, workerID)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get worker details: %w", err)
	}

	req = &api.WorkerUpdateRequest{}

	// Step 2: Choose what to update
	tui.StepHeader(2, 3, "Select Fields to Update")

	fmt.Println()
	fmt.Println(styles.Subtitle.Render("Current configuration:"))
	status := tui.NewStatusTable().
		Add("Name", worker.Name).
		Add("Port", fmt.Sprintf("%d", worker.ListenPort)).
		AddWithStatus("Enabled", boolToYesNo(worker.Enabled), boolToYesNo(worker.Enabled)).
		Add("GPUs", strings.Join(worker.GPUIDs, ", "))
	fmt.Println(status.String())

	updateOptions := []tui.SelectOption{
		{Label: "Name", Value: "name"},
		{Label: "Port", Value: "port"},
		{Label: "Enabled/Disabled", Value: "enabled"},
	}

	selectedFields, err := tui.MultiSelectPrompt("What would you like to update?", updateOptions)
	if err != nil {
		return "", nil, fmt.Errorf("failed to select fields: %w", err)
	}

	// Collect updates for selected fields
	for _, field := range selectedFields {
		switch field {
		case "name":
			newName, err := tui.InputPromptWithDefault("Enter new name", worker.Name)
			if err != nil {
				return "", nil, fmt.Errorf("failed to get name: %w", err)
			}
			req.Name = &newName

		case "port":
			portStr, err := tui.InputPromptWithDefault("Enter new port", fmt.Sprintf("%d", worker.ListenPort))
			if err != nil {
				return "", nil, fmt.Errorf("failed to get port: %w", err)
			}
			port, err := strconv.Atoi(portStr)
			if err != nil {
				return "", nil, fmt.Errorf("invalid port number: %s", portStr)
			}
			req.ListenPort = &port

		case "enabled":
			enabledOptions := []tui.SelectOption{
				{Label: "Enabled", Value: "true"},
				{Label: "Disabled", Value: "false"},
			}
			defaultIdx := 0
			if !worker.Enabled {
				defaultIdx = 1
			}
			enabledStr, err := tui.SelectPromptWithDefault("Enable or disable worker:", enabledOptions, defaultIdx, false)
			if err != nil {
				return "", nil, fmt.Errorf("failed to select enabled: %w", err)
			}
			enabledVal := enabledStr == "true"
			req.Enabled = &enabledVal
		}
	}

	// Step 3: Confirmation
	tui.StepHeader(3, 3, "Confirm Changes")
	fmt.Println()

	changeTable := tui.NewStatusTable()
	changeTable.Add("Worker ID", workerID[:12]+"...")
	if req.Name != nil {
		changeTable.Add("New Name", *req.Name)
	}
	if req.ListenPort != nil {
		changeTable.Add("New Port", fmt.Sprintf("%d", *req.ListenPort))
	}
	if req.Enabled != nil {
		changeTable.AddWithStatus("New Enabled", boolToYesNo(*req.Enabled), boolToYesNo(*req.Enabled))
	}

	fmt.Println(changeTable.String())
	fmt.Println()

	confirmed, err := tui.ConfirmPrompt("Apply these changes?")
	if err != nil {
		return "", nil, fmt.Errorf("failed to confirm: %w", err)
	}
	if !confirmed {
		return "", nil, fmt.Errorf("operation cancelled")
	}

	return selectedWorkerID, req, nil
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
		Use:   "delete [worker-id]",
		Short: "Delete a worker",
		Long: `Delete a GPU worker.

If worker-id is not provided, the command enters interactive TUI mode
to let you select a worker to delete.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client := getClient()
			ctx := context.Background()
			out := getOutput()

			var workerID string
			var workerName string
			if len(args) > 0 {
				workerID = args[0]
			}

			// Interactive mode if no worker-id provided
			if workerID == "" {
				if out.IsJSON() {
					return fmt.Errorf("worker-id is required in JSON mode")
				}

				var err error
				workerID, workerName, err = interactiveWorkerDelete(ctx, client)
				if err != nil {
					cmd.SilenceUsage = true
					return err
				}
			}

			if !force && !out.IsJSON() {
				displayID := workerID
				if workerName != "" {
					displayID = fmt.Sprintf("%s (%s)", workerName, workerID[:12]+"...")
				}
				confirmed, err := tui.ConfirmPrompt(fmt.Sprintf("Are you sure you want to delete worker %s?", displayID))
				if err != nil {
					return fmt.Errorf("failed to confirm: %w", err)
				}
				if !confirmed {
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

// interactiveWorkerDelete guides user through worker deletion via TUI
func interactiveWorkerDelete(ctx context.Context, client *api.Client) (workerID, workerName string, err error) {
	styles := tui.DefaultStyles()

	fmt.Println()
	fmt.Println(styles.Title.Render("🗑️  Delete GPU Worker"))

	// Step 1: Select worker
	tui.StepHeader(1, 1, "Select Worker to Delete")

	workersResp, err := client.ListWorkers(ctx, "", "")
	if err != nil {
		return "", "", fmt.Errorf("failed to list workers: %w", err)
	}
	if len(workersResp.Workers) == 0 {
		return "", "", fmt.Errorf("no workers found")
	}

	var workerItems []tui.WorkerSelectItem
	workerNameMap := make(map[string]string)
	for _, w := range workersResp.Workers {
		workerItems = append(workerItems, tui.WorkerSelectItem{
			Name:     w.Name,
			WorkerID: w.WorkerID,
			Status:   w.Status,
		})
		workerNameMap[w.WorkerID] = w.Name
	}

	workerOptions := tui.FormatWorkerOptions(workerItems)
	workerID, err = tui.SelectPromptWithDefault("Select a worker to delete:", workerOptions, 0, false)
	if err != nil {
		return "", "", fmt.Errorf("failed to select worker: %w", err)
	}

	return workerID, workerNameMap[workerID], nil
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

			// Determine if we need interactive mode
			needsWorkerSelection := len(args) == 0
			needsIPSelection := connectionIP == ""
			totalSteps := 0
			if needsWorkerSelection {
				totalSteps++
			}
			if needsIPSelection {
				totalSteps++
			}

			currentStep := 0

			// Show title for interactive mode
			if !out.IsJSON() && (needsWorkerSelection || needsIPSelection) {
				styles := tui.DefaultStyles()
				fmt.Println()
				fmt.Println(styles.Title.Render("🔗 Share GPU Worker"))
				fmt.Println(styles.Muted.Render("Create a shareable link for your GPU worker"))
			}

			// Step 1: Get worker (from arg or selection)
			if needsWorkerSelection {
				currentStep++
			}
			workerID, workerName, agentID, err := selectWorker(ctx, client, args, out, currentStep, totalSteps)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			// Step 2: Get connection IP (from flag, agent IPs, or manual input)
			if needsIPSelection {
				currentStep++
				selectedIP, err := selectConnectionIP(ctx, client, agentID, out, currentStep, totalSteps)
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
func selectWorker(ctx context.Context, client *api.Client, args []string, out *tui.Output, stepNum, totalSteps int) (workerID, workerName, agentID string, err error) {
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

	// Show step header
	tui.StepHeader(stepNum, totalSteps, "Select Worker")

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
	// Default to first worker
	selectedWorkerID, err := tui.SelectPromptWithDefault("Select a worker to share:", options, 0, false)
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
func selectConnectionIP(ctx context.Context, client *api.Client, agentID string, out *tui.Output, stepNum, totalSteps int) (string, error) {
	if out.IsJSON() {
		return "", fmt.Errorf("--connection-ip is required in JSON output mode")
	}

	// Show step header
	tui.StepHeader(stepNum, totalSteps, "Select Connection IP")

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

	// Show IP selection with default first option and allow custom input
	options := tui.FormatIPOptions(networkIPs)
	return tui.SelectPromptWithDefault("Select connection IP address:", options, 0, true)
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
