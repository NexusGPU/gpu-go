package version

import (
	"fmt"
	"runtime"

	"github.com/NexusGPU/gpu-go/cmd/ggo/cmdutil"
	"github.com/NexusGPU/gpu-go/internal/tui"
	"github.com/spf13/cobra"
)

var (
	// These variables are set via build flags (ldflags)
	Version      = "dev"
	Commit       = "unknown"
	BuildDate    = "unknown"
	GoVersion    = runtime.Version()
	outputFormat string
)

// NewVersionCmd creates the version command
func NewVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Display version information",
		Long:  `Display version and build metadata for ggo CLI.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmdutil.NewOutput(outputFormat)
			return out.Render(&versionResult{})
		},
	}
	cmdutil.AddOutputFlag(cmd, &outputFormat)
	return cmd
}

// versionResult implements Renderable for version command
type versionResult struct{}

func (r *versionResult) RenderJSON() any {
	return map[string]string{
		"version":    Version,
		"commit":     Commit,
		"build_date": BuildDate,
		"go_version": GoVersion,
		"platform":   fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}

func (r *versionResult) RenderTUI(out *tui.Output) {
	fmt.Printf("ggo version %s\n", Version)
	fmt.Printf("Commit: %s\n", Commit)
	fmt.Printf("Build Date: %s\n", BuildDate)
	fmt.Printf("Go Version: %s\n", GoVersion)
	fmt.Printf("Platform: %s/%s\n", runtime.GOOS, runtime.GOARCH)
}
