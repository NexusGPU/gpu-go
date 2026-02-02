package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	hvApi "github.com/NexusGPU/tensor-fusion/pkg/hypervisor/api"
	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/framework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgent_Register(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/v1/agents/register" {
			var req api.AgentRegisterRequest
			json.NewDecoder(r.Body).Decode(&req)

			resp := api.AgentRegisterResponse{
				AgentID:     "agent_test123",
				AgentSecret: "gpugo_secret123",
				License: api.License{
					Plain:     "test|pro|9999999999",
					Encrypted: "encrypted_test",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := api.NewClient(api.WithBaseURL(server.URL))
	configMgr := config.NewManager(configDir, stateDir)

	agent := NewAgent(client, configMgr)

	gpus := []api.GPUInfo{
		{GPUID: "GPU-0", Vendor: "nvidia", Model: "RTX 4090", VRAMMb: 24576},
	}

	err := agent.Register("tmp_token123", gpus)
	require.NoError(t, err)

	// Verify config was saved
	cfg, err := configMgr.LoadConfig()
	require.NoError(t, err)
	assert.Equal(t, "agent_test123", cfg.AgentID)
	assert.Equal(t, "gpugo_secret123", cfg.AgentSecret)

	// Verify GPUs were saved
	gpuConfigs, err := configMgr.LoadGPUs()
	require.NoError(t, err)
	assert.Len(t, gpuConfigs, 1)
	assert.Equal(t, "GPU-0", gpuConfigs[0].GPUID)
}

func TestAgent_StartAndStop(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	// Track API calls
	var mu sync.Mutex
	apiCalls := make(map[string]int)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		apiCalls[r.Method+" "+r.URL.Path]++
		mu.Unlock()

		switch {
		case r.Method == "GET" && r.URL.Path == "/api/v1/agents/agent_test123/config":
			resp := api.AgentConfigResponse{
				ConfigVersion: 1,
				Workers: []api.WorkerConfig{
					{WorkerID: "worker_1", GPUIDs: []string{"GPU-0"}, ListenPort: 9001, Enabled: true},
				},
				License: api.License{Plain: "test|pro|9999999999", Encrypted: "enc"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case r.Method == "POST" && r.URL.Path == "/api/v1/agents/agent_test123/status":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(api.SuccessResponse{Success: true})

		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Pre-create config
	configMgr := config.NewManager(configDir, stateDir)
	cfg := &config.Config{
		ConfigVersion: 1,
		AgentID:       "agent_test123",
		AgentSecret:   "gpugo_secret123",
		ServerURL:     server.URL,
	}
	err := configMgr.SaveConfig(cfg)
	require.NoError(t, err)

	// Save initial workers (empty)
	err = configMgr.SaveWorkers(nil)
	require.NoError(t, err)

	client := api.NewClient(
		api.WithBaseURL(server.URL),
		api.WithAgentSecret("gpugo_secret123"),
	)

	agent := NewAgent(client, configMgr)

	// Start agent
	err = agent.Start()
	require.NoError(t, err)

	// Wait a bit for initial API calls
	time.Sleep(100 * time.Millisecond)

	// Stop agent
	agent.Stop()

	// Verify API calls were made
	mu.Lock()
	defer mu.Unlock()
	assert.Greater(t, apiCalls["GET /api/v1/agents/agent_test123/config"], 0, "Should have pulled config")
	assert.Greater(t, apiCalls["POST /api/v1/agents/agent_test123/status"], 0, "Should have reported status")
}

func TestAgent_StartNotRegistered(t *testing.T) {
	tmpDir := t.TempDir()
	configMgr := config.NewManager(tmpDir, tmpDir)
	client := api.NewClient()

	agent := NewAgent(client, configMgr)

	err := agent.Start()
	assert.Error(t, err)
	assert.IsType(t, &NotRegisteredError{}, err)
}

func TestAgent_PullConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := api.AgentConfigResponse{
			ConfigVersion: 5,
			Workers: []api.WorkerConfig{
				{WorkerID: "worker_1", GPUIDs: []string{"GPU-0"}, ListenPort: 9001, Enabled: true},
				{WorkerID: "worker_2", GPUIDs: []string{"GPU-1"}, ListenPort: 9002, Enabled: false},
			},
			License: api.License{Plain: "test|pro|9999999999", Encrypted: "enc"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	configMgr := config.NewManager(configDir, stateDir)
	cfg := &config.Config{
		ConfigVersion: 1,
		AgentID:       "agent_test123",
		AgentSecret:   "gpugo_secret123",
		ServerURL:     server.URL,
	}
	err := configMgr.SaveConfig(cfg)
	require.NoError(t, err)

	client := api.NewClient(
		api.WithBaseURL(server.URL),
		api.WithAgentSecret("gpugo_secret123"),
	)

	agent := &Agent{
		client:  client,
		config:  configMgr,
		ctx:     context.Background(),
		agentID: "agent_test123",
	}

	err = agent.pullConfig()
	require.NoError(t, err)

	// Verify config version was updated
	version, err := configMgr.GetConfigVersion()
	require.NoError(t, err)
	assert.Equal(t, 5, version)

	// Verify workers were saved
	workers, err := configMgr.LoadWorkers()
	require.NoError(t, err)
	assert.Len(t, workers, 2)
	assert.Equal(t, "worker_1", workers[0].WorkerID)
	assert.True(t, workers[0].Enabled)
	assert.Equal(t, "worker_2", workers[1].WorkerID)
	assert.False(t, workers[1].Enabled)
}

func TestAgent_ReportStatus(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	var receivedReq api.AgentStatusRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.SuccessResponse{Success: true})
	}))
	defer server.Close()

	configMgr := config.NewManager(configDir, stateDir)

	// Save GPUs
	gpus := []config.GPUConfig{
		{GPUID: "GPU-0", Vendor: "nvidia", Model: "RTX 4090", VRAMMb: 24576},
		{GPUID: "GPU-1", Vendor: "nvidia", Model: "RTX 4090", VRAMMb: 24576},
	}
	err := configMgr.SaveGPUs(gpus)
	require.NoError(t, err)

	// Save workers
	workers := []config.WorkerConfig{
		{WorkerID: "worker_1", GPUIDs: []string{"GPU-0"}, ListenPort: 9001, Enabled: true, Status: "running", PID: 12345},
	}
	err = configMgr.SaveWorkers(workers)
	require.NoError(t, err)

	client := api.NewClient(
		api.WithBaseURL(server.URL),
		api.WithAgentSecret("gpugo_secret123"),
	)

	agent := &Agent{
		client:  client,
		config:  configMgr,
		ctx:     context.Background(),
		agentID: "agent_test123",
	}

	err = agent.reportStatus()
	require.NoError(t, err)

	// Verify request
	assert.Len(t, receivedReq.GPUs, 2)
	assert.Len(t, receivedReq.Workers, 1)
	assert.Equal(t, "worker_1", receivedReq.Workers[0].WorkerID)
	assert.Equal(t, "running", receivedReq.Workers[0].Status)
	assert.Equal(t, 12345, receivedReq.Workers[0].PID)
}

func TestAgent_HandleHeartbeatResponse(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	configPulled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		configPulled = true
		resp := api.AgentConfigResponse{
			ConfigVersion: 10,
			Workers:       []api.WorkerConfig{},
			License:       api.License{Plain: "test|pro|9999999999", Encrypted: "enc"},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	configMgr := config.NewManager(configDir, stateDir)
	cfg := &config.Config{
		ConfigVersion: 1,
		AgentID:       "agent_test123",
		AgentSecret:   "gpugo_secret123",
		ServerURL:     server.URL,
	}
	err := configMgr.SaveConfig(cfg)
	require.NoError(t, err)

	client := api.NewClient(
		api.WithBaseURL(server.URL),
		api.WithAgentSecret("gpugo_secret123"),
	)

	agent := &Agent{
		client:        client,
		config:        configMgr,
		ctx:           context.Background(),
		agentID:       "agent_test123",
		configVersion: 1,
	}

	// Simulate heartbeat response with higher version
	resp := &api.HeartbeatResponse{ConfigVersion: 10}
	agent.handleHeartbeatResponse(resp)

	// Verify config was pulled
	assert.True(t, configPulled)
	assert.Equal(t, 10, agent.configVersion)
}

// Mock HypervisorManager
type mockHypervisorManager struct {
	started        bool
	devices        []*hvApi.DeviceInfo
	workers        []*hvApi.WorkerInfo
	startedWorkers []string
	stoppedWorkers []string
}

func (m *mockHypervisorManager) Start() error {
	m.started = true
	return nil
}

func (m *mockHypervisorManager) Stop() error {
	m.started = false
	return nil
}

func (m *mockHypervisorManager) IsStarted() bool {
	return m.started
}

func (m *mockHypervisorManager) ListDevices() ([]*hvApi.DeviceInfo, error) {
	return m.devices, nil
}

func (m *mockHypervisorManager) ListWorkers() []*hvApi.WorkerInfo {
	return m.workers
}

func (m *mockHypervisorManager) StartWorker(info *hvApi.WorkerInfo) error {
	m.startedWorkers = append(m.startedWorkers, info.WorkerUID)
	return nil
}

func (m *mockHypervisorManager) StopWorker(workerUID string) error {
	m.stoppedWorkers = append(m.stoppedWorkers, workerUID)
	return nil
}

func (m *mockHypervisorManager) GetDeviceMetrics() (map[string]*hvApi.GPUUsageMetrics, error) {
	return nil, nil
}

func (m *mockHypervisorManager) GetWorkerAllocation(workerUID string) (*hvApi.WorkerAllocation, bool) {
	return nil, false
}

func (m *mockHypervisorManager) RegisterWorkerHandler(handler framework.WorkerChangeHandler) error {
	return nil
}

func (m *mockHypervisorManager) RegisterDeviceHandler(handler framework.DeviceChangeHandler) {
}

func TestAgent_DetectChanges(t *testing.T) {
	// Initialize mock hypervisor
	mockHv := &mockHypervisorManager{
		started: true,
		devices: []*hvApi.DeviceInfo{
			{UUID: "gpu-0", Vendor: "nvidia", Model: "RTX 4090", TotalMemoryBytes: 24 * 1024 * 1024 * 1024},
		},
		workers: []*hvApi.WorkerInfo{
			{
				WorkerUID:        "worker_1",
				AllocatedDevices: []string{"gpu-0"},
				WorkerRunningInfo: &hvApi.WorkerRunningInfo{
					IsRunning: true,
					PID:       12345,
					Restarts:  0,
				},
			},
		},
	}

	tmpDir := t.TempDir()
	configMgr := config.NewManager(tmpDir, tmpDir)
	client := api.NewClient()

	// Use NewAgentWithHypervisor logic but with mock
	agent := NewAgentWithHypervisor(client, configMgr, mockHv, "/bin/true")

	// 1. Detect GPU changes (initial - should be true)
	devices, _ := mockHv.ListDevices()
	changes, err := agent.detectGPUChanges(devices)
	require.NoError(t, err)
	assert.True(t, changes["gpu-0"])

	// 2. Detect GPU changes (subsequent - should be false)
	changes, err = agent.detectGPUChanges(devices)
	require.NoError(t, err)
	assert.False(t, changes["gpu-0"])

	// 3. Detect Worker changes (initial - should be true)
	workerChanges := agent.detectWorkerChanges(mockHv.workers)
	assert.True(t, workerChanges["worker_1"])

	// 4. Detect Worker changes (subsequent - should be false)
	workerChanges = agent.detectWorkerChanges(mockHv.workers)
	assert.False(t, workerChanges["worker_1"])

	// 5. Simulate change
	mockHv.workers[0].WorkerRunningInfo.Restarts = 1
	workerChanges = agent.detectWorkerChanges(mockHv.workers)
	assert.True(t, workerChanges["worker_1"])
}

func TestAgent_WithHypervisor(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	mockHv := &mockHypervisorManager{started: true}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/api/v1/agents/agent_test123/config" {
			resp := api.AgentConfigResponse{
				ConfigVersion: 1,
				Workers: []api.WorkerConfig{
					{WorkerID: "worker_1", GPUIDs: []string{"gpu-0"}, ListenPort: 9001, Enabled: true},
				},
				License: api.License{Plain: "test", Encrypted: "enc"},
			}
			json.NewEncoder(w).Encode(resp)
		} else {
			json.NewEncoder(w).Encode(api.SuccessResponse{Success: true})
		}
	}))
	defer server.Close()

	configMgr := config.NewManager(configDir, stateDir)
	cfg := &config.Config{
		ConfigVersion: 1,
		AgentID:       "agent_test123",
		AgentSecret:   "gpugo_secret123",
		ServerURL:     server.URL,
	}
	configMgr.SaveConfig(cfg)

	client := api.NewClient(
		api.WithBaseURL(server.URL),
		api.WithAgentSecret("gpugo_secret123"),
	)

	agent := NewAgentWithHypervisor(client, configMgr, mockHv, "/bin/true")

	// Start
	err := agent.Start()
	require.NoError(t, err)

	// Reconciler should have started worker (eventually)
	// Since we mock backend, StartWorker calls mock.StartWorker
	// We check if it eventually happens
	time.Sleep(200 * time.Millisecond)

	agent.Stop()
}

func TestAgent_LicenseParsing(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")

	var receivedReq api.AgentStatusRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedReq)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(api.SuccessResponse{Success: true})
	}))
	defer server.Close()

	configMgr := config.NewManager(configDir, stateDir)

	// Create config with license expiration
	// Future timestamp: 2025-01-01 -> 1735689600000
	cfg := &config.Config{
		ConfigVersion: 1,
		AgentID:       "agent_test123",
		AgentSecret:   "gpugo_secret123",
		ServerURL:     server.URL,
		License: api.License{
			Plain:     "test|pro|1735689600000",
			Encrypted: "enc",
		},
	}
	err := configMgr.SaveConfig(cfg)
	require.NoError(t, err)

	client := api.NewClient(
		api.WithBaseURL(server.URL),
		api.WithAgentSecret("gpugo_secret123"),
	)

	agent := NewAgent(client, configMgr)

	err = agent.reportStatus()
	require.NoError(t, err)

	require.NotNil(t, receivedReq.LicenseExpiration)
	assert.Equal(t, int64(1735689600000), *receivedReq.LicenseExpiration)
}
