package appserver

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// These tests are the user workflows. They skip unless MCODE_E2E_LLM=1.
// MCODE_E2E alone does not run them. Each one selects the session model and
// reads it back before any model call. None of them treats an HTTP 200 as the
// workflow result. Reports go to the feature file, outside the module.

func TestLiveWorkflowGoFix(t *testing.T) {
	_, desk, sessionID, workspace := startWorkflowLLM(t)
	writeGoBug(t, workspace)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	asks := watchPermissions(t, ctx, desk, sessionID, allowOnceWorkspaceWrite)
	content := "The Go module in this workspace fails TestAdd because Add subtracts. Change only add.go so go test passes. Do not edit add_test.go. Do not print the source in the final reply."
	if err := runTurn(ctx, desk, sessionID, content, scenarioSelection()); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "test", ".")
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go test: %v\n%s", err, out)
	}
	answered, _ := asks.snapshot()
	body, err := os.ReadFile(filepath.Join(workspace, "add_test.go"))
	if err != nil || !strings.Contains(string(body), "Add(1, 2) != 3") {
		t.Fatalf("test file changed or missing: %v %s", err, body)
	}
	writeFeatureRow(t, featureRow{
		ID:       "feature.go-bugfix",
		Layer:    "feature",
		Result:   "pass",
		Evidence: fmt.Sprintf("go test passed; permission asks %d; AllowAlways was not sent", answered),
	})
}

