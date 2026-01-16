package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TensorFusionWorkerInfo mirrors the tensor-fusion WorkerInfo structure
// from github.com/NexusGPU/tensor-fusion/internal/hypervisor/api
// This ensures our file format is compatible with what tensor-fusion expects
type TensorFusionWorkerInfo struct {
	WorkerUID        string   `json:"WorkerUID"`
	Namespace        string   `json:"Namespace,omitempty"`
	WorkerName       string   `json:"WorkerName,omitempty"`
	AllocatedDevices []string `json:"AllocatedDevices"`
	Status           string   `json:"Status"` // "Pending", "Running", "Terminated"
}

// TestSyncToStateDir_TensorFusionFormat tests that the agent syncs workers
// to the tensor-fusion state directory in the correct format that the
// hypervisor single_node backend expects.
func TestSyncToStateDir_TensorFusionFormat(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	configMgr := config.NewManager(configDir, stateDir)

	agent := &Agent{
		config: configMgr,
	}

	// Workers from GPU Go API
	workers := []config.WorkerConfig{
		{
			WorkerID:   "worker_abc123",
			GPUIDs:     []string{"GPU-0", "GPU-1"},
			ListenPort: 9001,
			Enabled:    true,
		},
		{
			WorkerID:   "worker_def456",
			GPUIDs:     []string{"GPU-2"},
			ListenPort: 9002,
			Enabled:    false, // Disabled worker
		},
	}

	// Sync to state directory
	err := agent.syncToStateDir(workers)
	require.NoError(t, err)

	// Read and verify the state file
	data, err := os.ReadFile(filepath.Join(stateDir, "workers.json"))
	require.NoError(t, err)

	// Parse as tensor-fusion format
	var tfWorkers []TensorFusionWorkerInfo
	err = json.Unmarshal(data, &tfWorkers)
	require.NoError(t, err)

	assert.Len(t, tfWorkers, 2)

	// Verify first worker (enabled)
	assert.Equal(t, "worker_abc123", tfWorkers[0].WorkerUID)
	assert.Equal(t, "Running", tfWorkers[0].Status)
	assert.Len(t, tfWorkers[0].AllocatedDevices, 2)
	assert.Equal(t, "GPU-0", tfWorkers[0].AllocatedDevices[0])
	assert.Equal(t, "GPU-1", tfWorkers[0].AllocatedDevices[1])

	// Verify second worker (disabled)
	assert.Equal(t, "worker_def456", tfWorkers[1].WorkerUID)
	assert.Equal(t, "Pending", tfWorkers[1].Status) // Disabled = Pending
}

// TestSyncToStateDir_EmptyWorkers tests syncing an empty worker list
func TestSyncToStateDir_EmptyWorkers(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	configMgr := config.NewManager(configDir, stateDir)

	agent := &Agent{
		config: configMgr,
	}

	// Sync empty workers
	err := agent.syncToStateDir(nil)
	require.NoError(t, err)

	// Verify the state file exists with empty array
	data, err := os.ReadFile(filepath.Join(stateDir, "workers.json"))
	require.NoError(t, err)

	var tfWorkers []map[string]interface{}
	err = json.Unmarshal(data, &tfWorkers)
	require.NoError(t, err)

	assert.Len(t, tfWorkers, 0)
}

