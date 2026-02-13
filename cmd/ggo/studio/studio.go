package studio

import (
	"context"
	"flag"
	"fmt"
	"sort"
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
	command       []string
	endpoint      string
	platform      string // container platform (e.g., linux/amd64, linux/arm64)
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
  - apple-container: Apple Container (macOS 26+)
  - docker: Native Docker
  - k8s:    Kubernetes (kind, minikube, etc.)
  - auto:   Auto-detect best available platform

Examples:
  # Create a new studio environment with remote GPU
  ggo studio create my-studio -s "https://worker.example.com:9001"

  # Create with specific mode
  ggo studio create my-studio --mode wsl -s "https://..."

  # Create with Apple Container (macOS 26+)
  ggo studio create my-studio --mode apple-container -s "https://..."

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
  ggo studio rm my-studio

  # Batch remove all environments
  ggo studio rm --all -f`,
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
	cmd.AddCommand(newTagsCmd())
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

// printBackendAndSocket prints current backend name and container unix sock path to TUI.
func printBackendAndSocket(ctx context.Context, out *tui.Output, mgr *studio.Manager, mode studio.Mode) {
	backend, err := mgr.GetBackend(mode)
	if err != nil {
		return
	}
	styles := tui.DefaultStyles()
	out.Println()
	out.Println(styles.Subtitle.Render("Backend"))
	sock := "—"
	if sp, ok := backend.(studio.BackendSocketPath); ok {
		if p := sp.SocketPath(ctx); p != "" {
			sock = p
		}
	}
	out.Printf("  Backend: %s\n", backend.Name())
	out.Printf("  Container unix sock: %s\n", sock)
	out.Println()
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
  ggo studio create my-env -s abc123 -v ~/data:/data

  # Create with custom startup command (supplements ENTRYPOINT args)
  ggo studio create my-env -s abc123 -c /bin/bash -c "echo hello"

  # Create with endpoint override (override GPU worker endpoint)
  ggo studio create my-env -s abc123 --endpoint "https://custom-worker.example.com:9001"`,
		Args: cobra.ExactArgs(1),
		RunE: runCreate,
	}

	cmd.Flags().StringVarP(&mode, "mode", "m", "", "Container/VM mode (wsl, colima, apple-container, docker, k8s, auto)")
	cmd.Flags().StringVarP(&image, "image", "i", "tensorfusion/studio-torch:latest", "Container image")
	cmd.Flags().StringVarP(&shareLink, "share-link", "s", "", "Share link or share code to remote vGPU worker (recommended for GPU access)")
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
	cmd.Flags().StringArrayVarP(&command, "command", "c", nil, "Container startup command or ENTRYPOINT args (can be specified multiple times)")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "Override GPU worker endpoint URL")
	cmd.Flags().StringVar(&platform, "platform", "", "Container image platform (e.g., linux/amd64, linux/arm64). Default: linux/amd64")

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

		// Append share code to connection URL for authentication
		shareInfo.ConnectionURL = shareInfo.ConnectionURL + "+" + shortCode
		klog.Infof("Resolved share link: worker_id=%s vendor=%s connection_url=%s",
			shareInfo.WorkerID, shareInfo.HardwareVendor, shareInfo.ConnectionURL)

		// Determine target arch from platform flag (default: amd64)
		targetArch := "amd64"
		if platform != "" {
			if parts := strings.SplitN(platform, "/", 2); len(parts) == 2 {
				targetArch = parts[1]
			}
		}

		// Download required GPU client libraries before creating studio
		// Filter by vendor from share info to avoid downloading unnecessary libraries
		if err := ensureRemoteGPUClientLibs(ctx, out, shareInfo.HardwareVendor, targetArch); err != nil {
			cmd.SilenceUsage = true
			klog.Errorf("Failed to ensure GPU client libraries: error=%v", err)
			return fmt.Errorf("failed to download GPU client libraries: %w", err)
		}
	} else if !out.IsJSON() {
		styles := tui.DefaultStyles()
		out.Printf("%s No share link provided. Studio will have no remote GPU access.\n",
			styles.Warning.Render("!"))
		out.Println("   Use -s <share-link> to connect to a remote GPU worker.")
		out.Println()
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

	backendName, socketPath := "", ""
	if backend, err := mgr.GetBackend(env.Mode); err == nil {
		backendName = backend.Name()
		if sp, ok := backend.(studio.BackendSocketPath); ok {
			socketPath = sp.SocketPath(ctx)
		}
	}
	return out.Render(&createResult{
		env:         env,
		mgr:         mgr,
		noSSH:       noSSH,
		backendName: backendName,
		socketPath:  socketPath,
	})
}

