package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
)

func encodeRequest(spec methodSpec, req any) (string, map[string]string, []byte, error) {
	v := reflect.ValueOf(req)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return "", nil, nil, fmt.Errorf("desktop: %s request is nil", spec.Name)
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", nil, nil, fmt.Errorf("desktop: %s request must be a struct", spec.Name)
	}
	if spec.WholeBody {
		body, err := json.Marshal(req)
		if err != nil {
			return "", nil, nil, fmt.Errorf("desktop: %s encode body: %w", spec.Name, err)
		}
		return spec.Path, map[string]string{}, body, nil
	}

	path := spec.Path
	query := url.Values{}
	headers := map[string]string{}
	bodyObj := map[string]json.RawMessage{}
	for _, binding := range spec.Bindings {
		field := v.FieldByName(binding.Field)
		if !field.IsValid() {
			return "", nil, nil, fmt.Errorf("desktop: %s missing field %s", spec.Name, binding.Field)
		}
		if !field.CanInterface() {
			return "", nil, nil, fmt.Errorf("desktop: %s field %s is not readable", spec.Name, binding.Field)
		}
		if isUnset(field) {
			if binding.Required {
				return "", nil, nil, fmt.Errorf("desktop: %s missing required %s %s", spec.Name, binding.Kind, binding.Wire)
			}
			continue
		}
		switch binding.Kind {
		case "path":
			text, err := scalarString(field)
			if err != nil {
				return "", nil, nil, fmt.Errorf("desktop: %s path %s: %w", spec.Name, binding.Wire, err)
			}
			next, err := replacePath(path, binding.Wire, text)
			if err != nil {
				return "", nil, nil, fmt.Errorf("desktop: %s: %w", spec.Name, err)
			}
			path = next
		case "query":
			if err := writeQuery(query, binding, field); err != nil {
				return "", nil, nil, fmt.Errorf("desktop: %s query %s: %w", spec.Name, binding.Wire, err)
			}
		case "header":
			text, err := scalarString(field)
			if err != nil {
				return "", nil, nil, fmt.Errorf("desktop: %s header %s: %w", spec.Name, binding.Wire, err)
			}
			headers[binding.Wire] = text
		case "body", "implicitBody":
			raw, err := json.Marshal(field.Interface())
			if err != nil {
				return "", nil, nil, fmt.Errorf("desktop: %s body %s: %w", spec.Name, binding.Wire, err)
			}
			if bytes.Equal(raw, []byte("null")) {
				if binding.Required {
					return "", nil, nil, fmt.Errorf("desktop: %s missing required body %s", spec.Name, binding.Wire)
				}
				continue
			}
			bodyObj[binding.Wire] = raw
		default:
			return "", nil, nil, fmt.Errorf("desktop: %s unsupported binding kind %q", spec.Name, binding.Kind)
		}
	}
	if err := unresolvedPath(path); err != nil {
		return "", nil, nil, fmt.Errorf("desktop: %s: %w", spec.Name, err)
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var body []byte
	if len(bodyObj) > 0 {
		var err error
		body, err = json.Marshal(bodyObj)
		if err != nil {
			return "", nil, nil, err
		}
	}
	return path, headers, body, nil
}

func isUnset(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Interface:
		return v.IsNil()
	default:
		return false
	}
}

func scalarString(v reflect.Value) (string, error) {
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	switch value := v.Interface().(type) {
	case string:
		return value, nil
	case bool:
		if value {
			return "true", nil
		}
		return "false", nil
	case Number:
		raw, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return string(raw), nil
	case Scalar:
		if s, ok := value.String(); ok {
			return s, nil
		}
		if b, ok := value.Bool(); ok {
			if b {
				return "true", nil
			}
			return "false", nil
		}
		if n, ok := value.Float64(); ok {
			raw, err := json.Marshal(n)
			if err != nil {
				return "", err
			}
			return string(raw), nil
		}
		return "", fmt.Errorf("scalar %s is not a string, bool, or number", value)
	default:
		raw, err := json.Marshal(v.Interface())
		if err != nil {
			return "", err
		}
		if len(raw) >= 2 && raw[0] == '"' {
			var s string
			if err := json.Unmarshal(raw, &s); err != nil {
				return "", err
			}
			return s, nil
		}
		return string(raw), nil
	}
}

func writeQuery(query url.Values, binding bindingSpec, field reflect.Value) error {
	if field.Kind() == reflect.Pointer {
		field = field.Elem()
	}
	switch binding.ValueKind {
	case "scalar":
		text, err := scalarString(field)
		if err != nil {
			return err
		}
		query.Add(binding.Wire, text)
		return nil
	case "scalarList":
		if field.Kind() != reflect.Slice && field.Kind() != reflect.Array {
			return fmt.Errorf("expected a list")
		}
		for i := 0; i < field.Len(); i++ {
			text, err := scalarString(field.Index(i))
			if err != nil {
				return err
			}
			query.Add(binding.Wire, text)
		}
		return nil
	case "struct":
		flatten := true
		for _, child := range binding.Children {
			if child.ValueKind != "scalar" && child.ValueKind != "" {
				flatten = false
				break
			}
		}
		if !flatten || len(binding.Children) == 0 && field.Kind() == reflect.Map {
			raw, err := json.Marshal(field.Interface())
			if err != nil {
				return err
			}
			query.Add(binding.Wire, string(raw))
			return nil
		}
		if len(binding.Children) == 0 {
			// The 0.7.8 encoder flattens struct query bindings using structFields.
			// An empty child list adds no parameters.
			return nil
		}
		for _, child := range binding.Children {
			childField := field
			if childField.Kind() == reflect.Pointer {
				if childField.IsNil() {
					continue
				}
				childField = childField.Elem()
			}
			if childField.Kind() != reflect.Struct {
				return fmt.Errorf("struct query %s is %s", binding.Wire, childField.Kind())
			}
			fv := childField.FieldByName(child.Field)
			if !fv.IsValid() || isUnset(fv) {
				continue
			}
			text, err := scalarString(fv)
			if err != nil {
				return err
			}
			query.Add(binding.Wire+"["+child.Wire+"]", text)
		}
		return nil
	default:
		if binding.ValueKind == "unsupported" {
			raw, err := json.Marshal(field.Interface())
			if err != nil {
				return err
			}
			query.Add(binding.Wire, string(raw))
			return nil
		}
		return fmt.Errorf("unsupported query type %s", binding.ValueKind)
	}
}

func replacePath(path, wire, value string) (string, error) {
	token := ":" + wire
	parts := strings.Split(path, "/")
	found := false
	escaped := encodeURIComponent(value)
	for i, part := range parts {
		if part == token {
			parts[i] = escaped
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("path binding %s is not present in %s", wire, path)
	}
	return strings.Join(parts, "/"), nil
}

func unresolvedPath(path string) error {
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, ":") {
			return fmt.Errorf("unresolved path binding %s", part)
		}
	}
	return nil
}

// encodeURIComponent matches the encoding used by the app-server path binder.
func encodeURIComponent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isURIUnescaped(c) {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func isURIUnescaped(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '-', '_', '.', '!', '~', '*', '\'', '(', ')':
		return true
	default:
		return false
	}
}
