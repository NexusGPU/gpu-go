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
// All paths are stored in the user directory (~/.gpugo)
func DefaultPaths() *Paths {
	p := &Paths{}
	// Initialize userDir first as other paths depend on it
	p.userDir = p.defaultUserDir()
	p.configDir = p.defaultConfigDir()
	p.stateDir = p.defaultStateDir()
	p.cacheDir = p.defaultCacheDir()
	return p
}

// ConfigDir returns the configuration directory
// All platforms: ~/.gpugo/config (or UserDir/config)
func (p *Paths) ConfigDir() string {
	return p.configDir
}

// StateDir returns the state directory for runtime data
// All platforms: ~/.gpugo/state (or UserDir/state)
func (p *Paths) StateDir() string {
	return p.stateDir
}

// CacheDir returns the cache directory for downloaded files
// All platforms: ~/.gpugo/cache (or UserDir/cache)
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
// All platforms: ~/.gpugo/lib (or UserDir/lib)
func (p *Paths) LibDir() string {
	return filepath.Join(p.userDir, "lib")
}

// BinDir returns the directory for binaries
// All platforms: ~/.gpugo/bin (or UserDir/bin)
func (p *Paths) BinDir() string {
	return filepath.Join(p.userDir, "bin")
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

	// All paths stored in user directory
	return filepath.Join(p.userDir, "config")
}

func (p *Paths) defaultStateDir() string {
	// Check environment variable first
	if dir := os.Getenv("TENSOR_FUSION_STATE_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("GGO_STATE_DIR"); dir != "" {
		return dir
	}

	// All paths stored in user directory
	return filepath.Join(p.userDir, "state")
}

func (p *Paths) defaultCacheDir() string {
	// Check environment variable first
	if dir := os.Getenv("GGO_CACHE_DIR"); dir != "" {
		return dir
	}

	// All paths stored in user directory
	return filepath.Join(p.userDir, "cache")
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
