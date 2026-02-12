package agent

import (
	"strings"
	"testing"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/stretchr/testify/assert"
)

func TestBuildMetricsLineProtocol_SingleGPU(t *testing.T) {
	gpuMetrics := map[string]*api.GPUMetrics{
		"gpu-001": {
			GPUID:       "gpu-001",
			Utilization: 75.5,
			VRAMUsedMb:  8192,
			Temperature: 72.0,
			PowerUsageW: 250.0,
			PCIeRxKB:    1024.5,
			PCIeTxKB:    512.3,
		},
	}
	gpuConfigs := []api.GPUStatus{
		{GPUID: "gpu-001", Vendor: "nvidia", Model: "RTX 4090", VRAMMb: 24576},
	}
	workers := []api.WorkerStatus{
		{WorkerID: "w-1", Status: "running", Restarts: 2, Connections: []api.ConnectionInfo{{ClientIP: "1.2.3.4"}}},
	}
	sysMetrics := &api.SystemMetrics{CPUUsage: 45.2, MemoryUsedMb: 8000, MemoryTotalMb: 16384}
	ts := int64(1700000000000)

	result := buildMetricsLineProtocol("agent-abc", "my-host", gpuMetrics, gpuConfigs, workers, sysMetrics, ts)

	lines := strings.Split(result, "\n")
	assert.Len(t, lines, 3)

	// GPU line
	assert.Contains(t, lines[0], "gpu_metrics,agent_id=agent-abc,gpu_id=gpu-001,model=RTX\\ 4090,vendor=nvidia")
	assert.Contains(t, lines[0], "utilization=75.50,vram_used_mb=8192i,vram_total_mb=24576i,temperature=72.0,power_usage_w=250.0,pcie_rx_kb=1024.5,pcie_tx_kb=512.3")
	assert.True(t, strings.HasSuffix(lines[0], " 1700000000000"))

	// System line
	assert.Contains(t, lines[1], "system_metrics,agent_id=agent-abc,hostname=my-host")
	assert.Contains(t, lines[1], "cpu_usage=45.20,memory_used_mb=8000i,memory_total_mb=16384i")

	// Worker line
	assert.Contains(t, lines[2], "worker_metrics,agent_id=agent-abc,worker_id=w-1")
	assert.Contains(t, lines[2], "status=1i,connections=1i,restarts=2i")
}

func TestBuildMetricsLineProtocol_MultipleGPUs(t *testing.T) {
	gpuMetrics := map[string]*api.GPUMetrics{
		"gpu-001": {GPUID: "gpu-001", Utilization: 50.0, VRAMUsedMb: 4000, Temperature: 65.0, PowerUsageW: 200.0},
		"gpu-002": {GPUID: "gpu-002", Utilization: 80.0, VRAMUsedMb: 6000, Temperature: 70.0, PowerUsageW: 300.0},
	}
	gpuConfigs := []api.GPUStatus{
		{GPUID: "gpu-001", Vendor: "nvidia", Model: "RTX 3090", VRAMMb: 24576},
		{GPUID: "gpu-002", Vendor: "nvidia", Model: "RTX 4090", VRAMMb: 24576},
	}

	result := buildMetricsLineProtocol("agent-1", "host1", gpuMetrics, gpuConfigs, nil, nil, 1700000000000)

	lines := strings.Split(result, "\n")
	assert.Len(t, lines, 2, "should have exactly 2 GPU metric lines")

	// Check both GPUs are present (map iteration order is non-deterministic)
	combined := result
	assert.Contains(t, combined, "gpu_id=gpu-001")
	assert.Contains(t, combined, "gpu_id=gpu-002")
	assert.Contains(t, combined, "model=RTX\\ 3090")
	assert.Contains(t, combined, "model=RTX\\ 4090")
}

func TestBuildMetricsLineProtocol_TagEscaping(t *testing.T) {
	gpuMetrics := map[string]*api.GPUMetrics{
		"gpu=special": {GPUID: "gpu=special", Utilization: 10.0, VRAMUsedMb: 100, Temperature: 50.0, PowerUsageW: 100.0},
	}
	gpuConfigs := []api.GPUStatus{
		{GPUID: "gpu=special", Vendor: "vendor,test", Model: "Model With Spaces", VRAMMb: 8192},
	}

	result := buildMetricsLineProtocol("agent,id=1", "host name", gpuMetrics, gpuConfigs, nil, nil, 1700000000000)

	assert.Contains(t, result, "agent_id=agent\\,id\\=1")
	assert.Contains(t, result, "gpu_id=gpu\\=special")
	assert.Contains(t, result, "model=Model\\ With\\ Spaces")
	assert.Contains(t, result, "vendor=vendor\\,test")
}

