package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

// assertLocalCRUD creates temporary resources and checks the fields that
// come back. A decoded 2xx without the written value is not recorded as real.
func assertLocalCRUD(t *testing.T, ctx context.Context, client *Client, desk *desktop.Client, ledger *coverageLedger, workspace, agent, sessionID, projectKey string) {
	t.Helper()
	assertEcho(t, ctx, desk, ledger)
	assertComposer(t, ctx, desk, ledger)
	assertShellPreference(t, ctx, desk, ledger)
	assertMCPServer(t, ctx, desk, ledger)
	assertArtifactSpace(t, ctx, desk, ledger)
	assertPinSession(t, ctx, desk, ledger, sessionID)
	assertDeletedSession(t, ctx, desk, ledger, agent)
	assertDisabledCron(t, ctx, desk, ledger, agent)
	assertPrewarm(t, ctx, desk, ledger, workspace)
	assertAccountRead(t, ctx, client, ledger)
	assertEventStream(t, ctx, desk, ledger)
	assertPrepareRestart(t, ledger)
	assertUnixHealth(t, ledger)
	assertRoundTrips(t, ctx, desk, ledger, workspace, agent, sessionID, projectKey)
	assertTopo(t, ctx, client, desk, ledger, workspace, agent, sessionID)
	assertLocalExtras(t, ctx, desk, ledger, workspace, agent, projectKey)
}

func assertEcho(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	echo, err := desk.GetEcho(ctx, desktop.GetEchoReq{Message: "crud-echo"})
	if err != nil {
		noteServer(t, ledger, "idl.GetEcho", err)
		return
	}
	text, ok := echo.Message.String()
	if !ok || !strings.Contains(text, "crud-echo") {
		t.Fatalf("GetEcho %q", text)
	}
	ledger.note("idl.GetEcho", "real", "message contains crud-echo")
	sent, err := desk.SubmitFieldEcho(ctx, desktop.SubmitFieldEchoReq{Title: "sdk-field", ClientName: "go-sdk"})
	if err != nil {
		noteServer(t, ledger, "idl.SubmitFieldEcho", err)
		return
	}
	body, ok := sent.Message.String()
	if !ok || !strings.Contains(body, "sdk-field") {
		t.Fatalf("SubmitFieldEcho %q", body)
	}
	ledger.note("idl.SubmitFieldEcho", "real", "message contains sdk-field")
}

func assertComposer(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	current, err := desk.GetComposerSendBehavior(ctx, desktop.GetComposerSendBehaviorReq{})
	if err != nil {
		noteServer(t, ledger, "idl.GetComposerSendBehavior", err)
		return
	}
	original, ok := current.Behavior.String()
	if !ok || original == "" {
		t.Fatal("composer send behavior empty")
	}
	flipComposer(t, ledger, "idl.GetComposerSendBehavior", "idl.PutComposerSendBehavior", original, []string{"queue", "steer"}, func(behavior string) (string, error) {
		put, err := desk.PutComposerSendBehavior(ctx, desktop.PutComposerSendBehaviorReq{Behavior: behavior})
		if err != nil {
			return "", err
		}
		return behaviorText(put.Behavior)
	}, func() (string, error) {
		got, err := desk.GetComposerSendBehavior(ctx, desktop.GetComposerSendBehaviorReq{})
		if err != nil {
			return "", err
		}
		return behaviorText(got.Behavior)
	})
	enter, err := desk.GetComposerEnterBehavior(ctx, desktop.GetComposerEnterBehaviorReq{})
	if err != nil {
		noteServer(t, ledger, "idl.GetComposerEnterBehavior", err)
		return
	}
	enterOriginal, ok := enter.Behavior.String()
	if !ok || enterOriginal == "" {
		t.Fatal("composer enter behavior empty")
	}
	flipComposer(t, ledger, "idl.GetComposerEnterBehavior", "idl.PutComposerEnterBehavior", enterOriginal, []string{"enter", "cmdIfMultiline", "cmdAlways"}, func(behavior string) (string, error) {
		put, err := desk.PutComposerEnterBehavior(ctx, desktop.PutComposerEnterBehaviorReq{Behavior: behavior})
		if err != nil {
			return "", err
		}
		return behaviorText(put.Behavior)
	}, func() (string, error) {
		got, err := desk.GetComposerEnterBehavior(ctx, desktop.GetComposerEnterBehaviorReq{})
		if err != nil {
			return "", err
		}
		return behaviorText(got.Behavior)
	})
}

