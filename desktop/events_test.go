package desktop

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestUnknownEventTypeIsPreserved(t *testing.T) {
	frame := GlobalEventFrame{
		Kind:        mustScalar(t, `"event"`),
		Timestamp:   mustScalar(t, `1710000000000`),
		Type:        scalarPtr(mustScalar(t, `"not.a.known.event"`)),
		PayloadJSON: scalarPtr(mustScalar(t, `"{\"custom\":true}"`)),
	}
	if typ, ok := frame.EventType(); !ok || typ != "not.a.known.event" {
		t.Fatalf("type %q ok %v", typ, ok)
	}
	got := Interpret(frame)
	if !got.HasType || got.Type != "not.a.known.event" {
		t.Fatalf("%+v", got)
	}
	if _, ok := got.PermissionResolvedPayload(); ok {
		t.Fatal("unknown type matched permission.resolved")
	}
	var payload map[string]bool
	if err := json.Unmarshal(got.Payload, &payload); err != nil || !payload["custom"] {
		t.Fatalf("payload %s err %v", got.Payload, err)
	}
}

func TestPermissionResolvedShape(t *testing.T) {
	frame := GlobalEventFrame{
		Kind:        mustScalar(t, `"event"`),
		Timestamp:   mustScalar(t, `1`),
		Type:        scalarPtr(mustScalar(t, `"permission.resolved"`)),
		PayloadJSON: scalarPtr(mustScalar(t, `"{\"requestId\":\"r1\",\"sessionId\":\"s1\",\"decision\":\"deny\"}"`)),
	}
	got := Interpret(frame)
	body, ok := got.PermissionResolvedPayload()
	if !ok || body.Decision != "deny" || body.RequestID != "r1" {
		t.Fatalf("%+v ok %v", body, ok)
	}
}

func TestPermissionAskAndOtherPublishers(t *testing.T) {
	ask := Interpret(GlobalEventFrame{
		Kind:        mustScalar(t, `"event"`),
		Type:        scalarPtr(mustScalar(t, `"permission.ask"`)),
		PayloadJSON: scalarPtr(mustScalar(t, `"{\"requestId\":\"r\",\"sessionId\":\"s\",\"turnId\":\"t\",\"agentName\":\"mavis\",\"toolName\":\"shell\",\"ruleContents\":[],\"reason\":\"because\",\"allowAlwaysSupported\":false,\"createdAt\":1,\"extra\":true}"`)),
	})
	body, ok := ask.PermissionAskPayload()
	if !ok || body.ToolName != "shell" || string(body.CreatedAt) != "1" {
		t.Fatalf("%+v ok %v", body, ok)
	}
	rewind := Interpret(GlobalEventFrame{
		Kind:        mustScalar(t, `"event"`),
		Type:        scalarPtr(mustScalar(t, `"message.rewind"`)),
		PayloadJSON: scalarPtr(mustScalar(t, `"{\"sessionId\":\"s\",\"contextReset\":true}"`)),
	})
	rw, ok := rewind.MessageRewindPayload()
	if !ok || !rw.ContextReset || rw.SessionID != "s" {
		t.Fatalf("%+v", rw)
	}
	sub := Interpret(GlobalEventFrame{
		Kind:        mustScalar(t, `"event"`),
		Type:        scalarPtr(mustScalar(t, `"subagent.execution"`)),
		PayloadJSON: scalarPtr(mustScalar(t, `"{\"eventType\":\"subagent.execution\",\"status\":\"started\",\"parent_session_id\":\"p\",\"child_session_id\":\"c\",\"turn_id\":\"t\",\"agent_name\":\"mavis\",\"sessionId\":\"p\",\"msgId\":\"m\"}"`)),
	})
	got, ok := sub.SubagentExecutionPayload()
	if !ok || got.ChildSessionID != "c" {
		t.Fatalf("%+v %v", got, ok)
	}
	bad := Interpret(GlobalEventFrame{
		Kind:        mustScalar(t, `"event"`),
		Type:        scalarPtr(mustScalar(t, `"subagent.execution"`)),
		PayloadJSON: scalarPtr(mustScalar(t, `"{\"status\":\"nope\",\"parent_session_id\":\"p\",\"child_session_id\":\"c\",\"turn_id\":\"t\",\"agent_name\":\"mavis\"}"`)),
	})
	if _, ok := bad.SubagentExecutionPayload(); ok {
		t.Fatal("unsupported status accepted")
	}
	ui := Interpret(GlobalEventFrame{
		Kind:        mustScalar(t, `"event"`),
		Type:        scalarPtr(mustScalar(t, `"ui.request"`)),
		PayloadJSON: scalarPtr(mustScalar(t, `"{\"requestId\":\"r\",\"sessionId\":\"s\",\"operation\":\"session.open\",\"path\":\"/tmp/a\"}"`)),
	})
	req, ok := ui.UIRequestPayload()
	if !ok || req.Operation != "session.open" {
		t.Fatalf("%+v", req)
	}
	if !strings.Contains(string(ui.Payload), `"path":"/tmp/a"`) {
		t.Fatalf("payload lost extra field %s", ui.Payload)
	}
}

func TestDriveListRejectsBadLimit(t *testing.T) {
	err := validateDriveGateway("ListDeployedWebsites", ListDeployedWebsitesReq{Offset: 0, Limit: 0})
	if err == nil {
		t.Fatal("expected limit error")
	}
	err = validateDriveGateway("GetWebsiteTrafficStats", GetWebsiteTrafficStatsReq{NodeID: "n", Days: 5})
	if err == nil {
		t.Fatal("expected days error")
	}
}

func mustScalar(t *testing.T, raw string) Scalar {
	t.Helper()
	var s Scalar
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	return s
}

func scalarPtr(s Scalar) *Scalar { return &s }
