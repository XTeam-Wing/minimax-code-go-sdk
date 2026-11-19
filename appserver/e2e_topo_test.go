package appserver

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

func assertTopo(t *testing.T, ctx context.Context, client *Client, desk *desktop.Client, ledger *coverageLedger, workspace, agent, sessionID string) {
	t.Helper()
	assertEchoFixtures(t, ctx, desk, ledger)
	assertLocalAgent(t, ctx, desk, ledger, workspace)
	assertWorkingState(t, ctx, desk, ledger)
	assertSidebarQueries(t, ctx, desk, ledger, sessionID)
	assertPluginAndRuntimeSync(t, ctx, desk, ledger)
	assertImportArtifact(t, ctx, desk, ledger)
	assertCanvasRead(t, ctx, desk, ledger, sessionID)
	assertGitCheckout(t, ctx, desk, ledger, workspace)
	assertAdvisorToggle(t, ctx, desk, ledger)
	assertPinnedOrder(t, ctx, desk, ledger, sessionID)
	assertRPCErrors(t, ledger)
}

func assertEchoFixtures(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	ping, err := desk.Ping(ctx, desktop.PingReq{Message: "topo"})
	if err != nil {
		recordCall(t, ledger, "idl.Ping", err)
	} else if text, ok := ping.Message.String(); !ok || !strings.Contains(text, "topo") {
		t.Fatalf("ping %q", text)
	} else {
		ledger.note("idl.Ping", "real", "message contains topo")
	}
	header, err := desk.GetHeaderEcho(ctx, desktop.GetHeaderEchoReq{Message: "topo-header", ClientName: "go-sdk"})
	if err != nil {
		recordCall(t, ledger, "idl.GetHeaderEcho", err)
	} else if text, ok := header.Message.String(); !ok || !strings.Contains(text, "topo-header") {
		t.Fatalf("header echo %q", text)
	} else {
		ledger.note("idl.GetHeaderEcho", "real", "message contains topo-header")
	}
	path, err := desk.GetPathEcho(ctx, desktop.GetPathEchoReq{ResourceID: "topo-path"})
	if err != nil {
		recordCall(t, ledger, "idl.GetPathEcho", err)
	} else if text, ok := path.Message.String(); !ok || !strings.Contains(text, "topo-path") {
		t.Fatalf("path echo %q", text)
	} else {
		ledger.note("idl.GetPathEcho", "real", "message contains topo-path")
	}
	limit := desktop.Number(1)
	nested, err := desk.SubmitNestedFieldEcho(ctx, desktop.SubmitNestedFieldEchoReq{
		Title:      "sdk-nested",
		ClientName: "go-sdk",
		Nested:     &desktop.BindingNestedQuery{NestedPage: &desktop.BindingPageQuery{Limit: &limit}},
	})
	if err != nil {
		recordCall(t, ledger, "idl.SubmitNestedFieldEcho", err)
		return
	}
	if text, ok := nested.Message.String(); !ok || !strings.Contains(text, "sdk-nested") {
		t.Fatalf("nested echo %q", text)
	}
	if nested.Nested == nil || nested.Nested.NestedPage == nil || nested.Nested.NestedPage.Limit == nil || float64(*nested.Nested.NestedPage.Limit) != 1 {
		t.Fatal("nested limit was not echoed")
	}
	ledger.note("idl.SubmitNestedFieldEcho", "real", "message contains sdk-nested and nested.limit is 1")
}