func TestLiveWorkflowSessionContinue(t *testing.T) {
	client, desk, sessionID, data := startWorkflowLLMParts(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	watchPermissions(t, ctx, desk, sessionID, denyAll)
	marker := "willow-session-marker"
	content := "Reply with exactly " + marker + " and nothing else. Do not call tools."
	if err := runTurn(ctx, desk, sessionID, content, scenarioSelection()); err != nil {
		t.Fatal(err)
	}
	client.Close()
	restarted := startWorkflow(t, data, t.TempDir(), "")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel2()
	desk = restarted.Desktop()
	limit := desktop.Number(50)
	messages, err := desk.GetMessages(ctx2, desktop.GetMessagesReq{ID: sessionID, Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if countMarker(messages, marker) != 1 {
		t.Fatalf("marker count %d after reconnect", countMarker(messages, marker))
	}
	again := "If the conversation already contains " + marker + ", reply again and do not write files."
	if err := runTurn(ctx2, desk, sessionID, again, scenarioSelection()); err != nil {
		t.Fatal(err)
	}
	messages, err = desk.GetMessages(ctx2, desktop.GetMessagesReq{ID: sessionID, Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if countMarker(messages, marker) < 1 {
		t.Fatal("history lost the first marker")
	}
	writeFeatureRow(t, featureRow{
		ID:       "feature.session-continue",
		Layer:    "feature",
		Result:   "pass",
		Evidence: "GetMessages after a new process still contained the first turn marker once before the follow-up",
	})
}

func TestLiveWorkflowStreamCancel(t *testing.T) {
	_, desk, sessionID, workspace := startWorkflowLLM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	target := filepath.Join(workspace, "stream-once.txt")
	watchPermissions(t, ctx, desk, sessionID, allowOnceWorkspaceWrite)
	marker := "stream-once-marker"
	content := "Write exactly one line " + marker + " into stream-once.txt and then stop. Do not write any other file."
	stream, err := desk.SendMessage(ctx, desktop.SendMessageReq{ID: sessionID, Content: &content, Model: scenarioSelection()})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		recvCtx, recvCancel := context.WithTimeout(ctx, 2*time.Second)
		_, recvErr := stream.Recv(recvCtx)
		recvCancel()
		if body, readErr := os.ReadFile(target); readErr == nil && strings.TrimSpace(string(body)) == marker {
			break
		}
		if recvErr != nil && !errors.Is(recvErr, context.DeadlineExceeded) {
			if errors.Is(recvErr, io.EOF) {
				break
			}
			t.Fatalf("stream: %v", recvErr)
		}
	}
	stream.Close()
	before, err := os.ReadFile(target)
	if err != nil || strings.TrimSpace(string(before)) != marker {
		t.Fatalf("cancel left %q (%v); a missing file is not a cancelled write", before, err)
	}
	sum := sha256.Sum256(before)
	follow := "Do not modify stream-once.txt. Reply with the single word done."
	if err := runTurn(ctx, desk, sessionID, follow, scenarioSelection()); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(after) != sum {
		t.Fatalf("file changed after reconnect %q", after)
	}
	writeFeatureRow(t, featureRow{
		ID:       "feature.stream-cancel",
		Layer:    "feature",
		Result:   "pass",
		Evidence: "stream-once.txt hash was unchanged after the stream closed and a follow-up turn",
	})
}

func TestLiveWorkflowGoalDeliverable(t *testing.T) {
	_, desk, sessionID, workspace := startWorkflowLLM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	budget := desktop.Number(24000)
	if raw := os.Getenv("MCODE_E2E_GOAL_TOKEN_BUDGET"); raw != "" {
		var n desktop.Number
		if err := n.UnmarshalJSON([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		budget = n
	}
	marker := "goal-deliverable-marker"
	objective := "Create GOAL_DONE.txt containing exactly " + marker + ". Do not change any other file."
	watchPermissions(t, ctx, desk, sessionID, allowOnceWorkspaceWrite)
	req := desktop.CreateGoalReq{SessionID: sessionID, Objective: &objective, TokenBudget: &budget}
	if err := desktop.ValidateCreateGoal(req); err != nil {
		t.Fatal(err)
	}
	created, err := desk.CreateGoal(ctx, req)
	if err != nil || created.Goal == nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	deadline := time.Now().Add(5 * time.Minute)
	var last desktop.GoalState
	for time.Now().Before(deadline) {
		got, err := desk.GetGoal(ctx, desktop.GetGoalReq{SessionID: sessionID})
		if err != nil || got.Goal == nil {
			t.Fatalf("GetGoal: %v", err)
		}
		last = *got.Goal
		status, _ := last.StatusText()
		reason := scalarString(last.StatusReason)
		if strings.Contains(reason, "budget_limited") || strings.Contains(reason, "verifier_") || status == desktop.GoalStatusBudgetLimited {
			break
		}
		if last.LastVerification != nil && last.LastVerification.Verdict != nil {
			break
		}
		if status == desktop.GoalStatusComplete && last.LastVerification != nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	body, readErr := os.ReadFile(filepath.Join(workspace, "GOAL_DONE.txt"))
	verdict := ""
	if last.LastVerification != nil {
		verdict = scalarString(last.LastVerification.Verdict)
	}
	reason := scalarString(last.StatusReason)
	if readErr != nil || strings.TrimSpace(string(body)) != marker || !strings.EqualFold(verdict, "pass") {
		t.Fatalf("deliverable %q read %v verdict %q status %q reason %q", body, readErr, verdict, mustStatus(last), reason)
	}
	writeFeatureRow(t, featureRow{
		ID:       "feature.goal-deliverable",
		Layer:    "feature",
		Result:   "pass",
		Evidence: "GOAL_DONE.txt matched and last_verification.verdict was pass",
	})
}

func TestLiveWorkflowSubagent(t *testing.T) {
	_, desk, sessionID, workspace := startWorkflowLLM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	marker := "child-product-marker"
	content := "Use the Agent tool once. The child must write child.txt containing exactly " + marker + ". After the child finishes, copy that text into parent.txt. Do not write parent.txt yourself before the child file exists."
	watchPermissions(t, ctx, desk, sessionID, allowOnceWorkspaceWrite)
	found := make(chan desktop.SubagentExecution, 1)
	go func() {
		events, err := desk.SubscribeGlobalEvents(ctx, desktop.SubscribeGlobalEventsReq{})
		if err != nil {
			return
		}
		defer events.Close()
		for {
			frame, err := events.Recv(ctx)
			if err != nil {
				return
			}
			got := desktop.Interpret(frame)
			if body, ok := got.SubagentExecutionPayload(); ok && body.Status == "started" && body.ChildSessionID != "" {
				select {
				case found <- body:
				default:
				}
			}
		}
	}()
	if err := runTurn(ctx, desk, sessionID, content, scenarioSelection()); err != nil {
		t.Fatal(err)
	}
	var child desktop.SubagentExecution
	select {
	case child = <-found:
	default:
		t.Fatal("no child session started")
	}
	session, err := desk.GetSession(ctx, desktop.GetSessionReq{ID: child.ChildSessionID})
	if err != nil || session.Session == nil {
		t.Fatalf("child session: %v", err)
	}
	childBody, err := os.ReadFile(filepath.Join(workspace, "child.txt"))
	if err != nil || strings.TrimSpace(string(childBody)) != marker {
		t.Fatalf("child file %q %v", childBody, err)
	}
	parentBody, err := os.ReadFile(filepath.Join(workspace, "parent.txt"))
	if err != nil || strings.TrimSpace(string(parentBody)) != marker {
		t.Fatalf("parent file %q %v", parentBody, err)
	}
	writeFeatureRow(t, featureRow{
		ID:       "feature.subagent-handoff",
		Layer:    "feature",
		Result:   "pass",
		Evidence: "child.txt and parent.txt both contained the marker and GetSession read the child",
	})
}

func TestLiveWorkflowPermissionDeny(t *testing.T) {
	_, desk, sessionID, workspace := startWorkflowLLM(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	asks := watchPermissions(t, ctx, desk, sessionID, denyAll)
	content := "Create denied.txt containing the word denied. If a permission prompt appears, you must not get approval."
	if err := runTurn(ctx, desk, sessionID, content, scenarioSelection()); err != nil {
		t.Fatal(err)
	}
	_, statErr := os.Stat(filepath.Join(workspace, "denied.txt"))
	if statErr == nil {
		t.Fatal("denied.txt exists")
	}
	_, denied := asks.snapshot()
	if denied == 0 {
		t.Fatal("no permission.ask was denied; a missing file without an ask is not the deny scenario")
	}
	writeFeatureRow(t, featureRow{
		ID:       "feature.permission-deny",
		Layer:    "feature",
		Result:   "pass",
		Evidence: fmt.Sprintf("denied %d ask(s); denied.txt was not created; AllowAlways was not sent", denied),
	})
}

type askCount struct {
	mu       sync.Mutex
	answered int
	denied   int
}

func (c *askCount) add(denied bool) {
	c.mu.Lock()
	c.answered++
	if denied {
		c.denied++
	}
	c.mu.Unlock()
}

func (c *askCount) snapshot() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.answered, c.denied
}

func watchPermissions(t *testing.T, ctx context.Context, desk *desktop.Client, sessionID string, allowWrite bool) *askCount {
	t.Helper()
	count := &askCount{}
	events, err := desk.SubscribeGlobalEvents(ctx, desktop.SubscribeGlobalEventsReq{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { events.Close() })
	go func() {
		for {
			frame, err := events.Recv(ctx)
			if err != nil {
				return
			}
			got := desktop.Interpret(frame)
			ask, ok := got.PermissionAskPayload()
			if !ok || (ask.SessionID != "" && ask.SessionID != sessionID) {
				continue
			}
			reply := desktop.PermissionReplyDeny
			if allowWrite && workspaceWriteTool(ask.ToolName) {
				reply = desktop.PermissionReplyAllowOnce
			}
			name := ask.AgentName
			if name == "" {
				name = "mavis"
			}
			_, _ = desk.ReplyPermission(ctx, desktop.ReplyPermissionReq{Name: name, RequestID: ask.RequestID, Reply: &reply})
			count.add(reply == desktop.PermissionReplyDeny)
		}
	}()
	return count
}

func workspaceWriteTool(name string) bool {
	switch strings.ToLower(name) {
	case "write", "edit", "apply_patch", "notebookedit":
		return true
	default:
		return false
	}
}

const (
	denyAll                 = false
	allowOnceWorkspaceWrite = true
)

func runTurn(ctx context.Context, desk *desktop.Client, sessionID, content string, model *desktop.ModelSelectionInput) error {
	stream, err := desk.SendMessage(ctx, desktop.SendMessageReq{ID: sessionID, Content: &content, Model: model})
	if err != nil {
		return err
	}
	defer stream.Close()
	var turn desktop.AssistantTurn
	for {
		frame, err := stream.Recv(ctx)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		desktop.ObserveSessionFrame(frame, &turn)
	}
}

func startWorkflowLLM(t *testing.T) (*Client, *desktop.Client, string, string) {
	t.Helper()
	client, desk, sessionID, _ := startWorkflowLLMParts(t)
	return client, desk, sessionID, client.opt.Workspace
}

func startWorkflowLLMParts(t *testing.T) (*Client, *desktop.Client, string, string) {
	t.Helper()
	if os.Getenv("MCODE_E2E_LLM") != "1" {
		t.Skip("set MCODE_E2E_LLM=1 to run a user-workflow model scenario")
	}
	data := os.Getenv("MCODE_LLM_DATA_DIR")
	if data == "" {
		t.Fatal("MCODE_LLM_DATA_DIR must be an isolated directory with the API-key provider selected")
	}
	if os.Getenv("MCODE_AUTH_MODE") == "desktop" {
		t.Fatal("refusing AuthMode desktop for a model scenario")
	}
	workspace := t.TempDir()
	if err := os.Chmod(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	client := startWorkflow(t, data, workspace, "")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	desk := client.Desktop()
	before, err := globalSelectedKey(ctx, desk)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := createWorkflowSession(t, ctx, desk, "sdk-workflow")
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
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		reason := "sdk workflow cleanup"
		_, _ = desk.AbortSession(cleanupCtx, desktop.AbortSessionReq{ID: sessionID, Reason: &reason})
	})
	return client, desk, sessionID, data
}

func writeGoBug(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"go.mod":      "module example.com/bugfix\n\ngo 1.22\n",
		"add.go":      "package bugfix\n\nfunc Add(a, b int) int { return a - b }\n",
		"add_test.go": "package bugfix\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(Add(1, 2))\n\t}\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func countMarker(resp desktop.GetMessagesResp, marker string) int {
	n := 0
	for _, message := range resp.Messages {
		if message.MsgContent == nil {
			continue
		}
		text, ok := message.MsgContent.String()
		if ok && strings.Contains(text, marker) {
			n++
		}
	}
	return n
}

func mustStatus(goal desktop.GoalState) string {
	text, _ := goal.StatusText()
	return text
}

func scalarString(value *desktop.Scalar) string {
	if value == nil {
		return ""
	}
	text, ok := value.String()
	if !ok {
		return ""
	}
	return text
}
