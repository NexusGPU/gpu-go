package api

import "time"

// TokenResponse represents the response from POST /api/v1/tokens/generate
type TokenResponse struct {
	Token          string    `json:"token"`
	ExpiresAt      time.Time `json:"expires_at"`
	InstallCommand string    `json:"install_command"`
}

// GPUInfo represents GPU information for agent registration
type GPUInfo struct {
	GPUID         string `json:"gpu_id"`
	GPUIndex      int    `json:"gpu_index"`
	Vendor        string `json:"vendor"`
	Model         string `json:"model"`
	VRAMMb        int64  `json:"vram_mb"`
	DriverVersion string `json:"driver_version,omitempty"`
	CUDAVersion   string `json:"cuda_version,omitempty"`
}

// AgentRegisterRequest represents the request body for agent registration
type AgentRegisterRequest struct {
	Token      string    `json:"token"`
	Hostname   string    `json:"hostname"`
	OS         string    `json:"os"`
	Arch       string    `json:"arch"`
	GPUs       []GPUInfo `json:"gpus"`
	NetworkIPs []string  `json:"network_ips"`
}

// License represents the license information
type License struct {
	Plain     string `json:"plain"`
	Encrypted string `json:"encrypted"`
}

// AgentRegisterResponse represents the response from agent registration
type AgentRegisterResponse struct {
	AgentID     string  `json:"agent_id"`
	AgentSecret string  `json:"agent_secret"`
	License     License `json:"license"`
}

// AgentInfo represents agent information
type AgentInfo struct {
	AgentID    string       `json:"agent_id"`
	Hostname   string       `json:"hostname"`
	Status     string       `json:"status"`
	OS         string       `json:"os"`
	Arch       string       `json:"arch"`
	NetworkIPs []string     `json:"network_ips,omitempty"`
	GPUs       []GPUInfo    `json:"gpus,omitempty"`
	Workers    []WorkerInfo `json:"workers,omitempty"`
	GPUCount   int          `json:"gpu_count,omitempty"`
	GPUSummary string       `json:"gpu_summary,omitempty"`
	LastSeenAt time.Time    `json:"last_seen_at"`
	CreatedAt  time.Time    `json:"created_at"`
}

// AgentListResponse represents the response from GET /api/v1/agents
type AgentListResponse struct {
	Agents []AgentInfo `json:"agents"`
}

// WorkerConfig represents worker configuration from server
type WorkerConfig struct {
	WorkerID       string   `json:"worker_id"`
	GPUIDs         []string `json:"gpu_ids"`
	GPUIndices     []int    `json:"gpu_indices,omitempty"`
	VRAMMb         int64    `json:"vram_mb,omitempty"`
	ComputePercent int      `json:"compute_percent,omitempty"`
	IsolationMode  string   `json:"isolation_mode,omitempty"`
	ListenPort     int      `json:"listen_port"`
	Enabled        bool     `json:"enabled"`
	ShareCodes     []string `json:"share_codes,omitempty"`
}

// AgentConfigResponse represents the response from GET /api/v1/agents/{agent_id}/config
type AgentConfigResponse struct {
	ConfigVersion int            `json:"config_version"`
	Workers       []WorkerConfig `json:"workers"`
	License       License        `json:"license"`
}

// GPUStatus represents GPU status for status report
type GPUStatus struct {
	GPUID         string  `json:"gpu_id"`
	GPUIndex      int     `json:"gpu_index"`
	UsedByWorker  *string `json:"used_by_worker"`
	Vendor        string  `json:"vendor"`
	Model         string  `json:"model"`
	VRAMMb        int64   `json:"vram_mb"`
	DriverVersion string  `json:"driver_version,omitempty"`
	CUDAVersion   string  `json:"cuda_version,omitempty"`
	GPUChanged    bool    `json:"gpu_changed,omitempty"`
}

// ConnectionInfo represents client connection information
type ConnectionInfo struct {
	ClientIP    string    `json:"client_ip"`
	ConnectedAt time.Time `json:"connected_at"`
}

