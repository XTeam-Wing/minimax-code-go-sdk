package desktop

// Continuation states are the 0.7.8 enum FAe returned by
// InspectTurnContinuation. The JSON value is a number. Scalar.String does
// not read it.
const (
	ContinuationUnavailable    = 0
	ContinuationAvailable      = 1
	ContinuationRunning        = 2
	ContinuationWaitingForUser = 3
)

// ContinuationState reads InspectTurnContinuation.state. The second result
// is false when the literal is not a finite JSON number.
func ContinuationState(state Scalar) (int, bool) {
	n, ok := state.Float64()
	if !ok || n != float64(int(n)) {
		return 0, false
	}
	return int(n), true
}
