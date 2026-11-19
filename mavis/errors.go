package mavis

import "errors"

var (
	errDecision  = errors.New("mavis: decision must be allowAlways, allowOnce, or deny")
	errTool      = errors.New("mavis: tool_name is required")
	errAgentName = errors.New("mavis: agent name must match ^[a-z][a-z0-9_-]*$ when set")
	errSessionID = errors.New("mavis: session id is required")
)
