package hypervisor

import (
	"context"
	"fmt"
	"sync"
	"time"

	tfv1 "github.com/NexusGPU/tensor-fusion/api/v1"
	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/api"
	"github.com/rs/zerolog"
)

// WorkerSpec represents the desired worker specification from cloud backend
type WorkerSpec struct {
	WorkerID          string
	GPUIDs            []string
	Enabled           bool
	IsolationMode     tfv1.IsolationModeType
	VRAMMb            int64
	ComputePercent    int
	PartitionTemplate string
}

// Reconciler reconciles cloud-desired workers with hypervisor-actual workers
type Reconciler struct {
	manager *Manager
	log     zerolog.Logger

	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	reconcileSignal chan struct{}
	desiredWorkers  map[string]*WorkerSpec

	// Callbacks for status updates
	onWorkerStarted     func(workerID string)
	onWorkerStopped     func(workerID string)
	onReconcileComplete func(added, removed, updated int)
}

// ReconcilerConfig holds configuration for the reconciler
type ReconcilerConfig struct {
	Manager *Manager
	Logger  zerolog.Logger

	// Optional callbacks
	OnWorkerStarted     func(workerID string)
	OnWorkerStopped     func(workerID string)
	OnReconcileComplete func(added, removed, updated int)
}

// NewReconciler creates a new worker reconciler
func NewReconciler(cfg ReconcilerConfig) *Reconciler {
	ctx, cancel := context.WithCancel(context.Background())

	return &Reconciler{
		manager:             cfg.Manager,
		log:                 cfg.Logger.With().Str("component", "reconciler").Logger(),
		ctx:                 ctx,
		cancel:              cancel,
		reconcileSignal:     make(chan struct{}, 1),
		desiredWorkers:      make(map[string]*WorkerSpec),
		onWorkerStarted:     cfg.OnWorkerStarted,
		onWorkerStopped:     cfg.OnWorkerStopped,
		onReconcileComplete: cfg.OnReconcileComplete,
	}
}

// SetDesiredWorkers updates the desired worker state
// This should be called when cloud backend config is pulled
func (r *Reconciler) SetDesiredWorkers(specs []WorkerSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.desiredWorkers = make(map[string]*WorkerSpec, len(specs))
	for i := range specs {
		spec := specs[i]
		r.desiredWorkers[spec.WorkerID] = &spec
	}

	r.log.Debug().Int("count", len(specs)).Msg("desired workers updated")

	// Signal reconciliation
	select {
	case r.reconcileSignal <- struct{}{}:
	default:
	}
}

// Start begins the reconciliation loop
func (r *Reconciler) Start() {
	go r.reconcileLoop()
	r.log.Info().Msg("reconciler started")
}

// Stop stops the reconciliation loop
func (r *Reconciler) Stop() {
	r.cancel()
	r.log.Info().Msg("reconciler stopped")
}

// TriggerReconcile triggers an immediate reconciliation
func (r *Reconciler) TriggerReconcile() {
	select {
	case r.reconcileSignal <- struct{}{}:
	default:
	}
}

func (r *Reconciler) reconcileLoop() {
	// Initial reconciliation
	r.reconcile()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.reconcile()
		case <-r.reconcileSignal:
			r.reconcile()
		}
	}
}

func (r *Reconciler) reconcile() {
	r.mu.RLock()
	desired := make(map[string]*WorkerSpec, len(r.desiredWorkers))
	for k, v := range r.desiredWorkers {
		desired[k] = v
	}
	r.mu.RUnlock()

	// Get actual workers from hypervisor
	actual := r.manager.ListWorkers()
	actualMap := make(map[string]*api.WorkerInfo, len(actual))
	for _, w := range actual {
		actualMap[w.WorkerUID] = w
	}

	var added, removed, updated int

	// 1. Find workers to start (in desired but not in actual, or disabled in actual)
	for workerID, spec := range desired {
		if !spec.Enabled {
			// If worker should be disabled, stop it if running
			if _, exists := actualMap[workerID]; exists {
				if err := r.stopWorker(workerID); err != nil {
					r.log.Error().Err(err).Str("worker_id", workerID).Msg("failed to stop disabled worker")
				} else {
					removed++
				}
			}
			continue
		}

		actualWorker, exists := actualMap[workerID]
		if !exists {
			// Worker doesn't exist, start it
			if err := r.startWorker(spec); err != nil {
				r.log.Error().Err(err).Str("worker_id", workerID).Msg("failed to start worker")
			} else {
				added++
			}
		} else if r.needsUpdate(spec, actualWorker) {
			// Worker exists but config changed, restart it
			if err := r.restartWorker(spec); err != nil {
				r.log.Error().Err(err).Str("worker_id", workerID).Msg("failed to restart worker")
			} else {
				updated++
			}
		}
	}

	// 2. Find workers to stop (in actual but not in desired)
	for workerID := range actualMap {
		if _, exists := desired[workerID]; !exists {
			if err := r.stopWorker(workerID); err != nil {
				r.log.Error().Err(err).Str("worker_id", workerID).Msg("failed to stop orphan worker")
			} else {
				removed++
			}
		}
	}

	if added > 0 || removed > 0 || updated > 0 {
		r.log.Info().
			Int("added", added).
			Int("removed", removed).
			Int("updated", updated).
			Msg("reconciliation complete")

		if r.onReconcileComplete != nil {
			r.onReconcileComplete(added, removed, updated)
		}
	}
}

