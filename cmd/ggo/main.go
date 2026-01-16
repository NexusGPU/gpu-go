package main

import (
	"os"

	"github.com/NexusGPU/gpu-go/cmd/ggo/agent"
	"github.com/NexusGPU/gpu-go/cmd/ggo/deps"
	"github.com/NexusGPU/gpu-go/cmd/ggo/share"
	"github.com/NexusGPU/gpu-go/cmd/ggo/studio"
	"github.com/NexusGPU/gpu-go/cmd/ggo/use"
	"github.com/NexusGPU/gpu-go/cmd/ggo/worker"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
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
			// Configure logging
			zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
			if verbose {
				zerolog.SetGlobalLevel(zerolog.DebugLevel)
			} else {
				zerolog.SetGlobalLevel(zerolog.InfoLevel)
			}
			log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
		},
	}
)

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	// Add subcommands
	rootCmd.AddCommand(agent.NewAgentCmd())
	rootCmd.AddCommand(worker.NewWorkerCmd())
	rootCmd.AddCommand(share.NewShareCmd())
	rootCmd.AddCommand(use.NewUseCmd())
	rootCmd.AddCommand(use.NewCleanCmd())
	rootCmd.AddCommand(deps.NewDepsCmd())
	rootCmd.AddCommand(studio.NewStudioCmd())
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
