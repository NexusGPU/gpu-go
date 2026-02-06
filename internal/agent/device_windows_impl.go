package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func detectWindowsGPUVendor() (string, string, bool) {
	smiPath := findNvidiaSMIExecutable()
	smiDriver := ""
	if smiPath != "" {
		smiDriver = queryNvidiaSMIDriverVersion(smiPath)
	}

	entries, _ := queryWindowsVideoControllers()
	wmiVendor, wmiDriver := pickWindowsVendor(entries)

	vendor := wmiVendor
	driverVersion := wmiDriver
	verified := false

	if smiPath != "" {
		vendor = vendorNVIDIA
		if smiDriver != "" {
			driverVersion = smiDriver
		}
		if wmiDriver != "" && smiDriver != "" {
			verified = nvidiaDriverVersionsMatch(smiDriver, wmiDriver)
		}
	}

	if vendor == "" {
		vendor = wmiVendor
	}
	if driverVersion == "" {
		driverVersion = wmiDriver
	}

	return vendor, driverVersion, verified
}

func findNvidiaSMIExecutable() string {
	if path, err := exec.LookPath("nvidia-smi"); err == nil {
		return path
	}

	candidates := []string{
		filepath.Join(os.Getenv("ProgramW6432"), "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe"),
		filepath.Join(os.Getenv("SystemRoot"), "System32", "nvidia-smi.exe"),
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if fileExists(candidate) {
			return candidate
		}
	}

	if output, err := exec.Command("where", "nvidia-smi").Output(); err == nil {
		for _, line := range strings.Split(string(output), "\n") {
			path := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
			if path == "" {
				continue
			}
			if fileExists(path) {
				return path
			}
		}
	}

	return ""
}

func queryNvidiaSMIDriverVersion(smiPath string) string {
	cmd := exec.Command(smiPath, "--query-gpu=driver_version", "--format=csv,noheader")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return parseNvidiaSMIDriverVersion(string(output))
}

func queryWindowsVideoControllers() ([]windowsVideoControllerInfo, error) {
	if infos := queryPowerShellVideoControllers("powershell"); len(infos) > 0 {
		return infos, nil
	}
	if infos := queryPowerShellVideoControllers("pwsh"); len(infos) > 0 {
		return infos, nil
	}
	if infos := queryWMICVideoControllers(); len(infos) > 0 {
		return infos, nil
	}
	return nil, fmt.Errorf("no Windows video controller info detected")
}

func queryPowerShellVideoControllers(shell string) []windowsVideoControllerInfo {
	cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-Command",
		"Get-CimInstance Win32_VideoController | Select-Object Name,DriverVersion | ConvertTo-Json -Compress")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parsePowerShellVideoControllersJSON(string(output))
}

func queryWMICVideoControllers() []windowsVideoControllerInfo {
	cmd := exec.Command("wmic", "path", "win32_videocontroller", "get", "Name,DriverVersion", "/format:csv")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseWMICVideoControllerCSV(string(output))
}