// WorkerStatus represents worker status for status report
type WorkerStatus struct {
	WorkerID    string           `json:"worker_id"`
	Status      string           `json:"status"`
	PID         int              `json:"pid,omitempty"`
	Restarts    int              `json:"restarts,omitempty"`
	GPUIDs      []string         `json:"gpu_ids"`
	GPUIndices  []int            `json:"gpu_indices,omitempty"`
	Connections []ConnectionInfo `json:"connections,omitempty"`
	// Optimization flags - only update DB when these are true
	WorkerChanged     *bool `json:"worker_changed,omitempty"`     // true if status/pid/restarts/gpu_ids changed
	ConnectionChanged *bool `json:"connection_changed,omitempty"` // true if connections changed
	GPUChanged        *bool `json:"gpu_changed,omitempty"`        // true if vendor/model/vram/driver/cuda changed
}

// AgentStatusEvent represents special events in status report
type AgentStatusEvent string

const (
	// AgentStatusEventShutdown indicates the agent is shutting down
	AgentStatusEventShutdown AgentStatusEvent = "shutdown"
)

// AgentStatusRequest represents the request body for agent status report
type AgentStatusRequest struct {
	Timestamp time.Time        `json:"timestamp"`
	GPUs      []GPUStatus      `json:"gpus"`
	Workers   []WorkerStatus   `json:"workers"`
	Event     AgentStatusEvent `json:"event,omitempty"`
	// License optimization - client reports current license expiration
	// Server only regenerates if < 10 minutes remaining
	LicenseExpiration *int64 `json:"license_expiration,omitempty"` // Unix timestamp in milliseconds
}

// AgentStatusResponse represents the response from agent status report
type AgentStatusResponse struct {
	Success          bool                `json:"success"`
	ConfigVersion    int                 `json:"config_version"`
	License          *License            `json:"license,omitempty"` // null if no regeneration needed
	WorkerShareCodes map[string][]string `json:"worker_share_codes,omitempty"` // workerID -> []shareCode
}

// SuccessResponse represents a simple success response
type SuccessResponse struct {
	Success bool `json:"success"`
}

// WorkerInfo represents worker information
type WorkerInfo struct {
	WorkerID      string           `json:"worker_id"`
	AgentID       string           `json:"agent_id,omitempty"`
	AgentHostname string           `json:"agent_hostname,omitempty"`
	Name          string           `json:"name"`
	GPUIDs        []string         `json:"gpu_ids"`
	GPUIndices    []int            `json:"gpu_indices,omitempty"`
	GPUs          []GPUInfo        `json:"gpus,omitempty"`
	ListenPort    int              `json:"listen_port"`
	Enabled       bool             `json:"enabled"`
	IsDefault     bool             `json:"is_default,omitempty"`
	Status        string           `json:"status"`
	PID           int              `json:"pid,omitempty"`
	Restarts      int              `json:"restarts,omitempty"`
	Connections   []ConnectionInfo `json:"connections,omitempty"`
	StartedAt     *time.Time       `json:"started_at,omitempty"`
	CreatedAt     time.Time        `json:"created_at,omitempty"`
}

// WorkerCreateRequest represents the request body for worker creation
type WorkerCreateRequest struct {
	AgentID    string   `json:"agent_id"`
	Name       string   `json:"name"`
	GPUIDs     []string `json:"gpu_ids"`
	ListenPort int      `json:"listen_port"`
	Enabled    bool     `json:"enabled"`
}

// WorkerUpdateRequest represents the request body for worker update
type WorkerUpdateRequest struct {
	Name       *string  `json:"name,omitempty"`
	GPUIDs     []string `json:"gpu_ids,omitempty"`
	ListenPort *int     `json:"listen_port,omitempty"`
	Enabled    *bool    `json:"enabled,omitempty"`
}

// WorkerListResponse represents the response from GET /api/v1/workers
type WorkerListResponse struct {
	Workers []WorkerInfo `json:"workers"`
}