func behaviorText(value desktop.Scalar) (string, error) {
	text, ok := value.String()
	if !ok || text == "" {
		return "", errors.New("behavior was not a string")
	}
	return text, nil
}

func flipComposer(t *testing.T, ledger *coverageLedger, getID, putID, original string, candidates []string, put func(string) (string, error), get func() (string, error)) {
	t.Helper()
	var accepted string
	for _, candidate := range candidates {
		if candidate == original {
			continue
		}
		got, err := put(candidate)
		if err != nil {
			continue
		}
		if got == candidate {
			accepted = candidate
			break
		}
	}
	if _, err := put(original); err != nil {
		t.Fatalf("restore %s: %v", putID, err)
	}
	restored, err := get()
	if err != nil || restored != original {
		t.Fatalf("restored %s %q: %v", getID, restored, err)
	}
	ledger.note(getID, "real", "behavior "+original)
	if accepted == "" {
		ledger.note(putID, "notrun", "server kept "+original+"; tried "+strings.Join(candidates, ", "))
		return
	}
	ledger.note(putID, "real", "behavior changed to "+accepted+" and restored to "+original)
}

func assertShellPreference(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	status, err := desk.GetAgentShellStatus(ctx, desktop.GetAgentShellStatusReq{})
	if err != nil || status.Status == nil {
		noteServer(t, ledger, "idl.GetAgentShellStatus", err)
		return
	}
	original, ok := status.Status.Preference.String()
	if !ok || original == "" {
		supported, _ := status.Status.Supported.Bool()
		ledger.note("idl.GetAgentShellStatus", "real", "supported decoded; preference was empty")
		ledger.note("idl.PutAgentShellPreference", "notrun", "no preference string was stored to change")
		if !supported && len(status.Status.Supported) == 0 {
			ledger.note("idl.GetAgentShellStatus", "notrun", "status decoded with neither supported nor preference set")
		}
		return
	}
	next := desktop.AgentShellGitBash
	if original == string(desktop.AgentShellGitBash) {
		next = desktop.AgentShellAuto
	}
	put, err := desk.PutAgentShellPreference(ctx, desktop.PutAgentShellPreferenceReq{Preference: next})
	if err != nil {
		noteServer(t, ledger, "idl.PutAgentShellPreference", err)
		ledger.note("idl.GetAgentShellStatus", "real", "preference "+original)
		return
	}
	if put.Status == nil {
		t.Fatal("shell put status")
	}
	got, ok := put.Status.Preference.String()
	if !ok || got != string(next) {
		t.Fatalf("shell preference %q", got)
	}
	again, err := desk.GetAgentShellStatus(ctx, desktop.GetAgentShellStatusReq{})
	if err != nil || again.Status == nil {
		t.Fatalf("shell reread: %v", err)
	}
	reread, ok := again.Status.Preference.String()
	if !ok || reread != string(next) {
		t.Fatalf("stored shell preference %q", reread)
	}
	restore := desktop.AgentShellPreference(original)
	if _, err := desk.PutAgentShellPreference(ctx, desktop.PutAgentShellPreferenceReq{Preference: restore}); err != nil {
		t.Fatalf("restore shell: %v", err)
	}
	ledger.note("idl.GetAgentShellStatus", "real", "preference changed to "+string(next))
	ledger.note("idl.PutAgentShellPreference", "real", "preference restored to "+original)
}

