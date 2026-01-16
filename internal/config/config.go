package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/NexusGPU/gpu-go/internal/api"
	"github.com/NexusGPU/gpu-go/internal/platform"
)

const (
	configFile  = "config.json"
	gpusFile    = "gpus.json"
	workersFile = "workers.json"
)

var (
	// DefaultPaths provides platform-specific default paths
	DefaultPaths = platform.DefaultPaths()
)

// Config represents the agent configuration
type Config struct {
	ConfigVersion int         `json:"config_version"`
	AgentID       string      `json:"agent_id"`
	AgentSecret   string      `json:"agent_secret"`
	ServerURL     string      `json:"server_url"`
	License       api.License `json:"license"`
}

// GPUConfig represents GPU configuration
type GPUConfig struct {
	GPUID        string  `json:"gpu_id"`
	Vendor       string  `json:"vendor"`
	Model        string  `json:"model"`
	VRAMMb       int64   `json:"vram_mb"`
	UsedByWorker *string `json:"used_by_worker"`
}

// WorkerConfig represents worker configuration with runtime state
type WorkerConfig struct {
	WorkerID    string               `json:"worker_id"`
	GPUIDs      []string             `json:"gpu_ids"`
	ListenPort  int                  `json:"listen_port"`
	Enabled     bool                 `json:"enabled"`
	PID         int                  `json:"pid,omitempty"`
	Status      string               `json:"status,omitempty"`
	Connections []api.ConnectionInfo `json:"connections,omitempty"`
}

// Manager manages configuration files
type Manager struct {
	configDir string
	stateDir  string
	mu        sync.RWMutex
}

// NewManager creates a new configuration manager
func NewManager(configDir, stateDir string) *Manager {
	paths := platform.DefaultPaths()
	if configDir == "" {
		configDir = paths.ConfigDir()
	}
	if stateDir == "" {
		stateDir = paths.StateDir()
	}
	return &Manager{
		configDir: configDir,
		stateDir:  stateDir,
	}
}

// NewManagerWithPaths creates a new configuration manager from platform paths
func NewManagerWithPaths(paths *platform.Paths) *Manager {
	return &Manager{
		configDir: paths.ConfigDir(),
		stateDir:  paths.StateDir(),
	}
}

// EnsureDirs ensures configuration directories exist
func (m *Manager) EnsureDirs() error {
	if err := os.MkdirAll(m.configDir, 0755); err != nil {
		return err
	}
	return os.MkdirAll(m.stateDir, 0755)
}

// ConfigPath returns the path to the config file
func (m *Manager) ConfigPath() string {
	return filepath.Join(m.configDir, configFile)
}

// GPUsPath returns the path to the GPUs file
func (m *Manager) GPUsPath() string {
	return filepath.Join(m.configDir, gpusFile)
}

// WorkersPath returns the path to the workers file
func (m *Manager) WorkersPath() string {
	return filepath.Join(m.configDir, workersFile)
}

// StateDir returns the state directory
func (m *Manager) StateDir() string {
	return m.stateDir
}

// LoadConfig loads the agent configuration
func (m *Manager) LoadConfig() (*Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// SaveConfig saves the agent configuration
func (m *Manager) SaveConfig(cfg *Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.EnsureDirs(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := m.ConfigPath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}

	return os.Rename(tmpPath, m.ConfigPath())
}

// LoadGPUs loads GPU configurations
func (m *Manager) LoadGPUs() ([]GPUConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.GPUsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var gpus []GPUConfig
	if err := json.Unmarshal(data, &gpus); err != nil {
		return nil, err
	}

	return gpus, nil
}

// SaveGPUs saves GPU configurations
func (m *Manager) SaveGPUs(gpus []GPUConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.EnsureDirs(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(gpus, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := m.GPUsPath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, m.GPUsPath())
}

// LoadWorkers loads worker configurations
func (m *Manager) LoadWorkers() ([]WorkerConfig, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.WorkersPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var workers []WorkerConfig
	if err := json.Unmarshal(data, &workers); err != nil {
		return nil, err
	}

	return workers, nil
}

// SaveWorkers saves worker configurations
func (m *Manager) SaveWorkers(workers []WorkerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.EnsureDirs(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(workers, "", "  ")
	if err != nil {
		return err
	}

	tmpPath := m.WorkersPath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}

	return os.Rename(tmpPath, m.WorkersPath())
}

// UpdateConfigVersion updates the config version and license
func (m *Manager) UpdateConfigVersion(version int, license api.License) error {
	cfg, err := m.LoadConfig()
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil
	}

	cfg.ConfigVersion = version
	cfg.License = license
	return m.SaveConfig(cfg)
}

// GetConfigVersion returns the current config version
func (m *Manager) GetConfigVersion() (int, error) {
	cfg, err := m.LoadConfig()
	if err != nil {
		return 0, err
	}
	if cfg == nil {
		return 0, nil
	}
	return cfg.ConfigVersion, nil
}

// ConfigExists checks if config file exists
func (m *Manager) ConfigExists() bool {
	_, err := os.Stat(m.ConfigPath())
	return err == nil
}
