package agent

import (
	"testing"

	"github.com/NexusGPU/gpu-go/internal/hypervisor"
	"github.com/stretchr/testify/assert"
)

func TestParseVGPURestartWorkers(t *testing.T) {
	workers := parseVGPURestartWorkers([]string{
		" worker-1 ",
		"",
		"worker-2",
		"worker-1",
		"   ",
	})

	assert.Equal(t, []string{"worker-1", "worker-2"}, workers)
}

func TestHandleVGPURestartEvent(t *testing.T) {
	agent := &Agent{
		agentID: "agent-1",
		reconciler: hypervisor.NewReconciler(hypervisor.ReconcilerConfig{
			Manager: &mockHypervisorManager{},
		}),
	}

	queued := agent.handleVGPURestartEvent([]string{"worker-1", " worker-2 ", "worker-1"})
	assert.Equal(t, 2, queued)

	// Duplicate request should not queue again until reconcile consumes it.
	queued = agent.handleVGPURestartEvent([]string{"worker-1"})
	assert.Equal(t, 0, queued)
}