func assertMCPServer(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	const name = "sdkmcp"
	command := "true"
	description := "sdk mcp"
	enabled := true
	created, err := desk.CreateLocalMcpServer(ctx, desktop.CreateLocalMCPServerReq{
		Name:    name,
		Enabled: &enabled,
		Config: &desktop.LocalMCPServerConfig{
			Transport:   desktop.LocalMcpTransportStdio,
			Command:     &command,
			Description: &description,
		},
	})
	if err != nil || created.Server == nil || created.Server.Config == nil {
		noteServer(t, ledger, "idl.CreateLocalMcpServer", err)
		return
	}
	gotName, ok := created.Server.Name.String()
	gotCommand := ""
	if created.Server.Config.Command != nil {
		gotCommand = *created.Server.Config.Command
	}
	if !ok || gotName != name || gotCommand != command {
		t.Fatalf("mcp create name %q command %q", gotName, gotCommand)
	}
	listed, err := desk.ListLocalMcpServers(ctx, desktop.ListLocalMCPServersReq{})
	if err != nil || !mcpListed(listed, name) {
		t.Fatalf("ListLocalMcpServers: %v", err)
	}
	updatedDescription := "sdk mcp renamed"
	updated, err := desk.UpdateLocalMcpServer(ctx, desktop.UpdateLocalMCPServerReq{
		ServerName: name,
		Config: &desktop.LocalMCPServerConfig{
			Transport:   desktop.LocalMcpTransportStdio,
			Command:     &command,
			Description: &updatedDescription,
		},
	})
	if err != nil || updated.Server == nil || updated.Server.Config == nil || updated.Server.Config.Description == nil || *updated.Server.Config.Description != updatedDescription {
		t.Fatalf("UpdateLocalMcpServer: %v", err)
	}
	disabled, err := desk.SetLocalMcpServerEnabled(ctx, desktop.SetLocalMCPServerEnabledReq{ServerName: name, Enabled: false})
	if err != nil || disabled.Server == nil {
		t.Fatalf("SetLocalMcpServerEnabled: %v", err)
	}
	flag, ok := disabled.Server.Enabled.Bool()
	if !ok || flag {
		t.Fatalf("mcp enabled %#v", disabled.Server.Enabled)
	}
	fetched, err := desk.GetLocalMcpServer(ctx, desktop.GetLocalMCPServerReq{ServerName: name})
	if err != nil || fetched.Server == nil || fetched.Server.Config == nil || fetched.Server.Config.Description == nil || *fetched.Server.Config.Description != updatedDescription {
		t.Fatalf("GetLocalMcpServer: %v", err)
	}
	assertSyntheticSecret(t, ctx, desk, ledger, name)
	runtimeServer, err := desk.GetLocalMcpRuntimeServer(ctx, desktop.GetLocalMCPRuntimeServerReq{ServerName: name})
	if err != nil {
		recordCall(t, ledger, "idl.GetLocalMcpRuntimeServer", err)
	} else if got, ok := runtimeServer.Name.String(); !ok || got != name {
		t.Fatalf("runtime server %q", got)
	} else {
		ledger.note("idl.GetLocalMcpRuntimeServer", "real", "name sdkmcp")
	}
	tools, err := desk.ListLocalMcpRuntimeTools(ctx, desktop.ListLocalMCPRuntimeToolsReq{ServerName: name})
	if err != nil {
		recordCall(t, ledger, "idl.ListLocalMcpRuntimeTools", err)
	} else if got, ok := tools.Server.String(); !ok || got != name {
		t.Fatalf("runtime tools server %q", got)
	} else {
		ledger.note("idl.ListLocalMcpRuntimeTools", "real", "server sdkmcp")
	}
	disabledRuntime, err := desk.DisableLocalMcpRuntimeServer(ctx, desktop.DisableLocalMCPRuntimeServerReq{ServerName: name})
	if err != nil {
		recordCall(t, ledger, "idl.DisableLocalMcpRuntimeServer", err)
	} else if flag, ok := disabledRuntime.Disabled.Bool(); !ok || !flag {
		t.Fatalf("runtime disabled %#v", disabledRuntime.Disabled)
	} else {
		ledger.note("idl.DisableLocalMcpRuntimeServer", "real", "disabled true")
	}
	tested, err := desk.TestLocalMcpServer(ctx, desktop.TestLocalMCPServerReq{ServerName: name})
	if err != nil || tested.Result == nil {
		recordCall(t, ledger, "idl.TestLocalMcpServer", err)
	} else if detail, ok := populatedDetail(reflect.ValueOf(tested.Result)); ok {
		ledger.note("idl.TestLocalMcpServer", "real", detail)
	} else {
		ledger.note("idl.TestLocalMcpServer", "notrun", "test result had no populated field")
	}
	if _, err := desk.DeleteLocalMcpServer(ctx, desktop.DeleteLocalMCPServerReq{ServerName: name}); err != nil {
		t.Fatalf("DeleteLocalMcpServer: %v", err)
	}
	after, err := desk.ListLocalMcpServers(ctx, desktop.ListLocalMCPServersReq{})
	if err != nil || mcpListed(after, name) {
		t.Fatalf("mcp server still listed: %v", err)
	}
	ledger.note("idl.CreateLocalMcpServer", "real", "name sdkmcp command true")
	ledger.note("idl.ListLocalMcpServers", "real", "sdkmcp listed then absent")
	ledger.note("idl.UpdateLocalMcpServer", "real", "description sdk mcp renamed")
	ledger.note("idl.SetLocalMcpServerEnabled", "real", "enabled false")
	ledger.note("idl.GetLocalMcpServer", "real", "description reread")
	ledger.note("idl.DeleteLocalMcpServer", "real", "name absent after delete")
}

