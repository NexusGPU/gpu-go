package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gorilla/websocket"
	"k8s.io/klog/v2"
)

const (
	defaultBaseURL = "https://go.gpu.tf"
	defaultTimeout = 30 * time.Second
)

// HeartbeatMode defines the connection mode for heartbeat
type HeartbeatMode string

const (
	HeartbeatModeWebSocket HeartbeatMode = "websocket"
	HeartbeatModePolling   HeartbeatMode = "polling"
)

// GetDefaultHeartbeatMode returns the default heartbeat mode from env var or websocket
func GetDefaultHeartbeatMode() HeartbeatMode {
	if mode := os.Getenv("GPU_GO_HEARTBEAT_MODE"); mode != "" {
		switch mode {
		case "polling", "long_polling":
			return HeartbeatModePolling
		case "websocket", "ws":
			return HeartbeatModeWebSocket
		}
	}
	return HeartbeatModePolling
}

// GetDefaultBaseURL returns the default base URL, checking GPU_GO_ENDPOINT env var first
func GetDefaultBaseURL() string {
	if endpoint := os.Getenv("GPU_GO_ENDPOINT"); endpoint != "" {
		return endpoint
	}
	return defaultBaseURL
}

// Client is the HTTP client for GPU Go API
type Client struct {
	baseURL     string
	httpClient  *resty.Client
	userToken   string
	agentSecret string

	// Heartbeat configuration
	heartbeatMode HeartbeatMode

	// WebSocket connection
	wsConn      *websocket.Conn
	wsMu        sync.Mutex
	wsStopCh    chan struct{}
	wsAgentID   string
	wsReconnect bool

	// Long polling state
	lpMu        sync.Mutex
	lpStopCh    chan struct{}
	lpAgentID   string
	lpReconnect bool
}

// ClientOption is a function that configures the client
type ClientOption func(*Client)

// WithBaseURL sets the base URL for the client
func WithBaseURL(url string) ClientOption {
	return func(c *Client) {
		c.baseURL = url
	}
}

// WithUserToken sets the user token for authentication
func WithUserToken(token string) ClientOption {
	return func(c *Client) {
		c.userToken = token
	}
}

// WithAgentSecret sets the agent secret for authentication
func WithAgentSecret(secret string) ClientOption {
	return func(c *Client) {
		c.agentSecret = secret
	}
}

// WithHTTPClient sets a custom HTTP client
func WithHTTPClient(httpClient *resty.Client) ClientOption {
	return func(c *Client) {
		c.httpClient = httpClient
	}
}

// WithHeartbeatMode sets the heartbeat connection mode (websocket or longpolling)
func WithHeartbeatMode(mode HeartbeatMode) ClientOption {
	return func(c *Client) {
		c.heartbeatMode = mode
	}
}

// NewClient creates a new API client
// The base URL defaults to GPU_GO_ENDPOINT env var if set, otherwise https://tensor-fusion.ai
// Heartbeat mode defaults to GPU_GO_HEARTBEAT_MODE env var if set, otherwise websocket
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		baseURL:       GetDefaultBaseURL(),
		httpClient:    resty.New().SetTimeout(defaultTimeout),
		heartbeatMode: GetDefaultHeartbeatMode(),
		wsStopCh:      make(chan struct{}),
		lpStopCh:      make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// SetBaseURL sets the base URL for the client
func (c *Client) SetBaseURL(url string) {
	c.baseURL = url
}

// GetBaseURL returns the base URL for the client
func (c *Client) GetBaseURL() string {
	return c.baseURL
}

// SetUserToken sets the user token for authentication
func (c *Client) SetUserToken(token string) {
	c.userToken = token
}

// SetAgentSecret sets the agent secret for authentication
func (c *Client) SetAgentSecret(secret string) {
	c.agentSecret = secret
}

func (c *Client) userAuthHeader() string {
	return "Bearer " + c.userToken
}

func (c *Client) agentAuthHeader() string {
	return "Bearer " + c.agentSecret
}

// authType constants for request helpers
type authType int

const (
	authNone authType = iota
	authUser
	authAgent
	authCustom
)

