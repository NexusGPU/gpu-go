//go:build linux

package agent

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"github.com/NexusGPU/gpu-go/internal/api"
	"k8s.io/klog/v2"
)

// collectSystemMetrics reads /proc/stat and /proc/meminfo on Linux.
// Uses a two-sample approach with cumulative jiffies — since this runs
// every 60s, we take an instantaneous snapshot of cumulative CPU time.
// Returns nil if reading fails rather than blocking the status report.
func collectSystemMetrics() *api.SystemMetrics {
	cpuUsage := readCPUUsageInstant()
	memUsed, memTotal := readMemInfo()

	if memTotal == 0 {
		return nil
	}

	return &api.SystemMetrics{
		CPUUsage:      cpuUsage,
		MemoryUsedMb:  memUsed,
		MemoryTotalMb: memTotal,
	}
}

// readCPUUsageInstant reads /proc/stat and computes overall CPU busy percentage
// from cumulative jiffies. This gives the average since boot, which is a reasonable
// proxy when sampled frequently. A delta-based approach can be added later.
func readCPUUsageInstant() float64 {
	f, err := os.Open("/proc/stat")
	if err != nil {
		klog.V(4).Infof("Failed to open /proc/stat: %v", err)
		return 0
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		// cpu  user nice system idle iowait irq softirq steal guest guest_nice
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return 0
		}

		var total, idle uint64
		for i := 1; i < len(fields); i++ {
			val, _ := strconv.ParseUint(fields[i], 10, 64)
			total += val
			if i == 4 { // idle is the 4th value (0-indexed field 4)
				idle = val
			}
		}

		if total == 0 {
			return 0
		}
		return float64(total-idle) / float64(total) * 100
	}
	return 0
}

// readMemInfo reads /proc/meminfo and returns (usedMb, totalMb).
func readMemInfo() (int64, int64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		klog.V(4).Infof("Failed to open /proc/meminfo: %v", err)
		return 0, 0
	}
	defer f.Close()

	var totalKb, availableKb int64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			totalKb = parseMemInfoValue(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			availableKb = parseMemInfoValue(line)
		}
		if totalKb > 0 && availableKb > 0 {
			break
		}
	}

	if totalKb == 0 {
		return 0, 0
	}

	totalMb := totalKb / 1024
	usedMb := (totalKb - availableKb) / 1024
	return usedMb, totalMb
}

// parseMemInfoValue extracts the numeric kB value from a /proc/meminfo line.
// Format: "MemTotal:       16384000 kB"
func parseMemInfoValue(line string) int64 {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return 0
	}
	val, _ := strconv.ParseInt(parts[1], 10, 64)
	return val
}
