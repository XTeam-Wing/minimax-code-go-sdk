package desktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// Alias wire types below were read from @minimax-ai/code 0.7.8 handlers.
// An IDL name by itself is not a type. Aliases with no recovered reader stay
// desktop.Scalar:
//
//   - SessionKind is dual. OLd accepts the strings conversation, task, peek,
//     channel, cron, and unknown, and the numbers oy.Unknown=0 through
//     oy.Cron=5. Use SessionKindString or SessionKindNumber. Anything else
//     throws INVALID_SESSION_KIND.
//   - CloudCronScheduleKind is the IDL name of schedule_kind. No handler reads
//     that field. The nested schedule.kind field is CronScheduleKind, a
//     different alias.
//   - WorkShareKind is the body of PackShareWork. That service method throws
//     unimplemented and no serializer was recovered.
//
// Query bindings stringify numbers. Handlers that call Number() or compare
// with "1"/"2" accept that decimal form. MarketplaceCategory's list filter
// (MHs) compares the decoded query string with the numeric enum, and the
// query decoder does not coerce aliases, so a category query does not match.
// Body JSON numbers do.

// AgentShellPreference is the string Uz accepts on PutAgentShellPreference.
// Any other value throws AGENT_SHELL_CONFIG_INVALID.
type AgentShellPreference string

const (
	AgentShellAuto       AgentShellPreference = "auto"
	AgentShellPowerShell AgentShellPreference = "powershell"
	AgentShellGitBash    AgentShellPreference = "git-bash"
)

// MarshalJSON rejects values Uz does not accept.
func (p AgentShellPreference) MarshalJSON() ([]byte, error) {
	switch p {
	case AgentShellAuto, AgentShellPowerShell, AgentShellGitBash:
		return json.Marshal(string(p))
	default:
		return nil, fmt.Errorf("desktop: agent shell preference must be auto, powershell, or git-bash")
	}
}

// UnmarshalJSON accepts only those three strings.
func (p *AgentShellPreference) UnmarshalJSON(b []byte) error {
	if bytes.Equal(b, []byte("null")) {
		return fmt.Errorf("desktop: agent shell preference is null")
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("desktop: agent shell preference: %w", err)
	}
	v := AgentShellPreference(s)
	switch v {
	case AgentShellAuto, AgentShellPowerShell, AgentShellGitBash:
		*p = v
		return nil
	default:
		return fmt.Errorf("desktop: agent shell preference %q is not auto, powershell, or git-bash", s)
	}
}

// PermissionReply is the numeric enum jhu accepts. 0 AllowOnce, 1 AllowAlways,
// 2 Deny. A string such as "deny" throws PERMISSION_REPLY_INVALID. The field
// is a pointer because 0 is a real decision; a nil pointer is omitted.
type PermissionReply int

const (
	PermissionReplyAllowOnce   PermissionReply = 0
	PermissionReplyAllowAlways PermissionReply = 1
	PermissionReplyDeny        PermissionReply = 2
)

// MarshalJSON emits the number. Other values are rejected.
func (p PermissionReply) MarshalJSON() ([]byte, error) {
	return marshalClosedInt("permission reply", int(p), 0, 1, 2)
}

// UnmarshalJSON accepts only the numbers 0, 1, and 2.
func (p *PermissionReply) UnmarshalJSON(b []byte) error {
	n, err := unmarshalClosedInt("permission reply", b, 0, 1, 2)
	if err != nil {
		return err
	}
	*p = PermissionReply(n)
	return nil
}

// BatchPermissionDecision is the numeric enum Uhu accepts. 0 BatchAllowOnce,
// 1 BatchAllowAlways, 2 BatchDeny. Other values throw PERMISSION_DECISION_INVALID.
// This is separate from the mavis batch-reply strings allowOnce, allowAlways,
// and deny.
type BatchPermissionDecision int

const (
	BatchPermissionAllowOnce   BatchPermissionDecision = 0
	BatchPermissionAllowAlways BatchPermissionDecision = 1
	BatchPermissionDeny        BatchPermissionDecision = 2
)

