package platform

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
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

// AgentPIDFile returns the path to the agent PID file
// All platforms: ~/.gpugo/state/agent.pid (or StateDir/agent.pid)
func (p *Paths) AgentPIDFile() string {
	return filepath.Join(p.stateDir, "agent.pid")
}

// ConnectionsDir returns the directory for worker connection files
// Each worker writes its connections to a separate file: {workerID}.txt
// All platforms: ~/.gpugo/state/connections (or StateDir/connections)
func (p *Paths) ConnectionsDir() string {
	return filepath.Join(p.stateDir, "connections")
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

// StudioDir returns the directory for studio configurations
// All platforms: ~/.gpugo/studio (or UserDir/studio)
func (p *Paths) StudioDir() string {
	return filepath.Join(p.userDir, "studio")
}

// StudioLogsDir returns the logs directory for a specific studio
// name: the normalized studio name (use NormalizeName to sanitize)
// All platforms: ~/.gpugo/studio/{name}/logs
func (p *Paths) StudioLogsDir(name string) string {
	return filepath.Join(p.StudioDir(), NormalizeName(name), "logs")
}

// StudioConfigDir returns the config directory for a specific studio
// Used for storing ld.so.preload and ld.so.conf content for the studio
// All platforms: ~/.gpugo/studio/{name}/config
func (p *Paths) StudioConfigDir(name string) string {
	return filepath.Join(p.StudioDir(), NormalizeName(name), "config")
}

// CurrentOSLogsDir returns the logs directory for the current OS (used by ggo use)
// All platforms: ~/.gpugo/studio/current-os/logs
func (p *Paths) CurrentOSLogsDir() string {
	return p.StudioLogsDir("current-os")
}

// CurrentOSConfigDir returns the config directory for the current OS (used by ggo use)
// All platforms: ~/.gpugo/studio/current-os/config
func (p *Paths) CurrentOSConfigDir() string {
	return p.StudioConfigDir("current-os")
}

// LDSoConfPath returns the path to the ld.so.conf.d file for a studio
// This file will be mounted to /etc/ld.so.conf.d/zz_tensor-fusion.conf in containers
func (p *Paths) LDSoConfPath(name string) string {
	return filepath.Join(p.StudioConfigDir(name), "ld.so.conf.d", "zz_tensor-fusion.conf")
}

// LDSoPreloadPath returns the path to the ld.so.preload file for a studio
// This file will be mounted to /etc/ld.so.preload in containers
func (p *Paths) LDSoPreloadPath(name string) string {
	return filepath.Join(p.StudioConfigDir(name), "ld.so.preload")
}

// NormalizeName normalizes a string to be a valid folder name
// - Converts to lowercase
// - Replaces spaces and special chars with hyphens
// - Removes consecutive hyphens
// - Trims leading/trailing hyphens
func NormalizeName(name string) string {
	// Convert to lowercase
	name = strings.ToLower(name)

	// Replace any non-alphanumeric characters (except hyphen and underscore) with hyphen
	re := regexp.MustCompile(`[^a-z0-9\-_]+`)
	name = re.ReplaceAllString(name, "-")

	// Remove consecutive hyphens
	re = regexp.MustCompile(`-+`)
	name = re.ReplaceAllString(name, "-")

	// Trim leading/trailing hyphens
	name = strings.Trim(name, "-")

	// Ensure not empty
	if name == "" {
		name = "default"
	}

	return name
}

// EnsureStudioDirs creates all required directories for a studio
func (p *Paths) EnsureStudioDirs(name string) error {
	dirs := []string{
		p.StudioLogsDir(name),
		p.StudioConfigDir(name),
		filepath.Dir(p.LDSoConfPath(name)), // ld.so.conf.d directory
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
