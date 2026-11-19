package desktop

import "reflect"

// MethodInfo is one generated DesktopService method.
// SideEffect is the static inference shipped with the 0.7.8 research inventory
// (read, mutating, destructive, approval, auth_credential). It is not a second
// protocol field. The client never replays a call after the response is lost.
type MethodInfo struct {
	InventoryID     string
	Name            string
	HTTPMethod      string
	Path            string
	Transport       string
	RequestType     string
	ResponseType    string
	FrameType       string
	ErrorBodyType   string
	WholeBody       bool
	GatewayOverride bool
	Category        string
	Phase           string
	SideEffect      string
}

type methodSpec struct {
	Name            string
	HTTPMethod      string
	Path            string
	RequestType     string
	ResponseType    string
	FrameType       string
	ErrorBodyType   string
	Transport       string
	WholeBody       bool
	GatewayOverride bool
	Bindings        []bindingSpec
	SideEffect      string
	Category        string
	Phase           string
}

type bindingSpec struct {
	Field     string
	Wire      string
	JSON      string
	Kind      string
	Required  bool
	TSType    string
	ValueKind string
	Children  []childBinding
}

type childBinding struct {
	Field     string
	Wire      string
	JSON      string
	Required  bool
	TSType    string
	ValueKind string
}

// BaselineVersion is the only CLI release this module was generated from.
const BaselineVersion = "0.7.8"

// EmptyRequired reports a required binding that is nil or an empty string.
// The HTTP encoder sends an empty string, and several handlers then answer
// that the field is required. Callers should not treat that as a server fault.
func EmptyRequired(method string, req any) (field string, empty bool) {
	spec, ok := specByName[method]
	if !ok {
		return "", false
	}
	value := reflect.ValueOf(req)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "request", true
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return "", false
	}
	for _, binding := range spec.Bindings {
		if !binding.Required {
			continue
		}
		field := value.FieldByName(binding.Field)
		if !field.IsValid() {
			return binding.Field, true
		}
		switch field.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
			if field.IsNil() {
				return binding.Field, true
			}
		case reflect.String:
			if field.String() == "" {
				return binding.Field, true
			}
		}
	}
	return "", false
}
