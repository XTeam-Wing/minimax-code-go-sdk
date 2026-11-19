package desktop

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestEveryWrapperMatchesInventory checks each of the 470 generated methods,
// not only the count. The inventory binding list is compared with the encoder
// spec, and calling the wrapper must emit that HTTP method and path.
func TestEveryWrapperMatchesInventory(t *testing.T) {
	body, err := os.ReadFile("../internal/inventory/desktop_service.json")
	if err != nil {
		t.Fatal(err)
	}
	var inv struct {
		Methods []struct {
			MethodName string       `json:"method_name"`
			HTTPMethod string       `json:"http_method"`
			Path       string       `json:"path"`
			Transport  string       `json:"transport"`
			WholeBody  bool         `json:"whole_body"`
			Bindings   []invBinding `json:"bindings"`
		} `json:"methods"`
	}
	if err := json.Unmarshal(body, &inv); err != nil {
		t.Fatal(err)
	}
	if len(inv.Methods) != 470 {
		t.Fatalf("inventory length %d", len(inv.Methods))
	}
	clientType := reflect.TypeOf((*Client)(nil))
	seen := map[string]bool{}
	for _, method := range inv.Methods {
		if seen[method.MethodName] {
			t.Fatalf("duplicate %s", method.MethodName)
		}
		seen[method.MethodName] = true
		spec, ok := specByName[method.MethodName]
		if !ok {
			t.Fatalf("no spec %s", method.MethodName)
		}
		if spec.HTTPMethod != method.HTTPMethod || spec.Path != method.Path || spec.Transport != method.Transport || spec.WholeBody != method.WholeBody {
			t.Fatalf("%s spec %+v inventory method %s %s whole %v", method.MethodName, spec, method.HTTPMethod, method.Path, method.WholeBody)
		}
		if len(spec.Bindings) != len(method.Bindings) {
			t.Fatalf("%s bindings spec %d inventory %d", method.MethodName, len(spec.Bindings), len(method.Bindings))
		}
		for i, binding := range method.Bindings {
			got := spec.Bindings[i]
			if got.Field != binding.GoField || got.Wire != binding.Wire || got.JSON != binding.JSON || got.Kind != binding.Kind || got.Required != (binding.Requiredness == "required") || got.TSType != binding.TSType || got.ValueKind != binding.ValueKind {
				t.Fatalf("%s binding %d spec %+v inventory %+v", method.MethodName, i, got, binding)
			}
			if len(got.Children) != len(binding.Children) {
				t.Fatalf("%s binding %s children %d vs %d", method.MethodName, binding.Field, len(got.Children), len(binding.Children))
			}
			for j, child := range binding.Children {
				gchild := got.Children[j]
				if gchild.Wire != child.Wire || gchild.JSON != child.JSON || gchild.Required != (child.Requiredness == "required") || gchild.TSType != child.TSType || gchild.ValueKind != child.ValueKind {
					t.Fatalf("%s child %s spec %+v inventory %+v", method.MethodName, binding.Field, gchild, child)
				}
			}
		}
		m, ok := clientType.MethodByName(method.MethodName)
		if !ok || m.Type.NumIn() != 3 || m.Type.NumOut() != 2 {
			t.Fatalf("%s method shape", method.MethodName)
		}
		req := reflect.New(m.Type.In(2)).Elem()
		for _, binding := range spec.Bindings {
			if !binding.Required {
				continue
			}
			fillBinding(req.FieldByName(binding.Field), binding.Field, binding.Children)
		}
		adjustGateway(method.MethodName, req)
		rt := &captureRoundTrip{}
		call := reflect.ValueOf(New(rt))
		out := m.Func.Call([]reflect.Value{call, reflect.ValueOf(context.Background()), req})
		if !out[1].IsNil() {
			t.Fatalf("%s call: %v", method.MethodName, out[1].Interface())
		}
		closeResult(out[0])
		if rt.req.Method != method.HTTPMethod {
			t.Fatalf("%s method %s", method.MethodName, rt.req.Method)
		}
		if !pathMatches(method.Path, rt.req.URL) {
			t.Fatalf("%s path %s from %s", method.MethodName, rt.req.URL, method.Path)
		}
		if method.WholeBody {
			continue
		}
		parsed, err := url.Parse(rt.req.URL)
		if err != nil {
			t.Fatal(err)
		}
		var bodyMap map[string]json.RawMessage
		if len(rt.req.Body) > 0 {
			if err := json.Unmarshal(rt.req.Body, &bodyMap); err != nil {
				t.Fatalf("%s body: %v", method.MethodName, err)
			}
		}
		for _, binding := range method.Bindings {
			if binding.Requiredness != "required" {
				continue
			}
			switch binding.Kind {
			case "query":
				if parsed.Query().Get(binding.Wire) == "" && !queryHasPrefix(parsed, binding.Wire) {
					t.Fatalf("%s missing query %s in %s", method.MethodName, binding.Wire, rt.req.URL)
				}
			case "body", "implicitBody":
				if _, ok := bodyMap[binding.Wire]; !ok {
					t.Fatalf("%s missing body %s in %s", method.MethodName, binding.Wire, rt.req.Body)
				}
			}
		}
	}
}

