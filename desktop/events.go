package desktop

import "encoding/json"

// Confirmed global-event types are the publishGlobalEvent sites recovered from
// @minimax-ai/code 0.7.8. They are not a closed enum. SubscribeGlobalEvents
// delivers every frame, including types that are not in this list.
const (
	EventAgentUpdated                       = "agent.updated"
	EventConfigPermissionModeChanged        = "config.permission_mode.changed"
	EventMessageRewind                      = "message.rewind"
	EventPermissionAsk                      = "permission.ask"
	EventPermissionResolved                 = "permission.resolved"
	EventSubagentExecution                  = "subagent.execution"
	EventThreadGoalObjectiveUpdatedSteering = "thread_goal.objective_updated_steering"
	EventUIRequest                          = "ui.request"
)

// EventType returns GlobalEventFrame.type when it is a JSON string.
// Other JSON types return the raw literal and ok=false so callers can still
// forward the frame. A missing type returns "", false.
func (f GlobalEventFrame) EventType() (string, bool) {
	if f.Type == nil {
		return "", false
	}
	return f.Type.String()
}

// InterpretedEvent is a GlobalEventFrame plus a payload object when
// payload_json is a JSON string or a JSON object. Unknown type values are
// preserved on Type and do not fail interpretation. The type set is not closed.
type InterpretedEvent struct {
	Kind      string
	Type      string
	HasType   bool
	Source    *Scalar
	Timestamp Scalar
	Payload   json.RawMessage
	Frame     GlobalEventFrame
}

// Interpret decodes one global-event frame. A missing or non-object payload
// leaves Payload empty or equal to the raw literal and still returns the event.
func Interpret(frame GlobalEventFrame) InterpretedEvent {
	out := InterpretedEvent{
		Timestamp: frame.Timestamp,
		Source:    frame.Source,
		Frame:     frame,
	}
	if kind, ok := frame.Kind.String(); ok {
		out.Kind = kind
	}
	if typ, ok := frame.EventType(); ok {
		out.Type = typ
		out.HasType = true
	}
	if frame.PayloadJSON != nil {
		raw := []byte(*frame.PayloadJSON)
		if s, ok := frame.PayloadJSON.String(); ok {
			raw = []byte(s)
		}
		if json.Valid(raw) && len(raw) > 0 && (raw[0] == '{' || raw[0] == '[') {
			out.Payload = append(json.RawMessage(nil), raw...)
		} else {
			out.Payload = append(json.RawMessage(nil), []byte(*frame.PayloadJSON)...)
		}
	}
	return out
}

// PermissionResolved is the payload of type permission.resolved.
type PermissionResolved struct {
	RequestID string `json:"requestId"`
	SessionID string `json:"sessionId"`
	Decision  string `json:"decision"`
}