func (r *Reconciler) startWorker(spec *WorkerSpec) error {
	workerInfo := r.specToWorkerInfo(spec)

	if err := r.manager.StartWorker(workerInfo); err != nil {
		return err
	}

	if r.onWorkerStarted != nil {
		r.onWorkerStarted(spec.WorkerID)
	}

	return nil
}

func (r *Reconciler) stopWorker(workerID string) error {
	if err := r.manager.StopWorker(workerID); err != nil {
		return err
	}

	if r.onWorkerStopped != nil {
		r.onWorkerStopped(workerID)
	}

	return nil
}

func (r *Reconciler) restartWorker(spec *WorkerSpec) error {
	// Stop first
	if err := r.stopWorker(spec.WorkerID); err != nil {
		r.log.Warn().Err(err).Str("worker_id", spec.WorkerID).Msg("failed to stop worker during restart")
	}

	// Small delay to ensure cleanup
	time.Sleep(100 * time.Millisecond)

	// Start with new config
	return r.startWorker(spec)
}

func (r *Reconciler) needsUpdate(spec *WorkerSpec, actual *api.WorkerInfo) bool {
	// Check if GPU allocation changed
	if len(spec.GPUIDs) != len(actual.AllocatedDevices) {
		return true
	}

	// Create set of desired GPUs
	desiredGPUs := make(map[string]bool, len(spec.GPUIDs))
	for _, gpuID := range spec.GPUIDs {
		desiredGPUs[gpuID] = true
	}

	// Check if all actual GPUs are in desired set
	for _, gpuID := range actual.AllocatedDevices {
		if !desiredGPUs[gpuID] {
			return true
		}
	}

	// Check if isolation mode changed
	if spec.IsolationMode != "" && spec.IsolationMode != actual.IsolationMode {
		return true
	}

	return false
}

func (r *Reconciler) specToWorkerInfo(spec *WorkerSpec) *api.WorkerInfo {
	isolationMode := spec.IsolationMode
	if isolationMode == "" {
		isolationMode = tfv1.IsolationModeShared
	}

	workerInfo := &api.WorkerInfo{
		WorkerUID:           spec.WorkerID,
		WorkerName:          spec.WorkerID,
		AllocatedDevices:    spec.GPUIDs,
		Status:              api.WorkerStatusPending,
		IsolationMode:       isolationMode,
		PartitionTemplateID: spec.PartitionTemplate,
	}

	// Set resource requests if specified
	if spec.VRAMMb > 0 || spec.ComputePercent > 0 {
		workerInfo.Requests = tfv1.Resource{}
		// Note: Resource fields use resource.Quantity which needs proper conversion
		// This is a simplified version
	}

	return workerInfo
}

// GetStatus returns current reconciler status
func (r *Reconciler) GetStatus() ReconcilerStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	actual := r.manager.ListWorkers()

	return ReconcilerStatus{
		DesiredCount: len(r.desiredWorkers),
		ActualCount:  len(actual),
		InSync:       r.isInSync(actual),
	}
}

func (r *Reconciler) isInSync(actual []*api.WorkerInfo) bool {
	actualMap := make(map[string]*api.WorkerInfo, len(actual))
	for _, w := range actual {
		actualMap[w.WorkerUID] = w
	}

	enabledCount := 0
	for workerID, spec := range r.desiredWorkers {
		if !spec.Enabled {
			continue
		}
		enabledCount++
		if _, exists := actualMap[workerID]; !exists {
			return false
		}
	}

	return enabledCount == len(actual)
}

// ReconcilerStatus represents the current reconciliation status
type ReconcilerStatus struct {
	DesiredCount int
	ActualCount  int
	InSync       bool
}

// String returns a human-readable representation
func (s *ReconcilerStatus) String() string {
	syncStatus := "out-of-sync"
	if s.InSync {
		syncStatus = "in-sync"
	}
	return fmt.Sprintf("desired=%d actual=%d %s", s.DesiredCount, s.ActualCount, syncStatus)
}
