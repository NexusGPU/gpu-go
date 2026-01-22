# GPU Go REST API Schema

## Types

### Token
```typescript
TokenType: "agent_install"
GenerateTokenRequest: { type: TokenType }
GenerateTokenResponse: {
  token_id: string
  token: string
  expires_at: string (ISO8601)
  install_command: string
}
```

### Personal Access Token (PAT)
```typescript
CreatePatRequest: { expires_in_days: number (positive int) | null }
PatResponse: {
  token: string
  expires_at: string (ISO8601) | null
  created_at: string (ISO8601)
}
PatInfo: {
  exists: boolean
  expires_at: string (ISO8601) | null
  last_used_at: string (ISO8601) | null
  created_at: string (ISO8601) | null
}
```

### GPU
```typescript
GpuInfo: {
  gpu_id: string
  vendor: "nvidia" | "amd"
  model: string
  vram_mb: number (positive int)
  driver_version?: string
  cuda_version?: string
}
GpuStatus: {
  gpu_id: string
  used_by_worker: string | null
}
```

### Agent
```typescript
AgentStatus: "pending" | "online" | "offline"
AgentRegisterRequest: {
  hostname: string (1-255 chars)
  os: string (1-20 chars)
  arch: string (1-20 chars)
  gpus: GpuInfo[] (min 1)
  network_ips?: string[]
}
AgentRegisterResponse: {
  agent_id: string
  agent_secret: string
  license: { plain: string, encrypted: string }
}
AgentListItem: {
  agent_id: string
  hostname: string
  status: AgentStatus
  os: string
  arch: string
  gpuCount: number
  last_seen_at: string (ISO8601) | null
  created_at: string (ISO8601)
}
```

### Worker
```typescript
WorkerStatus: "pending" | "running" | "stopping" | "stopped"
WorkerConnection: {
  client_ip: string
  connected_at: string (ISO8601)
}
CreateWorkerRequest: {
  agent_id: string
  name: string (1-100 chars)
  gpu_ids: string[] (min 1)
  listen_port: number (1024-65535)
  enabled?: boolean (default: true)
}
UpdateWorkerRequest: {
  name?: string (1-100 chars)
  gpu_ids?: string[] (min 1)
  listen_port?: number (1024-65535)
  enabled?: boolean
}
WorkerListItem: {
  worker_id: string
  agent_id: string
  name: string
  gpu_ids: string[]
  listen_port: number
  enabled: boolean
  status: WorkerStatus
  created_at: string (ISO8601)
}
WorkerConfig: {
  worker_id: string
  gpu_ids: string[]
  vram_mb?: number (int)
  compute_percent?: number (1-100)
  isolation_mode?: "soft" | "hard"
  listen_port: number
  enabled: boolean
}
```

### Status Report
```typescript
WorkerStatusReport: {
  worker_id: string
  status: WorkerStatus
  pid?: number (int)
  gpu_ids: string[]
  connections?: WorkerConnection[]
}
AgentStatusReportRequest: {
  timestamp: string (ISO8601)
  gpus: GpuStatus[]
  workers: WorkerStatusReport[]
}
```

### Metrics
```typescript
GpuMetrics: {
  gpu_id: string
  utilization: number (0-100)
  vram_used_mb: number (int)
  vram_total_mb: number (int)
  temperature: number (int)
  power_usage_w: number
}
SystemMetrics: {
  cpu_usage: number (0-100)
  memory_used_mb: number (int)
  memory_total_mb: number (int)
}
AgentMetricsRequest: {
  timestamp: string (ISO8601)
  system: SystemMetrics
  gpus: GpuMetrics[]
}
```

### Share
```typescript
CreateShareRequest: {
  worker_id: string
  connection_ip: string (min 1)
  expires_at?: string (ISO8601)
  max_uses?: number (positive int)
}
ShareListItem: {
  share_id: string
  short_code: string
  short_link: string
  worker_id: string
  hardware_vendor: string
  connection_url: string
  expires_at: string (ISO8601) | null
  max_uses: number | null
  used_count: number
  created_at: string (ISO8601)
}
PublicShareInfo: {
  worker_id: string
  hardware_vendor: string
  connection_url: string
}
```

### Config
```typescript
AgentConfigResponse: {
  config_version: number (int)
  workers: WorkerConfig[]
  license: { plain: string, encrypted: string }
}
```

## Endpoints

### Tokens

#### POST /api/v1/tokens/generate
**Auth:** User session  
**Input:** `GenerateTokenRequest`  
**Output:** `GenerateTokenResponse` (201)  
**Constraints:** Token expires in 30 minutes

#### GET /api/v1/tokens/{tokenId}/status
**Auth:** User session  
**Path:** `tokenId: string`  
**Output:** `{ token_id, status: "active" | "used" | "expired", expires_at, used_at, is_expired, agent? }`

#### GET /api/v1/tokens/pat
**Auth:** User session  
**Output:** `PatInfo`

#### POST /api/v1/tokens/pat
**Auth:** User session  
**Input:** `CreatePatRequest`  
**Output:** `PatResponse` (201)  
**Constraints:** Singleton per user (regenerates existing)