func assertLocalAgent(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	name := "sdklocal"
	only := true
	created, err := desk.CreateAgent(ctx, desktop.CreateAgentReq{
		Name:                &name,
		DisplayName:         &name,
		DefinitionOnly:      &only,
		DefaultWorkspaceDir: &workspace,
	})
	if err != nil || created.Name == nil {
		if contentSafety(err) {
			const why = "CreateAgent display or name was reviewed and the uninitialized content-safety owner fail-closed with HTTP 422. Wire sent name, display_name, definition_only, and default_workspace_dir."
			ledger.note("idl.CreateAgent", "blocked", why)
			blockRoot(ledger, "idl.CreateAgent", []string{"idl.UpdateAgent", "idl.DeleteAgent", "idl.GetAgentConfig", "idl.PutAgentConfig"}, why)
			return
		}
		recordCall(t, ledger, "idl.CreateAgent", err)
		return
	}
	got, ok := created.Name.String()
	if !ok || got != name {
		t.Fatalf("agent name %q", got)
	}
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = desk.DeleteAgent(cleanCtx, desktop.DeleteAgentReq{Name: name})
	})
	fetched, err := desk.GetAgent(ctx, desktop.GetAgentReq{Name: name})
	if err != nil || fetched.Agent == nil || fetched.Agent.Name == nil {
		t.Fatalf("GetAgent created: %v", err)
	}
	if again, ok := fetched.Agent.Name.String(); !ok || again != name {
		t.Fatalf("stored agent %q", again)
	}
	display := "sdklocaltwo"
	updated, err := desk.UpdateAgent(ctx, desktop.UpdateAgentReq{Name: name, DisplayName: &display})
	if err != nil {
		recordCall(t, ledger, "idl.UpdateAgent", err)
	} else if updated.Agent == nil || updated.Agent.DisplayName == nil {
		ledger.note("idl.UpdateAgent", "notrun", "update returned no display name")
	} else if got, ok := updated.Agent.DisplayName.String(); !ok || got != display {
		t.Fatalf("display %q", got)
	} else {
		ledger.note("idl.UpdateAgent", "real", "display_name sdklocaltwo")
	}
	cfg, err := desk.GetAgentConfig(ctx, desktop.GetDesktopAgentConfigReq{Name: name})
	if err != nil || cfg.Config == nil {
		recordCall(t, ledger, "idl.GetAgentConfig", err)
	} else {
		content, _ := cfg.Config.Content.String()
		revision, revOK := cfg.Config.Revision.String()
		if !revOK || revision == "" {
			ledger.note("idl.GetAgentConfig", "real", "config document without a revision string")
		} else {
			marker := content + "\n# sdk-local\n"
			put, err := desk.PutAgentConfig(ctx, desktop.PutDesktopAgentConfigReq{Name: name, Content: marker, ExpectedRevision: revision})
			if err != nil {
				recordCall(t, ledger, "idl.PutAgentConfig", err)
			} else if put.Config == nil {
				ledger.note("idl.PutAgentConfig", "notrun", "put returned no config")
			} else if stored, ok := put.Config.Content.String(); !ok || !strings.Contains(stored, "sdk-local") {
				t.Fatalf("config content %q", stored)
			} else {
				ledger.note("idl.PutAgentConfig", "real", "content contains sdk-local")
			}
			ledger.note("idl.GetAgentConfig", "real", "revision read before put")
		}
	}
	if _, err := desk.DeleteAgent(ctx, desktop.DeleteAgentReq{Name: name}); err != nil {
		recordCall(t, ledger, "idl.DeleteAgent", err)
		return
	}
	_, err = desk.GetAgent(ctx, desktop.GetAgentReq{Name: name})
	if err == nil {
		t.Fatal("deleted agent still readable")
	}
	ledger.note("idl.CreateAgent", "real", "definition_only name sdklocal")
	ledger.note("idl.DeleteAgent", "real", "GetAgent failed for sdklocal after delete")
}

func assertWorkingState(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	ensured, err := desk.EnsureBunny(ctx, desktop.EnsureBunnyReq{})
	if err != nil {
		outcome, detail := classifyServer(err)
		if outcome == "blocked" || outcome == "negative" {
			ledger.note("idl.EnsureBunny", outcome, detail)
			blockRoot(ledger, "idl.EnsureBunny", []string{
				"idl.AddWorkingStateEntry", "idl.UpdateWorkingStateEntry", "idl.CloseWorkingStateEntry",
				"idl.ConfirmWorkingStateEntry", "idl.ReportBunnyViewState",
			}, detail)
			return
		}
		recordCall(t, ledger, "idl.EnsureBunny", err)
		return
	}
	if detail, ok := populatedDetail(reflect.ValueOf(ensured)); ok {
		ledger.note("idl.EnsureBunny", "real", detail)
	} else {
		ledger.note("idl.EnsureBunny", "notrun", "ensure returned no populated field")
		return
	}
	added, err := desk.AddWorkingStateEntry(ctx, desktop.AddWorkingStateEntryReq{Kind: "constraint", Text: "hello note"})
	if err != nil || added.Entry == nil {
		recordCall(t, ledger, "idl.AddWorkingStateEntry", err)
		return
	}
	id, ok := added.Entry.ID.String()
	text, textOK := added.Entry.Text.String()
	if !ok || id == "" || !textOK || text != "hello note" {
		t.Fatalf("working state id %q text %q", id, text)
	}
	ledger.note("idl.AddWorkingStateEntry", "real", "kind constraint text hello note")
	updated, err := desk.UpdateWorkingStateEntry(ctx, desktop.UpdateWorkingStateEntryReq{ID: id, Text: "hello there"})
	if err != nil || updated.Entry == nil {
		recordCall(t, ledger, "idl.UpdateWorkingStateEntry", err)
		return
	}
	if got, ok := updated.Entry.Text.String(); !ok || got != "hello there" {
		t.Fatalf("updated text %q", got)
	}
	ledger.note("idl.UpdateWorkingStateEntry", "real", "text hello there")
	closeID := id
	if next, ok := updated.Entry.ID.String(); ok && next != "" {
		closeID = next
	}
	closed, err := desk.CloseWorkingStateEntry(ctx, desktop.CloseWorkingStateEntryReq{ID: closeID, Reason: "done"})
	if err != nil || closed.Entry == nil || closed.Entry.CloseReason == nil {
		recordCall(t, ledger, "idl.CloseWorkingStateEntry", err)
		return
	}
	if reason, ok := closed.Entry.CloseReason.String(); !ok || reason != "done" {
		t.Fatalf("close reason %q", reason)
	}
	ledger.note("idl.CloseWorkingStateEntry", "real", "close_reason done")
}

