package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/gorilla/websocket"
	"github.com/rs/zerolog/log"
)

const (
	defaultBaseURL = "https://api.gpu.tf"
	defaultTimeout = 30 * time.Second
)

// Client is the HTTP client for GPU Go API
type Client struct {
	baseURL     string
	httpClient  *resty.Client
	userToken   string
	agentSecret string

	// WebSocket connection
	wsConn      *websocket.Conn
	wsMu        sync.Mutex
	wsStopCh    chan struct{}
	wsAgentID   string
	wsReconnect bool
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

// NewClient creates a new API client
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    defaultBaseURL,
		httpClient: resty.New().SetTimeout(defaultTimeout),
		wsStopCh:   make(chan struct{}),
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

// --- Token APIs ---

// GenerateToken generates a temporary installation token
func (c *Client) GenerateToken(ctx context.Context, tokenType string) (*TokenResponse, error) {
	var resp TokenResponse
	req := map[string]string{"type": tokenType}

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		SetResult(&resp).
		Post(c.baseURL + "/api/v1/tokens/generate")

	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to generate token: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// --- Agent APIs ---

// RegisterAgent registers an agent with the server
func (c *Client) RegisterAgent(ctx context.Context, tempToken string, req *AgentRegisterRequest) (*AgentRegisterResponse, error) {
	var resp AgentRegisterResponse

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+tempToken).
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		SetResult(&resp).
		Post(c.baseURL + "/api/v1/agents/register")

	if err != nil {
		return nil, fmt.Errorf("failed to register agent: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK && httpResp.StatusCode() != http.StatusCreated {
		return nil, fmt.Errorf("failed to register agent: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// ListAgents lists all agents for the current user
func (c *Client) ListAgents(ctx context.Context) (*AgentListResponse, error) {
	var resp AgentListResponse

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		SetResult(&resp).
		Get(c.baseURL + "/api/v1/agents")

	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to list agents: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// GetAgent gets a single agent by ID
func (c *Client) GetAgent(ctx context.Context, agentID string) (*AgentInfo, error) {
	var resp AgentInfo

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		SetResult(&resp).
		Get(c.baseURL + "/api/v1/agents/" + agentID)

	if err != nil {
		return nil, fmt.Errorf("failed to get agent: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to get agent: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// DeleteAgent deletes an agent
func (c *Client) DeleteAgent(ctx context.Context, agentID string) error {
	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		Delete(c.baseURL + "/api/v1/agents/" + agentID)

	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK && httpResp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("failed to delete agent: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return nil
}

// GetAgentConfig gets the agent configuration
func (c *Client) GetAgentConfig(ctx context.Context, agentID string) (*AgentConfigResponse, error) {
	var resp AgentConfigResponse

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.agentAuthHeader()).
		SetResult(&resp).
		Get(c.baseURL + "/api/v1/agents/" + agentID + "/config")

	if err != nil {
		return nil, fmt.Errorf("failed to get agent config: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to get agent config: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// ReportAgentStatus reports the agent status to the server
func (c *Client) ReportAgentStatus(ctx context.Context, agentID string, req *AgentStatusRequest) error {
	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.agentAuthHeader()).
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		Post(c.baseURL + "/api/v1/agents/" + agentID + "/status")

	if err != nil {
		return fmt.Errorf("failed to report status: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to report status: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return nil
}

// ReportAgentMetrics reports the agent metrics to the server
func (c *Client) ReportAgentMetrics(ctx context.Context, agentID string, req *AgentMetricsRequest) error {
	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.agentAuthHeader()).
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		Post(c.baseURL + "/api/v1/agents/" + agentID + "/metrics")

	if err != nil {
		return fmt.Errorf("failed to report metrics: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return fmt.Errorf("failed to report metrics: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return nil
}

// --- Worker APIs ---

// CreateWorker creates a new worker
func (c *Client) CreateWorker(ctx context.Context, req *WorkerCreateRequest) (*WorkerInfo, error) {
	var resp WorkerInfo

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		SetResult(&resp).
		Post(c.baseURL + "/api/v1/workers")

	if err != nil {
		return nil, fmt.Errorf("failed to create worker: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK && httpResp.StatusCode() != http.StatusCreated {
		return nil, fmt.Errorf("failed to create worker: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
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
		return nil, fmt.Errorf("failed to list workers: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to list workers: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// GetWorker gets a single worker by ID
func (c *Client) GetWorker(ctx context.Context, workerID string) (*WorkerInfo, error) {
	var resp WorkerInfo

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		SetResult(&resp).
		Get(c.baseURL + "/api/v1/workers/" + workerID)

	if err != nil {
		return nil, fmt.Errorf("failed to get worker: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to get worker: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// UpdateWorker updates a worker
func (c *Client) UpdateWorker(ctx context.Context, workerID string, req *WorkerUpdateRequest) (*WorkerInfo, error) {
	var resp WorkerInfo

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		SetResult(&resp).
		Patch(c.baseURL + "/api/v1/workers/" + workerID)

	if err != nil {
		return nil, fmt.Errorf("failed to update worker: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to update worker: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// DeleteWorker deletes a worker
func (c *Client) DeleteWorker(ctx context.Context, workerID string) error {
	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		Delete(c.baseURL + "/api/v1/workers/" + workerID)

	if err != nil {
		return fmt.Errorf("failed to delete worker: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK && httpResp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("failed to delete worker: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return nil
}

// --- Share APIs ---

// CreateShare creates a new share link
func (c *Client) CreateShare(ctx context.Context, req *ShareCreateRequest) (*ShareInfo, error) {
	var resp ShareInfo

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		SetHeader("Content-Type", "application/json").
		SetBody(req).
		SetResult(&resp).
		Post(c.baseURL + "/api/v1/shares")

	if err != nil {
		return nil, fmt.Errorf("failed to create share: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK && httpResp.StatusCode() != http.StatusCreated {
		return nil, fmt.Errorf("failed to create share: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// ListShares lists all shares for the current user
func (c *Client) ListShares(ctx context.Context) (*ShareListResponse, error) {
	var resp ShareListResponse

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		SetResult(&resp).
		Get(c.baseURL + "/api/v1/shares")

	if err != nil {
		return nil, fmt.Errorf("failed to list shares: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to list shares: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// GetSharePublic gets public share information by short code
func (c *Client) GetSharePublic(ctx context.Context, shortCode string) (*SharePublicInfo, error) {
	var resp SharePublicInfo

	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetResult(&resp).
		Get(c.baseURL + "/s/" + shortCode)

	if err != nil {
		return nil, fmt.Errorf("failed to get share: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("failed to get share: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return &resp, nil
}

// DeleteShare deletes a share
func (c *Client) DeleteShare(ctx context.Context, shareID string) error {
	httpResp, err := c.httpClient.R().
		SetContext(ctx).
		SetHeader("Authorization", c.userAuthHeader()).
		Delete(c.baseURL + "/api/v1/shares/" + shareID)

	if err != nil {
		return fmt.Errorf("failed to delete share: %w", err)
	}

	if httpResp.StatusCode() != http.StatusOK && httpResp.StatusCode() != http.StatusNoContent {
		return fmt.Errorf("failed to delete share: status %d, body: %s", httpResp.StatusCode(), httpResp.String())
	}

	return nil
}

// --- WebSocket APIs ---

// WebSocket backoff constants
const (
	wsInitialBackoff         = 1 * time.Minute
	wsMaxBackoff             = 60 * time.Minute
	wsHeartbeatInterval      = 2 * time.Minute
	wsReadTimeout            = 30 * time.Second
	wsHandshakeTimeout       = 10 * time.Second
	wsMaxConsecutiveFailures = 3
)

// HeartbeatHandler is a callback function for heartbeat responses
type HeartbeatHandler func(resp *HeartbeatResponse)

// StartHeartbeat starts the WebSocket heartbeat loop with automatic reconnection
func (c *Client) StartHeartbeat(ctx context.Context, agentID string, handler HeartbeatHandler) error {
	c.wsMu.Lock()
	c.wsAgentID = agentID
	c.wsReconnect = true
	// Reinitialize stop channel in case client is reused
	c.wsStopCh = make(chan struct{})
	c.wsMu.Unlock()

	// Initial connection
	if err := c.connectWebSocket(ctx); err != nil {
		return fmt.Errorf("failed to establish initial websocket connection: %w", err)
	}

	// Start heartbeat loop with reconnection support
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

	log.Info().Str("agent_id", agentID).Msg("WebSocket connected")
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

		log.Info().
			Dur("backoff", *currentBackoff).
			Msg("Attempting WebSocket reconnection")

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
			log.Error().
				Err(err).
				Dur("backoff", *currentBackoff).
				Msg("WebSocket reconnection failed, will retry")

			// Increase backoff for next attempt (exponential: 1min, 2min, 4min, 8min, ... 60min max)
			*currentBackoff = min(*currentBackoff*2, wsMaxBackoff)
			continue
		}

		// Reconnection successful, reset backoff
		*currentBackoff = wsInitialBackoff
		log.Info().Msg("WebSocket reconnection successful")
		return true
	}
}

// heartbeatLoop sends heartbeats and receives responses with automatic reconnection
func (c *Client) heartbeatLoop(ctx context.Context, handler HeartbeatHandler) {
	ticker := time.NewTicker(wsHeartbeatInterval)
	defer ticker.Stop()

	backoff := wsInitialBackoff
	consecutiveFailures := 0

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
				log.Error().Err(err).Msg("Failed to send heartbeat ping")
				consecutiveFailures++

				if consecutiveFailures >= wsMaxConsecutiveFailures {
					log.Warn().
						Int("failures", consecutiveFailures).
						Msg("Too many consecutive failures, reconnecting")

					if !c.reconnectWithBackoff(ctx, &backoff) {
						return
					}
					consecutiveFailures = 0
				}
				continue
			}

			// Set read deadline
			if err := conn.SetReadDeadline(time.Now().Add(wsReadTimeout)); err != nil {
				log.Error().Err(err).Msg("Failed to set read deadline")
			}

			// Read response
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Error().Err(err).Msg("Failed to read heartbeat response")
				consecutiveFailures++

				// Check if this is a connection error that requires reconnection
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
					websocket.IsUnexpectedCloseError(err) ||
					consecutiveFailures >= wsMaxConsecutiveFailures {
					log.Warn().
						Int("failures", consecutiveFailures).
						Msg("Connection error detected, reconnecting")

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
				log.Error().Err(err).Msg("Failed to parse heartbeat response")
				continue
			}

			if handler != nil {
				handler(&resp)
			}
		}
	}
}

// StopHeartbeat stops the WebSocket heartbeat and closes the connection
func (c *Client) StopHeartbeat() {
	c.wsMu.Lock()
	c.wsReconnect = false
	c.wsMu.Unlock()

	// Signal stop - use select to avoid panic if already closed
	select {
	case <-c.wsStopCh:
		// Already closed
	default:
		close(c.wsStopCh)
	}

	c.wsMu.Lock()
	defer c.wsMu.Unlock()

	if c.wsConn != nil {
		// Send close message before closing
		_ = c.wsConn.WriteMessage(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		)
		_ = c.wsConn.Close()
		c.wsConn = nil
	}
}
