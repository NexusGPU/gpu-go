package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/NexusGPU/gpu-go/internal/errors"
	"github.com/NexusGPU/gpu-go/internal/platform"
)

// Manager manages AI studio environments across different backends
type Manager struct {
	paths    *platform.Paths
	backends map[Mode]Backend
	mu       sync.RWMutex
}

// NewManager creates a new studio manager
func NewManager() *Manager {
	m := &Manager{
		paths:    platform.DefaultPaths(),
		backends: make(map[Mode]Backend),
	}
	return m
}

// RegisterBackend registers a backend
func (m *Manager) RegisterBackend(backend Backend) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.backends[backend.Mode()] = backend
}

// GetBackend returns a backend by mode
func (m *Manager) GetBackend(mode Mode) (Backend, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if mode == ModeAuto {
		return m.detectBestBackend()
	}

	backend, ok := m.backends[mode]
	if !ok {
		return nil, errors.NotFoundf("backend not registered for mode: %s", mode)
	}
	return backend, nil
}

// detectBestBackend detects the best available backend for the current platform
func (m *Manager) detectBestBackend() (Backend, error) {
	ctx := context.Background()

	// Platform-specific preference order
	var preferenceOrder []Mode
	switch runtime.GOOS {
	case "windows":
		preferenceOrder = []Mode{ModeWSL, ModeDocker, ModeKubernetes}
	case "darwin":
		preferenceOrder = []Mode{ModeColima, ModeAppleContainer, ModeDocker, ModeKubernetes}
	case "linux":
		preferenceOrder = []Mode{ModeDocker, ModeColima, ModeKubernetes}
	default:
		preferenceOrder = []Mode{ModeDocker, ModeKubernetes}
	}

	for _, mode := range preferenceOrder {
		if backend, ok := m.backends[mode]; ok {
			if backend.IsAvailable(ctx) {
				return backend, nil
			}
		}
	}

	return nil, errors.Unavailable("no backend available for platform: " + runtime.GOOS)
}

// ListAvailableBackends returns all available backends
func (m *Manager) ListAvailableBackends(ctx context.Context) []Backend {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var available []Backend
	for _, backend := range m.backends {
		if backend.IsAvailable(ctx) {
			available = append(available, backend)
		}
	}
	return available
}

