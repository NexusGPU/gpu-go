package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	"github.com/NexusGPU/gpu-go/cmd/ggo/auth"
	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
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

const (
	stateRunning = "running"
	stateStopped = "stopped"
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
	cmdutil.AddOutputFlag(cmd, &outputFormat)
	cmd.PersistentFlags().StringVar(&acceleratorLib, "accelerator-lib", "", "Path to accelerator library (auto-detected if not specified)")
	cmd.PersistentFlags().StringVar(&isolationMode, "isolation-mode", "shared", "Worker isolation mode (shared, soft, partitioned)")

	cmd.AddCommand(newRegisterCmd())
	cmd.AddCommand(newStartCmd())
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newGetCmd())

	return cmd
}

func getOutput() *tui.Output {
	return cmdutil.NewOutput(outputFormat)
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
			client := api.NewClient(api.WithBaseURL(serverURL))

			if token == "" {
				token = os.Getenv("GPU_GO_TOKEN")
			}
			if token == "" {
				if !out.IsJSON() {
					out.Error("Token is required. Use --token flag or GPU_GO_TOKEN environment variable")
				}
				return fmt.Errorf("token is required")
			}

			configMgr := config.NewManager(configDir, stateDir)
			registered, err := configMgr.IsRegistered()
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to check registration status: error=%v", err)
				return err
			}
			if registered {
				cmd.SilenceUsage = true
				if !out.IsJSON() {
					out.Error("Agent already registered on this machine. Please run the uninstall command and register again.")
				}
				return agent.ErrAlreadyRegistered
			}

			// Sync deps manifest before registration
			depsMgr := deps.NewManager(
				deps.WithPaths(paths),
				deps.WithAPIClient(client),
			)
			if _, err := depsMgr.SyncReleases(context.Background(), "", ""); err != nil {
				klog.Fatalf("Failed to sync deps manifest: error=%v", err)
			}

			gpus, err := discoverGPUs()
			if err != nil {
				cmd.SilenceUsage = true
				if !out.IsJSON() {
					out.Error(fmt.Sprintf("Failed to discover GPUs: %v", err))
				}
				return err
			}
			if len(gpus) == 0 {
				cmd.SilenceUsage = true
				if !out.IsJSON() {
					out.Error("No GPUs found. Please check your GPU configuration or use GPU_GO_MOCK_GPUS for testing")
				}
				return fmt.Errorf("no GPUs found")
			}

			agentInstance := agent.NewAgent(client, configMgr)
			if err := agentInstance.Register(token, gpus); err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to register agent: error=%v", err)
				return err
			}

			return out.Render(&cmdutil.ActionData{
				Success: true,
				Message: "Agent registered successfully!",
			})
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

			if !configMgr.ConfigExists() {
				cmd.SilenceUsage = true
				if !out.IsJSON() {
					out.Error("Agent is not registered. Please run 'ggo agent register' first")
				}
				return agent.ErrNotRegistered
			}

			cfg, err := configMgr.LoadConfig()
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to load config: error=%v", err)
				return err
			}

			setProductNameEnv(cfg.License)

			client := api.NewClient(
				api.WithBaseURL(serverURL),
				api.WithAgentSecret(cfg.AgentSecret),
			)

			hvMgr, err := getHypervisorManager()
			if err != nil {
				cmd.SilenceUsage = true
				if !out.IsJSON() {
					out.Error(fmt.Sprintf("Failed to initialize hypervisor manager: %v", err))
				}
				return err
			}

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
						out.Error(fmt.Sprintf("Failed to get remote-gpu-worker binary: %v", err))
						out.Println(tui.Muted("The agent requires remote-gpu-worker to manage workers. Please ensure the binary is available for your platform."))
					}
					return err
				}
				klog.V(4).Infof("Using remote-gpu-worker binary: path=%s", workerBinaryPath)
			}

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
				out.Printf("%s Agent started (ID: %s)\n",
					styles.Success.Render("●"),
					styles.Bold.Render(cfg.AgentID))
				if hvMgr != nil {
					out.Printf("%s Hypervisor integration enabled (vendor: %s)\n",
						styles.Success.Render("●"),
						hvMgr.GetVendor())
				}
				out.Println(tui.Muted("Press Ctrl+C to stop..."))
			}

			sigCh := make(chan os.Signal, 1)
			signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
			<-sigCh

			if !out.IsJSON() {
				out.Info("Shutting down...")
			}

			agentInstance.Stop()
			stopHypervisorManager()
			return nil
		},
	}

	return cmd
}

func newListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all agents",
		Long:  `List all registered GPU agents (machines) for the current user.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := getUserClient()
			ctx := context.Background()
			out := getOutput()

			resp, err := client.ListAgents(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to list agents: error=%v", err)
				return err
			}

			return out.Render(&agentListResult{agents: resp.Agents})
		},
	}

	return cmd
}

// getUserClient creates an API client authenticated with user token (PAT)
func getUserClient() *api.Client {
	token := os.Getenv("GPU_GO_TOKEN")
	if token == "" {
		token = os.Getenv("GPU_GO_USER_TOKEN")
	}
	if token == "" {
		cfgMgr := config.NewManager(configDir, stateDir)
		if agentCfg, err := cfgMgr.LoadConfig(); err == nil && agentCfg != nil && agentCfg.AgentSecret != "" {
			token = agentCfg.AgentSecret
		}
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

// agentListResult implements Renderable for agent list
type agentListResult struct {
	agents []api.AgentInfo
}

func (r *agentListResult) RenderJSON() any {
	return tui.NewListResult(r.agents)
}

func (r *agentListResult) RenderTUI(out *tui.Output) {
	if len(r.agents) == 0 {
		out.Info("No agents found")
		return
	}

	styles := tui.DefaultStyles()
	var rows [][]string
	for _, a := range r.agents {
		statusIcon := tui.StatusIcon(a.Status)
		statusStyled := styles.StatusStyle(a.Status).Render(statusIcon + " " + a.Status)

		gpuSummary := "-"
		if len(a.GPUs) > 0 {
			gpuSummary = fmt.Sprintf("%d", len(a.GPUs))
			if a.GPUs[0].Model != "" {
				gpuSummary = fmt.Sprintf("%d x %s", len(a.GPUs), a.GPUs[0].Model)
			}
		}

		rows = append(rows, []string{
			a.AgentID,
			a.Hostname,
			statusStyled,
			fmt.Sprintf("%s/%s", a.OS, a.Arch),
			gpuSummary,
			fmt.Sprintf("%d", len(a.Workers)),
		})
	}

	table := tui.NewTable().
		Headers("AGENT ID", "HOSTNAME", "STATUS", "OS/ARCH", "GPUS", "WORKERS").
		Rows(rows)

	out.Println(table.String())
}

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <agent-id>",
		Short: "Get agent details",
		Long:  `Get detailed information about a specific agent including GPUs and workers.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]
			client := getUserClient()
			ctx := context.Background()
			out := getOutput()

			resp, err := client.GetAgent(ctx, agentID)
			if err != nil {
				cmd.SilenceUsage = true
				klog.Errorf("Failed to get agent: error=%v", err)
				return err
			}

			return out.Render(&agentDetailResult{agent: resp})
		},
	}

	return cmd
}

// agentDetailResult implements Renderable for agent detail
type agentDetailResult struct {
	agent *api.AgentInfo
}

func (r *agentDetailResult) RenderJSON() any {
	return tui.NewDetailResult(r.agent)
}

