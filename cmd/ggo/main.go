package main

import (
	"os"

	"github.com/NexusGPU/gpu-go/cmd/ggo/agent"
	"github.com/NexusGPU/gpu-go/cmd/ggo/auth"
	"github.com/NexusGPU/gpu-go/cmd/ggo/deps"
	"github.com/NexusGPU/gpu-go/cmd/ggo/launch"
	"github.com/NexusGPU/gpu-go/cmd/ggo/libs"
	"github.com/NexusGPU/gpu-go/cmd/ggo/share"
	"github.com/NexusGPU/gpu-go/cmd/ggo/studio"
	"github.com/NexusGPU/gpu-go/cmd/ggo/use"
	"github.com/NexusGPU/gpu-go/cmd/ggo/version"
	"github.com/NexusGPU/gpu-go/cmd/ggo/worker"
	"github.com/spf13/cobra"
)

var (
	verbose bool
	rootCmd = &cobra.Command{
		Use:   "ggo",
		Short: "GPU Go - Remote GPU environment management CLI",
		Long: `GPU Go (ggo) is a command-line tool for managing remote GPU environments.

It provides commands to:
  - Run an agent on GPU servers to sync with the cloud platform
  - Set up temporary or long-term remote GPU environments
  - Manage workers on GPU servers
  - Share GPU workers with others via share links`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// klog verbosity is controlled by -v flag, no need to configure here
		},
	}
)

func init() {
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "Enable verbose output")

	// Add subcommands
	rootCmd.AddCommand(agent.NewAgentCmd())
	rootCmd.AddCommand(worker.NewWorkerCmd())
	rootCmd.AddCommand(share.NewShareCmd())
	rootCmd.AddCommand(use.NewUseCmd())
	rootCmd.AddCommand(use.NewCleanCmd())
	rootCmd.AddCommand(deps.NewDepsCmd())
	rootCmd.AddCommand(studio.NewStudioCmd())
	rootCmd.AddCommand(libs.NewLibsCmd())

	// Auth commands (login/logout at root level for convenience)
	rootCmd.AddCommand(auth.NewLoginCmd())
	rootCmd.AddCommand(auth.NewLogoutCmd())
	rootCmd.AddCommand(auth.NewAuthCmd())

	// Version command
	rootCmd.AddCommand(version.NewVersionCmd())

	// Launch command (Windows/Linux - returns nil on macOS)
	if launchCmd := launch.NewLaunchCmd(); launchCmd != nil {
		rootCmd.AddCommand(launchCmd)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
