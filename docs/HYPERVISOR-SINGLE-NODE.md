# Hypervisor Single Node Module Documentation

This document describes the Tensor Fusion Hypervisor's single_node module and how gpu-go integrates with it for GPU discovery and worker management.

## Overview

The Hypervisor module from `github.com/NexusGPU/tensor-fusion` provides:
- **GPU Device Discovery**: Using `purego` to call native accelerator libraries (NVML for NVIDIA, ROCm for AMD, etc.)
- **Worker Management**: Tracking worker processes and their allocated GPU devices
- **File-based State**: Single node mode uses JSON files for state persistence instead of Kubernetes

## Module Structure

```
internal/hypervisor/
├── api/                          # Data types and interfaces
│   ├── device_types.go           # DeviceInfo, GPUUsageMetrics, ProcessInformation
│   └── worker_types.go           # WorkerInfo, WorkerAllocation, WorkerStatus
├── backend/
│   └── single_node/              # Single node mode (non-Kubernetes)
│       ├── single_node_backend.go  # Backend implementation
│       └── filestate.go            # File-based state persistence
├── device/
│   ├── accelerator.go            # purego FFI bindings to native accelerator lib
│   └── controller.go             # Device discovery and lifecycle management
├── framework/
│   └── framework.go              # Interfaces: DeviceController, Backend, etc.
└── worker/
    ├── controller.go             # Worker controller
    └── allocation.go             # Device allocation logic
```

## Key Types

### DeviceInfo (api/device_types.go)

```go
type DeviceInfo struct {
    UUID                       string
    Vendor                     string
    Model                      string
    Index                      int32
    NUMANode                   int32
    TotalMemoryBytes           uint64
    MaxTflops                  float64
    VirtualizationCapabilities VirtualizationCapabilities
    Properties                 map[string]string
    Healthy                    bool
    ParentUUID                 string              // For partitioned devices
    DeviceNode                 map[string]string   // Host -> Guest device node mapping
    DeviceEnv                  map[string]string   // Env to inject to guest
    IsolationMode              IsolationMode
}
```

### WorkerInfo (api/worker_types.go)

```go
type WorkerInfo struct {
    WorkerUID        string
    Namespace        string
    WorkerName       string
    AllocatedDevices []string
    Status           WorkerStatus  // Pending, DeviceAllocating, Running, Terminated
    QoS              tfv1.QoSLevel
    IsolationMode    IsolationMode
    Requests         tfv1.Resource
    Limits           tfv1.Resource
    PartitionTemplateID string
    Labels           map[string]string
    Annotations      map[string]string
    DeletedAt        int64
}

type WorkerStatus string
const (
    WorkerStatusPending          WorkerStatus = "Pending"
    WorkerStatusDeviceAllocating WorkerStatus = "DeviceAllocating"
    WorkerStatusRunning          WorkerStatus = "Running"
    WorkerStatusTerminated       WorkerStatus = "Terminated"
)
```

### GPUUsageMetrics (api/device_types.go)

```go
type GPUUsageMetrics struct {
    DeviceUUID        string
    MemoryBytes       uint64
    MemoryPercentage  float64
    ComputePercentage float64
    ComputeTflops     float64
    Rx                float64     // PCIe RX in KB
    Tx                float64     // PCIe TX in KB
    Temperature       float64
    PowerUsage        int64       // in watts
    ExtraMetrics      map[string]float64
}
```

## Core Interfaces (framework/framework.go)

### DeviceController

```go
type DeviceController interface {
    Start() error
    Stop() error
    DiscoverDevices() error
    ListDevices() ([]*api.DeviceInfo, error)
    GetDevice(deviceUUID string) (*api.DeviceInfo, bool)
    SplitDevice(deviceUUID string, partitionID string) (*api.DeviceInfo, error)
    RemovePartitionedDevice(partitionUUID, deviceUUID string) error
    GetDeviceMetrics() (map[string]*api.GPUUsageMetrics, error)
    GetProcessInformation() ([]api.ProcessInformation, error)
    GetVendorMountLibs() ([]*api.Mount, error)
    RegisterDeviceUpdateHandler(handler DeviceChangeHandler)
    GetAcceleratorVendor() string
}
```

### Backend (Single Node)

```go
type Backend interface {
    Start() error
    Stop() error
    RegisterWorkerUpdateHandler(handler WorkerChangeHandler) error
    StartWorker(worker *api.WorkerInfo) error
    StopWorker(workerUID string) error
    GetProcessMappingInfo(hostPID uint32) (*ProcessMappingInfo, error)
    GetDeviceChangeHandler() DeviceChangeHandler
    ListWorkers() []*api.WorkerInfo
}
```