func (r *agentDetailResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()
	a := r.agent

	out.Println()
	out.Println(styles.Title.Render("Agent Details"))
	out.Println()

	statusIcon := tui.StatusIcon(a.Status)
	statusStyled := styles.StatusStyle(a.Status).Render(statusIcon + " " + a.Status)

	status := tui.NewStatusTable().
		Add("Agent ID", a.AgentID).
		Add("Hostname", a.Hostname).
		Add("Status", statusStyled).
		Add("OS/Arch", fmt.Sprintf("%s/%s", a.OS, a.Arch))

	if len(a.NetworkIPs) > 0 {
		status.Add("Network IPs", fmt.Sprintf("%v", a.NetworkIPs))
	}

	out.Println(status.String())

	if len(a.GPUs) > 0 {
		out.Println()
		out.Println(styles.Subtitle.Render(fmt.Sprintf("GPUs (%d)", len(a.GPUs))))
		out.Println()

		var rows [][]string
		for _, g := range a.GPUs {
			vramGb := fmt.Sprintf("%.1f GB", float64(g.VRAMMb)/1024)
			rows = append(rows, []string{
				g.GPUID,
				g.Vendor,
				g.Model,
				vramGb,
				g.DriverVersion,
			})
		}

		table := tui.NewTable().
			Headers("GPU ID", "VENDOR", "MODEL", "VRAM", "DRIVER").
			Rows(rows)

		out.Println(table.String())
	}

	if len(a.Workers) > 0 {
		out.Println()
		out.Println(styles.Subtitle.Render(fmt.Sprintf("Workers (%d)", len(a.Workers))))
		out.Println()

		var rows [][]string
		for _, w := range a.Workers {
			statusIcon := tui.StatusIcon(w.Status)
			statusStyled := styles.StatusStyle(w.Status).Render(statusIcon + " " + w.Status)
			rows = append(rows, []string{
				w.WorkerID,
				w.Name,
				statusStyled,
				fmt.Sprintf("%d", w.ListenPort),
			})
		}

		table := tui.NewTable().
			Headers("WORKER ID", "NAME", "STATUS", "PORT").
			Rows(rows)

		out.Println(table.String())
	}
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show agent status",
		Long:  `Show the current status of the GPU agent (server-side and local).`,
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
				return out.Render(&agentStatusResult{registered: false})
			}

			// Get local status by checking PID file
			localStatus := agent.GetLocalStatus(paths)

			// Get server-side status
			client := api.NewClient(
				api.WithBaseURL(serverURL),
				api.WithAgentSecret(cfg.AgentSecret),
			)

			ctx := context.Background()
			agentConfig, err := client.GetAgentConfig(ctx, cfg.AgentID)
			if err != nil {
				cmd.SilenceUsage = true
				if !out.IsJSON() {
					out.Error(fmt.Sprintf("Failed to fetch config from server: %v", err))
				}
				return err
			}

			return out.Render(&agentStatusResult{
				registered:  true,
				cfg:         cfg,
				agentConfig: agentConfig,
				localStatus: localStatus,
			})
		},
	}

	return cmd
}

// agentStatusResult implements Renderable for agent status
type agentStatusResult struct {
	registered  bool
	cfg         *config.Config
	agentConfig *api.AgentConfigResponse
	localStatus agent.LocalStatus
}

func (r *agentStatusResult) RenderJSON() any {
	if !r.registered {
		return map[string]any{
			"registered": false,
			"message":    "Agent is not registered",
		}
	}

	localState := stateStopped
	if r.localStatus.Running {
		localState = stateRunning
	}

	result := map[string]any{
		"registered":     true,
		"agent_id":       r.cfg.AgentID,
		"config_version": r.cfg.ConfigVersion,
		"server_url":     r.cfg.ServerURL,
		"local_status": map[string]any{
			"state": localState,
			"pid":   r.localStatus.PID,
		},
	}

	if r.agentConfig != nil {
		result["config_version"] = r.agentConfig.ConfigVersion

		workers := make([]map[string]any, len(r.agentConfig.Workers))
		for i, w := range r.agentConfig.Workers {
			workers[i] = map[string]any{
				"worker_id":      w.WorkerID,
				"gpu_ids":        w.GPUIDs,
				"listen_port":    w.ListenPort,
				"enabled":        w.Enabled,
				"isolation_mode": w.IsolationMode,
			}
		}
		result["workers"] = workers
	}
	return result
}

