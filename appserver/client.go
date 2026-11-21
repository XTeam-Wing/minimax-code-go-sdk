package appserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// Client is one initialized app-server connection.
type Client struct {
	opt      Options
	link     link
	proc     process
	desktopC *desktop.Client

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan inbound
	calls   map[string]*deskCall
	closed  bool
	init    InitializeResult
	idSeq   atomic.Uint64

	closeOnce sync.Once
	doneOnce  sync.Once
	done      chan struct{}
	wg        sync.WaitGroup

	socketPath string
	argv       []string
	stderr     *tailWriter
	ownedDirs  []string
}

type link interface {
	ReadMessage() (payload []byte, binary bool, err error)
	WriteMessage(payload []byte) error
	Close() error
}

// InitializeResult is the server's initialize response. Raw is the full object.
type InitializeResult struct {
	ProtocolVersion int             `json:"protocolVersion"`
	ServerInfo      ServerInfo      `json:"serverInfo"`
	WorkspaceDir    string          `json:"workspaceDir"`
	Environment     json.RawMessage `json:"environment,omitempty"`
	HostIdentity    json.RawMessage `json:"hostIdentity,omitempty"`
	Capabilities    Capabilities    `json:"capabilities"`
	Raw             json.RawMessage `json:"-"`
}

// ServerInfo identifies the app-server build.
type ServerInfo struct {
	Name    string `json:"name"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

// Capabilities is the initialize capability advertisement.
type Capabilities struct {
	Methods             []string           `json:"methods"`
	ClientNotifications []string           `json:"clientNotifications"`
	ServerNotifications []string           `json:"serverNotifications"`
	Transports          []string           `json:"transports"`
	Features            []string           `json:"features"`
	DesktopService      DesktopServiceInfo `json:"desktopService"`
	Reconnect           ReconnectInfo      `json:"reconnect"`
	Auth                AuthInfo           `json:"auth"`
}

// DesktopServiceInfo describes the tunnel the server negotiated.
type DesktopServiceInfo struct {
	Contract                        string   `json:"contract"`
	TransportMethod                 string   `json:"transportMethod"`
	TransportProtocolVersion        int      `json:"transportProtocolVersion"`
	RequestFrame                    string   `json:"requestFrame"`
	ResponseFrames                  []string `json:"responseFrames"`
	NegotiatedBinaryPayloadEncoding string   `json:"negotiatedBinaryPayloadEncoding"`
	DefaultStreamWindow             int      `json:"defaultStreamWindow"`
	MaxStreamWindow                 int      `json:"maxStreamWindow"`
}

// ReconnectInfo reports whether the server keeps history across reconnects.
// This client does not replay requests after a dropped response.
type ReconnectInfo struct {
	History bool `json:"history"`
}

// AuthInfo is the credential mode the server advertised.
type AuthInfo struct {
	DeviceAuthorization bool   `json:"deviceAuthorization"`
	CredentialTransfer  bool   `json:"credentialTransfer"`
	Mode                string `json:"mode"`
}

// Start launches mcode app-server and performs the handshake.
// Close stops the process when this client started it.
func Start(ctx context.Context, opt Options) (*Client, error) {
	opt, err := opt.normalize()
	if err != nil {
		return nil, err
	}
	proc, lnk, socket, dirs, argv, stderr, err := launch(ctx, opt)
	if err != nil {
		return nil, err
	}
	c := newClient(opt, lnk, proc)
	c.socketPath = socket
	c.ownedDirs = dirs
	c.argv = argv
	c.stderr = stderr
	c.wg.Add(1)
	go c.readLoop()
	if err := c.finishStart(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// DialStdio handshakes over an existing newline-delimited JSON stream.
// The caller owns the process. Close closes stdin when it implements io.Closer.
func DialStdio(ctx context.Context, stdout io.Reader, stdin io.Writer, opt Options) (*Client, error) {
	opt.Listen = ListenStdio
	opt, err := opt.normalize()
	if err != nil {
		return nil, err
	}
	c := newClient(opt, newStdioLink(stdout, stdin), nil)
	c.wg.Add(1)
	go c.readLoop()
	if err := c.finishStart(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func newClient(opt Options, lnk link, proc process) *Client {
	return &Client{
		opt:     opt,
		link:    lnk,
		proc:    proc,
		pending: map[string]chan inbound{},
		calls:   map[string]*deskCall{},
		done:    make(chan struct{}),
	}
}

func (c *Client) finishStart(ctx context.Context) error {
	if c.opt.skipHandshake {
		c.desktopC = desktop.New(c)
		return nil
	}
	if err := c.handshake(ctx); err != nil {
		return err
	}
	c.desktopC = desktop.New(c)
	return nil
}

// Close releases the connection. It is safe to call more than once.
// When Start created the process, Close stops it and does not restart it.
// Close does not call AbortSession. Stopping an owned process ends a running
// turn because the server exits.
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		pending, calls := c.takeClosed()
		c.closeDone()
		for _, call := range calls {
			call.fail(ErrClosed)
		}
		for _, ch := range pending {
			ch <- inbound{goErr: ErrClosed}
		}
		if c.link != nil {
			_ = c.link.Close()
		}
		if c.proc != nil {
			_ = c.proc.stop()
		}
		for _, dir := range c.ownedDirs {
			_ = removeAll(dir)
		}
	})
	c.wg.Wait()
	return nil
}

// Desktop returns the typed DesktopService client.
func (c *Client) Desktop() *desktop.Client { return c.desktopC }

// InitializeResult returns the handshake response.
func (c *Client) InitializeResult() InitializeResult { return c.init }

// ServerVersion is serverInfo.version from initialize. It may be empty.
func (c *Client) ServerVersion() string { return c.init.ServerInfo.Version }

// VersionVerified reports whether the server version is exactly 0.7.8.
func (c *Client) VersionVerified() bool {
	return c.init.ServerInfo.Version == desktop.BaselineVersion
}

// Feature reports whether initialize advertised the feature name.
func (c *Client) Feature(name string) bool {
	for _, feature := range c.init.Capabilities.Features {
		if feature == name {
			return true
		}
	}
	return false
}

// SupportsMethod reports whether initialize advertised an outer JSON-RPC method.
func (c *Client) SupportsMethod(name string) bool {
	for _, method := range c.init.Capabilities.Methods {
		if method == name {
			return true
		}
	}
	return false
}

// Argv is the child command line without credential values. Dial returns nil.
func (c *Client) Argv() []string { return append([]string(nil), c.argv...) }

// StderrTail is a redacted snapshot of the child process stderr.
func (c *Client) StderrTail() string {
	if c.stderr == nil {
		return ""
	}
	return redact(c.stderr.string())
}

func (c *Client) handshake(ctx context.Context) error {
	params := map[string]any{
		"protocolVersion": ProtocolVersion,
		"clientInfo": map[string]string{
			"name":    c.opt.Client.Name,
			"title":   c.opt.Client.Title,
			"version": c.opt.Client.Version,
		},
		"capabilities": map[string]any{
			"desktopService": map[string]any{
				"transportProtocolVersion": TransportProtocolVersion,
				"binaryPayloadEncodings":   []string{"base64"},
			},
		},
	}
	raw, err := c.call(ctx, "initialize", params)
	if err != nil {
		return err
	}
	var result InitializeResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("appserver: initialize response: %w", err)
	}
	result.Raw = append(json.RawMessage(nil), raw...)
	if result.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("%w: protocol %d", ErrUnsupportedProtocol, result.ProtocolVersion)
	}
	if result.Capabilities.DesktopService.TransportProtocolVersion != 0 &&
		result.Capabilities.DesktopService.TransportProtocolVersion != TransportProtocolVersion {
		return fmt.Errorf("%w: transport %d", ErrUnsupportedTransport, result.Capabilities.DesktopService.TransportProtocolVersion)
	}
	if c.opt.StrictBaseline && result.ServerInfo.Version != desktop.BaselineVersion {
		return fmt.Errorf("%w: got %q", ErrUnverifiedVersion, result.ServerInfo.Version)
	}
	if err := c.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	c.init = result
	return nil
}

// AccountRead calls account/read when the server advertised it.
func (c *Client) AccountRead(ctx context.Context) (json.RawMessage, error) {
	return c.call(ctx, "account/read", map[string]any{})
}

// AccountLoginStart calls account/login/start. It does not store the result.
func (c *Client) AccountLoginStart(ctx context.Context) (json.RawMessage, error) {
	return c.call(ctx, "account/login/start", map[string]any{})
}

// AccountLoginCancel calls account/login/cancel. loginID is required by the server.
func (c *Client) AccountLoginCancel(ctx context.Context, loginID string) (json.RawMessage, error) {
	if loginID == "" {
		return nil, fmt.Errorf("%w: loginId is required", ErrInvalidParams)
	}
	return c.call(ctx, "account/login/cancel", map[string]string{"loginId": loginID})
}

// AccountLogout calls account/logout. A reconnect does not repeat it.
func (c *Client) AccountLogout(ctx context.Context) (json.RawMessage, error) {
	return c.call(ctx, "account/logout", map[string]any{})
}

// PrepareRestart calls server/prepareRestart. The SDK never calls this on its own
// and does not spawn a daemon or an update loop.
func (c *Client) PrepareRestart(ctx context.Context) (json.RawMessage, error) {
	return c.call(ctx, "server/prepareRestart", map[string]any{})
}

// NotifyRuntimeAuthContext sends runtime/authContext as a notification.
func (c *Client) NotifyRuntimeAuthContext(ctx RuntimeAuthContext) error {
	if err := validateAuthContext(ctx); err != nil {
		return err
	}
	return c.notify("runtime/authContext", map[string]any{"context": ctx})
}

// RuntimeAuthContext sends runtime/authContext as a request.
func (c *Client) RuntimeAuthContext(ctx context.Context, auth RuntimeAuthContext) (json.RawMessage, error) {
	if err := validateAuthContext(auth); err != nil {
		return nil, err
	}
	return c.call(ctx, "runtime/authContext", map[string]any{"context": auth})
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if ctx == nil {
		return nil, errors.New("appserver: nil context")
	}
	id := fmt.Sprintf("c%d", c.idSeq.Add(1))
	ch := make(chan inbound, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.writeJSON(wireMessage{JSONRPC: "2.0", ID: quotedID(id), Method: method, Params: params}); err != nil {
		c.dropPending(id)
		return nil, err
	}
	select {
	case <-ctx.Done():
		c.dropPending(id)
		return nil, ctx.Err()
	case <-c.done:
		return nil, ErrClosed
	case msg, ok := <-ch:
		if !ok {
			return nil, ErrClosed
		}
		if msg.goErr != nil {
			return nil, msg.goErr
		}
		if msg.err != nil {
			return nil, msg.err
		}
		return msg.result, nil
	}
}

func (c *Client) notify(method string, params any) error {
	return c.writeJSON(wireMessage{JSONRPC: "2.0", Method: method, Params: params})
}

func (c *Client) writeJSON(msg wireMessage) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return ErrClosed
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(body) > MaxMessageBytes {
		return ErrMessageTooLarge
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.link.WriteMessage(body)
}

func (c *Client) dropPending(id string) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *Client) readLoop() {
	defer c.wg.Done()
	for {
		payload, binary, err := c.link.ReadMessage()
		if err != nil {
			c.failAll(err)
			return
		}
		if binary {
			payload, err = binaryFrameToMessage(payload)
			if err != nil {
				c.failAll(err)
				return
			}
		}
		if len(bytes.TrimSpace(payload)) == 0 {
			continue
		}
		msg, err := parseInbound(payload)
		if err != nil {
			c.failAll(err)
			return
		}
		c.dispatch(msg)
	}
}

func (c *Client) dispatch(msg inbound) {
	switch {
	case msg.method != "" && msg.hasID:
		go c.answerServer(msg)
	case msg.method != "" && !msg.hasID:
		c.onNotification(msg)
	case msg.hasID:
		c.mu.Lock()
		ch := c.pending[msg.id]
		delete(c.pending, msg.id)
		c.mu.Unlock()
		if ch != nil {
			ch <- msg
		}
	default:
		c.failAll(&Error{Code: CodeInvalidRequest, Message: "Message must be a request, notification, or response."})
	}
}

func (c *Client) onNotification(msg inbound) {
	switch msg.method {
	case methodDesktopTransport:
		var params struct {
			Frame serverFrame `json:"frame"`
		}
		if err := json.Unmarshal(msg.params, &params); err != nil {
			return
		}
		c.mu.Lock()
		call := c.calls[params.Frame.ID]
		c.mu.Unlock()
		if call == nil {
			return
		}
		call.deliver(params.Frame)
	case "account/login/completed":
		if c.opt.Handlers.OnAccountLoginCompleted != nil {
			notice := AccountNotice{Params: append(json.RawMessage(nil), msg.params...)}
			go c.opt.Handlers.OnAccountLoginCompleted(notice)
		}
	case "account/updated":
		if c.opt.Handlers.OnAccountUpdated != nil {
			notice := AccountNotice{Params: append(json.RawMessage(nil), msg.params...)}
			go c.opt.Handlers.OnAccountUpdated(notice)
		}
	default:
		// Unknown notifications are ignored, matching the server's treatment
		// of unknown client notifications.
	}
}

func (c *Client) answerServer(msg inbound) {
	var result any
	var rpcErr *Error
	if c.opt.Handlers.OnServerRequest != nil {
		value, err := c.opt.Handlers.OnServerRequest(msg.method, msg.params)
		if err != nil {
			var as *Error
			if errors.As(err, &as) {
				rpcErr = as
			} else {
				rpcErr = &Error{Code: CodeInternal, Message: err.Error()}
			}
		} else {
			result = value
		}
	} else {
		// A missing handler denies the request. It must not return a successful
		// result, which a caller could treat as approval.
		rpcErr = &Error{
			Code:    CodeMethodNotFound,
			Message: "denied: no handler is registered for " + msg.method,
			Data:    json.RawMessage(`{"decision":"deny"}`),
		}
	}
	resp := wireMessage{JSONRPC: "2.0", ID: msg.idRaw}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		raw, err := json.Marshal(result)
		if err != nil {
			resp.Error = &Error{Code: CodeInternal, Message: "failed to encode response"}
		} else {
			resp.Result = raw
		}
	}
	_ = c.writeJSON(resp)
}

func (c *Client) failAll(err error) {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, ErrClosed) {
		err = ErrClosed
	}
	pending, calls := c.takeClosed()
	if pending == nil && calls == nil {
		return
	}
	c.closeDone()
	for _, ch := range pending {
		ch <- inbound{goErr: err}
	}
	for _, call := range calls {
		call.fail(err)
	}
}

func (c *Client) takeClosed() (map[string]chan inbound, map[string]*deskCall) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil
	}
	c.closed = true
	pending := c.pending
	calls := c.calls
	c.pending = map[string]chan inbound{}
	c.calls = map[string]*deskCall{}
	return pending, calls
}

func (c *Client) closeDone() {
	c.doneOnce.Do(func() { close(c.done) })
}

func asError(err error) *Error {
	var rpc *Error
	if errors.As(err, &rpc) {
		return rpc
	}
	return &Error{Code: CodeInternal, Message: err.Error()}
}
