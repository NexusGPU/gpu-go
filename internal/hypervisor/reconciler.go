package hypervisor

import (
	"context"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/api"
	"k8s.io/klog/v2"
)

// Reconciler reconciles cloud-desired workers with hypervisor-actual workers
type Reconciler struct {
	manager HypervisorManager

	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	reconcileSignal chan struct{}
	desiredWorkers  map[string]*api.WorkerInfo
	forceRestarts   map[string]struct{}

	// Callbacks for status updates
	onWorkerStarted     func(workerID string)
	onWorkerStopped     func(workerID string)
	onReconcileComplete func(added, removed, updated int)
}

// ReconcilerConfig holds configuration for the reconciler
type ReconcilerConfig struct {
	Manager HypervisorManager

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
		ctx:                 ctx,
		cancel:              cancel,
		reconcileSignal:     make(chan struct{}, 1),
		desiredWorkers:      make(map[string]*api.WorkerInfo),
		forceRestarts:       make(map[string]struct{}),
		onWorkerStarted:     cfg.OnWorkerStarted,
		onWorkerStopped:     cfg.OnWorkerStopped,
		onReconcileComplete: cfg.OnReconcileComplete,
	}
}

// SetDesiredWorkers updates the desired worker state
// This should be called when cloud backend config is pulled
func (r *Reconciler) SetDesiredWorkers(infos []*api.WorkerInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.desiredWorkers = make(map[string]*api.WorkerInfo, len(infos))
	for _, info := range infos {
		r.desiredWorkers[info.WorkerUID] = info
	}

	klog.V(4).Infof("Desired workers updated: count=%d", len(infos))

	// Signal reconciliation
	select {
	case r.reconcileSignal <- struct{}{}:
	default:
	}
}

// Start begins the reconciliation loop
func (r *Reconciler) Start() {
	go r.reconcileLoop()
	klog.Info("Reconciler started")
}

// Stop stops the reconciliation loop
func (r *Reconciler) Stop() {
	r.cancel()
	klog.Info("Reconciler stopped")
}

// TriggerReconcile triggers an immediate reconciliation
func (r *Reconciler) TriggerReconcile() {
	select {
	case r.reconcileSignal <- struct{}{}:
	default:
	}
}

// RequestWorkerRestarts queues explicit worker restart requests and triggers reconciliation.
// Returns the number of newly queued workers.
func (r *Reconciler) RequestWorkerRestarts(workerIDs []string) int {
	r.mu.Lock()
	queued := 0
	for _, workerID := range workerIDs {
		if workerID == "" {
			continue
		}
		if _, exists := r.forceRestarts[workerID]; exists {
			continue
		}
		r.forceRestarts[workerID] = struct{}{}
		queued++
	}
	r.mu.Unlock()

	if queued > 0 {
		r.TriggerReconcile()
	}

	return queued
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
	r.mu.Lock()
	desired := make(map[string]*api.WorkerInfo, len(r.desiredWorkers))
	maps.Copy(desired, r.desiredWorkers)
	forceRestarts := make(map[string]struct{}, len(r.forceRestarts))
	maps.Copy(forceRestarts, r.forceRestarts)
	// Drain current restart requests; requests arriving during reconcile are queued for next cycle.
	r.forceRestarts = make(map[string]struct{}, len(r.forceRestarts))
	r.mu.Unlock()

	// Get actual workers from hypervisor manager (SSoT)
	actual := r.manager.ListWorkers()
	actualMap := make(map[string]*api.WorkerInfo, len(actual))
	for _, w := range actual {
		actualMap[w.WorkerUID] = w
	}

	var added, removed, updated int
	retryRestarts := make(map[string]struct{})

	// 1. Find workers to start (in desired but not in actual)
	for workerID, desiredInfo := range desired {
		actualWorker, exists := actualMap[workerID]
		_, forceRestart := forceRestarts[workerID]
		if !exists {
			// Worker doesn't exist, start it
			if err := r.startWorker(desiredInfo); err != nil {
				klog.Errorf("Failed to start worker: worker_id=%s error=%v", workerID, err)
				if forceRestart {
					retryRestarts[workerID] = struct{}{}
				}
			} else {
				added++
			}
		} else if forceRestart || r.needsRestart(desiredInfo, actualWorker) {
			if forceRestart {
				klog.Infof("Force restart requested for worker: worker_id=%s", workerID)
			}
			// Structural change (GPU allocation, executable, args) requires restart
			if err := r.restartWorker(desiredInfo); err != nil {
				klog.Errorf("Failed to restart worker: worker_id=%s error=%v", workerID, err)
				if forceRestart {
					retryRestarts[workerID] = struct{}{}
				}
			} else {
				updated++
			}
		} else if r.needsEnvUpdate(desiredInfo, actualWorker) {
			// Env-only change: update in place without restarting the running process.
			// New env vars take effect on next process restart (crash recovery).
			if err := r.manager.UpdateWorkerEnv(workerID, desiredInfo.WorkerRunningInfo.Env); err != nil {
				klog.Errorf("Failed to update worker env: worker_id=%s error=%v", workerID, err)
			} else {
				updated++
			}
		}
	}

	// 2. Find workers to stop (in actual but not in desired)
	for workerID := range actualMap {
		if _, exists := desired[workerID]; !exists {
			if err := r.stopWorker(workerID); err != nil {
				klog.Errorf("Failed to stop orphan worker: worker_id=%s error=%v", workerID, err)
			} else {
				removed++
			}
		}
	}

	if len(retryRestarts) > 0 {
		r.mu.Lock()
		for workerID := range retryRestarts {
			r.forceRestarts[workerID] = struct{}{}
		}
		r.mu.Unlock()
	}

	if added > 0 || removed > 0 || updated > 0 {
		klog.Infof("Reconciliation complete: added=%d removed=%d updated=%d", added, removed, updated)

		if r.onReconcileComplete != nil {
			r.onReconcileComplete(added, removed, updated)
		}
	}
}