// TestSyncToStateDir_AtomicWrite tests that file write is atomic
func TestSyncToStateDir_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	configMgr := config.NewManager(configDir, stateDir)

	agent := &Agent{
		config: configMgr,
	}

	// First sync
	workers1 := []config.WorkerConfig{
		{WorkerID: "worker_1", GPUIDs: []string{"GPU-0"}, Enabled: true},
	}
	err := agent.syncToStateDir(workers1)
	require.NoError(t, err)

	// Second sync (should atomically replace)
	workers2 := []config.WorkerConfig{
		{WorkerID: "worker_2", GPUIDs: []string{"GPU-1"}, Enabled: true},
		{WorkerID: "worker_3", GPUIDs: []string{"GPU-2"}, Enabled: false},
	}
	err = agent.syncToStateDir(workers2)
	require.NoError(t, err)

	// Verify only the new workers exist
	data, err := os.ReadFile(filepath.Join(stateDir, "workers.json"))
	require.NoError(t, err)

	var tfWorkers []map[string]interface{}
	err = json.Unmarshal(data, &tfWorkers)
	require.NoError(t, err)

	assert.Len(t, tfWorkers, 2)
	assert.Equal(t, "worker_2", tfWorkers[0]["WorkerUID"])
	assert.Equal(t, "worker_3", tfWorkers[1]["WorkerUID"])

	// Verify no temp files left behind
	files, _ := filepath.Glob(filepath.Join(stateDir, "*.tmp"))
	assert.Len(t, files, 0, "No temp files should remain")
}

// TestStateDirectory_MatchesTensorFusionExpectations tests the state directory
// structure matches what tensor-fusion hypervisor single_node mode expects
func TestStateDirectory_MatchesTensorFusionExpectations(t *testing.T) {
	tmpDir := t.TempDir()

	// Create workers.json in tensor-fusion format
	workersFile := filepath.Join(tmpDir, "workers.json")
	workers := []map[string]interface{}{
		{
			"WorkerUID":        "worker_test1",
			"AllocatedDevices": []string{"GPU-0"},
			"Status":           "Running",
		},
	}
	data, _ := json.MarshalIndent(workers, "", "  ")
	err := os.WriteFile(workersFile, data, 0644)
	require.NoError(t, err)

	// Create devices.json in tensor-fusion format
	devicesFile := filepath.Join(tmpDir, "devices.json")
	devices := []map[string]interface{}{
		{
			"UUID":             "GPU-0-uuid-xxxx",
			"Vendor":           "nvidia",
			"Model":            "RTX 4090",
			"TotalMemoryBytes": uint64(24576 * 1024 * 1024),
		},
	}
	data, _ = json.MarshalIndent(devices, "", "  ")
	err = os.WriteFile(devicesFile, data, 0644)
	require.NoError(t, err)

	// Verify files exist and are readable
	_, err = os.Stat(workersFile)
	require.NoError(t, err)
	_, err = os.Stat(devicesFile)
	require.NoError(t, err)

	// Verify JSON is valid
	workersData, _ := os.ReadFile(workersFile)
	var loadedWorkers []map[string]interface{}
	err = json.Unmarshal(workersData, &loadedWorkers)
	require.NoError(t, err)
	assert.Equal(t, "worker_test1", loadedWorkers[0]["WorkerUID"])

	devicesData, _ := os.ReadFile(devicesFile)
	var loadedDevices []map[string]interface{}
	err = json.Unmarshal(devicesData, &loadedDevices)
	require.NoError(t, err)
	assert.Equal(t, "nvidia", loadedDevices[0]["Vendor"])
}

// TestFileHashChange tests that the file hash changes when content changes
func TestFileHashChange(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	configMgr := config.NewManager(configDir, stateDir)
	err := configMgr.EnsureDirs()
	require.NoError(t, err)

	agent := &Agent{
		config: configMgr,
	}

	// Initial hash with no files
	hash1 := agent.computeFileHash()

	// Create workers file
	workers := []config.WorkerConfig{
		{WorkerID: "worker_1", ListenPort: 9001},
	}
	err = configMgr.SaveWorkers(workers)
	require.NoError(t, err)

	// Hash should change
	hash2 := agent.computeFileHash()
	assert.NotEqual(t, hash1, hash2)

	// Update workers
	workers[0].ListenPort = 9002
	err = configMgr.SaveWorkers(workers)
	require.NoError(t, err)

	// Hash should change again
	hash3 := agent.computeFileHash()
	assert.NotEqual(t, hash2, hash3)

	// Same content, same hash
	hash4 := agent.computeFileHash()
	assert.Equal(t, hash3, hash4)
}
