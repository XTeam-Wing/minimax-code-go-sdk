package desktop

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
