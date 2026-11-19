package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// The caller selected these three values for the goal scenario. They are the
// session SelectModel fields, not a CreateGoal field and not a process default.
const (
	scenarioProviderID = "minimax_api"
	scenarioModelID    = "MiniMax-M3.1-Flash"
	scenarioEffort     = "high"
	scenarioModelKey   = scenarioProviderID + "/" + scenarioModelID
)

func TestLiveSessionModelEffortReadback(t *testing.T) {
	data := os.Getenv("MCODE_LLM_DATA_DIR")
	if data == "" {
		t.Skip("SelectModel minimax_api/MiniMax-M3.1-Flash on a data directory without an API key returns HTTP 400 NO_API_KEY. Set MCODE_LLM_DATA_DIR to an isolated directory already prepared with provider set-minimax-key and provider use api-key. This test does not send a model turn.")
	}
	workspace := t.TempDir()
	client := startWorkflow(t, data, workspace, "")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	desk := client.Desktop()
	before, err := globalSelectedKey(ctx, desk)
	if err != nil {
		t.Fatal(err)
	}
	sessionID := createWorkflowSession(t, ctx, desk, "sdk-effort")
	if err := selectSessionScenario(ctx, desk, sessionID); err != nil {
		t.Fatal(err)
	}
	after, err := globalSelectedKey(ctx, desk)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("session SelectModel changed the process selection from %q to %q", before, after)
	}
	other := createWorkflowSession(t, ctx, desk, "sdk-effort-other")
	if key, effort, ok := sessionModelReadback(t, ctx, desk, other); ok && key == scenarioModelKey && effort == scenarioEffort {
		t.Fatalf("second session inherited %s effort %s", key, effort)
	}
	client.Close()

	restarted := startWorkflow(t, data, workspace, "desktop")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel2()
	key, effort, ok := sessionModelReadback(t, ctx2, restarted.Desktop(), sessionID)
	if !ok || key != scenarioModelKey || effort != scenarioEffort {
		t.Fatalf("restart readback key %q effort %q", key, effort)
	}
	writeFeatureRow(t, featureRow{
		ID:       "feature.session-model-readback",
		Layer:    "feature",
		Result:   "pass",
		Evidence: "GetSession effective_model and model.thinking.effort after process restart; process selection unchanged",
	})
}

