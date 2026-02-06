package agent

import (
	"encoding/csv"
	"encoding/json"
	"strings"
)

type windowsVideoControllerInfo struct {
	Name          string `json:"Name"`
	DriverVersion string `json:"DriverVersion"`
}

func parsePowerShellVideoControllersJSON(output string) []windowsVideoControllerInfo {
	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}

	var infos []windowsVideoControllerInfo
	if strings.HasPrefix(output, "[") {
		if err := json.Unmarshal([]byte(output), &infos); err != nil {
			return nil
		}
		return filterWindowsVideoControllerInfos(infos)
	}

	var info windowsVideoControllerInfo
	if err := json.Unmarshal([]byte(output), &info); err != nil {
		return nil
	}

	return filterWindowsVideoControllerInfos([]windowsVideoControllerInfo{info})
}

func parseWMICVideoControllerCSV(output string) []windowsVideoControllerInfo {
	reader := csv.NewReader(strings.NewReader(output))
	reader.FieldsPerRecord = -1

	records, err := reader.ReadAll()
	if err != nil {
		return nil
	}

	infos := make([]windowsVideoControllerInfo, 0, len(records))
	for _, record := range records {
		if len(record) < 3 {
			continue
		}
		name := strings.TrimSpace(record[1])
		driver := strings.TrimSpace(record[2])
		if strings.EqualFold(name, "Name") && strings.EqualFold(driver, "DriverVersion") {
			continue
		}
		if name == "" && driver == "" {
			continue
		}
		infos = append(infos, windowsVideoControllerInfo{
			Name:          name,
			DriverVersion: driver,
		})
	}

	return filterWindowsVideoControllerInfos(infos)
}

func filterWindowsVideoControllerInfos(infos []windowsVideoControllerInfo) []windowsVideoControllerInfo {
	filtered := make([]windowsVideoControllerInfo, 0, len(infos))
	for _, info := range infos {
		name := strings.TrimSpace(info.Name)
		driver := strings.TrimSpace(info.DriverVersion)
		if name == "" && driver == "" {
			continue
		}
		filtered = append(filtered, windowsVideoControllerInfo{
			Name:          name,
			DriverVersion: driver,
		})
	}
	return filtered
}

func vendorFromWindowsGPUName(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "nvidia"):
		return vendorNVIDIA
	case strings.Contains(lower, "amd"), strings.Contains(lower, "radeon"), strings.Contains(lower, "advanced micro devices"):
		return vendorAMD
	default:
		return ""
	}
}

func pickWindowsVendor(infos []windowsVideoControllerInfo) (string, string) {
	var amdDriver string
	for _, info := range infos {
		vendor := vendorFromWindowsGPUName(info.Name)
		switch vendor {
		case vendorNVIDIA:
			return vendorNVIDIA, info.DriverVersion
		case vendorAMD:
			if amdDriver == "" {
				amdDriver = info.DriverVersion
			}
		}
	}

	if amdDriver != "" {
		return vendorAMD, amdDriver
	}

	return "", ""
}

func parseNvidiaSMIDriverVersion(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" {
			continue
		}
		if comma := strings.Index(line, ","); comma >= 0 {
			line = strings.TrimSpace(line[:comma])
		}
		return line
	}
	return ""
}

func nvidiaDriverVersionsMatch(smiVersion, wmiVersion string) bool {
	smiDigits := digitsOnly(smiVersion)
	if smiDigits == "" {
		return false
	}
	wmiDigits := lastComponentDigits(wmiVersion)
	if wmiDigits == "" {
		return false
	}
	return strings.HasSuffix(smiDigits, wmiDigits)
}

func lastComponentDigits(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return ""
	}
	parts := strings.Split(version, ".")
	if len(parts) == 0 {
		return ""
	}
	return digitsOnly(parts[len(parts)-1])
}

func digitsOnly(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
