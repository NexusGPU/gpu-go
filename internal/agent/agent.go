package agent

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/config"
	"github.com/NexusGPU/gpu-go/internal/hypervisor"
	"github.com/NexusGPU/gpu-go/internal/platform"
	hvApi "github.com/NexusGPU/tensor-fusion/pkg/hypervisor/api"
	"k8s.io/klog/v2"
)

const (
	statusReportInterval = 1 * time.Minute
	forceRefreshInterval = 6 * time.Hour
	connectionsFileName  = "connections.txt"
	// EnvConnectionInfoPath is the environment variable name for connection info file path
	EnvConnectionInfoPath = "TF_CONNECTION_INFO_PATH"

	// Worker status constants
	workerStatusRunning  = "running"
	workerStatusStopped  = "stopped"
	workerStatusPending  = "pending"
	workerStatusStopping = "stopping"
)

// workerSnapshot captures worker state for change detection
type workerSnapshot struct {
	Status   string
	PID      int
	Restarts int
	GPUIDs   []string
}

// gpuSnapshot captures GPU state for change detection
type gpuSnapshot struct {
	GPUID         string
	Vendor        string
	Model         string
	VRAMMb        int64
	DriverVersion string
	CUDAVersion   string
}

// Agent manages the GPU agent lifecycle
type Agent struct {
	client *api.Client
	config *config.Manager
	paths  *platform.Paths
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	agentID       string
	hostname      string
	configVersion int

	// Hypervisor integration
	hypervisorMgr hypervisor.HypervisorManager
	reconciler    *hypervisor.Reconciler

	// Dependencies for worker binary
	workerBinaryPath string

	// Change tracking state
	mu                  sync.RWMutex
	lastForceRefresh    time.Time
	prevWorkers         map[string]*workerSnapshot // workerID -> snapshot
	prevConnections     map[string][]string        // workerID -> []connectionLine
	prevGPUs            map[string]*gpuSnapshot    // gpuID -> snapshot
	connectionsFilePath string
}

// NewAgent creates a new agent
func NewAgent(client *api.Client, configMgr *config.Manager) *Agent {
	ctx, cancel := context.WithCancel(context.Background())

	hostname, _ := os.Hostname()
	paths := platform.DefaultPaths()
	connectionsPath := filepath.Join(paths.StateDir(), connectionsFileName)

	return &Agent{
		client:              client,
		config:              configMgr,
		paths:               paths,
		ctx:                 ctx,
		cancel:              cancel,
		hostname:            hostname,
		prevWorkers:         make(map[string]*workerSnapshot),
		prevConnections:     make(map[string][]string),
		prevGPUs:            make(map[string]*gpuSnapshot),
		connectionsFilePath: connectionsPath,
	}
}

// NewAgentWithHypervisor creates a new agent with hypervisor manager
func NewAgentWithHypervisor(client *api.Client, configMgr *config.Manager, hvMgr hypervisor.HypervisorManager, workerBinaryPath string) *Agent {
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

	// Set TF_CONNECTION_INFO_PATH environment variable for worker processes
	// Workers will write connection info to this file, agent will read it for status reporting
	if err := os.Setenv(EnvConnectionInfoPath, a.connectionsFilePath); err != nil {
		klog.Warningf("Failed to set %s env var: error=%v", EnvConnectionInfoPath, err)
	} else {
		klog.V(4).Infof("Set %s=%s for worker processes", EnvConnectionInfoPath, a.connectionsFilePath)
	}

	// Write PID file
	if err := a.writePIDFile(); err != nil {
		klog.Warningf("Failed to write PID file: error=%v", err)
	}

	// Start reconciler if available
	if a.reconciler != nil {
		a.reconciler.Start()
	}

	// Pull initial config
	if err := a.pullConfig(); err != nil {
		klog.Errorf("Failed to pull initial config: error=%v", err)
	}

	// Start background tasks
	a.wg.Add(1)
	go a.statusReportLoop()

	// Start WebSocket heartbeat
	if err := a.client.StartHeartbeat(a.ctx, a.agentID, a.handleHeartbeatResponse); err != nil {
		klog.Warningf("Failed to start heartbeat, will retry: error=%v", err)
	}

	klog.Infof("Agent started: agent_id=%s pid=%d", a.agentID, os.Getpid())

	return nil
}