func assertSyntheticSecret(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, name string) {
	t.Helper()
	const secret = "sdk-synthetic-not-a-real-secret"
	_, err := desk.SetLocalMcpServerOAuthClientSecret(ctx, desktop.SetLocalMCPServerOAuthClientSecretReq{ServerName: name, ClientSecret: crudPtr(secret)})
	if err != nil {
		var statusErr *desktop.StatusError
		if errors.As(err, &statusErr) && strings.Contains(string(statusErr.Body), "MCP_OAUTH_TARGET_NOT_FOUND") {
			ledger.note("idl.SetLocalMcpServerOAuthClientSecret", "notrun", "HTTP 404 MCP_OAUTH_TARGET_NOT_FOUND: a stdio command server is not an OAuth target. Success needs an MCP server whose config has an OAuth client, then a synthetic secret in this temp data dir, and a reread that exposes only client_secret_configured")
			return
		}
		noteServer(t, ledger, "idl.SetLocalMcpServerOAuthClientSecret", err)
		return
	}
	fetched, err := desk.GetLocalMcpServer(ctx, desktop.GetLocalMCPServerReq{ServerName: name})
	if err != nil || fetched.Server == nil || fetched.Server.Config == nil || fetched.Server.Config.OAuth == nil || fetched.Server.Config.OAuth.ClientSecretConfigured == nil {
		ledger.note("idl.SetLocalMcpServerOAuthClientSecret", "notrun", "secret write returned success but client_secret_configured was absent")
		return
	}
	configured, ok := fetched.Server.Config.OAuth.ClientSecretConfigured.Bool()
	raw, marshalErr := json.Marshal(fetched)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("synthetic MCP secret was returned in clear text")
	}
	if !ok || !configured {
		ledger.note("idl.SetLocalMcpServerOAuthClientSecret", "notrun", "client_secret_configured was not true")
		return
	}
	ledger.note("idl.SetLocalMcpServerOAuthClientSecret", "real", "synthetic secret stored in the temp data dir and read back only as client_secret_configured=true")
}

