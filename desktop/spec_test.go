package desktop

import "testing"

func TestEmptyRequired(t *testing.T) {
	field, empty := EmptyRequired("ListBunnyTraceEvents", ListBunnyTraceEventsReq{})
	if !empty || field != "SessionID" {
		t.Fatalf("empty session: %q %v", field, empty)
	}
	field, empty = EmptyRequired("ListBunnyTraceEvents", ListBunnyTraceEventsReq{SessionID: "s"})
	if empty {
		t.Fatalf("filled session reported %s", field)
	}
	field, empty = EmptyRequired("ListProjectSessions", ListProjectSessionsReq{})
	if empty {
		t.Fatalf("optional project fields reported required: %s", field)
	}
	field, empty = EmptyRequired("NoSuchMethod", struct{}{})
	if empty {
		t.Fatal("unknown method")
	}
}
