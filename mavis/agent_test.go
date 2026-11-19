package mavis

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

type agentRT struct {
	req  desktop.HTTPRequest
	body []byte
	hdr  map[string]string
}

func (a *agentRT) RoundTrip(ctx context.Context, req desktop.HTTPRequest) (desktop.HTTPResponse, error) {
	a.req = req
	body := a.body
	if body == nil {
		body = []byte(`{"name":"helper","rootSessionId":"mvs_1","sessionId":"mvs_1","sessions":[],"session":{},"agent":{},"channel":null,"activeBindings":[],"localRuntime":true,"ok":true}`)
	}
	return desktop.HTTPResponse{Status: 200, Headers: a.hdr, Body: body}, nil
}

func (a *agentRT) RoundTripStream(ctx context.Context, req desktop.HTTPRequest) (*desktop.HTTPStream, error) {
	return nil, errors.New("unexpected stream")
}

func TestAgentRoutes(t *testing.T) {
	rt := &agentRT{}
	client := New(desktop.New(rt))
	ctx := context.Background()
	if _, err := client.CreateLocalAgent(ctx, LocalAgentCreate{Name: "Bad"}); !errors.Is(err, errAgentName) {
		t.Fatal(err)
	}
	if rt.req.URL != "" {
		t.Fatalf("invalid name was sent to %s", rt.req.URL)
	}
	created, err := client.CreateLocalAgent(ctx, LocalAgentCreate{Name: "helper", DisplayName: "Helper"})
	if err != nil {
		t.Fatal(err)
	}
	if rt.req.Method != "POST" || rt.req.URL != "/mavis/api/agent" || !strings.Contains(string(rt.req.Body), `"displayName":"Helper"`) {
		t.Fatalf("%s %s %s", rt.req.Method, rt.req.URL, rt.req.Body)
	}
	if created.RootSessionID != "mvs_1" {
		t.Fatalf("%+v", created)
	}
	if _, err := client.AgentSessionLookup(ctx, ""); !errors.Is(err, errSessionID) {
		t.Fatal(err)
	}
	if _, err := client.AgentSessionLookup(ctx, "mvs/1"); err != nil {
		t.Fatal(err)
	}
	if rt.req.URL != "/mavis/api/agent/session/mvs%2F1" {
		t.Fatal(rt.req.URL)
	}
	if _, err := client.AgentIMStatus(ctx, "helper"); err != nil {
		t.Fatal(err)
	}
	if rt.req.Method != "GET" || rt.req.URL != "/mavis/api/agent/helper/im" {
		t.Fatal(rt.req.URL)
	}
	if _, err := client.DeleteAgentIM(ctx, "helper"); err != nil || rt.req.Method != "DELETE" {
		t.Fatal(rt.req.Method, err)
	}
	if _, err := client.ListAgentSessions(ctx, "helper"); err != nil || rt.req.URL != "/mavis/api/agent/helper/session" {
		t.Fatal(rt.req.URL)
	}
	if _, err := client.AgentSessionTree(ctx, "helper"); err != nil || rt.req.URL != "/mavis/api/agent/helper/session/tree" {
		t.Fatal(rt.req.URL)
	}
	if _, err := client.GetAgentRootSession(ctx, "helper"); err != nil || rt.req.URL != "/mavis/api/agent/helper/session/root" {
		t.Fatal(rt.req.URL)
	}
	if _, err := client.ReplaceAgentRootSession(ctx, "helper", ""); !errors.Is(err, errSessionID) {
		t.Fatal(err)
	}
	if _, err := client.ReplaceAgentRootSession(ctx, "helper", "mvs_1"); err != nil {
		t.Fatal(err)
	}
	if rt.req.Method != "PUT" || !strings.Contains(string(rt.req.Body), `"sessionId":"mvs_1"`) {
		t.Fatalf("%s %s", rt.req.Method, rt.req.Body)
	}
	if _, err := client.CreateAgentSession(ctx, "helper", []byte(`{"title":"t"}`)); err != nil {
		t.Fatal(err)
	}
	if rt.req.Method != "POST" || rt.req.URL != "/mavis/api/agent/helper/session" || string(rt.req.Body) != `{"title":"t"}` {
		t.Fatalf("%s %s %s", rt.req.Method, rt.req.URL, rt.req.Body)
	}
	if _, err := client.RequestAgentCompaction(ctx, "helper", "", nil); !errors.Is(err, errSessionID) {
		t.Fatal(err)
	}
	if _, err := client.RequestAgentCompaction(ctx, "helper", "a b", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if rt.req.URL != "/mavis/api/agent/helper/session/a%20b/request-compaction" {
		t.Fatal(rt.req.URL)
	}
	rt.hdr = map[string]string{"Content-Type": "image/png"}
	rt.body = []byte("png")
	blob, ctype, err := client.AgentAvatar(ctx, "helper")
	if err != nil || ctype != "image/png" || string(blob) != "png" {
		t.Fatalf("%s %q %v", ctype, blob, err)
	}
	if rt.req.URL != "/mavis/api/agent/helper/identity/avatar" {
		t.Fatal(rt.req.URL)
	}
}
