package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"runtime"
	"sync"
	"time"

	tfv1 "github.com/NexusGPU/tensor-fusion/api/v1"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/NexusGPU/gpu-go/internal/hypervisor"
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

	// Hypervisor integration
	hypervisorMgr *hypervisor.Manager
	reconciler    *hypervisor.Reconciler

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

// NewAgentWithHypervisor creates a new agent with hypervisor manager
func NewAgentWithHypervisor(client *api.Client, configMgr *config.Manager, hvMgr *hypervisor.Manager) *Agent {
	agent := NewAgent(client, configMgr)
	agent.hypervisorMgr = hvMgr

	// Create reconciler
	agent.reconciler = hypervisor.NewReconciler(hypervisor.ReconcilerConfig{
		Manager: hvMgr,
		Logger:  log.Logger,
		OnWorkerStarted: func(workerID string) {
			log.Info().Str("worker_id", workerID).Msg("worker started via reconciler")
		},
		OnWorkerStopped: func(workerID string) {
			log.Info().Str("worker_id", workerID).Msg("worker stopped via reconciler")
		},
		OnReconcileComplete: func(added, removed, updated int) {
			log.Debug().
				Int("added", added).
				Int("removed", removed).
				Int("updated", updated).
				Msg("reconciliation complete")
		},
	})

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

	// Start reconciler if available
	if a.reconciler != nil {
		a.reconciler.Start()
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

	// Stop reconciler
	if a.reconciler != nil {
		a.reconciler.Stop()
	}

	// Stop hypervisor manager
	if a.hypervisorMgr != nil {
		if err := a.hypervisorMgr.Stop(); err != nil {
			log.Error().Err(err).Msg("Failed to stop hypervisor manager")
		}
	}

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

	// Reconcile workers with hypervisor if available
	if a.reconciler != nil {
		specs := a.convertToWorkerSpecs(resp.Workers)
		a.reconciler.SetDesiredWorkers(specs)
	}

	a.configVersion = resp.ConfigVersion

	log.Info().
		Int("version", resp.ConfigVersion).
		Int("workers", len(resp.Workers)).
		Msg("Config pulled successfully")

	return nil
}

// convertToWorkerSpecs converts API worker configs to hypervisor worker specs
func (a *Agent) convertToWorkerSpecs(apiWorkers []api.WorkerConfig) []hypervisor.WorkerSpec {
	specs := make([]hypervisor.WorkerSpec, len(apiWorkers))
	for i, w := range apiWorkers {
		specs[i] = hypervisor.WorkerSpec{
			WorkerID:       w.WorkerID,
			GPUIDs:         w.GPUIDs,
			Enabled:        w.Enabled,
			VRAMMb:         w.VRAMMb,
			ComputePercent: w.ComputePercent,
		}
		if w.IsolationMode != "" {
			specs[i].IsolationMode = toTFIsolationMode(w.IsolationMode)
		}
	}
	return specs
}

// toTFIsolationMode converts a string to tensor-fusion IsolationModeType
func toTFIsolationMode(s string) tfv1.IsolationModeType {
	switch s {
	case "soft":
		return tfv1.IsolationModeSoft
	case "partitioned":
		return tfv1.IsolationModePartitioned
	default:
		return tfv1.IsolationModeShared
	}
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

	// If hypervisor is available, get worker status from it
	var workerStatuses []api.WorkerStatus
	if a.hypervisorMgr != nil && a.hypervisorMgr.IsStarted() {
		hvWorkers := a.hypervisorMgr.ListWorkers()
		workerStatuses = make([]api.WorkerStatus, len(hvWorkers))
		for i, w := range hvWorkers {
			workerStatuses[i] = api.WorkerStatus{
				WorkerID: w.WorkerUID,
				Status:   string(w.Status),
				GPUIDs:   w.AllocatedDevices,
			}
		}
	} else {
		// Fallback to config-based status
		workerStatuses = make([]api.WorkerStatus, len(workerConfigs))
		for i, w := range workerConfigs {
			workerStatuses[i] = api.WorkerStatus{
				WorkerID:    w.WorkerID,
				Status:      w.Status,
				PID:         w.PID,
				GPUIDs:      w.GPUIDs,
				Connections: w.Connections,
			}
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

// GetHypervisorManager returns the hypervisor manager if available
func (a *Agent) GetHypervisorManager() *hypervisor.Manager {
	return a.hypervisorMgr
}

// GetReconciler returns the reconciler if available
func (a *Agent) GetReconciler() *hypervisor.Reconciler {
	return a.reconciler
}

// ErrNotRegistered indicates the agent is not registered
var ErrNotRegistered = &NotRegisteredError{}

// NotRegisteredError is returned when the agent is not registered
type NotRegisteredError struct{}

func (e *NotRegisteredError) Error() string {
	return "agent is not registered, please run 'ggo agent register' first"
}
