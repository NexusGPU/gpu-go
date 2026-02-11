package studio

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/NexusGPU/gpu-go/internal/platform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockBackend is a mock implementation of the Backend interface
type MockBackend struct {
	mode       Mode
	available  bool
	envs       map[string]*Environment
	createFunc func(ctx context.Context, opts *CreateOptions) (*Environment, error)
	startFunc  func(ctx context.Context, envID string) error
	stopFunc   func(ctx context.Context, envID string) error
	removeFunc func(ctx context.Context, envID string) error
	getFunc    func(ctx context.Context, idOrName string) (*Environment, error)
	listFunc   func(ctx context.Context) ([]*Environment, error)
	execFunc   func(ctx context.Context, envID string, cmd []string) ([]byte, error)
	logsFunc   func(ctx context.Context, envID string, follow bool) (<-chan string, error)
}

func (m *MockBackend) Name() string                         { return string(m.mode) }
func (m *MockBackend) Mode() Mode                           { return m.mode }
func (m *MockBackend) IsAvailable(ctx context.Context) bool { return m.available }

func (m *MockBackend) Create(ctx context.Context, opts *CreateOptions) (*Environment, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, opts)
	}
	env := &Environment{
		ID:        "env-123",
		Name:      opts.Name,
		Mode:      m.mode,
		Status:    StatusRunning,
		CreatedAt: time.Now(),
	}
	if m.envs == nil {
		m.envs = make(map[string]*Environment)
	}
	m.envs[env.ID] = env
	return env, nil
}

func (m *MockBackend) Start(ctx context.Context, envID string) error {
	if m.startFunc != nil {
		return m.startFunc(ctx, envID)
	}
	if env, ok := m.envs[envID]; ok {
		env.Status = StatusRunning
		return nil
	}
	return fmt.Errorf("env not found")
}

func (m *MockBackend) Stop(ctx context.Context, envID string) error {
	if m.stopFunc != nil {
		return m.stopFunc(ctx, envID)
	}
	if env, ok := m.envs[envID]; ok {
		env.Status = StatusStopped
		return nil
	}
	return fmt.Errorf("env not found")
}

func (m *MockBackend) Remove(ctx context.Context, envID string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, envID)
	}
	if _, ok := m.envs[envID]; ok {
		delete(m.envs, envID)
		return nil
	}
	return fmt.Errorf("env not found")
}

func (m *MockBackend) List(ctx context.Context) ([]*Environment, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	var envs []*Environment
	for _, env := range m.envs {
		envs = append(envs, env)
	}
	return envs, nil
}

func (m *MockBackend) Get(ctx context.Context, idOrName string) (*Environment, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, idOrName)
	}
	for _, env := range m.envs {
		if env.ID == idOrName || env.Name == idOrName {
			return env, nil
		}
	}
	return nil, fmt.Errorf("env not found")
}

func (m *MockBackend) Exec(ctx context.Context, envID string, cmd []string) ([]byte, error) {
	if m.execFunc != nil {
		return m.execFunc(ctx, envID, cmd)
	}
	return []byte("output"), nil
}

func (m *MockBackend) Logs(ctx context.Context, envID string, follow bool) (<-chan string, error) {
	if m.logsFunc != nil {
		return m.logsFunc(ctx, envID, follow)
	}
	ch := make(chan string)
	close(ch)
	return ch, nil
}

func TestManager_RegisterAndGetBackend(t *testing.T) {
	m := NewManager()

	backend := &MockBackend{mode: ModeDocker, available: true}
	m.RegisterBackend(backend)

	b, err := m.GetBackend(ModeDocker)
	require.NoError(t, err)
	assert.Equal(t, backend, b)

	_, err = m.GetBackend(ModeWSL)
	assert.Error(t, err)
}

func TestManager_DetectBestBackend(t *testing.T) {
	m := NewManager()

	// Register multiple backends
	docker := &MockBackend{mode: ModeDocker, available: true}
	wsl := &MockBackend{mode: ModeWSL, available: false}
	m.RegisterBackend(docker)
	m.RegisterBackend(wsl)

	// Should prefer available backend
	// Note: preference order depends on OS, but Docker is usually fallback
	// For testing, we just check if it returns an available one
	b, err := m.GetBackend(ModeAuto)
	require.NoError(t, err)
	assert.Equal(t, ModeDocker, b.Mode())

	// If no backend available
	m = NewManager()
	docker.available = false
	m.RegisterBackend(docker)

	_, err = m.GetBackend(ModeAuto)
	assert.Error(t, err)
}

