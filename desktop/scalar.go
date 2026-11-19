package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
)

// Scalar is a JSON literal whose IDL schema records kind scalar without a
// TypeScript type. The primitive is unverified. Call String, Bool, or Float64
// to interpret a literal the caller expects. Stability of the primitive is
// unstable; the field name is part of the 0.7.8 schema.
type Scalar []byte

// MarshalJSON returns the original literal, or null when empty.
func (s Scalar) MarshalJSON() ([]byte, error) {
	if len(s) == 0 {
		return []byte("null"), nil
	}
	if !json.Valid(s) {
		return nil, fmt.Errorf("desktop: scalar is not valid JSON")
	}
	return append([]byte(nil), s...), nil
}

// UnmarshalJSON stores the literal, including strings, numbers, and bools.
func (s *Scalar) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		*s = nil
		return nil
	}
	if !json.Valid(b) {
		return fmt.Errorf("desktop: scalar is not valid JSON")
	}
	*s = append(Scalar(nil), b...)
	return nil
}

// String returns the value when the literal is a JSON string.
func (s Scalar) String() (string, bool) {
	if len(s) == 0 || s[0] != '"' {
		return "", false
	}
	var out string
	if err := json.Unmarshal(s, &out); err != nil {
		return "", false
	}
	return out, true
}

// Bool returns the value when the literal is true or false.
func (s Scalar) Bool() (bool, bool) {
	if bytes.Equal(s, []byte("true")) {
		return true, true
	}
	if bytes.Equal(s, []byte("false")) {
		return false, true
	}
	return false, false
}

// Float64 returns the value when the literal is a finite JSON number.
func (s Scalar) Float64() (float64, bool) {
	if len(s) == 0 || s[0] == '"' || s[0] == '{' || s[0] == '[' || s[0] == 't' || s[0] == 'f' || s[0] == 'n' {
		return 0, false
	}
	var n float64
	if err := json.Unmarshal(s, &n); err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, false
	}
	return n, true
}

// Number is a JSON number from an IDL binding whose TypeScript type is number
// or ThriftI64. The app-server decodes these with JavaScript Number, so values
// beyond the float64 safe-integer range are already lossy on the server.
type Number float64

// MarshalJSON encodes a finite number.
func (n Number) MarshalJSON() ([]byte, error) {
	if math.IsNaN(float64(n)) || math.IsInf(float64(n), 0) {
		return nil, fmt.Errorf("desktop: number must be finite")
	}
	return json.Marshal(float64(n))
}

// UnmarshalJSON accepts a finite JSON number.
func (n *Number) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		return fmt.Errorf("desktop: number is null")
	}
	var v float64
	if err := json.Unmarshal(b, &v); err != nil {
		return fmt.Errorf("desktop: number: %w", err)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("desktop: number must be finite")
	}
	*n = Number(v)
	return nil
}

// Float64 returns the numeric value.
func (n Number) Float64() float64 { return float64(n) }
