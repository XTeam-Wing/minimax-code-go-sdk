package appserver

import (
	"errors"
	"fmt"
)

// JSON-RPC error codes advertised by mcode app-server 0.7.8.
const (
	CodeParseError           = -32700
	CodeInvalidRequest       = -32600
	CodeMethodNotFound       = -32601
	CodeInvalidParams        = -32602
	CodeInternal             = -32603
	CodeOverloaded           = -32001
	CodeNotInitialized       = -32002
	CodeAlreadyInitialized   = -32003
	CodeUnsupportedTransport = -32004
	CodeUnsupportedProtocol  = -32005
	CodeAuthOperationFailed  = -32010
)

// Sentinel errors. errors.Is matches on the numeric code.
var (
	ErrParseError           = &Error{Code: CodeParseError, Message: "parseError"}
	ErrInvalidRequest       = &Error{Code: CodeInvalidRequest, Message: "invalidRequest"}
	ErrMethodNotFound       = &Error{Code: CodeMethodNotFound, Message: "methodNotFound"}
	ErrInvalidParams        = &Error{Code: CodeInvalidParams, Message: "invalidParams"}
	ErrInternal             = &Error{Code: CodeInternal, Message: "internalError"}
	ErrOverloaded           = &Error{Code: CodeOverloaded, Message: "overloaded"}
	ErrNotInitialized       = &Error{Code: CodeNotInitialized, Message: "notInitialized"}
	ErrAlreadyInitialized   = &Error{Code: CodeAlreadyInitialized, Message: "alreadyInitialized"}
	ErrUnsupportedTransport = &Error{Code: CodeUnsupportedTransport, Message: "unsupportedTransport"}
	ErrUnsupportedProtocol  = &Error{Code: CodeUnsupportedProtocol, Message: "unsupportedProtocol"}
	ErrAuthOperationFailed  = &Error{Code: CodeAuthOperationFailed, Message: "authOperationFailed"}
)

// Error is a JSON-RPC error object.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    jsonRawData `json:"data,omitempty"`
}

// jsonRawData is declared in message.go as an alias to avoid an import cycle
// inside this file. The field type is json.RawMessage; see message.go.

func (e *Error) Error() string {
	if e == nil {
		return "appserver: rpc error"
	}
	return fmt.Sprintf("appserver: rpc %d: %s", e.Code, e.Message)
}

// Is reports whether target is an *Error with the same numeric code.
func (e *Error) Is(target error) bool {
	var other *Error
	if !errors.As(target, &other) || other == nil || e == nil {
		return false
	}
	return e.Code == other.Code
}

// Retryable reports the overloaded code. The client does not retry on its own,
// because a retry could repeat a side effect whose response was lost.
func (e *Error) Retryable() bool {
	return e != nil && e.Code == CodeOverloaded
}

// ErrClosed is returned after Close, or when the transport ends.
var ErrClosed = errors.New("appserver: connection closed")

// ErrMessageTooLarge is a frame above the 16 MiB app-server limit.
var ErrMessageTooLarge = errors.New("appserver: message exceeds 16MiB limit")

// ErrHealthUnsupported means Readyz or Healthz was called on a stdio session.
var ErrHealthUnsupported = errors.New("appserver: readyz and healthz exist only on the unix listener")

// ErrUnverifiedVersion means StrictBaseline rejected a server version other than 0.7.8.
var ErrUnverifiedVersion = errors.New("appserver: server version is outside the verified 0.7.8 baseline")

// ErrInFlightLimit means this client already has MaxInFlight desktop requests open.
var ErrInFlightLimit = errors.New("appserver: too many active desktop requests")
