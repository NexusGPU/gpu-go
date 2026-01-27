package hypervisor

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	tfv1 "github.com/NexusGPU/tensor-fusion/api/v1"
	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/api"
	"github.com/NexusGPU/tensor-fusion/pkg/hypervisor/framework"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getExampleLibPath() string {
	// Known issue: example library causes bus error on macOS ARM64
	// See docs/HYPERVISOR-SINGLE-NODE.md for details
	if runtime.GOOS == "darwin" {
		return "" // Skip on macOS
	}

	suffix := ".so"

	// Try multiple paths for the example accelerator library
	paths := []string{
		"/Users/joeyyang/Code/tensor-fusion/tensor-fusion-operator/provider/build/libaccelerator_example" + suffix,
		os.Getenv("TENSOR_FUSION_OPERATOR_PATH") + "/provider/build/libaccelerator_example" + suffix,
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func TestManager_E2E_DeviceDiscovery(t *testing.T) {
	libPath := getExampleLibPath()
	if libPath == "" {
		t.Skip("Example accelerator library not found")
	}

	tmpDir := t.TempDir()

	mgr, err := NewManager(Config{
		LibPath:       libPath,
		Vendor:        "stub",
		IsolationMode: tfv1.IsolationModeShared,
		StateDir:      tmpDir,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)
	require.NotNil(t, mgr)

	// Start manager
	err = mgr.Start()
	require.NoError(t, err)

	// Wait for device discovery
	time.Sleep(500 * time.Millisecond)

	// List devices
	devices, err := mgr.ListDevices()
	require.NoError(t, err)
	require.NotEmpty(t, devices, "should discover at least one device")

	t.Logf("Discovered %d devices", len(devices))
	for _, dev := range devices {
		t.Logf("  Device: %s, Vendor: %s, Model: %s, Memory: %d MB",
			dev.UUID, dev.Vendor, dev.Model, dev.TotalMemoryBytes/(1024*1024))
		assert.NotEmpty(t, dev.UUID)
		assert.NotEmpty(t, dev.Vendor)
	}

	// Get device metrics
	metrics, err := mgr.GetDeviceMetrics()
	require.NoError(t, err)
	assert.NotEmpty(t, metrics)

	// Stop manager
	err = mgr.Stop()
	require.NoError(t, err)
}

func TestManager_E2E_WorkerLifecycle(t *testing.T) {
	libPath := getExampleLibPath()
	if libPath == "" {
		t.Skip("Example accelerator library not found")
	}

	tmpDir := t.TempDir()

	mgr, err := NewManager(Config{
		LibPath:       libPath,
		Vendor:        "stub",
		IsolationMode: tfv1.IsolationModeShared,
		StateDir:      tmpDir,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)

	err = mgr.Start()
	require.NoError(t, err)
	defer mgr.Stop()

	// Wait for device discovery
	time.Sleep(500 * time.Millisecond)

	// Get a device UUID
	devices, err := mgr.ListDevices()
	require.NoError(t, err)
	require.NotEmpty(t, devices)

	deviceUUID := devices[0].UUID

	// Start a worker
	workerInfo := &api.WorkerInfo{
		WorkerUID:        "test-worker-1",
		WorkerName:       "Test Worker",
		AllocatedDevices: []string{deviceUUID},
		Status:           api.WorkerStatusPending,
		IsolationMode:    tfv1.IsolationModeShared,
	}

	err = mgr.StartWorker(workerInfo)
	require.NoError(t, err)

	// Verify worker is listed
	workers := mgr.ListWorkers()
	assert.Len(t, workers, 1)
	assert.Equal(t, "test-worker-1", workers[0].WorkerUID)

	// Verify allocation
	alloc, found := mgr.GetWorkerAllocation("test-worker-1")
	assert.True(t, found)
	assert.NotNil(t, alloc)

	// Stop the worker
	err = mgr.StopWorker("test-worker-1")
	require.NoError(t, err)

	// Verify worker removed
	workers = mgr.ListWorkers()
	assert.Empty(t, workers)
}

func TestManager_E2E_MultipleWorkers(t *testing.T) {
	libPath := getExampleLibPath()
	if libPath == "" {
		t.Skip("Example accelerator library not found")
	}

	tmpDir := t.TempDir()

	mgr, err := NewManager(Config{
		LibPath:       libPath,
		Vendor:        "stub",
		IsolationMode: tfv1.IsolationModeShared,
		StateDir:      tmpDir,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)

	err = mgr.Start()
	require.NoError(t, err)
	defer mgr.Stop()

	time.Sleep(500 * time.Millisecond)

	devices, err := mgr.ListDevices()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(devices), 2, "need at least 2 devices for this test")

	// Start multiple workers
	for i, dev := range devices[:2] {
		workerInfo := &api.WorkerInfo{
			WorkerUID:        "worker-" + string(rune('a'+i)),
			AllocatedDevices: []string{dev.UUID},
			IsolationMode:    tfv1.IsolationModeShared,
		}
		err = mgr.StartWorker(workerInfo)
		require.NoError(t, err)
	}

	workers := mgr.ListWorkers()
	assert.Len(t, workers, 2)

	// Stop all workers
	for _, w := range workers {
		err = mgr.StopWorker(w.WorkerUID)
		require.NoError(t, err)
	}

	workers = mgr.ListWorkers()
	assert.Empty(t, workers)
}

func TestManager_E2E_Reconciler(t *testing.T) {
	libPath := getExampleLibPath()
	if libPath == "" {
		t.Skip("Example accelerator library not found")
	}

	tmpDir := t.TempDir()

	mgr, err := NewManager(Config{
		LibPath:       libPath,
		Vendor:        "stub",
		IsolationMode: tfv1.IsolationModeShared,
		StateDir:      tmpDir,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)

	err = mgr.Start()
	require.NoError(t, err)
	defer mgr.Stop()

	time.Sleep(500 * time.Millisecond)

	devices, err := mgr.ListDevices()
	require.NoError(t, err)
	require.NotEmpty(t, devices)

	// Create reconciler
	var startedWorkers, stoppedWorkers []string
	reconciler := NewReconciler(ReconcilerConfig{
		Manager: mgr,
		Logger:  zerolog.Nop(),
		OnWorkerStarted: func(workerID string) {
			startedWorkers = append(startedWorkers, workerID)
		},
		OnWorkerStopped: func(workerID string) {
			stoppedWorkers = append(stoppedWorkers, workerID)
		},
	})

	reconciler.Start()
	defer reconciler.Stop()

	// Set desired workers
	reconciler.SetDesiredWorkers([]WorkerSpec{
		{WorkerID: "reconciler-worker-1", GPUIDs: []string{devices[0].UUID}, Enabled: true},
	})

	// Wait for reconciliation
	time.Sleep(1 * time.Second)

	assert.Contains(t, startedWorkers, "reconciler-worker-1")

	workers := mgr.ListWorkers()
	assert.Len(t, workers, 1)

	// Disable worker
	reconciler.SetDesiredWorkers([]WorkerSpec{
		{WorkerID: "reconciler-worker-1", GPUIDs: []string{devices[0].UUID}, Enabled: false},
	})

	time.Sleep(1 * time.Second)

	assert.Contains(t, stoppedWorkers, "reconciler-worker-1")

	workers = mgr.ListWorkers()
	assert.Empty(t, workers)
}

func TestManager_StateFilePersistence(t *testing.T) {
	libPath := getExampleLibPath()
	if libPath == "" {
		t.Skip("Example accelerator library not found")
	}

	tmpDir := t.TempDir()

	mgr, err := NewManager(Config{
		LibPath:       libPath,
		Vendor:        "stub",
		IsolationMode: tfv1.IsolationModeShared,
		StateDir:      tmpDir,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)

	err = mgr.Start()
	require.NoError(t, err)

	time.Sleep(500 * time.Millisecond)

	devices, err := mgr.ListDevices()
	require.NoError(t, err)
	require.NotEmpty(t, devices)

	// Start a worker
	workerInfo := &api.WorkerInfo{
		WorkerUID:        "persistent-worker",
		AllocatedDevices: []string{devices[0].UUID},
		IsolationMode:    tfv1.IsolationModeShared,
	}
	err = mgr.StartWorker(workerInfo)
	require.NoError(t, err)

	// Verify state file exists
	workersFile := filepath.Join(tmpDir, "workers.json")
	_, err = os.Stat(workersFile)
	require.NoError(t, err, "workers.json should exist")

	// Stop manager
	err = mgr.Stop()
	require.NoError(t, err)

	// Re-create manager and verify state is loaded
	mgr2, err := NewManager(Config{
		LibPath:       libPath,
		Vendor:        "stub",
		IsolationMode: tfv1.IsolationModeShared,
		StateDir:      tmpDir,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)

	err = mgr2.Start()
	require.NoError(t, err)
	defer mgr2.Stop()

	time.Sleep(500 * time.Millisecond)

	// Worker should be restored
	workers := mgr2.ListWorkers()
	assert.Len(t, workers, 1)
	assert.Equal(t, "persistent-worker", workers[0].WorkerUID)
}

func TestManager_NotStartedErrors(t *testing.T) {
	mgr, err := NewManager(Config{
		LibPath: "/nonexistent/path.so",
		Vendor:  "stub",
		Logger:  zerolog.Nop(),
	})
	require.NoError(t, err)

	// Operations should fail when not started
	_, err = mgr.ListDevices()
	assert.ErrorIs(t, err, ErrNotStarted)

	_, err = mgr.GetDeviceMetrics()
	assert.ErrorIs(t, err, ErrNotStarted)

	err = mgr.StartWorker(&api.WorkerInfo{WorkerUID: "test"})
	assert.ErrorIs(t, err, ErrNotStarted)

	err = mgr.StopWorker("test")
	assert.ErrorIs(t, err, ErrNotStarted)

	err = mgr.RegisterWorkerHandler(framework.WorkerChangeHandler{})
	assert.ErrorIs(t, err, ErrNotStarted)
}

func TestManager_Idempotent(t *testing.T) {
	libPath := getExampleLibPath()
	if libPath == "" {
		t.Skip("Example accelerator library not found")
	}

	tmpDir := t.TempDir()

	mgr, err := NewManager(Config{
		LibPath:       libPath,
		Vendor:        "stub",
		IsolationMode: tfv1.IsolationModeShared,
		StateDir:      tmpDir,
		Logger:        zerolog.Nop(),
	})
	require.NoError(t, err)

	// Start twice should be safe
	err = mgr.Start()
	require.NoError(t, err)
	err = mgr.Start()
	require.NoError(t, err)

	// Stop twice should be safe
	err = mgr.Stop()
	require.NoError(t, err)
	err = mgr.Stop()
	require.NoError(t, err)
}