// MarshalJSON emits the number.
func (p BatchPermissionDecision) MarshalJSON() ([]byte, error) {
	return marshalClosedInt("batch permission decision", int(p), 0, 1, 2)
}

// UnmarshalJSON accepts only 0, 1, and 2.
func (p *BatchPermissionDecision) UnmarshalJSON(b []byte) error {
	n, err := unmarshalClosedInt("batch permission decision", b, 0, 1, 2)
	if err != nil {
		return err
	}
	*p = BatchPermissionDecision(n)
	return nil
}

// LocalHookScope is the numeric enum pwe accepts after Number(). 1 is user
// and 2 is project. Anything else throws LOCAL_HOOK_SCOPE_INVALID. The string
// "user" is not a wire value.
type LocalHookScope int

const (
	LocalHookScopeUser    LocalHookScope = 1
	LocalHookScopeProject LocalHookScope = 2
)

// MarshalJSON emits 1 or 2.
func (p LocalHookScope) MarshalJSON() ([]byte, error) {
	return marshalClosedInt("local hook scope", int(p), 1, 2)
}

// UnmarshalJSON accepts 1, 2, or the decimal strings the query decoder leaves.
func (p *LocalHookScope) UnmarshalJSON(b []byte) error {
	n, err := unmarshalClosedInt("local hook scope", b, 1, 2)
	if err != nil {
		s, ok := jsonString(b)
		if !ok {
			return err
		}
		n, err = unmarshalClosedInt("local hook scope", []byte(s), 1, 2)
		if err != nil {
			return err
		}
	}
	*p = LocalHookScope(n)
	return nil
}

// InstalledPluginSource is the numeric enum fwt accepts. 1 is official and 2
// is local. fwt also accepts the strings "1" and "2". Other values throw
// INVALID_PLUGIN_SOURCE.
type InstalledPluginSource int

const (
	InstalledPluginSourceOfficial InstalledPluginSource = 1
	InstalledPluginSourceLocal    InstalledPluginSource = 2
)

// MarshalJSON emits 1 or 2.
func (p InstalledPluginSource) MarshalJSON() ([]byte, error) {
	return marshalClosedInt("installed plugin source", int(p), 1, 2)
}

// UnmarshalJSON accepts 1, 2, "1", and "2".
func (p *InstalledPluginSource) UnmarshalJSON(b []byte) error {
	n, err := unmarshalClosedInt("installed plugin source", b, 1, 2)
	if err != nil {
		s, ok := jsonString(b)
		if !ok {
			return err
		}
		n, err = unmarshalClosedInt("installed plugin source", []byte(s), 1, 2)
		if err != nil {
			return err
		}
	}
	*p = InstalledPluginSource(n)
	return nil
}

// MarketplaceCategory is Vv, the numeric category MHs compares. Unknown
// numbers do not throw; the user-marketplace list returns an empty page.
// Display strings such as "Code" are the internal map keys, not the request
// value. Query bindings stay strings, so this filter does not match when the
// field is sent as a query parameter.
type MarketplaceCategory int

const (
	MarketplaceCategoryOther                MarketplaceCategory = 0
	MarketplaceCategoryOffice               MarketplaceCategory = 1
	MarketplaceCategoryStudio               MarketplaceCategory = 2
	MarketplaceCategoryDesignAndSites       MarketplaceCategory = 3
	MarketplaceCategoryCode                 MarketplaceCategory = 4
	MarketplaceCategoryBusiness             MarketplaceCategory = 5
	MarketplaceCategorySales                MarketplaceCategory = 6
	MarketplaceCategoryProductivity         MarketplaceCategory = 7
	MarketplaceCategoryTools                MarketplaceCategory = 8
	MarketplaceCategoryScienceAndHealthcare MarketplaceCategory = 9
	MarketplaceCategoryEducation            MarketplaceCategory = 10
)

// MarshalJSON emits the integer. Upstream does not reject unknown numbers.
func (p MarketplaceCategory) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(p))
}

// UnmarshalJSON accepts a JSON integer.
func (p *MarketplaceCategory) UnmarshalJSON(b []byte) error {
	n, err := unmarshalInt("marketplace category", b)
	if err != nil {
		return err
	}
	*p = MarketplaceCategory(n)
	return nil
}