func assertSidebarQueries(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, sessionID string) {
	t.Helper()
	projects, err := desk.QuerySidebarProjects(ctx, desktop.QuerySidebarProjectsReq{})
	notePopulated(t, ledger, "idl.QuerySidebarProjects", err, projects)
	allProjects := true
	sessions, err := desk.QuerySidebarProjectSessions(ctx, desktop.QuerySidebarProjectSessionsReq{AllProjects: &allProjects})
	notePopulated(t, ledger, "idl.QuerySidebarProjectSessions", err, sessions)
	agents, err := desk.QuerySidebarAgents(ctx, desktop.QuerySidebarAgentsReq{})
	notePopulated(t, ledger, "idl.QuerySidebarAgents", err, agents)
	one, err := desk.GetSidebarSession(ctx, desktop.GetSidebarSessionReq{SessionID: &sessionID})
	notePopulated(t, ledger, "idl.GetSidebarSession", err, one)
}

func assertPluginAndRuntimeSync(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	refreshed, err := desk.RefreshPlugins(ctx, desktop.RefreshPluginsReq{})
	notePopulated(t, ledger, "idl.RefreshPlugins", err, refreshed)
	synced, err := desk.SyncLocalMcpRuntimeServers(ctx, desktop.SyncLocalMCPRuntimeServersReq{})
	notePopulated(t, ledger, "idl.SyncLocalMcpRuntimeServers", err, synced)
}

func assertImportArtifact(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	space, err := desk.SaveArtifactSpace(ctx, desktop.SaveArtifactSpaceReq{Name: "import space"})
	if err != nil || space.Space == nil {
		recordCall(t, ledger, "idl.ImportArtifact", err)
		return
	}
	spaceID, _ := space.Space.ID.String()
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = desk.DeleteArtifactSpace(cleanCtx, desktop.DeleteArtifactSpaceReq{ID: spaceID})
	})
	imported, err := desk.ImportArtifact(ctx, desktop.ImportArtifactReq{
		Name:          "note.txt",
		ContentBase64: base64.StdEncoding.EncodeToString([]byte("hello")),
		SpaceID:       &spaceID,
	})
	if err != nil || imported.Artifact == nil {
		recordCall(t, ledger, "idl.ImportArtifact", err)
		return
	}
	name, ok := imported.Artifact.Name.String()
	if !ok || name != "note.txt" {
		t.Fatalf("artifact name %q", name)
	}
	ledger.note("idl.ImportArtifact", "real", "name note.txt")
}

func assertCanvasRead(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, sessionID string) {
	t.Helper()
	canvas, err := desk.ReadCanvas(ctx, desktop.ReadCanvasReq{SessionID: sessionID})
	notePopulated(t, ledger, "idl.ReadCanvas", err, canvas)
}

func assertGitCheckout(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	before, err := desk.GetWorkspaceGitBranches(ctx, desktop.GitBranchesReq{Workspace: workspace})
	if err != nil || before.Current == nil {
		recordCall(t, ledger, "idl.CheckoutWorkspaceGitBranch", err)
		return
	}
	current, ok := before.Current.String()
	if !ok || current == "" {
		ledger.note("idl.CheckoutWorkspaceGitBranch", "notrun", "current branch was empty")
		return
	}
	target := current
	for _, name := range []string{"main", "master", "sdk-local-branch"} {
		if name == current {
			continue
		}
		if scalarListHas(before.Local, name) || scalarListHas(before.Branches, name) {
			target = name
			break
		}
	}
	if target == current {
		ledger.note("idl.CheckoutWorkspaceGitBranch", "notrun", "the temp repository has no second branch to check out")
		return
	}
	checked, err := desk.CheckoutWorkspaceGitBranch(ctx, desktop.GitCheckoutReq{Workspace: workspace, Branch: target})
	if err != nil || !scalarTrue(checked.Success) {
		recordCall(t, ledger, "idl.CheckoutWorkspaceGitBranch", err)
		return
	}
	mid, err := desk.GetWorkspaceGitBranches(ctx, desktop.GitBranchesReq{Workspace: workspace})
	if err != nil || mid.Current == nil {
		t.Fatalf("branch after checkout: %v", err)
	}
	if got, ok := mid.Current.String(); !ok || got != target {
		t.Fatalf("current branch %q", got)
	}
	back, err := desk.CheckoutWorkspaceGitBranch(ctx, desktop.GitCheckoutReq{Workspace: workspace, Branch: current})
	if err != nil || !scalarTrue(back.Success) {
		t.Fatalf("checkout restore: %v", err)
	}
	ledger.note("idl.CheckoutWorkspaceGitBranch", "real", "current branch changed from "+current+" to "+target+" and was restored")
}