// ShareInfo represents share link information
type ShareInfo struct {
	ShareID        string     `json:"share_id"`
	ShortCode      string     `json:"short_code"`
	ShortLink      string     `json:"short_link"`
	WorkerID       string     `json:"worker_id"`
	HardwareVendor string     `json:"hardware_vendor"`
	ConnectionURL  string     `json:"connection_url"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	MaxUses        *int       `json:"max_uses,omitempty"`
	UsedCount      int        `json:"used_count"`
	CreatedAt      time.Time  `json:"created_at"`
}

// ShareCreateRequest represents the request body for share creation
type ShareCreateRequest struct {
	WorkerID     string     `json:"worker_id"`
	ConnectionIP string     `json:"connection_ip"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	MaxUses      *int       `json:"max_uses,omitempty"`
}

// ShareListResponse represents the response from GET /api/v1/shares
type ShareListResponse struct {
	Shares []ShareInfo `json:"shares"`
}

// SharePublicInfo represents public share information
type SharePublicInfo struct {
	WorkerID       string `json:"worker_id"`
	HardwareVendor string `json:"hardware_vendor"`
	ConnectionURL  string `json:"connection_url"`
}

// SystemMetrics represents system metrics for metrics report
type SystemMetrics struct {
	CPUUsage      float64 `json:"cpu_usage"`
	MemoryUsedMb  int64   `json:"memory_used_mb"`
	MemoryTotalMb int64   `json:"memory_total_mb"`
}

// GPUMetrics represents GPU metrics for metrics report
type GPUMetrics struct {
	GPUID       string  `json:"gpu_id"`
	Utilization float64 `json:"utilization"`
	VRAMUsedMb  int64   `json:"vram_used_mb"`
	VRAMTotalMb int64   `json:"vram_total_mb"`
	Temperature float64 `json:"temperature"`
	PowerUsageW float64 `json:"power_usage_w"`
}

// AgentMetricsRequest represents the request body for agent metrics report
type AgentMetricsRequest struct {
	Timestamp time.Time     `json:"timestamp"`
	System    SystemMetrics `json:"system"`
	GPUs      []GPUMetrics  `json:"gpus"`
}

// HeartbeatResponse represents the response from WebSocket heartbeat
type HeartbeatResponse struct {
	ConfigVersion int `json:"config_version"`
}

// IsolationModeType mirrors tensor-fusion's IsolationModeType
type IsolationModeType = string

// Isolation mode constants - use utils.IsolationMode* for canonical values
const (
	IsolationModeShared      IsolationModeType = "shared"
	IsolationModeSoft        IsolationModeType = "soft"
	IsolationModePartitioned IsolationModeType = "partitioned"
)

// ToIsolationMode converts a string to IsolationModeType
// Deprecated: Use utils.ToTFIsolationMode for tensor-fusion types
func ToIsolationMode(s string) IsolationModeType {
	switch s {
	case "soft":
		return IsolationModeSoft
	case "partitioned":
		return IsolationModePartitioned
	default:
		return IsolationModeShared
	}
}

// ReleaseArtifact represents a downloadable artifact for a release
type ReleaseArtifact struct {
	CPUArch  string            `json:"cpuArch"`
	OS       string            `json:"os"`
	URL      string            `json:"url"`
	SHA256   string            `json:"sha256"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ReleaseRequirements represents version requirements for a release
type ReleaseRequirements struct {
	MinTensorFusionVersion string `json:"minTensorFusionVersion,omitempty"`
	MinDriverVersion       string `json:"minDriverVersion,omitempty"`
}

// VendorInfo represents vendor information in a release
type VendorInfo struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// ReleaseInfo represents a middleware release from the API
type ReleaseInfo struct {
	ID           string              `json:"id"`
	Vendor       VendorInfo          `json:"vendor"`
	Version      string              `json:"version"`
	ReleaseType  string              `json:"releaseType"`
	ReleaseDate  time.Time           `json:"releaseDate"`
	Artifacts    []ReleaseArtifact   `json:"artifacts"`
	Requirements ReleaseRequirements `json:"requirements"`
	IsLatest     bool                `json:"isLatest"`
}

// ReleasesResponse represents the response from GET /api/ecosystem/releases
type ReleasesResponse struct {
	Releases []ReleaseInfo `json:"releases"`
	Count    int           `json:"count"`
}
