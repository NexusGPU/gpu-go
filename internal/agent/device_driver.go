package agent

import (
	"strconv"
	"strings"

	"k8s.io/klog/v2"
)

const (
	minNvidiaDriverMajor  = 535
	nvidiaDriverUpdateURL = "https://www.nvidia.com/drivers/"
)

func warnIfNvidiaDriverOutdated(version string) {
	if !isNvidiaDriverOutdated(version) {
		return
	}
	trimmed := strings.TrimSpace(version)
	klog.Warningf("Detected NVIDIA driver version %s which is below the supported minimum %d. Please update your NVIDIA driver: %s",
		trimmed, minNvidiaDriverMajor, nvidiaDriverUpdateURL)
}

func isNvidiaDriverOutdated(version string) bool {
	major, ok := nvidiaDriverMajorFromVersion(version)
	if !ok {
		return false
	}
	return major < minNvidiaDriverMajor
}

func nvidiaDriverMajorFromVersion(version string) (int, bool) {
	version = strings.TrimSpace(version)
	if version == "" {
		return 0, false
	}

	if major, ok := parseLeadingInt(version); ok && major >= 100 {
		return major, true
	}

	digits := lastComponentDigits(version)
	if digits == "" {
		return 0, false
	}
	if len(digits) == 4 {
		digits = "5" + digits
	}
	if len(digits) < 5 {
		return 0, false
	}
	major, err := strconv.Atoi(digits[:3])
	if err != nil {
		return 0, false
	}
	return major, true
}

func parseLeadingInt(value string) (int, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	var b strings.Builder
	for _, r := range value {
		if r < '0' || r > '9' {
			break
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return 0, false
	}
	num, err := strconv.Atoi(b.String())
	if err != nil {
		return 0, false
	}
	return num, true
}