// ensureRemoteGPUClientLibs downloads remote-gpu-client libraries if not already present
// vendorSlug filters by vendor (e.g., "nvidia", "amd") to avoid downloading unnecessary libraries
// targetArch specifies the CPU architecture (e.g., "amd64", "arm64") for the target container platform
// Note: Studio environments run in Linux containers, so we always download Linux libraries
func ensureRemoteGPUClientLibs(ctx context.Context, out *tui.Output, vendorSlug, targetArch string) error {
	depsMgr := deps.NewManager()

	// Target library types that are needed for GPU client functionality
	targetTypes := []string{deps.LibraryTypeRemoteGPUClient, deps.LibraryTypeVGPULibrary}

	if !out.IsJSON() {
		out.Printf("Downloading GPU client libraries for %s (linux/%s)...\n", vendorSlug, targetArch)
	}

	progressFn := func(lib deps.Library, downloaded, total int64) {
		if !out.IsJSON() && total > 0 {
			pct := float64(downloaded) / float64(total) * 100
			fmt.Printf("\r  %s: %.1f%%", lib.Name, pct)
		}
	}

	// Studio environments always run in Linux containers
	// Download libraries for the specified target architecture
	libs, err := depsMgr.EnsureLibrariesByTypesForPlatform(ctx, targetTypes, vendorSlug, "linux", targetArch, progressFn)
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

	// Allow --endpoint to override the connection URL
	endpointOverride := ""
	if endpoint != "" {
		endpointOverride = endpoint
		// If endpoint is specified but no share link, use endpoint as gpuWorkerURL
		if gpuWorkerURL == "" {
			gpuWorkerURL = endpoint
		}
	}

	// Default platform to linux/amd64 for studio containers
	effectivePlatform := platform
	if effectivePlatform == "" {
		effectivePlatform = "linux/amd64"
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
		Command:     command,
		Endpoint:    endpointOverride,
		Platform:    effectivePlatform,
		UseLocalGPU: gpuWorkerURL == "" && (studioMode == studio.ModeDocker || studioMode == studio.ModeWSL || studioMode == studio.ModeAuto),
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
	env         *studio.Environment
	mgr         *studio.Manager
	noSSH       bool
	backendName string
	socketPath  string
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
	if r.backendName != "" {
		status = status.Add("Backend", r.backendName)
		sock := r.socketPath
		if sock == "" {
			sock = "—"
		}
		status = status.Add("Container unix sock", sock)
	}
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
	offlineModes := make(map[string]struct{})
	for _, env := range r.envs {
		statusIcon := tui.StatusIcon(string(env.Status))
		statusStyled := styles.StatusStyle(string(env.Status)).Render(statusIcon + " " + string(env.Status))

		sshInfo := styles.Muted.Render("N/A")
		if env.Status == studio.StatusUnknown {
			offlineModes[string(env.Mode)] = struct{}{}
		}
		if env.Status != studio.StatusUnknown && env.Status != studio.StatusDeleted && env.SSHPort > 0 && env.SSHHost != "" {
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

	if len(offlineModes) > 0 {
		modes := make([]string, 0, len(offlineModes))
		for mode := range offlineModes {
			modes = append(modes, mode)
		}
		sort.Strings(modes)
		out.Println()
		out.Warning(fmt.Sprintf("Container runtime offline: %s", strings.Join(modes, ", ")))
	}
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

			env, err := mgr.Get(ctx, args[0])
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}
			if !out.IsJSON() {
				printBackendAndSocket(ctx, out, mgr, env.Mode)
			}
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

			env, err := mgr.Get(ctx, args[0])
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}
			if !out.IsJSON() {
				printBackendAndSocket(ctx, out, mgr, env.Mode)
			}
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
	var all bool

	cmd := &cobra.Command{
		Use:     "rm <name>",
		Short:   "Remove studio environment(s)",
		Aliases: []string{"remove", "delete"},
		Args: func(cmd *cobra.Command, args []string) error {
			if all {
				if len(args) > 0 {
					return fmt.Errorf("--all does not accept a studio name")
				}
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()
			out := getOutput()

			if all {
				if !force && !out.IsJSON() {
					styles := tui.DefaultStyles()
					fmt.Printf("%s Are you sure you want to remove ALL studio environments? [y/N]: ", styles.Warning.Render("!"))
					var confirm string
					fmt.Scanln(&confirm)
					if confirm != "y" && confirm != "Y" {
						out.Info("Cancelled")
						return nil
					}
				}

				removedNames, err := mgr.RemoveAll(ctx)
				if err != nil {
					cmd.SilenceUsage = true
					return err
				}
				if len(removedNames) == 0 {
					out.Info("No studio environments found")
					return nil
				}
				for _, removedName := range removedNames {
					if err := mgr.RemoveSSHConfig(removedName); err != nil {
						klog.Warningf("Failed to remove SSH config for %s: error=%v", removedName, err)
					}
				}

				return out.Render(&cmdutil.ActionData{
					Success: true,
					Message: fmt.Sprintf("Removed %d studio environment(s)", len(removedNames)),
					ID:      "all",
				})
			}

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
	cmd.Flags().BoolVar(&all, "all", false, "Remove all studio environments in one batch")
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

func newTagsCmd() *cobra.Command {
	var registryURL string
	var limit int

	cmd := &cobra.Command{
		Use:   "tags <image>",
		Short: "List available tags for a container image",
		Long: `List available tags for a container image from its registry.

Uses the Docker V2 Registry API with credentials from ~/.docker/config.json.
Supports Docker Hub, AWS ECR, GCP GCR/Artifact Registry, Azure ACR, and
local registries — any registry that Docker CLI can authenticate to.

Examples:
  # List tags for a Docker Hub image
  ggo studio tags tensorfusion/studio-torch

  # List tags for a private registry image
  ggo studio tags myapp --registry gcr.io/my-project

  # List tags from a local registry
  ggo studio tags test/nginx --registry localhost:5555

  # Limit number of results
  ggo studio tags tensorfusion/studio-torch --limit 10`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			out := getOutput()

			imageRef := args[0]
			if registryURL != "" && registryURL != "docker.io" {
				imageRef = registryURL + "/" + imageRef
			}

			tags, err := studio.ListRemoteTags(ctx, imageRef, limit)
			if err != nil {
				cmd.SilenceUsage = true
				return err
			}

			return out.Render(&tagsResult{tags: tags, image: args[0]})
		},
	}

	cmd.Flags().StringVar(&registryURL, "registry", "", "Container registry URL (default: docker.io)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of tags to return (0 = all)")

	return cmd
}

// tagsResult implements Renderable for tags command
type tagsResult struct {
	tags  []string
	image string
}

type tagItem struct {
	Name string `json:"name"`
}

func (r *tagsResult) RenderJSON() any {
	items := make([]tagItem, len(r.tags))
	for i, t := range r.tags {
		items[i] = tagItem{Name: t}
	}
	return tui.NewListResult(items)
}

func (r *tagsResult) RenderTUI(out *tui.Output) {
	if len(r.tags) == 0 {
		out.Info(fmt.Sprintf("No tags found for '%s'", r.image))
		return
	}

	styles := tui.DefaultStyles()
	out.Println()
	out.Println(styles.Subtitle.Render(fmt.Sprintf("Tags for %s (%d)", r.image, len(r.tags))))
	out.Println()
	for _, tag := range r.tags {
		out.Printf("  %s\n", tag)
	}
	out.Println()
}

func newBackendsCmd() *cobra.Command {
	var showAll bool

	cmd := &cobra.Command{
		Use:   "backends",
		Short: "List available container/VM backends",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()
			out := getOutput()

			if showAll {
				statuses := mgr.ListAllBackends(ctx)
				return out.Render(&allBackendsResult{statuses: statuses})
			}

			backends := mgr.ListAvailableBackends(ctx)
			return out.Render(&backendsResult{backends: backends})
		},
	}

	cmd.Flags().BoolVar(&showAll, "all", false, "Show all registered backends including unavailable ones")

	return cmd
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
		out.Println("  • " + styles.Bold.Render("Apple Container (macOS 26+):") + " " + tui.URL("https://github.com/apple/container/releases"))
		out.Println("  • " + styles.Bold.Render("Docker:") + " " + tui.URL("https://docs.docker.com/get-docker/"))
		out.Println("  • " + styles.Bold.Render("Colima (macOS):") + " " + tui.Code("brew install colima"))
		out.Println("  • " + styles.Bold.Render("OrbStack (macOS):") + " " + tui.Code("brew install orbstack"))
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

// allBackendsResult implements Renderable for backends --all command
type allBackendsResult struct {
	statuses []studio.BackendStatus
}

func (r *allBackendsResult) RenderJSON() any {
	type backendInfo struct {
		Name      string `json:"name"`
		Mode      string `json:"mode"`
		Available bool   `json:"available"`
		Installed bool   `json:"installed"`
	}
	var result []backendInfo
	for _, s := range r.statuses {
		result = append(result, backendInfo{
			Name:      s.Backend.Name(),
			Mode:      string(s.Backend.Mode()),
			Available: s.Available,
			Installed: s.Installed,
		})
	}
	return tui.NewListResult(result)
}

func (r *allBackendsResult) RenderTUI(out *tui.Output) {
	styles := tui.DefaultStyles()

	out.Println()
	out.Println(styles.Title.Render("All Backends"))
	out.Println()

	var rows [][]string
	for _, s := range r.statuses {
		status := styles.Error.Render("○ not installed")
		if s.Available {
			status = styles.Success.Render("● available")
		} else if s.Installed {
			status = styles.Warning.Render("◐ not running")
		}
		rows = append(rows, []string{
			styles.Bold.Render(s.Backend.Name()),
			string(s.Backend.Mode()),
			status,
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
