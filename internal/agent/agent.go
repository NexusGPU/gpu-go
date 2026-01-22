package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/NexusGPU/gpu-go/internal/log"
	"github.com/NexusGPU/gpu-go/internal/worker"
	"github.com/fsnotify/fsnotify"
)

const (
	statusReportInterval    = 60 * time.Minute
	configPollInterval      = 10 * time.Second
	workerReconcileInterval = 5 * time.Second
)

// Agent manages the GPU agent lifecycle
type Agent struct {
	client *api.Client
	config *config.Manager
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	log    *log.Logger

	agentID       string
	hostname      string
	lastFileHash  string
	configVersion int

	// File watcher
	watcher *fsnotify.Watcher

	// Worker management
	workerManager *worker.Manager
	singleNode    bool   // Enable single-node mode with local worker management
	workerBinary  string // Path to worker binary
}

// NewAgent creates a new agent
func NewAgent(client *api.Client, configMgr *config.Manager) *Agent {
	return NewAgentWithOptions(client, configMgr, false, "")
}

// NewAgentWithOptions creates a new agent with additional options
func NewAgentWithOptions(client *api.Client, configMgr *config.Manager, singleNode bool, workerBinary string) *Agent {
	ctx, cancel := context.WithCancel(context.Background())

	hostname, _ := os.Hostname()
	logger := log.Default.WithComponent("agent")

	agent := &Agent{
		client:       client,
		config:       configMgr,
		ctx:          ctx,
		cancel:       cancel,
		hostname:     hostname,
		log:          logger,
		singleNode:   singleNode,
		workerBinary: workerBinary,
	}

	// Initialize worker manager for single-node mode
	if singleNode {
		if workerBinary == "" {
			workerBinary = "tensor-fusion-worker"
		}
		agent.workerManager = worker.NewManager(configMgr.StateDir(), workerBinary)
		logger.Info().Bool("single_node", true).Msg("single-node mode enabled, worker management active")
	}

	return agent
}

// Register registers the agent with the server using a temporary token
func (a *Agent) Register(tempToken string, gpus []api.GPUInfo) error {
	// Get network IPs
	networkIPs := getNetworkIPs()

	req := &api.AgentRegisterRequest{
		Token:      tempToken,
		Hostname:   a.hostname,
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		GPUs:       gpus,
		NetworkIPs: networkIPs,
	}

	resp, err := a.client.RegisterAgent(a.ctx, tempToken, req)
	if err != nil {
		return err
	}

	// Save configuration
	cfg := &config.Config{
		ConfigVersion: 1,
		AgentID:       resp.AgentID,
		AgentSecret:   resp.AgentSecret,
		ServerURL:     a.client.GetBaseURL(),
		License:       resp.License,
	}

	if err := a.config.SaveConfig(cfg); err != nil {
		return err
	}

	// Save GPUs
	gpuConfigs := make([]config.GPUConfig, len(gpus))
	for i, gpu := range gpus {
		gpuConfigs[i] = config.GPUConfig{
			GPUID:  gpu.GPUID,
			Vendor: gpu.Vendor,
			Model:  gpu.Model,
			VRAMMb: gpu.VRAMMb,
		}
	}
	if err := a.config.SaveGPUs(gpuConfigs); err != nil {
		return err
	}

	a.agentID = resp.AgentID
	a.client.SetAgentSecret(resp.AgentSecret)
	a.log = a.log.WithAgentID(resp.AgentID)

	a.log.Info().Str("agent_id", resp.AgentID).Msg("agent registered successfully")

	return nil
}

// Start starts the agent
func (a *Agent) Start() error {
	// Load existing configuration
	cfg, err := a.config.LoadConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		return ErrNotRegistered
	}

	a.agentID = cfg.AgentID
	a.configVersion = cfg.ConfigVersion
	a.client.SetAgentSecret(cfg.AgentSecret)
	a.log = a.log.WithAgentID(cfg.AgentID)

	// Initialize file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	a.watcher = watcher

	// Watch config directory
	if err := watcher.Add(a.config.StateDir()); err != nil {
		a.log.Warn().Err(err).Str("dir", a.config.StateDir()).Msg("failed to watch state directory")
	}

	// Pull initial config
	if err := a.pullConfig(); err != nil {
		a.log.Error().Err(err).Msg("failed to pull initial config")
	}

	// Start background tasks
	taskCount := 3
	if a.singleNode {
		taskCount = 4 // Add worker reconcile loop
	}
	a.wg.Add(taskCount)
	go a.statusReportLoop()
	go a.configPollLoop()
	go a.fileWatchLoop()
	if a.singleNode {
		go a.workerReconcileLoop()
	}

	// Start WebSocket heartbeat
	if err := a.client.StartHeartbeat(a.ctx, a.agentID, a.handleHeartbeatResponse); err != nil {
		a.log.Warn().Err(err).Msg("failed to start heartbeat, will retry")
	}

	a.log.Info().Msg("agent started")

	return nil
}

