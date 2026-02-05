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
	// EnvConnectionInfoPath is the environment variable name for connection info directory path
	// Workers should write their connections to: {TF_CLIENT_INFO_PATH}/{workerID}.txt
	EnvConnectionInfoPath = "TF_CLIENT_INFO_PATH"

	// Worker status constants
	workerStatusRunning  = "running"
	workerStatusStopped  = "stopped"
	workerStatusPending  = "pending"
	workerStatusStopping = "stopping"

	// Hard limiter environment variables for Fractional GPU support
	// These only take effect when isolation_mode is "hard"
	// HardSMLimiterEnv sets compute limit in percent (1-100)
	HardSMLimiterEnv = "TF_CUDA_SM_PERCENT_LIMIT"
	// HardMemLimiterEnv sets memory limit in megabytes
	HardMemLimiterEnv = "TF_CUDA_MEMORY_LIMIT"

	// GPU visibility environment variables
	envCUDAVisibleDevices = "CUDA_VISIBLE_DEVICES"
	envHIPVisibleDevices  = "HIP_VISIBLE_DEVICES"
	envROCRVisibleDevices = "ROCR_VISIBLE_DEVICES"
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
	mu               sync.RWMutex
	lastForceRefresh time.Time
	prevWorkers      map[string]*workerSnapshot // workerID -> snapshot
	prevConnections  map[string][]string        // workerID -> []connectionLine
	prevGPUs         map[string]*gpuSnapshot    // gpuID -> snapshot
	connectionsDir   string                     // directory containing per-worker connection files
}

// NewAgent creates a new agent
func NewAgent(client *api.Client, configMgr *config.Manager) *Agent {
	ctx, cancel := context.WithCancel(context.Background())

	hostname, _ := os.Hostname()
	paths := platform.DefaultPaths()

	return &Agent{
		client:          client,
		config:          configMgr,
		paths:           paths,
		ctx:             ctx,
		cancel:          cancel,
		hostname:        hostname,
		prevWorkers:     make(map[string]*workerSnapshot),
		prevConnections: make(map[string][]string),
		prevGPUs:        make(map[string]*gpuSnapshot),
		connectionsDir:  paths.ConnectionsDir(),
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

	// Ensure connections directory exists for worker processes
	if err := os.MkdirAll(a.connectionsDir, 0755); err != nil {
		klog.Warningf("Failed to create connections directory: path=%s error=%v", a.connectionsDir, err)
	}

	// Set TF_CLIENT_INFO_PATH environment variable for worker processes
	// Workers will write connection info to: {TF_CLIENT_INFO_PATH}/{workerID}.txt
	// Each worker has its own file with format: clientIP,clientPort,clientPID (one per line)
	if err := os.Setenv(EnvConnectionInfoPath, a.connectionsDir); err != nil {
		klog.Warningf("Failed to set %s env var: error=%v", EnvConnectionInfoPath, err)
	} else {
		klog.V(4).Infof("Set %s=%s for worker processes", EnvConnectionInfoPath, a.connectionsDir)
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
			WorkerID:       w.WorkerID,
			GPUIDs:         w.GPUIDs,
			VRAMMb:         w.VRAMMb,
			ComputePercent: w.ComputePercent,
			ListenPort:     w.ListenPort,
			Enabled:        w.Enabled,
		}
	}
	if err := a.config.SaveWorkers(workers); err != nil {
		return err
	}

	// Reconcile workers with hypervisor if available
	if a.reconciler != nil {
		infos, err := a.convertToWorkerInfos(resp.Workers)
		if err != nil {
			return fmt.Errorf("failed to convert worker infos: %w", err)
		}
		a.reconciler.SetDesiredWorkers(infos)
	}

	a.configVersion = resp.ConfigVersion

	klog.Infof("Config pulled successfully: version=%d workers=%d", resp.ConfigVersion, len(resp.Workers))

	return nil
}