// SkillScope is the number G$n compares with QVd. 1 is agent and 2 is global
// (builtin, project, workspace, global, and user storage all map to 2).
// Other numbers do not throw; they match no skill.
type SkillScope int

const (
	SkillScopeAgent  SkillScope = 1
	SkillScopeGlobal SkillScope = 2
)

// MarshalJSON emits the integer.
func (p SkillScope) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(p))
}

// UnmarshalJSON accepts a JSON integer.
func (p *SkillScope) UnmarshalJSON(b []byte) error {
	n, err := unmarshalInt("skill scope", b)
	if err != nil {
		return err
	}
	*p = SkillScope(n)
	return nil
}

// SkillSourceType is the number G$n compares with YVd. 1 is the built-in
// publisher and 2 is a user contribution. Other numbers do not throw.
type SkillSourceType int

const (
	SkillSourceOfficial         SkillSourceType = 1
	SkillSourceUserContribution SkillSourceType = 2
)

// MarshalJSON emits the integer.
func (p SkillSourceType) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(p))
}

// UnmarshalJSON accepts a JSON integer.
func (p *SkillSourceType) UnmarshalJSON(b []byte) error {
	n, err := unmarshalInt("skill source type", b)
	if err != nil {
		return err
	}
	*p = SkillSourceType(n)
	return nil
}

// SkillSortType is a number. Yzd keeps 2 and 3 and replaces every other
// finite number with 1. The bundle does not name 2 or 3.
type SkillSortType int

const (
	SkillSortDefault SkillSortType = 1
	SkillSortKept2   SkillSortType = 2
	SkillSortKept3   SkillSortType = 3
)

// MarshalJSON emits the integer, including values Yzd will replace.
func (p SkillSortType) MarshalJSON() ([]byte, error) {
	return json.Marshal(int(p))
}

// UnmarshalJSON accepts a JSON integer.
func (p *SkillSortType) UnmarshalJSON(b []byte) error {
	n, err := unmarshalInt("skill sort type", b)
	if err != nil {
		return err
	}
	*p = SkillSortType(n)
	return nil
}

// CronScheduleKind is schedule.kind inside CronSchedule. lNi accepts 0
// (recurring) and 1 (once) and throws "schedule.kind is invalid" otherwise.
// The field is a pointer because 0 is a real schedule.
type CronScheduleKind int

const (
	CronScheduleRecurring CronScheduleKind = 0
	CronScheduleOnce      CronScheduleKind = 1
)

// MarshalJSON emits 0 or 1.
func (p CronScheduleKind) MarshalJSON() ([]byte, error) {
	return marshalClosedInt("cron schedule kind", int(p), 0, 1)
}

// UnmarshalJSON accepts only 0 and 1.
func (p *CronScheduleKind) UnmarshalJSON(b []byte) error {
	n, err := unmarshalClosedInt("cron schedule kind", b, 0, 1)
	if err != nil {
		return err
	}
	*p = CronScheduleKind(n)
	return nil
}

// CronSessionTargetMode is sessionTarget.mode. dNi accepts 0 (new) and 1
// (session id) and throws "sessionTarget.mode is invalid" otherwise.
// The field is a pointer because 0 is a real mode.
type CronSessionTargetMode int

const (
	CronSessionTargetNew       CronSessionTargetMode = 0
	CronSessionTargetSessionID CronSessionTargetMode = 1
)

// MarshalJSON emits 0 or 1.
func (p CronSessionTargetMode) MarshalJSON() ([]byte, error) {
	return marshalClosedInt("cron session target mode", int(p), 0, 1)
}

// UnmarshalJSON accepts only 0 and 1.
func (p *CronSessionTargetMode) UnmarshalJSON(b []byte) error {
	n, err := unmarshalClosedInt("cron session target mode", b, 0, 1)
	if err != nil {
		return err
	}
	*p = CronSessionTargetMode(n)
	return nil
}

// LocalMcpTransport is the numeric enum ghu and yhu accept. 1 stdio, 2 http,
// 3 streamable-http, 4 sse. Other values throw MCP_CONFIG_INVALID. The
// strings "stdio" and "http" are the stored config form, not this request field.
type LocalMcpTransport int