// Create creates a new environment
func (m *Manager) Create(ctx context.Context, opts *CreateOptions) (*Environment, error) {
	backend, err := m.GetBackend(opts.Mode)
	if err != nil {
		return nil, err
	}

	env, err := backend.Create(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Save environment to local state
	if err := m.saveEnvironment(env); err != nil {
		// Log but don't fail
		fmt.Fprintf(os.Stderr, "Warning: failed to save environment state: %v\n", err)
	}

	return env, nil
}

// Get gets an environment by ID or name
func (m *Manager) Get(ctx context.Context, idOrName string) (*Environment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, backend := range m.backends {
		if !backend.IsAvailable(ctx) {
			continue
		}
		env, err := backend.Get(ctx, idOrName)
		if err == nil && env != nil {
			return env, nil
		}
	}

	return nil, errors.NotFound("environment", idOrName)
}

// List lists all environments across all backends
func (m *Manager) List(ctx context.Context) ([]*Environment, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var allEnvs []*Environment
	for _, backend := range m.backends {
		if !backend.IsAvailable(ctx) {
			continue
		}
		envs, err := backend.List(ctx)
		if err != nil {
			continue // Log but continue with other backends
		}
		allEnvs = append(allEnvs, envs...)
	}

	return allEnvs, nil
}

// Stop stops an environment
func (m *Manager) Stop(ctx context.Context, idOrName string) error {
	env, err := m.Get(ctx, idOrName)
	if err != nil {
		return err
	}

	backend, err := m.GetBackend(env.Mode)
	if err != nil {
		return err
	}

	return backend.Stop(ctx, env.ID)
}

// Start starts an environment
func (m *Manager) Start(ctx context.Context, idOrName string) error {
	env, err := m.Get(ctx, idOrName)
	if err != nil {
		return err
	}

	backend, err := m.GetBackend(env.Mode)
	if err != nil {
		return err
	}

	return backend.Start(ctx, env.ID)
}

// Remove removes an environment
func (m *Manager) Remove(ctx context.Context, idOrName string) error {
	env, err := m.Get(ctx, idOrName)
	if err != nil {
		return err
	}

	backend, err := m.GetBackend(env.Mode)
	if err != nil {
		return err
	}

	if err := backend.Remove(ctx, env.ID); err != nil {
		return err
	}

	// Remove from local state
	return m.removeEnvironment(env.ID)
}

// AddSSHConfig adds an SSH config entry for an environment
func (m *Manager) AddSSHConfig(env *Environment) error {
	if env.SSHHost == "" || env.SSHPort == 0 {
		return errors.BadRequest("environment does not have SSH configured")
	}

	sshConfigPath := m.getSSHConfigPath()

	// Read existing config
	existingConfig := ""
	if data, err := os.ReadFile(sshConfigPath); err == nil {
		existingConfig = string(data)
	}

	// Generate new entry
	hostName := fmt.Sprintf("ggo-%s", env.Name)
	entry := fmt.Sprintf(`
# GPU Go Studio Environment: %s
Host %s
    HostName %s
    Port %d
    User %s
    StrictHostKeyChecking no
    UserKnownHostsFile /dev/null
`, env.Name, hostName, env.SSHHost, env.SSHPort, env.SSHUser)

	// Check if entry already exists
	if strings.Contains(existingConfig, fmt.Sprintf("Host %s", hostName)) {
		// Update existing entry by removing old and adding new
		existingConfig = m.removeSSHConfigEntry(existingConfig, hostName)
	}

	// Append new entry
	newConfig := existingConfig + entry

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(sshConfigPath), 0700); err != nil {
		return errors.Wrap(err, "failed to create SSH config directory")
	}

	// Write config
	if err := os.WriteFile(sshConfigPath, []byte(newConfig), 0600); err != nil {
		return errors.Wrap(err, "failed to write SSH config")
	}

	return nil
}

// RemoveSSHConfig removes an SSH config entry for an environment
func (m *Manager) RemoveSSHConfig(envName string) error {
	sshConfigPath := m.getSSHConfigPath()

	data, err := os.ReadFile(sshConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	hostName := fmt.Sprintf("ggo-%s", envName)
	newConfig := m.removeSSHConfigEntry(string(data), hostName)

	return os.WriteFile(sshConfigPath, []byte(newConfig), 0600)
}

func (m *Manager) removeSSHConfigEntry(config, hostName string) string {
	lines := strings.Split(config, "\n")
	var result []string
	inEntry := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check if this is the start of our entry
		if strings.HasPrefix(trimmed, "# GPU Go Studio Environment:") {
			inEntry = true
			continue
		}
		if strings.HasPrefix(trimmed, "Host "+hostName) {
			inEntry = true
			continue
		}

		// Check if we're exiting the entry
		if inEntry && (strings.HasPrefix(trimmed, "Host ") || strings.HasPrefix(trimmed, "# ")) {
			inEntry = false
		}

		if !inEntry {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

func (m *Manager) getSSHConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ssh", "config")
}

// State management

func (m *Manager) getStatePath() string {
	return filepath.Join(m.paths.ConfigDir(), "studios.json")
}

func (m *Manager) loadState() (map[string]*Environment, error) {
	data, err := os.ReadFile(m.getStatePath())
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*Environment), nil
		}
		return nil, err
	}

	var state map[string]*Environment
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return state, nil
}

func (m *Manager) saveState(state map[string]*Environment) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(m.getStatePath()), 0755); err != nil {
		return err
	}

	return os.WriteFile(m.getStatePath(), data, 0644)
}

func (m *Manager) saveEnvironment(env *Environment) error {
	state, err := m.loadState()
	if err != nil {
		state = make(map[string]*Environment)
	}

	state[env.ID] = env
	return m.saveState(state)
}

func (m *Manager) removeEnvironment(id string) error {
	state, err := m.loadState()
	if err != nil {
		return nil
	}

	delete(state, id)
	return m.saveState(state)
}
