package desktop

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The files under testdata/live-2026-11-19-flash-pong are not a byte-for-byte
// capture of SessionStreamFrameView and not a second model call. The live
// harness did not persist raw frames. An external consumer reconstructed
// minimal 0.7.8 shapes from that same turn's session ledger and the saved
// event/usage record. live_rerun is false.

func TestReconstructedTurnAssistantIsPong(t *testing.T) {
	frames, contentFrames, ledger, expected := loadLiveFixture(t)
	want, _ := expected["assistant_text"].(string)
	if want != "pong" {
		t.Fatalf("fixture assistant_text %q", want)
	}
	liveRaw, _ := expected["assistant_text_live_raw"].(string)
	if !strings.Contains(liveRaw, want) || liveRaw == want {
		t.Fatalf("live raw %q", liveRaw)
	}
	if live, _ := expected["live_rerun"].(bool); live {
		t.Fatal("fixture must say live_rerun false")
	}
	prompt, _ := expected["prompt"].(string)
	for _, got := range []AssistantTurn{observeFrames(frames), observeFrames(contentFrames), observeLedger(ledger)} {
		if strings.TrimSpace(got.Text) != want {
			t.Fatalf("assistant text %q", got.Text)
		}
		if strings.Contains(got.Text, prompt) {
			t.Fatalf("assistant text includes the prompt: %q", got.Text)
		}
	}
}

func TestReconstructedTurnCompletionIsNotTokenCount(t *testing.T) {
	frames, _, ledger, _ := loadLiveFixture(t)
	got := observeFrames(frames)
	if !got.SawFinished || !got.SawSessionStatus || got.SessionStatus != "completed" {
		t.Fatalf("completion flags %+v", got)
	}
	if got.FinishReason != "stop" {
		t.Fatalf("finish reason %q", got.FinishReason)
	}
	if !got.HasUsage || got.OutputTokens != 3 {
		t.Fatalf("usage %+v", got)
	}
	ledgerTurn := observeLedger(ledger)
	if ledgerTurn.FinishReason != "stop" || strings.TrimSpace(ledgerTurn.Text) != "pong" {
		t.Fatalf("ledger %+v", ledgerTurn)
	}
	// A usage-only object must not count as completion.
	var tokensOnly AssistantTurn
	ObserveSessionFrame(frameFromJSON(t, `{"agent_message":{"role":"assistant","msg_content":"pong","usage":{"output_tokens":3}}}`), &tokensOnly)
	if tokensOnly.Completed || tokensOnly.Text != "pong" || tokensOnly.OutputTokens != 3 {
		t.Fatalf("usage-only %+v", tokensOnly)
	}
}

func TestReconstructedCleanupIsAbortRecord(t *testing.T) {
	raw, err := os.ReadFile("testdata/live-2026-11-19-flash-pong/live-result-excerpt.json")
	if err != nil {
		t.Fatal(err)
	}
	var live map[string]any
	if err := json.Unmarshal(raw, &live); err != nil {
		t.Fatal(err)
	}
	aborted, _ := live["aborted"].(bool)
	if !aborted {
		t.Fatal("live result aborted")
	}
	expected := loadExpected(t)
	sources, _ := expected["cleanup_sources"].([]any)
	found := false
	for _, item := range sources {
		m, _ := item.(map[string]any)
		field, _ := m["field"].(string)
		if strings.Contains(strings.ToLower(field), "output_tokens") {
			t.Fatal("cleanup source is output_tokens")
		}
		if field == "aborted" && m["value"] == "true" {
			found = true
		}
	}
	if !found {
		t.Fatalf("cleanup sources %#v", sources)
	}
}

func TestNaiveWalkerStillIncludesPrompt(t *testing.T) {
	frames, _, _, expected := loadLiveFixture(t)
	naive := naiveWalk(frames)
	liveRaw, _ := expected["assistant_text_live_raw"].(string)
	if naive != liveRaw {
		t.Fatalf("naive %q want %q", naive, liveRaw)
	}
	got := observeFrames(frames)
	if strings.TrimSpace(got.Text) != "pong" || got.Text == naive {
		t.Fatalf("extractor %q naive %q", got.Text, naive)
	}
}

func loadLiveFixture(t *testing.T) (frames, content []wireFrame, ledger []map[string]any, expected map[string]any) {
	t.Helper()
	var wrap struct {
		Frames             []wireFrame `json:"frames"`
		FramesContentShape []wireFrame `json:"frames_content_shape"`
		Provenance         struct {
			LiveRerun bool `json:"live_rerun"`
		} `json:"provenance"`
	}
	raw, err := os.ReadFile("testdata/live-2026-11-19-flash-pong/stream-frames.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		t.Fatal(err)
	}
	if wrap.Provenance.LiveRerun {
		t.Fatal("stream fixture provenance live_rerun")
	}
	body, err := os.ReadFile("testdata/live-2026-11-19-flash-pong/messages.min.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		ledger = append(ledger, rec)
	}
	return wrap.Frames, wrap.FramesContentShape, ledger, loadExpected(t)
}

func loadExpected(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("testdata/live-2026-11-19-flash-pong/expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected map[string]any
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatal(err)
	}
	return expected
}

type wireFrame struct {
	EventJSON any `json:"event_json,omitempty"`
	DataJSON  any `json:"data_json,omitempty"`
}

func observeFrames(frames []wireFrame) AssistantTurn {
	var turn AssistantTurn
	for _, frame := range frames {
		ObserveSessionFrame(SessionStreamFrameView{
			EventJSON: scalarAny(frame.EventJSON),
			DataJSON:  scalarAny(frame.DataJSON),
		}, &turn)
	}
	return turn
}

func observeLedger(records []map[string]any) AssistantTurn {
	var turn AssistantTurn
	for _, rec := range records {
		msg, _ := rec["message"].(map[string]any)
		if msg == nil {
			continue
		}
		raw, err := json.Marshal(map[string]any{"agent_message": msg})
		if err != nil {
			continue
		}
		var s Scalar
		if err := json.Unmarshal(raw, &s); err != nil {
			continue
		}
		ObserveSessionFrame(SessionStreamFrameView{DataJSON: &s}, &turn)
	}
	return turn
}

func scalarAny(v any) *Scalar {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var s Scalar
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil
	}
	return &s
}

func frameFromJSON(t *testing.T, raw string) SessionStreamFrameView {
	t.Helper()
	s := mustScalar(t, raw)
	return SessionStreamFrameView{DataJSON: &s}
}

func naiveWalk(frames []wireFrame) string {
	var b strings.Builder
	var walk func(v any, path string)
	walk = func(v any, path string) {
		low := strings.ToLower(path)
		switch node := v.(type) {
		case map[string]any:
			for key, child := range node {
				next := key
				if path != "" {
					next = path + "." + key
				}
				walk(child, next)
			}
		case []any:
			for _, child := range node {
				walk(child, path+"[]")
			}
		case string:
			if node == "" {
				return
			}
			if strings.HasSuffix(low, "text") || strings.HasSuffix(low, "content") || strings.HasSuffix(low, "delta") || strings.HasSuffix(low, "finalresponse") {
				if b.Len() == 0 {
					b.WriteString(node)
					return
				}
				if strings.HasSuffix(b.String(), node) {
					return
				}
				if strings.Contains(b.String(), node) && len(node) > 8 {
					return
				}
				b.WriteString(node)
			}
		}
	}
	for _, frame := range frames {
		for _, raw := range []any{frame.EventJSON, frame.DataJSON} {
			walk(raw, "")
		}
	}
	return b.String()
}
