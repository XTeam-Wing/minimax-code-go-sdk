package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// Client calls DesktopService methods over a RoundTripper.
// A nil permission callback is not an approval. This client never sends an
// allow decision unless the caller invokes ReplyPermission or BatchReplyPermissions
// with an explicit decision.
type Client struct {
	rt RoundTripper
}

// New wraps a tunnel. rt must be non-nil.
func New(rt RoundTripper) *Client {
	return &Client{rt: rt}
}

type gatewayRaw interface {
	setGatewayRaw([]byte)
}

func callUnary[T any](c *Client, ctx context.Context, spec methodSpec, req any) (T, error) {
	var zero T
	if c == nil || c.rt == nil {
		return zero, fmt.Errorf("desktop: nil client")
	}
	httpReq, err := buildHTTP(spec, req, false)
	if err != nil {
		return zero, err
	}
	resp, err := c.rt.RoundTrip(ctx, httpReq)
	if err != nil {
		return zero, err
	}
	if err := resp.StatusError(spec.Name); err != nil {
		return zero, err
	}
	if keeper, ok := any(&zero).(gatewayRaw); ok {
		keeper.setGatewayRaw(resp.Body)
	}
	if len(bytes.TrimSpace(resp.Body)) == 0 {
		return zero, nil
	}
	if err := json.Unmarshal(resp.Body, &zero); err != nil {
		return zero, &DecodeError{Method: spec.Name, Body: resp.Body, Err: err}
	}
	return zero, nil
}

func callStream[T any](c *Client, ctx context.Context, spec methodSpec, req any) (*EventStream[T], error) {
	if c == nil || c.rt == nil {
		return nil, fmt.Errorf("desktop: nil client")
	}
	httpReq, err := buildHTTP(spec, req, true)
	if err != nil {
		return nil, err
	}
	stream, err := c.rt.RoundTripStream(ctx, httpReq)
	if err != nil {
		return nil, err
	}
	if stream.Status < 200 || stream.Status > 299 {
		body, _ := readAllStream(ctx, stream)
		_ = stream.Close()
		return nil, &StatusError{Method: spec.Name, Status: stream.Status, StatusText: stream.StatusText, Headers: stream.Headers, Body: body}
	}
	return &EventStream[T]{method: spec.Name, http: stream}, nil
}

func readAllStream(ctx context.Context, stream *HTTPStream) ([]byte, error) {
	var buf bytes.Buffer
	for {
		chunk, err := stream.Recv(ctx)
		if err == io.EOF {
			return buf.Bytes(), nil
		}
		if err != nil {
			return buf.Bytes(), err
		}
		buf.Write(chunk)
	}
}

func buildHTTP(spec methodSpec, req any, stream bool) (HTTPRequest, error) {
	rawURL, headers, body, err := encodeRequest(spec, req)
	if err != nil {
		return HTTPRequest{}, err
	}
	if headers == nil {
		headers = map[string]string{}
	}
	if stream {
		headers["accept"] = "text/event-stream"
	} else if _, ok := headers["accept"]; !ok {
		headers["accept"] = "application/json"
	}
	if len(body) > 0 {
		if _, ok := headers["content-type"]; !ok && !hasHeader(headers, "content-type") {
			headers["content-type"] = "application/json"
		}
	}
	return HTTPRequest{Method: spec.HTTPMethod, URL: rawURL, Headers: headers, Body: body, Stream: stream}, nil
}

func hasHeader(headers map[string]string, name string) bool {
	for key := range headers {
		if equalHeader(key, name) {
			return true
		}
	}
	return false
}

func equalHeader(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// RawCall sends an HTTP request through the same tunnel without an IDL binding.
// It is the escape hatch for a method this baseline does not name. A non-2xx
// status is returned in HTTPResponse and is not an error; transport failures are.
// RawCall does not replay the request.
func (c *Client) RawCall(ctx context.Context, method, rawURL string, headers map[string]string, body []byte) (HTTPResponse, error) {
	if c == nil || c.rt == nil {
		return HTTPResponse{}, fmt.Errorf("desktop: nil client")
	}
	if headers == nil {
		headers = map[string]string{}
	}
	copied := make(map[string]string, len(headers))
	for k, v := range headers {
		copied[k] = v
	}
	return c.rt.RoundTrip(ctx, HTTPRequest{Method: method, URL: rawURL, Headers: copied, Body: body})
}