type invBinding struct {
	Field        string     `json:"field"`
	Wire         string     `json:"wire"`
	JSON         string     `json:"json"`
	Kind         string     `json:"kind"`
	Requiredness string     `json:"requiredness"`
	TSType       string     `json:"tsType"`
	ValueKind    string     `json:"valueKind"`
	GoField      string     `json:"goField"`
	Children     []invChild `json:"children"`
}

type invChild struct {
	Field        string `json:"field"`
	Wire         string `json:"wire"`
	JSON         string `json:"json"`
	Requiredness string `json:"requiredness"`
	TSType       string `json:"tsType"`
	ValueKind    string `json:"valueKind"`
}

type captureRoundTrip struct {
	req HTTPRequest
}

func (c *captureRoundTrip) RoundTrip(ctx context.Context, req HTTPRequest) (HTTPResponse, error) {
	c.req = req
	return HTTPResponse{Status: 200, Body: []byte(`{}`)}, nil
}

func (c *captureRoundTrip) RoundTripStream(ctx context.Context, req HTTPRequest) (*HTTPStream, error) {
	c.req = req
	return NewHTTPStream(200, "OK", nil, func(context.Context) ([]byte, error) {
		return nil, io.EOF
	}, func(string) {}), nil
}

func pathMatches(template, got string) bool {
	got = strings.SplitN(got, "?", 2)[0]
	tparts := strings.Split(template, "/")
	gparts := strings.Split(got, "/")
	if len(tparts) != len(gparts) {
		return false
	}
	for i := range tparts {
		if strings.HasPrefix(tparts[i], ":") {
			if gparts[i] == "" {
				return false
			}
			continue
		}
		if tparts[i] != gparts[i] {
			return false
		}
	}
	return true
}

func queryHasPrefix(u *url.URL, wire string) bool {
	for key := range u.Query() {
		if key == wire || strings.HasPrefix(key, wire+"[") {
			return true
		}
	}
	return false
}

func closeResult(v reflect.Value) {
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return
	}
	m := v.MethodByName("Close")
	if m.IsValid() {
		m.Call(nil)
	}
}

func fillBinding(f reflect.Value, name string, children []childBinding) {
	if !f.IsValid() || !f.CanSet() {
		return
	}
	if f.Kind() == reflect.Pointer {
		f.Set(reflect.New(f.Type().Elem()))
		fillBinding(f.Elem(), name, children)
		return
	}
	if f.Kind() == reflect.Struct && len(children) > 0 {
		for _, child := range children {
			if !child.Required {
				continue
			}
			fillBinding(f.FieldByName(child.Field), child.Field, nil)
		}
		return
	}
	assignSample(f, name)
}

