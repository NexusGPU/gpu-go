package studio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_AddSSHConfigCreatesAndUpdatesSingleEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewManager()

	first := &Environment{Name: "alpha", SSHHost: "127.0.0.1", SSHPort: 2201, SSHUser: "root"}
	require.NoError(t, m.AddSSHConfig(first))

	updated := &Environment{Name: "alpha", SSHHost: "10.10.10.10", SSHPort: 2222, SSHUser: "ubuntu"}
	require.NoError(t, m.AddSSHConfig(updated))

	configData, err := os.ReadFile(filepath.Join(home, ".ssh", "config"))
	require.NoError(t, err)

	config := string(configData)
	assert.Equal(t, 1, strings.Count(config, "Host ggo-alpha"), "entry should be updated in-place without duplication")
	assert.Contains(t, config, "HostName 10.10.10.10")
	assert.Contains(t, config, "Port 2222")
	assert.Contains(t, config, "User ubuntu")
	assert.NotContains(t, config, "HostName 127.0.0.1")
}

func TestManager_RemoveSSHConfigRemovesOnlyTargetEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	configPath := filepath.Join(home, ".ssh", "config")
	require.NoError(t, os.MkdirAll(filepath.Dir(configPath), 0o700))

	initial := `Host github.com
    User git

# GPU Go Studio Environment: alpha
Host ggo-alpha
    HostName 127.0.0.1
    Port 2201
    User root
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null

# GPU Go Studio Environment: beta
Host ggo-beta
    HostName 10.0.0.2
    Port 2202
    User ubuntu
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
`
	require.NoError(t, os.WriteFile(configPath, []byte(initial), 0o600))

	m := NewManager()
	require.NoError(t, m.RemoveSSHConfig("alpha"))

	configData, err := os.ReadFile(configPath)
	require.NoError(t, err)

	config := string(configData)
	assert.NotContains(t, config, "Host ggo-alpha")
	assert.NotContains(t, config, "Environment: alpha")
	assert.Contains(t, config, "Host github.com")
	assert.Contains(t, config, "# GPU Go Studio Environment: beta")
	assert.Contains(t, config, "Host ggo-beta")
}

func TestManager_AddSSHConfigRejectsMissingSSHMetadata(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	m := NewManager()
	err := m.AddSSHConfig(&Environment{Name: "missing-ssh"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not have SSH configured")
}
