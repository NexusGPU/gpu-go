package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_GenerateToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/tokens/generate", r.URL.Path)
		assert.Equal(t, "Bearer test-user-token", r.Header.Get("Authorization"))

		resp := TokenResponse{
			Token:          "tmp_xxxxxxxxxxxx",
			ExpiresAt:      time.Now().Add(30 * time.Minute),
			InstallCommand: `GPU_GO_TOKEN="tmp_xxxxxxxxxxxx" curl -sfL https://gpu.tf/install | sh`,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUserToken("test-user-token"),
	)

	resp, err := client.GenerateToken(context.Background(), "agent_install")
	require.NoError(t, err)
	assert.Equal(t, "tmp_xxxxxxxxxxxx", resp.Token)
	assert.Contains(t, resp.InstallCommand, "tmp_xxxxxxxxxxxx")
}

func TestClient_RegisterAgent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/agents/register", r.URL.Path)
		assert.Equal(t, "Bearer tmp_xxxxxxxxxxxx", r.Header.Get("Authorization"))

		var req AgentRegisterRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "my-gpu-server", req.Hostname)
		assert.Equal(t, "linux", req.OS)
		assert.Len(t, req.GPUs, 2)

		resp := AgentRegisterResponse{
			AgentID:     "agent_xxxxxxxxxxxx",
			AgentSecret: "gpugo_xxxxxxxxxxxx",
			License: License{
				Plain:     "plyF5Usp2FKiVhWYBlxR0xQ8jkbsMtZw|pro|1768379729916",
				Encrypted: "base64_ed25519_signature_xxxx",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	req := &AgentRegisterRequest{
		Token:    "tmp_xxxxxxxxxxxx",
		Hostname: "my-gpu-server",
		OS:       "linux",
		Arch:     "amd64",
		GPUs: []GPUInfo{
			{GPUID: "GPU-0", Vendor: "nvidia", Model: "RTX 4090", VRAMMb: 24576},
			{GPUID: "GPU-1", Vendor: "nvidia", Model: "RTX 4090", VRAMMb: 24576},
		},
		NetworkIPs: []string{"192.168.1.50"},
	}

	resp, err := client.RegisterAgent(context.Background(), "tmp_xxxxxxxxxxxx", req)
	require.NoError(t, err)
	assert.Equal(t, "agent_xxxxxxxxxxxx", resp.AgentID)
	assert.Equal(t, "gpugo_xxxxxxxxxxxx", resp.AgentSecret)
	assert.NotEmpty(t, resp.License.Plain)
}

func TestClient_ListAgents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/agents", r.URL.Path)

		resp := AgentListResponse{
			Agents: []AgentInfo{
				{
					AgentID:   "agent_xxxxxxxxxxxx",
					Hostname:  "my-gpu-server",
					Status:    "online",
					OS:        "linux",
					Arch:      "amd64",
					CreatedAt: time.Now(),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUserToken("test-user-token"),
	)

	resp, err := client.ListAgents(context.Background())
	require.NoError(t, err)
	assert.Len(t, resp.Agents, 1)
	assert.Equal(t, "agent_xxxxxxxxxxxx", resp.Agents[0].AgentID)
	assert.Equal(t, "online", resp.Agents[0].Status)
}

func TestClient_GetAgentConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/agents/agent_xxxxxxxxxxxx/config", r.URL.Path)
		assert.Equal(t, "Bearer gpugo_xxxxxxxxxxxx", r.Header.Get("Authorization"))

		resp := AgentConfigResponse{
			ConfigVersion: 3,
			Workers: []WorkerConfig{
				{
					WorkerID:   "worker_yyyy",
					GPUIDs:     []string{"GPU-0"},
					ListenPort: 42345,
					Enabled:    true,
				},
			},
			License: License{
				Plain:     "plyF5Usp2FKiVhWYBlxR0xQ8jkbsMtZw|pro|1768379729916",
				Encrypted: "base64_ed25519_signature_xxxx",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithAgentSecret("gpugo_xxxxxxxxxxxx"),
	)

	resp, err := client.GetAgentConfig(context.Background(), "agent_xxxxxxxxxxxx")
	require.NoError(t, err)
	assert.Equal(t, 3, resp.ConfigVersion)
	assert.Len(t, resp.Workers, 1)
	assert.Equal(t, "worker_yyyy", resp.Workers[0].WorkerID)
}

func TestClient_ReportAgentStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/agents/agent_xxxxxxxxxxxx/status", r.URL.Path)

		var req AgentStatusRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Len(t, req.GPUs, 2)
		assert.Len(t, req.Workers, 1)

		resp := SuccessResponse{Success: true}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithAgentSecret("gpugo_xxxxxxxxxxxx"),
	)

	workerID := "worker_yyyy"
	req := &AgentStatusRequest{
		Timestamp: time.Now(),
		GPUs: []GPUStatus{
			{GPUID: "GPU-0", UsedByWorker: &workerID},
			{GPUID: "GPU-1", UsedByWorker: nil},
		},
		Workers: []WorkerStatus{
			{
				WorkerID: workerID,
				Status:   "running",
				PID:      12345,
				GPUIDs:   []string{"GPU-0"},
			},
		},
	}

	err := client.ReportAgentStatus(context.Background(), "agent_xxxxxxxxxxxx", req)
	require.NoError(t, err)
}