// doGet performs a GET request with the specified auth type
func doGet[T any](c *Client, ctx context.Context, path string, auth authType, customAuth string) (*T, error) {
	klog.Infof("doGet: path=%s, auth=%d", path, auth)
	var resp T
	req := c.httpClient.R().
		SetContext(ctx).
		SetResult(&resp)

	switch auth {
	case authUser:
		req.SetHeader("Authorization", c.userAuthHeader())
	case authAgent:
		req.SetHeader("Authorization", c.agentAuthHeader())
	case authCustom:
		req.SetHeader("Authorization", customAuth)
	}

	httpResp, err := req.Get(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("request failed: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// doPost performs a POST request with the specified auth type and body
func doPost[T any](c *Client, ctx context.Context, path string, body any, auth authType, customAuth string, acceptedCodes ...int) (*T, error) {
	var resp T
	req := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		SetResult(&resp)

	switch auth {
	case authUser:
		req.SetHeader("Authorization", c.userAuthHeader())
	case authAgent:
		req.SetHeader("Authorization", c.agentAuthHeader())
	case authCustom:
		req.SetHeader("Authorization", customAuth)
	}

	httpResp, err := req.Post(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// Default accepted codes
	if len(acceptedCodes) == 0 {
		acceptedCodes = []int{http.StatusOK, http.StatusCreated}
	}

	statusOk := false
	for _, code := range acceptedCodes {
		if httpResp.StatusCode() == code {
			statusOk = true
			break
		}
	}
	if !statusOk {
		return nil, fmt.Errorf("request failed: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// doPostNoResponse performs a POST request that doesn't return a body
func doPostNoResponse(c *Client, ctx context.Context, path string, body any, auth authType) error {
	klog.Infof("doPost: path=%s, body=%+v, auth=%d", path, body, auth)
	req := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body)

	switch auth {
	case authUser:
		req.SetHeader("Authorization", c.userAuthHeader())
	case authAgent:
		req.SetHeader("Authorization", c.agentAuthHeader())
	}

	httpResp, err := req.Post(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return fmt.Errorf("request failed: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return nil
}

// doPatch performs a PATCH request
func doPatch[T any](c *Client, ctx context.Context, path string, body any, auth authType) (*T, error) {
	klog.Infof("doPatch: path=%s, body=%+v, auth=%d", path, body, auth)
	var resp T
	req := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Content-Type", "application/json").
		SetBody(body).
		SetResult(&resp)

	switch auth {
	case authUser:
		req.SetHeader("Authorization", c.userAuthHeader())
	case authAgent:
		req.SetHeader("Authorization", c.agentAuthHeader())
	}

	httpResp, err := req.Patch(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("request failed: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// doDelete performs a DELETE request
func doDelete(c *Client, ctx context.Context, path string, auth authType) error {
	klog.Infof("doDelete: path=%s, auth=%d", path, auth)
	req := c.httpClient.R().
		SetContext(ctx)

	switch auth {
	case authUser:
		req.SetHeader("Authorization", c.userAuthHeader())
	case authAgent:
		req.SetHeader("Authorization", c.agentAuthHeader())
	}

	httpResp, err := req.Delete(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK && httpResp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("request failed: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return nil
}

// --- Token APIs ---

// GenerateToken generates a temporary installation token
func (c *Client) GenerateToken(ctx context.Context, tokenType string) (*TokenResponse, error) {
	req := map[string]string{"type": tokenType}
	return doPost[TokenResponse](c, ctx, "/api/v1/tokens/generate", req, authUser, "", http.StatusOK)
}

// --- Agent APIs ---

// RegisterAgent registers an agent with the server
func (c *Client) RegisterAgent(ctx context.Context, tempToken string, req *AgentRegisterRequest) (*AgentRegisterResponse, error) {
	return doPost[AgentRegisterResponse](c, ctx, "/api/v1/agents/register", req, authCustom, "Bearer "+tempToken)
}

// ListAgents lists all agents for the current user
func (c *Client) ListAgents(ctx context.Context) (*AgentListResponse, error) {
	return doGet[AgentListResponse](c, ctx, "/api/v1/agents", authUser, "")
}

// GetAgent gets a single agent by ID
func (c *Client) GetAgent(ctx context.Context, agentID string) (*AgentInfo, error) {
	return doGet[AgentInfo](c, ctx, "/api/v1/agents/"+agentID, authUser, "")
}

// DeleteAgent deletes an agent
func (c *Client) DeleteAgent(ctx context.Context, agentID string) error {
	return doDelete(c, ctx, "/api/v1/agents/"+agentID, authUser)
}

// GetAgentConfig gets the agent configuration
func (c *Client) GetAgentConfig(ctx context.Context, agentID string) (*AgentConfigResponse, error) {
	return doGet[AgentConfigResponse](c, ctx, "/api/v1/agents/"+agentID+"/config", authAgent, "")
}

// ReportAgentStatus reports the agent status to the server and returns the response
func (c *Client) ReportAgentStatus(ctx context.Context, agentID string, req *AgentStatusRequest) (*AgentStatusResponse, error) {
	return doPost[AgentStatusResponse](c, ctx, "/api/v1/agents/"+agentID+"/status", req, authAgent, "")
}

// ReportAgentMetrics reports the agent metrics to the server
func (c *Client) ReportAgentMetrics(ctx context.Context, agentID string, req *AgentMetricsRequest) error {
	return doPostNoResponse(c, ctx, "/api/v1/agents/"+agentID+"/metrics", req, authAgent)
}

// --- Worker APIs ---

// CreateWorker creates a new worker
func (c *Client) CreateWorker(ctx context.Context, req *WorkerCreateRequest) (*WorkerInfo, error) {
	return doPost[WorkerInfo](c, ctx, "/api/v1/workers", req, authUser, "")
}

// ListWorkers lists all workers for the current user
func (c *Client) ListWorkers(ctx context.Context, agentID, hostname string) (*WorkerListResponse, error) {
	var resp WorkerListResponse

	req := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		SetResult(&resp)

	if agentID != "" {
		req.SetQueryParam("agent_id", agentID)
	}
	if hostname != "" {
		req.SetQueryParam("hostname", hostname)
	}

	httpResp, err := req.Get(c.baseURL + "/api/v1/workers")
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("request failed: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// GetWorker gets a single worker by ID
func (c *Client) GetWorker(ctx context.Context, workerID string) (*WorkerInfo, error) {
	return doGet[WorkerInfo](c, ctx, "/api/v1/workers/"+workerID, authUser, "")
}

// UpdateWorker updates a worker
func (c *Client) UpdateWorker(ctx context.Context, workerID string, req *WorkerUpdateRequest) (*WorkerInfo, error) {
	return doPatch[WorkerInfo](c, ctx, "/api/v1/workers/"+workerID, req, authUser)
}

// DeleteWorker deletes a worker
func (c *Client) DeleteWorker(ctx context.Context, workerID string) error {
	return doDelete(c, ctx, "/api/v1/workers/"+workerID, authUser)
}

// --- Share APIs ---

// CreateShare creates a new share link
func (c *Client) CreateShare(ctx context.Context, req *ShareCreateRequest) (*ShareInfo, error) {
	return doPost[ShareInfo](c, ctx, "/api/v1/shares", req, authUser, "")
}

// ListShares lists all shares for the current user
func (c *Client) ListShares(ctx context.Context) (*ShareListResponse, error) {
	return doGet[ShareListResponse](c, ctx, "/api/v1/shares", authUser, "")
}

// GetSharePublic gets public share information by short code
func (c *Client) GetSharePublic(ctx context.Context, shortCode string) (*SharePublicInfo, error) {
	return doGet[SharePublicInfo](c, ctx, "/s/"+shortCode, authNone, "")
}

// DeleteShare deletes a share
func (c *Client) DeleteShare(ctx context.Context, shareID string) error {
	return doDelete(c, ctx, "/api/v1/shares/"+shareID, authUser)
}

// --- Ecosystem/Releases APIs ---

// GetReleases fetches middleware releases from the ecosystem API
// vendor: optional vendor slug (e.g., "nvidia")
// size: optional number of results (default: 10, max: 500)
func (c *Client) GetReleases(ctx context.Context, vendor string, size int) (*ReleasesResponse, error) {
	var resp ReleasesResponse

	req := c.httpClient.R().
		SetContext(ctx).
		SetResult(&resp)

	if vendor != "" {
		req.SetQueryParam("vendor", vendor)
	}
	if size > 0 {
		req.SetQueryParam("size", fmt.Sprintf("%d", size))
	}

	httpResp, err := req.Get(c.baseURL + "/api/ecosystem/releases")
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("request failed: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// --- WebSocket APIs ---

// WebSocket backoff constants
var (
	wsInitialBackoff         = 1 * time.Minute
	wsMaxBackoff             = 60 * time.Minute
	wsHeartbeatInterval      = 2 * time.Minute
	wsReadTimeout            = 30 * time.Second
	wsHandshakeTimeout       = 10 * time.Second
	wsMaxConsecutiveFailures = 3
)

// Long polling constants
var (
	lpInitialBackoff         = 1 * time.Minute
	lpMaxBackoff             = 60 * time.Minute
	lpPollTimeout            = 90 * time.Second // Server-side timeout, client should reconnect before this
	lpMaxConsecutiveFailures = 3
)

// HeartbeatHandler is a callback function for heartbeat responses
type HeartbeatHandler func(resp *HeartbeatResponse)

// StartHeartbeat starts the heartbeat loop with automatic reconnection
// Uses WebSocket or Long Polling based on client configuration
func (c *Client) StartHeartbeat(ctx context.Context, agentID string, handler HeartbeatHandler) error {
	mode := c.heartbeatMode
	if mode == "" {
		mode = GetDefaultHeartbeatMode()
	}

	switch mode {
	case HeartbeatModePolling:
		return c.startPolling(ctx, agentID, handler)
	case HeartbeatModeWebSocket:
		fallthrough
	default:
		return c.startWebSocket(ctx, agentID, handler)
	}
}

// startWebSocket starts the WebSocket heartbeat loop
func (c *Client) startWebSocket(ctx context.Context, agentID string, handler HeartbeatHandler) error {
	c.wsMu.Lock()
	c.wsAgentID = agentID
	c.wsReconnect = true
	// Reinitialize stop channel in case client is reused
	c.wsStopCh = make(chan struct{})
	c.wsMu.Unlock()

	// Start heartbeat loop with reconnection support (including initial connection)
	// The loop will handle initial connection and retries
	go c.heartbeatLoop(ctx, handler)

	return nil
}

// connectWebSocket establishes a WebSocket connection
func (c *Client) connectWebSocket(ctx context.Context) error {
	c.wsMu.Lock()
	agentID := c.wsAgentID
	// Close existing connection if any
	if c.wsConn != nil {
		_ = c.wsConn.Close()
		c.wsConn = nil
	}
	c.wsMu.Unlock()

	wsURL := c.baseURL
	if len(wsURL) > 5 && wsURL[:5] == "https" {
		wsURL = "wss" + wsURL[5:]
	} else if len(wsURL) > 4 && wsURL[:4] == "http" {
		wsURL = "ws" + wsURL[4:]
	}
	wsURL = wsURL + "/ws/" + agentID

	dialer := websocket.Dialer{
		HandshakeTimeout: wsHandshakeTimeout,
	}

	header := http.Header{}
	header.Set("Authorization", c.agentAuthHeader())

	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return err
	}

	c.wsMu.Lock()
	c.wsConn = conn
	c.wsMu.Unlock()

	klog.Infof("WebSocket connected: agent_id=%s", agentID)
	return nil
}

// reconnectWithBackoff attempts to reconnect with exponential backoff
// Returns true if reconnection succeeded, false if context was cancelled or reconnection disabled
func (c *Client) reconnectWithBackoff(ctx context.Context, currentBackoff *time.Duration) bool {
	for {
		c.wsMu.Lock()
		shouldReconnect := c.wsReconnect
		c.wsMu.Unlock()

		if !shouldReconnect {
			return false
		}

		select {
		case <-ctx.Done():
			return false
		case <-c.wsStopCh:
			return false
		default:
		}

		klog.Infof("Attempting WebSocket reconnection: backoff=%v", *currentBackoff)

		// Wait before reconnecting
		select {
		case <-ctx.Done():
			return false
		case <-c.wsStopCh:
			return false
		case <-time.After(*currentBackoff):
		}

		// Attempt reconnection
		if err := c.connectWebSocket(ctx); err != nil {
			klog.Errorf("WebSocket reconnection failed, will retry: error=%v backoff=%v", err, *currentBackoff)

			// Increase backoff for next attempt (exponential: 1min, 2min, 4min, 8min, ... 60min max)
			*currentBackoff = min(*currentBackoff*2, wsMaxBackoff)
			continue
		}

		// Reconnection successful, reset backoff
		*currentBackoff = wsInitialBackoff
		klog.Info("WebSocket reconnection successful")
		return true
	}
}

// heartbeatLoop sends heartbeats and receives responses with automatic reconnection
func (c *Client) heartbeatLoop(ctx context.Context, handler HeartbeatHandler) {
	ticker := time.NewTicker(wsHeartbeatInterval)
	defer ticker.Stop()

	backoff := wsInitialBackoff
	consecutiveFailures := 0

	// Attempt initial connection before starting ticker
	c.wsMu.Lock()
	conn := c.wsConn
	c.wsMu.Unlock()

	if conn == nil {
		// No initial connection, attempt reconnect
		if !c.reconnectWithBackoff(ctx, &backoff) {
			return
		}
		consecutiveFailures = 0
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.wsStopCh:
			return
		case <-ticker.C:
			c.wsMu.Lock()
			conn := c.wsConn
			c.wsMu.Unlock()

			if conn == nil {
				// No connection, attempt reconnect
				if !c.reconnectWithBackoff(ctx, &backoff) {
					return
				}
				consecutiveFailures = 0
				continue
			}

			// Send ping
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				klog.Errorf("Failed to send heartbeat ping: error=%v", err)
				consecutiveFailures++

				if consecutiveFailures >= wsMaxConsecutiveFailures {
					klog.Warningf("Too many consecutive failures, reconnecting: failures=%d", consecutiveFailures)

					if !c.reconnectWithBackoff(ctx, &backoff) {
						return
					}
					consecutiveFailures = 0
				}
				continue
			}

			// Set read deadline
			if err := conn.SetReadDeadline(time.Now().Add(wsReadTimeout)); err != nil {
				klog.Errorf("Failed to set read deadline: error=%v", err)
			}

			// Read response
			_, message, err := conn.ReadMessage()
			if err != nil {
				klog.Errorf("Failed to read heartbeat response: error=%v", err)
				consecutiveFailures++

				// Check if this is a connection error that requires reconnection
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
					websocket.IsUnexpectedCloseError(err) ||
					consecutiveFailures >= wsMaxConsecutiveFailures {
					klog.Warningf("Connection error detected, reconnecting: failures=%d", consecutiveFailures)

					if !c.reconnectWithBackoff(ctx, &backoff) {
						return
					}
					consecutiveFailures = 0
				}
				continue
			}

			// Success - reset failure tracking and backoff
			consecutiveFailures = 0
			backoff = wsInitialBackoff

			var resp HeartbeatResponse
			if err := json.Unmarshal(message, &resp); err != nil {
				klog.Errorf("Failed to parse heartbeat response: error=%v", err)
				continue
			}

			if handler != nil {
				handler(&resp)
			}
		}
	}
}

// StopHeartbeat stops the heartbeat and closes the connection (WebSocket or Long Polling)
func (c *Client) StopHeartbeat() {
	// Stop WebSocket if active
	c.wsMu.Lock()
	c.wsReconnect = false
	shouldCloseWS := c.wsConn != nil
	wsStopCh := c.wsStopCh
	c.wsMu.Unlock()

	if shouldCloseWS {
		// Signal stop - use select to avoid panic if already closed
		select {
		case <-wsStopCh:
			// Already closed
		default:
			close(wsStopCh)
		}

		c.wsMu.Lock()
		if c.wsConn != nil {
			// Send close message before closing
			_ = c.wsConn.WriteMessage(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
			)
			_ = c.wsConn.Close()
			c.wsConn = nil
		}
		c.wsMu.Unlock()
	}

	// Stop Long Polling if active
	c.lpMu.Lock()
	c.lpReconnect = false
	shouldCloseLP := c.lpAgentID != ""
	lpStopCh := c.lpStopCh
	c.lpMu.Unlock()

	if shouldCloseLP {
		// Signal stop - use select to avoid panic if already closed
		select {
		case <-lpStopCh:
			// Already closed
		default:
			close(lpStopCh)
		}
	}
}

// startPolling starts the long polling heartbeat loop
func (c *Client) startPolling(ctx context.Context, agentID string, handler HeartbeatHandler) error {
	c.lpMu.Lock()
	c.lpAgentID = agentID
	c.lpReconnect = true
	// Reinitialize stop channel in case client is reused
	c.lpStopCh = make(chan struct{})
	c.lpMu.Unlock()

	// Start long polling loop with reconnection support
	go c.pollingLoop(ctx, handler)

	return nil
}

// pollingLoop continuously polls the server for heartbeat responses
func (c *Client) pollingLoop(ctx context.Context, handler HeartbeatHandler) {
	backoff := lpInitialBackoff
	consecutiveFailures := 0

	// Create a 60-second ticker for regular polling when healthy
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	// Perform a single poll attempt
	doPoll := func() (shouldRetry bool, retryDelay time.Duration) {
		c.lpMu.Lock()
		agentID := c.lpAgentID
		shouldReconnect := c.lpReconnect
		c.lpMu.Unlock()

		if !shouldReconnect {
			return false, 0
		}

		// Perform long polling request
		resp, err := c.pollHeartbeat(ctx, agentID)
		if err != nil {
			klog.Errorf("Long polling heartbeat failed: error=%v", err)
			consecutiveFailures++

			if consecutiveFailures >= lpMaxConsecutiveFailures {
				klog.Warningf("Too many consecutive failures, backing off: failures=%d backoff=%v", consecutiveFailures, backoff)

				// Increase backoff for next attempt
				currentBackoff := backoff
				backoff = min(backoff*2, lpMaxBackoff)
				consecutiveFailures = 0
				// Return true to retry after backoff
				return true, currentBackoff
			}
			// For failures below threshold, retry after a short delay (5 seconds)
			return true, 5 * time.Second
		}

		// Success - reset failure tracking and backoff
		consecutiveFailures = 0
		backoff = lpInitialBackoff

		if handler != nil && resp != nil {
			handler(resp)
		}
		return false, 0
	}

	// Initial poll with retry loop
	for {
		shouldRetry, delay := doPoll()
		if !shouldRetry {
			break
		}
		// Wait before retrying
		select {
		case <-ctx.Done():
			return
		case <-c.lpStopCh:
			return
		case <-time.After(delay):
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.lpStopCh:
			return
		case <-ticker.C:
			// Regular polling interval - poll and handle retries
			for {
				shouldRetry, delay := doPoll()
				if !shouldRetry {
					break
				}
				// Wait before retrying
				select {
				case <-ctx.Done():
					return
				case <-c.lpStopCh:
					return
				case <-time.After(delay):
				}
			}
		}
	}
}

// pollHeartbeat performs a single long polling HTTP request
func (c *Client) pollHeartbeat(ctx context.Context, agentID string) (*HeartbeatResponse, error) {
	url := c.baseURL + "/api/v1/ws-poll/" + agentID

	// Create a context with timeout for the long poll
	pollCtx, cancel := context.WithTimeout(ctx, lpPollTimeout)
	defer cancel()

	var resp HeartbeatResponse
	httpResp, err := c.httpClient.R().
		SetContext(pollCtx).
		SetHeader("Authorization", c.agentAuthHeader()).
		SetResult(&resp).
		Get(url)

	if err != nil {
		// Check if it's a context timeout (expected for long polling)
		if pollCtx.Err() == context.DeadlineExceeded {
			// Server didn't respond within timeout, this is normal for long polling
			// Return nil response but no error to continue polling
			return nil, nil
		}
		return nil, fmt.Errorf("long polling request failed: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("long polling heartbeat failed: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}
