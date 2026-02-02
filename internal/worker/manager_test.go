package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_SaveAndLoadState(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, "dummy-binary")

	// Create some dummy state
	mgr.workers["worker-1"] = &WorkerState{
		Config: WorkerConfig{
			WorkerID: "worker-1",
			GPUIDs:   []string{"gpu-0"},
			Enabled:  true,
		},
		Status: WorkerStatusRunning,
	}
	mgr.workers["worker-2"] = &WorkerState{
		Config: WorkerConfig{
			WorkerID: "worker-2",
			GPUIDs:   []string{"gpu-1"},
			Enabled:  false,
		},
		Status: WorkerStatusTerminated,
	}

	err := mgr.SaveStateFile()
	require.NoError(t, err)

	// Check file content
	content, err := os.ReadFile(filepath.Join(tmpDir, "workers.json"))
	require.NoError(t, err)

	var tfWorkers []TensorFusionWorkerInfo
	err = json.Unmarshal(content, &tfWorkers)
	require.NoError(t, err)
	assert.Len(t, tfWorkers, 2)

	// Load state
	loadedConfigs, err := mgr.LoadStateFile()
	require.NoError(t, err)
	assert.Len(t, loadedConfigs, 2)

	// Verify loaded configs
	configMap := make(map[string]WorkerConfig)
	for _, c := range loadedConfigs {
		configMap[c.WorkerID] = c
	}

	assert.True(t, configMap["worker-1"].Enabled)
	assert.False(t, configMap["worker-2"].Enabled) // Terminated maps to Enabled=false usually?
	// LoadStateFile logic: Enabled: tw.Status == "Running"
	assert.True(t, configMap["worker-1"].Enabled)
	assert.False(t, configMap["worker-2"].Enabled)
}

func TestManager_Reconcile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping process management tests on Windows")
	}

	tmpDir := t.TempDir()
	// Use 'sleep' as dummy worker
	mgr := NewManager(tmpDir, "sleep")

	desired := []WorkerConfig{
		{
			WorkerID:         "worker-1",
			GPUIDs:           []string{"gpu-0"},
			ListenPort:       12345,
			Enabled:          true,
			WorkerBinaryPath: "sleep",
		},
	}

	// Override buildWorkerArgs to return valid args for sleep
	// But buildWorkerArgs is private and hardcoded.
	// We can't easily override it.
	// However, buildWorkerArgs returns ["-p", "12345"] for TCP mode.
	// `sleep -p 12345` is invalid.
	// `sleep` expects a duration.

	// We need a mock binary that accepts -p flag.
	// We can use a small go program or shell script.
	// `sh -c 'sleep 1'` ignores extra args? No.

	// Let's create a dummy script that ignores args and sleeps.
	dummyScript := filepath.Join(tmpDir, "dummy_worker.sh")
	scriptContent := "#!/bin/sh\nwhile true; do sleep 1; done\n"
	err := os.WriteFile(dummyScript, []byte(scriptContent), 0755)
	require.NoError(t, err)

	mgr.workerBinary = dummyScript

	// Reconcile start
	err = mgr.Reconcile(desired)
	require.NoError(t, err)

	// Verify started
	state, err := mgr.GetStatus("worker-1")
	require.NoError(t, err)
	assert.Equal(t, WorkerStatusRunning, state.Status)
	assert.Greater(t, state.PID, 0)

	// Reconcile update (disable)
	desired[0].Enabled = false
	err = mgr.Reconcile(desired)
	require.NoError(t, err)

	// Verify stopped
	// Stop is async/graceful, might need to wait
	time.Sleep(100 * time.Millisecond)

	state, err = mgr.GetStatus("worker-1")
	require.NoError(t, err)
	assert.Equal(t, WorkerStatusStopped, state.Status)

	// Reconcile remove
	err = mgr.Reconcile([]WorkerConfig{})
	require.NoError(t, err)

	_, err = mgr.GetStatus("worker-1")
	assert.Error(t, err) // Should be deleted
}

func TestManager_StartStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping process management tests on Windows")
	}

	tmpDir := t.TempDir()

	// Create dummy script
	dummyScript := filepath.Join(tmpDir, "dummy_worker.sh")
	scriptContent := "#!/bin/sh\nwhile true; do sleep 1; done\n"
	err := os.WriteFile(dummyScript, []byte(scriptContent), 0755)
	require.NoError(t, err)

	mgr := NewManager(tmpDir, dummyScript)

	config := WorkerConfig{
		WorkerID:   "worker-1",
		ListenPort: 12345,
		Mode:       WorkerModeTCP,
	}

	// Start
	err = mgr.Start(config)
	require.NoError(t, err)

	state, err := mgr.GetStatus("worker-1")
	require.NoError(t, err)
	assert.Equal(t, WorkerStatusRunning, state.Status)

	// Stop
	err = mgr.Stop("worker-1")
	require.NoError(t, err)

	state, err = mgr.GetStatus("worker-1")
	require.NoError(t, err)
	assert.Equal(t, WorkerStatusStopped, state.Status)
}