// convertToWorkerInfos converts API worker configs to hypervisor WorkerInfo
func (a *Agent) convertToWorkerInfos(apiWorkers []api.WorkerConfig) ([]*hvApi.WorkerInfo, error) {
	// Load config to get license information
	cfg, err := a.config.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	// Validate license fields
	if cfg.License.Plain == "" {
		klog.Errorf("License plain field is missing in config.json")
		return nil, fmt.Errorf("license plain field is missing in config.json")
	}
	if cfg.License.Encrypted == "" {
		klog.Errorf("License encrypted field is missing in config.json")
		return nil, fmt.Errorf("license encrypted field is missing in config.json")
	}

	gpuVendorByID, err := a.loadGPUVendorByID()
	if err != nil {
		klog.Warningf("Failed to load GPU config for visibility env: %v", err)
	}

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
		// Ensure Env map is initialized (not nil) to avoid issues in single_node backend
		envVars := make(map[string]string)
		envVars["TF_LICENSE"] = cfg.License.Plain
		envVars["TF_LICENSE_SIGN"] = cfg.License.Encrypted

		// Set hard limiter environment variables for Fractional GPU support
		// TODO: use MIG for partitioned
		if w.ComputePercent > 0 {
			envVars[HardSMLimiterEnv] = fmt.Sprintf("%d", w.ComputePercent)
			klog.Infof("Worker %s: Setting compute limit to %d%% (%s=%d)",
				w.WorkerID, w.ComputePercent, HardSMLimiterEnv, w.ComputePercent)
		}
		if w.VRAMMb > 0 {
			envVars[HardMemLimiterEnv] = fmt.Sprintf("%d", w.VRAMMb)
			klog.Infof("Worker %s: Setting memory limit to %d MB (%s=%d)",
				w.WorkerID, w.VRAMMb, HardMemLimiterEnv, w.VRAMMb)
		}

		vendor := resolveWorkerVendor(w.WorkerID, w.GPUIDs, gpuVendorByID)
		for k, v := range buildGPUVisibilityEnv(vendor, w.GPUIDs) {
			envVars[k] = v
		}

		info.WorkerRunningInfo = &hvApi.WorkerRunningInfo{
			Type:       hvApi.WorkerRuntimeTypeProcess,
			Executable: a.workerBinaryPath,
			Args:       []string{"-p", fmt.Sprintf("%d", w.ListenPort), "-n", "native"},
			WorkingDir: a.config.StateDir(),
			Env:        envVars,
		}
		klog.Infof("Set environment variables for worker %s: TF_LICENSE (len=%d, empty=%v), TF_LICENSE_SIGN (len=%d, empty=%v)",
			w.WorkerID, len(cfg.License.Plain), cfg.License.Plain == "", len(cfg.License.Encrypted), cfg.License.Encrypted == "")

		infos = append(infos, info)
	}
	return infos, nil
}

func (a *Agent) loadGPUVendorByID() (map[string]string, error) {
	gpus, err := a.config.LoadGPUs()
	if err != nil {
		return nil, err
	}

	vendorByID := make(map[string]string, len(gpus))
	for _, gpu := range gpus {
		id := strings.ToLower(strings.TrimSpace(gpu.GPUID))
		if id == "" {
			continue
		}
		vendor := strings.ToLower(strings.TrimSpace(gpu.Vendor))
		if vendor == "" {
			continue
		}
		vendorByID[id] = vendor
	}

	return vendorByID, nil
}

func resolveWorkerVendor(workerID string, gpuIDs []string, gpuVendorByID map[string]string) string {
	if len(gpuIDs) == 0 || len(gpuVendorByID) == 0 {
		return ""
	}

	var vendor string
	for _, id := range gpuIDs {
		normalizedID := strings.ToLower(strings.TrimSpace(id))
		if normalizedID == "" {
			continue
		}
		v, ok := gpuVendorByID[normalizedID]
		if !ok || v == "" {
			continue
		}
		if vendor == "" {
			vendor = v
			continue
		}
		if v != vendor {
			klog.Warningf("Worker has mixed GPU vendors; using first vendor for visibility env: worker_id=%s vendor=%s other_vendor=%s", workerID, vendor, v)
			return vendor
		}
	}

	return vendor
}

func buildGPUVisibilityEnv(vendor string, gpuIDs []string) map[string]string {
	if vendor == "" || len(gpuIDs) == 0 {
		return nil
	}

	visibleIDs := make([]string, 0, len(gpuIDs))
	for _, id := range gpuIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		switch vendor {
		case vendorNVIDIA:
			visibleIDs = append(visibleIDs, normalizeNvidiaGPUID(trimmed))
		default:
			visibleIDs = append(visibleIDs, trimmed)
		}
	}

	if len(visibleIDs) == 0 {
		return nil
	}

	value := strings.Join(visibleIDs, ",")
	env := make(map[string]string, 2)
	switch vendor {
	case vendorNVIDIA:
		env[envCUDAVisibleDevices] = value
	case vendorAMD, "hygon":
		env[envHIPVisibleDevices] = value
		env[envROCRVisibleDevices] = value
	}

	return env
}

