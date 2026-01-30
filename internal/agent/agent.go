package agent

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/NexusGPU/gpu-go/internal/hypervisor"
	hvApi "github.com/NexusGPU/tensor-fusion/pkg/hypervisor/api"
	"k8s.io/klog/v2"
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
	configVersion int

	// Hypervisor integration
	hypervisorMgr *hypervisor.Manager
	reconciler    *hypervisor.Reconciler

	// Dependencies for worker binary
	workerBinaryPath string
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
func NewAgentWithHypervisor(client *api.Client, configMgr *config.Manager, hvMgr *hypervisor.Manager, workerBinaryPath string) *Agent {
	agent := NewAgent(client, configMgr)
	agent.hypervisorMgr = hvMgr
	agent.workerBinaryPath = workerBinaryPath

	// Create reconciler
	agent.reconciler = hypervisor.NewReconciler(hypervisor.ReconcilerConfig{
		Manager: hvMgr,
		OnWorkerStarted: func(workerID string) {
			klog.Infof("Worker started via reconciler: worker_id=%s", workerID)
		},
		OnWorkerStopped: func(workerID string) {
			klog.Infof("Worker stopped via reconciler: worker_id=%s", workerID)
		},
		OnReconcileComplete: func(added, removed, updated int) {
			klog.V(4).Infof("Reconciliation complete: added=%d removed=%d updated=%d", added, removed, updated)
		},
	})

	return agent
}

// Register registers the agent with the server using a temporary token.
// Registration does not send a status report; status is reported only after Start() via statusReportLoop.
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

	klog.Infof("Agent registered successfully: agent_id=%s", resp.AgentID)

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

	// Start reconciler if available
	if a.reconciler != nil {
		a.reconciler.Start()
	}

	// Pull initial config
	if err := a.pullConfig(); err != nil {
		klog.Errorf("Failed to pull initial config: error=%v", err)
	}

	// Start background tasks
	a.wg.Add(2)
	go a.statusReportLoop()
	go a.configPollLoop()

	// Start WebSocket heartbeat
	if err := a.client.StartHeartbeat(a.ctx, a.agentID, a.handleHeartbeatResponse); err != nil {
		klog.Warningf("Failed to start heartbeat, will retry: error=%v", err)
	}

	klog.Infof("Agent started: agent_id=%s", a.agentID)

	return nil
}

// Stop stops the agent
func (a *Agent) Stop() {
	klog.Info("Stopping agent...")

	a.cancel()
	a.client.StopHeartbeat()

	// Stop reconciler
	if a.reconciler != nil {
		a.reconciler.Stop()
	}

	// Stop hypervisor manager
	if a.hypervisorMgr != nil {
		if err := a.hypervisorMgr.Stop(); err != nil {
			klog.Errorf("Failed to stop hypervisor manager: error=%v", err)
		}
	}

	a.wg.Wait()

	klog.Info("Agent stopped")
}

