package agent

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// Result represents the return code from accelerator library functions
type Result int32

const (
	ResultSuccess                Result = 0
	ResultErrorInvalidParam      Result = 1
	ResultErrorNotFound          Result = 2
	ResultErrorNotSupported      Result = 3
	ResultErrorResourceExhausted Result = 4
	ResultErrorOperationFailed   Result = 5
	ResultErrorInternal          Result = 6
)

// VirtualizationCapabilities represents virtualization capabilities
type VirtualizationCapabilities struct {
	SupportsPartitioning  bool
	SupportsSoftIsolation bool
	SupportsHardIsolation bool
	SupportsSnapshot      bool
	SupportsMetrics       bool
	SupportsRemoting      bool
	MaxPartitions         uint32
	MaxWorkersPerDevice   uint32
}

// DeviceBasicInfo matches the C struct DeviceBasicInfo
type DeviceBasicInfo struct {
	UUID              [64]byte
	Vendor            [32]byte
	Model             [128]byte
	DriverVersion     [64]byte
	FirmwareVersion   [64]byte
	Index             int32
	NUMANode          int32
	TotalMemoryBytes  uint64
	TotalComputeUnits uint64
	MaxTflops         float64
	PCIeGen           uint32
	PCIeWidth         uint32
}

// DevicePropertyKV represents a key-value property
type DevicePropertyKV struct {
	Key   [64]byte
	Value [256]byte
}

const MaxDeviceProperties = 64

// DeviceProperties contains device properties
type DeviceProperties struct {
	Properties [MaxDeviceProperties]DevicePropertyKV
	Count      uintptr
}

// ExtendedDeviceInfo contains full device information
type ExtendedDeviceInfo struct {
	Basic        DeviceBasicInfo
	Props        DeviceProperties
	Capabilities VirtualizationCapabilities
}

// ExtraMetric represents an extra metric
type ExtraMetric struct {
	Key   [64]byte
	Value float64
}

const MaxExtraMetrics = 64

// DeviceMetrics contains device metrics
type DeviceMetrics struct {
	DeviceUUID         [64]byte
	PowerUsageWatts    float64
	TemperatureCelsius float64
	PCIeRxBytes        uint64
	PCIeTxBytes        uint64
	UtilizationPercent uint32
	MemoryUsedBytes    uint64
	ExtraMetrics       [MaxExtraMetrics]ExtraMetric
	ExtraMetricsCount  uintptr
}

// Function pointers for purego
var (
	libHandle      uintptr
	virtualGPUInit func() Result
	getDeviceCount func(*uintptr) Result
	getAllDevices  func(*ExtendedDeviceInfo, uintptr, *uintptr) Result
	getDeviceMetricsFunc func(**byte, uintptr, *DeviceMetrics) Result
)

// AcceleratorInterface provides Go bindings for the C accelerator library using purego
type AcceleratorInterface struct {
	libPath string
	mu      sync.RWMutex
	loaded  bool
}

// NewAcceleratorInterface creates a new accelerator interface and loads the library
func NewAcceleratorInterface(libPath string) (*AcceleratorInterface, error) {
	accel := &AcceleratorInterface{
		libPath: libPath,
		loaded:  false,
	}

	if err := accel.Load(); err != nil {
		return nil, fmt.Errorf("failed to load accelerator library from %s: %w", libPath, err)
	}

	return accel, nil
}

// Load loads the accelerator library dynamically using purego
func (a *AcceleratorInterface) Load() error {
	if a.libPath == "" {
		return fmt.Errorf("library path is empty")
	}

	handle, err := purego.Dlopen(a.libPath, purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return fmt.Errorf("failed to open library: %w", err)
	}
	libHandle = handle

	// Register required functions
	purego.RegisterLibFunc(&virtualGPUInit, handle, "VirtualGPUInit")
	purego.RegisterLibFunc(&getDeviceCount, handle, "GetDeviceCount")
	purego.RegisterLibFunc(&getAllDevices, handle, "GetAllDevices")
	purego.RegisterLibFunc(&getDeviceMetricsFunc, handle, "GetDeviceMetrics")

	result := virtualGPUInit()
	if result != ResultSuccess {
		return fmt.Errorf("failed to initialize virtual GPU: %d", result)
	}

	a.loaded = true
	return nil
}

// Close unloads the accelerator library
func (a *AcceleratorInterface) Close() error {
	if a.loaded && libHandle != 0 {
		a.loaded = false
	}
	return nil
}

// byteArrayToString converts a fixed-size byte array to Go string
func byteArrayToString(arr []byte) string {
	for i, b := range arr {
		if b == 0 {
			return string(arr[:i])
		}
	}
	return string(arr)
}

// DeviceInfo represents discovered GPU device information
type DeviceInfo struct {
	UUID             string
	Vendor           string
	Model            string
	Index            int32
	NUMANode         int32
	TotalMemoryBytes uint64
	MaxTflops        float64
	Properties       map[string]string
}