// PermissionResolvedPayload returns the payload when Type is permission.resolved
// and the object has the fields the publisher sets. Other types return false.
func (e InterpretedEvent) PermissionResolvedPayload() (PermissionResolved, bool) {
	var out PermissionResolved
	if e.Type != EventPermissionResolved || len(e.Payload) == 0 {
		return out, false
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil || out.RequestID == "" {
		return PermissionResolved{}, false
	}
	return out, true
}

// PermissionModeChanged is the payload of config.permission_mode.changed.
type PermissionModeChanged struct {
	PermissionMode string `json:"permissionMode"`
}

// PermissionModePayload returns the payload for config.permission_mode.changed.
func (e InterpretedEvent) PermissionModePayload() (PermissionModeChanged, bool) {
	var out PermissionModeChanged
	if e.Type != EventConfigPermissionModeChanged || len(e.Payload) == 0 {
		return out, false
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil || out.PermissionMode == "" {
		return PermissionModeChanged{}, false
	}
	return out, true
}

// AgentUpdated is the payload of agent.updated.
type AgentUpdated struct {
	AgentName string `json:"agentName"`
}

// AgentUpdatedPayload returns the payload for agent.updated.
func (e InterpretedEvent) AgentUpdatedPayload() (AgentUpdated, bool) {
	var out AgentUpdated
	if e.Type != EventAgentUpdated || len(e.Payload) == 0 {
		return out, false
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil || out.AgentName == "" {
		return AgentUpdated{}, false
	}
	return out, true
}

// QuestionnaireDismiss is the payload of questionnaire.dismiss.
type QuestionnaireDismiss struct {
	RequestID string `json:"requestId"`
	SessionID string `json:"sessionId"`
	AgentName string `json:"agentName"`
	Status    string `json:"status"`
}

// QuestionnaireDismissPayload returns the payload for questionnaire.dismiss.
func (e InterpretedEvent) QuestionnaireDismissPayload() (QuestionnaireDismiss, bool) {
	var out QuestionnaireDismiss
	if e.Type != "questionnaire.dismiss" || len(e.Payload) == 0 {
		return out, false
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil || out.RequestID == "" {
		return QuestionnaireDismiss{}, false
	}
	return out, true
}

// ThreadGoalSteering is the payload of thread_goal.objective_updated_steering.
type ThreadGoalSteering struct {
	SessionID string `json:"sessionId"`
	GoalID    string `json:"goalId"`
}

// ThreadGoalSteeringPayload returns the payload for that event type.
func (e InterpretedEvent) ThreadGoalSteeringPayload() (ThreadGoalSteering, bool) {
	var out ThreadGoalSteering
	if e.Type != EventThreadGoalObjectiveUpdatedSteering || len(e.Payload) == 0 {
		return out, false
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil || out.SessionID == "" {
		return ThreadGoalSteering{}, false
	}
	return out, true
}

// PermissionAsk is the object O6e publishes for permission.ask.
// toolInput and createdAt are forwarded as JSON because their primitive was
// not fixed by the publisher. ruleContents is the array the publisher passes
// through; element types were not recovered, so the field stays raw.
// allowAlwaysSupported is true when ruleContents is non-empty or
// persistWholeToolRuleOnReply is true. reason is prefixed when a parent tool
// call is set. Extra fields remain on InterpretedEvent.Payload.
type PermissionAsk struct {
	RequestID             string          `json:"requestId"`
	SessionID             string          `json:"sessionId"`
	TurnID                string          `json:"turnId"`
	AgentName             string          `json:"agentName"`
	ToolName              string          `json:"toolName"`
	RuleContents          json.RawMessage `json:"ruleContents"`
	ToolInput             json.RawMessage `json:"toolInput,omitempty"`
	ToolDescription       string          `json:"toolDescription,omitempty"`
	ParentToolCallID      string          `json:"parentToolCallId,omitempty"`
	ParentToolDescription string          `json:"parentToolDescription,omitempty"`
	StopPtcOnDeny         bool            `json:"stopPtcOnDeny,omitempty"`
	Reason                string          `json:"reason"`
	AllowAlwaysSupported  bool            `json:"allowAlwaysSupported"`
	CreatedAt             json.RawMessage `json:"createdAt"`
}

// PermissionAskPayload returns the permission.ask object when requestId is set.
func (e InterpretedEvent) PermissionAskPayload() (PermissionAsk, bool) {
	var out PermissionAsk
	if e.Type != EventPermissionAsk || len(e.Payload) == 0 {
		return out, false
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil || out.RequestID == "" {
		return PermissionAsk{}, false
	}
	return out, true
}

// MessageRewind is the object published for message.rewind.
// The recovered publisher sets sessionId and contextReset true.
type MessageRewind struct {
	SessionID    string `json:"sessionId"`
	ContextReset bool   `json:"contextReset"`
}

// MessageRewindPayload returns the payload when sessionId is set.
func (e InterpretedEvent) MessageRewindPayload() (MessageRewind, bool) {
	var out MessageRewind
	if e.Type != EventMessageRewind || len(e.Payload) == 0 {
		return out, false
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil || out.SessionID == "" {
		return MessageRewind{}, false
	}
	return out, true
}

// SubagentExecution is the fact Vgt accepts, plus the sessionId, msgId, and
// timestamp the commit callback adds. status is one of started, completed,
// failed, aborted, interrupted. Further fact fields stay on Payload.
type SubagentExecution struct {
	EventType       string          `json:"eventType"`
	Status          string          `json:"status"`
	ParentSessionID string          `json:"parent_session_id"`
	ChildSessionID  string          `json:"child_session_id"`
	TurnID          string          `json:"turn_id"`
	AgentName       string          `json:"agent_name"`
	SessionID       string          `json:"sessionId"`
	MsgID           string          `json:"msgId"`
	Timestamp       json.RawMessage `json:"timestamp,omitempty"`
}

// SubagentExecutionPayload returns the payload when the required strings are set
// and status is one of the five values Vgt allows.
func (e InterpretedEvent) SubagentExecutionPayload() (SubagentExecution, bool) {
	var out SubagentExecution
	if e.Type != EventSubagentExecution || len(e.Payload) == 0 {
		return out, false
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil {
		return SubagentExecution{}, false
	}
	switch out.Status {
	case "started", "completed", "failed", "aborted", "interrupted":
	default:
		return SubagentExecution{}, false
	}
	if out.ParentSessionID == "" || out.ChildSessionID == "" || out.TurnID == "" || out.AgentName == "" || out.ParentSessionID == out.ChildSessionID {
		return SubagentExecution{}, false
	}
	return out, true
}

// UIRequest is the prefix of a ui.request payload. The publisher sets
// requestId and spreads the rest of the request, which includes sessionId and
// operation. Known operation names in 0.7.8 are session.open,
// session.mark_read, sidebar.preferences.update, usage.get, app.update_check,
// panel.open_file, and panel.open_review. The publisher does not reject other
// operations, so this list is not a closed enum. Other request fields stay on Payload.
type UIRequest struct {
	RequestID string `json:"requestId"`
	SessionID string `json:"sessionId"`
	Operation string `json:"operation"`
}

// UIRequestPayload returns the payload when requestId and operation are set.
func (e InterpretedEvent) UIRequestPayload() (UIRequest, bool) {
	var out UIRequest
	if e.Type != EventUIRequest || len(e.Payload) == 0 {
		return out, false
	}
	if err := json.Unmarshal(e.Payload, &out); err != nil || out.RequestID == "" || out.Operation == "" {
		return UIRequest{}, false
	}
	return out, true
}
