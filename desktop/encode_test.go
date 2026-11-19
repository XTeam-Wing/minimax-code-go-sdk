package desktop

import (
	"strings"
	"testing"
)

func TestEncodeGetSessionPath(t *testing.T) {
	spec := specByName["GetSession"]
	rawURL, _, body, err := encodeRequest(spec, GetSessionReq{ID: "a b/c"})
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		t.Fatalf("body %s", body)
	}
	if !strings.Contains(rawURL, "/session/a%20b%2Fc") {
		t.Fatalf("url %s", rawURL)
	}
}

func TestEncodeListAgentsQuery(t *testing.T) {
	spec := specByName["ListAgents"]
	limit := Number(10)
	rawURL, _, body, err := encodeRequest(spec, ListAgentsReq{Limit: &limit, Search: strPtr("hello world")})
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		t.Fatalf("body %s", body)
	}
	if !strings.Contains(rawURL, "limit=10") || !strings.Contains(rawURL, "search=hello+world") {
		t.Fatalf("url %s", rawURL)
	}
}

func TestListSessionsNameIsThePathSegment(t *testing.T) {
	spec := specByName["ListSessions"]
	rawURL, _, _, err := encodeRequest(spec, ListSessionsReq{Name: "mavis"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawURL, "/agent/mavis/session") {
		t.Fatalf("url %s", rawURL)
	}
	emptyURL, _, _, err := encodeRequest(spec, ListSessionsReq{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(emptyURL, "/agent//session") {
		t.Fatalf("empty name url %s", emptyURL)
	}
}

func TestEncodeRequiredEmptyStringIsSent(t *testing.T) {
	// The 0.7.8 binder treats only null as missing. A Go empty string is sent.
	spec := specByName["GetSession"]
	rawURL, _, _, err := encodeRequest(spec, GetSessionReq{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rawURL, "/session/") {
		t.Fatalf("url %s", rawURL)
	}
}

func TestCatalogClosedSet(t *testing.T) {
	cat := MethodCatalog()
	if len(cat) != 470 {
		t.Fatalf("catalog %d", len(cat))
	}
	seen := map[string]MethodInfo{}
	paths := map[string]struct{}{}
	var sse, gateway, whole int
	for _, item := range cat {
		if _, ok := seen[item.Name]; ok {
			t.Fatalf("duplicate %s", item.Name)
		}
		seen[item.Name] = item
		key := item.HTTPMethod + " " + item.Path
		if _, ok := paths[key]; ok {
			t.Fatalf("duplicate path %s", key)
		}
		paths[key] = struct{}{}
		if spec, ok := specByName[item.Name]; !ok || spec.Path != item.Path || spec.HTTPMethod != item.HTTPMethod {
			t.Fatalf("spec mismatch %s", item.Name)
		}
		if item.Transport == "sse" {
			sse++
		}
		if item.GatewayOverride {
			gateway++
		}
		if item.WholeBody {
			whole++
		}
	}
	if sse != 8 || gateway != 13 || whole != 73 {
		t.Fatalf("sse %d gateway %d whole %d", sse, gateway, whole)
	}
	if _, ok := seen["SubscribeGlobalEvents"]; !ok {
		t.Fatal("missing events")
	}
	if _, ok := seen["ReplyPermission"]; !ok || seen["ReplyPermission"].SideEffect != "approval" {
		t.Fatal("reply permission")
	}
}

func strPtr(s string) *string { return &s }
