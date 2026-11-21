package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
	"github.com/XTeam-Wing/minimax-code-go-sdk/mavis"
)

// TestLiveAppServer drives the official @minimax-ai/code app-server.
// It is not a mock. Set MCODE_E2E=1 and point MCODE_BIN / MCODE_LEADING at the
// installed CLI. The test uses a temporary data directory and workspace and
// only calls read-only methods plus creating and aborting one local session.
// It does not send a model turn. AuthMode desktop is intentional here: this
// process has no API key and does not call SendMessage. A headless API-key
// model turn must omit AuthMode. See TestLiveModelTurn.
func TestLiveAppServer(t *testing.T) {
	if os.Getenv("MCODE_E2E") != "1" {
		t.Skip("set MCODE_E2E=1 to run the official app-server")
	}
	bin := os.Getenv("MCODE_BIN")
	if bin == "" {
		bin = "mcode"
	}
	data := t.TempDir()
	workspace := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	opt := Options{
		Command:   bin,
		DataDir:   data,
		Workspace: workspace,
		AuthMode:  "desktop",
		Listen:    ListenStdio,
		Stderr:    &testLog{t},
	}
	if leading := os.Getenv("MCODE_LEADING"); leading != "" {
		opt.LeadingArgs = []string{leading}
	}
	client, err := Start(ctx, opt)
	if err != nil {
		t.Fatalf("start stdio: %v\nstderr:\n%s", err, stderrOf(client))
	}
	t.Cleanup(func() { _ = client.Close() })
	t.Logf("argv=%v", client.Argv())
	t.Logf("server=%s verified=%v protocol=%d workspace=%s", client.ServerVersion(), client.VersionVerified(), client.InitializeResult().ProtocolVersion, client.InitializeResult().WorkspaceDir)
	if !client.VersionVerified() {
		t.Fatalf("expected baseline %s, got %q", BaselineVersion(), client.ServerVersion())
	}
	if !client.Feature("project.icon.update") {
		t.Fatal("missing project.icon.update")
	}
	agents, err := client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{})
	if err != nil {
		t.Fatalf("ListAgents: %v\nstderr:\n%s", err, client.StderrTail())
	}
	t.Logf("ListAgents count=%d", len(agents.Agents))
	if len(agents.Agents) > 0 && agents.Agents[0].Name != nil {
		if name, ok := agents.Agents[0].Name.String(); ok && name != "" {
			created, err := client.Desktop().CreateSession(ctx, desktop.CreateSessionReq{Name: name, Title: strPtr("go-sdk-e2e")})
			if err != nil {
				t.Logf("CreateSession blocker: %v", err)
			} else if created.SessionID == nil {
				t.Logf("CreateSession returned no session id: %+v", created.AgentName)
			} else if id, ok := created.SessionID.String(); !ok || id == "" {
				t.Logf("CreateSession session id literal=%s", rawPreview(*created.SessionID))
			} else {
				reason := "go-sdk e2e"
				if _, err := client.Desktop().AbortSession(ctx, desktop.AbortSessionReq{ID: id, Reason: &reason}); err != nil {
					t.Logf("AbortSession %s: %v", id, err)
				} else {
					t.Logf("CreateSession+AbortSession ok id=%s agent=%s", id, name)
				}
			}
		}
	}
	echo, err := client.Desktop().GetEcho(ctx, desktop.GetEchoReq{Message: "go-sdk"})
	if err != nil {
		t.Fatalf("GetEcho: %v", err)
	}
	t.Logf("GetEcho message literal=%s", rawPreview(echo.Message))
	ping, err := client.Desktop().Ping(ctx, desktop.PingReq{Message: "ping"})
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	t.Logf("Ping message literal=%s", rawPreview(ping.Message))
	health, err := mavis.New(client.Desktop()).Health(ctx)
	if err != nil {
		t.Fatalf("mavis health: %v", err)
	}
	t.Logf("mavis health status=%s runtime=%s", health.Status, health.Runtime)
	streamCtx, streamCancel := context.WithTimeout(ctx, 3*time.Second)
	stream, err := client.Desktop().SubscribeGlobalEvents(streamCtx, desktop.SubscribeGlobalEventsReq{})
	if err != nil {
		t.Fatalf("SubscribeGlobalEvents: %v", err)
	}
	eventCount := 0
	for {
		frame, err := stream.Recv(streamCtx)
		if err != nil {
			if streamCtx.Err() == nil && !errors.Is(err, io.EOF) {
				t.Fatalf("event recv: %v", err)
			}
			break
		}
		eventCount++
		got := desktop.Interpret(frame)
		t.Logf("event type=%q hasType=%v kind=%q", got.Type, got.HasType, got.Kind)
		if eventCount >= 5 {
			break
		}
	}
	streamCancel()
	if err := stream.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("event subscription received %d frame(s) then closed", eventCount)
	missing, err := client.Desktop().RawCall(ctx, "GET", "/minimax-desktop/api/v1/does-not-exist", nil, nil)
	if err != nil {
		t.Fatalf("raw transport: %v", err)
	}
	if missing.Status < 400 {
		t.Fatalf("expected an error status for an unknown path, got %d %s", missing.Status, missing.Body)
	}
	t.Logf("unknown path status=%d", missing.Status)
}

func TestLiveUnixHealth(t *testing.T) {
	if os.Getenv("MCODE_E2E") != "1" {
		t.Skip("set MCODE_E2E=1 to run the official app-server")
	}
	bin := os.Getenv("MCODE_BIN")
	if bin == "" {
		bin = "mcode"
	}
	data := t.TempDir()
	workspace := t.TempDir()
	sockDir := t.TempDir()
	// The unix listener refuses a control directory that is not mode 0700.
	for _, dir := range []string{data, workspace, sockDir} {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	socket := filepath.Join(sockDir, "s")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	opt := Options{
		Command:    bin,
		DataDir:    data,
		Workspace:  workspace,
		AuthMode:   "desktop",
		Listen:     ListenUnix,
		SocketPath: socket,
		Stderr:     &testLog{t},
	}
	if leading := os.Getenv("MCODE_LEADING"); leading != "" {
		opt.LeadingArgs = []string{leading}
	}
	client, err := Start(ctx, opt)
	if err != nil {
		t.Fatalf("start unix: %v\nstderr:\n%s", err, stderrOf(client))
	}
	t.Cleanup(func() { _ = client.Close() })
	ready, err := client.Readyz(ctx)
	if err != nil {
		t.Fatalf("readyz: %v", err)
	}
	health, err := client.Healthz(ctx)
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	if !ready.OK || !health.OK {
		t.Fatalf("ready=%v health=%v", ready.OK, health.OK)
	}
	t.Logf("unix health version=%s protocol=%d auth=%s", health.Version, health.ProtocolVersion, health.AuthMode)
	if _, err := client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{}); err != nil {
		t.Fatalf("unix ListAgents: %v", err)
	}
}

type testLog struct{ t *testing.T }

func (l *testLog) Write(p []byte) (int, error) {
	l.t.Logf("stderr: %s", redact(string(p)))
	return len(p), nil
}

func stderrOf(client *Client) string {
	if client == nil {
		return ""
	}
	return client.StderrTail()
}

func rawPreview(s desktop.Scalar) string {
	if len(s) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(s, &v); err != nil {
		return string(s)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return string(s)
	}
	return string(b)
}

// Silence unused in case a caller wants to discard stderr.
var _ io.Writer = (*testLog)(nil)

func strPtr(s string) *string { return &s }
