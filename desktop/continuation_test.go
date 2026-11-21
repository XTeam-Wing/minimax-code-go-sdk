package desktop

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContinuationStateIsNumeric(t *testing.T) {
	var resp InspectTurnContinuationResp
	if err := json.Unmarshal([]byte(`{"state":1}`), &resp); err != nil {
		t.Fatal(err)
	}
	got, ok := ContinuationState(resp.State)
	if !ok || got != ContinuationAvailable {
		t.Fatalf("state %d %v raw %s", got, ok, resp.State)
	}
	if _, ok := resp.State.String(); ok {
		t.Fatal("numeric continuation state was read as a string")
	}
	for _, raw := range []string{`{"state":0}`, `{"state":2}`, `{"state":3}`} {
		var one InspectTurnContinuationResp
		if err := json.Unmarshal([]byte(raw), &one); err != nil {
			t.Fatal(err)
		}
		if _, ok := ContinuationState(one.State); !ok {
			t.Fatalf("raw %s", raw)
		}
	}
}

func TestEncodeResumeDoesNotRestartATurn(t *testing.T) {
	rawURL, _, body, err := encodeRequest(specByName["ResumeSession"], ResumeSessionReq{ID: "mvs_example"})
	if err != nil {
		t.Fatal(err)
	}
	if rawURL != "/minimax-desktop/api/v1/session/mvs_example/resume" {
		t.Fatalf("url %s", rawURL)
	}
	text := string(body)
	if strings.Contains(text, "drain_queued") || strings.Contains(text, "continue_paused_queue") || strings.Contains(text, "turn") {
		t.Fatalf("body %s", text)
	}
	contURL, _, contBody, err := encodeRequest(specByName["ContinueTurn"], ContinueTurnReq{ID: "mvs_example"})
	if err != nil {
		t.Fatal(err)
	}
	if contURL != "/minimax-desktop/api/v1/session/mvs_example/turn-continuation" {
		t.Fatalf("url %s", contURL)
	}
	if strings.Contains(string(contBody), "queue_item_id") {
		t.Fatalf("body %s", contBody)
	}
}

func TestEncodeResumeQueueFlagsSelectDispatch(t *testing.T) {
	drain := true
	_, _, body, err := encodeRequest(specByName["ResumeSession"], ResumeSessionReq{ID: "mvs_example", DrainQueued: &drain})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"drain_queued":true`) || strings.Contains(string(body), "continue_paused_queue") {
		t.Fatalf("body %s", body)
	}
	keep := true
	_, _, body, err = encodeRequest(specByName["ResumeSession"], ResumeSessionReq{ID: "mvs_example", ContinuePausedQueue: &keep})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"continue_paused_queue":true`) || strings.Contains(string(body), "drain_queued") {
		t.Fatalf("body %s", body)
	}
}
