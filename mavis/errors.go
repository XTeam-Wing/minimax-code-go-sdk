package mavis

import "errors"

var (
	errDecision = errors.New("mavis: decision must be allowAlways, allowOnce, or deny")
	errTool     = errors.New("mavis: tool_name is required")
)
