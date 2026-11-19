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
// "queue" and "steer" are the only other values. CreateGoal on this baseline
// always requests kickoff admission, so a successful create starts a model
// turn. A prompt that mentions a goal is not evidence that a goal exists.
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
