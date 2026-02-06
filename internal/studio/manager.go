package studio

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
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
	case OSWindows:
		preferenceOrder = []Mode{ModeWSL, ModeDocker, ModeKubernetes}
	case OSDarwin:
		preferenceOrder = []Mode{ModeColima, ModeAppleContainer, ModeDocker, ModeKubernetes}
	case OSLinux:
		preferenceOrder = []Mode{ModeDocker, ModeColima, ModeKubernetes}
	default:
		preferenceOrder = []Mode{ModeDocker, ModeKubernetes}
	}

	// First pass: check for backends that are currently available (running)
	for _, mode := range preferenceOrder {
		if backend, ok := m.backends[mode]; ok {
			if backend.IsAvailable(ctx) {
				return backend, nil
			}
		}
	}

	// Second pass: check for auto-startable backends that are installed but not running
	for _, mode := range preferenceOrder {
		if backend, ok := m.backends[mode]; ok {
			if autoStartable, ok := backend.(AutoStartableBackend); ok {
				if autoStartable.IsInstalled(ctx) {
					// Backend is installed and can be auto-started
					return backend, nil
				}
			}
		}
	}

	return nil, errors.Unavailable("no backend available for platform: " + runtime.GOOS + ". " + platformBackendHint(ctx, runtime.GOOS))
}

// platformBackendHint returns a short hint for what to install/start on the given OS.
// On Linux, if docker is installed but fails with permission denied, suggests adding user to docker group.
func platformBackendHint(ctx context.Context, goos string) string {
	if goos == "linux" {
		if hint := linuxDockerPermissionHint(ctx); hint != "" {
			return hint
		}
		return "Install and start Docker (https://docs.docker.com/get-docker/). If you get permission denied, add your user to the docker group: sudo usermod -aG docker $USER, then log out and back in."
	}
	switch goos {
	case "darwin":
		// Check if colima is installed
		if _, err := exec.LookPath("colima"); err != nil {
			// Colima is not installed, provide installation command
			// Check if brew is available
			if _, err := exec.LookPath("brew"); err == nil {
				return "Colima is not installed. Install it with: brew install colima"
			}
			return "Colima is not installed. Install Homebrew first (https://brew.sh/), then run: brew install colima"
		}
		// Colima is installed but not running, or other backends are preferred
		return "Install and start Colima, Docker, or Apple Container runtime."
	case "windows":
		return "Install and start WSL or Docker Desktop."
	default:
		return "Install Docker or another supported runtime."
	}
}

// linuxDockerPermissionHint runs docker info and if the failure is permission-related, returns a hint.
func linuxDockerPermissionHint(ctx context.Context) string {
	cmd := exec.CommandContext(ctx, "docker", "info")
	output, _ := cmd.CombinedOutput()
	s := strings.ToLower(string(output))
	if strings.Contains(s, "permission denied") || strings.Contains(s, "got permission denied") {
		return "Docker is installed but the current user does not have permission. Add your user to the docker group: sudo usermod -aG docker $USER, then log out and back in (or run with sudo)."
	}
	return ""
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

	state, err := m.loadState()
	if err != nil {
		state = make(map[string]*Environment)
	}

	runtimeByMode := make(map[Mode][]*Environment)
	runtimeIDs := make(map[Mode]map[string]struct{})
	offlineModes := make(map[Mode]struct{})

	for _, backend := range m.backends {
		mode := backend.Mode()
		if !backend.IsAvailable(ctx) {
			offlineModes[mode] = struct{}{}
			continue
		}
		envs, err := backend.List(ctx)
		if err != nil {
			offlineModes[mode] = struct{}{}
			continue
		}
		runtimeByMode[mode] = envs
		ids := make(map[string]struct{}, len(envs))
		for _, env := range envs {
			ids[env.ID] = struct{}{}
		}
		runtimeIDs[mode] = ids
	}

	var allEnvs []*Environment
	includedIDs := make(map[string]struct{})
	for _, envs := range runtimeByMode {
		for _, env := range envs {
			allEnvs = append(allEnvs, env)
			includedIDs[env.ID] = struct{}{}
		}
	}

	stateEnvs := make([]*Environment, 0, len(state))
	for _, env := range state {
		stateEnvs = append(stateEnvs, env)
	}
	sort.Slice(stateEnvs, func(i, j int) bool {
		if stateEnvs[i].Name == stateEnvs[j].Name {
			return stateEnvs[i].ID < stateEnvs[j].ID
		}
		return stateEnvs[i].Name < stateEnvs[j].Name
	})

	for _, env := range stateEnvs {
		if _, ok := includedIDs[env.ID]; ok {
			continue
		}

		envCopy := cloneEnvironment(env)
		if _, offline := offlineModes[env.Mode]; offline {
			envCopy.Status = StatusUnknown
		} else if _, ok := runtimeIDs[env.Mode]; ok {
			envCopy.Status = StatusDeleted
		} else {
			envCopy.Status = StatusUnknown
		}
		allEnvs = append(allEnvs, envCopy)
	}

	return allEnvs, nil
}

func cloneEnvironment(env *Environment) *Environment {
	if env == nil {
		return nil
	}
	copyEnv := *env
	if env.Labels != nil {
		labels := make(map[string]string, len(env.Labels))
		for k, v := range env.Labels {
			labels[k] = v
		}
		copyEnv.Labels = labels
	}
	return &copyEnv
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
