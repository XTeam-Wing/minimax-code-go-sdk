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
// status_code that is not a finite number are not this error. Exact zero is
// success. A non-zero finite number that is not an int64 is still this error,
// and RawStatus keeps the literal. Other numeric fields are not read.
//
// The method still returns the decoded response value, so the caller can read
// the payload and errors.As the business error.
type BusinessError struct {
	Method     string
	StatusCode int64
	// RawStatus is the original status_code literal when it is not an int64.
	// A non-zero finite number that does not fit in an int64 still returns
	// this error. StatusCode is then 0, and RawStatus keeps the literal.
	RawStatus string
	Code      string
	Message   string
	Body      []byte
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
	case e.RawStatus != "":
		return fmt.Sprintf("desktop: %s: status_code=%s", e.Method, e.RawStatus)
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
	status, rawStatus, nonzero, ok := envelopeStatus(envelope["status_code"])
	if !ok || !nonzero {
		return nil
	}
	return &BusinessError{
		Method:     method,
		StatusCode: status,
		RawStatus:  rawStatus,
		Code:       envelopeString(envelope["code"]),
		Message:    envelopeString(envelope["status_msg"]),
		Body:       append([]byte(nil), trimmed...),
	}
}

// envelopeStatus reports whether status_code is a recognized number.
// nonzero is false only for an exact zero. An integer that fits in int64
// is returned in code and rawLiteral is empty. Any other non-zero finite
// number keeps its original literal in rawLiteral and still counts as nonzero.
func envelopeStatus(raw json.RawMessage) (code int64, rawLiteral string, nonzero bool, ok bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return 0, "", false, false
	}
	if raw[0] == '"' {
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return 0, "", false, false
		}
		text = strings.TrimSpace(text)
		if text == "" || !decimalInteger(text) {
			return 0, "", false, false
		}
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return 0, text, true, true
		}
		return n, "", n != 0, true
	}
	if raw[0] == '{' || raw[0] == '[' {
		return 0, "", false, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var number json.Number
	if err := dec.Decode(&number); err != nil {
		return 0, "", false, false
	}
	literal := number.String()
	if n, err := strconv.ParseInt(literal, 10, 64); err == nil {
		return n, "", n != 0, true
	}
	f, err := strconv.ParseFloat(literal, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, "", false, false
	}
	if f == 0 {
		return 0, "", false, true
	}
	return 0, literal, true, true
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
