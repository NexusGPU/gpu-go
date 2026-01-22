package worker

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/NexusGPU/gpu-go/internal/errors"
	"github.com/NexusGPU/gpu-go/internal/log"
)

// WorkerMode represents the worker network mode
type WorkerMode string

const (
	WorkerModeTCP   WorkerMode = "tcp"
	WorkerModeShmem WorkerMode = "shmem"
)

// WorkerStatus represents the current status of a worker
type WorkerStatus string

const (
	WorkerStatusPending    WorkerStatus = "Pending"
	WorkerStatusRunning    WorkerStatus = "Running"
	WorkerStatusStopped    WorkerStatus = "Stopped"
	WorkerStatusTerminated WorkerStatus = "Terminated"
	WorkerStatusError      WorkerStatus = "Error"
)

// WorkerConfig represents configuration for a worker process
type WorkerConfig struct {
	WorkerID         string     `json:"worker_id"`
	GPUIDs           []string   `json:"gpu_ids"`
	ListenPort       int        `json:"listen_port"`
	Mode             WorkerMode `json:"mode"`
	ShmemFile        string     `json:"shmem_file,omitempty"`
	ShmemSizeMB      int        `json:"shmem_size_mb,omitempty"`
	Enabled          bool       `json:"enabled"`
	WorkerBinaryPath string     `json:"worker_binary_path,omitempty"`
}

// WorkerState represents the runtime state of a worker
type WorkerState struct {
	Config    WorkerConfig `json:"config"`
	Status    WorkerStatus `json:"status"`
	PID       int          `json:"pid,omitempty"`
	StartedAt *time.Time   `json:"started_at,omitempty"`
	Error     string       `json:"error,omitempty"`
}

// TensorFusionWorkerInfo represents the worker info format expected by tensor-fusion hypervisor
type TensorFusionWorkerInfo struct {
	WorkerUID        string   `json:"WorkerUID"`
	Namespace        string   `json:"Namespace,omitempty"`
	WorkerName       string   `json:"WorkerName,omitempty"`
	AllocatedDevices []string `json:"AllocatedDevices"`
	Status           string   `json:"Status"` // "Pending", "Running", "Terminated"
}

// Manager manages worker processes
type Manager struct {
	mu           sync.RWMutex
	workers      map[string]*WorkerState
	processes    map[string]*exec.Cmd
	stateDir     string
	workerBinary string
	ctx          context.Context
	cancel       context.CancelFunc
	log          *log.Logger
}

// NewManager creates a new worker manager
func NewManager(stateDir, workerBinary string) *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	if workerBinary == "" {
		workerBinary = "tensor-fusion-worker"
	}

	return &Manager{
		workers:      make(map[string]*WorkerState),
		processes:    make(map[string]*exec.Cmd),
		stateDir:     stateDir,
		workerBinary: workerBinary,
		ctx:          ctx,
		cancel:       cancel,
		log:          log.Default.WithComponent("worker-manager"),
	}
}

// Start starts a worker process
func (m *Manager) Start(config WorkerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	workerID := config.WorkerID
	l := m.log.WithWorkerID(workerID)

	// Check if already running
	if state, exists := m.workers[workerID]; exists {
		if state.Status == WorkerStatusRunning {
			return errors.Conflict("worker", "already running").WithDetail("worker_id", workerID)
		}
	}

	// Build command arguments
	args := m.buildWorkerArgs(config)

	// Determine binary path
	binaryPath := config.WorkerBinaryPath
	if binaryPath == "" {
		binaryPath = m.workerBinary
	}

	// Create command
	cmd := exec.CommandContext(m.ctx, binaryPath, args...)

	// Set environment
	cmd.Env = append(os.Environ(),
		"NVIDIA_VISIBLE_DEVICES=all",
		"WORKER_ID="+workerID,
	)

	// Set process group for proper cleanup
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	// Redirect output to logs
	logFile, err := m.createLogFile(workerID)
	if err != nil {
		l.Warn().Err(err).Msg("failed to create log file")
	} else {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return errors.Wrap(err, "failed to start worker process")
	}

	now := time.Now()
	state := &WorkerState{
		Config:    config,
		Status:    WorkerStatusRunning,
		PID:       cmd.Process.Pid,
		StartedAt: &now,
	}

	m.workers[workerID] = state
	m.processes[workerID] = cmd

	// Start goroutine to monitor process
	go m.monitorProcess(workerID, cmd)

	l.Info().Int("pid", cmd.Process.Pid).Int("port", config.ListenPort).Msg("worker started")

	return nil
}

// Stop stops a worker process
func (m *Manager) Stop(workerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	l := m.log.WithWorkerID(workerID)

	cmd, exists := m.processes[workerID]
	if !exists {
		return errors.NotFound("worker", workerID)
	}

	state := m.workers[workerID]
	if state.Status != WorkerStatusRunning {
		return nil // Already stopped
	}

	// Send SIGTERM first for graceful shutdown
	if cmd.Process != nil {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			l.Warn().Err(err).Msg("failed to send SIGTERM")
		}
	}

	// Wait for graceful shutdown with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		l.Info().Msg("worker stopped gracefully")
	case <-time.After(10 * time.Second):
		// Force kill if graceful shutdown fails
		if cmd.Process != nil {
			cmd.Process.Kill()
		}
		l.Warn().Msg("worker force killed after timeout")
	}

	state.Status = WorkerStatusStopped
	delete(m.processes, workerID)

	return nil
}

// GetStatus returns the status of a worker
func (m *Manager) GetStatus(workerID string) (*WorkerState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, exists := m.workers[workerID]
	if !exists {
		return nil, errors.NotFound("worker", workerID)
	}

	return state, nil
}

