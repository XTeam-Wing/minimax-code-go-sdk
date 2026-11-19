package desktop

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
