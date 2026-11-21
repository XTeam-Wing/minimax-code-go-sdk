package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// TestLiveModelTurn is an opt-in call to the official app-server that sends one
// model turn. It does not run under MCODE_E2E. Set MCODE_E2E_LLM=1 and
// MCODE_LLM_DATA_DIR to an isolated data directory already prepared with
// `mcode provider set-minimax-key --api-key-env MCODE_PROVIDER_API_KEY` and
// `mcode provider use api-key`. The key stays in that directory. This test
// drops env vars whose names contain KEY, TOKEN, SECRET, or PASSWORD before
// starting the child, and it does not print them.
//
// AuthMode is left empty. --auth-mode desktop returns 503
// local_runtime_auth_pending for SendMessage even when the API-key provider
// is configured. That flag is the process lease, not the model provider.
//
// A short mavis turn includes tool and skill context and can use well over
// ten thousand input tokens. Do not run this without opt-in credentials.
func TestLiveModelTurn(t *testing.T) {
	if os.Getenv("MCODE_E2E_LLM") != "1" {
		t.Skip("set MCODE_E2E_LLM=1 to send one opt-in model turn")
	}
	data := os.Getenv("MCODE_LLM_DATA_DIR")
	if data == "" {
		t.Fatal("MCODE_LLM_DATA_DIR must be an isolated directory with the API-key provider already selected")
	}
	if os.Getenv("MCODE_AUTH_MODE") == "desktop" {
		t.Fatal("refusing MCODE_AUTH_MODE=desktop for a model turn; omit it")
	}
	bin := os.Getenv("MCODE_BIN")
	if bin == "" {
		bin = "mcode"
	}
	workspace := t.TempDir()
	if err := os.Chmod(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	opt := Options{
		Command:   bin,
		DataDir:   data,
		Workspace: workspace,
		AuthMode:  "",
		Env:       envWithoutSecrets(),
		Stderr:    &testLog{t},
	}
	if leading := os.Getenv("MCODE_LEADING"); leading != "" {
		opt.LeadingArgs = []string{leading}
	}
	client, err := Start(ctx, opt)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, stderrOf(client))
	}
	t.Cleanup(func() { _ = client.Close() })
	for _, arg := range client.Argv() {
		if arg == "--auth-mode" {
			t.Fatalf("model turn argv included auth mode: %v", client.Argv())
		}
	}
	if !client.VersionVerified() {
		t.Fatalf("server %q", client.ServerVersion())
	}
	agents, err := client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{})
	if err != nil {
		t.Fatal(err)
	}
	var agent string
	for _, item := range agents.Agents {
		if item.Name == nil {
			continue
		}
		name, ok := item.Name.String()
		if ok && name == "mavis" {
			agent = name
			break
		}
	}
	if agent == "" {
		t.Fatal("mavis agent was not listed")
	}
	created, err := client.Desktop().CreateSession(ctx, desktop.CreateSessionReq{Name: agent, Title: strPtr("go-sdk-llm")})
	if err != nil || created.SessionID == nil {
		t.Fatalf("create: %v %+v", err, created)
	}
	sessionID, ok := created.SessionID.String()
	if !ok || sessionID == "" {
		t.Fatal("session id")
	}
	aborted := false
	t.Cleanup(func() {
		if aborted {
			return
		}
		reason := "go-sdk llm e2e"
		abortCtx, abortCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer abortCancel()
		_, _ = client.Desktop().AbortSession(abortCtx, desktop.AbortSessionReq{ID: sessionID, Reason: &reason})
	})
	before, err := globalSelectedKey(ctx, client.Desktop())
	if err != nil {
		t.Fatal(err)
	}
	if err := selectSessionScenario(ctx, client.Desktop(), sessionID); err != nil {
		t.Fatalf("session model: %v", err)
	}
	after, err := globalSelectedKey(ctx, client.Desktop())
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("session SelectModel changed the process selection from %q to %q", before, after)
	}
	content := "Reply with exactly the single word pong and nothing else. Do not call tools or run commands."
	stream, err := client.Desktop().SendMessage(ctx, desktop.SendMessageReq{
		ID:      sessionID,
		Content: &content,
		Model:   scenarioSelection(),
	})
	if err != nil {
		t.Fatalf("SendMessage: %v\n%s", err, client.StderrTail())
	}
	defer stream.Close()
	var turn desktop.AssistantTurn
	var frames []desktop.SessionStreamFrameView
	for {
		frame, err := stream.Recv(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("stream: %v text=%q", err, turn.Text)
		}
		frames = append(frames, frame)
		desktop.ObserveSessionFrame(frame, &turn)
	}
	writeRedactedFrames(t, frames)
	if strings.TrimSpace(turn.Text) != "pong" {
		t.Fatalf("assistant text %q", turn.Text)
	}
	if strings.Contains(turn.Text, content) {
		t.Fatalf("assistant text includes the prompt: %q", turn.Text)
	}
	if !turn.SawFinished && turn.FinishReason == "" && !turn.SawSessionStatus {
		t.Fatal("completion was not signaled by finished, session_status, or finish_reason")
	}
	if !turn.HasUsage || turn.OutputTokens < 1 || turn.OutputTokens > 16000 {
		t.Fatalf("output tokens %d present %v", turn.OutputTokens, turn.HasUsage)
	}
	t.Logf("assistant=%q output_tokens=%d finished=%v session_status=%q finish_reason=%q", turn.Text, turn.OutputTokens, turn.SawFinished, turn.SessionStatus, turn.FinishReason)
	reason := "go-sdk llm e2e"
	if _, err := client.Desktop().AbortSession(ctx, desktop.AbortSessionReq{ID: sessionID, Reason: &reason}); err != nil {
		t.Fatalf("AbortSession: %v", err)
	}
	aborted = true
}

func writeRedactedFrames(t *testing.T, frames []desktop.SessionStreamFrameView) {
	t.Helper()
	path := os.Getenv("MCODE_E2E_FRAMES_OUT")
	if path == "" {
		return
	}
	type wire struct {
		EventJSON json.RawMessage `json:"event_json,omitempty"`
		DataJSON  json.RawMessage `json:"data_json,omitempty"`
	}
	out := struct {
		Provenance string `json:"provenance"`
		Frames     []wire `json:"frames"`
	}{
		Provenance: "raw SessionStreamFrameView payloads from this opt-in run, redacted",
	}
	for _, frame := range frames {
		item := wire{}
		if frame.EventJSON != nil {
			item.EventJSON = append(json.RawMessage(nil), *frame.EventJSON...)
		}
		if frame.DataJSON != nil {
			item.DataJSON = append(json.RawMessage(nil), *frame.DataJSON...)
		}
		out.Frames = append(out.Frames, item)
	}
	body, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(redact(string(body))), 0o600); err != nil {
		t.Fatal(err)
	}
}

func envWithoutSecrets() []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(name)
		if strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