func TestBuildMetricsLineProtocol_EmptyMetrics(t *testing.T) {
	result := buildMetricsLineProtocol("agent-1", "host1", nil, nil, nil, nil, 1700000000000)
	assert.Empty(t, result)
}

func TestBuildMetricsLineProtocol_WorkerStatusMapping(t *testing.T) {
	workers := []api.WorkerStatus{
		{WorkerID: "w-running", Status: "running"},
		{WorkerID: "w-stopped", Status: "stopped"},
		{WorkerID: "w-pending", Status: "pending"},
		{WorkerID: "w-stopping", Status: "stopping"},
		{WorkerID: "w-unknown", Status: "weird"},
	}

	result := buildMetricsLineProtocol("agent-1", "host1", nil, nil, workers, nil, 1700000000000)

	lines := strings.Split(result, "\n")
	assert.Len(t, lines, 5)

	// Verify status integer mapping
	assert.Contains(t, lines[0], "worker_id=w-running")
	assert.Contains(t, lines[0], "status=1i")

	assert.Contains(t, lines[1], "worker_id=w-stopped")
	assert.Contains(t, lines[1], "status=0i")

	assert.Contains(t, lines[2], "worker_id=w-pending")
	assert.Contains(t, lines[2], "status=2i")

	assert.Contains(t, lines[3], "worker_id=w-stopping")
	assert.Contains(t, lines[3], "status=3i")

	assert.Contains(t, lines[4], "worker_id=w-unknown")
	assert.Contains(t, lines[4], "status=-1i")
}

func TestBuildMetricsLineProtocol_OnlySystemMetrics(t *testing.T) {
	sysMetrics := &api.SystemMetrics{CPUUsage: 25.0, MemoryUsedMb: 4096, MemoryTotalMb: 8192}

	result := buildMetricsLineProtocol("agent-1", "host1", nil, nil, nil, sysMetrics, 1700000000000)

	assert.Contains(t, result, "system_metrics")
	assert.NotContains(t, result, "gpu_metrics")
	assert.NotContains(t, result, "worker_metrics")
}

func TestBuildMetricsLineProtocol_TimestampFormat(t *testing.T) {
	gpuMetrics := map[string]*api.GPUMetrics{
		"gpu-001": {GPUID: "gpu-001", Utilization: 50.0, VRAMUsedMb: 4000, Temperature: 65.0, PowerUsageW: 200.0},
	}
	gpuConfigs := []api.GPUStatus{
		{GPUID: "gpu-001", Vendor: "nvidia", Model: "RTX4090", VRAMMb: 24576},
	}
	ts := int64(1700000000123) // millisecond precision

	result := buildMetricsLineProtocol("agent-1", "host1", gpuMetrics, gpuConfigs, nil, nil, ts)

	assert.True(t, strings.HasSuffix(result, "1700000000123"), "timestamp should be in milliseconds")
}

func TestEscapeTagValue(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"has space", "has\\ space"},
		{"has,comma", "has\\,comma"},
		{"has=equals", "has\\=equals"},
		{"all three, = mixed", "all\\ three\\,\\ \\=\\ mixed"},
		{"", ""},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.expected, escapeTagValue(tc.input), "escapeTagValue(%q)", tc.input)
	}
}

func TestWorkerStatusToInt(t *testing.T) {
	assert.Equal(t, 1, workerStatusToInt("running"))
	assert.Equal(t, 0, workerStatusToInt("stopped"))
	assert.Equal(t, 2, workerStatusToInt("pending"))
	assert.Equal(t, 3, workerStatusToInt("stopping"))
	assert.Equal(t, -1, workerStatusToInt("unknown"))
}

func TestBuildMetricsLineProtocol_GPUConfigMismatch(t *testing.T) {
	// GPU has metrics but no matching config — vendor/model should be empty
	gpuMetrics := map[string]*api.GPUMetrics{
		"gpu-orphan": {GPUID: "gpu-orphan", Utilization: 30.0, VRAMUsedMb: 1000, Temperature: 55.0, PowerUsageW: 150.0},
	}

	result := buildMetricsLineProtocol("agent-1", "host1", gpuMetrics, nil, nil, nil, 1700000000000)

	assert.Contains(t, result, "gpu_id=gpu-orphan")
	assert.Contains(t, result, "model=,vendor=") // empty but present
	assert.Contains(t, result, "vram_total_mb=0i")
}
