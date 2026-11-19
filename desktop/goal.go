package desktop

import "fmt"

// Goal statuses Q4u accepts on patch. Any other string is HTTP 400
// VALIDATION_ERROR from the 0.7.8 handler.
const (
	GoalStatusActive        = "active"
	GoalStatusPaused        = "paused"
	GoalStatusComplete      = "complete"
	GoalStatusBlocked       = "blocked"
	GoalStatusBudgetLimited = "budget_limited"
	GoalStatusUsageLimited  = "usage_limited"
)

// Goal send behaviors Y8i accepts. Empty means the server default.
// "queue" and "steer" are the only other values. The 0.7.8 create handler
// always passes requireKickoffAdmission true, so a successful create starts
// a model turn. CreateGoalReq has session, objective, token budget, attachments,
// and send behavior. It has no request limit and no active-seconds field.
// Q8i stores a positive token budget and drops any other number. The host
// compares tokensUsed with that budget after each accounted request, and
// tokensUsed includes input, cache writes, cache reads, and output. Crossing
// the budget sets budget_limited and stops auto-continuation. update_goal
// complete is a model proposal. The tool result settlement is
// pending_host_validation and the goal snapshot is before_host_settlement.
// The host then dispatches a separate foreground agent named verifier on
// the same provider/model key. GetGoal status_reason paused(verifier_runtime)
// is only the mapped issue spawn_failed or child_crash, and last_verification
// stays unset until a verdict is stored. The stored verdict string is
// "met", and the status reason is complete(verifier_met). "pass" and
// "PASS" are not that field. The verifier task input is
// agent_name "verifier" and model set to the worker key "provider/model".
// That key drops thinking. The task selection is therefore model only.
// When that string does not match the parent session model, the source
// task-model-override requires effort, reasoning, or variant on that
// selection. The prefix minimax_api is not rewritten to minimax.
// CreateGoal and PatchGoal have no effort field. SelectModel with session_id
// stores thinking.effort on that session. GetSession returns it at
// session.model.thinking.effort and session.effective_model. ListModels does
// not return the stored effort. A call without session_id writes the process
// default and drops thinking. The same-key path uses the parent snapshot and
// does not set requireExplicitEffort. The SDK does not choose an effort.
// A parent request count of 6 with tokens still climbing is the verifier
// still running. Stopping there is a harness limit, not a product verdict.
// PatchGoal status complete is a separate call. A prompt that mentions a
// goal is not evidence that a goal exists.
const (
	GoalSendQueue = "queue"
	GoalSendSteer = "steer"
)

// ValidateCreateGoal checks the fields the 0.7.8 handler rejects before a
// model turn starts. TokenBudget must be a positive integer when set; zero
// is stored as null and would not cap the turn.
func ValidateCreateGoal(req CreateGoalReq) error {
	if req.SessionID == "" {
		return fmt.Errorf("desktop: goal session id is required")
	}
	if req.Objective != nil && *req.Objective == "" {
		return fmt.Errorf("desktop: goal objective is empty")
	}
	if req.SendBehavior != nil {
		switch *req.SendBehavior {
		case GoalSendQueue, GoalSendSteer:
		default:
			return fmt.Errorf("desktop: send_behavior must be queue or steer")
		}
	}
	if req.TokenBudget != nil {
		n := req.TokenBudget.Float64()
		if n != float64(int64(n)) || n <= 0 {
			return fmt.Errorf("desktop: token_budget must be a positive integer")
		}
	}
	return nil
}

// ValidateGoalStatus checks a patch status against Q4u.
func ValidateGoalStatus(status string) error {
	switch status {
	case GoalStatusActive, GoalStatusPaused, GoalStatusComplete, GoalStatusBlocked, GoalStatusBudgetLimited, GoalStatusUsageLimited:
		return nil
	default:
		return fmt.Errorf("desktop: invalid goal status %q", status)
	}
}

// ID is goal_id when it is a JSON string.
func (g GoalState) ID() (string, bool) {
	if g.GoalID == nil {
		return "", false
	}
	return g.GoalID.String()
}

// ObjectiveText is the stored objective string.
func (g GoalState) ObjectiveText() (string, bool) {
	if g.Objective == nil {
		return "", false
	}
	return g.Objective.String()
}

// StatusText is the stored status string.
func (g GoalState) StatusText() (string, bool) {
	if g.Status == nil {
		return "", false
	}
	return g.Status.String()
}

// SessionText is the stored session id.
func (g GoalState) SessionText() (string, bool) {
	if g.SessionID == nil {
		return "", false
	}
	return g.SessionID.String()
}