func assertAdvisorToggle(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	listed, err := desk.ListModels(ctx, desktop.ListModelsReq{})
	if err != nil {
		recordCall(t, ledger, "idl.SelectAdvisorModel", err)
		return
	}
	original := false
	if listed.Advisor != nil {
		original = listed.Advisor.Enabled
	}
	selected, err := desk.SelectAdvisorModel(ctx, desktop.SelectAdvisorModelReq{Enabled: !original})
	if err != nil || selected.Advisor == nil || selected.Advisor.Enabled == original {
		recordCall(t, ledger, "idl.SelectAdvisorModel", err)
		return
	}
	again, err := desk.ListModels(ctx, desktop.ListModelsReq{})
	if err != nil || again.Advisor == nil || again.Advisor.Enabled == original {
		t.Fatalf("advisor enabled did not change: %v", err)
	}
	if _, err := desk.SelectAdvisorModel(ctx, desktop.SelectAdvisorModelReq{Enabled: original}); err != nil {
		t.Fatalf("restore advisor: %v", err)
	}
	ledger.note("idl.SelectAdvisorModel", "real", "advisor.enabled flipped and restored")
	ledger.note("idl.ListModels", "real", "advisor.enabled observed")
}

func assertPinnedOrder(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, sessionID string) {
	t.Helper()
	pinned, err := desk.PinSession(ctx, desktop.PinSessionReq{SessionID: sessionID, Pinned: true})
	if err != nil || len(pinned.Items) == 0 {
		recordCall(t, ledger, "idl.PutPinnedItemsOrder", err)
		return
	}
	var items []desktop.PinnedItemRef
	for _, item := range pinned.Items {
		items = append(items, desktop.PinnedItemRef{ItemType: item.ItemType, ID: item.ID, Type: item.Type})
	}
	ordered, err := desk.PutPinnedItemsOrder(ctx, desktop.PutPinnedItemsOrderReq{Items: items})
	if err != nil {
		recordCall(t, ledger, "idl.PutPinnedItemsOrder", err)
		return
	}
	if !pinContains(ordered.Items, nil, sessionID) {
		t.Fatal("pinned order omitted the session")
	}
	if _, err := desk.PinSession(ctx, desktop.PinSessionReq{SessionID: sessionID, Pinned: false}); err != nil {
		t.Fatalf("unpin after order: %v", err)
	}
	ledger.note("idl.PutPinnedItemsOrder", "real", "order contains the session id")
}

func assertRPCErrors(t *testing.T, ledger *coverageLedger) {
	t.Helper()
	owned, _ := startLive(t, "desktop")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := owned.call(ctx, "initialize", map[string]any{})
	var rpcErr *Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32003 {
		ledger.note("error.-32003", "failed", errorText(err))
		t.Errorf("second initialize: %v", err)
	} else {
		ledger.note("error.-32003", "negative", "second initialize on an owned connection returned -32003 alreadyInitialized")
	}
	_, err = owned.call(ctx, "account/login/cancel", map[string]any{})
	rpcErr = nil
	if errors.As(err, &rpcErr) && rpcErr.Code == -32601 {
		ledger.note("request.account.login.cancel", "negative", "0.7.8 app-server returns -32601 for account/login/cancel. The method is in the outer protocol and is not implemented. The empty loginId did not reach parameter validation.")
		ledger.note("error.-32602", "notrun", "account/login/cancel is unknown on this server, so it cannot provoke -32602. A missing-parameter probe needs a method this server implements.")
		return
	}
	if !errors.As(err, &rpcErr) || rpcErr.Code != -32602 {
		ledger.note("error.-32602", "failed", errorText(err))
		t.Errorf("login cancel without id: %v", err)
		return
	}
	ledger.note("error.-32602", "negative", "account/login/cancel without loginId returned -32602 invalid params")
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func notePopulated(t *testing.T, ledger *coverageLedger, id string, err error, value any) {
	t.Helper()
	if err != nil {
		recordCall(t, ledger, id, err)
		return
	}
	detail, ok := populatedDetail(reflect.ValueOf(value))
	if !ok {
		ledger.note(id, "notrun", "response had no populated typed field")
		return
	}
	ledger.note(id, "real", detail)
}
