package appserver

import (
	"fmt"
	"io"
	"os"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// ListenMode selects the app-server transport.
type ListenMode int

const (
	// ListenStdio is newline-delimited JSON on the child process stdin/stdout.
	ListenStdio ListenMode = iota
	// ListenUnix is a WebSocket on a Unix socket, plus GET /readyz and /healthz.
	ListenUnix
)

// ClientInfo is sent in initialize.
type ClientInfo struct {
	Name    string
	Title   string
	Version string
}

// Options configures Start or Dial.
type Options struct {
	// Command is the mcode executable. Start defaults it to "mcode".
	Command string
	// LeadingArgs are placed after Command and before "app-server".
	// Use this when Command is "node" and the first argument is cli.js.
	LeadingArgs []string
	Listen      ListenMode
	// SocketPath is the unix socket. Start creates a temporary path when empty.
	SocketPath string
	DataDir    string
	Workspace  string
	// AuthMode is "remote-oauth" or "desktop". Empty omits the flag.
	AuthMode string
	// Region is "cn" or "global". Empty omits the flag.
	Region    string
	StateFile string
	Env       []string
	Stderr    io.Writer
	Client    ClientInfo
	Handlers  Handlers
	// StreamWindow is the initial DesktopService credit window for SSE calls.
	// Zero selects 16. Values above 256 are rejected.
	StreamWindow int
	// MaxInFlight limits concurrent desktop requests. Zero selects 64.
	MaxInFlight int
	// StrictBaseline fails the handshake when serverInfo.version is not 0.7.8.
	StrictBaseline bool
	// Dir is the child process working directory.
	Dir string

	skipHandshake bool
}

func (o Options) normalize() (Options, error) {
	if o.Command == "" {
		o.Command = "mcode"
	}
	if o.Client.Name == "" {
		o.Client.Name = "minimax-code-go-sdk"
	}
	if o.Client.Title == "" {
		o.Client.Title = "MiniMax Code Go SDK"
	}
	if o.Client.Version == "" {
		o.Client.Version = "0.1.0"
	}
	if o.StreamWindow == 0 {
		o.StreamWindow = DefaultStreamWindow
	}
	if o.StreamWindow < 1 || o.StreamWindow > MaxStreamWindow {
		return Options{}, fmt.Errorf("appserver: stream window must be 1..%d", MaxStreamWindow)
	}
	if o.MaxInFlight == 0 {
		o.MaxInFlight = DefaultMaxInFlight
	}
	if o.MaxInFlight < 1 {
		return Options{}, fmt.Errorf("appserver: max in-flight must be positive")
	}
	switch o.AuthMode {
	case "", "remote-oauth", "desktop":
	default:
		return Options{}, fmt.Errorf("appserver: auth mode must be remote-oauth or desktop")
	}
	switch o.Region {
	case "", "cn", "global":
	default:
		return Options{}, fmt.Errorf("appserver: region must be cn or global")
	}
	switch o.Listen {
	case ListenStdio, ListenUnix:
	default:
		return Options{}, fmt.Errorf("appserver: unsupported listen mode")
	}
	if o.Env == nil {
		o.Env = os.Environ()
	}
	if o.Stderr == nil {
		o.Stderr = io.Discard
	}
	return o, nil
}

func commandArgs(o Options) []string {
	args := append([]string{}, o.LeadingArgs...)
	args = append(args, "app-server")
	switch o.Listen {
	case ListenUnix:
		args = append(args, "--listen", "unix://"+o.SocketPath, "--foreground")
	default:
		args = append(args, "--stdio", "--foreground")
	}
	if o.DataDir != "" {
		args = append(args, "--data-dir", o.DataDir)
	}
	if o.Workspace != "" {
		args = append(args, "--workspace", o.Workspace)
	}
	if o.AuthMode != "" {
		args = append(args, "--auth-mode", o.AuthMode)
	}
	if o.Region != "" {
		args = append(args, "--region", o.Region)
	}
	if o.StateFile != "" {
		args = append(args, "--state-file", o.StateFile)
	}
	return args
}

// BaselineVersion is the CLI release this client was verified against.
func BaselineVersion() string { return desktop.BaselineVersion }
