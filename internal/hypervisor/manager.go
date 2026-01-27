package hypervisor

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	tfv1 "github.com/NexusGPU/tensor-fusion/api/v1"
	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/api"
	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/backend/single_node"
	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/device"
	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/framework"
	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/worker"
	"github.com/rs/zerolog"
)

// ErrNotStarted is returned when the manager is not started
var ErrNotStarted = errors.New("hypervisor manager not started")

// Manager wraps tensor-fusion hypervisor components for single-node GPU management
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	log    zerolog.Logger

	// Hypervisor components
	deviceController     *device.Controller
	allocationController framework.WorkerAllocationController
	backend              *single_node.SingleNodeBackend
	workerController     framework.WorkerController

	// Configuration
	libPath       string
	vendor        string
	isolationMode tfv1.IsolationModeType
	stateDir      string

	// State
	mu      sync.RWMutex
	started bool
}

// Config holds configuration for the hypervisor manager
type Config struct {
	// LibPath is the path to the accelerator library (e.g., libaccelerator_nvidia.so)
	LibPath string

	// Vendor identifier (e.g., "nvidia", "amd", "stub")
	Vendor string

	// IsolationMode for worker processes (shared, soft, partitioned)
	IsolationMode tfv1.IsolationModeType

	// StateDir for tensor-fusion state files (workers.json, devices.json)
	StateDir string

	// Logger for the manager
	Logger zerolog.Logger
}

// NewManager creates a new hypervisor manager
func NewManager(cfg Config) (*Manager, error) {
	if cfg.LibPath == "" {
		return nil, fmt.Errorf("accelerator library path is required")
	}

	if cfg.Vendor == "" {
		cfg.Vendor = "unknown"
	}

	if cfg.IsolationMode == "" {
		cfg.IsolationMode = tfv1.IsolationModeShared
	}

	if cfg.StateDir == "" {
		cfg.StateDir = os.Getenv("TENSOR_FUSION_STATE_DIR")
		if cfg.StateDir == "" {
			cfg.StateDir = "/tmp/tensor-fusion-state"
		}
	}

	// Set state dir env var for hypervisor components
	os.Setenv("TENSOR_FUSION_STATE_DIR", cfg.StateDir)

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		ctx:           ctx,
		cancel:        cancel,
		log:           cfg.Logger.With().Str("component", "hypervisor-manager").Logger(),
		libPath:       cfg.LibPath,
		vendor:        cfg.Vendor,
		isolationMode: cfg.IsolationMode,
		stateDir:      cfg.StateDir,
	}, nil
}

// Start initializes and starts all hypervisor components
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil
	}

	m.log.Info().
		Str("lib_path", m.libPath).
		Str("vendor", m.vendor).
		Str("isolation_mode", string(m.isolationMode)).
		Msg("starting hypervisor manager")

	// 1. Create device controller with accelerator library
	dc, err := device.NewController(m.ctx, m.libPath, m.vendor, 1*time.Hour, string(m.isolationMode))
	if err != nil {
		return fmt.Errorf("failed to create device controller: %w", err)
	}
	m.deviceController = dc

	// 2. Create allocation controller
	m.allocationController = worker.NewAllocationController(m.deviceController)
	m.deviceController.SetAllocationController(m.allocationController)

	// 3. Create single node backend
	m.backend = single_node.NewSingleNodeBackend(m.ctx, m.deviceController, m.allocationController)

	// 4. Create worker controller
	m.workerController = worker.NewWorkerController(
		m.deviceController,
		m.allocationController,
		m.isolationMode,
		m.backend,
	)

	// 5. Start device controller
	if err := m.deviceController.Start(); err != nil {
		return fmt.Errorf("failed to start device controller: %w", err)
	}

	// 6. Register device change handler from backend
	m.deviceController.RegisterDeviceUpdateHandler(m.backend.GetDeviceChangeHandler())

	// 7. Start backend
	if err := m.backend.Start(); err != nil {
		m.deviceController.Stop()
		return fmt.Errorf("failed to start backend: %w", err)
	}

	// 8. Start worker controller
	if err := m.workerController.Start(); err != nil {
		m.backend.Stop()
		m.deviceController.Stop()
		return fmt.Errorf("failed to start worker controller: %w", err)
	}

	m.started = true
	m.log.Info().Msg("hypervisor manager started")

	return nil
}

