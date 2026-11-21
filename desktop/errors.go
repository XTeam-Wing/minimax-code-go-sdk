package desktop

import (
	"fmt"
	"strings"
)

// StatusError is a non-2xx DesktopService HTTP response inside a transport stream.
type StatusError struct {
	Method     string
	Status     int
	StatusText string
	Headers    map[string]string
	Body       []byte
}

func (e *StatusError) Error() string {
	if e == nil {
		return "desktop: status error"
	}
	msg := strings.TrimSpace(string(e.Body))
	if len(msg) > 512 {
		msg = msg[:512] + "…"
	}
	if msg == "" {
		msg = e.StatusText
	}
	return fmt.Sprintf("desktop: %s: HTTP %d %s", e.Method, e.Status, msg)
}

// DecodeError means the response body was not the IDL JSON object.
type DecodeError struct {
	Method string
	Body   []byte
	Err    error
}

func (e *DecodeError) Error() string {
	if e == nil {
		return "desktop: decode error"
	}
	return fmt.Sprintf("desktop: %s: decode response: %v", e.Method, e.Err)
}

func (e *DecodeError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// StreamError is a desktopService stream-error frame.
type StreamError struct {
	Method  string
	Code    string
	Message string
}

func (e *StreamError) Error() string {
	if e == nil {
		return "desktop: stream error"
	}
	if e.Code == "" {
		return fmt.Sprintf("desktop: %s: %s", e.Method, e.Message)
	}
	return fmt.Sprintf("desktop: %s: %s (%s)", e.Method, e.Message, e.Code)
}
