package system

import (
	"fmt"
)

type scriptAction string

const (
	scriptActionUpdate    scriptAction = "update"
	scriptActionUninstall scriptAction = "uninstall"
)

const cdnBaseURL = "https://cdn.tensor-fusion.ai/archive/gpugo"

func buildScriptCommand(action scriptAction, goos string) (string, []string, error) {
	scriptBase, err := scriptBaseName(action)
	if err != nil {
		return "", nil, err
	}

	switch goos {
	case "linux", "darwin":
		url := fmt.Sprintf("%s/%s.sh", cdnBaseURL, scriptBase)
		return "sh", []string{"-c", fmt.Sprintf("curl -sfL %s | sh", url)}, nil
	case "windows":
		url := fmt.Sprintf("%s/%s.ps1", cdnBaseURL, scriptBase)
		return "powershell", []string{
			"-NoProfile",
			"-ExecutionPolicy",
			"Bypass",
			"-Command",
			fmt.Sprintf("irm %s | iex", url),
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported OS: %s", goos)
	}
}

func scriptBaseName(action scriptAction) (string, error) {
	switch action {
	case scriptActionUpdate:
		return "install", nil
	case scriptActionUninstall:
		return "uninstall", nil
	default:
		return "", fmt.Errorf("unsupported action: %s", action)
	}
}