func assignSample(f reflect.Value, name string) {
	if !f.IsValid() || !f.CanSet() {
		return
	}
	if f.Kind() == reflect.Pointer {
		f.Set(reflect.New(f.Type().Elem()))
		assignSample(f.Elem(), name)
		return
	}
	switch f.Type() {
	case reflect.TypeOf(AgentShellPreference("")):
		f.Set(reflect.ValueOf(AgentShellAuto))
	case reflect.TypeOf(PermissionReply(0)):
		f.Set(reflect.ValueOf(PermissionReplyDeny))
	case reflect.TypeOf(BatchPermissionDecision(0)):
		f.Set(reflect.ValueOf(BatchPermissionDeny))
	case reflect.TypeOf(LocalHookScope(0)):
		f.Set(reflect.ValueOf(LocalHookScopeUser))
	case reflect.TypeOf(InstalledPluginSource(0)):
		f.Set(reflect.ValueOf(InstalledPluginSourceOfficial))
	case reflect.TypeOf(MarketplaceCategory(0)):
		f.Set(reflect.ValueOf(MarketplaceCategoryCode))
	case reflect.TypeOf(SkillScope(0)):
		f.Set(reflect.ValueOf(SkillScopeAgent))
	case reflect.TypeOf(SkillSourceType(0)):
		f.Set(reflect.ValueOf(SkillSourceOfficial))
	case reflect.TypeOf(SkillSortType(0)):
		f.Set(reflect.ValueOf(SkillSortDefault))
	case reflect.TypeOf(CronScheduleKind(0)):
		f.Set(reflect.ValueOf(CronScheduleRecurring))
	case reflect.TypeOf(CronSessionTargetMode(0)):
		f.Set(reflect.ValueOf(CronSessionTargetNew))
	case reflect.TypeOf(LocalMcpTransport(0)):
		f.Set(reflect.ValueOf(LocalMcpTransportStdio))
	case reflect.TypeOf(SandboxLocalAccess(0)):
		f.Set(reflect.ValueOf(SandboxLocalAccessOpen))
	case reflect.TypeOf(Number(0)):
		n := Number(1)
		if name == "Days" {
			n = 7
		}
		if name == "Offset" {
			n = 0
		}
		f.Set(reflect.ValueOf(n))
	case reflect.TypeOf(""):
		s := "x"
		switch name {
		case "NodeID":
			s = "n"
		case "Alias", "DisplayName":
			s = "alias"
		}
		f.SetString(s)
	case reflect.TypeOf(false):
		f.SetBool(true)
	case reflect.TypeOf(Scalar(nil)):
		var s Scalar
		if err := json.Unmarshal([]byte(`1`), &s); err != nil {
			panic(err)
		}
		f.Set(reflect.ValueOf(s))
	default:
		switch f.Kind() {
		case reflect.Struct:
			for i := 0; i < f.NumField(); i++ {
				sf := f.Field(i)
				if sf.Kind() == reflect.Pointer {
					continue
				}
				assignSample(sf, f.Type().Field(i).Name)
			}
		case reflect.Slice:
			elem := reflect.New(f.Type().Elem()).Elem()
			assignSample(elem, name)
			f.Set(reflect.Append(f, elem))
		case reflect.Map:
			f.Set(reflect.MakeMap(f.Type()))
		}
	}
}

func adjustGateway(name string, v reflect.Value) {
	switch name {
	case "ListDeployedWebsites":
		setNumber(v, "Offset", 0)
		setNumber(v, "Limit", 1)
	case "GetWebsiteTrafficStats":
		setString(v, "NodeID", "n")
		setNumber(v, "Days", 7)
	case "UpdateWebsiteDisplayName":
		setString(v, "NodeID", "n")
		setString(v, "DisplayName", "name")
	case "SetWebsiteAlias", "CheckWebsiteAliasAvailability", "UnbindWebsiteAlias":
		setString(v, "NodeID", "n")
		setString(v, "Alias", "alias")
	case "UpdateWebsiteWatermark", "GetWebsiteState", "CancelWebsiteDeployment", "UnpublishWebsiteDeployment", "RepublishWebsiteDeployment", "RemoveWebsiteAlias", "GetWebsiteProjectSourceDownloadURL":
		setString(v, "NodeID", "n")
	}
}

func setString(v reflect.Value, name, s string) {
	f := v.FieldByName(name)
	if !f.IsValid() || !f.CanSet() {
		return
	}
	if f.Kind() == reflect.Pointer {
		f.Set(reflect.New(f.Type().Elem()))
		f.Elem().SetString(s)
		return
	}
	if f.Kind() == reflect.String {
		f.SetString(s)
	}
}

func setNumber(v reflect.Value, name string, n float64) {
	f := v.FieldByName(name)
	if !f.IsValid() || !f.CanSet() {
		return
	}
	if f.Kind() == reflect.Pointer {
		f.Set(reflect.New(f.Type().Elem()))
		f = f.Elem()
	}
	if f.Type() == reflect.TypeOf(Number(0)) {
		f.Set(reflect.ValueOf(Number(n)))
	}
}
