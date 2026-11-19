package appserver

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type clientFrame struct {
	Type         string            `json:"type"`
	ID           string            `json:"id"`
	Method       string            `json:"method,omitempty"`
	URL          string            `json:"url,omitempty"`
	Headers      map[string]string `json:"headers"`
	Body         string            `json:"body,omitempty"`
	StreamWindow *int              `json:"streamWindow,omitempty"`
	Credits      int               `json:"credits,omitempty"`
	Reason       string            `json:"reason,omitempty"`
}

type serverFrame struct {
	Type       string            `json:"type"`
	ID         string            `json:"id"`
	Seq        int               `json:"seq"`
	Status     int               `json:"status"`
	StatusText string            `json:"statusText"`
	Headers    map[string]string `json:"headers"`
	Payload    json.RawMessage   `json:"payload"`
	Message    string            `json:"message"`
	Code       string            `json:"code"`
}

func decodeServerPayload(raw json.RawMessage) ([]byte, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []byte(s), nil
	}
	var wrapped struct {
		Encoding string `json:"encoding"`
		Data     string `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	if wrapped.Encoding != "base64" {
		return nil, fmt.Errorf("appserver: unsupported payload encoding %q", wrapped.Encoding)
	}
	return base64.StdEncoding.DecodeString(wrapped.Data)
}

// binaryFrameToMessage converts a runtime binary frame (prefix byte 30) into
// the JSON notification the text path would have carried.
func binaryFrameToMessage(frame []byte) ([]byte, error) {
	if len(frame) < 2 || frame[0] != 30 {
		return nil, fmt.Errorf("appserver: binary frame is missing the 0x1e prefix")
	}
	rel := bytes.IndexByte(frame[1:], '\n')
	if rel < 0 {
		return nil, fmt.Errorf("appserver: binary frame is missing the header terminator")
	}
	header := frame[1 : 1+rel]
	payload := frame[1+rel+1:]
	var meta struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Seq        int    `json:"seq"`
		ByteLength int    `json:"byteLength"`
	}
	if err := json.Unmarshal(header, &meta); err != nil {
		return nil, err
	}
	if meta.Type != "stream-chunk" || meta.ID == "" {
		return nil, fmt.Errorf("appserver: binary frame is not a stream-chunk")
	}
	if meta.ByteLength >= 0 && meta.ByteLength != len(payload) && meta.ByteLength != 0 {
		// The server records the payload length. A mismatch is a broken frame.
		if meta.ByteLength != len(payload) {
			return nil, fmt.Errorf("appserver: binary frame length %d != %d", len(payload), meta.ByteLength)
		}
	}
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  methodDesktopTransport,
		"params": map[string]any{
			"frame": map[string]any{
				"type": meta.Type,
				"id":   meta.ID,
				"seq":  meta.Seq,
				"payload": map[string]string{
					"encoding": "base64",
					"data":     base64.StdEncoding.EncodeToString(payload),
				},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return body, nil
}