func TestLiveSessionRecordReconnect(t *testing.T) {
	data := t.TempDir()
	workspace := t.TempDir()
	client := startWorkflow(t, data, workspace, "desktop")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	title := "sdk-reconnect-title"
	sessionID := createWorkflowSession(t, ctx, client.Desktop(), title)
	client.Close()

	restarted := startWorkflow(t, data, workspace, "desktop")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel2()
	desk := restarted.Desktop()
	limit := desktop.Number(50)
	listed, err := desk.ListSessions(ctx2, desktop.ListSessionsReq{Name: "mavis", Limit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, item := range listed.Sessions {
		id, _ := item.SessionID.String()
		got, _ := item.Title.String()
		if id == sessionID && got == title {
			found = true
		}
	}
	if !found {
		t.Fatalf("session %s title %q was not listed after restart", sessionID, title)
	}
	got, err := desk.GetSession(ctx2, desktop.GetSessionReq{ID: sessionID})
	if err != nil || got.Session == nil {
		t.Fatalf("GetSession: %v", err)
	}
	if text, ok := got.Session.Title.String(); !ok || text != title {
		t.Fatalf("title %q", text)
	}
	messages, err := desk.GetMessages(ctx2, desktop.GetMessagesReq{ID: sessionID})
	if err != nil {
		t.Fatal(err)
	}
	writeFeatureRow(t, featureRow{
		ID:       "feature.session-reconnect",
		Layer:    "feature",
		Result:   "pass",
		Evidence: fmt.Sprintf("session %s title survived restart; message count %d; no model turn was sent", sessionID, len(messages.Messages)),
	})
}

func TestLiveMemorySurvivesRestart(t *testing.T) {
	data := t.TempDir()
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "config.yaml"), []byte("memory:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	client := startWorkflow(t, data, workspace, "desktop")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	name := "mavis"
	marker := "sdk-memory-across-restart"
	if _, err := client.Desktop().AppendMemory(ctx, desktop.AppendMemoryReq{Content: marker + "\n", AgentName: &name}); err != nil {
		t.Fatalf("append: %v", err)
	}
	first := createWorkflowSession(t, ctx, client.Desktop(), "sdk-memory-a")
	client.Close()

	restarted := startWorkflow(t, data, workspace, "desktop")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel2()
	second := createWorkflowSession(t, ctx2, restarted.Desktop(), "sdk-memory-b")
	if first == second {
		t.Fatal("restart created the same session id")
	}
	got, err := restarted.Desktop().GetMemory(ctx2, desktop.GetMemoryReq{AgentName: &name})
	if err != nil || got.Content == nil {
		t.Fatalf("get: %v", err)
	}
	text, ok := got.Content.String()
	if !ok || !strings.Contains(text, marker) {
		t.Fatalf("content %q", text)
	}
	searched, err := restarted.Desktop().SearchMemory(ctx2, desktop.SearchMemoryReq{Query: marker, AgentName: &name})
	if err != nil || !searchHasLine(searched, marker) {
		t.Fatalf("search: %v %#v", err, searched.Results)
	}
	writeFeatureRow(t, featureRow{
		ID:       "feature.memory-across-session",
		Layer:    "feature",
		Result:   "pass",
		Evidence: "memory written in one process was read and searched after restart from a second session",
	})
}

func selectSessionScenario(ctx context.Context, desk *desktop.Client, sessionID string) error {
	effort := scenarioEffort
	selected, err := desk.SelectModel(ctx, desktop.SelectModelReq{
		ProviderID: scenarioProviderID,
		ModelID:    scenarioModelID,
		SessionID:  &sessionID,
		Thinking:   &desktop.ModelThinkingInput{Effort: &effort},
	})
	if err != nil {
		return err
	}
	if !scalarTrue(selected.Success) {
		return fmt.Errorf("SelectModel success was not true")
	}
	key, got, ok := readSessionModel(ctx, desk, sessionID)
	if !ok || key != scenarioModelKey || got != scenarioEffort {
		return fmt.Errorf("readback key %q effort %q", key, got)
	}
	return nil
}

func scenarioSelection() *desktop.ModelSelectionInput {
	provider := scenarioProviderID
	model := scenarioModelID
	effort := scenarioEffort
	return &desktop.ModelSelectionInput{
		ProviderID: &provider,
		ModelID:    &model,
		Thinking:   &desktop.ModelThinkingInput{Effort: &effort},
	}
}

func readSessionModel(ctx context.Context, desk *desktop.Client, sessionID string) (string, string, bool) {
	got, err := desk.GetSession(ctx, desktop.GetSessionReq{ID: sessionID})
	if err != nil || got.Session == nil || got.Session.Model == nil || got.Session.Model.Thinking == nil || got.Session.Model.Thinking.Effort == nil {
		return "", "", false
	}
	provider, _ := got.Session.Model.ProviderID.String()
	model, _ := got.Session.Model.ModelID.String()
	effective, _ := got.Session.EffectiveModel.String()
	if effective != "" && effective != provider+"/"+model {
		return effective, *got.Session.Model.Thinking.Effort, false
	}
	return provider + "/" + model, *got.Session.Model.Thinking.Effort, provider != "" && model != ""
}

func sessionModelReadback(t *testing.T, ctx context.Context, desk *desktop.Client, sessionID string) (string, string, bool) {
	t.Helper()
	return readSessionModel(ctx, desk, sessionID)
}

func globalSelectedKey(ctx context.Context, desk *desktop.Client) (string, error) {
	listed, err := desk.ListModels(ctx, desktop.ListModelsReq{})
	if err != nil {
		return "", err
	}
	var key string
	for _, item := range listed.Models {
		on, ok := item.Selected.Bool()
		if !ok || !on {
			continue
		}
		provider, _ := item.ProviderID.String()
		model, _ := item.ModelID.String()
		if key != "" {
			return "", fmt.Errorf("more than one selected model")
		}
		key = provider + "/" + model
	}
	return key, nil
}

func createWorkflowSession(t *testing.T, ctx context.Context, desk *desktop.Client, title string) string {
	t.Helper()
	created, err := desk.CreateSession(ctx, desktop.CreateSessionReq{Name: "mavis", Title: &title})
	if err != nil || created.SessionID == nil {
		t.Fatalf("CreateSession: %v", err)
	}
	id, ok := created.SessionID.String()
	if !ok || id == "" {
		t.Fatal("session id")
	}
	return id
}

func startWorkflow(t *testing.T, data, workspace, auth string) *Client {
	t.Helper()
	if os.Getenv("MCODE_E2E") != "1" && os.Getenv("MCODE_E2E_LLM") != "1" {
		t.Skip("set MCODE_E2E=1 to run the official app-server")
	}
	bin := os.Getenv("MCODE_BIN")
	if bin == "" {
		bin = "mcode"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	opt := Options{
		Command:   bin,
		DataDir:   data,
		Workspace: workspace,
		AuthMode:  auth,
		Listen:    ListenStdio,
		Stderr:    &testLog{t},
	}
	if leading := os.Getenv("MCODE_LEADING"); leading != "" {
		opt.LeadingArgs = []string{leading}
	}
	if auth == "" {
		opt.Env = envWithoutSecrets()
	}
	client, err := Start(ctx, opt)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, stderrOf(client))
	}
	t.Cleanup(func() { _ = client.Close() })
	if !client.VersionVerified() {
		t.Fatalf("server %q", client.ServerVersion())
	}
	return client
}

type featureRow struct {
	ID       string `json:"id"`
	Layer    string `json:"layer"`
	Result   string `json:"result"`
	Evidence string `json:"evidence"`
}

func featureReportPath() string {
	if path := os.Getenv("MCODE_FEATURE_REPORT"); path != "" {
		return path
	}
	return filepath.Join(os.TempDir(), "mcode-feature-report.json")
}

func writeFeatureRow(t *testing.T, row featureRow) {
	t.Helper()
	if row.Layer != "feature" || len(row.ID) < 8 || row.ID[:8] != "feature." {
		t.Fatalf("feature row %q", row.ID)
	}
	path := featureReportPath()
	var doc struct {
		Layer string       `json:"layer"`
		Rows  []featureRow `json:"rows"`
	}
	if body, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(body, &doc)
	}
	rows := doc.Rows
	replaced := false
	for i := range rows {
		if rows[i].ID == row.ID {
			rows[i] = row
			replaced = true
		}
	}
	if !replaced {
		rows = append(rows, row)
	}
	body, err := json.MarshalIndent(struct {
		Layer string       `json:"layer"`
		Rows  []featureRow `json:"rows"`
	}{Layer: "feature", Rows: rows}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
}
