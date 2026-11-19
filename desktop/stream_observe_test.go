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

func TestAssistantChunkIsNotTheUserString(t *testing.T) {
	var turn AssistantTurn
	frame := scalarPtr(mustScalar(t, `"{\"agent_message\":{\"role\":\"user\",\"msg_content\":\"pong\"},\"agent_message_chunk\":{\"role\":\"assistant\",\"msg_content\":\"hello\",\"finish\":true}}"`))
	ObserveSessionFrame(SessionStreamFrameView{EventJSON: frame}, &turn)
	if turn.Text != "hello" || !turn.Completed {
		t.Fatalf("%+v", turn)
	}
}
