//go:build windows

package appserver

import (
	"context"
	"errors"
)

// HealthStatus is the JSON body of GET /readyz and GET /healthz.
type HealthStatus struct {
	OK              bool   `json:"ok"`
	AuthMode        string `json:"authMode,omitempty"`
	InstanceID      string `json:"instanceId,omitempty"`
	ProtocolVersion int    `json:"protocolVersion,omitempty"`
	Version         string `json:"version,omitempty"`
	Region          string `json:"region,omitempty"`
	BuildEnv        string `json:"buildEnv,omitempty"`
	Lane            string `json:"lane,omitempty"`
	Raw             []byte `json:"-"`
}

func dialUnix(ctx context.Context, path string) (link, error) {
	return nil, errors.New("appserver: unix sockets are not supported on windows")
}

func getHealth(ctx context.Context, socketPath, urlPath string) (HealthStatus, error) {
	return HealthStatus{}, errors.New("appserver: unix sockets are not supported on windows")
}

// Readyz is only available on the unix listener.
func (c *Client) Readyz(ctx context.Context) (HealthStatus, error) {
	return HealthStatus{}, ErrHealthUnsupported
}

// Healthz is only available on the unix listener.
func (c *Client) Healthz(ctx context.Context) (HealthStatus, error) {
	return HealthStatus{}, ErrHealthUnsupported
}