func (r *Reconciler) startWorker(info *api.WorkerInfo) error {
	if err := r.manager.StartWorker(info); err != nil {
		return err
	}

	if r.onWorkerStarted != nil {
		r.onWorkerStarted(info.WorkerUID)
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

func (r *Reconciler) restartWorker(info *api.WorkerInfo) error {
	// Stop first
	if err := r.stopWorker(info.WorkerUID); err != nil {
		klog.Warningf("Failed to stop worker during restart: worker_id=%s error=%v", info.WorkerUID, err)
	}

	// Small delay to ensure cleanup
	time.Sleep(100 * time.Millisecond)

	// Start with new config
	return r.startWorker(info)
}

// needsRestart checks if structural config changed (GPU allocation, executable, args)
// that requires stopping and restarting the worker process.
func (r *Reconciler) needsRestart(desired, actual *api.WorkerInfo) bool {
	// Check if GPU allocation changed
	if len(desired.AllocatedDevices) != len(actual.AllocatedDevices) {
		return true
	}

	// Create set of desired GPUs
	desiredGPUs := make(map[string]bool, len(desired.AllocatedDevices))
	for _, gpuID := range desired.AllocatedDevices {
		desiredGPUs[gpuID] = true
	}

	// Check if all actual GPUs are in desired set
	for _, gpuID := range actual.AllocatedDevices {
		if !desiredGPUs[gpuID] {
			return true
		}
	}

	// Check if WorkerRunningInfo changed (port, executable, etc.)
	if desired.WorkerRunningInfo != nil && actual.WorkerRunningInfo != nil {
		if desired.WorkerRunningInfo.Executable != actual.WorkerRunningInfo.Executable {
			return true
		}
		// Check args (specifically the port)
		if len(desired.WorkerRunningInfo.Args) != len(actual.WorkerRunningInfo.Args) {
			return true
		}
		for i, arg := range desired.WorkerRunningInfo.Args {
			if i < len(actual.WorkerRunningInfo.Args) && arg != actual.WorkerRunningInfo.Args[i] {
				return true
			}
		}
	} else if desired.WorkerRunningInfo != nil || actual.WorkerRunningInfo != nil {
		// One is nil, the other is not - they differ
		return true
	}

	return false
}

// needsEnvUpdate checks if only environment variables changed.
// Env changes are applied in place without restarting the running process;
// the new values take effect on next process restart (crash recovery).
func (r *Reconciler) needsEnvUpdate(desired, actual *api.WorkerInfo) bool {
	if desired.WorkerRunningInfo != nil && actual.WorkerRunningInfo != nil {
		if !maps.Equal(desired.WorkerRunningInfo.Env, actual.WorkerRunningInfo.Env) {
			return true
		}
	}
	return false
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

	for workerID := range r.desiredWorkers {
		if _, exists := actualMap[workerID]; !exists {
			return false
		}
	}

	return len(r.desiredWorkers) == len(actual)
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