// Stop stops the agent
func (a *Agent) Stop() {
	klog.Info("Stopping agent...")

	// Send shutdown event to server before stopping
	if err := a.reportShutdown(); err != nil {
		klog.Errorf("Failed to report shutdown event: error=%v", err)
	}

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

	// Remove PID file
	if err := a.removePIDFile(); err != nil {
		klog.Warningf("Failed to remove PID file: error=%v", err)
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
	case workerStatusRunning, workerStatusStopping, workerStatusStopped, workerStatusPending:
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return workerStatusPending
	}
}

// shouldForceRefresh checks if 6 hours have passed since last force refresh
func (a *Agent) shouldForceRefresh() bool {
	a.mu.RLock()
	lastRefresh := a.lastForceRefresh
	a.mu.RUnlock()

	return time.Since(lastRefresh) >= forceRefreshInterval
}

// updateForceRefreshTime updates the last force refresh timestamp
func (a *Agent) updateForceRefreshTime() {
	a.mu.Lock()
	a.lastForceRefresh = time.Now()
	a.mu.Unlock()
}

// detectGPUChanges compares discovered GPUs with previous state
// Returns a map of gpuID -> changed flag
func (a *Agent) detectGPUChanges() (map[string]bool, error) {
	changes := make(map[string]bool)

	// Get current discovered GPUs from hypervisor
	var currentGPUs []*gpuSnapshot
	if a.hypervisorMgr != nil && a.hypervisorMgr.IsStarted() {
		devices, err := a.hypervisorMgr.ListDevices()
		if err != nil {
			return nil, fmt.Errorf("failed to list devices: %w", err)
		}
		for _, dev := range devices {
			driverVersion, cudaVersion := "", ""
			if dev.Properties != nil {
				driverVersion = dev.Properties["driverVersion"]
				cudaVersion = dev.Properties["cudaVersion"]
			}
			currentGPUs = append(currentGPUs, &gpuSnapshot{
				GPUID:         strings.ToLower(dev.UUID),
				Vendor:        dev.Vendor,
				Model:         dev.Model,
				VRAMMb:        int64(dev.TotalMemoryBytes / (1024 * 1024)),
				DriverVersion: driverVersion,
				CUDAVersion:   cudaVersion,
			})
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Compare with previous state
	currentMap := make(map[string]*gpuSnapshot)
	for _, gpu := range currentGPUs {
		currentMap[gpu.GPUID] = gpu
	}

	// Check for changed or new GPUs
	for gpuID, current := range currentMap {
		prev, exists := a.prevGPUs[gpuID]
		if !exists {
			changes[gpuID] = true
			continue
		}
		// Compare fields that matter for gpu_changed
		if current.Vendor != prev.Vendor ||
			current.Model != prev.Model ||
			current.VRAMMb != prev.VRAMMb ||
			current.DriverVersion != prev.DriverVersion ||
			current.CUDAVersion != prev.CUDAVersion {
			changes[gpuID] = true
		} else {
			changes[gpuID] = false
		}
	}

	// Check for removed GPUs
	for gpuID := range a.prevGPUs {
		if _, exists := currentMap[gpuID]; !exists {
			changes[gpuID] = true
		}
	}

	// Update previous state
	a.prevGPUs = currentMap

	return changes, nil
}

// detectWorkerChanges compares current workers with previous state
// Returns workerID -> changed flag
func (a *Agent) detectWorkerChanges(currentWorkers []*hvApi.WorkerInfo) map[string]bool {
	changes := make(map[string]bool)

	a.mu.Lock()
	defer a.mu.Unlock()

	// Build current snapshot map
	currentMap := make(map[string]*workerSnapshot)
	for _, w := range currentWorkers {
		status := workerStatusStopped
		var pid, restarts int
		if w.WorkerRunningInfo != nil {
			if w.WorkerRunningInfo.IsRunning {
				status = workerStatusRunning
			}
			pid = int(w.WorkerRunningInfo.PID)
			restarts = w.WorkerRunningInfo.Restarts
		}
		currentMap[w.WorkerUID] = &workerSnapshot{
			Status:   status,
			PID:      pid,
			Restarts: restarts,
			GPUIDs:   w.AllocatedDevices,
		}
	}

	// Check for changed or new workers
	for workerID, current := range currentMap {
		prev, exists := a.prevWorkers[workerID]
		if !exists {
			changes[workerID] = true
			continue
		}
		// Compare fields that matter for worker_changed
		if current.Status != prev.Status ||
			current.PID != prev.PID ||
			current.Restarts != prev.Restarts ||
			!slices.Equal(current.GPUIDs, prev.GPUIDs) {
			changes[workerID] = true
		} else {
			changes[workerID] = false
		}
	}

	// Check for removed workers
	for workerID := range a.prevWorkers {
		if _, exists := currentMap[workerID]; !exists {
			changes[workerID] = true
		}
	}

	// Update previous state
	a.prevWorkers = currentMap

	return changes
}

// readConnectionsFile reads the connections file written by worker processes
// Format: one connection per line: workerID,clientIP,clientPort,clientPID
// Returns workerID -> []connectionLine
func (a *Agent) readConnectionsFile() (connections map[string][]string, err error) {
	connections = make(map[string][]string)

	file, err := os.Open(a.connectionsFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return connections, nil // No connections file yet
		}
		return nil, fmt.Errorf("failed to open connections file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close connections file: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: workerID,clientIP,clientPort,clientPID
		parts := strings.SplitN(line, ",", 2)
		if len(parts) >= 2 {
			workerID := parts[0]
			connectionData := parts[1] // clientIP,clientPort,clientPID
			connections[workerID] = append(connections[workerID], connectionData)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read connections file: %w", err)
	}

	return connections, nil
}

// detectConnectionChanges compares current connections with previous state
// Returns workerID -> changed flag
func (a *Agent) detectConnectionChanges() (map[string]bool, error) {
	changes := make(map[string]bool)

	// Read current connections from file
	currentConnections, err := a.readConnectionsFile()
	if err != nil {
		return nil, err
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	// Check for changed or new connections
	for workerID, current := range currentConnections {
		prev, exists := a.prevConnections[workerID]
		if !exists {
			changes[workerID] = true
			continue
		}
		// Sort and compare
		slices.Sort(current)
		slices.Sort(prev)
		if !slices.Equal(current, prev) {
			changes[workerID] = true
		} else {
			changes[workerID] = false
		}
	}

	// Check for workers that lost all connections
	for workerID := range a.prevConnections {
		if _, exists := currentConnections[workerID]; !exists {
			changes[workerID] = true
		}
	}

	// Update previous state
	a.prevConnections = currentConnections

	return changes, nil
}

// parseConnectionsToAPI converts connection strings to API ConnectionInfo
func parseConnectionsToAPI(connectionLines []string) []api.ConnectionInfo {
	var connections []api.ConnectionInfo
	for _, line := range connectionLines {
		// Format: clientIP,clientPort,clientPID
		parts := strings.Split(line, ",")
		if len(parts) >= 1 {
			connections = append(connections, api.ConnectionInfo{
				ClientIP:    parts[0],
				ConnectedAt: time.Now(), // We don't have exact connect time, use current
			})
		}
	}
	return connections
}

// reportStatus reports current status to the server
func (a *Agent) reportStatus() error {
	klog.Infof("Reporting agent status to server: agent_id=%s", a.agentID)

	// Check if we should force refresh (every 6 hours)
	forceRefresh := a.shouldForceRefresh()
	if forceRefresh {
		klog.V(4).Infof("Force refresh triggered (6-hour interval)")
		a.updateForceRefreshTime()
	}

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

	// Detect GPU changes
	gpuChanges, err := a.detectGPUChanges()
	if err != nil {
		klog.Warningf("Failed to detect GPU changes: error=%v", err)
	}

	// Detect connection changes
	connectionChanges, err := a.detectConnectionChanges()
	if err != nil {
		klog.Warningf("Failed to detect connection changes: error=%v", err)
	}

	// Read current connections for reporting
	currentConnections, _ := a.readConnectionsFile()

	// Get worker status from hypervisor (SSoT)
	var workerStatuses []api.WorkerStatus
	if a.hypervisorMgr != nil && a.hypervisorMgr.IsStarted() {
		hvWorkers := a.hypervisorMgr.ListWorkers()

		// Detect worker changes
		workerChanges := a.detectWorkerChanges(hvWorkers)

		// Build status and log summary
		var runningCount, stoppedCount int
		var summaryParts []string
		workerStatuses = make([]api.WorkerStatus, 0, len(hvWorkers))

		for _, w := range hvWorkers {
			// Determine status from WorkerRunningInfo.IsRunning (API: pending|running|stopping|stopped)
			status := workerStatusStopped
			var pid int
			var restarts int
			if w.WorkerRunningInfo != nil {
				if w.WorkerRunningInfo.IsRunning {
					status = workerStatusRunning
					runningCount++
				} else {
					stoppedCount++
				}
				pid = int(w.WorkerRunningInfo.PID)
				restarts = w.WorkerRunningInfo.Restarts
			} else {
				stoppedCount++
			}

			// Compute change flags
			workerChanged := forceRefresh || workerChanges[w.WorkerUID]
			connectionChanged := forceRefresh || connectionChanges[w.WorkerUID]
			gpuChanged := forceRefresh || a.anyGPUChanged(w.AllocatedDevices, gpuChanges)

			// Get connections for this worker
			var connections []api.ConnectionInfo
			if connLines, ok := currentConnections[w.WorkerUID]; ok {
				connections = parseConnectionsToAPI(connLines)
			}

			workerStatuses = append(workerStatuses, api.WorkerStatus{
				WorkerID:          w.WorkerUID,
				Status:            normalizeWorkerStatus(status),
				PID:               pid,
				Restarts:          restarts,
				GPUIDs:            w.AllocatedDevices,
				Connections:       connections,
				WorkerChanged:     &workerChanged,
				ConnectionChanged: &connectionChanged,
				GPUChanged:        &gpuChanged,
			})
			summaryParts = append(summaryParts, fmt.Sprintf("%s(status=%s,pid=%d,restarts=%d,wc=%v,cc=%v,gc=%v)",
				w.WorkerUID, status, pid, restarts, workerChanged, connectionChanged, gpuChanged))
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
			// In fallback mode, mark all as changed since we can't accurately track
			workerChanged := true
			connectionChanged := true
			gpuChanged := forceRefresh || len(gpuChanges) > 0

			workerStatuses[i] = api.WorkerStatus{
				WorkerID:          w.WorkerID,
				Status:            normalizeWorkerStatus(w.Status),
				PID:               w.PID,
				GPUIDs:            w.GPUIDs,
				Connections:       w.Connections,
				WorkerChanged:     &workerChanged,
				ConnectionChanged: &connectionChanged,
				GPUChanged:        &gpuChanged,
			}
		}
	}

	// Load config to get license expiration
	cfg, err := a.config.LoadConfig()
	if err != nil {
		return err
	}

	// Parse license expiration from license plain text
	var licenseExpiration *int64
	if cfg != nil && cfg.License.Plain != "" {
		if exp := parseLicenseExpiration(cfg.License.Plain); exp > 0 {
			licenseExpiration = &exp
		}
	}

	req := &api.AgentStatusRequest{
		Timestamp:         time.Now(),
		GPUs:              gpuStatuses,
		Workers:           workerStatuses,
		LicenseExpiration: licenseExpiration,
	}

	resp, err := a.client.ReportAgentStatus(a.ctx, a.agentID, req)
	if err != nil {
		return err
	}

	// Handle response
	if resp != nil {
		// Update license if server returned a new one
		if resp.License != nil {
			klog.Infof("Server returned new license, updating config")
			if err := a.config.UpdateConfigVersion(a.configVersion, *resp.License); err != nil {
				klog.Errorf("Failed to update license: error=%v", err)
			}
		}

		// Pull new config if version changed
		if resp.ConfigVersion > a.configVersion {
			klog.Infof("Config version changed: old=%d new=%d, pulling new config", a.configVersion, resp.ConfigVersion)
			if err := a.pullConfig(); err != nil {
				klog.Errorf("Failed to pull config after version change: error=%v", err)
			}
		}
	}

	return nil
}

// anyGPUChanged checks if any GPU in the list has changed
func (a *Agent) anyGPUChanged(gpuIDs []string, gpuChanges map[string]bool) bool {
	for _, gpuID := range gpuIDs {
		if changed, ok := gpuChanges[gpuID]; ok && changed {
			return true
		}
	}
	return false
}

// parseLicenseExpiration extracts expiration timestamp from license plain text
// License plain text format is expected to contain expiration info
// Returns Unix timestamp in milliseconds, or 0 if parsing fails
func parseLicenseExpiration(licensePlain string) int64 {
	// Try pipe format first: gpuIDs|plan|expiry
	parts := strings.Split(licensePlain, "|")
	if len(parts) >= 3 {
		// Clean up potential trailing info or spaces
		expStr := strings.TrimSpace(parts[2])
		if exp, err := strconv.ParseInt(expStr, 10, 64); err == nil {
			return exp
		}
	}
	return 0
}

// GetHypervisorManager returns the hypervisor manager if available
func (a *Agent) GetHypervisorManager() hypervisor.HypervisorManager {
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

// writePIDFile writes the current process PID to the PID file
func (a *Agent) writePIDFile() error {
	pidFile := a.paths.AgentPIDFile()

	// Ensure state directory exists
	if err := os.MkdirAll(a.paths.StateDir(), 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	pid := os.Getpid()
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	klog.V(4).Infof("Wrote PID file: path=%s pid=%d", pidFile, pid)
	return nil
}

// removePIDFile removes the PID file
func (a *Agent) removePIDFile() error {
	pidFile := a.paths.AgentPIDFile()
	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove PID file: %w", err)
	}
	klog.V(4).Infof("Removed PID file: path=%s", pidFile)
	return nil
}

// reportShutdown sends shutdown event to the server
func (a *Agent) reportShutdown() error {
	// Use a short timeout context for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req := &api.AgentStatusRequest{
		Timestamp: time.Now(),
		GPUs:      []api.GPUStatus{},
		Workers:   []api.WorkerStatus{},
		Event:     api.AgentStatusEventShutdown,
	}

	klog.Infof("Sending shutdown event to server: agent_id=%s", a.agentID)
	_, err := a.client.ReportAgentStatus(ctx, a.agentID, req)
	return err
}

// LocalStatus represents the local agent status
type LocalStatus struct {
	Running bool
	PID     int
}

// GetLocalStatus checks local agent status by reading PID file and checking process
func GetLocalStatus(paths *platform.Paths) LocalStatus {
	pidFile := paths.AgentPIDFile()

	data, err := os.ReadFile(pidFile)
	if err != nil {
		return LocalStatus{Running: false, PID: 0}
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return LocalStatus{Running: false, PID: 0}
	}

	// Check if process is running
	process, err := os.FindProcess(pid)
	if err != nil {
		return LocalStatus{Running: false, PID: pid}
	}

	// On Unix, FindProcess always succeeds, so we need to send signal 0 to check
	if err := process.Signal(syscall.Signal(0)); err != nil {
		return LocalStatus{Running: false, PID: pid}
	}

	return LocalStatus{Running: true, PID: pid}
}
