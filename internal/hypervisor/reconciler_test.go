package hypervisor

import (
	"sync"
	"testing"
	"time"

	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/api"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

// MockManager implements a mock hypervisor manager for testing reconciler
type MockManager struct {
	mu            sync.RWMutex
	workers       map[string]*api.WorkerInfo
	devices       []*api.DeviceInfo
	startedCount  int
	stoppedCount  int
	startErr      error
	stopErr       error
	listDeviceErr error
}

func NewMockManager() *MockManager {
	return &MockManager{
		workers: make(map[string]*api.WorkerInfo),
		devices: []*api.DeviceInfo{
			{UUID: "gpu-0", Vendor: "nvidia", Model: "RTX 4090", TotalMemoryBytes: 24 * 1024 * 1024 * 1024},
			{UUID: "gpu-1", Vendor: "nvidia", Model: "RTX 4090", TotalMemoryBytes: 24 * 1024 * 1024 * 1024},
		},
	}
}

func (m *MockManager) ListWorkers() []*api.WorkerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	workers := make([]*api.WorkerInfo, 0, len(m.workers))
	for _, w := range m.workers {
		workers = append(workers, w)
	}
	return workers
}

func (m *MockManager) StartWorker(workerInfo *api.WorkerInfo) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	m.workers[workerInfo.WorkerUID] = workerInfo
	m.startedCount++
	return nil
}

func (m *MockManager) StopWorker(workerUID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopErr != nil {
		return m.stopErr
	}
	delete(m.workers, workerUID)
	m.stoppedCount++
	return nil
}

func (m *MockManager) ListDevices() ([]*api.DeviceInfo, error) {
	if m.listDeviceErr != nil {
		return nil, m.listDeviceErr
	}
	return m.devices, nil
}

func (m *MockManager) IsStarted() bool {
	return true
}

func (m *MockManager) GetVendor() string {
	return "mock"
}

func (m *MockManager) GetStateDir() string {
	return "/tmp/test-state"
}

// testReconciler wraps Reconciler with a mock manager
type testReconciler struct {
	*Reconciler
	mockMgr *MockManager
}

func newTestReconciler() *testReconciler {
	mockMgr := NewMockManager()

	// Create a custom reconciler that uses the mock manager
	r := &testReconciler{
		mockMgr: mockMgr,
	}

	r.Reconciler = &Reconciler{
		log:             zerolog.Nop(),
		reconcileSignal: make(chan struct{}, 1),
		desiredWorkers:  make(map[string]*WorkerSpec),
	}

	return r
}

// Override reconcile to use mock manager
func (r *testReconciler) reconcileWithMock() (added, removed, updated int) {
	r.mu.RLock()
	desired := make(map[string]*WorkerSpec, len(r.desiredWorkers))
	for k, v := range r.desiredWorkers {
		desired[k] = v
	}
	r.mu.RUnlock()

	// Get actual workers from mock manager
	actual := r.mockMgr.ListWorkers()
	actualMap := make(map[string]*api.WorkerInfo, len(actual))
	for _, w := range actual {
		actualMap[w.WorkerUID] = w
	}

	// 1. Find workers to start (in desired but not in actual, or disabled in actual)
	for workerID, spec := range desired {
		if !spec.Enabled {
			// If worker should be disabled, stop it if running
			if _, exists := actualMap[workerID]; exists {
				r.mockMgr.StopWorker(workerID)
				removed++
			}
			continue
		}

		_, exists := actualMap[workerID]
		if !exists {
			// Worker doesn't exist, start it
			workerInfo := &api.WorkerInfo{
				WorkerUID:        spec.WorkerID,
				AllocatedDevices: spec.GPUIDs,
				Status:           api.WorkerStatusPending,
				IsolationMode:    spec.IsolationMode,
			}
			r.mockMgr.StartWorker(workerInfo)
			added++
		}
	}

	// 2. Find workers to stop (in actual but not in desired)
	for workerID := range actualMap {
		if _, exists := desired[workerID]; !exists {
			r.mockMgr.StopWorker(workerID)
			removed++
		}
	}

	return added, removed, updated
}

func TestReconciler_AddWorkers(t *testing.T) {
	r := newTestReconciler()

	// Set desired workers
	r.SetDesiredWorkers([]WorkerSpec{
		{WorkerID: "worker-1", GPUIDs: []string{"gpu-0"}, Enabled: true},
		{WorkerID: "worker-2", GPUIDs: []string{"gpu-1"}, Enabled: true},
	})

	// Run reconciliation
	added, removed, _ := r.reconcileWithMock()

	assert.Equal(t, 2, added, "should add 2 workers")
	assert.Equal(t, 0, removed, "should remove 0 workers")
	assert.Equal(t, 2, len(r.mockMgr.workers), "should have 2 workers")
}

func TestReconciler_RemoveWorkers(t *testing.T) {
	r := newTestReconciler()

	// Pre-populate with workers
	r.mockMgr.workers["worker-1"] = &api.WorkerInfo{WorkerUID: "worker-1"}
	r.mockMgr.workers["worker-2"] = &api.WorkerInfo{WorkerUID: "worker-2"}

	// Set desired workers (only worker-1)
	r.SetDesiredWorkers([]WorkerSpec{
		{WorkerID: "worker-1", GPUIDs: []string{"gpu-0"}, Enabled: true},
	})

	// Run reconciliation
	added, removed, _ := r.reconcileWithMock()

	assert.Equal(t, 0, added, "should add 0 workers")
	assert.Equal(t, 1, removed, "should remove 1 worker")
	assert.Equal(t, 1, len(r.mockMgr.workers), "should have 1 worker")
	assert.Contains(t, r.mockMgr.workers, "worker-1", "worker-1 should remain")
}