func normalizeNvidiaGPUID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == "" {
		return id
	}
	dash := strings.Index(id, "-")
	if dash == -1 {
		return strings.ToUpper(id)
	}
	return strings.ToUpper(id[:dash]) + id[dash:]
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
func (a *Agent) detectGPUChanges(devices []*hvApi.DeviceInfo) (map[string]bool, error) {
	changes := make(map[string]bool)

	// Get current discovered GPUs from hypervisor
	var currentGPUs []*gpuSnapshot
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

// readConnectionsFromDir reads connection files from the connections directory
// Each worker has its own file: {connectionsDir}/{workerID}.txt
// File format: one connection per line: clientIP,clientPort,clientPID
// Returns workerID -> []connectionLine
func (a *Agent) readConnectionsFromDir() (map[string][]string, error) {
	connections := make(map[string][]string)

	entries, err := os.ReadDir(a.connectionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return connections, nil // No connections directory yet
		}
		return nil, fmt.Errorf("failed to read connections directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}

		// Extract workerID from filename (remove .txt suffix)
		workerID := strings.TrimSuffix(entry.Name(), ".txt")
		if workerID == "" {
			continue
		}

		// Read connection lines from worker's file
		connLines, err := a.readWorkerConnectionFile(filepath.Join(a.connectionsDir, entry.Name()))
		if err != nil {
			klog.V(4).Infof("Failed to read connection file for worker %s: %v", workerID, err)
			continue
		}

		if len(connLines) > 0 {
			connections[workerID] = connLines
		}
	}

	return connections, nil
}

// readWorkerConnectionFile reads a single worker's connection file
// Format: one connection per line: clientIP,clientPort,clientPID
func (a *Agent) readWorkerConnectionFile(filePath string) (lines []string, err error) {
	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("failed to close file: %w", closeErr)
		}
	}()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Validate format: should have at least clientIP
		if parts := strings.Split(line, ","); len(parts) >= 1 && parts[0] != "" {
			lines = append(lines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan file: %w", err)
	}

	return lines, nil
}

