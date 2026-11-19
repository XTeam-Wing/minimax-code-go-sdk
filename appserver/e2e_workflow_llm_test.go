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

// TestLiveWorkflowGoFix checks one SendMessage repair. It does not wait for
// the host verifier. TestLiveWorkflowGoalDeliverable is that lifecycle.
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
	if err != nil || !strings.Contains(string(body), "Add(2, -1) != 1") {
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
	workspace := client.opt.Workspace
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	watchPermissions(t, ctx, desk, sessionID, denyAll)
	marker := "willow-session-marker"
	content := "Reply with exactly " + marker + " and nothing else. Do not call tools."
	if err := runTurn(ctx, desk, sessionID, content, scenarioSelection()); err != nil {
		t.Fatal(err)
	}
	client.Close()
	restarted := startWorkflow(t, data, workspace, "")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel2()
	desk = restarted.Desktop()
	again, err := desk.GetSession(ctx2, desktop.GetSessionReq{ID: sessionID})
	if err != nil || again.Session == nil || again.Session.SessionID == nil {
		t.Fatalf("session missing after restart: %v", err)
	}
	if id, ok := again.Session.SessionID.String(); !ok || id != sessionID {
		t.Fatalf("session id %q", id)
	}
	limit := desktop.Number(50)
	messages, err := desk.GetMessages(ctx2, desktop.GetMessagesReq{ID: sessionID, Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if countMarker(messages, marker) != 1 {
		t.Fatalf("marker count %d after reconnect", countMarker(messages, marker))
	}
	follow := "If the conversation already contains " + marker + ", reply again and do not write files."
	if err := runTurn(ctx2, desk, sessionID, follow, scenarioSelection()); err != nil {
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
		Evidence: "GetSession and GetMessages after a new process still showed the same session and the first marker, before abort",
	})
	releaseWorkflow(t, desk, sessionID)
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
	ctx, cancel := context.WithTimeout(context.Background(), goalWall+time.Minute)
	defer cancel()
	testHash, modHash := initShareRepo(t, workspace)
	budget := desktop.Number(goalScenarioBudget)
	if raw := os.Getenv("MCODE_E2E_GOAL_TOKEN_BUDGET"); raw != "" {
		var n desktop.Number
		if err := n.UnmarshalJSON([]byte(raw)); err != nil {
			t.Fatal(err)
		}
		budget = n
	}
	objective := "SplitCents(100, 3) returns [33 33 33]. TestSplitCents wants [34 33 33]. Fix only share.go. Do not modify share_test.go or go.mod."
	watchPermissions(t, ctx, desk, sessionID, allowOnceWorkspaceWrite)
	req := desktop.CreateGoalReq{SessionID: sessionID, Objective: &objective, TokenBudget: &budget}
	if err := desktop.ValidateCreateGoal(req); err != nil {
		t.Fatal(err)
	}
	created, err := desk.CreateGoal(ctx, req)
	if err != nil || created.Goal == nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	started := time.Now()
	var wait goalWait
	wait.Started = started
	var last desktop.GoalState
	var samples []string
	stop := ""
	for {
		got, err := desk.GetGoal(ctx, desktop.GetGoalReq{SessionID: sessionID})
		if err != nil || got.Goal == nil {
			t.Fatalf("GetGoal: %v", err)
		}
		last = *got.Goal
		status, _ := last.StatusText()
		verdict := ""
		if last.LastVerification != nil {
			verdict = scalarString(last.LastVerification.Verdict)
		}
		tokens := scalarFloat(last.TokensUsed)
		requests := scalarFloat(last.RequestsUsed)
		now := time.Now()
		samples = append(samples, fmt.Sprintf("%s status=%s tokens=%.0f requests=%.0f verdict=%s reason=%s", now.Sub(started).Round(time.Second), status, tokens, requests, verdict, scalarString(last.StatusReason)))
		if verdict != "" || status != desktop.GoalStatusActive {
			break
		}
		stop = wait.step(now, tokens, requests, status, verdict)
		if stop != "" {
			break
		}
		time.Sleep(2 * time.Second)
	}
	trace := goalTrace(last, stop, samples, workspace)
	diffOK, diffText := shareDiff(workspace, testHash, modHash)
	testOut, testErr := runGoTest(workspace)
	if stop != "" || !goalSettledMet(last) || testErr != nil || !diffOK {
		writeFeatureRow(t, featureRow{
			ID:       "feature.goal-deliverable",
			Layer:    "feature",
			Result:   goalTraceResult(stop),
			Evidence: trace + "\ngo-test: " + oneLine(testOut) + "\ndiff: " + oneLine(diffText),
		})
		t.Fatalf("host verdict unproven\n%s\ngo-test: %s\n%v\ndiff: %s", trace, testOut, testErr, diffText)
	}
	writeFeatureRow(t, featureRow{
		ID:       "feature.goal-deliverable",
		Layer:    "feature",
		Result:   "pass",
		Evidence: trace,
	})
	releaseWorkflow(t, desk, sessionID)
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
		// Safety net after the test function returns. A supplied data directory
		// is not removed. Persistence assertions must already have run.
		releaseWorkflow(t, desk, sessionID)
	})
	return client, desk, sessionID, data
}