// GPUUsageMetrics represents GPU device metrics
type GPUUsageMetrics struct {
	DeviceUUID        string
	MemoryBytes       uint64
	MemoryPercentage  float64
	ComputePercentage float64
	ComputeTflops     float64
	Rx                float64
	Tx                float64
	Temperature       float64
	PowerUsage        int64
	ExtraMetrics      map[string]float64
}

// GetAllDevices retrieves all available devices from the accelerator library
func (a *AcceleratorInterface) GetAllDevices() ([]*DeviceInfo, error) {
	var cDeviceCount uintptr
	result := getDeviceCount(&cDeviceCount)
	if result != ResultSuccess {
		return nil, fmt.Errorf("failed to get device count: %d", result)
	}

	if cDeviceCount == 0 {
		return []*DeviceInfo{}, nil
	}

	const maxStackDevices = 64
	var stackDevices [maxStackDevices]ExtendedDeviceInfo
	maxDevices := min(int(cDeviceCount), maxStackDevices)

	var cCount uintptr
	result = getAllDevices(&stackDevices[0], uintptr(maxDevices), &cCount)
	if result != ResultSuccess {
		return nil, fmt.Errorf("failed to get all devices: %d", result)
	}

	if cCount == 0 {
		return []*DeviceInfo{}, nil
	}

	devices := make([]*DeviceInfo, int(cCount))

	for i := 0; i < int(cCount); i++ {
		cInfo := &stackDevices[i]

		properties := make(map[string]string, int(cInfo.Props.Count))
		for j := 0; j < int(cInfo.Props.Count) && j < MaxDeviceProperties; j++ {
			key := byteArrayToString(cInfo.Props.Properties[j].Key[:])
			value := byteArrayToString(cInfo.Props.Properties[j].Value[:])
			if key != "" {
				properties[key] = value
			}
		}

		devices[i] = &DeviceInfo{
			UUID:             byteArrayToString(cInfo.Basic.UUID[:]),
			Vendor:           byteArrayToString(cInfo.Basic.Vendor[:]),
			Model:            byteArrayToString(cInfo.Basic.Model[:]),
			Index:            cInfo.Basic.Index,
			NUMANode:         cInfo.Basic.NUMANode,
			TotalMemoryBytes: cInfo.Basic.TotalMemoryBytes,
			MaxTflops:        cInfo.Basic.MaxTflops,
			Properties:       properties,
		}
	}
	return devices, nil
}

// GetDeviceMetrics retrieves device metrics for the specified device UUIDs
func (a *AcceleratorInterface) GetDeviceMetrics(deviceUUIDs []string) ([]*GPUUsageMetrics, error) {
	if len(deviceUUIDs) == 0 {
		return []*GPUUsageMetrics{}, nil
	}

	const maxStackDevices = 64
	deviceCount := min(len(deviceUUIDs), maxStackDevices)

	cStrings := make([]*byte, deviceCount)
	cStringData := make([][]byte, deviceCount)
	for i := range deviceCount {
		cStringData[i] = []byte(deviceUUIDs[i])
		cStringData[i] = append(cStringData[i], 0)
		cStrings[i] = &cStringData[i][0]
	}

	var cMetrics [maxStackDevices]DeviceMetrics

	result := getDeviceMetricsFunc(&cStrings[0], uintptr(deviceCount), &cMetrics[0])
	if result != ResultSuccess {
		return nil, fmt.Errorf("failed to get device metrics: %d", result)
	}

	metrics := make([]*GPUUsageMetrics, deviceCount)
	for i := range deviceCount {
		cm := &cMetrics[i]
		memoryUsed := cm.MemoryUsedBytes

		extraMetrics := make(map[string]float64, int(cm.ExtraMetricsCount))
		for j := 0; j < int(cm.ExtraMetricsCount); j++ {
			em := &cm.ExtraMetrics[j]
			key := byteArrayToString(em.Key[:])
			if key != "" {
				extraMetrics[key] = em.Value
			}
		}

		metrics[i] = &GPUUsageMetrics{
			DeviceUUID:        byteArrayToString(cm.DeviceUUID[:]),
			MemoryBytes:       memoryUsed,
			ComputePercentage: float64(cm.UtilizationPercent),
			Rx:                float64(cm.PCIeRxBytes),
			Tx:                float64(cm.PCIeTxBytes),
			Temperature:       cm.TemperatureCelsius,
			PowerUsage:        int64(cm.PowerUsageWatts),
			ExtraMetrics:      extraMetrics,
		}
	}

	return metrics, nil
}

// cStringToGoString converts a C string (null-terminated byte array) to Go string
func cStringToGoString(cstr *byte) string {
	if cstr == nil {
		return ""
	}
	ptr := unsafe.Pointer(cstr)
	length := 0
	for *(*byte)(unsafe.Add(ptr, uintptr(length))) != 0 {
		length++
	}
	return string(unsafe.Slice(cstr, length))
}
