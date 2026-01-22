package platform

import (
	"os"
	"path/filepath"
	"runtime"
)

// Platform constants for runtime.GOOS comparisons
const (
	osWindows = "windows"
	osDarwin  = "darwin"
	osLinux   = "linux"
)

// Paths provides cross-platform path resolution for ggo
type Paths struct {
	configDir string
	stateDir  string
	cacheDir  string
	userDir   string
}

// DefaultPaths returns the default paths for the current platform
func DefaultPaths() *Paths {
	p := &Paths{}
	p.configDir = p.defaultConfigDir()
	p.stateDir = p.defaultStateDir()
	p.cacheDir = p.defaultCacheDir()
	p.userDir = p.defaultUserDir()
	return p
}

// ConfigDir returns the configuration directory
// - Linux: /etc/gpugo (system) or ~/.config/gpugo (user)
// - macOS: /Library/Application Support/gpugo (system) or ~/Library/Application Support/gpugo (user)
// - Windows: %ProgramData%\gpugo (system) or %APPDATA%\gpugo (user)
func (p *Paths) ConfigDir() string {
	return p.configDir
}

// StateDir returns the state directory for runtime data
// - Linux: /var/lib/gpugo or /tmp/tensor-fusion-state
// - macOS: /var/lib/gpugo or /tmp/tensor-fusion-state
// - Windows: %ProgramData%\gpugo\state
func (p *Paths) StateDir() string {
	return p.stateDir
}

// CacheDir returns the cache directory for downloaded files
// - Linux: /var/cache/gpugo or ~/.cache/gpugo
// - macOS: ~/Library/Caches/gpugo
// - Windows: %LOCALAPPDATA%\gpugo\cache
func (p *Paths) CacheDir() string {
	return p.cacheDir
}

// UserDir returns the user-specific directory
// - Linux: ~/.gpugo
// - macOS: ~/.gpugo
// - Windows: %USERPROFILE%\.gpugo
func (p *Paths) UserDir() string {
	return p.userDir
}

// LibDir returns the directory for shared libraries
func (p *Paths) LibDir() string {
	switch runtime.GOOS {
	case osWindows:
		return filepath.Join(p.configDir, "lib")
	default:
		return filepath.Join(p.configDir, "lib")
	}
}

// BinDir returns the directory for binaries
func (p *Paths) BinDir() string {
	switch runtime.GOOS {
	case osWindows:
		return filepath.Join(p.configDir, "bin")
	case osDarwin:
		return "/usr/local/bin"
	default:
		return "/usr/local/bin"
	}
}

// TempDir returns a platform-appropriate temporary directory
func (p *Paths) TempDir() string {
	switch runtime.GOOS {
	case osWindows:
		return os.TempDir()
	default:
		return "/tmp"
	}
}

// GlobPattern returns the platform-appropriate glob pattern for temp cleanup
func (p *Paths) GlobPattern(prefix string) string {
	return filepath.Join(p.TempDir(), prefix+"*")
}

func (p *Paths) defaultConfigDir() string {
	// Check environment variable first
	if dir := os.Getenv("GGO_CONFIG_DIR"); dir != "" {
		return dir
	}

	switch runtime.GOOS {
	case osWindows:
		// Use ProgramData for system-wide config
		if programData := os.Getenv("ProgramData"); programData != "" {
			return filepath.Join(programData, "gpugo")
		}
		return `C:\ProgramData\gpugo`
	case osDarwin:
		// Check if running as root
		if os.Geteuid() == 0 {
			return "/Library/Application Support/gpugo"
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Application Support", "gpugo")
		}
		return "/Library/Application Support/gpugo"
	default: // linux and others
		// Check if running as root
		if os.Geteuid() == 0 {
			return "/etc/gpugo"
		}
		if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
			return filepath.Join(configHome, "gpugo")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".config", "gpugo")
		}
		return "/etc/gpugo"
	}
}

func (p *Paths) defaultStateDir() string {
	// Check environment variable first
	if dir := os.Getenv("TENSOR_FUSION_STATE_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("GGO_STATE_DIR"); dir != "" {
		return dir
	}

	switch runtime.GOOS {
	case osWindows:
		if programData := os.Getenv("ProgramData"); programData != "" {
			return filepath.Join(programData, "gpugo", "state")
		}
		return `C:\ProgramData\gpugo\state`
	case osDarwin:
		// macOS uses /tmp for state (similar to Linux)
		return "/tmp/tensor-fusion-state"
	default: // linux and others
		return "/tmp/tensor-fusion-state"
	}
}

func (p *Paths) defaultCacheDir() string {
	// Check environment variable first
	if dir := os.Getenv("GGO_CACHE_DIR"); dir != "" {
		return dir
	}

	switch runtime.GOOS {
	case osWindows:
		if localAppData := os.Getenv("LOCALAPPDATA"); localAppData != "" {
			return filepath.Join(localAppData, "gpugo", "cache")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "AppData", "Local", "gpugo", "cache")
		}
		return `C:\ProgramData\gpugo\cache`
	case osDarwin:
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Caches", "gpugo")
		}
		return "/tmp/gpugo-cache"
	default: // linux and others
		if cacheHome := os.Getenv("XDG_CACHE_HOME"); cacheHome != "" {
			return filepath.Join(cacheHome, "gpugo")
		}
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ".cache", "gpugo")
		}
		return "/var/cache/gpugo"
	}
}

func (p *Paths) defaultUserDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		switch runtime.GOOS {
		case osWindows:
			return `C:\Users\Default\.gpugo`
		default:
			return "/tmp/.gpugo"
		}
	}
	return filepath.Join(home, ".gpugo")
}

// WithConfigDir returns a new Paths with a custom config directory
func (p *Paths) WithConfigDir(dir string) *Paths {
	return &Paths{
		configDir: dir,
		stateDir:  p.stateDir,
		cacheDir:  p.cacheDir,
		userDir:   p.userDir,
	}
}

// WithStateDir returns a new Paths with a custom state directory
func (p *Paths) WithStateDir(dir string) *Paths {
	return &Paths{
		configDir: p.configDir,
		stateDir:  dir,
		cacheDir:  p.cacheDir,
		userDir:   p.userDir,
	}
}

// EnsureAllDirs creates all required directories
func (p *Paths) EnsureAllDirs() error {
	dirs := []string{p.configDir, p.stateDir, p.cacheDir, p.userDir}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

// IsWindows returns true if running on Windows
func IsWindows() bool {
	return runtime.GOOS == osWindows
}

// IsDarwin returns true if running on macOS
func IsDarwin() bool {
	return runtime.GOOS == osDarwin
}

// IsLinux returns true if running on Linux
func IsLinux() bool {
	return runtime.GOOS == osLinux
}