func writeGoBug(t *testing.T, dir string) {
	t.Helper()
	files := map[string]string{
		"go.mod":      "module example.com/bugfix\n\ngo 1.22\n",
		"add.go":      "package bugfix\n\nfunc Add(a, b int) int { return a - b }\n",
		"add_test.go": "package bugfix\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(Add(1, 2))\n\t}\n\tif Add(2, -1) != 1 {\n\t\tt.Fatal(Add(2, -1))\n\t}\n\tif Add(0, 0) != 0 {\n\t\tt.Fatal(Add(0, 0))\n\t}\n}\n",
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

func initShareRepo(t *testing.T, dir string) ([32]byte, [32]byte) {
	t.Helper()
	files := map[string]string{
		"go.mod":        "module example.com/share\n\ngo 1.22\n",
		"share.go":      "package share\n\nfunc SplitCents(total, n int) []int {\n\tif n <= 0 {\n\t\treturn nil\n\t}\n\tbase := total / n\n\tout := make([]int, n)\n\tfor i := range out {\n\t\tout[i] = base\n\t}\n\treturn out\n}\n",
		"share_test.go": "package share\n\nimport \"testing\"\n\nfunc TestSplitCents(t *testing.T) {\n\tgot := SplitCents(100, 3)\n\twant := []int{34, 33, 33}\n\tif len(got) != len(want) {\n\t\tt.Fatalf(\"got %v\", got)\n\t}\n\tfor i := range want {\n\t\tif got[i] != want[i] {\n\t\t\tt.Fatalf(\"got %v want %v\", got, want)\n\t\t}\n\t}\n}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, dir, "init", "-b", "master")
	runGit(t, dir, "add", "go.mod", "share.go", "share_test.go")
	runGit(t, dir, "-c", "user.email=sdk@example.com", "-c", "user.name=sdk", "commit", "-m", "equal split")
	return hashFile(t, filepath.Join(dir, "share_test.go")), hashFile(t, filepath.Join(dir, "go.mod"))
}

func hashFile(t *testing.T, path string) [32]byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(body)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func runGoTest(dir string) (string, error) {
	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func goalSettledMet(goal desktop.GoalState) bool {
	status, ok := goal.StatusText()
	if !ok || status != desktop.GoalStatusComplete || goal.LastVerification == nil || goal.LastVerification.Verdict == nil {
		return false
	}
	verdict, ok := goal.LastVerification.Verdict.String()
	return ok && verdict == "met" && scalarString(goal.StatusReason) == "complete(verifier_met)"
}

func shareDiff(dir string, testHash, modHash [32]byte) (bool, string) {
	cmd := exec.Command("git", "-C", dir, "diff", "--name-only")
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		return false, text + err.Error()
	}
	names := strings.Fields(strings.ReplaceAll(text, "\n", " "))
	if len(names) != 1 || names[0] != "share.go" {
		return false, text
	}
	if hashFileQuiet(filepath.Join(dir, "share_test.go")) != testHash || hashFileQuiet(filepath.Join(dir, "go.mod")) != modHash {
		return false, text + "\ninvariant file changed"
	}
	return true, text
}

func hashFileQuiet(path string) [32]byte {
	body, err := os.ReadFile(path)
	if err != nil {
		return [32]byte{}
	}
	return sha256.Sum256(body)
}

func releaseWorkflow(t *testing.T, desk *desktop.Client, sessionID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	reason := "sdk workflow cleanup"
	_, _ = desk.AbortSession(ctx, desktop.AbortSessionReq{ID: sessionID, Reason: &reason})
}

func mustStatus(goal desktop.GoalState) string {
	text, _ := goal.StatusText()
	return text
}

func goalTrace(goal desktop.GoalState, stop string, samples []string, workspace string) string {
	verdict := ""
	if goal.LastVerification != nil {
		verdict = scalarString(goal.LastVerification.Verdict)
	}
	usage := ""
	if goal.TokenUsage != nil {
		usage = fmt.Sprintf(" input=%.0f cache_write=%.0f cache_read=%.0f output=%.0f", scalarFloat(goal.TokenUsage.InputTokens), scalarFloat(goal.TokenUsage.CacheWriteTokens), scalarFloat(goal.TokenUsage.CacheReadTokens), scalarFloat(goal.TokenUsage.OutputTokens))
	}
	kind := "product"
	if stop != "" {
		kind = "harness_limit:" + stop
	}
	return fmt.Sprintf("%s status=%s reason=%s verdict=%s tokens=%.0f requests=%.0f%s workspace=%s samples=%s", kind, mustStatus(goal), scalarString(goal.StatusReason), verdict, scalarFloat(goal.TokensUsed), scalarFloat(goal.RequestsUsed), usage, workspace, strings.Join(samples, " | "))
}

func goalTraceResult(stop string) string {
	if stop == "" {
		return "fail"
	}
	return "limit"
}

func oneLine(text string) string {
	text = strings.ReplaceAll(text, "\n", " ")
	if len(text) > 500 {
		return text[:500]
	}
	return text
}

func scalarFloat(value *desktop.Scalar) float64 {
	if value == nil {
		return 0
	}
	n, ok := value.Float64()
	if !ok {
		return 0
	}
	return n
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
