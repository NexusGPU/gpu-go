//go:build !linux

package agent

import (
	"github.com/NexusGPU/gpu-go/internal/api"
)

// collectSystemMetrics returns nil on non-Linux platforms.
// GPU metrics are the primary value; system metrics are secondary.
func collectSystemMetrics() *api.SystemMetrics {
	return nil
}
