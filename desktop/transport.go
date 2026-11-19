package desktop

import "context"

// HTTPRequest is one DesktopService call after IDL bindings are applied.
type HTTPRequest struct {
	Method  string
	URL     string
	Headers map[string]string
	Body    []byte
	Stream  bool
}

// HTTPResponse is a completed unary DesktopService response.
type HTTPResponse struct {
	Status     int
	StatusText string
	Headers    map[string]string
	Body       []byte
}

// StatusError returns a non-2xx error, or nil.
func (r HTTPResponse) StatusError(method string) error {
	if r.Status >= 200 && r.Status <= 299 {
		return nil
	}
	return &StatusError{Method: method, Status: r.Status, StatusText: r.StatusText, Headers: r.Headers, Body: append([]byte(nil), r.Body...)}
}

// HTTPStream is the body of one DesktopService response, chunked the way the
// tunnel delivered it. Recv returns io.EOF after stream-done. Close aborts the
// server stream and does not retry it.
type HTTPStream struct {
	Status     int
	StatusText string
	Headers    map[string]string
	recv       func(context.Context) ([]byte, error)
	abort      func(string)
}

// NewHTTPStream builds a stream around transport callbacks.
func NewHTTPStream(status int, statusText string, headers map[string]string, recv func(context.Context) ([]byte, error), abort func(string)) *HTTPStream {
	copied := make(map[string]string, len(headers))
	for k, v := range headers {
		copied[k] = v
	}
	return &HTTPStream{Status: status, StatusText: statusText, Headers: copied, recv: recv, abort: abort}
}

// Recv returns the next body chunk.
func (s *HTTPStream) Recv(ctx context.Context) ([]byte, error) {
	if s == nil || s.recv == nil {
		return nil, ioEOF()
	}
	return s.recv(ctx)
}

// Close aborts the stream. It is safe to call more than once.
func (s *HTTPStream) Close() error {
	if s != nil && s.abort != nil {
		s.abort("client closed")
		s.abort = nil
	}
	return nil
}

func ioEOF() error { return errEOF }

// RoundTripper sends a DesktopService HTTP request through an app-server tunnel.
// Implementations must not replay a request after the response is lost.
type RoundTripper interface {
	RoundTrip(ctx context.Context, req HTTPRequest) (HTTPResponse, error)
	RoundTripStream(ctx context.Context, req HTTPRequest) (*HTTPStream, error)
}
