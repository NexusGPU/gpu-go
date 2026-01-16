package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_SaveAndLoadConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	mgr := NewManager(configDir, stateDir)

	cfg := &Config{
		ConfigVersion: 3,
		AgentID:       "agent_xxxxxxxxxxxx",
		AgentSecret:   "gpugo_xxxxxxxxxxxx",
		ServerURL:     "https://api.gpu.tf",
		License: api.License{
			Plain:     "plyF5Usp2FKiVhWYBlxR0xQ8jkbsMtZw|pro|1768379729916",
			Encrypted: "base64_ed25519_signature_xxxx",
		},
	}

	err := mgr.SaveConfig(cfg)
	require.NoError(t, err)

	loaded, err := mgr.LoadConfig()
	require.NoError(t, err)
	require.NotNil(t, loaded)

	assert.Equal(t, cfg.ConfigVersion, loaded.ConfigVersion)
	assert.Equal(t, cfg.AgentID, loaded.AgentID)
	assert.Equal(t, cfg.AgentSecret, loaded.AgentSecret)
	assert.Equal(t, cfg.ServerURL, loaded.ServerURL)
	assert.Equal(t, cfg.License.Plain, loaded.License.Plain)
}

func TestManager_LoadConfigNotExists(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, tmpDir)

	cfg, err := mgr.LoadConfig()
	require.NoError(t, err)
	assert.Nil(t, cfg)
}

func TestManager_SaveAndLoadGPUs(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, tmpDir)

	gpus := []GPUConfig{
		{
			GPUID:  "GPU-0",
			Vendor: "nvidia",
			Model:  "RTX 4090",
			VRAMMb: 24576,
		},
		{
			GPUID:  "GPU-1",
			Vendor: "nvidia",
			Model:  "RTX 4090",
			VRAMMb: 24576,
		},
	}

	err := mgr.SaveGPUs(gpus)
	require.NoError(t, err)

	loaded, err := mgr.LoadGPUs()
	require.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Equal(t, "GPU-0", loaded[0].GPUID)
	assert.Equal(t, "nvidia", loaded[0].Vendor)
	assert.Equal(t, int64(24576), loaded[0].VRAMMb)
}

func TestManager_SaveAndLoadWorkers(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, tmpDir)

	workers := []WorkerConfig{
		{
			WorkerID:   "worker_yyyy",
			GPUIDs:     []string{"GPU-0"},
			ListenPort: 9001,
			Enabled:    true,
			Status:     "running",
			PID:        12345,
		},
		{
			WorkerID:   "worker_zzzz",
			GPUIDs:     []string{"GPU-1"},
			ListenPort: 9002,
			Enabled:    false,
			Status:     "stopped",
		},
	}

	err := mgr.SaveWorkers(workers)
	require.NoError(t, err)

	loaded, err := mgr.LoadWorkers()
	require.NoError(t, err)
	assert.Len(t, loaded, 2)
	assert.Equal(t, "worker_yyyy", loaded[0].WorkerID)
	assert.Equal(t, 12345, loaded[0].PID)
	assert.True(t, loaded[0].Enabled)
	assert.False(t, loaded[1].Enabled)
}

func TestManager_UpdateConfigVersion(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, tmpDir)

	// First save initial config
	cfg := &Config{
		ConfigVersion: 1,
		AgentID:       "agent_xxxxxxxxxxxx",
		AgentSecret:   "gpugo_xxxxxxxxxxxx",
		ServerURL:     "https://api.gpu.tf",
	}
	err := mgr.SaveConfig(cfg)
	require.NoError(t, err)

	// Update version
	newLicense := api.License{
		Plain:     "updated|pro|1768379729916",
		Encrypted: "updated_signature",
	}
	err = mgr.UpdateConfigVersion(5, newLicense)
	require.NoError(t, err)

	// Verify update
	loaded, err := mgr.LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, 5, loaded.ConfigVersion)
	assert.Equal(t, "updated|pro|1768379729916", loaded.License.Plain)
}

