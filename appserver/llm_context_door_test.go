package appserver

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

const legacyInspectorDoorDetail = "HTTP 500 text/plain body \"Internal Server Error\". mcode app-server sets runtimeOwnerKind app-server, so GetLlmContextCapture, PutLlmContextCapture, GetLlmContextSessions, and GetSessionLlmContextOverview stay on Bwe. Bwe throws CronContractError Uh(501, LLM_CONTEXT_INSPECTOR_UNSUPPORTED_ON_LEGACY_HOST). That error has no getResponse, and the desktop router answers with the plain 500. The thrown key is not in the HTTP body. MAVIS_TUI_LLM_CONTEXT_INSPECTOR does not change this command. The SDK returns this status and body."

func legacyInspectorHTTP500(err error) bool {
	var statusErr *desktop.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 500 {
		return false
	}
	return string(statusErr.Body) == "Internal Server Error" && strings.Contains(strings.ToLower(statusErr.Headers["content-type"]), "text/plain")
}

func TestLiveLlmContextInspectorDoor(t *testing.T) {
	stderr := &bytes.Buffer{}
	client := startInspectorDoor(t, stderr, nil)
	desk := client.Desktop()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := desk.GetLlmContextCapture(ctx, desktop.GetLLMContextCaptureReq{})
	assertLegacyInspectorDoor(t, "GetLlmContextCapture", err, stderr.String())
	_, err = desk.GetLlmContextSessions(ctx, desktop.GetLLMContextSessionsReq{})
	assertLegacyInspectorDoor(t, "GetLlmContextSessions", err, stderr.String())

	title := "inspector door"
	created, err := desk.CreateSession(ctx, desktop.CreateSessionReq{Name: "mavis", Title: &title})
	if err != nil || created.SessionID == nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessionID, ok := created.SessionID.String()
	if !ok || sessionID == "" {
		t.Fatal("session id")
	}
	_, err = desk.GetSessionLlmContextOverview(ctx, desktop.GetSessionLLMContextOverviewReq{SessionID: sessionID})
	assertLegacyInspectorDoor(t, "GetSessionLlmContextOverview", err, stderr.String())
}

func TestLiveTuiInspectorEnvDoesNotChangeAppServer(t *testing.T) {
	stderr := &bytes.Buffer{}
	env := append([]string{}, os.Environ()...)
	env = append(env, "MAVIS_TUI_LLM_CONTEXT_INSPECTOR=1")
	client := startInspectorDoor(t, stderr, env)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := client.Desktop().GetLlmContextCapture(ctx, desktop.GetLLMContextCaptureReq{})
	assertLegacyInspectorDoor(t, "GetLlmContextCapture", err, stderr.String())
}

func startInspectorDoor(t *testing.T, stderr *bytes.Buffer, env []string) *Client {
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	opt := Options{
		Command:   bin,
		DataDir:   data,
		Workspace: workspace,
		AuthMode:  "desktop",
		Listen:    ListenStdio,
		Stderr:    stderr,
		Env:       env,
	}
	if leading := os.Getenv("MCODE_LEADING"); leading != "" {
		opt.LeadingArgs = []string{leading}
	}
	client, err := Start(ctx, opt)
	if err != nil {
		t.Fatalf("start: %v\n%s", err, stderr.String())
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func assertLegacyInspectorDoor(t *testing.T, method string, err error, stderr string) {
	t.Helper()
	var statusErr *desktop.StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("%s returned %v; stderr %s", method, err, stderr)
	}
	body := string(statusErr.Body)
	contentType := strings.ToLower(statusErr.Headers["content-type"])
	if statusErr.Status != 500 || body != "Internal Server Error" || !strings.Contains(contentType, "text/plain") {
		t.Fatalf("%s HTTP %d status %q headers %#v body %q\nstderr:\n%s", method, statusErr.Status, statusErr.StatusText, statusErr.Headers, body, stderr)
	}
	if strings.Contains(body, "LLM_CONTEXT_INSPECTOR") || strings.Contains(stderr, "LLM_CONTEXT_INSPECTOR_UNSUPPORTED_ON_LEGACY_HOST") {
		t.Fatalf("%s exposed the thrown key on the SDK wire or child stderr, which this app-server path does not:\nbody %s\nstderr %s", method, body, stderr)
	}
}
