package studio

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/deps"
	"github.com/NexusGPU/gpu-go/internal/studio"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var (
	mode          string
	image         string
	shareLink     string
	serverURL     string
	sshKey        string
	ports         []string
	volumes       []string
	envVars       []string
	cpus          float64
	memory        string
	noSSH         bool
	colimaProfile string
	wslDistro     string
	dockerHost    string
	outputFormat  string
)

// NewStudioCmd creates the studio command
func NewStudioCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "studio",
		Short: "Manage AI development studio environments",
		Long: `Create and manage AI development studio environments across various platforms.

Supported platforms:
  - wsl:    Windows Subsystem for Linux (Windows only)
  - colima: Colima container runtime (macOS/Linux)
  - docker: Native Docker
  - k8s:    Kubernetes (kind, minikube, etc.)
  - auto:   Auto-detect best available platform

Examples:
  # Create a new studio environment with remote GPU
  ggo studio create my-studio -s "https://worker.example.com:9001"

  # Create with specific mode
  ggo studio create my-studio --mode wsl -s "https://..."

  # Create with specific Colima profile
  ggo studio create my-studio --mode colima --colima-profile myprofile

  # Create with specific WSL distribution
  ggo studio create my-studio --mode wsl --wsl-distro Ubuntu-22.04

  # Create with custom Docker socket path
  ggo studio create my-studio --docker-host unix:///path/to/docker.sock

  # List all environments
  ggo studio list

  # Connect to an environment
  ggo studio ssh my-studio

  # Stop an environment
  ggo studio stop my-studio

  # Remove an environment
  ggo studio rm my-studio`,
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

	cmdutil.AddOutputFlag(cmd, &outputFormat)

	cmd.AddCommand(newCreateCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newStartCmd())
	cmd.AddCommand(newStopCmd())
	cmd.AddCommand(newRemoveCmd())
	cmd.AddCommand(newSSHCmd())
	cmd.AddCommand(newLogsCmd())
	cmd.AddCommand(newImagesCmd())
	cmd.AddCommand(newBackendsCmd())

	return cmd
}

func getManager() *studio.Manager {
	mgr := studio.NewManager()

	dockerBackend := studio.NewDockerBackend()
	if dockerHost != "" {
		dockerBackend = studio.NewDockerBackendWithHost(dockerHost)
	}
	mgr.RegisterBackend(dockerBackend)

	colimaBackend := studio.NewColimaBackend()
	if colimaProfile != "" {
		colimaBackend = studio.NewColimaBackendWithProfile(colimaProfile)
	}
	if dockerHost != "" {
		colimaBackend.SetDockerHost(dockerHost)
	}
	mgr.RegisterBackend(colimaBackend)

	wslBackend := studio.NewWSLBackend()
	if wslDistro != "" {
		wslBackend = studio.NewWSLBackendWithDistro(wslDistro)
	}
	mgr.RegisterBackend(wslBackend)

	mgr.RegisterBackend(studio.NewAppleContainerBackend())

	return mgr
}

func getOutput() *tui.Output {
	return cmdutil.NewOutput(outputFormat)
}

func newCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new studio environment",
		Long: `Create a new AI development studio environment.

The environment will be configured with:
  - SSH server for VS Code Remote connection
  - GPU environment variables for remote GPU access
  - Pre-installed AI/ML libraries (based on image)

Examples:
  # Create with share link (required for GPU access)
  ggo studio create my-env -s abc123

  # Create with specific image
  ggo studio create my-env -s abc123 --image tensorfusion/studio-torch:latest

  # Create with WSL on Windows (specific distro)
  ggo studio create my-env --mode wsl --wsl-distro Ubuntu-22.04 -s abc123

  # Create with Colima on macOS (specific profile)
  ggo studio create my-env --mode colima --colima-profile myprofile -s abc123

  # Create with custom Docker socket
  ggo studio create my-env -s abc123 --docker-host unix://$HOME/.colima/custom/docker.sock

  # Create with custom ports and volumes (mount project directory)
  ggo studio create my-env -s abc123 -p 8888:8888 -v ~/projects:/workspace

  # Best practice: mount user data directory to prevent data loss on studio rebuild
  ggo studio create my-env -s abc123 -v ~/data:/data`,
		Args: cobra.ExactArgs(1),
		RunE: runCreate,
	}

	cmd.Flags().StringVarP(&mode, "mode", "m", "", "Container/VM mode (wsl, colima, docker, k8s, auto)")
	cmd.Flags().StringVarP(&image, "image", "i", "tensorfusion/studio-torch:latest", "Container image")
	cmd.Flags().StringVarP(&shareLink, "share-link", "s", "", "Share link or share code to remote vGPU worker (required)")
	cmd.Flags().StringVar(&serverURL, "server", api.GetDefaultBaseURL(), "Server URL for resolving share links")
	cmd.Flags().StringVar(&sshKey, "ssh-key", "", "SSH public key to authorize")
	cmd.Flags().StringArrayVarP(&ports, "port", "p", nil, "Port mappings (host:container)")
	cmd.Flags().StringArrayVarP(&volumes, "volume", "v", nil, "Volume mounts (host:container[:ro])")
	cmd.Flags().StringArrayVarP(&envVars, "env", "e", nil, "Environment variables (KEY=VALUE)")
	cmd.Flags().Float64Var(&cpus, "cpus", 0, "CPU limit")
	cmd.Flags().StringVar(&memory, "memory", "", "Memory limit (e.g., 8Gi)")
	cmd.Flags().BoolVar(&noSSH, "no-ssh", false, "Don't configure SSH")
	cmd.Flags().StringVar(&colimaProfile, "colima-profile", "", "Colima profile name (default: 'default')")
	cmd.Flags().StringVar(&wslDistro, "wsl-distro", "", "WSL distribution name (default: use default distro)")
	cmd.Flags().StringVar(&dockerHost, "docker-host", "", "Custom Docker socket path (e.g., unix:///path/to/docker.sock)")

	return cmd
}

func runCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	ctx := context.Background()
	mgr := getManager()
	out := getOutput()

	// Resolve share link if provided
	var shareInfo *api.SharePublicInfo
	if shareLink != "" {
		shortCode := extractShortCode(shareLink)
		client := api.NewClient(api.WithBaseURL(serverURL))

		var err error
		shareInfo, err = client.GetSharePublic(ctx, shortCode)
		if err != nil {
			cmd.SilenceUsage = true
			klog.Errorf("Failed to resolve share link: error=%v", err)
			return fmt.Errorf("failed to resolve share link '%s': %w", shareLink, err)
		}
		klog.Infof("Resolved share link: worker_id=%s vendor=%s connection_url=%s",
			shareInfo.WorkerID, shareInfo.HardwareVendor, shareInfo.ConnectionURL)

		// Download required GPU client libraries before creating studio
		// Filter by vendor from share info to avoid downloading unnecessary libraries
		if err := ensureRemoteGPUClientLibs(ctx, out, shareInfo.HardwareVendor); err != nil {
			cmd.SilenceUsage = true
			klog.Errorf("Failed to ensure GPU client libraries: error=%v", err)
			return fmt.Errorf("failed to download GPU client libraries: %w", err)
		}
	}

	opts, err := buildCreateOptions(name, shareInfo)
	if err != nil {
		return err
	}

	if !out.IsJSON() {
		styles := tui.DefaultStyles()
		out.Printf("%s Creating studio environment '%s'...\n",
			styles.Info.Render("◐"),
			styles.Bold.Render(name))
		if shareInfo != nil {
			out.Printf("   GPU: %s (vendor: %s)\n", shareInfo.WorkerID, shareInfo.HardwareVendor)
		}
	}

	env, err := mgr.Create(ctx, opts)
	if err != nil {
		cmd.SilenceUsage = true
		return err
	}

	return out.Render(&createResult{env: env, mgr: mgr, noSSH: noSSH})
}

// ensureRemoteGPUClientLibs downloads remote-gpu-client libraries if not already present
// vendorSlug filters by vendor (e.g., "nvidia", "amd") to avoid downloading unnecessary libraries
func ensureRemoteGPUClientLibs(ctx context.Context, out *tui.Output, vendorSlug string) error {
	depsMgr := deps.NewManager()

	// Target library types that are needed for GPU client functionality
	targetTypes := []string{deps.LibraryTypeRemoteGPUClient, deps.LibraryTypeVGPULibrary}

	if !out.IsJSON() {
		out.Printf("Downloading GPU client libraries for %s...\n", vendorSlug)
	}

	progressFn := func(lib deps.Library, downloaded, total int64) {
		if !out.IsJSON() && total > 0 {
			pct := float64(downloaded) / float64(total) * 100
			fmt.Printf("\r  %s: %.1f%%", lib.Name, pct)
		}
	}

	libs, err := depsMgr.EnsureLibrariesByTypes(ctx, targetTypes, vendorSlug, progressFn)
	if err != nil {
		return fmt.Errorf("failed to ensure GPU client libraries: %w", err)
	}

	if !out.IsJSON() {
		if len(libs) > 0 {
			fmt.Println()
			out.Success("GPU client libraries downloaded successfully!")
		} else {
			klog.V(4).Info("All required GPU client libraries are already downloaded")
		}
	}

	return nil
}

