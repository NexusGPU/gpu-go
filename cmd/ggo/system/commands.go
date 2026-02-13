package system

import (
	"os"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

// NewUpdateCmd creates the update command.
func NewUpdateCmd() *cobra.Command {
	return newScriptCmd(
		scriptActionUpdate,
		"update",
		"Update ggo to the latest version",
		"Downloads and runs the platform install script from the CDN.",
		"ggo update",
	)
}

// NewUninstallCmd creates the uninstall command.
func NewUninstallCmd() *cobra.Command {
	return newScriptCmd(
		scriptActionUninstall,
		"uninstall",
		"Uninstall ggo from this machine",
		"Downloads and runs the platform uninstall script from the CDN.",
		"ggo uninstall",
	)
}

func newScriptCmd(action scriptAction, use, short, long, example string) *cobra.Command {
	cmd := &cobra.Command{
		Use:     use,
		Short:   short,
		Long:    long,
		Example: example,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			command, cmdArgs, err := buildScriptCommand(action, runtime.GOOS)
			if err != nil {
				return err
			}
			execCmd := exec.Command(command, cmdArgs...)
			execCmd.Stdout = os.Stdout
			execCmd.Stderr = os.Stderr
			execCmd.Stdin = os.Stdin
			return execCmd.Run()
		},
	}

	return cmd
}