### Change Handlers

```go
type DeviceChangeHandler struct {
    OnAdd               func(device *api.DeviceInfo)
    OnRemove            func(device *api.DeviceInfo)
    OnUpdate            func(oldDevice, newDevice *api.DeviceInfo)
    OnDiscoveryComplete func(nodeInfo *api.NodeInfo)
}

type WorkerChangeHandler struct {
    OnAdd    func(worker *api.WorkerInfo)
    OnRemove func(worker *api.WorkerInfo)
    OnUpdate func(oldWorker, newWorker *api.WorkerInfo)
}
```

## AcceleratorInterface (device/accelerator.go)

Uses `purego` to dynamically load and call native GPU libraries. Key methods:

```go
// Create interface with path to native library
accel, err := device.NewAcceleratorInterface(libPath)

// Discover all GPUs
devices, err := accel.GetAllDevices()

// Get GPU metrics
metrics, err := accel.GetDeviceMetrics(deviceUUIDs)

// Get process-level GPU usage
processInfos, err := accel.GetProcessInformation()

// Partition management (for MIG, etc.)
partitionResult, err := accel.AssignPartition(templateID, deviceUUID)
err = accel.RemovePartition(partitionUUID, deviceUUID)

// Set resource limits
err = accel.SetMemHardLimit(workerID, deviceUUID, memoryLimitBytes)
err = accel.SetComputeUnitHardLimit(workerID, deviceUUID, computeUnitLimit)

// Get mount paths for vendor libraries
mounts, err := accel.GetVendorMountLibs()
```

### C API Signatures (accelerator.h)

The native library must export these functions:

```c
Result VirtualGPUInit();
Result GetDeviceCount(size_t* count);
Result GetAllDevices(ExtendedDeviceInfo* devices, size_t maxDevices, size_t* count);
Result GetDeviceTopology(int32_t* deviceIndices, size_t count, ExtendedDeviceTopology* topology);
bool AssignPartition(PartitionAssignment* assignment);
bool RemovePartition(const char* partitionUUID, const char* deviceUUID);
Result SetMemHardLimit(const char* workerID, const char* deviceUUID, uint64_t memoryLimitBytes);
Result SetComputeUnitHardLimit(const char* workerID, const char* deviceUUID, uint32_t computeUnitLimit);
Result Snapshot(ProcessArray* processes);
Result Resume(ProcessArray* processes);
Result GetProcessInformation(ProcessInformation* infos, size_t maxInfos, size_t* count);
Result GetDeviceMetrics(const char** deviceUUIDs, size_t count, DeviceMetrics* metrics);
Result GetVendorMountLibs(Mount* mounts, size_t maxMounts, size_t* count);
Result RegisterLogCallback(LogCallback callback);
```

## Single Node Backend (backend/single_node)

### FileStateManager

Manages JSON-based state persistence:

```go
// State files stored in TENSOR_FUSION_STATE_DIR (default: /tmp/tensor-fusion-state)
const (
    workersFile = "workers.json"
    devicesFile = "devices.json"
)

// Workers JSON format:
[
  {
    "WorkerUID": "worker-1",
    "Namespace": "",
    "WorkerName": "my-worker",
    "AllocatedDevices": ["gpu-uuid-0"],
    "Status": "Running"
  }
]
```

### SingleNodeBackend

```go
// Create backend
backend := single_node.NewSingleNodeBackend(ctx, deviceController, allocationController)

// Start (loads state from files, starts periodic discovery)
err := backend.Start()

// Register handler for worker changes
backend.RegisterWorkerUpdateHandler(framework.WorkerChangeHandler{
    OnAdd:    func(worker *api.WorkerInfo) { /* handle new worker */ },
    OnUpdate: func(old, new *api.WorkerInfo) { /* handle update */ },
    OnRemove: func(worker *api.WorkerInfo) { /* handle removal */ },
})

// Start/stop workers
backend.StartWorker(workerInfo)
backend.StopWorker(workerUID)

// List workers
workers := backend.ListWorkers()
```

## Usage Example (from hypervisor_suite_test.go)