func assertArtifactSpace(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	const name = "sdk space"
	saved, err := desk.SaveArtifactSpace(ctx, desktop.SaveArtifactSpaceReq{Name: name})
	if err != nil || saved.Space == nil {
		noteServer(t, ledger, "idl.SaveArtifactSpace", err)
		return
	}
	id, ok := saved.Space.ID.String()
	got, nameOK := saved.Space.Name.String()
	if !ok || id == "" || !nameOK || got != name {
		t.Fatalf("artifact space id %q name %q", id, got)
	}
	fetched, err := desk.GetArtifactSpace(ctx, desktop.GetArtifactSpaceReq{ID: id})
	if err != nil || fetched.Space == nil {
		t.Fatalf("GetArtifactSpace: %v", err)
	}
	reread, ok := fetched.Space.Name.String()
	if !ok || reread != name {
		t.Fatalf("artifact name %q", reread)
	}
	if _, err := desk.DeleteArtifactSpace(ctx, desktop.DeleteArtifactSpaceReq{ID: id}); err != nil {
		t.Fatalf("DeleteArtifactSpace: %v", err)
	}
	_, err = desk.GetArtifactSpace(ctx, desktop.GetArtifactSpaceReq{ID: id})
	var statusErr *desktop.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 404 {
		t.Fatalf("deleted artifact space: %v", err)
	}
	ledger.note("idl.SaveArtifactSpace", "real", "id returned name sdk space")
	ledger.note("idl.GetArtifactSpace", "real", "name reread then 404 after delete")
	ledger.note("idl.DeleteArtifactSpace", "real", "GetArtifactSpace 404 for the same id")
}

func assertPinSession(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, sessionID string) {
	t.Helper()
	pinned, err := desk.PinSession(ctx, desktop.PinSessionReq{SessionID: sessionID, Pinned: true})
	if err != nil {
		noteServer(t, ledger, "idl.PinSession", err)
		return
	}
	if !pinContains(pinned.Items, pinned.Item, sessionID) {
		t.Fatalf("pin response did not contain %s", sessionID)
	}
	order, err := desk.GetPinnedItemsOrder(ctx, desktop.GetPinnedItemsOrderReq{})
	if err != nil {
		t.Fatalf("GetPinnedItemsOrder: %v", err)
	}
	if !pinContains(order.Items, nil, sessionID) {
		t.Fatal("pinned order omitted the session")
	}
	unpinned, err := desk.PinSession(ctx, desktop.PinSessionReq{SessionID: sessionID, Pinned: false})
	if err != nil {
		t.Fatalf("unpin: %v", err)
	}
	if pinContains(unpinned.Items, unpinned.Item, sessionID) {
		t.Fatal("session remained pinned")
	}
	ledger.note("idl.PinSession", "real", "session id pinned then absent")
	ledger.note("idl.GetPinnedItemsOrder", "real", "order contained the session id")
}

func assertDeletedSession(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent string) {
	t.Helper()
	title := "delete me"
	created, err := desk.CreateSession(ctx, desktop.CreateSessionReq{Name: agent, Title: &title})
	if err != nil || created.SessionID == nil {
		t.Fatalf("second session: %v", err)
	}
	id, ok := created.SessionID.String()
	if !ok || id == "" {
		t.Fatal("second session id")
	}
	deleted, err := desk.DeleteSession(ctx, desktop.DeleteSessionReq{ID: id})
	if err != nil {
		noteServer(t, ledger, "idl.DeleteSession", err)
		return
	}
	if deleted.Success != nil {
		if ok, yes := deleted.Success.Bool(); yes && !ok {
			t.Fatal("DeleteSession success=false")
		}
	}
	_, err = desk.GetSession(ctx, desktop.GetSessionReq{ID: id})
	var statusErr *desktop.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 404 {
		t.Fatalf("GetSession after delete: %v", err)
	}
	ledger.note("idl.DeleteSession", "real", "GetSession 404 for the deleted id")
}

