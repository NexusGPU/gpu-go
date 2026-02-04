//go:build darwin

package launch

import (
	"github.com/spf13/cobra"
)

// NewLaunchCmd returns nil on macOS
// macOS uses LD_PRELOAD which works reliably via 'ggo use'
// For Windows and Linux, dedicated implementations are provided.
func NewLaunchCmd() *cobra.Command {
	return nil
}