// extractShortCode extracts the short code from a share link URL
func extractShortCode(input string) string {
	input = strings.TrimSpace(input)
	if strings.Contains(input, "/") {
		parts := strings.Split(strings.TrimSuffix(input, "/"), "/")
		if len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	return input
}

func buildCreateOptions(name string, shareInfo *api.SharePublicInfo) (*studio.CreateOptions, error) {
	studioMode := studio.ModeAuto
	if mode != "" {
		studioMode = studio.Mode(mode)
	}

	portMappings, err := parsePorts(ports)
	if err != nil {
		return nil, err
	}

	volumeMounts, err := parseVolumes(volumes)
	if err != nil {
		return nil, err
	}

	envMap, err := parseEnvVars(envVars)
	if err != nil {
		return nil, err
	}

	// Set GPU connection info from share link
	gpuWorkerURL := ""
	hardwareVendor := ""
	if shareInfo != nil {
		gpuWorkerURL = shareInfo.ConnectionURL
		hardwareVendor = shareInfo.HardwareVendor
	}

	return &studio.CreateOptions{
		Name:           name,
		Mode:           studioMode,
		Image:          image,
		GPUWorkerURL:   gpuWorkerURL,
		HardwareVendor: hardwareVendor,
		SSHPublicKey:   sshKey,
		Ports:          portMappings,
		Volumes:        volumeMounts,
		Envs:           envMap,
		Resources: studio.ResourceSpec{
			CPUs:   cpus,
			Memory: memory,
		},
	}, nil
}

func parsePorts(ports []string) ([]studio.PortMapping, error) {
	var mappings []studio.PortMapping
	for _, p := range ports {
		parts := strings.Split(p, ":")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid port format: %s (expected host:container)", p)
		}
		var hostPort, containerPort int
		if _, err := fmt.Sscanf(parts[0], "%d", &hostPort); err != nil {
			return nil, fmt.Errorf("invalid host port: %s", parts[0])
		}
		if _, err := fmt.Sscanf(parts[1], "%d", &containerPort); err != nil {
			return nil, fmt.Errorf("invalid container port: %s", parts[1])
		}
		mappings = append(mappings, studio.PortMapping{
			HostPort:      hostPort,
			ContainerPort: containerPort,
		})
	}
	return mappings, nil
}

func parseVolumes(volumes []string) ([]studio.VolumeMount, error) {
	var mounts []studio.VolumeMount
	for _, v := range volumes {
		parts := strings.Split(v, ":")
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid volume format: %s (expected host:container[:ro])", v)
		}
		mount := studio.VolumeMount{
			HostPath:      parts[0],
			ContainerPath: parts[1],
		}
		if len(parts) > 2 && parts[2] == "ro" {
			mount.ReadOnly = true
		}
		mounts = append(mounts, mount)
	}
	return mounts, nil
}

func parseEnvVars(envVars []string) (map[string]string, error) {
	envMap := make(map[string]string)
	for _, e := range envVars {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid env var format: %s (expected KEY=VALUE)", e)
		}
		envMap[parts[0]] = parts[1]
	}
	return envMap, nil
}

// createResult implements Renderable for create command output
type createResult struct {
	env   *studio.Environment
	mgr   *studio.Manager
	noSSH bool
}

func (r *createResult) RenderJSON() any {
	return r.env
}