func assertDisabledCron(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent string) {
	t.Helper()
	enabled := false
	kind := desktop.CronScheduleOnce
	mode := desktop.CronSessionTargetNew
	when := desktop.Number(1893456000000)
	created, err := desk.CreateCronDefinition(ctx, desktop.CreateCronDefinitionReq{
		Name:          "hello note",
		AgentName:     agent,
		Prompt:        "hello",
		Enabled:       &enabled,
		Schedule:      &desktop.CronSchedule{Kind: &kind, RunAtMs: &when},
		SessionTarget: &desktop.CronSessionTarget{Mode: &mode},
	})
	if err != nil || created.Definition == nil {
		var statusErr *desktop.StatusError
		if errors.As(err, &statusErr) && strings.Contains(string(statusErr.Body), "CONTENT_POLICY_VIOLATION") {
			const why = "official aje reviews cron name as CronTitle and prompt as ConfigField. US blocks when pass is false and errorKind is not api_error. Uninitialized reviewContent returns local_error, so the HTTP 422 is fail-closed content safety, not a rejected JSON shape. Wire includes name and prompt. enabled was false and the definition was not triggered."
			ledger.note("idl.CreateCronDefinition", "blocked", why)
			blockRoot(ledger, "idl.CreateCronDefinition", []string{"idl.UpdateCronDefinition", "idl.GetCronDefinition", "idl.DeleteCronDefinition", "idl.ListCronRuns"}, why)
			return
		}
		noteServer(t, ledger, "idl.CreateCronDefinition", err)
		return
	}
	id, ok := created.Definition.CronID.String()
	name, nameOK := created.Definition.Name.String()
	flag, flagOK := created.Definition.Enabled.Bool()
	if !ok || id == "" || !nameOK || name != "hello note" || !flagOK || flag {
		t.Fatalf("cron id %q name %q enabled %v", id, name, flag)
	}
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = desk.DeleteCronDefinition(cleanCtx, desktop.DeleteCronDefinitionReq{CronID: id})
	})
	renamed := "hello there"
	updated, err := desk.UpdateCronDefinition(ctx, desktop.UpdateCronDefinitionReq{CronID: id, Name: &renamed, Enabled: &enabled})
	if err != nil || updated.Definition == nil {
		var denied *desktop.StatusError
		if errors.As(err, &denied) && strings.Contains(string(denied.Body), "CONTENT_POLICY_VIOLATION") {
			ledger.note("idl.CreateCronDefinition", "real", "disabled once cron id returned")
			ledger.note("idl.UpdateCronDefinition", "negative", "HTTP 422 CONTENT_POLICY_VIOLATION for the replacement name")
			_, _ = desk.DeleteCronDefinition(ctx, desktop.DeleteCronDefinitionReq{CronID: id})
			return
		}
		t.Fatalf("UpdateCronDefinition: %v", err)
	}
	if got, ok := updated.Definition.Name.String(); !ok || got != renamed {
		t.Fatalf("cron name %q", got)
	}
	fetched, err := desk.GetCronDefinition(ctx, desktop.GetCronDefinitionReq{CronID: id})
	if err != nil || fetched.Definition == nil {
		t.Fatalf("GetCronDefinition: %v", err)
	}
	if got, ok := fetched.Definition.Name.String(); !ok || got != renamed {
		t.Fatalf("stored cron name %q", got)
	}
	if _, err := desk.DeleteCronDefinition(ctx, desktop.DeleteCronDefinitionReq{CronID: id}); err != nil {
		t.Fatalf("DeleteCronDefinition: %v", err)
	}
	_, err = desk.GetCronDefinition(ctx, desktop.GetCronDefinitionReq{CronID: id})
	var statusErr *desktop.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 404 {
		t.Fatalf("deleted cron: %v", err)
	}
	ledger.note("idl.CreateCronDefinition", "real", "disabled once cron id returned")
	ledger.note("idl.UpdateCronDefinition", "real", "name hello there")
	ledger.note("idl.GetCronDefinition", "real", "renamed value reread then 404")
	ledger.note("idl.DeleteCronDefinition", "real", "GetCronDefinition 404 for the same id")
}