// Stop gracefully shuts down all hypervisor components
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return nil
	}

	m.log.Info().Msg("stopping hypervisor manager")

	var errs []error

	if m.workerController != nil {
		if err := m.workerController.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("worker controller stop: %w", err))
		}
	}

	if m.backend != nil {
		if err := m.backend.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("backend stop: %w", err))
		}
	}

	if m.deviceController != nil {
		if err := m.deviceController.Stop(); err != nil {
			errs = append(errs, fmt.Errorf("device controller stop: %w", err))
		}
	}

	m.cancel()
	m.started = false

	if len(errs) > 0 {
		return fmt.Errorf("errors during shutdown: %v", errs)
	}

	m.log.Info().Msg("hypervisor manager stopped")
	return nil
}

// ListDevices returns all discovered GPU devices
func (m *Manager) ListDevices() ([]*api.DeviceInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return nil, ErrNotStarted
	}
	return m.deviceController.ListDevices()
}

// GetDeviceMetrics returns metrics for all devices
func (m *Manager) GetDeviceMetrics() (map[string]*api.GPUUsageMetrics, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return nil, ErrNotStarted
	}
	return m.deviceController.GetDeviceMetrics()
}

// ListWorkers returns all workers from the backend
func (m *Manager) ListWorkers() []*api.WorkerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return nil
	}

	return m.backend.ListWorkers()
}

// StartWorker starts a worker with the given configuration
func (m *Manager) StartWorker(workerInfo *api.WorkerInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return ErrNotStarted
	}

	// Allocate devices for the worker
	if _, err := m.allocationController.AllocateWorkerDevices(workerInfo); err != nil {
		return fmt.Errorf("allocate devices: %w", err)
	}

	// Start the worker in the backend
	if err := m.backend.StartWorker(workerInfo); err != nil {
		m.allocationController.DeallocateWorker(workerInfo.WorkerUID)
		return fmt.Errorf("start worker: %w", err)
	}

	m.log.Info().
		Str("worker_uid", workerInfo.WorkerUID).
		Strs("devices", workerInfo.AllocatedDevices).
		Msg("worker started")

	return nil
}

// StopWorker stops a worker by its UID
func (m *Manager) StopWorker(workerUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return ErrNotStarted
	}

	// Deallocate worker devices (log warning but continue)
	if err := m.allocationController.DeallocateWorker(workerUID); err != nil {
		m.log.Warn().Err(err).Str("worker_uid", workerUID).Msg("deallocate failed")
	}

	if err := m.backend.StopWorker(workerUID); err != nil {
		return fmt.Errorf("stop worker: %w", err)
	}

	m.log.Info().Str("worker_uid", workerUID).Msg("worker stopped")
	return nil
}

// GetWorkerAllocation returns the allocation for a specific worker
func (m *Manager) GetWorkerAllocation(workerUID string) (*api.WorkerAllocation, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return nil, false
	}
	return m.allocationController.GetWorkerAllocation(workerUID)
}

// RegisterWorkerHandler registers a handler for worker change events
func (m *Manager) RegisterWorkerHandler(handler framework.WorkerChangeHandler) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.started {
		return ErrNotStarted
	}
	return m.backend.RegisterWorkerUpdateHandler(handler)
}

// RegisterDeviceHandler registers a handler for device change events
func (m *Manager) RegisterDeviceHandler(handler framework.DeviceChangeHandler) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.started {
		m.deviceController.RegisterDeviceUpdateHandler(handler)
	}
}

// GetVendor returns the accelerator vendor
func (m *Manager) GetVendor() string {
	return m.vendor
}

// GetStateDir returns the state directory path
func (m *Manager) GetStateDir() string {
	return m.stateDir
}

// IsStarted returns whether the manager is running
func (m *Manager) IsStarted() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.started
}

// DeviceController returns the underlying device controller for advanced use cases
func (m *Manager) DeviceController() framework.DeviceController {
	return m.deviceController
}

// Backend returns the underlying backend for advanced use cases
func (m *Manager) Backend() framework.Backend {
	return m.backend
}
