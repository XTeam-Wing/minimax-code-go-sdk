package appserver

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// TestLiveGoalAndSubagent is the parent-run model scenario. It skips unless
// MCODE_E2E_LLM=1 and MCODE_LLM_DATA_DIR point at an isolated directory whose
// API key was stored by `mcode provider set-minimax-key` and selected with
// `mcode provider use api-key`. This process must not receive the key.
//
// AuthMode is empty. A prompt that mentions a goal or a subagent is not a
// pass. CreateGoal must return a goal id, and GetGoal must show the same
// objective and session. A subagent counts only when a subagent.execution
// event has status started and GetSession on the child session succeeds.
// permission.ask fails the scenario; the test does not reply allow.
//
// CreateGoal starts a model turn. TokenBudget defaults to 24000 and can be
// overridden with MCODE_E2E_GOAL_TOKEN_BUDGET. This observation stops when a
// child session starts. It does not wait for the host verifier. The verifier
// lifecycle is TestLiveWorkflowGoalDeliverable. DeleteGoal and AbortSession
// run even when the model does not spawn a subagent.
func TestLiveGoalAndSubagent(t *testing.T) {
	if os.Getenv("MCODE_E2E_LLM") != "1" {
		t.Skip("set MCODE_E2E_LLM=1 to run the bounded goal and subagent scenario")
	}
	data := os.Getenv("MCODE_LLM_DATA_DIR")
	if data == "" {
		t.Fatal("MCODE_LLM_DATA_DIR must be an isolated directory with the API-key provider selected")
	}
	if os.Getenv("MCODE_AUTH_MODE") == "desktop" {
		t.Fatal("refusing AuthMode desktop for a model scenario")
	}
	bin := os.Getenv("MCODE_BIN")
	if bin == "" {
		bin = "mcode"
	}
	workspace := t.TempDir()
	if err := os.Chmod(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
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
			t.Fatalf("argv included auth mode: %v", client.Argv())
		}
	}
	desk := client.Desktop()
	agents, err := desk.ListAgents(ctx, desktop.ListAgentsReq{})
	if err != nil {
		t.Fatal(err)
	}
	agent := ""
	for _, item := range agents.Agents {
		if item.Name == nil {
			continue
		}
		name, ok := item.Name.String()
		if ok && name == "mavis" {
			agent = name
		}
	}
	if agent == "" {
		t.Fatal("mavis was not listed")
	}
	title := "sdk-goal"
	created, err := desk.CreateSession(ctx, desktop.CreateSessionReq{Name: agent, Title: &title})
	if err != nil || created.SessionID == nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionID, ok := created.SessionID.String()
	if !ok || sessionID == "" {
		t.Fatal("session id")
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		_, _ = desk.DeleteGoal(cleanupCtx, desktop.DeleteGoalReq{SessionID: sessionID})
		reason := "sdk goal cleanup"
		_, _ = desk.AbortSession(cleanupCtx, desktop.AbortSessionReq{ID: sessionID, Reason: &reason})
	})

	eventCtx, eventCancel := context.WithCancel(ctx)
	defer eventCancel()
	events, err := desk.SubscribeGlobalEvents(eventCtx, desktop.SubscribeGlobalEventsReq{})
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	found := make(chan desktop.SubagentExecution, 4)
	asks := make(chan struct{}, 1)
	go func() {
		for {
			frame, err := events.Recv(eventCtx)
			if err != nil {
				return
			}
			got := desktop.Interpret(frame)
			if got.Type == desktop.EventPermissionAsk {
				select {
				case asks <- struct{}{}:
				default:
				}
				return
			}
			if body, ok := got.SubagentExecutionPayload(); ok && body.Status == "started" {
				found <- body
			}
		}
	}()

	before, err := globalSelectedKey(ctx, desk)
	if err != nil {
		t.Fatal(err)
	}
	if err := selectSessionScenario(ctx, desk, sessionID); err != nil {
		t.Fatalf("session model: %v", err)
	}
	after, err := globalSelectedKey(ctx, desk)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("session SelectModel changed the process selection from %q to %q", before, after)
	}
	objective := "Use the Agent tool to spawn exactly one subagent whose only job is to reply pong. Do not use any other tool."
	budget := desktop.Number(24000)
	if raw := os.Getenv("MCODE_E2E_GOAL_TOKEN_BUDGET"); raw != "" {
		var n desktop.Number
		if err := n.UnmarshalJSON([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		budget = n
	}
	req := desktop.CreateGoalReq{SessionID: sessionID, Objective: &objective, TokenBudget: &budget}
	if err := desktop.ValidateCreateGoal(req); err != nil {
		t.Fatal(err)
	}
	createdGoal, err := desk.CreateGoal(ctx, req)
	if err != nil || createdGoal.Goal == nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	goalID, ok := createdGoal.Goal.ID()
	if !ok || goalID == "" {
		t.Fatal("CreateGoal returned no goal id")
	}
	if text, ok := createdGoal.Goal.ObjectiveText(); !ok || text != objective {
		t.Fatalf("create objective %q", text)
	}
	if sid, ok := createdGoal.Goal.SessionText(); !ok || sid != sessionID {
		t.Fatalf("create session %q", sid)
	}
	if status, ok := createdGoal.Goal.StatusText(); !ok || desktop.ValidateGoalStatus(status) != nil {
		t.Fatalf("create status %q", status)
	}
	fetched, err := desk.GetGoal(ctx, desktop.GetGoalReq{SessionID: sessionID})
	if err != nil || fetched.Goal == nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if id, ok := fetched.Goal.ID(); !ok || id != goalID {
		t.Fatalf("GetGoal id %q", id)
	}
	if text, ok := fetched.Goal.ObjectiveText(); !ok || text != objective {
		t.Fatalf("GetGoal objective %q", text)
	}

	var spawned desktop.SubagentExecution
	select {
	case <-asks:
		t.Fatal("permission.ask arrived; the scenario does not auto-approve")
	case spawned = <-found:
	case <-time.After(30 * time.Second):
		t.Fatal("no subagent.execution started event; mentioning a subagent in the objective is not a spawn")
	}
	if spawned.ParentSessionID != sessionID || spawned.ChildSessionID == "" || spawned.ChildSessionID == spawned.ParentSessionID {
		t.Fatalf("subagent payload %+v", spawned)
	}
	child, err := desk.GetSession(ctx, desktop.GetSessionReq{ID: spawned.ChildSessionID})
	if err != nil || child.Session == nil || child.Session.SessionID == nil {
		t.Fatalf("child session: %v", err)
	}
	if id, ok := child.Session.SessionID.String(); !ok || id != spawned.ChildSessionID {
		t.Fatalf("child id %q", id)
	}
	if child.Session.ParentSessionID != nil {
		if parent, ok := child.Session.ParentSessionID.String(); ok && parent != "" && parent != sessionID {
			t.Fatalf("child parent %q", parent)
		}
	}

	if _, err := desk.DeleteGoal(ctx, desktop.DeleteGoalReq{SessionID: sessionID}); err != nil {
		t.Fatalf("DeleteGoal: %v", err)
	}
	reason := "sdk goal"
	if _, err := desk.AbortSession(ctx, desktop.AbortSessionReq{ID: sessionID, Reason: &reason}); err != nil {
		t.Fatalf("AbortSession: %v", err)
	}
	if _, err := desk.AbortSession(ctx, desktop.AbortSessionReq{ID: spawned.ChildSessionID, Reason: &reason}); err != nil && !errors.Is(err, io.EOF) {
		var statusErr *desktop.StatusError
		if !errors.As(err, &statusErr) || statusErr.Status < 400 {
			t.Fatalf("abort child: %v", err)
		}
	}
}
