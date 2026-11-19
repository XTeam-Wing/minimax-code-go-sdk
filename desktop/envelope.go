package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// BusinessError is a DesktopService body whose top-level base_resp.status_code
// is a finite number other than 0, or a decimal integer string other than "0".
// The 0.7.8 memory service returns this on HTTP 200: status_code 1, status_msg
// the sentence, and code such as MEMORY_DISABLED or OLD_STRING_AMBIGUOUS.
//
// A missing base_resp, a non-object base_resp, a missing status_code, and a
// status_code whose JSON is not a finite number or a decimal integer are not
// this error. Other numeric fields are not read.
//
// The method still returns the decoded response value, so the caller can read
// the payload and errors.As the business error.
type BusinessError struct {
	Method     string
	StatusCode int64
	Code       string
	Message    string
	Body       []byte
}

func (e *BusinessError) Error() string {
	if e == nil {
		return "desktop: business error"
	}
	code := e.Code
	msg := e.Message
	switch {
	case code != "" && msg != "":
		return fmt.Sprintf("desktop: %s: %s: %s", e.Method, code, msg)
	case code != "":
		return fmt.Sprintf("desktop: %s: %s", e.Method, code)
	case msg != "":
		return fmt.Sprintf("desktop: %s: %s", e.Method, msg)
	default:
		return fmt.Sprintf("desktop: %s: status_code=%d", e.Method, e.StatusCode)
	}
}

func envelopeError(method string, body []byte) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &root); err != nil {
		return nil
	}
	raw, ok := root["base_resp"]
	if !ok || len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	if bytes.TrimSpace(raw)[0] != '{' {
		return nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil
	}
	status, ok := envelopeStatus(envelope["status_code"])
	if !ok || status == 0 {
		return nil
	}
	return &BusinessError{
		Method:     method,
		StatusCode: status,
		Code:       envelopeString(envelope["code"]),
		Message:    envelopeString(envelope["status_msg"]),
		Body:       append([]byte(nil), trimmed...),
	}
}

func envelopeStatus(raw json.RawMessage) (int64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, false
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, false
		}
		text = strings.TrimSpace(text)
		if text == "" || !decimalInteger(text) {
			return 0, false
		}
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	if raw[0] == '{' || raw[0] == '[' {
		return 0, false
	}
	var n float64
	if err := json.Unmarshal(raw, &n); err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	if n < math.MinInt64 || n > math.MaxInt64 {
		return 0, false
	}
	return int64(n), true
}

func decimalInteger(text string) bool {
	if text == "" {
		return false
	}
	if text[0] == '-' {
		text = text[1:]
		if text == "" {
			return false
		}
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func envelopeString(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || raw[0] != '"' {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return ""
	}
	return text
}
