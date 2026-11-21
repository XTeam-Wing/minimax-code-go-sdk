package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// RoundTrip implements desktop.RoundTripper. The request is not replayed.
func (c *Client) RoundTrip(ctx context.Context, req desktop.HTTPRequest) (desktop.HTTPResponse, error) {
	stream, err := c.open(ctx, req, 0)
	if err != nil {
		return desktop.HTTPResponse{}, err
	}
	defer stream.finish()
	var body []byte
	for {
		chunk, err := stream.recv(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return desktop.HTTPResponse{}, err
		}
		body = append(body, chunk...)
	}
	return desktop.HTTPResponse{
		Status:     stream.status,
		StatusText: stream.statusText,
		Headers:    stream.headers,
		Body:       body,
	}, nil
}

// RoundTripStream implements desktop.RoundTripper for an SSE or chunked body.
// Credits are returned only as the caller reads, bounded by the stream window.
func (c *Client) RoundTripStream(ctx context.Context, req desktop.HTTPRequest) (*desktop.HTTPStream, error) {
	window := c.opt.StreamWindow
	call, err := c.open(ctx, req, window)
	if err != nil {
		return nil, err
	}
	return newHTTPStream(call), nil
}

func (c *Client) open(ctx context.Context, req desktop.HTTPRequest, window int) (*deskCall, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id := "d" + itoa(c.idSeq.Add(1))
	call := newDeskCall(id, window)
	call.client = c
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrClosed
	}
	if len(c.calls) >= c.opt.MaxInFlight {
		c.mu.Unlock()
		return nil, ErrInFlightLimit
	}
	c.calls[id] = call
	c.mu.Unlock()

	frame := clientFrame{
		Type:    "request",
		ID:      id,
		Method:  req.Method,
		URL:     req.URL,
		Headers: req.Headers,
		Body:    string(req.Body),
	}
	if frame.Headers == nil {
		frame.Headers = map[string]string{}
	}
	if window > 0 {
		w := window
		frame.StreamWindow = &w
	}
	raw, err := c.call(ctx, methodDesktopTransport, map[string]any{"frame": frame})
	if err != nil {
		c.forget(id)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			_ = c.abort(id, "context canceled")
		}
		return nil, err
	}
	var accepted struct {
		Accepted  bool   `json:"accepted"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(raw, &accepted); err != nil || !accepted.Accepted {
		c.forget(id)
		call.fail(fmtError("desktop request was not accepted"))
		return nil, fmtError("desktop request was not accepted")
	}
	if err := call.waitStart(ctx); err != nil {
		_ = c.abort(id, "context canceled")
		c.forget(id)
		return nil, err
	}
	return call, nil
}

func (c *Client) abort(id, reason string) error {
	// Best-effort and non-blocking. A peer that is not reading must not pin the
	// caller that is already canceling the request.
	frame := clientFrame{Type: "abort", ID: id, Headers: map[string]string{}, Reason: reason}
	go func() { _ = c.notify(methodDesktopTransport, map[string]any{"frame": frame}) }()
	return nil
}

func (c *Client) credit(id string, n int) error {
	if n < 1 || n > MaxStreamWindow {
		return nil
	}
	frame := clientFrame{Type: "stream-credit", ID: id, Headers: map[string]string{}, Credits: n}
	return c.notify(methodDesktopTransport, map[string]any{"frame": frame})
}

func (c *Client) forget(id string) {
	c.mu.Lock()
	delete(c.calls, id)
	c.mu.Unlock()
}

type deskCall struct {
	id         string
	window     int
	mu         sync.Mutex
	status     int
	statusText string
	headers    map[string]string
	started    bool
	chunks     [][]byte
	err        error
	finished   bool
	wake       chan struct{}
	client     *Client
}

func newDeskCall(id string, window int) *deskCall {
	return &deskCall{id: id, window: window, wake: make(chan struct{}, 1)}
}

func (d *deskCall) deliver(frame serverFrame) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finished {
		return
	}
	switch frame.Type {
	case "response-start":
		d.started = true
		d.status = frame.Status
		d.statusText = frame.StatusText
		d.headers = frame.Headers
		d.poke()
	case "stream-chunk":
		payload, err := decodeServerPayload(frame.Payload)
		if err != nil {
			d.err = err
			d.finished = true
			d.poke()
			return
		}
		d.chunks = append(d.chunks, payload)
		d.poke()
	case "stream-done":
		d.finished = true
		d.poke()
	case "stream-error":
		d.err = &desktop.StreamError{Code: frame.Code, Message: frame.Message}
		d.finished = true
		d.poke()
	default:
		d.err = fmtError("unknown desktop frame " + frame.Type)
		d.finished = true
		d.poke()
	}
}

func (d *deskCall) fail(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.finished {
		return
	}
	d.err = err
	d.finished = true
	d.poke()
}

func (d *deskCall) poke() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *deskCall) waitStart(ctx context.Context) error {
	for {
		d.mu.Lock()
		started := d.started
		finished := d.finished
		err := d.err
		d.mu.Unlock()
		if started {
			return nil
		}
		if finished {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.wake:
		}
	}
}

func (d *deskCall) recv(ctx context.Context) ([]byte, error) {
	for {
		d.mu.Lock()
		if len(d.chunks) > 0 {
			chunk := d.chunks[0]
			d.chunks = d.chunks[1:]
			window := d.window
			id := d.id
			d.mu.Unlock()
			if window > 0 && d.client != nil {
				_ = d.client.credit(id, 1)
			}
			return chunk, nil
		}
		if d.finished {
			err := d.err
			client := d.client
			id := d.id
			d.mu.Unlock()
			if client != nil {
				client.forget(id)
			}
			if err == nil {
				return nil, io.EOF
			}
			return nil, err
		}
		d.mu.Unlock()
		select {
		case <-ctx.Done():
			if d.client != nil {
				_ = d.client.abort(d.id, "context canceled")
			}
			return nil, ctx.Err()
		case <-d.wake:
		}
	}
}

func (d *deskCall) finish() {
	if d.client != nil {
		d.client.forget(d.id)
	}
}

func newHTTPStream(call *deskCall) *desktop.HTTPStream {
	var once sync.Once
	return desktop.NewHTTPStream(call.status, call.statusText, call.headers, call.recv, func(reason string) {
		once.Do(func() {
			if call.client != nil {
				_ = call.client.abort(call.id, reason)
				call.client.forget(call.id)
			}
			call.fail(io.EOF)
		})
	})
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

type stringError string

func (e stringError) Error() string { return string(e) }

func fmtError(s string) error { return stringError("appserver: " + s) }
