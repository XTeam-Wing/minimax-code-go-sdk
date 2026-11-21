package appserver

import (
	"bytes"
	"encoding/json"
)

// jsonRawData is the JSON-RPC error data payload.
type jsonRawData = json.RawMessage

type wireMessage struct {
	JSONRPC string          `json:"jsonrpc,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  any             `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type inbound struct {
	hasID  bool
	id     string
	idRaw  json.RawMessage
	method string
	params json.RawMessage
	result json.RawMessage
	err    *Error
	hasRes bool
	goErr  error
}

func parseInbound(b []byte) (inbound, error) {
	var raw map[string]json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return inbound{}, &Error{Code: CodeParseError, Message: "Invalid JSON."}
	}
	if dec.More() {
		return inbound{}, &Error{Code: CodeInvalidRequest, Message: "Message must be a single JSON value."}
	}
	var msg inbound
	if rpc, ok := raw["jsonrpc"]; ok {
		var s string
		if err := json.Unmarshal(rpc, &s); err != nil || (s != "" && s != "2.0") {
			return inbound{}, &Error{Code: CodeInvalidRequest, Message: "Only JSON-RPC 2.0 messages are supported."}
		}
	}
	if id, ok := raw["id"]; ok && !bytes.Equal(bytes.TrimSpace(id), []byte("null")) {
		s, literal, ok := parseID(id)
		if !ok {
			return inbound{}, &Error{Code: CodeInvalidRequest, Message: "Request id must be a string or number."}
		}
		msg.hasID = true
		msg.id = s
		msg.idRaw = literal
	}
	if m, ok := raw["method"]; ok {
		if err := json.Unmarshal(m, &msg.method); err != nil || msg.method == "" {
			return inbound{}, &Error{Code: CodeInvalidRequest, Message: "Message must be a request, notification, or response."}
		}
	}
	if p, ok := raw["params"]; ok {
		if len(p) > 0 && p[0] != '{' {
			return inbound{}, &Error{Code: CodeInvalidParams, Message: "Request params must be an object."}
		}
		msg.params = append(json.RawMessage(nil), p...)
	}
	if res, ok := raw["result"]; ok {
		msg.hasRes = true
		msg.result = append(json.RawMessage(nil), res...)
	}
	if er, ok := raw["error"]; ok {
		var rpc Error
		if err := json.Unmarshal(er, &rpc); err != nil || rpc.Code == 0 && rpc.Message == "" {
			return inbound{}, &Error{Code: CodeInvalidRequest, Message: "Response error must contain a numeric code."}
		}
		msg.err = &rpc
	}
	if msg.method == "" && !msg.hasID {
		return inbound{}, &Error{Code: CodeInvalidRequest, Message: "Message must be a request, notification, or response."}
	}
	if msg.hasRes && msg.err != nil {
		return inbound{}, &Error{Code: CodeInvalidRequest, Message: "Response cannot contain both result and error."}
	}
	return msg, nil
}

func parseID(raw json.RawMessage) (string, json.RawMessage, bool) {
	literal := append(json.RawMessage(nil), bytes.TrimSpace(raw)...)
	var s string
	if err := json.Unmarshal(literal, &s); err == nil && s != "" {
		return s, literal, true
	}
	dec := json.NewDecoder(bytes.NewReader(literal))
	dec.UseNumber()
	var n json.Number
	if err := dec.Decode(&n); err == nil && n.String() != "" {
		return n.String(), literal, true
	}
	return "", nil, false
}

func quotedID(id string) json.RawMessage {
	raw, err := json.Marshal(id)
	if err != nil {
		return nil
	}
	return raw
}

// MaxMessageBytes is the app-server JSON-RPC limit (16 MiB).
const MaxMessageBytes = 16 * 1024 * 1024

// ProtocolVersion is the outer JSON-RPC protocol. The server requires an exact match.
const ProtocolVersion = 1

// TransportProtocolVersion is the DesktopService tunnel version. Exact match.
const TransportProtocolVersion = 2

// DefaultStreamWindow is the server default. MaxStreamWindow is the server cap.
const (
	DefaultStreamWindow = 16
	MaxStreamWindow     = 256
	DefaultMaxInFlight  = 64
)

const methodDesktopTransport = "desktopService/transport"
