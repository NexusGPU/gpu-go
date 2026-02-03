//go:build !windows

package launch

import (
	"github.com/spf13/cobra"
)

// NewLaunchCmd returns nil on non-Windows platforms
// The launch command is only available on Windows because:
// - Linux/macOS use LD_PRELOAD which works reliably via 'ggo use'
// - Windows requires SetDllDirectory to override System32 DLLs
func NewLaunchCmd() *cobra.Command {
	return nil
}
