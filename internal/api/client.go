package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/go-resty/resty/v2"
	"k8s.io/klog/v2"
)

const (
	defaultBaseURL = "https://go.gpu.tf"
	defaultTimeout = 30 * time.Second
)

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
// The base URL defaults to GPU_GO_ENDPOINT env var if set, otherwise https://tensor-fusion.ai
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		baseURL:    GetDefaultBaseURL(),
		httpClient: resty.New().SetTimeout(defaultTimeout),
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