func TestManager_CreateEnvironment(t *testing.T) {
	// Override paths for testing state persistence
	m := &Manager{
		paths:    platform.DefaultPaths(), // We can't easily override platform paths internal dir structure
		backends: make(map[Mode]Backend),
	}

	// Hack: To test persistence properly without polluting real config,
	// we would need to mock platform.Paths or override getStatePath.
	// Since getStatePath uses m.paths.ConfigDir(), we can try to override m.paths if we could create a custom one.
	// But platform.DefaultPaths() creates a struct with private fields.

	// Let's create a custom manager that points to temp dir for state file
	// We can't change getStatePath behavior easily.
	// However, we can test that Create calls Backend.Create.

	backend := &MockBackend{mode: ModeDocker, available: true}
	m.RegisterBackend(backend)

	opts := &CreateOptions{
		Name: "test-env",
		Mode: ModeDocker,
	}

	env, err := m.Create(context.Background(), opts)
	require.NoError(t, err)
	assert.Equal(t, "test-env", env.Name)
	assert.Equal(t, ModeDocker, env.Mode)

	// Verify backend state
	assert.Len(t, backend.envs, 1)
}

func TestManager_GetEnvironment(t *testing.T) {
	m := NewManager()
	backend := &MockBackend{
		mode:      ModeDocker,
		available: true,
		envs: map[string]*Environment{
			"env-1": {ID: "env-1", Name: "my-env", Mode: ModeDocker},
		},
	}
	m.RegisterBackend(backend)

	// Get by ID
	env, err := m.Get(context.Background(), "env-1")
	require.NoError(t, err)
	assert.Equal(t, "my-env", env.Name)

	// Get by Name
	env, err = m.Get(context.Background(), "my-env")
	require.NoError(t, err)
	assert.Equal(t, "env-1", env.ID)

	// Not found
	_, err = m.Get(context.Background(), "unknown")
	assert.Error(t, err)
}

func TestManager_Lifecycle(t *testing.T) {
	m := NewManager()
	backend := &MockBackend{
		mode:      ModeDocker,
		available: true,
		envs: map[string]*Environment{
			"env-1": {ID: "env-1", Name: "my-env", Mode: ModeDocker, Status: StatusRunning},
		},
	}
	m.RegisterBackend(backend)

	ctx := context.Background()

	// Stop
	err := m.Stop(ctx, "env-1")
	require.NoError(t, err)
	assert.Equal(t, StatusStopped, backend.envs["env-1"].Status)

	// Start
	err = m.Start(ctx, "env-1")
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, backend.envs["env-1"].Status)

	// Remove
	err = m.Remove(ctx, "env-1")
	require.NoError(t, err)
	assert.NotContains(t, backend.envs, "env-1")
}

func TestManager_ListAvailableBackends(t *testing.T) {
	m := NewManager()
	m.RegisterBackend(&MockBackend{mode: ModeDocker, available: true})
	m.RegisterBackend(&MockBackend{mode: ModeWSL, available: false})

	backends := m.ListAvailableBackends(context.Background())
	assert.Len(t, backends, 1)
	assert.Equal(t, ModeDocker, backends[0].Mode())
}

func TestFormatContainerCommand(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty command",
			input:    []string{},
			expected: nil,
		},
		{
			name:     "nil command",
			input:    nil,
			expected: nil,
		},
		{
			name:     "multiple args - passed as-is",
			input:    []string{"sh", "-c", "sleep 1d"},
			expected: []string{"sh", "-c", "sleep 1d"},
		},
		{
			name:     "single simple command without shell chars",
			input:    []string{"sleep"},
			expected: []string{"sleep"},
		},
		{
			name:     "single shell command with space - wrapped",
			input:    []string{"sh -c 'sleep 1d'"},
			expected: []string{"sh", "-c", "sh -c 'sleep 1d'"},
		},
		{
			name:     "single command with pipe - wrapped",
			input:    []string{"echo hello | cat"},
			expected: []string{"sh", "-c", "echo hello | cat"},
		},
		{
			name:     "single command with && - wrapped",
			input:    []string{"sleep 1d && echo done"},
			expected: []string{"sh", "-c", "sleep 1d && echo done"},
		},
		{
			name:     "single command with semicolon - wrapped",
			input:    []string{"echo hello; sleep 1d"},
			expected: []string{"sh", "-c", "echo hello; sleep 1d"},
		},
		{
			name:     "single command with quotes - wrapped",
			input:    []string{`echo "hello world"`},
			expected: []string{"sh", "-c", `echo "hello world"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := FormatContainerCommand(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
