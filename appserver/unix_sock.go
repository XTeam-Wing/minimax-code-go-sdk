//go:build !windows

package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/internal/ws"
)

// HealthStatus is the JSON body of GET /readyz and GET /healthz.
type HealthStatus struct {
	OK              bool            `json:"ok"`
	AuthMode        string          `json:"authMode,omitempty"`
	InstanceID      string          `json:"instanceId,omitempty"`
	ProtocolVersion int             `json:"protocolVersion,omitempty"`
	Version         string          `json:"version,omitempty"`
	Region          string          `json:"region,omitempty"`
	BuildEnv        string          `json:"buildEnv,omitempty"`
	Lane            string          `json:"lane,omitempty"`
	Raw             json.RawMessage `json:"-"`
}

type wsLink struct {
	conn *ws.Conn
}

func dialUnix(ctx context.Context, path string) (link, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	wsConn, err := ws.Handshake(conn, "mcode.local")
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &wsLink{conn: wsConn}, nil
}

func (w *wsLink) ReadMessage() ([]byte, bool, error) { return w.conn.ReadMessage() }
func (w *wsLink) WriteMessage(payload []byte) error  { return w.conn.WriteMessage(payload, false) }
func (w *wsLink) Close() error                       { return w.conn.Close() }

func getHealth(ctx context.Context, socketPath, urlPath string) (HealthStatus, error) {
	var zero HealthStatus
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return zero, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	req := fmt.Sprintf("GET %s HTTP/1.1\r\nHost: mcode.local\r\nConnection: close\r\n\r\n", urlPath)
	if _, err := io.WriteString(conn, req); err != nil {
		return zero, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return zero, err
	}
	if resp.StatusCode != http.StatusOK {
		return zero, fmt.Errorf("appserver: %s returned HTTP %d", urlPath, resp.StatusCode)
	}
	if err := json.Unmarshal(body, &zero); err != nil {
		return zero, err
	}
	zero.Raw = append(json.RawMessage(nil), body...)
	return zero, nil
}

// Readyz calls GET /readyz on the unix listener.
func (c *Client) Readyz(ctx context.Context) (HealthStatus, error) {
	return c.health(ctx, "/readyz")
}

// Healthz calls GET /healthz on the unix listener.
func (c *Client) Healthz(ctx context.Context) (HealthStatus, error) {
	return c.health(ctx, "/healthz")
}

func (c *Client) health(ctx context.Context, path string) (HealthStatus, error) {
	if strings.TrimSpace(c.socketPath) == "" {
		return HealthStatus{}, ErrHealthUnsupported
	}
	return getHealth(ctx, c.socketPath, path)
}