func TestReconciler_DisabledWorkers(t *testing.T) {
	r := newTestReconciler()

	// Pre-populate with a running worker
	r.mockMgr.workers["worker-1"] = &api.WorkerInfo{WorkerUID: "worker-1"}

	// Set desired workers with worker-1 disabled
	r.SetDesiredWorkers([]WorkerSpec{
		{WorkerID: "worker-1", GPUIDs: []string{"gpu-0"}, Enabled: false},
	})

	// Run reconciliation
	_, removed, _ := r.reconcileWithMock()

	assert.Equal(t, 1, removed, "should remove 1 disabled worker")
	assert.Equal(t, 0, len(r.mockMgr.workers), "should have 0 workers")
}

func TestReconciler_MixedOperations(t *testing.T) {
	r := newTestReconciler()

	// Pre-populate with workers
	r.mockMgr.workers["worker-1"] = &api.WorkerInfo{WorkerUID: "worker-1"}
	r.mockMgr.workers["worker-2"] = &api.WorkerInfo{WorkerUID: "worker-2"}
	r.mockMgr.workers["worker-3"] = &api.WorkerInfo{WorkerUID: "worker-3"}

	// Set desired workers:
	// - worker-1: keep
	// - worker-2: remove
	// - worker-3: disable
	// - worker-4: add
	r.SetDesiredWorkers([]WorkerSpec{
		{WorkerID: "worker-1", GPUIDs: []string{"gpu-0"}, Enabled: true},
		{WorkerID: "worker-3", GPUIDs: []string{"gpu-0"}, Enabled: false},
		{WorkerID: "worker-4", GPUIDs: []string{"gpu-1"}, Enabled: true},
	})

	// Run reconciliation
	added, removed, _ := r.reconcileWithMock()

	assert.Equal(t, 1, added, "should add 1 worker (worker-4)")
	assert.Equal(t, 2, removed, "should remove 2 workers (worker-2 and worker-3)")
	assert.Equal(t, 2, len(r.mockMgr.workers), "should have 2 workers")
	assert.Contains(t, r.mockMgr.workers, "worker-1", "worker-1 should remain")
	assert.Contains(t, r.mockMgr.workers, "worker-4", "worker-4 should be added")
}

func TestReconciler_NoChanges(t *testing.T) {
	r := newTestReconciler()

	// Pre-populate with matching workers
	r.mockMgr.workers["worker-1"] = &api.WorkerInfo{WorkerUID: "worker-1", AllocatedDevices: []string{"gpu-0"}}

	// Set same desired workers
	r.SetDesiredWorkers([]WorkerSpec{
		{WorkerID: "worker-1", GPUIDs: []string{"gpu-0"}, Enabled: true},
	})

	// Run reconciliation
	added, removed, _ := r.reconcileWithMock()

	assert.Equal(t, 0, added, "should add 0 workers")
	assert.Equal(t, 0, removed, "should remove 0 workers")
	assert.Equal(t, 1, len(r.mockMgr.workers), "should have 1 worker")
}

func TestReconcilerStatus(t *testing.T) {
	r := newTestReconciler()

	// Set desired workers
	r.SetDesiredWorkers([]WorkerSpec{
		{WorkerID: "worker-1", GPUIDs: []string{"gpu-0"}, Enabled: true},
		{WorkerID: "worker-2", GPUIDs: []string{"gpu-1"}, Enabled: true},
	})

	// Before reconciliation - use mock status
	status := r.getStatusWithMock()
	assert.Equal(t, 2, status.DesiredCount, "should have 2 desired workers")
	assert.Equal(t, 0, status.ActualCount, "should have 0 actual workers before reconcile")
	assert.False(t, status.InSync, "should not be in sync before reconcile")
}

func (r *testReconciler) getStatusWithMock() ReconcilerStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	actual := r.mockMgr.ListWorkers()

	return ReconcilerStatus{
		DesiredCount: len(r.desiredWorkers),
		ActualCount:  len(actual),
		InSync:       r.isInSyncWithMock(actual),
	}
}

func (r *testReconciler) isInSyncWithMock(actual []*api.WorkerInfo) bool {
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

func TestReconciler_TriggerReconcile(t *testing.T) {
	r := newTestReconciler()

	// Test that TriggerReconcile sends signal to channel
	r.TriggerReconcile()

	select {
	case <-r.reconcileSignal:
		// Signal received as expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("TriggerReconcile should send signal to channel")
	}
}

func TestReconcilerStatus_String(t *testing.T) {
	tests := []struct {
		name     string
		status   ReconcilerStatus
		expected string
	}{
		{
			name:     "in sync",
			status:   ReconcilerStatus{DesiredCount: 2, ActualCount: 2, InSync: true},
			expected: "desired=2 actual=2 in-sync",
		},
		{
			name:     "out of sync",
			status:   ReconcilerStatus{DesiredCount: 3, ActualCount: 1, InSync: false},
			expected: "desired=3 actual=1 out-of-sync",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.status.String())
		})
	}
}
