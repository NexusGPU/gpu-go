package system

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildScriptCommand_UninstallLinux(t *testing.T) {
	cmd, args, err := buildScriptCommand(scriptActionUninstall, "linux")
	require.NoError(t, err)
	assert.Equal(t, "sh", cmd)
	assert.Equal(t, []string{
		"-c",
		"curl -sfL https://cdn.tensor-fusion.ai/archive/gpugo/uninstall.sh | sh",
	}, args)
}

func TestBuildScriptCommand_UninstallWindows(t *testing.T) {
	cmd, args, err := buildScriptCommand(scriptActionUninstall, "windows")
	require.NoError(t, err)
	assert.Equal(t, "powershell", cmd)
	assert.Equal(t, []string{
		"-NoProfile",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		"irm https://cdn.tensor-fusion.ai/archive/gpugo/uninstall.ps1 | iex",
	}, args)
}

func TestBuildScriptCommand_UpdateDarwin(t *testing.T) {
	cmd, args, err := buildScriptCommand(scriptActionUpdate, "darwin")
	require.NoError(t, err)
	assert.Equal(t, "sh", cmd)
	assert.Equal(t, []string{
		"-c",
		"curl -sfL https://cdn.tensor-fusion.ai/archive/gpugo/install.sh | sh",
	}, args)
}

func TestBuildScriptCommand_UpdateWindows(t *testing.T) {
	cmd, args, err := buildScriptCommand(scriptActionUpdate, "windows")
	require.NoError(t, err)
	assert.Equal(t, "powershell", cmd)
	assert.Equal(t, []string{
		"-NoProfile",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		"irm https://cdn.tensor-fusion.ai/archive/gpugo/install.ps1 | iex",
	}, args)
}

func TestBuildScriptCommand_UnsupportedOS(t *testing.T) {
	_, _, err := buildScriptCommand(scriptActionUpdate, "plan9")
	require.Error(t, err)
}