// statusReportLoop periodically reports status to the server
func (a *Agent) statusReportLoop() {
	defer a.wg.Done()

	// Report immediately on start
	if err := a.reportStatus(); err != nil {
		klog.Errorf("Failed to report initial status: error=%v", err)
	}

	ticker := time.NewTicker(statusReportInterval)
	defer ticker.Stop()

	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			if err := a.reportStatus(); err != nil {
				klog.Errorf("Failed to report status: error=%v", err)
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

// handleHeartbeatResponse handles WebSocket heartbeat responses
func (a *Agent) handleHeartbeatResponse(resp *api.HeartbeatResponse) {
	if resp.ConfigVersion > a.configVersion {
		klog.Infof("Config version changed, pulling new config: old_version=%d new_version=%d", a.configVersion, resp.ConfigVersion)

		if err := a.pullConfig(); err != nil {
			klog.Errorf("Failed to pull config: error=%v", err)
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

	// Convert and save workers to local config (raw API result)
	workers := make([]config.WorkerConfig, len(resp.Workers))
	for i, w := range resp.Workers {
		workers[i] = config.WorkerConfig{
			WorkerID:   w.WorkerID,
			GPUIDs:     w.GPUIDs,
			ListenPort: w.ListenPort,
			Enabled:    w.Enabled,
		}
	}
	if err := a.config.SaveWorkers(workers); err != nil {
		return err
	}

	// Reconcile workers with hypervisor if available
	if a.reconciler != nil {
		infos := a.convertToWorkerInfos(resp.Workers)
		a.reconciler.SetDesiredWorkers(infos)
	}

	a.configVersion = resp.ConfigVersion

	klog.Infof("Config pulled successfully: version=%d workers=%d", resp.ConfigVersion, len(resp.Workers))

	return nil
}

// convertToWorkerInfos converts API worker configs to hypervisor WorkerInfo
func (a *Agent) convertToWorkerInfos(apiWorkers []api.WorkerConfig) []*hvApi.WorkerInfo {
	infos := make([]*hvApi.WorkerInfo, 0, len(apiWorkers))
	for _, w := range apiWorkers {
		if !w.Enabled {
			continue
		}

		info := &hvApi.WorkerInfo{
			WorkerUID:        w.WorkerID,
			WorkerName:       w.WorkerID,
			AllocatedDevices: w.GPUIDs,
			Status:           hvApi.WorkerStatusPending,
		}

		// Set WorkerRunningInfo with remote-gpu-worker binary
		info.WorkerRunningInfo = &hvApi.WorkerRunningInfo{
			Type:       hvApi.WorkerRuntimeTypeProcess,
			Executable: a.workerBinaryPath,
			Args:       []string{"-p", fmt.Sprintf("%d", w.ListenPort), "-n", "native"},
			WorkingDir: a.config.StateDir(),
		}

		infos = append(infos, info)
	}
	return infos
}

// normalizeWorkerStatus maps status to API-allowed WorkerStatus: "pending" | "running" | "stopping" | "stopped".
// Empty or invalid values become "pending".
func normalizeWorkerStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running", "stopping", "stopped", "pending":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "pending"
	}
}

// reportStatus reports current status to the server
func (a *Agent) reportStatus() error {
	// Load current GPUs
	gpuConfigs, err := a.config.LoadGPUs()
	if err != nil {
		return err
	}

	// Build GPU status
	gpuStatuses := make([]api.GPUStatus, len(gpuConfigs))
	for i, gpu := range gpuConfigs {
		gpuStatuses[i] = api.GPUStatus{
			GPUID:        gpu.GPUID,
			UsedByWorker: gpu.UsedByWorker,
		}
	}

	// Get worker status from hypervisor (SSoT)
	var workerStatuses []api.WorkerStatus
	if a.hypervisorMgr != nil && a.hypervisorMgr.IsStarted() {
		hvWorkers := a.hypervisorMgr.ListWorkers()

		// Build status and log summary
		var runningCount, stoppedCount int
		var summaryParts []string
		workerStatuses = make([]api.WorkerStatus, 0, len(hvWorkers))

		for _, w := range hvWorkers {
			// Determine status from WorkerRunningInfo.IsRunning (API: pending|running|stopping|stopped)
			status := "stopped"
			if w.WorkerRunningInfo != nil && w.WorkerRunningInfo.IsRunning {
				status = "running"
				runningCount++
			} else {
				stoppedCount++
			}

			workerStatuses = append(workerStatuses, api.WorkerStatus{
				WorkerID: w.WorkerUID,
				Status:   normalizeWorkerStatus(status),
				GPUIDs:   w.AllocatedDevices,
			})
			summaryParts = append(summaryParts, fmt.Sprintf("%s(%s)", w.WorkerUID, status))
		}

		if len(hvWorkers) > 0 {
			klog.V(4).Infof("Workers summary: total=%d running=%d stopped=%d workers=[%s]",
				len(hvWorkers), runningCount, stoppedCount, strings.Join(summaryParts, ", "))
		}
	} else {
		// Fallback to config-based status if hypervisor not available
		workerConfigs, err := a.config.LoadWorkers()
		if err != nil {
			return err
		}
		workerStatuses = make([]api.WorkerStatus, len(workerConfigs))
		for i, w := range workerConfigs {
			workerStatuses[i] = api.WorkerStatus{
				WorkerID:    w.WorkerID,
				Status:      normalizeWorkerStatus(w.Status),
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
