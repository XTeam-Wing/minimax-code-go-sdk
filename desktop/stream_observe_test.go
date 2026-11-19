package desktop

import (
	"encoding/json"
	"testing"
)

func TestAssistantTextIgnoresUserPrompt(t *testing.T) {
	var turn AssistantTurn
	user := scalarPtr(mustScalar(t, `"{\"role\":\"user\",\"msg_content\":\"Reply with exactly the single word pong and nothing else.\"}"`))
	ObserveSessionFrame(SessionStreamFrameView{EventJSON: user}, &turn)
	if turn.Text != "" || turn.Completed || turn.HasUsage {
		t.Fatalf("user frame produced %+v", turn)
	}
	chunk := scalarPtr(mustScalar(t, `"{\"type\":6,\"agent_message_chunk\":{\"role\":\"assistant\",\"msg_content\":\"po\"}}"`))
	ObserveSessionFrame(SessionStreamFrameView{DataJSON: chunk}, &turn)
	if turn.Text != "po" || turn.Completed {
		t.Fatalf("chunk %+v", turn)
	}
	final := scalarPtr(mustScalar(t, `"{\"type\":2,\"agent_message\":{\"role\":\"assistant\",\"msg_content\":\"pong\",\"usage\":{\"output_tokens\":3}}}"`))
	ObserveSessionFrame(SessionStreamFrameView{EventJSON: final}, &turn)
	done := scalarPtr(mustScalar(t, `"{\"type\":\"finished\"}"`))
	ObserveSessionFrame(SessionStreamFrameView{EventJSON: done}, &turn)
	if turn.Text != "pong" || !turn.Completed || !turn.HasUsage || turn.OutputTokens != 3 {
		t.Fatalf("%+v", turn)
	}
	raw, _ := json.Marshal(turn.Text)
	if string(raw) != `"pong"` {
		t.Fatal(raw)
	}
}

func TestCommittedMessageReplacesChunkText(t *testing.T) {
	var turn AssistantTurn
	chunk := scalarPtr(mustScalar(t, `{"type":6,"agent_message_chunk":{"role":"assistant","msg_content":"pong"}}`))
	committed := scalarPtr(mustScalar(t, `{"type":2,"agent_message":{"role":"assistant","msg_type":1,"msg_content":"pong","finish_reason":"stop"}}`))
	status := scalarPtr(mustScalar(t, `{"type":"session_status","session_status":{"type":"finished"}}`))
	ObserveSessionFrame(SessionStreamFrameView{DataJSON: chunk}, &turn)
	ObserveSessionFrame(SessionStreamFrameView{DataJSON: committed}, &turn)
	ObserveSessionFrame(SessionStreamFrameView{DataJSON: status}, &turn)
	if turn.Text != "pong" {
		t.Fatalf("text %q", turn.Text)
	}
	if turn.FinishReason != "stop" || turn.SessionStatus != "finished" || !turn.SawSessionStatus {
		t.Fatalf("%+v", turn)
	}
	// A later chunk must not be appended onto the committed message.
	again := scalarPtr(mustScalar(t, `{"type":6,"agent_message_chunk":{"role":"assistant","msg_content":"pong"}}`))
	ObserveSessionFrame(SessionStreamFrameView{DataJSON: again}, &turn)
	if turn.Text != "pong" {
		t.Fatalf("doubled text %q", turn.Text)
	}
}

func TestAssistantChunkIsNotTheUserString(t *testing.T) {
	var turn AssistantTurn
	frame := scalarPtr(mustScalar(t, `"{\"agent_message\":{\"role\":\"user\",\"msg_content\":\"pong\"},\"agent_message_chunk\":{\"role\":\"assistant\",\"msg_content\":\"hello\",\"finish\":true}}"`))
	ObserveSessionFrame(SessionStreamFrameView{EventJSON: frame}, &turn)
	if turn.Text != "hello" || !turn.Completed {
		t.Fatalf("%+v", turn)
	}
}
