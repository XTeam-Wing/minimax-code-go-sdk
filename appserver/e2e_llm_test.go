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
// One short turn still costs on the order of 17k input tokens because the
// mavis agent context includes tools and skills. Do not run this without
// opt-in credentials.
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
	models, err := client.Desktop().ListModels(ctx, desktop.ListModelsReq{})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	model := pickLiveModel(models, os.Getenv("MCODE_E2E_MODEL"))
	content := "Reply with exactly the single word pong and nothing else. Do not call tools or run commands."
	stream, err := client.Desktop().SendMessage(ctx, desktop.SendMessageReq{
		ID:      sessionID,
		Content: &content,
		Model:   model,
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
	if !turn.HasUsage || turn.OutputTokens < 1 || turn.OutputTokens > 64 {
		t.Fatalf("output tokens %d present %v", turn.OutputTokens, turn.HasUsage)
	}
	t.Logf("assistant=%q output_tokens=%d finished=%v session_status=%q finish_reason=%q", turn.Text, turn.OutputTokens, turn.SawFinished, turn.SessionStatus, turn.FinishReason)
	reason := "go-sdk llm e2e"
	if _, err := client.Desktop().AbortSession(ctx, desktop.AbortSessionReq{ID: sessionID, Reason: &reason}); err != nil {
		t.Fatalf("AbortSession: %v", err)
	}
	aborted = true
}

func pickLiveModel(resp desktop.ListModelsResp, override string) *desktop.ModelSelectionInput {
	maxOut := desktop.Number(64)
	low := "low"
	thinking := &desktop.ModelThinkingInput{Effort: &low}
	if override != "" {
		provider := "minimax"
		modelID := override
		return &desktop.ModelSelectionInput{ProviderID: &provider, ModelID: &modelID, MaxOutputTokens: &maxOut, Thinking: thinking}
	}
	var selected, flash, first *desktop.ModelInfo
	for i := range resp.Models {
		item := &resp.Models[i]
		if first == nil {
			first = item
		}
		if item.Selected != nil {
			if on, ok := item.Selected.Bool(); ok && on {
				selected = item
			}
		}
		if id, ok := item.ModelID.String(); ok && strings.Contains(strings.ToLower(id), "flash") && flash == nil {
			flash = item
		}
	}
	choice := selected
	if choice == nil {
		choice = flash
	}
	if choice == nil {
		choice = first
	}
	if choice == nil {
		provider := "minimax"
		modelID := "MiniMax-M3.1-Flash"
		return &desktop.ModelSelectionInput{ProviderID: &provider, ModelID: &modelID, MaxOutputTokens: &maxOut, Thinking: thinking}
	}
	sel := &desktop.ModelSelectionInput{MaxOutputTokens: &maxOut, Thinking: thinking}
	if s, ok := choice.ProviderID.String(); ok {
		sel.ProviderID = &s
	}
	if s, ok := choice.ModelID.String(); ok {
		sel.ModelID = &s
	}
	return sel
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