func (r *agentStatusResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()

	if !r.registered {
		out.Warning("Agent is not registered")
		return
	}

	out.Println()
	out.Println(styles.Title.Render("Agent Status"))
	out.Println()

	configVersion := r.cfg.ConfigVersion
	if r.agentConfig != nil {
		configVersion = r.agentConfig.ConfigVersion
	}

	// Local status
	localState := stateStopped
	localPID := "-"
	if r.localStatus.Running {
		localState = stateRunning
		localPID = strconv.Itoa(r.localStatus.PID)
	} else if r.localStatus.PID > 0 {
		localPID = fmt.Sprintf("%d (not running)", r.localStatus.PID)
	}

	localStateStyled := styles.StatusStyle(localState).Render(tui.StatusIcon(localState) + " " + localState)

	status := tui.NewStatusTable().
		Add("Agent ID", r.cfg.AgentID).
		Add("Config Version", fmt.Sprintf("%d", configVersion)).
		Add("Server URL", r.cfg.ServerURL).
		Add("Local Status", localStateStyled).
		Add("Local PID", localPID)

	out.Println(status.String())

	if r.agentConfig != nil && len(r.agentConfig.Workers) > 0 {
		out.Println()
		out.Println(styles.Subtitle.Render(fmt.Sprintf("Workers (%d)", len(r.agentConfig.Workers))))
		out.Println()

		var rows [][]string
		for _, w := range r.agentConfig.Workers {
			enabledIcon := tui.StatusIcon(boolToYesNo(w.Enabled))
			enabledStyled := styles.StatusStyle(boolToYesNo(w.Enabled)).Render(enabledIcon)

			rows = append(rows, []string{
				w.WorkerID,
				fmt.Sprintf("%d", w.ListenPort),
				enabledStyled,
				w.IsolationMode,
			})
		}

		table := tui.NewTable().
			Headers("ID", "PORT", "ENABLED", "ISOLATION").
			Rows(rows)

		out.Println(table.String())
	}
}

// discoverGPUs discovers GPUs using hypervisor or returns mock GPUs
func discoverGPUs() ([]api.GPUInfo, error) {
	if mockCount := os.Getenv("GPU_GO_MOCK_GPUS"); mockCount != "" {
		count, err := strconv.Atoi(mockCount)
		if err != nil || count <= 0 {
			count = 1
		}
		klog.Infof("Using mock GPUs for testing: count=%d", count)
		return agent.CreateMockGPUs(count), nil
	}

	hvMgr, err := getHypervisorManager()
	if err != nil {
		return nil, err
	}

	devices, err := hvMgr.ListDevices()
	if err != nil {
		return nil, fmt.Errorf("failed to list devices: %w", err)
	}

	return agent.ConvertDevicesToGPUInfo(devices), nil
}

func boolToYesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// parseLicenseType parses the license type from the license plain text
func parseLicenseType(license api.License) string {
	if license.Plain == "" {
		return ""
	}

	var licenseData map[string]any
	if err := json.Unmarshal([]byte(license.Plain), &licenseData); err != nil {
		return ""
	}

	if licenseType, ok := licenseData["type"].(string); ok {
		return licenseType
	}

	if licenseType, ok := licenseData["license_type"].(string); ok {
		return licenseType
	}

	return ""
}

// setProductNameEnv sets the product name environment variable based on license type
func setProductNameEnv(license api.License) {
	licenseType := parseLicenseType(license)

	var productName string
	switch licenseType {
	case "free":
		productName = "gpu-go-free"
	case "paid":
		productName = "gpu-go-paid"
	default:
		return
	}

	if err := os.Setenv("GPU_GO_PRODUCT_NAME", productName); err != nil {
		klog.Warningf("Failed to set GPU_GO_PRODUCT_NAME environment variable: error=%v", err)
		return
	}

	klog.Infof("License type detected: type=%s, product_name=%s", licenseType, productName)
}