// detectConnectionChanges compares current connections with previous state
// Returns workerID -> changed flag
func (a *Agent) detectConnectionChanges() (map[string]bool, error) {
	changes := make(map[string]bool)

	// Read current connections from directory
	currentConnections, err := a.readConnectionsFromDir()
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
// Input format per line: clientIP,clientPort,clientPID
func parseConnectionsToAPI(connectionLines []string) []api.ConnectionInfo {
	connections := make([]api.ConnectionInfo, 0, len(connectionLines))
	for _, line := range connectionLines {
		// Parse: clientIP,clientPort,clientPID
		parts := strings.Split(line, ",")
		if len(parts) < 1 || parts[0] == "" {
			continue
		}
		clientIP := strings.TrimSpace(parts[0])
		connections = append(connections, api.ConnectionInfo{
			ClientIP:    clientIP,
			ConnectedAt: time.Now(), // Worker doesn't track exact connect time
		})
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

	// 1. Collect GPU status
	gpuStatuses, gpuChanges, err := a.collectGPUStatus(forceRefresh)
	if err != nil {
		return err
	}

	// 2. Detect connection changes
	connectionChanges, err := a.detectConnectionChanges()
	if err != nil {
		klog.Warningf("Failed to detect connection changes: error=%v", err)
	}

	// 3. Read current connections
	currentConnections, _ := a.readConnectionsFromDir()

	// 4. Collect Worker status
	workerStatuses, err := a.collectWorkerStatus(forceRefresh, connectionChanges, currentConnections, gpuChanges)
	if err != nil {
		return err
	}

	// 5. Get license expiration
	licenseExpiration, err := a.getLicenseExpiration()
	if err != nil {
		return err
	}

	// 6. Send request
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

	// 7. Handle response
	a.handleReportResponse(resp)

	return nil
}

// collectGPUStatus collects current GPU status and changes
func (a *Agent) collectGPUStatus(forceRefresh bool) ([]api.GPUStatus, map[string]bool, error) {
	// Load current GPUs from config
	gpuConfigs, err := a.config.LoadGPUs()
	if err != nil {
		return nil, nil, err
	}

	// Get current device info from hypervisor if available
	var liveDevices []*hvApi.DeviceInfo
	if a.hypervisorMgr != nil && a.hypervisorMgr.IsStarted() {
		var err error
		liveDevices, err = a.hypervisorMgr.ListDevices()
		if err != nil {
			klog.Warningf("Failed to list devices from hypervisor: %v", err)
		}
	}

	// Create map for live devices
	liveDeviceMap := make(map[string]*hvApi.DeviceInfo)
	for _, dev := range liveDevices {
		liveDeviceMap[strings.ToLower(dev.UUID)] = dev
	}

	// Detect GPU changes
	gpuChanges, err := a.detectGPUChanges(liveDevices)
	if err != nil {
		klog.Warningf("Failed to detect GPU changes: error=%v", err)
		// If detection fails, we can assume no changes or proceed with empty map
		if gpuChanges == nil {
			gpuChanges = make(map[string]bool)
		}
	}

	// Build GPU status
	gpuStatuses := make([]api.GPUStatus, len(gpuConfigs))
	for i, gpu := range gpuConfigs {
		// Default values from config
		driverVer := ""
		cudaVer := ""

		// Update with live info if available
		if liveDev, ok := liveDeviceMap[strings.ToLower(gpu.GPUID)]; ok {
			if liveDev.Properties != nil {
				driverVer = liveDev.Properties["driverVersion"]
				cudaVer = liveDev.Properties["cudaVersion"]
			}
		}

		gpuChanged := forceRefresh || gpuChanges[gpu.GPUID]

		gpuStatuses[i] = api.GPUStatus{
			GPUID:         gpu.GPUID,
			UsedByWorker:  gpu.UsedByWorker,
			Vendor:        gpu.Vendor,
			Model:         gpu.Model,
			VRAMMb:        gpu.VRAMMb,
			DriverVersion: driverVer,
			CUDAVersion:   cudaVer,
			GPUChanged:    gpuChanged,
		}
	}

	return gpuStatuses, gpuChanges, nil
}

// collectWorkerStatus collects current worker status
func (a *Agent) collectWorkerStatus(
	forceRefresh bool,
	connectionChanges map[string]bool,
	currentConnections map[string][]string,
	gpuChanges map[string]bool,
) ([]api.WorkerStatus, error) {
	// If hypervisor is available, use it as SSoT
	if a.hypervisorMgr != nil && a.hypervisorMgr.IsStarted() {
		return a.collectWorkerStatusFromHypervisor(forceRefresh, connectionChanges, currentConnections, gpuChanges)
	}
	// Fallback to config-based status
	return a.collectWorkerStatusFromConfig(forceRefresh, gpuChanges)
}

// collectWorkerStatusFromHypervisor gets worker status from hypervisor
func (a *Agent) collectWorkerStatusFromHypervisor(
	forceRefresh bool,
	connectionChanges map[string]bool,
	currentConnections map[string][]string,
	gpuChanges map[string]bool,
) ([]api.WorkerStatus, error) {
	hvWorkers := a.hypervisorMgr.ListWorkers()

	// Detect worker changes
	workerChanges := a.detectWorkerChanges(hvWorkers)

	// Build status and log summary
	var runningCount, stoppedCount int
	var summaryParts []string
	workerStatuses := make([]api.WorkerStatus, 0, len(hvWorkers))

	for _, w := range hvWorkers {
		// Determine status from WorkerRunningInfo.IsRunning
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

	return workerStatuses, nil
}

// collectWorkerStatusFromConfig gets worker status from local config (fallback)
func (a *Agent) collectWorkerStatusFromConfig(forceRefresh bool, gpuChanges map[string]bool) ([]api.WorkerStatus, error) {
	workerConfigs, err := a.config.LoadWorkers()
	if err != nil {
		return nil, err
	}

	workerStatuses := make([]api.WorkerStatus, len(workerConfigs))
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
	return workerStatuses, nil
}

// getLicenseExpiration reads config and parses license expiration
func (a *Agent) getLicenseExpiration() (*int64, error) {
	cfg, err := a.config.LoadConfig()
	if err != nil {
		return nil, err
	}

	if cfg != nil && cfg.License.Plain != "" {
		if exp := parseLicenseExpiration(cfg.License.Plain); exp > 0 {
			return &exp, nil
		}
	}
	return nil, nil
}

// handleReportResponse handles the status report response
func (a *Agent) handleReportResponse(resp *api.AgentStatusResponse) {
	if resp == nil {
		return
	}

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
