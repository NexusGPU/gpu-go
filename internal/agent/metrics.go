package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/NexusGPU/gpu-go/internal/api"
	"k8s.io/klog/v2"
)

// workerStatusToInt maps worker status strings to integers for metrics storage.
func workerStatusToInt(status string) int {
	switch status {
	case workerStatusRunning:
		return 1
	case workerStatusPending:
		return 2
	case workerStatusStopping:
		return 3
	case workerStatusStopped:
		return 0
	default:
		return -1
	}
}

// buildMetricsLineProtocol builds InfluxDB v2 line protocol for GPU, system,
// and worker metrics. Each line is newline-separated. Timestamp is in milliseconds.
func buildMetricsLineProtocol(
	agentID string,
	hostname string,
	gpuMetrics map[string]*api.GPUMetrics,
	gpuConfigs []api.GPUStatus,
	workerStatuses []api.WorkerStatus,
	systemMetrics *api.SystemMetrics,
	timestampMs int64,
) string {
	var b strings.Builder

	// Build a lookup from GPU ID -> config for vendor/model/vram_total
	gpuConfigMap := make(map[string]*api.GPUStatus, len(gpuConfigs))
	for i := range gpuConfigs {
		gpuConfigMap[gpuConfigs[i].GPUID] = &gpuConfigs[i]
	}

	// gpu_metrics lines
	for gpuID, m := range gpuMetrics {
		model := ""
		vendor := ""
		vramTotalMb := int64(0)
		if cfg, ok := gpuConfigMap[gpuID]; ok {
			model = cfg.Model
			vendor = cfg.Vendor
			vramTotalMb = cfg.VRAMMb
		}

		b.WriteString("gpu_metrics,agent_id=")
		b.WriteString(escapeTagValue(agentID))
		b.WriteString(",gpu_id=")
		b.WriteString(escapeTagValue(m.GPUID))
		b.WriteString(",model=")
		b.WriteString(escapeTagValue(model))
		b.WriteString(",vendor=")
		b.WriteString(escapeTagValue(vendor))
		fmt.Fprintf(&b, " utilization=%.2f,vram_used_mb=%di,vram_total_mb=%di,temperature=%.1f,power_usage_w=%.1f,pcie_rx_kb=%.1f,pcie_tx_kb=%.1f %d",
			m.Utilization, m.VRAMUsedMb, vramTotalMb, m.Temperature, m.PowerUsageW, m.PCIeRxKB, m.PCIeTxKB, timestampMs)
		b.WriteByte('\n')
	}

	// system_metrics line
	if systemMetrics != nil {
		b.WriteString("system_metrics,agent_id=")
		b.WriteString(escapeTagValue(agentID))
		b.WriteString(",hostname=")
		b.WriteString(escapeTagValue(hostname))
		fmt.Fprintf(&b, " cpu_usage=%.2f,memory_used_mb=%di,memory_total_mb=%di %d",
			systemMetrics.CPUUsage, systemMetrics.MemoryUsedMb, systemMetrics.MemoryTotalMb, timestampMs)
		b.WriteByte('\n')
	}

	// worker_metrics lines
	for _, w := range workerStatuses {
		b.WriteString("worker_metrics,agent_id=")
		b.WriteString(escapeTagValue(agentID))
		b.WriteString(",worker_id=")
		b.WriteString(escapeTagValue(w.WorkerID))
		fmt.Fprintf(&b, " status=%di,connections=%di,restarts=%di %d",
			workerStatusToInt(w.Status), len(w.Connections), w.Restarts, timestampMs)
		b.WriteByte('\n')
	}

	return strings.TrimRight(b.String(), "\n")
}

// escapeTagValue escapes special characters in InfluxDB line protocol tag values.
// Spaces, commas, and equals signs must be backslash-escaped.
func escapeTagValue(s string) string {
	s = strings.ReplaceAll(s, " ", "\\ ")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "=", "\\=")
	return s
}

// collectMetricsLineProtocol gathers GPU and system metrics, then builds
// the InfluxDB line protocol string. Returns empty string on failure.
func (a *Agent) collectMetricsLineProtocol(
	gpuStatuses []api.GPUStatus,
	workerStatuses []api.WorkerStatus,
	now time.Time,
) string {
	// Collect GPU metrics from hypervisor (best-effort)
	var gpuMetrics map[string]*api.GPUMetrics
	if a.hypervisorMgr != nil && a.hypervisorMgr.IsStarted() {
		hvMetrics, err := a.hypervisorMgr.GetDeviceMetrics()
		if err != nil {
			klog.V(4).Infof("Failed to collect GPU metrics: %v", err)
		} else {
			gpuMetrics = ConvertMetricsToGPUMetrics(hvMetrics)
		}
	}

	// Collect system metrics (best-effort, nil on non-Linux)
	sysMetrics := collectSystemMetrics()

	// Nothing to report
	if len(gpuMetrics) == 0 && sysMetrics == nil && len(workerStatuses) == 0 {
		return ""
	}

	return buildMetricsLineProtocol(
		a.agentID,
		a.hostname,
		gpuMetrics,
		gpuStatuses,
		workerStatuses,
		sysMetrics,
		now.UnixMilli(),
	)
}