func assertPrewarm(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	warmed, err := desk.PrewarmWorkspaceIndexing(ctx, desktop.PrewarmWorkspaceIndexingReq{WorkspaceDir: workspace})
	if err != nil {
		noteServer(t, ledger, "idl.PrewarmWorkspaceIndexing", err)
		return
	}
	detail, ok := populatedDetail(reflect.ValueOf(warmed))
	if !ok {
		ledger.note("idl.PrewarmWorkspaceIndexing", "notrun", "response had no populated field")
		return
	}
	ledger.note("idl.PrewarmWorkspaceIndexing", "real", detail)
}

func assertAccountRead(t *testing.T, ctx context.Context, client *Client, ledger *coverageLedger) {
	t.Helper()
	_, err := client.AccountRead(ctx)
	var rpcErr *Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32601 {
		t.Fatalf("account/read: %v", err)
	}
	const detail = "0.7.8 app-server does not implement account/read and returns -32601 method not found"
	ledger.note("request.account.read", "negative", detail)
	ledger.note("error.-32601", "negative", "provoked by account/read; this records the incompatibility, not a successful account read")
}

func assertEventStream(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	openCtx, openCancel := context.WithCancel(ctx)
	defer openCancel()
	unread, err := desk.SubscribeGlobalEvents(openCtx, desktop.SubscribeGlobalEventsReq{})
	if err != nil {
		noteServer(t, ledger, "idl.SubscribeGlobalEvents", err)
		return
	}
	echo, err := desk.GetEcho(ctx, desktop.GetEchoReq{Message: "during-stream"})
	if err != nil {
		t.Fatalf("GetEcho during stream: %v", err)
	}
	if text, ok := echo.Message.String(); !ok || !strings.Contains(text, "during-stream") {
		t.Fatalf("echo during stream %q", text)
	}
	ledger.note("transport.unary-during-open-stream", "real", "GetEcho returned while SubscribeGlobalEvents was open and unread")
	openCancel()
	if err := unread.Close(); err != nil {
		t.Fatal(err)
	}
	streamCtx, streamCancel := context.WithTimeout(ctx, 3*time.Second)
	defer streamCancel()
	events, err := desk.SubscribeGlobalEvents(streamCtx, desktop.SubscribeGlobalEventsReq{})
	if err != nil {
		t.Fatalf("SubscribeGlobalEvents: %v", err)
	}
	frame, err := events.Recv(streamCtx)
	if err != nil {
		t.Fatalf("event recv: %v", err)
	}
	got := desktop.Interpret(frame)
	if got.Kind == "" && !got.HasType {
		t.Fatal("event frame had no kind")
	}
	streamCancel()
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	ledger.note("idl.SubscribeGlobalEvents", "real", "frame kind "+got.Kind+" then caller cancel")
}

func assertPrepareRestart(t *testing.T, ledger *coverageLedger) {
	t.Helper()
	owned, _ := startLive(t, "desktop")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, err := owned.PrepareRestart(ctx)
	if err != nil {
		ledger.note("request.server.prepareRestart", "failed", err.Error())
		t.Errorf("PrepareRestart: %v", err)
		return
	}
	if !jsonObject(raw) {
		ledger.note("request.server.prepareRestart", "notrun", "response was not a JSON object")
		return
	}
	ledger.note("request.server.prepareRestart", "real", "owned temporary server returned a JSON object")
}

func assertUnixHealth(t *testing.T, ledger *coverageLedger) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	client := startUnix(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ready, err := client.Readyz(ctx)
	if err != nil || !ready.OK || ready.ProtocolVersion != 1 {
		t.Fatalf("readyz: %v %+v", err, ready)
	}
	health, err := client.Healthz(ctx)
	if err != nil || !health.OK || health.Version != desktop.BaselineVersion || health.ProtocolVersion != 1 {
		t.Fatalf("healthz: %v %+v", err, health)
	}
	ledger.note("unix.readyz", "real", "ok protocol 1")
	ledger.note("unix.healthz", "real", "ok version "+health.Version+" protocol 1 auth "+health.AuthMode)
}