func TestClient_CreateWorker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/workers", r.URL.Path)

		var req WorkerCreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "agent_xxxxxxxxxxxx", req.AgentID)
		assert.Equal(t, "My Worker", req.Name)

		resp := WorkerInfo{
			WorkerID:   "worker_yyyy",
			AgentID:    req.AgentID,
			Name:       req.Name,
			GPUIDs:     req.GPUIDs,
			ListenPort: req.ListenPort,
			Enabled:    req.Enabled,
			Status:     "pending",
			CreatedAt:  time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUserToken("test-user-token"),
	)

	req := &WorkerCreateRequest{
		AgentID:    "agent_xxxxxxxxxxxx",
		Name:       "My Worker",
		GPUIDs:     []string{"GPU-0"},
		ListenPort: 9001,
		Enabled:    true,
	}

	resp, err := client.CreateWorker(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "worker_yyyy", resp.WorkerID)
	assert.Equal(t, "pending", resp.Status)
}

func TestClient_ListWorkers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/workers", r.URL.Path)

		// Check query params
		agentID := r.URL.Query().Get("agent_id")
		_ = r.URL.Query().Get("hostname") // Ignore hostname filter in mock

		workers := []WorkerInfo{
			{
				WorkerID:   "worker_yyyy",
				AgentID:    "agent_xxxxxxxxxxxx",
				Name:       "My Worker",
				GPUIDs:     []string{"GPU-0"},
				ListenPort: 9001,
				Enabled:    true,
				Status:     "running",
			},
		}

		// Filter if params provided
		if agentID != "" && agentID != "agent_xxxxxxxxxxxx" {
			workers = nil
		}

		resp := WorkerListResponse{Workers: workers}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUserToken("test-user-token"),
	)

	// Test without filters
	resp, err := client.ListWorkers(context.Background(), "", "")
	require.NoError(t, err)
	assert.Len(t, resp.Workers, 1)

	// Test with agent_id filter
	resp, err = client.ListWorkers(context.Background(), "agent_xxxxxxxxxxxx", "")
	require.NoError(t, err)
	assert.Len(t, resp.Workers, 1)
}

func TestClient_UpdateWorker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PATCH", r.Method)
		assert.Equal(t, "/api/v1/workers/worker_yyyy", r.URL.Path)

		var req WorkerUpdateRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.NotNil(t, req.Name)
		assert.Equal(t, "Updated Name", *req.Name)

		resp := WorkerInfo{
			WorkerID: "worker_yyyy",
			Name:     *req.Name,
			Status:   "running",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUserToken("test-user-token"),
	)

	name := "Updated Name"
	req := &WorkerUpdateRequest{Name: &name}

	resp, err := client.UpdateWorker(context.Background(), "worker_yyyy", req)
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", resp.Name)
}

func TestClient_DeleteWorker(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/api/v1/workers/worker_yyyy", r.URL.Path)

		resp := SuccessResponse{Success: true}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUserToken("test-user-token"),
	)

	err := client.DeleteWorker(context.Background(), "worker_yyyy")
	require.NoError(t, err)
}