// Stop stops the agent
func (a *Agent) Stop() {
	a.log.Info().Msg("stopping agent...")

	a.cancel()
	a.client.StopHeartbeat()

	if a.watcher != nil {
		_ = a.watcher.Close()
	}

	// Stop worker manager
	if a.workerManager != nil {
		a.log.Info().Msg("stopping worker manager...")
		a.workerManager.Shutdown()
	}

	a.wg.Wait()

	a.log.Info().Msg("agent stopped")
}

// statusReportLoop periodically reports status to the server
func (a *Agent) statusReportLoop() {
	defer a.wg.Done()

	// Report immediately on start
	if err := a.reportStatus(); err != nil {
		a.log.Error().Err(err).Msg("failed to report initial status")
	}

	ticker := time.NewTicker(statusReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if err := a.reportStatus(); err != nil {
				a.log.Error().Err(err).Msg("failed to report status")
			}
		}
	}
}

// configPollLoop periodically polls for config changes
func (a *Agent) configPollLoop() {
	defer a.wg.Done()

	ticker := time.NewTicker(configPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			// Config polling is handled by heartbeat response
			// This is a fallback in case heartbeat fails
		}
	}
}

// fileWatchLoop watches for file changes and reports status
func (a *Agent) fileWatchLoop() {
	defer a.wg.Done()

	for {
		select {
		case <-a.ctx.Done():
			return
		case event, ok := <-a.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				// Check if file hash changed
				newHash := a.computeFileHash()
				if newHash != a.lastFileHash {
					a.lastFileHash = newHash
					a.log.Debug().Str("file", event.Name).Msg("file changed, reporting status")
					if err := a.reportStatus(); err != nil {
						a.log.Error().Err(err).Msg("failed to report status after file change")
					}

					// Trigger worker reconciliation if in single-node mode
					if a.singleNode && a.workerManager != nil {
						a.reconcileWorkers()
					}
				}
			}
		case err, ok := <-a.watcher.Errors:
			if !ok {
				return
			}
			a.log.Error().Err(err).Msg("file watcher error")
		}
	}
}

// workerReconcileLoop periodically reconciles worker processes with desired state
func (a *Agent) workerReconcileLoop() {
	defer a.wg.Done()

	// Initial reconciliation
	a.reconcileWorkers()

	ticker := time.NewTicker(workerReconcileInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.reconcileWorkers()
		}
	}
}

// reconcileWorkers reconciles worker processes based on the workers.json file
func (a *Agent) reconcileWorkers() {
	if a.workerManager == nil {
		return
	}

	// Load workers from state file
	workersPath := filepath.Join(a.config.StateDir(), "workers.json")
	data, err := os.ReadFile(workersPath)
	if err != nil {
		if !os.IsNotExist(err) {
			a.log.Error().Err(err).Msg("failed to read workers.json")
		}
		return
	}

	var tfWorkers []worker.TensorFusionWorkerInfo
	if err := json.Unmarshal(data, &tfWorkers); err != nil {
		a.log.Error().Err(err).Msg("failed to parse workers.json")
		return
	}

	// Convert to worker configs
	workerConfigs := make([]worker.WorkerConfig, 0, len(tfWorkers))
	portBase := worker.DefaultWorkerPort
	for i, tw := range tfWorkers {
		enabled := tw.Status == "Running"
		workerConfigs = append(workerConfigs, worker.WorkerConfig{
			WorkerID:   tw.WorkerUID,
			GPUIDs:     tw.AllocatedDevices,
			ListenPort: portBase + i,
			Mode:       worker.WorkerModeTCP,
			Enabled:    enabled,
		})
	}

	// Reconcile workers
	if err := a.workerManager.Reconcile(workerConfigs); err != nil {
		a.log.Error().Err(err).Msg("failed to reconcile workers")
		return
	}

	// Update config file with actual worker status
	a.updateWorkerStatus()
}

// updateWorkerStatus updates the config workers file with actual process status
func (a *Agent) updateWorkerStatus() {
	states := a.workerManager.List()
	workers := make([]config.WorkerConfig, 0, len(states))

	for _, state := range states {
		status := "stopped"
		switch state.Status {
		case worker.WorkerStatusRunning:
			status = "running"
		case worker.WorkerStatusError:
			status = "error"
		case worker.WorkerStatusTerminated:
			status = "terminated"
		}

		workers = append(workers, config.WorkerConfig{
			WorkerID:   state.Config.WorkerID,
			GPUIDs:     state.Config.GPUIDs,
			ListenPort: state.Config.ListenPort,
			Enabled:    state.Config.Enabled,
			Status:     status,
			PID:        state.PID,
		})
	}

	if err := a.config.SaveWorkers(workers); err != nil {
		a.log.Error().Err(err).Msg("failed to save worker status")
	}
}