func TestManager_GetConfigVersion(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, tmpDir)

	// No config exists
	version, err := mgr.GetConfigVersion()
	require.NoError(t, err)
	assert.Equal(t, 0, version)

	// Save config
	cfg := &Config{
		ConfigVersion: 7,
		AgentID:       "agent_xxxxxxxxxxxx",
	}
	err = mgr.SaveConfig(cfg)
	require.NoError(t, err)

	// Get version
	version, err = mgr.GetConfigVersion()
	require.NoError(t, err)
	assert.Equal(t, 7, version)
}

func TestManager_ConfigExists(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, tmpDir)

	// No config exists
	assert.False(t, mgr.ConfigExists())

	// Save config
	cfg := &Config{
		ConfigVersion: 1,
		AgentID:       "agent_xxxxxxxxxxxx",
	}
	err := mgr.SaveConfig(cfg)
	require.NoError(t, err)

	// Config exists
	assert.True(t, mgr.ConfigExists())
}

func TestManager_EnsureDirs(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "nested", "config")
	stateDir := filepath.Join(tmpDir, "nested", "state")

	mgr := NewManager(configDir, stateDir)

	err := mgr.EnsureDirs()
	require.NoError(t, err)

	// Verify directories exist
	_, err = os.Stat(configDir)
	assert.NoError(t, err)
	_, err = os.Stat(stateDir)
	assert.NoError(t, err)
}

func TestManager_Paths(t *testing.T) {
	configDir := "/etc/gpugo"
	stateDir := "/tmp/tensor-fusion-state"

	mgr := NewManager(configDir, stateDir)

	assert.Equal(t, "/etc/gpugo/config.json", mgr.ConfigPath())
	assert.Equal(t, "/etc/gpugo/gpus.json", mgr.GPUsPath())
	assert.Equal(t, "/etc/gpugo/workers.json", mgr.WorkersPath())
	assert.Equal(t, stateDir, mgr.StateDir())
}

func TestManager_DefaultPaths(t *testing.T) {
	mgr := NewManager("", "")

	// Should use platform-specific defaults
	assert.Equal(t, DefaultPaths.ConfigDir(), filepath.Dir(mgr.ConfigPath()))
	assert.Equal(t, DefaultPaths.StateDir(), mgr.StateDir())
}

func TestManager_GPUWithUsedByWorker(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, tmpDir)

	workerID := "worker_yyyy"
	gpus := []GPUConfig{
		{
			GPUID:        "GPU-0",
			Vendor:       "nvidia",
			Model:        "RTX 4090",
			VRAMMb:       24576,
			UsedByWorker: &workerID,
		},
		{
			GPUID:        "GPU-1",
			Vendor:       "nvidia",
			Model:        "RTX 4090",
			VRAMMb:       24576,
			UsedByWorker: nil,
		},
	}

	err := mgr.SaveGPUs(gpus)
	require.NoError(t, err)

	loaded, err := mgr.LoadGPUs()
	require.NoError(t, err)
	assert.NotNil(t, loaded[0].UsedByWorker)
	assert.Equal(t, "worker_yyyy", *loaded[0].UsedByWorker)
	assert.Nil(t, loaded[1].UsedByWorker)
}

func TestManager_WorkerWithConnections(t *testing.T) {
	tmpDir := t.TempDir()
	mgr := NewManager(tmpDir, tmpDir)

	workers := []WorkerConfig{
		{
			WorkerID:   "worker_yyyy",
			GPUIDs:     []string{"GPU-0"},
			ListenPort: 9001,
			Enabled:    true,
			Status:     "running",
			Connections: []api.ConnectionInfo{
				{
					ClientIP: "192.168.1.100",
				},
			},
		},
	}

	err := mgr.SaveWorkers(workers)
	require.NoError(t, err)

	loaded, err := mgr.LoadWorkers()
	require.NoError(t, err)
	assert.Len(t, loaded[0].Connections, 1)
	assert.Equal(t, "192.168.1.100", loaded[0].Connections[0].ClientIP)
}