func TestClient_CreateShare(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/shares", r.URL.Path)

		var req ShareCreateRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, "worker_yyyy", req.WorkerID)

		resp := ShareInfo{
			ShareID:        "share_xxxx",
			ShortCode:      "abc123",
			ShortLink:      "https://gpu.tf/s/abc123",
			WorkerID:       req.WorkerID,
			HardwareVendor: "nvidia",
			ConnectionURL:  "tcp://192.168.1.50:9001",
			UsedCount:      0,
			CreatedAt:      time.Now(),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUserToken("test-user-token"),
	)

	req := &ShareCreateRequest{
		WorkerID:     "worker_yyyy",
		ConnectionIP: "192.168.1.50",
	}

	resp, err := client.CreateShare(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "share_xxxx", resp.ShareID)
	assert.Equal(t, "abc123", resp.ShortCode)
	assert.Equal(t, "https://gpu.tf/s/abc123", resp.ShortLink)
}

func TestClient_GetSharePublic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/s/abc123", r.URL.Path)

		resp := SharePublicInfo{
			WorkerID:       "worker_yyyy",
			HardwareVendor: "nvidia",
			ConnectionURL:  "tcp://192.168.1.50:9001",
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL))

	resp, err := client.GetSharePublic(context.Background(), "abc123")
	require.NoError(t, err)
	assert.Equal(t, "worker_yyyy", resp.WorkerID)
	assert.Equal(t, "nvidia", resp.HardwareVendor)
	assert.Equal(t, "tcp://192.168.1.50:9001", resp.ConnectionURL)
}

func TestClient_ListShares(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/api/v1/shares", r.URL.Path)

		maxUses := 10
		expiresAt := time.Now().Add(24 * time.Hour)
		resp := ShareListResponse{
			Shares: []ShareInfo{
				{
					ShareID:        "share_xxxx",
					ShortCode:      "abc123",
					ShortLink:      "https://gpu.tf/s/abc123",
					WorkerID:       "worker_yyyy",
					HardwareVendor: "nvidia",
					ConnectionURL:  "tcp://192.168.1.50:9001",
					ExpiresAt:      &expiresAt,
					MaxUses:        &maxUses,
					UsedCount:      3,
					CreatedAt:      time.Now(),
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUserToken("test-user-token"),
	)

	resp, err := client.ListShares(context.Background())
	require.NoError(t, err)
	assert.Len(t, resp.Shares, 1)
	assert.Equal(t, "abc123", resp.Shares[0].ShortCode)
	assert.Equal(t, 3, resp.Shares[0].UsedCount)
}

func TestClient_DeleteShare(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/api/v1/shares/share_xxxx", r.URL.Path)

		resp := SuccessResponse{Success: true}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUserToken("test-user-token"),
	)

	err := client.DeleteShare(context.Background(), "share_xxxx")
	require.NoError(t, err)
}

func TestClient_ReportAgentMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v1/agents/agent_xxxxxxxxxxxx/metrics", r.URL.Path)

		var req AgentMetricsRequest
		json.NewDecoder(r.Body).Decode(&req)
		assert.Equal(t, 23.5, req.System.CPUUsage)
		assert.Len(t, req.GPUs, 1)

		resp := SuccessResponse{Success: true}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithAgentSecret("gpugo_xxxxxxxxxxxx"),
	)

	req := &AgentMetricsRequest{
		Timestamp: time.Now(),
		System: SystemMetrics{
			CPUUsage:      23.5,
			MemoryUsedMb:  16384,
			MemoryTotalMb: 65536,
		},
		GPUs: []GPUMetrics{
			{
				GPUID:       "GPU-0",
				Utilization: 45.2,
				VRAMUsedMb:  8192,
				VRAMTotalMb: 24576,
				Temperature: 72,
				PowerUsageW: 280,
			},
		},
	}

	err := client.ReportAgentMetrics(context.Background(), "agent_xxxxxxxxxxxx", req)
	require.NoError(t, err)
}

func TestClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	defer server.Close()

	client := NewClient(
		WithBaseURL(server.URL),
		WithUserToken("invalid-token"),
	)

	_, err := client.ListAgents(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
