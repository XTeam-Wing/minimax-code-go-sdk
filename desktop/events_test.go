package desktop

import (
	"encoding/json"
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
