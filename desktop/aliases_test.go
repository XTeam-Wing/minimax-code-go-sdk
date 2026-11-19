package desktop

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestPermissionReplyIsNumeric(t *testing.T) {
	deny := PermissionReplyDeny
	req := ReplyPermissionReq{Name: "mavis", RequestID: "r", Reply: &deny}
	_, _, body, err := encodeRequest(specByName["ReplyPermission"], req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"reply":2`)) {
		t.Fatalf("body %s", body)
	}
	bad := PermissionReply(9)
	req.Reply = &bad
	if _, _, _, err := encodeRequest(specByName["ReplyPermission"], req); err == nil {
		t.Fatal("expected out-of-range reply to fail")
	}
	var decoded PermissionReply
	if err := json.Unmarshal([]byte(`"deny"`), &decoded); err == nil {
		t.Fatal("string deny was accepted")
	}
	if err := json.Unmarshal([]byte(`2`), &decoded); err != nil || decoded != PermissionReplyDeny {
		t.Fatal(err)
	}
}

func TestBatchDecisionAndShellPreference(t *testing.T) {
	decision := BatchPermissionDeny
	req := BatchReplyPermissionsReq{RequestIds: []string{"r"}, Decision: &decision}
	_, _, body, err := encodeRequest(specByName["BatchReplyPermissions"], req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"decision":2`)) {
		t.Fatalf("body %s", body)
	}
	bad := BatchPermissionDecision(4)
	req.Decision = &bad
	if _, _, _, err := encodeRequest(specByName["BatchReplyPermissions"], req); err == nil {
		t.Fatal("expected invalid batch decision")
	}
	pref := AgentShellPreference(9)
	shell := PutAgentShellPreferenceReq{Preference: &pref}
	if _, _, _, err := encodeRequest(specByName["PutAgentShellPreference"], shell); err == nil {
		t.Fatal("expected invalid shell preference")
	}
	valid := AgentShellGitBash
	shell.Preference = &valid
	_, _, body, err = encodeRequest(specByName["PutAgentShellPreference"], shell)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"preference":2`)) {
		t.Fatalf("body %s", body)
	}
}

func TestHookScopeAndPluginSourceQueries(t *testing.T) {
	path, _, _, err := encodeRequest(specByName["ListLocalHooks"], ListLocalHooksReq{Scope: LocalHookScopeProject})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, "scope=2") || strings.Contains(path, "scope=project") || strings.Contains(path, "scope=user") {
		t.Fatalf("path %s", path)
	}
	if _, err := json.Marshal(LocalHookScope(0)); err == nil {
		t.Fatal("scope 0 was accepted")
	}
	if _, err := json.Marshal(InstalledPluginSource(3)); err == nil {
		t.Fatal("plugin source 3 was accepted")
	}
	var src InstalledPluginSource
	if err := json.Unmarshal([]byte(`"2"`), &src); err != nil || src != InstalledPluginSourceLocal {
		t.Fatal(err)
	}
}

func TestClosedEnumsRejectUnknownValues(t *testing.T) {
	if _, err := json.Marshal(CronScheduleKind(3)); err == nil {
		t.Fatal("cron kind 3")
	}
	if _, err := json.Marshal(CronSessionTargetMode(4)); err == nil {
		t.Fatal("session target 4")
	}
	if _, err := json.Marshal(LocalMcpTransport(0)); err == nil {
		t.Fatal("mcp transport 0")
	}
	if _, err := json.Marshal(LocalMcpTransport(9)); err == nil {
		t.Fatal("mcp transport 9")
	}
	if err := json.Unmarshal([]byte(`"stdio"`), new(LocalMcpTransport)); err == nil {
		t.Fatal("stdio string was accepted as the IDL transport")
	}
	if _, err := json.Marshal(SandboxLocalAccess(0)); err == nil {
		t.Fatal("sandbox 0")
	}
}

func TestSessionKindKeepsStringAndNumber(t *testing.T) {
	text, err := SessionKindString("conversation")
	if err != nil {
		t.Fatal(err)
	}
	num, err := SessionKindNumber(1)
	if err != nil {
		t.Fatal(err)
	}
	req := CreateSessionReq{Name: "mavis", SessionKind: &text}
	_, _, body, err := encodeRequest(specByName["CreateSession"], req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"session_kind":"conversation"`)) {
		t.Fatalf("body %s", body)
	}
	req.SessionKind = &num
	_, _, body, err = encodeRequest(specByName["CreateSession"], req)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"session_kind":1`)) {
		t.Fatalf("body %s", body)
	}
	if _, err := SessionKindString("nope"); err == nil {
		t.Fatal("string nope")
	}
	if _, err := SessionKindNumber(9); err == nil {
		t.Fatal("number 9")
	}
	var kind Scalar
	if err := json.Unmarshal([]byte("null"), &kind); err != nil || kind != nil {
		t.Fatalf("null %+v %v", kind, err)
	}
	if err := json.Unmarshal([]byte("1"), &kind); err != nil {
		t.Fatal(err)
	}
	if n, ok := kind.Float64(); !ok || n != 1 {
		t.Fatalf("number %v %v", n, ok)
	}
}

func TestOpaqueAliasesStayScalar(t *testing.T) {
	if specByName["PackShareWork"].Bindings[0].TSType != "WorkShareKind" {
		t.Fatal(specByName["PackShareWork"].Bindings[0])
	}
	var share PackShareWorkReq
	share.Kind = mustScalar(t, `1`)
	_, _, body, err := encodeRequest(specByName["PackShareWork"], share)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"kind":1`)) {
		t.Fatalf("body %s", body)
	}
	share.Kind = mustScalar(t, `"skill"`)
	_, _, body, err = encodeRequest(specByName["PackShareWork"], share)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"kind":"skill"`)) {
		t.Fatalf("body %s", body)
	}
	var cron CreateCronReq
	kind := mustScalar(t, `0`)
	cron.ScheduleKind = &kind
	raw, err := json.Marshal(cron)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"schedule_kind":0`)) {
		t.Fatalf("cron %s", raw)
	}
}

func TestScalarNumberSurvivesStructDecode(t *testing.T) {
	var req BaseReq
	raw := []byte(`{"AppID_deprecated":1,"auth_state":null,"os_deprecated":"linux","not_in_idl":true}`)
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if req.AppIDDeprecated == nil {
		t.Fatal("app id dropped")
	}
	n, ok := req.AppIDDeprecated.Float64()
	if !ok || n != 1 {
		t.Fatalf("app id %s", rawPreviewScalar(req.AppIDDeprecated))
	}
	if req.AuthState != nil {
		t.Fatalf("auth state %s", rawPreviewScalar(req.AuthState))
	}
	if req.OSDeprecated == nil {
		t.Fatal("os dropped")
	}
	s, ok := req.OSDeprecated.String()
	if !ok || s != "linux" {
		t.Fatalf("os %s", rawPreviewScalar(req.OSDeprecated))
	}
}

func rawPreviewScalar(s *Scalar) string {
	if s == nil {
		return ""
	}
	return string(*s)
}
