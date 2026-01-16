package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultPaths(t *testing.T) {
	p := DefaultPaths()

	assert.NotEmpty(t, p.ConfigDir())
	assert.NotEmpty(t, p.StateDir())
	assert.NotEmpty(t, p.CacheDir())
	assert.NotEmpty(t, p.UserDir())
}

func TestPathsWithEnvOverride(t *testing.T) {
	// Test config dir override
	t.Run("config dir env override", func(t *testing.T) {
		origVal := os.Getenv("GGO_CONFIG_DIR")
		defer os.Setenv("GGO_CONFIG_DIR", origVal)

		os.Setenv("GGO_CONFIG_DIR", "/custom/config")
		p := DefaultPaths()
		assert.Equal(t, "/custom/config", p.ConfigDir())
	})

	// Test state dir override
	t.Run("state dir env override", func(t *testing.T) {
		origVal := os.Getenv("GGO_STATE_DIR")
		defer os.Setenv("GGO_STATE_DIR", origVal)

		os.Setenv("GGO_STATE_DIR", "/custom/state")
		p := DefaultPaths()
		assert.Equal(t, "/custom/state", p.StateDir())
	})

	// Test tensor fusion state dir override
	t.Run("tensor fusion state dir env override", func(t *testing.T) {
		origVal := os.Getenv("TENSOR_FUSION_STATE_DIR")
		origGgo := os.Getenv("GGO_STATE_DIR")
		defer func() {
			os.Setenv("TENSOR_FUSION_STATE_DIR", origVal)
			os.Setenv("GGO_STATE_DIR", origGgo)
		}()

		os.Unsetenv("GGO_STATE_DIR")
		os.Setenv("TENSOR_FUSION_STATE_DIR", "/tf/state")
		p := DefaultPaths()
		assert.Equal(t, "/tf/state", p.StateDir())
	})
}

func TestUserDir(t *testing.T) {
	p := DefaultPaths()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot get user home directory")
	}

	expected := filepath.Join(home, ".gpugo")
	assert.Equal(t, expected, p.UserDir())
}

func TestWithConfigDir(t *testing.T) {
	p := DefaultPaths()
	custom := p.WithConfigDir("/custom/path")

	assert.Equal(t, "/custom/path", custom.ConfigDir())
	assert.Equal(t, p.StateDir(), custom.StateDir())
	assert.Equal(t, p.CacheDir(), custom.CacheDir())
}

func TestWithStateDir(t *testing.T) {
	p := DefaultPaths()
	custom := p.WithStateDir("/custom/state")

	assert.Equal(t, p.ConfigDir(), custom.ConfigDir())
	assert.Equal(t, "/custom/state", custom.StateDir())
}

func TestPlatformDetection(t *testing.T) {
	switch runtime.GOOS {
	case "windows":
		assert.True(t, IsWindows())
		assert.False(t, IsDarwin())
		assert.False(t, IsLinux())
	case "darwin":
		assert.False(t, IsWindows())
		assert.True(t, IsDarwin())
		assert.False(t, IsLinux())
	case "linux":
		assert.False(t, IsWindows())
		assert.False(t, IsDarwin())
		assert.True(t, IsLinux())
	}
}

func TestLibDir(t *testing.T) {
	p := DefaultPaths()
	libDir := p.LibDir()
	assert.Contains(t, libDir, "lib")
}

func TestTempDir(t *testing.T) {
	p := DefaultPaths()
	tempDir := p.TempDir()
	assert.NotEmpty(t, tempDir)
}

func TestGlobPattern(t *testing.T) {
	p := DefaultPaths()
	pattern := p.GlobPattern("gpugo-")
	assert.Contains(t, pattern, "gpugo-*")
}