// List returns all worker states
func (m *Manager) List() []*WorkerState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	states := make([]*WorkerState, 0, len(m.workers))
	for _, state := range m.workers {
		states = append(states, state)
	}
	return states
}

// Reconcile reconciles the desired worker state with actual state
func (m *Manager) Reconcile(desiredWorkers []WorkerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Create map of desired workers
	desiredMap := make(map[string]WorkerConfig)
	for _, w := range desiredWorkers {
		desiredMap[w.WorkerID] = w
	}

	// Stop workers that are no longer desired
	for workerID, state := range m.workers {
		if _, desired := desiredMap[workerID]; !desired {
			if state.Status == WorkerStatusRunning {
				m.mu.Unlock()
				if err := m.Stop(workerID); err != nil {
					m.log.Error().Err(err).Str("worker_id", workerID).Msg("failed to stop unwanted worker")
				}
				m.mu.Lock()
			}
			delete(m.workers, workerID)
		}
	}

	// Start or update desired workers
	for workerID, config := range desiredMap {
		state, exists := m.workers[workerID]

		if !config.Enabled {
			// Worker should be stopped
			if exists && state.Status == WorkerStatusRunning {
				m.mu.Unlock()
				if err := m.Stop(workerID); err != nil {
					m.log.Error().Err(err).Str("worker_id", workerID).Msg("failed to stop disabled worker")
				}
				m.mu.Lock()
			}
			continue
		}

		// Worker should be running
		if !exists || state.Status != WorkerStatusRunning {
			m.mu.Unlock()
			if err := m.Start(config); err != nil {
				m.log.Error().Err(err).Str("worker_id", workerID).Msg("failed to start worker")
			}
			m.mu.Lock()
		}
	}

	return nil
}

// Shutdown stops all workers and cleans up
func (m *Manager) Shutdown() {
	m.cancel()

	m.mu.Lock()
	workerIDs := make([]string, 0, len(m.processes))
	for id := range m.processes {
		workerIDs = append(workerIDs, id)
	}
	m.mu.Unlock()

	for _, id := range workerIDs {
		if err := m.Stop(id); err != nil {
			m.log.Error().Err(err).Str("worker_id", id).Msg("failed to stop worker during shutdown")
		}
	}
}

// SaveStateFile saves worker state to a file in tensor-fusion format
func (m *Manager) SaveStateFile() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tfWorkers := make([]TensorFusionWorkerInfo, 0, len(m.workers))
	for _, state := range m.workers {
		status := "Pending"
		if state.Status == WorkerStatusRunning {
			status = "Running"
		} else if state.Status == WorkerStatusTerminated || state.Status == WorkerStatusError {
			status = "Terminated"
		}

		tfWorkers = append(tfWorkers, TensorFusionWorkerInfo{
			WorkerUID:        state.Config.WorkerID,
			AllocatedDevices: state.Config.GPUIDs,
			Status:           status,
		})
	}

	data, err := json.MarshalIndent(tfWorkers, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to marshal workers")
	}

	if err := os.MkdirAll(m.stateDir, 0755); err != nil {
		return errors.Wrap(err, "failed to create state directory")
	}

	filePath := filepath.Join(m.stateDir, "workers.json")
	tmpPath := filePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return errors.Wrap(err, "failed to write state file")
	}

	return os.Rename(tmpPath, filePath)
}

// LoadStateFile loads worker state from tensor-fusion format file
func (m *Manager) LoadStateFile() ([]WorkerConfig, error) {
	filePath := filepath.Join(m.stateDir, "workers.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "failed to read state file")
	}

	var tfWorkers []TensorFusionWorkerInfo
	if err := json.Unmarshal(data, &tfWorkers); err != nil {
		return nil, errors.Wrap(err, "failed to parse state file")
	}

	configs := make([]WorkerConfig, len(tfWorkers))
	for i, tw := range tfWorkers {
		configs[i] = WorkerConfig{
			WorkerID: tw.WorkerUID,
			GPUIDs:   tw.AllocatedDevices,
			Enabled:  tw.Status == "Running",
			Mode:     WorkerModeTCP,
		}
	}

	return configs, nil
}

func (m *Manager) buildWorkerArgs(config WorkerConfig) []string {
	var args []string

	switch config.Mode {
	case WorkerModeShmem:
		args = append(args, "-n", "shmem")
		if config.ShmemFile != "" {
			args = append(args, "-m", config.ShmemFile)
		}
		if config.ShmemSizeMB > 0 {
			args = append(args, "-M", strconv.Itoa(config.ShmemSizeMB))
		}
	default: // TCP mode
		args = append(args, "-p", strconv.Itoa(config.ListenPort))
	}

	return args
}

func (m *Manager) createLogFile(workerID string) (*os.File, error) {
	logDir := filepath.Join(m.stateDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, err
	}

	logPath := filepath.Join(logDir, "worker-"+workerID+".log")
	return os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
}

func (m *Manager) monitorProcess(workerID string, cmd *exec.Cmd) {
	err := cmd.Wait()

	m.mu.Lock()
	defer m.mu.Unlock()

	state, exists := m.workers[workerID]
	if !exists {
		return
	}

	l := m.log.WithWorkerID(workerID)
	if err != nil {
		state.Status = WorkerStatusError
		state.Error = err.Error()
		l.Error().Err(err).Msg("worker process exited with error")
	} else {
		state.Status = WorkerStatusTerminated
		l.Info().Msg("worker process exited")
	}

	delete(m.processes, workerID)
}

// DefaultWorkerPort returns the default worker port
const DefaultWorkerPort = 42352