func (r *createResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()
	env := r.env

	out.Println()
	out.Success("Studio environment created successfully!")
	out.Println()

	status := tui.NewStatusTable().
		Add("Name", styles.Bold.Render(env.Name)).
		Add("ID", env.ID).
		Add("Mode", string(env.Mode)).
		Add("Image", env.Image).
		AddWithStatus("Status", string(env.Status), string(env.Status))

	out.Println(status.String())

	if env.SSHPort > 0 && !r.noSSH {
		if err := r.mgr.AddSSHConfig(env); err != nil {
			klog.Warningf("Failed to add SSH config: error=%v", err)
		} else {
			out.Println()
			out.Println(styles.Subtitle.Render("SSH Configuration"))
			out.Println()

			sshStatus := tui.NewStatusTable().
				Add("Host", fmt.Sprintf("ggo-%s", env.Name)).
				Add("Port", fmt.Sprintf("%d", env.SSHPort)).
				Add("User", env.SSHUser)

			out.Println(sshStatus.String())

			out.Println()
			out.Println(styles.Subtitle.Render("Connect with:"))
			out.Println()
			out.Println("  " + tui.Code(fmt.Sprintf("ssh ggo-%s", env.Name)))
			out.Println()
			out.Println(styles.Subtitle.Render("Or in VS Code:"))
			out.Println()
			out.Println("  1. Install 'Remote - SSH' extension")
			out.Println("  2. Press F1 → 'Remote-SSH: Connect to Host...'")
			out.Printf("  3. Select '%s'\n", styles.Bold.Render(fmt.Sprintf("ggo-%s", env.Name)))
		}
	}
	out.Println()
}

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List all studio environments",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()
			out := getOutput()

			envs, err := mgr.List(ctx)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			return out.Render(&envListResult{envs: envs})
		},
	}
}

// envListResult implements Renderable for list command
type envListResult struct {
	envs []*studio.Environment
}

func (r *envListResult) RenderJSON() any {
	return tui.NewListResult(r.envs)
}

func (r *envListResult) RenderTUI(out *tui.Output) {
	if len(r.envs) == 0 {
		out.Info("No studio environments found")
		return
	}

	styles := tui.DefaultStyles()
	var rows [][]string
	for _, env := range r.envs {
		statusIcon := tui.StatusIcon(string(env.Status))
		statusStyled := styles.StatusStyle(string(env.Status)).Render(statusIcon + " " + string(env.Status))

		sshInfo := styles.Muted.Render("-")
		if env.SSHPort > 0 {
			sshInfo = fmt.Sprintf("%s:%d", env.SSHHost, env.SSHPort)
		}

		rows = append(rows, []string{
			styles.Bold.Render(env.Name),
			truncate(env.ID, 12),
			string(env.Mode),
			statusStyled,
			truncateImage(env.Image),
			sshInfo,
		})
	}

	table := tui.NewTable().
		Headers("NAME", "ID", "MODE", "STATUS", "IMAGE", "SSH").
		Rows(rows)

	out.Println(table.String())
}

func newStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start <name>",
		Short: "Start a stopped studio environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()
			out := getOutput()

			if err := mgr.Start(ctx, args[0]); err != nil {
				cmd.SilenceUsage = true
				return err
			}

			return out.Render(&cmdutil.ActionData{
				Success: true,
				Message: fmt.Sprintf("Environment '%s' started", args[0]),
				ID:      args[0],
			})
		},
	}
}

func newStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop <name>",
		Short: "Stop a running studio environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()
			out := getOutput()

			if err := mgr.Stop(ctx, args[0]); err != nil {
				cmd.SilenceUsage = true
				return err
			}

			return out.Render(&cmdutil.ActionData{
				Success: true,
				Message: fmt.Sprintf("Environment '%s' stopped", args[0]),
				ID:      args[0],
			})
		},
	}
}

func newRemoveCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:     "rm <name>",
		Short:   "Remove a studio environment",
		Aliases: []string{"remove", "delete"},
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()
			out := getOutput()
			name := args[0]

			if !force && !out.IsJSON() {
				styles := tui.DefaultStyles()
				fmt.Printf("%s Are you sure you want to remove environment %s? [y/N]: ",
					styles.Warning.Render("!"),
					styles.Bold.Render(name))
				var confirm string
				fmt.Scanln(&confirm)
				if confirm != "y" && confirm != "Y" {
					out.Info("Cancelled")
					return nil
				}
			}

			if err := mgr.Remove(ctx, name); err != nil {
				cmd.SilenceUsage = true
				return err
			}

			if err := mgr.RemoveSSHConfig(name); err != nil {
				klog.Warningf("Failed to remove SSH config: error=%v", err)
			}

			return out.Render(&cmdutil.ActionData{
				Success: true,
				Message: fmt.Sprintf("Environment '%s' removed", name),
				ID:      name,
			})
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Force remove")
	return cmd
}

func newSSHCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ssh <name>",
		Short: "SSH into a studio environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()
			out := getOutput()

			env, err := mgr.Get(ctx, args[0])
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			if env.SSHPort == 0 {
				cmd.SilenceUsage = true
				return fmt.Errorf("SSH not configured for this environment")
			}

			return out.Render(&sshResult{env: env})
		},
	}
}