const (
	LocalMcpTransportStdio          LocalMcpTransport = 1
	LocalMcpTransportHTTP           LocalMcpTransport = 2
	LocalMcpTransportStreamableHTTP LocalMcpTransport = 3
	LocalMcpTransportSSE            LocalMcpTransport = 4
)

// MarshalJSON emits 1, 2, 3, or 4.
func (p LocalMcpTransport) MarshalJSON() ([]byte, error) {
	return marshalClosedInt("local mcp transport", int(p), 1, 2, 3, 4)
}

// UnmarshalJSON accepts only those numbers.
func (p *LocalMcpTransport) UnmarshalJSON(b []byte) error {
	n, err := unmarshalClosedInt("local mcp transport", b, 1, 2, 3, 4)
	if err != nil {
		return err
	}
	*p = LocalMcpTransport(n)
	return nil
}

// SandboxLocalAccess is the numeric enum Wyu accepts. 1 is open and 2 is
// restricted. 0 and any other value throw SANDBOX_CONFIG_INVALID.
type SandboxLocalAccess int

const (
	SandboxLocalAccessOpen       SandboxLocalAccess = 1
	SandboxLocalAccessRestricted SandboxLocalAccess = 2
)

// MarshalJSON emits 1 or 2.
func (p SandboxLocalAccess) MarshalJSON() ([]byte, error) {
	return marshalClosedInt("sandbox local access", int(p), 1, 2)
}

// UnmarshalJSON accepts only 1 and 2.
func (p *SandboxLocalAccess) UnmarshalJSON(b []byte) error {
	n, err := unmarshalClosedInt("sandbox local access", b, 1, 2)
	if err != nil {
		return err
	}
	*p = SandboxLocalAccess(n)
	return nil
}

// SessionKindString encodes a session kind string OLd accepts.
func SessionKindString(v string) (Scalar, error) {
	switch v {
	case "conversation", "task", "peek", "channel", "cron", "unknown":
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return Scalar(raw), nil
	default:
		return nil, fmt.Errorf("desktop: unsupported session kind %q", v)
	}
}

// SessionKindNumber encodes a session kind number OLd accepts.
// 0 unknown, 1 conversation, 2 task, 3 peek, 4 channel, 5 cron.
func SessionKindNumber(v int) (Scalar, error) {
	switch v {
	case 0, 1, 2, 3, 4, 5:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		return Scalar(raw), nil
	default:
		return nil, fmt.Errorf("desktop: unsupported session kind %d", v)
	}
}

func marshalClosedInt(name string, value int, allowed ...int) ([]byte, error) {
	for _, n := range allowed {
		if value == n {
			return json.Marshal(value)
		}
	}
	return nil, fmt.Errorf("desktop: %s %d is not one of %v", name, value, allowed)
}

func unmarshalClosedInt(name string, b []byte, allowed ...int) (int, error) {
	n, err := unmarshalInt(name, b)
	if err != nil {
		return 0, err
	}
	for _, item := range allowed {
		if n == item {
			return n, nil
		}
	}
	return 0, fmt.Errorf("desktop: %s %d is not one of %v", name, n, allowed)
}

func unmarshalInt(name string, b []byte) (int, error) {
	if bytes.Equal(b, []byte("null")) {
		return 0, fmt.Errorf("desktop: %s is null", name)
	}
	if len(b) == 0 || b[0] == '"' || b[0] == '{' || b[0] == '[' || b[0] == 't' || b[0] == 'f' {
		return 0, fmt.Errorf("desktop: %s must be a JSON integer", name)
	}
	var raw json.Number
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return 0, fmt.Errorf("desktop: %s: %w", name, err)
	}
	n, err := strconv.Atoi(raw.String())
	if err != nil {
		return 0, fmt.Errorf("desktop: %s must be an integer", name)
	}
	return n, nil
}

func jsonString(b []byte) (string, bool) {
	if len(b) == 0 || b[0] != '"' {
		return "", false
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return "", false
	}
	return s, true
}
