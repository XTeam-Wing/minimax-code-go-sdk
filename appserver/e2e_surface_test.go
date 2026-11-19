package appserver

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
	"github.com/XTeam-Wing/minimax-code-go-sdk/mavis"
)

// TestLiveSurface calls official app-server routes that do not need a model
// turn, plus the desktop-auth rejection of CreateGoal. A 503 is evidence that
// the goal execution did not start. It is not evidence that goal mode works.
func TestLiveSurface(t *testing.T) {
	client, workspace := startLive(t, "desktop")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	desk := client.Desktop()

	if !client.VersionVerified() {
		t.Fatalf("server %q", client.ServerVersion())
	}
	agents, err := desk.ListAgents(ctx, desktop.ListAgentsReq{})
	if err != nil || len(agents.Agents) == 0 {
		t.Fatalf("ListAgents: %v count %d", err, len(agents.Agents))
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
		t.Fatal("mavis was not listed")
	}

	title := "hello note"
	created, err := desk.CreateSession(ctx, desktop.CreateSessionReq{Name: agent, Title: &title})
	if err != nil || created.SessionID == nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionID, ok := created.SessionID.String()
	if !ok || sessionID == "" {
		t.Fatal("session id")
	}
	t.Cleanup(func() {
		abortCtx, abortCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer abortCancel()
		_, _ = desk.DeleteGoal(abortCtx, desktop.DeleteGoalReq{SessionID: sessionID})
		reason := "sdk surface cleanup"
		_, _ = desk.AbortSession(abortCtx, desktop.AbortSessionReq{ID: sessionID, Reason: &reason})
	})

	got, err := desk.GetSession(ctx, desktop.GetSessionReq{ID: sessionID})
	if err != nil || got.Session == nil || got.Session.Title == nil {
		t.Fatalf("GetSession: %v", err)
	}
	if text, ok := got.Session.Title.String(); !ok || text != title {
		t.Fatalf("title %q", text)
	}
	renamed := "note"
	_, err = desk.UpdateSession(ctx, desktop.UpdateSessionReq{ID: sessionID, Title: &renamed})
	var statusErr *desktop.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 422 || !strings.Contains(string(statusErr.Body), "CONTENT_POLICY_VIOLATION") {
		t.Fatalf("UpdateSession: %v", err)
	}
	listed, err := desk.ListSessions(ctx, desktop.ListSessionsReq{Name: agent})
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if !sessionListed(listed, sessionID) {
		t.Fatal("ListSessions did not include the created session")
	}

	marker := filepath.Join(workspace, "sdk-surface.txt")
	if err := os.WriteFile(marker, []byte("surface"), 0o600); err != nil {
		t.Fatal(err)
	}
	tree, err := desk.ListWorkspaceFileTree(ctx, desktop.ListFileTreeReq{Workspace: workspace})
	if err != nil {
		t.Fatalf("ListWorkspaceFileTree: %v", err)
	}
	if !treeHas(tree, "sdk-surface.txt") {
		t.Fatal("file tree did not list sdk-surface.txt")
	}

	echo, err := desk.GetEcho(ctx, desktop.GetEchoReq{Message: "surface"})
	if err != nil {
		t.Fatalf("GetEcho: %v", err)
	}
	if text, ok := echo.Message.String(); !ok || !strings.Contains(text, "surface") {
		t.Fatalf("echo %q", text)
	}
	if _, err := desk.Ping(ctx, desktop.PingReq{Message: "ping"}); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	health, err := mavis.New(desk).Health(ctx)
	if err != nil || health.Status == "" {
		t.Fatalf("mavis health: %v %+v", err, health)
	}
	_, err = client.AccountRead(ctx)
	var rpcErr *Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32601 {
		t.Fatalf("account/read: %v", err)
	}

	streamCtx, streamCancel := context.WithTimeout(ctx, 3*time.Second)
	defer streamCancel()
	events, err := desk.SubscribeGlobalEvents(streamCtx, desktop.SubscribeGlobalEventsReq{})
	if err != nil {
		t.Fatalf("SubscribeGlobalEvents: %v", err)
	}
	saw := false
	for {
		frame, err := events.Recv(streamCtx)
		if err != nil {
			if errors.Is(err, io.EOF) || streamCtx.Err() != nil {
				break
			}
			t.Fatalf("event: %v", err)
		}
		got := desktop.Interpret(frame)
		if got.Kind == "heartbeat" || got.HasType || got.Kind != "" {
			saw = true
			break
		}
	}
	_ = events.Close()
	if !saw {
		t.Fatal("event stream produced no frame")
	}

	objective := "reply with the single word pong and do not call tools"
	createdGoal, err := desk.CreateGoal(ctx, desktop.CreateGoalReq{SessionID: sessionID, Objective: &objective})
	if err != nil || createdGoal.Goal == nil {
		t.Fatalf("CreateGoal: %v", err)
	}
	goalID, ok := createdGoal.Goal.ID()
	if !ok || goalID == "" {
		t.Fatal("CreateGoal goal id")
	}
	if text, ok := createdGoal.Goal.ObjectiveText(); !ok || text != objective {
		t.Fatalf("goal objective %q", text)
	}
	if sid, ok := createdGoal.Goal.SessionText(); !ok || sid != sessionID {
		t.Fatalf("goal session %q", sid)
	}
	if status, ok := createdGoal.Goal.StatusText(); ok {
		if err := desktop.ValidateGoalStatus(status); err != nil {
			t.Fatal(err)
		}
	}
	fetched, err := desk.GetGoal(ctx, desktop.GetGoalReq{SessionID: sessionID})
	if err != nil || fetched.Goal == nil {
		t.Fatalf("GetGoal: %v", err)
	}
	if id, ok := fetched.Goal.ID(); !ok || id != goalID {
		t.Fatalf("GetGoal id %q", id)
	}
	deleted, err := desk.DeleteGoal(ctx, desktop.DeleteGoalReq{SessionID: sessionID})
	if err != nil {
		t.Fatalf("DeleteGoal: %v", err)
	}
	if deleted.Success != nil {
		if ok, yes := deleted.Success.Bool(); yes && !ok {
			t.Fatal("DeleteGoal success=false")
		}
	}
	after, err := desk.GetGoal(ctx, desktop.GetGoalReq{SessionID: sessionID})
	if err == nil && after.Goal != nil {
		if id, ok := after.Goal.ID(); ok && id == goalID {
			status, _ := after.Goal.StatusText()
			t.Fatalf("goal %s still present after delete, status %s", id, status)
		}
	}

	missing, err := desk.RawCall(ctx, "GET", "/minimax-desktop/api/v1/does-not-exist", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if missing.Status < 400 {
		t.Fatalf("unknown path status %d", missing.Status)
	}

	reason := "sdk surface"
	if _, err := desk.AbortSession(ctx, desktop.AbortSessionReq{ID: sessionID, Reason: &reason}); err != nil {
		t.Fatalf("AbortSession: %v", err)
	}
}

func sessionListed(list desktop.ListSessionsResp, id string) bool {
	for _, item := range list.Sessions {
		if item.SessionID == nil {
			continue
		}
		got, ok := item.SessionID.String()
		if ok && got == id {
			return true
		}
	}
	return false
}

func treeHas(tree desktop.ListFileTreeResp, name string) bool {
	for _, entry := range tree.Entries {
		got, ok := entry.Name.String()
		if ok && got == name {
			return true
		}
	}
	return false
}

func startLive(t *testing.T, authMode string) (*Client, string) {
	t.Helper()
	if os.Getenv("MCODE_E2E") != "1" {
		t.Skip("set MCODE_E2E=1 to run the official app-server")
	}
	bin := os.Getenv("MCODE_BIN")
	if bin == "" {
		bin = "mcode"
	}
	data := t.TempDir()
	workspace := t.TempDir()
	for _, dir := range []string{data, workspace} {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	opt := Options{
		Command:   bin,
		DataDir:   data,
		Workspace: workspace,
		AuthMode:  authMode,
		Listen:    ListenStdio,
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
	return client, workspace
}