// sshResult implements Renderable for ssh command
type sshResult struct {
	env *studio.Environment
}

func (r *sshResult) RenderJSON() any {
	return map[string]any{
		"host":    r.env.SSHHost,
		"port":    r.env.SSHPort,
		"user":    r.env.SSHUser,
		"command": fmt.Sprintf("ssh -p %d %s@%s", r.env.SSHPort, r.env.SSHUser, r.env.SSHHost),
	}
}

func (r *sshResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()
	sshCmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p %d %s@%s",
		r.env.SSHPort, r.env.SSHUser, r.env.SSHHost)

	out.Printf("%s Connecting to %s...\n",
		styles.Info.Render("◐"),
		styles.Bold.Render(r.env.Name))
	out.Println()
	out.Println("  " + tui.Code(sshCmd))
	out.Println()
	out.Println(styles.Muted.Render(fmt.Sprintf("Please run: ssh -p %d %s@%s", r.env.SSHPort, r.env.SSHUser, r.env.SSHHost)))
}

func newLogsCmd() *cobra.Command {
	var follow bool

	cmd := &cobra.Command{
		Use:   "logs <name>",
		Short: "Show logs from a studio environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()

			env, err := mgr.Get(ctx, args[0])
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			backend, err := mgr.GetBackend(env.Mode)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			logCh, err := backend.Logs(ctx, env.ID, follow)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			for line := range logCh {
				fmt.Print(line)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow log output")
	return cmd
}

func newImagesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "images",
		Short: "List available studio images",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := getOutput()
			images := studio.DefaultImages()
			return out.Render(&imagesResult{images: images})
		},
	}
}

// imagesResult implements Renderable for images command
type imagesResult struct {
	images []studio.StudioImage
}

func (r *imagesResult) RenderJSON() any {
	return tui.NewListResult(r.images)
}

func (r *imagesResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()
	var rows [][]string
	for _, img := range r.images {
		rows = append(rows, []string{
			styles.Bold.Render(fmt.Sprintf("%s:%s", img.Name, img.Tag)),
			img.Description,
			styles.Muted.Render(strings.Join(img.Features, ", ")),
		})
	}

	table := tui.NewTable().
		Headers("IMAGE", "DESCRIPTION", "FEATURES").
		Rows(rows)

	out.Println(table.String())
}

func newBackendsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backends",
		Short: "List available container/VM backends",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()
			out := getOutput()

			backends := mgr.ListAvailableBackends(ctx)
			return out.Render(&backendsResult{backends: backends})
		},
	}
}

// backendsResult implements Renderable for backends command
type backendsResult struct {
	backends []studio.Backend
}

func (r *backendsResult) RenderJSON() any {
	type backendInfo struct {
		Name string `json:"name"`
		Mode string `json:"mode"`
	}
	var result []backendInfo
	for _, b := range r.backends {
		result = append(result, backendInfo{
			Name: b.Name(),
			Mode: string(b.Mode()),
		})
	}
	return tui.NewListResult(result)
}

func (r *backendsResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()

	if len(r.backends) == 0 {
		out.Warning("No backends available")
		out.Println()
		out.Println(styles.Subtitle.Render("Install one of the following:"))
		out.Println()
		out.Println("  • " + styles.Bold.Render("Docker:") + " " + tui.URL("https://docs.docker.com/get-docker/"))
		out.Println("  • " + styles.Bold.Render("Colima (macOS):") + " " + tui.Code("brew install colima"))
		out.Println("  • " + styles.Bold.Render("WSL (Windows):") + " " + tui.URL("https://docs.microsoft.com/en-us/windows/wsl/install"))
		out.Println()
		return
	}

	out.Println()
	out.Println(styles.Title.Render("Available Backends"))
	out.Println()

	var rows [][]string
	for _, b := range r.backends {
		rows = append(rows, []string{
			styles.Bold.Render(b.Name()),
			string(b.Mode()),
			styles.Success.Render("● available"),
		})
	}

	table := tui.NewTable().
		Headers("NAME", "MODE", "STATUS").
		Rows(rows)

	out.Println(table.String())
}

// Helper functions

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func truncateImage(image string) string {
	parts := strings.Split(image, "/")
	if len(parts) > 2 {
		image = strings.Join(parts[len(parts)-2:], "/")
	}
	return truncate(image, 30)
}
