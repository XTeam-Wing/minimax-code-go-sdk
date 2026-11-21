package desktop

import (
	"encoding/json"
	"strings"
)

// AssistantTurn is text and usage taken from assistant-role session frames.
// User-role text is ignored, including when the prompt contains the same
// words as the reply. Tool and system messages are ignored. msg_content is
// used when it is set; otherwise content[].text parts of type "text" are used.
// Chunk deltas are kept only until an agent_message frame replaces them.
//
// Completed is set by a stream type of "finished" or "session_status", by an
// assistant finish_reason/stopReason, or by a chunk finish flag. output_tokens
// is recorded and does not by itself mark the turn complete.
type AssistantTurn struct {
	Text             string
	OutputTokens     int64
	HasUsage         bool
	Completed        bool
	SawFinished      bool
	SawSessionStatus bool
	SessionStatus    string
	FinishReason     string
	final            bool
}

// ObserveSessionFrame updates the turn from one SendMessage frame.
// event_json and data_json are parsed when they are JSON objects or JSON
// strings that contain an object.
func ObserveSessionFrame(frame SessionStreamFrameView, turn *AssistantTurn) {
	if turn == nil {
		return
	}
	if frame.EventJSON != nil {
		observeJSON([]byte(*frame.EventJSON), turn)
	}
	if frame.DataJSON != nil {
		observeJSON([]byte(*frame.DataJSON), turn)
	}
}

func observeJSON(raw []byte, turn *AssistantTurn) {
	if len(raw) == 0 {
		return
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return
		}
		raw = []byte(s)
	}
	if len(raw) == 0 || (raw[0] != '{' && raw[0] != '[') {
		return
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return
	}
	walkSession(v, turn)
}

func walkSession(v any, turn *AssistantTurn) {
	switch node := v.(type) {
	case []any:
		for _, item := range node {
			walkSession(item, turn)
		}
	case map[string]any:
		if s, ok := node["type"].(string); ok {
			switch s {
			case "finished":
				turn.SawFinished = true
				turn.Completed = true
			case "session_status":
				turn.SawSessionStatus = true
				turn.Completed = true
				if st, ok := node["session_status"].(map[string]any); ok {
					if status, ok := st["type"].(string); ok {
						turn.SessionStatus = status
					}
				}
			}
		}
		if msg, ok := node["agent_message"].(map[string]any); ok {
			takeAssistant("agent_message", msg, turn)
		}
		if msg, ok := node["agent_message_chunk"].(map[string]any); ok {
			takeAssistant("agent_message_chunk", msg, turn)
		}
		for key, child := range node {
			if key == "agent_message" || key == "agent_message_chunk" {
				continue
			}
			walkSession(child, turn)
		}
	}
}

func takeAssistant(kind string, msg map[string]any, turn *AssistantTurn) {
	role, _ := msg["role"].(string)
	if role != "assistant" || skippedMessage(msg) {
		return
	}
	text := assistantText(msg)
	noteFinish(msg, turn)
	switch kind {
	case "agent_message":
		turn.Text = text
		turn.final = true
		if usage, ok := msg["usage"].(map[string]any); ok {
			if n, ok := jsonInt(usage["output_tokens"]); ok {
				turn.OutputTokens = n
				turn.HasUsage = true
			}
		}
	case "agent_message_chunk":
		if !turn.final {
			turn.Text += text
		}
		if fin, ok := msg["finish"].(bool); ok && fin {
			turn.Completed = true
		}
	}
}

func assistantText(msg map[string]any) string {
	if s, ok := msg["msg_content"].(string); ok && s != "" {
		return s
	}
	switch content := msg["content"].(type) {
	case string:
		return content
	case []any:
		var b strings.Builder
		for _, part := range content {
			item, ok := part.(map[string]any)
			if !ok {
				continue
			}
			if typ, _ := item["type"].(string); typ != "" && typ != "text" {
				continue
			}
			if s, ok := item["text"].(string); ok {
				b.WriteString(s)
			}
		}
		return b.String()
	default:
		return ""
	}
}

func noteFinish(msg map[string]any, turn *AssistantTurn) {
	for _, key := range []string{"finish_reason", "finishReason", "stopReason", "stop_reason"} {
		s, ok := msg[key].(string)
		if ok && s != "" {
			turn.FinishReason = s
			turn.Completed = true
			return
		}
	}
}

func skippedMessage(msg map[string]any) bool {
	switch mt := msg["msg_type"].(type) {
	case float64:
		if mt == 2 || mt == 3 {
			return true
		}
	case string:
		switch mt {
		case "tool", "tool_call", "system", "system_event":
			return true
		}
	}
	return false
}

func jsonInt(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}