#### DELETE /api/v1/tokens/pat
**Auth:** User session  
**Output:** `{ success: boolean }`

### Agents

#### GET /api/v1/agents
**Auth:** User session  
**Output:** `{ agents: AgentListItem[] }`

#### GET /api/v1/agents/{agent_id}
**Auth:** User session  
**Path:** `agent_id: string`  
**Output:** `{ agent_id, hostname, status, os, arch, network_ips, gpus: [{ gpu_id, vendor, model, vram_mb, used_by_worker }], workers: [{ worker_id, is_default, status, gpu_ids, listen_port, connections }], last_seen_at, created_at }`

#### DELETE /api/v1/agents/{agent_id}
**Auth:** User session  
**Path:** `agent_id: string`  
**Output:** `{ success: boolean }`  
**Constraints:** Cascades to GPUs, workers, shares

#### POST /api/v1/agents/register
**Auth:** Temporary token (Bearer)  
**Input:** `AgentRegisterRequest`  
**Output:** `AgentRegisterResponse` (201)  
**Constraints:** 
- Validates GPU count against plan limits
- Creates default worker
- Marks token as used
- License valid 120 minutes

#### POST /api/v1/agents/{agent_id}/status
**Auth:** Agent secret (Bearer)  
**Path:** `agent_id: string`  
**Input:** `AgentStatusReportRequest`  
**Output:** `{ success: boolean, config_version: number, license }`  
**Constraints:** Updates agent last_seen_at, worker statuses, GPU associations, connections

#### GET /api/v1/agents/{agent_id}/config
**Auth:** Agent secret (Bearer)  
**Path:** `agent_id: string`  
**Output:** `AgentConfigResponse`  
**Constraints:** Returns fresh license (120 min validity)

#### POST /api/v1/agents/{agent_id}/metrics
**Auth:** Agent secret (Bearer)  
**Path:** `agent_id: string`  
**Input:** `AgentMetricsRequest`  
**Output:** `{ success: boolean }`  
**Constraints:** Pro/Team plans only

### Workers

#### GET /api/v1/workers
**Auth:** User session  
**Query:** `?agent_id=string&hostname=string`  
**Output:** `{ workers: WorkerListItem[] }`

#### POST /api/v1/workers
**Auth:** User session  
**Input:** `CreateWorkerRequest`  
**Output:** `WorkerListItem` (201)  
**Constraints:** 
- Validates GPU IDs belong to agent
- Checks plan worker limits
- Increments agent config_version

#### GET /api/v1/workers/{worker_id}
**Auth:** User session  
**Path:** `worker_id: string`  
**Output:** `{ worker_id, agent_id, name, gpu_ids, listen_port, enabled, status, connections: WorkerConnection[], created_at }`

#### PATCH /api/v1/workers/{worker_id}
**Auth:** User session  
**Path:** `worker_id: string`  
**Input:** `UpdateWorkerRequest`  
**Output:** `{ worker_id, name, gpu_ids, listen_port, enabled, status }`  
**Constraints:** 
- Validates GPU IDs if updating
- Sets status to "stopping" if disabling running worker
- Increments agent config_version

#### DELETE /api/v1/workers/{worker_id}
**Auth:** User session  
**Path:** `worker_id: string`  
**Output:** `{ success: boolean }`  
**Constraints:** Increments agent config_version

### Shares

#### GET /api/v1/shares
**Auth:** User session  
**Output:** `{ shares: ShareListItem[] }`

#### POST /api/v1/shares
**Auth:** User session  
**Input:** `CreateShareRequest`  
**Output:** `ShareListItem` (201)  
**Constraints:** 
- Validates worker ownership
- Generates unique short_code
- Derives hardware_vendor from worker GPUs

#### DELETE /api/v1/shares/{share_id}
**Auth:** User session  
**Path:** `share_id: string`  
**Output:** `{ success: boolean }`

### Public Share

#### GET /s/{short_code}
**Auth:** None (public)  
**Path:** `short_code: string`  
**Output:** `PublicShareInfo`  
**Errors:** 
- 404: Share not found
- 410: Expired or max uses reached
**Constraints:** Increments used_count on access

## Authentication

- **User session:** Cookie-based session (dashboard users)
- **Temporary token:** Bearer token for agent installation (30 min expiry)
- **Agent secret:** Bearer token for agent operations
- **PAT:** Bearer token for API access (user-managed)

## Plan Limits

- **Free:** Limited workers/GPUs
- **Personal:** Higher limits
- **Team:** Highest limits + metrics
- **Pro:** Similar to Team

## Notes

- All timestamps are ISO8601 strings
- Agent offline threshold: 3 minutes since last_seen_at
- License refresh: 120 minutes validity, refreshed via status/config endpoints
- Config version increments trigger agent config sync
- GPU IDs are strings like "GPU-0", "GPU-1" (not database IDs)
- Short links format: `{SHORT_LINK_BASE}/s/{short_code}`
- Connection URLs format: `tcp://{ip}:{port}`
