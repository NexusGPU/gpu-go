package studio

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/NexusGPU/gpu-go/internal/studio"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	mode          string
	image         string
	gpuURL        string
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
  ggo studio create my-studio --gpu-url "https://worker.example.com:9001"

  # Create with specific mode
  ggo studio create my-studio --mode wsl --gpu-url "https://..."

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
	}

	// Add subcommands
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
	
	// Register Docker backend
	dockerBackend := studio.NewDockerBackend()
	if dockerHost != "" {
		dockerBackend = studio.NewDockerBackendWithHost(dockerHost)
	}
	mgr.RegisterBackend(dockerBackend)
	
	// Register Colima backend with custom profile if specified
	colimaBackend := studio.NewColimaBackend()
	if colimaProfile != "" {
		colimaBackend = studio.NewColimaBackendWithProfile(colimaProfile)
	}
	if dockerHost != "" {
		colimaBackend.SetDockerHost(dockerHost)
	}
	mgr.RegisterBackend(colimaBackend)
	
	// Register WSL backend with custom distro if specified
	wslBackend := studio.NewWSLBackend()
	if wslDistro != "" {
		wslBackend = studio.NewWSLBackendWithDistro(wslDistro)
	}
	mgr.RegisterBackend(wslBackend)
	
	// Register Apple Container backend
	mgr.RegisterBackend(studio.NewAppleContainerBackend())
	
	return mgr
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
  # Create with auto-detected platform
  ggo studio create my-env --gpu-url "https://worker:9001"

  # Create with specific image
  ggo studio create my-env --image tensorfusion/studio-torch:latest

  # Create with WSL on Windows (specific distro)
  ggo studio create my-env --mode wsl --wsl-distro Ubuntu-22.04 --gpu-url "https://..."

  # Create with Colima on macOS (specific profile)
  ggo studio create my-env --mode colima --colima-profile myprofile --gpu-url "https://..."

  # Create with custom Docker socket
  ggo studio create my-env --docker-host unix://$HOME/.colima/custom/docker.sock

  # Create with custom ports and volumes
  ggo studio create my-env --gpu-url "..." -p 8888:8888 -v ~/projects:/workspace`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := context.Background()
			mgr := getManager()

			// Parse mode
			studioMode := studio.ModeAuto
			if mode != "" {
				studioMode = studio.Mode(mode)
			}

			// Parse ports
			var portMappings []studio.PortMapping
			for _, p := range ports {
				parts := strings.Split(p, ":")
				if len(parts) != 2 {
					return fmt.Errorf("invalid port format: %s (expected host:container)", p)
				}
				var hostPort, containerPort int
				if _, err := fmt.Sscanf(parts[0], "%d", &hostPort); err != nil {
					return fmt.Errorf("invalid host port: %s", parts[0])
				}
				if _, err := fmt.Sscanf(parts[1], "%d", &containerPort); err != nil {
					return fmt.Errorf("invalid container port: %s", parts[1])
				}
				portMappings = append(portMappings, studio.PortMapping{
					HostPort:      hostPort,
					ContainerPort: containerPort,
				})
			}

			// Parse volumes
			var volumeMounts []studio.VolumeMount
			for _, v := range volumes {
				parts := strings.Split(v, ":")
				if len(parts) < 2 {
					return fmt.Errorf("invalid volume format: %s (expected host:container[:ro])", v)
				}
				mount := studio.VolumeMount{
					HostPath:      parts[0],
					ContainerPath: parts[1],
				}
				if len(parts) > 2 && parts[2] == "ro" {
					mount.ReadOnly = true
				}
				volumeMounts = append(volumeMounts, mount)
			}

			// Parse env vars
			envMap := make(map[string]string)
			for _, e := range envVars {
				parts := strings.SplitN(e, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid env var format: %s (expected KEY=VALUE)", e)
				}
				envMap[parts[0]] = parts[1]
			}

			opts := &studio.CreateOptions{
				Name:         name,
				Mode:         studioMode,
				Image:        image,
				GPUWorkerURL: gpuURL,
				SSHPublicKey: sshKey,
				Ports:        portMappings,
				Volumes:      volumeMounts,
				Envs:         envMap,
				Resources: studio.ResourceSpec{
					CPUs:   cpus,
					Memory: memory,
				},
			}

			fmt.Printf("Creating studio environment '%s'...\n", name)
			
			env, err := mgr.Create(ctx, opts)
			if err != nil {
				log.Error().Err(err).Msg("Failed to create environment")
				return err
			}

			fmt.Println()
			fmt.Println("Studio environment created successfully!")
			fmt.Println()
			fmt.Printf("  Name:     %s\n", env.Name)
			fmt.Printf("  ID:       %s\n", env.ID)
			fmt.Printf("  Mode:     %s\n", env.Mode)
			fmt.Printf("  Image:    %s\n", env.Image)
			fmt.Printf("  Status:   %s\n", env.Status)
			fmt.Println()

			if env.SSHPort > 0 && !noSSH {
				// Add SSH config
				if err := mgr.AddSSHConfig(env); err != nil {
					log.Warn().Err(err).Msg("Failed to add SSH config")
				} else {
					fmt.Println("SSH Configuration:")
					fmt.Printf("  Host:     ggo-%s\n", env.Name)
					fmt.Printf("  Port:     %d\n", env.SSHPort)
					fmt.Printf("  User:     %s\n", env.SSHUser)
					fmt.Println()
					fmt.Println("Connect with:")
					fmt.Printf("  ssh ggo-%s\n", env.Name)
					fmt.Println()
					fmt.Println("Or in VS Code:")
					fmt.Printf("  1. Install 'Remote - SSH' extension\n")
					fmt.Printf("  2. Press F1 -> 'Remote-SSH: Connect to Host...'\n")
					fmt.Printf("  3. Select 'ggo-%s'\n", env.Name)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&mode, "mode", "m", "", "Container/VM mode (wsl, colima, docker, k8s, auto)")
	cmd.Flags().StringVarP(&image, "image", "i", "tensorfusion/studio-torch:latest", "Container image")
	cmd.Flags().StringVar(&gpuURL, "gpu-url", "", "Remote GPU worker URL")
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

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all studio environments",
		Aliases: []string{"ls"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()

			envs, err := mgr.List(ctx)
			if err != nil {
				log.Error().Err(err).Msg("Failed to list environments")
				return err
			}

			if len(envs) == 0 {
				fmt.Println("No studio environments found")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tID\tMODE\tSTATUS\tIMAGE\tSSH")
			for _, env := range envs {
				sshInfo := "-"
				if env.SSHPort > 0 {
					sshInfo = fmt.Sprintf("%s:%d", env.SSHHost, env.SSHPort)
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					env.Name, env.ID, env.Mode, env.Status, env.Image, sshInfo)
			}
			w.Flush()

			return nil
		},
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

			if err := mgr.Start(ctx, args[0]); err != nil {
				log.Error().Err(err).Msg("Failed to start environment")
				return err
			}

			fmt.Printf("Environment '%s' started\n", args[0])
			return nil
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

			if err := mgr.Stop(ctx, args[0]); err != nil {
				log.Error().Err(err).Msg("Failed to stop environment")
				return err
			}

			fmt.Printf("Environment '%s' stopped\n", args[0])
			return nil
		},
	}
}

func newRemoveCmd() *cobra.Command {
	var force bool
	
	cmd := &cobra.Command{
		Use:   "rm <name>",
		Short: "Remove a studio environment",
		Aliases: []string{"remove", "delete"},
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()

			name := args[0]

			if err := mgr.Remove(ctx, name); err != nil {
				log.Error().Err(err).Msg("Failed to remove environment")
				return err
			}

			// Remove SSH config
			if err := mgr.RemoveSSHConfig(name); err != nil {
				log.Warn().Err(err).Msg("Failed to remove SSH config")
			}

			fmt.Printf("Environment '%s' removed\n", name)
			return nil
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

			env, err := mgr.Get(ctx, args[0])
			if err != nil {
				log.Error().Err(err).Msg("Failed to get environment")
				return err
			}

			if env.SSHPort == 0 {
				return fmt.Errorf("SSH not configured for this environment")
			}

			// Execute SSH
			sshCmd := fmt.Sprintf("ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -p %d %s@%s",
				env.SSHPort, env.SSHUser, env.SSHHost)
			
			fmt.Printf("Connecting to %s...\n", env.Name)
			fmt.Printf("Running: %s\n\n", sshCmd)

			// Use os/exec to run SSH interactively
			return runInteractiveSSH(env.SSHHost, env.SSHPort, env.SSHUser)
		},
	}
}

func runInteractiveSSH(host string, port int, user string) error {
	// This is a placeholder - in a real implementation, we'd use golang.org/x/crypto/ssh
	// or exec.Command with proper TTY handling
	fmt.Printf("Please run: ssh -p %d %s@%s\n", port, user, host)
	return nil
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
				log.Error().Err(err).Msg("Failed to get environment")
				return err
			}

			backend, err := mgr.GetBackend(env.Mode)
			if err != nil {
				return err
			}

			logCh, err := backend.Logs(ctx, env.ID, follow)
			if err != nil {
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
			images := studio.DefaultImages()

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "IMAGE\tDESCRIPTION\tFEATURES")
			for _, img := range images {
				fmt.Fprintf(w, "%s:%s\t%s\t%s\n",
					img.Name, img.Tag, img.Description, strings.Join(img.Features, ", "))
			}
			w.Flush()

			return nil
		},
	}
}

func newBackendsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backends",
		Short: "List available container/VM backends",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			mgr := getManager()

			backends := mgr.ListAvailableBackends(ctx)

			if len(backends) == 0 {
				fmt.Println("No backends available")
				fmt.Println()
				fmt.Println("Install one of the following:")
				fmt.Println("  - Docker: https://docs.docker.com/get-docker/")
				fmt.Println("  - Colima (macOS): brew install colima")
				fmt.Println("  - WSL (Windows): https://docs.microsoft.com/en-us/windows/wsl/install")
				return nil
			}

			fmt.Println("Available backends:")
			for _, b := range backends {
				fmt.Printf("  - %s (%s)\n", b.Name(), b.Mode())
			}

			return nil
		},
	}
}