// handleHeartbeatResponse handles WebSocket heartbeat responses
func (a *Agent) handleHeartbeatResponse(resp *api.HeartbeatResponse) {
	if resp.ConfigVersion > a.configVersion {
		a.log.Info().
			Int("old_version", a.configVersion).
			Int("new_version", resp.ConfigVersion).
			Msg("config version changed, pulling new config")

		if err := a.pullConfig(); err != nil {
			a.log.Error().Err(err).Msg("failed to pull config")
		}
	}
}

// pullConfig pulls configuration from the server
func (a *Agent) pullConfig() error {
	resp, err := a.client.GetAgentConfig(a.ctx, a.agentID)
	if err != nil {
		return err
	}

	// Update local config version and license
	if err := a.config.UpdateConfigVersion(resp.ConfigVersion, resp.License); err != nil {
		return err
	}

	// Convert and save workers
	workers := make([]config.WorkerConfig, len(resp.Workers))
	for i, w := range resp.Workers {
		status := "stopped"
		if w.Enabled {
			status = "running"
		}
		workers[i] = config.WorkerConfig{
			WorkerID:   w.WorkerID,
			GPUIDs:     w.GPUIDs,
			ListenPort: w.ListenPort,
			Enabled:    w.Enabled,
			Status:     status,
		}
	}
	if err := a.config.SaveWorkers(workers); err != nil {
		return err
	}

	// Write to tensor-fusion state directory for hypervisor to pick up
	if err := a.syncToStateDir(workers); err != nil {
		a.log.Warn().Err(err).Msg("failed to sync workers to state directory")
	}

	a.configVersion = resp.ConfigVersion

	a.log.Info().
		Int("version", resp.ConfigVersion).
		Int("workers", len(resp.Workers)).
		Msg("config pulled successfully")

	return nil
}

// syncToStateDir syncs worker configuration to tensor-fusion state directory
func (a *Agent) syncToStateDir(workers []config.WorkerConfig) error {
	// Convert to tensor-fusion format
	tfWorkers := make([]map[string]interface{}, len(workers))
	for i, w := range workers {
		tfWorkers[i] = map[string]interface{}{
			"WorkerUID":        w.WorkerID,
			"AllocatedDevices": w.GPUIDs,
			"Status":           "Pending",
		}
		if w.Enabled {
			tfWorkers[i]["Status"] = "Running"
		}
	}

	data, err := json.MarshalIndent(tfWorkers, "", "  ")
	if err != nil {
		return err
	}

	stateDir := a.config.StateDir()
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}

	filePath := stateDir + "/workers.json"
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, filePath)
}

// reportStatus reports current status to the server
func (a *Agent) reportStatus() error {
	// Load current GPUs and workers
	gpuConfigs, err := a.config.LoadGPUs()
	if err != nil {
		return err
	}

	workerConfigs, err := a.config.LoadWorkers()
	if err != nil {
		return err
	}

	// Build status request
	gpuStatuses := make([]api.GPUStatus, len(gpuConfigs))
	for i, gpu := range gpuConfigs {
		gpuStatuses[i] = api.GPUStatus{
			GPUID:        gpu.GPUID,
			UsedByWorker: gpu.UsedByWorker,
		}
	}

	workerStatuses := make([]api.WorkerStatus, len(workerConfigs))
	for i, w := range workerConfigs {
		workerStatuses[i] = api.WorkerStatus{
			WorkerID:    w.WorkerID,
			Status:      w.Status,
			PID:         w.PID,
			GPUIDs:      w.GPUIDs,
			Connections: w.Connections,
		}
	}

	req := &api.AgentStatusRequest{
		Timestamp: time.Now(),
		GPUs:      gpuStatuses,
		Workers:   workerStatuses,
	}

	return a.client.ReportAgentStatus(a.ctx, a.agentID, req)
}

// computeFileHash computes a hash of the relevant config files
func (a *Agent) computeFileHash() string {
	hash := sha256.New()

	// Hash workers file
	workersPath := a.config.WorkersPath()
	if data, err := os.ReadFile(workersPath); err == nil {
		hash.Write(data)
	}

	// Hash state directory workers file
	stateWorkersPath := a.config.StateDir() + "/workers.json"
	if data, err := os.ReadFile(stateWorkersPath); err == nil {
		hash.Write(data)
	}

	return hex.EncodeToString(hash.Sum(nil))
}

// ErrNotRegistered indicates the agent is not registered
var ErrNotRegistered = &NotRegisteredError{}

// NotRegisteredError is returned when the agent is not registered
type NotRegisteredError struct{}

func (e *NotRegisteredError) Error() string {
	return "agent is not registered, please run 'ggo agent register' first"
}