```go
import (
    "github.com/NexusGPU/tensor-fusion/internal/hypervisor/api"
    "github.com/NexusGPU/tensor-fusion/internal/hypervisor/backend/single_node"
    "github.com/NexusGPU/tensor-fusion/internal/hypervisor/device"
    "github.com/NexusGPU/tensor-fusion/internal/hypervisor/framework"
    "github.com/NexusGPU/tensor-fusion/internal/hypervisor/worker"
)

func main() {
    ctx := context.Background()
    
    // 1. Create device controller with accelerator library path
    stubLibPath := "path/to/libaccelerator_example.so"
    deviceController, err := device.NewController(ctx, stubLibPath, "stub", 1*time.Hour, "shared")
    
    // 2. Create allocation controller
    allocationController := worker.NewAllocationController(deviceController)
    deviceController.SetAllocationController(allocationController)
    
    // 3. Create single node backend
    backend := single_node.NewSingleNodeBackend(ctx, deviceController, allocationController)
    
    // 4. Create worker controller
    workerController := worker.NewWorkerController(deviceController, allocationController, "shared", backend)
    
    // 5. Start components
    deviceController.Start()
    backend.Start()
    workerController.Start()
    
    // 6. Wait for device discovery
    devices, _ := deviceController.ListDevices()
    
    // 7. Allocate devices for a worker
    req := &api.WorkerInfo{
        WorkerUID:        "my-worker-1",
        AllocatedDevices: []string{devices[0].UUID},
        IsolationMode:    "soft", // or "partitioned", "hard"
    }
    resp, _ := allocationController.AllocateWorkerDevices(req)
    
    // 8. Start worker in backend
    backend.StartWorker(req)
    
    // 9. Get metrics
    gpuMetrics, _ := deviceController.GetDeviceMetrics()
    
    // 10. Cleanup
    backend.StopWorker("my-worker-1")
    workerController.Stop()
    backend.Stop()
    deviceController.Stop()
}
```

## Integration with gpu-go Agent

The gpu-go agent should:

1. **Device Discovery**: Use `AcceleratorInterface` directly to discover GPUs at registration time
2. **Worker Management**: Use file-based state to communicate with workers
3. **Reconcile Loop**: Watch workers.json for changes and start/stop worker processes

### Key Integration Points

```go
// In cmd/ggo/agent/agent.go - discoverGPUs()
func discoverGPUs(libPath string) ([]api.GPUInfo, error) {
    accel, err := device.NewAcceleratorInterface(libPath)
    if err != nil {
        return nil, err
    }
    defer accel.Close()
    
    devices, err := accel.GetAllDevices()
    if err != nil {
        return nil, err
    }
    
    gpuInfos := make([]api.GPUInfo, len(devices))
    for i, d := range devices {
        gpuInfos[i] = api.GPUInfo{
            GPUID:  d.UUID,
            Vendor: d.Vendor,
            Model:  d.Model,
            VRAMMb: int64(d.TotalMemoryBytes / (1024 * 1024)),
        }
    }
    return gpuInfos, nil
}

// In internal/agent/agent.go - reconcileWorkers()
// Read workers.json, compare with running processes, start/stop as needed
// Worker command: ./tensor-fusion-worker -p <port>
```

## Environment Variables

- `TENSOR_FUSION_STATE_DIR`: State directory for workers.json and devices.json (default: `/tmp/tensor-fusion-state`)
- `NVIDIA_VISIBLE_DEVICES`: GPU visibility for worker processes
- `WORKER_ID`: Worker identifier passed to worker process

## Accelerator Library Paths

| Vendor | Library Name | Typical Location |
|--------|-------------|------------------|
| NVIDIA | libaccelerator_nvidia.so | /usr/lib/tensor-fusion/ |
| AMD | libaccelerator_amd.so | /usr/lib/tensor-fusion/ |
| Example (Stub) | libaccelerator_example.so | provider/build/ |

The example/stub library is useful for testing on systems without real GPUs.

## Known Issues

### Example Library Stack Overflow (macOS)

The example accelerator library (`libaccelerator_example.so`) has a bug where the limiter thread allocates a large array on the stack (~5MB), causing a stack overflow when loaded on macOS. This is due to:

```c
// In limiterThreadFunc():
ExtendedDeviceInfo devices[256]; // ~20KB per element * 256 = ~5MB stack allocation
```

**Workaround**: Use mock GPUs for testing instead:
```bash
GPU_GO_MOCK_GPUS=2 ./ggo agent register --token xxx
```

The real NVIDIA/AMD accelerator libraries do not have this issue.

## Testing Without Real GPUs

To test the agent without real GPUs or accelerator libraries:

```bash
# Set mock GPUs environment variable (number = GPU count)
export GPU_GO_MOCK_GPUS=2

# Register with mock GPUs
./ggo agent register --token <token>

# Start agent in single-node mode (manages workers locally)
./ggo agent start --single-node --worker-binary /path/to/tensor-fusion-worker
```

The mock GPUs will create fake NVIDIA RTX 4090 devices for testing purposes.
