// Package mavis calls routes that reach the app-server dispatcher outside the
// DesktopService IDL registrar.
//
// The 0.7.8 dispatcher stubs /mavis/health, /mavis/api/version, and
// /mavis/api/update. Named methods cover the dispatcher leaves that were
// recovered, including the agent routes cTu actually calls. GET
// /mavis/api/agent/{name} is implemented on routeResolvedAgent as "GET " but
// cTu only calls routeAgentApi when the path has more than two segments, so
// that info route is not reached. Responses whose schema was not recovered
// stay raw. Drive paths stay on desktop.Client.
package mavis

import (
	"context"
	"encoding/json"
	"net/url"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// Client uses a DesktopService tunnel to reach dispatcher routes.
type Client struct {
	d *desktop.Client
}

// New wraps a desktop client. The same tunnel is used; these calls are not
// part of the 470 IDL methods.
func New(d *desktop.Client) *Client { return &Client{d: d} }

// Health is the local stub from the dispatcher.
type Health struct {
	Status       string          `json:"status"`
	Runtime      string          `json:"runtime"`
	StartupToken string          `json:"startupToken,omitempty"`
	Raw          json.RawMessage `json:"-"`
}

// Version is the local stub. latestVersion is JSON null on 0.7.8.
type Version struct {
	Version         string          `json:"version"`
	LatestVersion   *string         `json:"latestVersion"`
	UpdateAvailable bool            `json:"updateAvailable"`
	UpdateCommand   string          `json:"updateCommand"`
	Raw             json.RawMessage `json:"-"`
}

// UpdateAck is the local stub body. Calling it does not start an updater in
// 0.7.8. The SDK never calls it unless the application does.
type UpdateAck struct {
	OK  bool            `json:"ok"`
	Raw json.RawMessage `json:"-"`
}

// RawDocument keeps a JSON body whose fields are not a closed schema.
type RawDocument struct {
	Raw json.RawMessage
}

// Health calls GET /mavis/health.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var out Health
	raw, err := c.get(ctx, "/mavis/health")
	if err != nil {
		return out, err
	}
	out.Raw = raw
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	out.Raw = raw
	return out, nil
}

// Version calls GET /mavis/api/version.
func (c *Client) Version(ctx context.Context) (Version, error) {
	var out Version
	raw, err := c.get(ctx, "/mavis/api/version")
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	out.Raw = raw
	return out, nil
}

// UpdateStub calls POST /mavis/api/update. On 0.7.8 the dispatcher returns
// {ok:true} without starting an update loop.
func (c *Client) UpdateStub(ctx context.Context) (UpdateAck, error) {
	var out UpdateAck
	raw, err := c.call(ctx, "POST", "/mavis/api/update", nil)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	out.Raw = raw
	return out, nil
}

// RuntimeCapabilities calls GET /mavis/api/runtime/capabilities.
func (c *Client) RuntimeCapabilities(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/runtime/capabilities")
}

// RuntimeStatus calls GET /mavis/api/runtime/status.
func (c *Client) RuntimeStatus(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/runtime/status")
}

// RuntimeDoctor calls GET /mavis/api/runtime/doctor.
func (c *Client) RuntimeDoctor(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/runtime/doctor")
}

// MigrationCheckQuery is the query string for the migration check route.
type MigrationCheckQuery struct {
	AgentName string
	Verbose   bool
}

// RuntimeMigrationCheck calls GET /mavis/api/runtime/migration/check.
func (c *Client) RuntimeMigrationCheck(ctx context.Context, q MigrationCheckQuery) (RawDocument, error) {
	values := url.Values{}
	if q.AgentName != "" {
		values.Set("agentName", q.AgentName)
	}
	if q.Verbose {
		values.Set("verbose", "true")
	}
	path := "/mavis/api/runtime/migration/check"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return c.getDoc(ctx, path)
}

// GetConfig calls GET /mavis/api/config. The document shape is not closed.
func (c *Client) GetConfig(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/config")
}

// ConfigAPIKeyStatus calls GET /mavis/api/config/api-key-status.
func (c *Client) ConfigAPIKeyStatus(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/config/api-key-status")
}

// PermissionModeOptions calls GET /mavis/api/config/permission-mode-options.
func (c *Client) PermissionModeOptions(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/config/permission-mode-options")
}

// LLMProviderDiscovery calls GET /mavis/api/external/llm-provider-discovery.
func (c *Client) LLMProviderDiscovery(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/external/llm-provider-discovery")
}

// Usage calls GET /mavis/api/usage.
func (c *Client) Usage(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/usage")
}

// UsageSummary calls GET /mavis/api/usage/summary.
func (c *Client) UsageSummary(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/usage/summary")
}

// PutConfig calls PUT /mavis/api/config with a caller-supplied document.
// The SDK does not invent config fields and does not call this by itself.
func (c *Client) PutConfig(ctx context.Context, body json.RawMessage) (RawDocument, error) {
	return c.callDoc(ctx, "PUT", "/mavis/api/config", body)
}

// LegacySessionSearch calls GET /mavis/api/session/search. On 0.7.8 the
// session fallthrough answers 503 Local Runtime V1 Conversation is unavailable.
// A 503 is returned as an error with the response body attached when the
// tunnel reports a non-2xx status.
func (c *Client) LegacySessionSearch(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/session/search")
}

// Raw calls a dispatcher path that does not have a named method yet.
func (c *Client) Raw(ctx context.Context, method, path string, body json.RawMessage) (desktop.HTTPResponse, error) {
	headers := map[string]string{}
	if len(body) > 0 {
		headers["content-type"] = "application/json"
	}
	return c.d.RawCall(ctx, method, path, headers, body)
}

func (c *Client) get(ctx context.Context, path string) (json.RawMessage, error) {
	resp, err := c.d.RawCall(ctx, "GET", path, map[string]string{"accept": "application/json"}, nil)
	if err != nil {
		return nil, err
	}
	if err := resp.StatusError(methodOf(path)); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), resp.Body...), nil
}

func (c *Client) call(ctx context.Context, method, path string, body []byte) (json.RawMessage, error) {
	headers := map[string]string{"accept": "application/json"}
	if len(body) > 0 {
		headers["content-type"] = "application/json"
	}
	resp, err := c.d.RawCall(ctx, method, path, headers, body)
	if err != nil {
		return nil, err
	}
	if err := resp.StatusError(method + " " + path); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), resp.Body...), nil
}

func (c *Client) getDoc(ctx context.Context, path string) (RawDocument, error) {
	raw, err := c.get(ctx, path)
	if err != nil {
		return RawDocument{}, err
	}
	return RawDocument{Raw: raw}, nil
}

func (c *Client) callDoc(ctx context.Context, method, path string, body json.RawMessage) (RawDocument, error) {
	raw, err := c.call(ctx, method, path, body)
	if err != nil {
		return RawDocument{}, err
	}
	return RawDocument{Raw: raw}, nil
}

func methodOf(path string) string { return "GET " + path }