func startUnix(t *testing.T) *Client {
	t.Helper()
	bin := os.Getenv("MCODE_BIN")
	if bin == "" {
		bin = "mcode"
	}
	data := t.TempDir()
	workspace := t.TempDir()
	sockDir := t.TempDir()
	for _, dir := range []string{data, workspace, sockDir} {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	opt := Options{
		Command:    bin,
		DataDir:    data,
		Workspace:  workspace,
		AuthMode:   "desktop",
		Listen:     ListenUnix,
		SocketPath: filepath.Join(sockDir, "s"),
		Stderr:     &testLog{t},
	}
	if leading := os.Getenv("MCODE_LEADING"); leading != "" {
		opt.LeadingArgs = []string{leading}
	}
	client, err := Start(ctx, opt)
	if err != nil {
		t.Fatalf("unix start: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func noteServer(t *testing.T, ledger *coverageLedger, id string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: nil error", id)
	}
	outcome, detail := classifyServer(err)
	ledger.note(id, outcome, detail)
	if outcome == "failed" {
		t.Errorf("%s: %s", id, detail)
	}
}

func noteUpstreamGaps(ledger *coverageLedger) {
	gaps := map[string]string{
		"idl.GetWechatBindStatus":          "channel-bridge wechat bind is not mounted; success needs that backend",
		"idl.ListChannelConnections":       "channel connection route is not mounted; success needs the channel backend",
		"idl.ListChannelBindings":          "channel-bridge status route is not mounted; success needs the channel backend",
		"idl.GetLarkOnboardStatus":         "lark onboard route is not mounted; success needs the lark backend",
		"idl.GetSidebarSessionTree":        "archon sidebar route is not mounted; success needs that backend",
		"idl.ListIdeApps":                  "IDE app list is not implemented in local-runtime-v2; success needs that service",
		"idl.OpenWorkspaceApp":             "open-app is not implemented in local-runtime-v2; success needs that service",
		"idl.ListCrons":                    "legacy cron list is unsupported on this host; success needs the legacy cron service. ListCronDefinitions is the replacement",
		"idl.ListModelAccess":              "model-access is unsupported on the legacy host; success needs the model-access service",
		"idl.ListModelAccessApiCandidates": "model-access candidates are unsupported on the legacy host; success needs the model-access service",
		"idl.ListShareInstallations":       "share installations are not implemented by local-runtime; success needs the share backend",
		"idl.GetWorkspaceFileMetadata":     "file metadata is not implemented in local-runtime-v2; success needs that file service",
		"idl.GetLlmContextCapture":         "llm-context capture returns HTTP 500 on a complete request; success needs a working llm-context backend",
		"idl.GetLlmContextSessions":        "llm-context sessions returns HTTP 500 on a complete request; success needs a working llm-context backend",
		"idl.GetSessionLlmContextOverview": "session llm-context overview returns HTTP 500; success needs a working llm-context backend",
		"idl.UpdateProject.archive":        "archive is rejected with HTTP 400 UNSUPPORTED_PROJECT_UPDATE; supported updates are name, icon, and folder",
	}
	for id, detail := range gaps {
		row, ok := ledger.outcome(id)
		if ok && row.Outcome == "negative" {
			ledger.note(id, "negative", detail)
		}
	}
}

func mcpListed(list desktop.ListLocalMCPServersResp, name string) bool {
	for _, item := range list.Servers {
		got, ok := item.Name.String()
		if ok && got == name {
			return true
		}
	}
	return false
}

func pinContains(items []desktop.PinnedItem, item *desktop.PinnedItem, sessionID string) bool {
	if item != nil && pinID(*item, sessionID) {
		return true
	}
	for _, candidate := range items {
		if pinID(candidate, sessionID) {
			return true
		}
	}
	return false
}

func pinID(item desktop.PinnedItem, sessionID string) bool {
	if id, ok := item.ID.String(); ok && id == sessionID {
		return true
	}
	if item.Session != nil && item.Session.SessionID != nil {
		id, ok := item.Session.SessionID.String()
		return ok && id == sessionID
	}
	return false
}

func crudPtr(value string) *string {
	return &value
}
