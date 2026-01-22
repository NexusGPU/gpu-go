package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/fsnotify/fsnotify"
	"github.com/rs/zerolog/log"
)

const (
	statusReportInterval = 60 * time.Minute
	configPollInterval   = 10 * time.Second
)

// Agent manages the GPU agent lifecycle
type Agent struct {
	client *api.Client
	config *config.Manager
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	agentID       string
	hostname      string
	lastFileHash  string
	configVersion int

	// File watcher
	watcher *fsnotify.Watcher
}

// NewAgent creates a new agent
func NewAgent(client *api.Client, configMgr *config.Manager) *Agent {
	ctx, cancel := context.WithCancel(context.Background())

	hostname, _ := os.Hostname()

	return &Agent{
		client:   client,
		config:   configMgr,
		ctx:      ctx,
		cancel:   cancel,
		hostname: hostname,
	}
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

	log.Info().
		Str("agent_id", resp.AgentID).
		Msg("Agent registered successfully")

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

	// Initialize file watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	a.watcher = watcher

	// Watch config directory
	if err := watcher.Add(a.config.StateDir()); err != nil {
		log.Warn().Err(err).Msg("Failed to watch state directory")
	}

	// Pull initial config
	if err := a.pullConfig(); err != nil {
		log.Error().Err(err).Msg("Failed to pull initial config")
	}

	// Start background tasks
	a.wg.Add(3)
	go a.statusReportLoop()
	go a.configPollLoop()
	go a.fileWatchLoop()

	// Start WebSocket heartbeat
	if err := a.client.StartHeartbeat(a.ctx, a.agentID, a.handleHeartbeatResponse); err != nil {
		log.Warn().Err(err).Msg("Failed to start heartbeat, will retry")
	}

	log.Info().
		Str("agent_id", a.agentID).
		Msg("Agent started")

	return nil
}

// Stop stops the agent
func (a *Agent) Stop() {
	log.Info().Msg("Stopping agent...")

	a.cancel()
	a.client.StopHeartbeat()

	if a.watcher != nil {
		_ = a.watcher.Close()
	}

	a.wg.Wait()

	log.Info().Msg("Agent stopped")
}

// statusReportLoop periodically reports status to the server
func (a *Agent) statusReportLoop() {
	defer a.wg.Done()

	// Report immediately on start
	if err := a.reportStatus(); err != nil {
		log.Error().Err(err).Msg("Failed to report initial status")
	}

	ticker := time.NewTicker(statusReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if err := a.reportStatus(); err != nil {
				log.Error().Err(err).Msg("Failed to report status")
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
					log.Debug().Str("file", event.Name).Msg("File changed, reporting status")
					if err := a.reportStatus(); err != nil {
						log.Error().Err(err).Msg("Failed to report status after file change")
					}
				}
			}
		case err, ok := <-a.watcher.Errors:
			if !ok {
				return
			}
			log.Error().Err(err).Msg("File watcher error")
		}
	}
}

// handleHeartbeatResponse handles WebSocket heartbeat responses
func (a *Agent) handleHeartbeatResponse(resp *api.HeartbeatResponse) {
	if resp.ConfigVersion > a.configVersion {
		log.Info().
			Int("old_version", a.configVersion).
			Int("new_version", resp.ConfigVersion).
			Msg("Config version changed, pulling new config")

		if err := a.pullConfig(); err != nil {
			log.Error().Err(err).Msg("Failed to pull config")
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
		log.Warn().Err(err).Msg("Failed to sync workers to state directory")
	}

	a.configVersion = resp.ConfigVersion

	log.Info().
		Int("version", resp.ConfigVersion).
		Int("workers", len(resp.Workers)).
		Msg("Config pulled successfully")

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
