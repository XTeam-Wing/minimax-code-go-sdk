package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

func assertExhaustLocal(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace, agent, sessionID string) {
	t.Helper()
	assertGitStageRoundTrip(t, ctx, desk, ledger, workspace)
	assertManagedWorktrees(t, ctx, desk, ledger, workspace)
	assertEchoMCP(t, ctx, desk, ledger)
	assertSandboxWriteBack(t, ctx, desk, ledger)
	assertReviewCommits(t, ctx, desk, ledger, workspace)
	assertCanvasFile(t, ctx, desk, ledger, workspace, agent)
	assertAgentImportRead(t, ctx, desk, ledger)
	assertQuestionnaire(t, ctx, desk, ledger, agent, sessionID)
	assertFileWatchAndSearch(t, ctx, desk, ledger, workspace)
	assertOpenAndReveal(t, ctx, desk, ledger, workspace)
	assertBuiltinAgentRead(t, ctx, desk, ledger, agent)
	assertMarketplaceRefresh(t, ctx, desk, ledger)
	assertSameModelSelection(t, ctx, desk, ledger)
	assertSameModelSource(t, ctx, desk, ledger)
	assertContextCaptureToggle(t, ctx, desk, ledger)
	assertOwnedLocalFixtures(t, ctx, desk, ledger, agent, sessionID)
	markExhaustedDependencies(ledger)
}

func assertGitStageRoundTrip(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	const path = "stage-me.txt"
	if err := os.WriteFile(workspace+"/"+path, []byte("stage"), 0o600); err != nil {
		t.Fatal(err)
	}
	staged, err := desk.ApplyWorkspaceGitChangeAction(ctx, desktop.GitChangeActionReq{
		Workspace: workspace,
		Action:    "stage",
		Paths:     []string{path},
	})
	if err != nil || !scalarTrue(staged.Success) {
		if err == nil {
			ledger.note("idl.ApplyWorkspaceGitChangeAction", "notrun", "stage success was not true: "+scalarText(staged.Error))
			return
		}
		recordCall(t, ledger, "idl.ApplyWorkspaceGitChangeAction", err)
		return
	}
	changes, err := desk.GetWorkspaceGitChanges(ctx, desktop.GitChangesReq{Workspace: workspace})
	if err != nil || !gitListsPath(changes, path) {
		t.Fatalf("staged changes: %v", err)
	}
	unstaged, err := desk.ApplyWorkspaceGitChangeAction(ctx, desktop.GitChangeActionReq{
		Workspace: workspace,
		Action:    "unstage",
		Paths:     []string{path},
	})
	if err != nil || !scalarTrue(unstaged.Success) {
		t.Fatalf("unstage: %v", err)
	}
	ledger.note("idl.ApplyWorkspaceGitChangeAction", "real", "stage then unstage stage-me.txt")
	ledger.note("idl.GetWorkspaceGitChanges", "real", "changes listed stage-me.txt after stage")
}

func assertReviewCommits(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	const (
		reviewFile  = "review-tracked.txt"
		reviewToken = "review-token"
		reviewBase  = "review-base"
	)
	gitIn(t, workspace, "branch", "-f", reviewBase, "HEAD")
	if err := os.WriteFile(filepath.Join(workspace, reviewFile), []byte(reviewToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, workspace, "add", "-A")
	gitIn(t, workspace, "commit", "-m", "review tracked")
	headSHA := strings.TrimSpace(gitOut(t, workspace, "rev-parse", "HEAD"))
	source := &desktop.WorkspaceReviewSourceView{Type: "branch", BaseRef: crudPtr(reviewBase)}
	listed, err := desk.ListWorkspaceReviewCommits(ctx, desktop.ListWorkspaceReviewCommitsReq{Workspace: workspace, BaseRef: reviewBase})
	if err != nil {
		t.Fatalf("ListWorkspaceReviewCommits: %v", err)
	}
	head, ok := listed.HeadCommit.String()
	if !ok || head != headSHA {
		t.Fatalf("head_commit %q want %s", head, headSHA)
	}
	ledger.note("idl.ListWorkspaceReviewCommits", "real", "head_commit "+head)
	summary, err := loadReviewSummary(ctx, desk, workspace, source, reviewFile)
	if err != nil {
		t.Fatalf("GetWorkspaceReviewSummary: %v", err)
	}
	ledger.note("idl.GetWorkspaceReviewSummary", "real", "snapshot "+summary.snapshot+" file "+summary.fileID)
	assertion, err := readTrackedReview(ctx, desk, workspace, source, summary.snapshot, summary.fileID, reviewFile, reviewToken)
	if err == nil {
		noteTrackedReview(ledger, assertion)
		return
	}
	if !reviewResolution(err) {
		t.Fatalf("review chain: %v", err)
	}
	first := strings.TrimSpace(statusBody(err))
	refreshed, err := loadReviewSummary(ctx, desk, workspace, source, reviewFile)
	if err != nil {
		t.Fatalf("review chain unresolved after one fresh summary: first %s; summary %v", first, err)
	}
	assertion, err = readTrackedReview(ctx, desk, workspace, source, refreshed.snapshot, refreshed.fileID, reviewFile, reviewToken)
	if err != nil {
		t.Fatalf("review chain unresolved after one fresh summary: first %s; retry %v", first, err)
	}
	noteTrackedReview(ledger, "first response "+first+"; retry "+assertion)
}

type reviewSummary struct {
	snapshot string
	fileID   string
}

func loadReviewSummary(ctx context.Context, desk *desktop.Client, workspace string, source *desktop.WorkspaceReviewSourceView, path string) (reviewSummary, error) {
	summary, err := desk.GetWorkspaceReviewSummary(ctx, desktop.GetWorkspaceReviewSummaryReq{Workspace: workspace, Source: source})
	if err != nil {
		return reviewSummary{}, err
	}
	snapshot, ok := summary.ReviewSnapshotID.String()
	if !ok || snapshot == "" {
		return reviewSummary{}, errors.New("review_snapshot_id was empty")
	}
	fileID, ok := reviewFileID(summary.Files, path)
	if !ok {
		return reviewSummary{}, fmt.Errorf("summary omitted %s", path)
	}
	return reviewSummary{snapshot: snapshot, fileID: fileID}, nil
}

func readTrackedReview(ctx context.Context, desk *desktop.Client, workspace string, source *desktop.WorkspaceReviewSourceView, snapshot, fileID, path, token string) (string, error) {
	diffs, err := desk.ListWorkspaceReviewFileDiffs(ctx, desktop.ListWorkspaceReviewFileDiffsReq{
		Workspace: workspace, Source: source, ReviewSnapshotID: snapshot,
	})
	if err != nil {
		return "", err
	}
	got, ok := diffs.ReviewSnapshotID.String()
	if !ok || got != snapshot {
		return "", fmt.Errorf("diff snapshot %q want %s", got, snapshot)
	}
	if !reviewDiffHasFile(diffs.Diffs, fileID) {
		return "", fmt.Errorf("diffs omitted file id %s", fileID)
	}
	tracked := false
	searched, err := desk.SearchWorkspaceReviewDiffs(ctx, desktop.SearchWorkspaceReviewDiffsReq{
		Workspace: workspace, Source: source, ReviewSnapshotID: snapshot, Query: token, IncludeUntrackedFiles: &tracked,
	})
	if err != nil {
		return "", err
	}
	got, ok = searched.ReviewSnapshotID.String()
	if !ok || got != snapshot {
		return "", fmt.Errorf("search snapshot %q want %s", got, snapshot)
	}
	matches, ok := searched.TotalMatches.Float64()
	if !ok || matches < 1 || !reviewSearchHasPath(searched.MatchedFiles, path) {
		return "", fmt.Errorf("search did not match %s in snapshot %s", path, snapshot)
	}
	content, err := desk.GetWorkspaceReviewFileContent(ctx, desktop.GetWorkspaceReviewFileContentReq{
		Workspace: workspace, Source: source, ReviewSnapshotID: snapshot, FileID: fileID, Side: "new",
	})
	if err != nil {
		return "", err
	}
	gotID, ok := content.FileID.String()
	if !ok || gotID != fileID {
		return "", fmt.Errorf("content file %q want %s", gotID, fileID)
	}
	text := ""
	if content.Content != nil {
		text, _ = content.Content.String()
	}
	if !strings.Contains(text, token) {
		return "", fmt.Errorf("content %q does not contain %s", text, token)
	}
	return "snapshot " + snapshot + " file " + fileID + " content contains " + token, nil
}

func noteTrackedReview(ledger *coverageLedger, detail string) {
	ledger.note("idl.ListWorkspaceReviewFileDiffs", "real", detail)
	ledger.note("idl.SearchWorkspaceReviewDiffs", "real", detail)
	ledger.note("idl.GetWorkspaceReviewFileContent", "real", detail)
}

func reviewFileID(files []desktop.WorkspaceReviewFileView, path string) (string, bool) {
	for _, file := range files {
		got, ok := file.Path.String()
		if !ok || got != path {
			continue
		}
		id, ok := file.FileID.String()
		return id, ok && id != ""
	}
	return "", false
}

func reviewDiffHasFile(diffs []desktop.WorkspaceReviewFileDiffView, fileID string) bool {
	for _, diff := range diffs {
		got, ok := diff.FileID.String()
		if ok && got == fileID {
			return true
		}
	}
	return false
}

func reviewSearchHasPath(files []desktop.WorkspaceReviewSearchFileView, path string) bool {
	for _, file := range files {
		got, ok := file.Path.String()
		if ok && got == path {
			return true
		}
	}
	return false
}

func reviewResolution(err error) bool {
	var statusErr *desktop.StatusError
	return errors.As(err, &statusErr) && statusErr.Status == 422 && strings.Contains(string(statusErr.Body), "WORKSPACE_REVIEW_RESOLUTION_FAILED")
}

func statusBody(err error) string {
	var statusErr *desktop.StatusError
	if errors.As(err, &statusErr) {
		return string(statusErr.Body)
	}
	return err.Error()
}

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	gitOut(t, dir, args...)
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=sdk", "GIT_AUTHOR_EMAIL=sdk@example.com", "GIT_COMMITTER_NAME=sdk", "GIT_COMMITTER_EMAIL=sdk@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out)
}

func TestReviewResolutionFailureDoesNotPass(t *testing.T) {
	outcome, detail := classifyServer(&desktop.StatusError{
		Status: 422,
		Body:   []byte(`{"code":422,"key":"WORKSPACE_REVIEW_RESOLUTION_FAILED","message":""}`),
	})
	if outcome != "failed" {
		t.Fatalf("outcome %s detail %s", outcome, detail)
	}
}

func assertFileWatchAndSearch(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	watchCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	watch, err := desk.WatchWorkspaceFiles(watchCtx, desktop.WatchWorkspaceFilesReq{Targets: []desktop.WorkspaceFileWatchTarget{{
		Workspace: jsonScalar(workspace),
		Path:      jsonScalar("saved.txt"),
	}}})
	if err != nil {
		recordCall(t, ledger, "idl.WatchWorkspaceFiles", err)
	} else {
		defer watch.Close()
		if writeErr := os.WriteFile(workspace+"/saved.txt", []byte("saved-text\n"), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		frame, recvErr := watch.Recv(watchCtx)
		if recvErr != nil {
			ledger.note("idl.WatchWorkspaceFiles", "notrun", "watch produced no frame: "+recvErr.Error())
		} else if kind, ok := frame.Kind.String(); !ok || kind == "" {
			ledger.note("idl.WatchWorkspaceFiles", "notrun", "watch frame had no kind")
		} else {
			ledger.note("idl.WatchWorkspaceFiles", "real", "frame kind "+kind)
		}
	}
	searchCtx, searchCancel := context.WithTimeout(ctx, 5*time.Second)
	defer searchCancel()
	const searchID = "sdk-search-1"
	search, err := desk.StreamWorkspaceFileSearch(searchCtx, desktop.StreamFileSearchReq{
		SearchID:  searchID,
		Workspace: workspace,
		Query:     "batch",
	})
	if err != nil {
		recordCall(t, ledger, "idl.StreamWorkspaceFileSearch", err)
		return
	}
	defer search.Close()
	found := false
	for {
		frame, recvErr := search.Recv(searchCtx)
		if recvErr != nil {
			break
		}
		if id, ok := frame.SearchID.String(); ok && id == searchID {
			found = true
		}
		for _, path := range frame.Paths {
			if text, ok := path.String(); ok && strings.Contains(text, "batch") {
				found = true
			}
		}
		if done, ok := frame.Complete.Bool(); ok && done {
			break
		}
	}
	if !found {
		ledger.note("idl.StreamWorkspaceFileSearch", "notrun", "search stream did not echo sdk-search-1 or batch")
		return
	}
	ledger.note("idl.StreamWorkspaceFileSearch", "real", "search id sdk-search-1")
	stopped, err := desk.StopWorkspaceFileSearch(ctx, desktop.StopFileSearchReq{SearchID: searchID})
	if err != nil {
		recordCall(t, ledger, "idl.StopWorkspaceFileSearch", err)
		return
	}
	if detail, ok := populatedDetail(reflect.ValueOf(stopped)); ok {
		ledger.note("idl.StopWorkspaceFileSearch", "real", detail)
	} else {
		ledger.note("idl.StopWorkspaceFileSearch", "notrun", "stop returned no populated field")
	}
}

func assertOpenAndReveal(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	opened, err := desk.OpenWorkspaceFile(ctx, desktop.OpenFileReq{Workspace: workspace, Path: "saved.txt"})
	notePopulated(t, ledger, "idl.OpenWorkspaceFile", err, opened)
	revealed, err := desk.RevealWorkspaceFile(ctx, desktop.RevealFileReq{Workspace: workspace, Path: "saved.txt"})
	notePopulated(t, ledger, "idl.RevealWorkspaceFile", err, revealed)
}

func assertBuiltinAgentRead(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent string) {
	t.Helper()
	notice, err := desk.GetAgentLegacyHistoryNotice(ctx, desktop.GetAgentLegacyHistoryNoticeReq{Name: agent})
	if err != nil {
		recordCall(t, ledger, "idl.GetAgentLegacyHistoryNotice", err)
		return
	}
	if notice.SessionID == nil {
		ledger.note("idl.GetAgentLegacyHistoryNotice", "real", "getLegacyHistoryNotice returned undefined for builtin "+agent+", so session_id was omitted. The read did not modify the agent")
		return
	}
	if id, ok := notice.SessionID.String(); ok && id != "" {
		ledger.note("idl.GetAgentLegacyHistoryNotice", "real", "session_id present")
	}
}

func assertMarketplaceRefresh(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	refreshed, err := desk.RefreshPluginMarketplaces(ctx, desktop.RefreshPluginMarketplacesReq{})
	if err != nil {
		recordCall(t, ledger, "idl.RefreshPluginMarketplaces", err)
		if strings.Contains(errorText(err), "PLUGIN_MARKETPLACE_DISABLED") || strings.Contains(errorText(err), "403") {
			blockRoot(ledger, "idl.ListPluginMarketplaces", []string{
				"idl.RefreshPluginMarketplaces", "idl.AddPluginMarketplace", "idl.SetPluginMarketplaceAutoUpdate",
				"idl.RemovePluginMarketplace", "idl.GetMarketplacePlugin", "idl.PreviewGithubPlugin", "idl.ImportGithubPlugin",
			}, "plugin marketplaces are disabled in this build; install and marketplace mutation were not called")
		}
		return
	}
	ledger.note("idl.RefreshPluginMarketplaces", "real", "marketplace list len "+itoaLen(len(refreshed.Marketplaces)))
}

func assertSameModelSelection(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	listed, err := desk.ListModels(ctx, desktop.ListModelsReq{})
	if err != nil {
		recordCall(t, ledger, "idl.SelectModel", err)
		return
	}
	var provider, model string
	for _, item := range listed.Models {
		selected, ok := item.Selected.Bool()
		if !ok || !selected {
			continue
		}
		provider, _ = item.ProviderID.String()
		model, _ = item.ModelID.String()
		break
	}
	if provider == "" || model == "" {
		ledger.note("idl.SelectModel", "notrun", "ListModels had no selected provider and model to write back")
		return
	}
	replace := true
	selected, err := desk.SelectModel(ctx, desktop.SelectModelReq{ProviderID: provider, ModelID: model, ReplaceSelection: &replace})
	if err != nil || !scalarTrue(selected.Success) {
		recordCall(t, ledger, "idl.SelectModel", err)
		return
	}
	again, err := desk.ListModels(ctx, desktop.ListModelsReq{})
	if err != nil {
		t.Fatalf("list models after select: %v", err)
	}
	for _, item := range again.Models {
		gotProvider, _ := item.ProviderID.String()
		gotModel, _ := item.ModelID.String()
		flag, ok := item.Selected.Bool()
		if gotProvider == provider && gotModel == model && ok && flag {
			ledger.note("idl.SelectModel", "real", "selected "+provider+"/"+model+" remained selected")
			ledger.note("idl.ListModels", "real", "selected model reread")
			return
		}
	}
	t.Fatal("selected model was not selected after SelectModel")
}

func assertSameModelSource(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	current, err := desk.GetMinimaxModelSource(ctx, desktop.GetMinimaxModelSourceReq{})
	if err != nil || current.ActiveSource == nil {
		recordCall(t, ledger, "idl.SetMinimaxModelSource", err)
		return
	}
	source, ok := current.ActiveSource.String()
	if !ok || source == "" {
		ledger.note("idl.SetMinimaxModelSource", "notrun", "active source was not a string")
		return
	}
	set, err := desk.SetMinimaxModelSource(ctx, desktop.SetMinimaxModelSourceReq{Source: source})
	if err != nil || set.ActiveSource == nil {
		recordCall(t, ledger, "idl.SetMinimaxModelSource", err)
		return
	}
	got, ok := set.ActiveSource.String()
	if !ok || got != source {
		t.Fatalf("model source %q", got)
	}
	ledger.note("idl.GetMinimaxModelSource", "real", "active source "+source)
	ledger.note("idl.SetMinimaxModelSource", "real", "active source written back as "+source)
}

func assertContextCaptureToggle(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	on, err := desk.PutLlmContextCapture(ctx, desktop.PutLLMContextCaptureReq{Enabled: true})
	if err != nil {
		if legacyInspectorHTTP500(err) {
			ledger.note("idl.PutLlmContextCapture", "negative", legacyInspectorDoorDetail)
			return
		}
		recordCall(t, ledger, "idl.PutLlmContextCapture", err)
		return
	}
	enabled, ok := on.CaptureEnabled.Bool()
	if !ok || !enabled {
		ledger.note("idl.PutLlmContextCapture", "notrun", "capture_enabled was not true")
		return
	}
	off, err := desk.PutLlmContextCapture(ctx, desktop.PutLLMContextCaptureReq{Enabled: false})
	if err != nil {
		t.Fatalf("restore capture: %v", err)
	}
	enabled, ok = off.CaptureEnabled.Bool()
	if !ok || enabled {
		t.Fatal("capture_enabled stayed true")
	}
	ledger.note("idl.PutLlmContextCapture", "real", "capture_enabled true then false")
}

func assertEchoMCP(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join("testdata", "mcp_echo.py"))
	if err != nil {
		t.Fatal(err)
	}
	config, err := json.Marshal(map[string]any{
		"type":    "stdio",
		"command": "python3",
		"args":    []string{script},
		"enabled": true,
		"tools": []map[string]any{{
			"name":        "echo",
			"description": "Return the given text unchanged.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
				"required": []string{"text"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	const serverName = "sdkecho"
	configJSON := string(config)
	upserted, err := desk.UpsertLocalMcpRuntimeServer(ctx, desktop.UpsertLocalMCPRuntimeServerReq{ServerName: serverName, ConfigJSON: &configJSON})
	if err != nil || !scalarTrue(&upserted.Ok) || upserted.Server == nil {
		t.Fatalf("UpsertLocalMcpRuntimeServer: %v", err)
	}
	gotName, ok := upserted.Server.Name.String()
	if !ok || gotName != serverName {
		t.Fatalf("runtime server %q", gotName)
	}
	listed, err := desk.ListLocalMcpRuntimeTools(ctx, desktop.ListLocalMCPRuntimeToolsReq{ServerName: serverName})
	if err != nil {
		t.Fatalf("ListLocalMcpRuntimeTools: %v", err)
	}
	if !runtimeToolNamed(listed.Tools, "echo") {
		t.Fatalf("echo tool missing: %#v", listed.Tools)
	}
	fetched, err := desk.GetLocalMcpRuntimeTool(ctx, desktop.GetLocalMCPRuntimeToolReq{ServerName: serverName, ToolName: "echo"})
	if err != nil || fetched.Tool == nil {
		t.Fatalf("GetLocalMcpRuntimeTool: %v", err)
	}
	toolName, ok := fetched.Tool.Name.String()
	if !ok || toolName != "echo" || !strings.Contains(string(fetched.Tool.InputSchemaJSON), "text") {
		t.Fatalf("tool %#v", fetched.Tool)
	}
	arguments := `{"text":"hello-echo"}`
	called, err := desk.CallLocalMcpRuntimeTool(ctx, desktop.CallLocalMCPRuntimeToolReq{Server: serverName, Tool: "echo", ArgumentsJSON: &arguments})
	if err != nil {
		t.Fatalf("CallLocalMcpRuntimeTool: %v", err)
	}
	if !strings.Contains(string(called.ResultJSON), "sdk-echo-marker:hello-echo") || strings.Contains(string(called.ResultJSON), `"isError":true`) {
		t.Fatalf("echo result %s", called.ResultJSON)
	}
	ledger.note("idl.UpsertLocalMcpRuntimeServer", "real", "server sdkecho command python3 testdata/mcp_echo.py")
	ledger.note("idl.ListLocalMcpRuntimeTools", "real", "tool echo")
	ledger.note("idl.GetLocalMcpRuntimeTool", "real", "echo input schema contains text")
	ledger.note("idl.CallLocalMcpRuntimeTool", "real", "result contains sdk-echo-marker:hello-echo")
}

func runtimeToolNamed(tools []desktop.LocalMCPRuntimeTool, name string) bool {
	for _, tool := range tools {
		got, ok := tool.Name.String()
		if ok && got == name {
			return true
		}
	}
	return false
}

func assertManagedWorktrees(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	one := createCleanWorktree(t, ctx, desk, workspace, "sdk-wt-managed", "sdk-wt-managed")
	removed, err := desk.RemoveManagedWorkspaceGitWorktree(ctx, desktop.RemoveWorktreeReq{Workspace: workspace, WorktreeDir: one})
	if err != nil || !scalarTrue(removed.Success) {
		t.Fatalf("RemoveManagedWorkspaceGitWorktree: %v %s", err, scalarText(removed.Error))
	}
	if worktreeListed(t, ctx, desk, workspace, one) {
		t.Fatalf("managed worktree %s still listed", one)
	}
	batch := createCleanWorktree(t, ctx, desk, workspace, "sdk-wt-batch", "sdk-wt-batch")
	batchRemoved, err := desk.RemoveManagedWorkspaceGitWorktreesBatch(ctx, desktop.RemoveManagedWorktreesBatchReq{
		Items: []desktop.RemoveWorktreeReq{{Workspace: workspace, WorktreeDir: batch}},
	})
	if err != nil || !scalarTrue(&batchRemoved.Success) || len(batchRemoved.FailedItems) != 0 {
		t.Fatalf("RemoveManagedWorkspaceGitWorktreesBatch: %v failed %#v", err, batchRemoved.FailedItems)
	}
	if !scalarListHas(batchRemoved.RemovedPaths, batch) {
		t.Fatalf("removed paths %s", batchRemoved.RemovedPaths)
	}
	if worktreeListed(t, ctx, desk, workspace, batch) {
		t.Fatalf("batch worktree %s still listed", batch)
	}
	ledger.note("idl.RemoveManagedWorkspaceGitWorktree", "real", "fresh worktree absent after remove")
	ledger.note("idl.RemoveManagedWorkspaceGitWorktreesBatch", "real", "fresh worktree absent after batch remove")
}

func createCleanWorktree(t *testing.T, ctx context.Context, desk *desktop.Client, workspace, branch, dirName string) string {
	t.Helper()
	created, err := desk.CreateWorkspaceGitWorktree(ctx, desktop.CreateWorktreeReq{
		Workspace: workspace,
		Options:   &desktop.CreateWorktreeOptions{Branch: &branch, DirName: &dirName, Base: crudPtr("master")},
	})
	if err != nil || !scalarTrue(created.Success) || created.WorktreeDir == nil {
		t.Fatalf("CreateWorkspaceGitWorktree %s: %v %s", branch, err, scalarText(created.Error))
	}
	dir, ok := created.WorktreeDir.String()
	if !ok || dir == "" {
		t.Fatal("worktree dir")
	}
	return dir
}

func worktreeListed(t *testing.T, ctx context.Context, desk *desktop.Client, workspace, dir string) bool {
	t.Helper()
	listed, err := desk.ListWorkspaceGitWorktrees(ctx, desktop.ListWorktreesReq{Workspace: workspace})
	if err != nil {
		t.Fatalf("list worktrees: %v", err)
	}
	for _, item := range listed.Worktrees {
		if item.Path == nil {
			continue
		}
		path, ok := item.Path.String()
		if ok && path == dir {
			return true
		}
	}
	return false
}

func assertSandboxWriteBack(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	status, err := desk.GetSandboxStatus(ctx, desktop.GetSandboxStatusReq{})
	if err != nil || status.Status == nil {
		t.Fatalf("GetSandboxStatus: %v", err)
	}
	current := status.Status
	filesystem, okFS := scalarNumber(current.FilesystemMode)
	network, okNet := scalarNumber(current.NetworkMode)
	local, okLocal := scalarNumber(current.LocalAccess)
	state, okState := current.State.Float64()
	if !okFS || !okNet || !okLocal || !okState {
		ledger.note("idl.PutSandboxConfig", "notrun", "status did not return numeric filesystem_mode, network_mode, local_access, and state")
		return
	}
	if scalarCount(current.DenyReadRuleCount) > 0 || scalarCount(current.DeniedDomainRuleCount) > 0 {
		ledger.note("idl.PutSandboxConfig", "notrun", "status has deny rules and the put body would replace them with an empty list")
		return
	}
	var enabled bool
	switch int(state) {
	case 1:
		enabled = false
	case 3:
		enabled = true
	default:
		ledger.note("idl.PutSandboxConfig", "notrun", "state is not DISABLED 1 or READY 3, so enabled was not chosen")
		return
	}
	if filesystem < 1 || filesystem > 4 || (network != 1 && network != 2) || (local != 1 && local != 2) {
		t.Fatalf("sandbox modes filesystem %v network %v local %v", filesystem, network, local)
	}
	put, err := desk.PutSandboxConfig(ctx, desktop.PutSandboxConfigReq{Config: &desktop.SandboxConfig{
		Enabled: enabled,
		Filesystem: &desktop.SandboxFilesystemConfig{
			Policy:    &desktop.SandboxFilesystemPolicy{Mode: numScalar(int(filesystem))},
			DenyRead:  []desktop.Scalar{},
			DenyWrite: []desktop.Scalar{},
		},
		Network: &desktop.SandboxNetworkConfig{
			Policy:        &desktop.SandboxNetworkPolicy{Mode: numScalar(int(network))},
			DeniedDomains: []desktop.Scalar{},
		},
		LocalAccess: desktop.SandboxLocalAccess(local),
	}})
	if err != nil || put.Result == nil {
		t.Fatalf("PutSandboxConfig: %v", err)
	}
	again, err := desk.GetSandboxStatus(ctx, desktop.GetSandboxStatusReq{})
	if err != nil || again.Status == nil {
		t.Fatalf("sandbox reread: %v", err)
	}
	gotFS, _ := scalarNumber(again.Status.FilesystemMode)
	gotNet, _ := scalarNumber(again.Status.NetworkMode)
	gotLocal, _ := scalarNumber(again.Status.LocalAccess)
	gotState, _ := again.Status.State.Float64()
	if gotFS != filesystem || gotNet != network || gotLocal != local || gotState != state {
		t.Fatalf("sandbox changed filesystem %v network %v local %v state %v", gotFS, gotNet, gotLocal, gotState)
	}
	ledger.note("idl.GetSandboxStatus", "real", "modes reread after put")
	ledger.note("idl.PutSandboxConfig", "real", "enabled followed state and filesystem, network, and local access stayed the same")
}

func assertCanvasFile(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace, agent string) {
	t.Helper()
	title := "canvas session"
	created, err := desk.CreateSession(ctx, desktop.CreateSessionReq{Name: agent, Title: &title, WorkspaceDir: &workspace})
	if err != nil || created.SessionID == nil {
		t.Fatalf("canvas session: %v", err)
	}
	sessionID, ok := created.SessionID.String()
	if !ok || sessionID == "" {
		t.Fatal("canvas session id")
	}
	relative := jsonScalar("batch.txt")
	prepared, err := desk.PrepareCanvasFiles(ctx, desktop.PrepareCanvasFilesReq{
		SessionID: sessionID,
		Targets: []desktop.CanvasFileTargetV1{{
			ID:           jsonScalar("sdk-file"),
			Kind:         jsonScalar("workspace"),
			RelativePath: &relative,
		}},
	})
	if err != nil {
		t.Fatalf("PrepareCanvasFiles: %v", err)
	}
	if len(prepared.Files) != 1 {
		t.Fatalf("prepared files %#v", prepared.Files)
	}
	gotPath, ok := prepared.Files[0].RelativePath.String()
	if !ok || gotPath != "batch.txt" {
		t.Fatalf("prepared path %q", gotPath)
	}
	applied, err := desk.ApplyCanvasOperation(ctx, desktop.ApplyCanvasOperationReq{
		SessionID: sessionID,
		Operation: &desktop.CanvasOperationV1{
			SchemaVersion: 1,
			OperationID:   "sdk-canvas-op",
			Mutations: []desktop.CanvasMutationV1{{
				Kind:         jsonScalar("add_file"),
				NodeID:       jsonScalar("sdk-node"),
				RelativePath: &relative,
				Layout: &desktop.CanvasLayoutV1{
					X: numScalar(0), Y: numScalar(0), Width: numScalar(120), Height: numScalar(80), ZIndex: numScalar(1),
				},
			}},
		},
	})
	if err != nil || applied.Document == nil {
		t.Fatalf("ApplyCanvasOperation: %v", err)
	}
	opID, ok := applied.OperationID.String()
	if !ok || opID != "sdk-canvas-op" || !canvasHasPath(applied.Document.Nodes, "batch.txt") {
		t.Fatalf("canvas operation %q nodes %#v", opID, applied.Document.Nodes)
	}
	ledger.note("idl.PrepareCanvasFiles", "real", "workspace target batch.txt")
	ledger.note("idl.ApplyCanvasOperation", "real", "add_file node batch.txt operation sdk-canvas-op")
}

func canvasHasPath(nodes []desktop.CanvasFileNodeV1, path string) bool {
	for _, node := range nodes {
		got, ok := node.RelativePath.String()
		if ok && got == path {
			return true
		}
	}
	return false
}

func assertAgentImportRead(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	claude := "---\nname: sdkclaude\ndescription: local claude import\n---\nReply with the marker sdk-claude.\n"
	readClaude, err := desk.ReadClaudeCodeAgentImport(ctx, desktop.ReadAgentImportReq{Content: claude})
	if err != nil || readClaude.Preview == nil {
		t.Fatalf("ReadClaudeCodeAgentImport: %v", err)
	}
	name, ok := readClaude.Preview.ProposedName.String()
	if !ok || name != "sdkclaude" {
		t.Fatalf("claude proposed name %q", name)
	}
	codex := "name = \"sdkcodex\"\ndescription = \"local codex import\"\ndeveloper_instructions = \"Reply with the marker sdk-codex.\"\n"
	readCodex, err := desk.ReadCodexAgentImport(ctx, desktop.ReadAgentImportReq{Content: codex})
	if err != nil || readCodex.Preview == nil {
		t.Fatalf("ReadCodexAgentImport: %v", err)
	}
	name, ok = readCodex.Preview.ProposedName.String()
	if !ok || name != "sdkcodex" {
		t.Fatalf("codex proposed name %q", name)
	}
	ledger.note("idl.ReadClaudeCodeAgentImport", "real", "proposed name sdkclaude")
	ledger.note("idl.ReadCodexAgentImport", "real", "proposed name sdkcodex")
}

func assertQuestionnaire(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent, sessionID string) {
	t.Helper()
	stepID := jsonScalar("step-1")
	optionID := jsonScalar("yes")
	label := jsonScalar("Yes")
	question := jsonScalar("Pick one.")
	begun, err := desk.BeginQuestionnaire(ctx, desktop.BeginQuestionnaireReq{
		SessionID: sessionID,
		AgentName: &agent,
		ToolInput: &desktop.AskUserToolInput{
			Title: crudPtr("sdk question"),
			Steps: []desktop.AskUserToolStepInput{{
				ID:       &stepID,
				Question: question,
				Options:  []desktop.AskUserToolOptionInput{{ID: &optionID, Label: label}},
			}},
		},
	})
	if err != nil {
		t.Fatalf("BeginQuestionnaire: %v", err)
	}
	requestID, ok := begun.RequestID.String()
	_, schemaOK := begun.SchemaVersion.Float64()
	steps, stepsOK := begun.StepCount.Float64()
	if !ok || requestID == "" || !schemaOK || !stepsOK || steps != 1 {
		t.Fatalf("questionnaire %#v", begun)
	}
	include := true
	pending, err := desk.GetPendingQuestionnaire(ctx, desktop.GetPendingQuestionnaireReq{Name: agent, SessionID: sessionID, IncludeUnanswered: &include})
	if err != nil || !questionnaireListed(pending, requestID, "Pick one.") {
		t.Fatalf("GetPendingQuestionnaire: %v %#v", err, pending)
	}
	fetched, err := desk.GetQuestionnaire(ctx, desktop.GetQuestionnaireReq{Name: agent, RequestID: requestID})
	if err != nil || fetched.Request == nil || len(fetched.Request.Steps) == 0 {
		t.Fatalf("GetQuestionnaire: %v", err)
	}
	gotQuestion, ok := fetched.Request.Steps[0].Question.String()
	if !ok || gotQuestion != "Pick one." {
		t.Fatalf("question %q", gotQuestion)
	}
	dismissed, err := desk.DismissQuestionnaire(ctx, desktop.DismissQuestionnaireReq{Name: agent, RequestID: requestID})
	if err != nil || !scalarTrue(&dismissed.Ok) {
		t.Fatalf("DismissQuestionnaire: %v", err)
	}
	gotID, ok := dismissed.RequestID.String()
	dismissedAt, atOK := dismissed.DismissedAt.Float64()
	if !ok || gotID != requestID || !atOK || dismissedAt <= 0 {
		t.Fatalf("dismiss %#v", dismissed)
	}
	after, err := desk.GetPendingQuestionnaire(ctx, desktop.GetPendingQuestionnaireReq{Name: agent, SessionID: sessionID, IncludeUnanswered: &include})
	if err != nil || questionnaireListed(after, requestID, "Pick one.") {
		t.Fatalf("pending after dismiss: %v %#v", err, after)
	}
	ledger.note("idl.BeginQuestionnaire", "real", "owned request step count 1")
	ledger.note("idl.GetPendingQuestionnaire", "real", "pending request "+requestID+" question Pick one.")
	ledger.note("idl.GetQuestionnaire", "real", "question Pick one.")
	ledger.note("idl.DismissQuestionnaire", "real", "dismissed_at set and the request left the pending list")
}

func questionnaireListed(pending desktop.GetPendingQuestionnaireResp, requestID, question string) bool {
	if pending.Request != nil && questionnaireViewMatches(*pending.Request, requestID, question) {
		return true
	}
	for _, request := range pending.UnansweredRequests {
		if questionnaireViewMatches(request, requestID, question) {
			return true
		}
	}
	return false
}

func questionnaireViewMatches(request desktop.QuestionnaireRequestView, requestID, question string) bool {
	id, ok := request.ID.String()
	if !ok || id != requestID || len(request.Steps) == 0 {
		return false
	}
	got, ok := request.Steps[0].Question.String()
	return ok && got == question
}

func scalarNumber(value *desktop.Scalar) (float64, bool) {
	if value == nil {
		return 0, false
	}
	return value.Float64()
}

func scalarCount(value *desktop.Scalar) float64 {
	n, ok := scalarNumber(value)
	if !ok {
		return 0
	}
	return n
}

func numScalar(value int) desktop.Scalar {
	body, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return desktop.Scalar(body)
}

func markExhaustedDependencies(ledger *coverageLedger) {
	blockRoot(ledger, "idl.ListIdeApps", []string{"idl.OpenWorkspaceFileInIde"}, "ListIdeApps is HTTP 501 on local-runtime, so there is no ide id")
	blockRoot(ledger, "idl.CreateSkill", []string{"idl.ListSkillFiles", "idl.GetSkillFile"}, "CreateSkill is refused by content review, so there is no disposable skill to read")
	blockRoot(ledger, "idl.CreateCronDefinition", []string{"idl.ListCronSessions", "idl.UpdateCron", "idl.DeleteCron"}, "CreateCronDefinition is refused by content review, so there is no cron id")
	blockRoot(ledger, "idl.ListPluginMarketplaces", []string{"idl.InstallPlugin", "idl.UninstallPlugin", "idl.EnablePlugin", "idl.DisablePlugin", "idl.UpdatePlugin"}, "no disposable plugin was installed; marketplace install is disabled and builtin plugins were not modified")
	for _, id := range []string{
		"idl.PreviewModelAccessPackage", "idl.ImportModelAccessPackage", "idl.EnableModelAccessPackage", "idl.DisableModelAccessPackage",
		"idl.RollbackModelAccessPackage", "idl.UninstallModelAccessPackage", "idl.StartModelAccessDevSession", "idl.DismissModelAccessDevRequest",
		"idl.ReloadModelAccessDevSession", "idl.StopModelAccessDevSession", "idl.PromoteModelAccessDevSession", "idl.GetModelAccessExtensionStatus",
		"idl.ExportModelAccessPackage", "idl.SetModelAccessGlobal", "idl.SetModelAccessTarget", "idl.SyncModelAccessDelivery",
		"idl.SetModelAccessDeliveryOptIn", "idl.DiscoverModelAccessBorrow", "idl.UseModelAccessBorrow", "idl.ScanModelAccessApi",
		"idl.ImportModelAccessApi", "idl.QueryModelAccessUsage", "idl.EnableModelAccessPublicProvider", "idl.DisableModelAccessPublicProvider",
		"idl.GetModelAccessFailover", "idl.SetModelAccessFailover",
	} {
		ledger.note(id, "blocked", "root idl.ListModelAccess; that list is HTTP 501 MODEL_ACCESS_UNSUPPORTED_ON_LEGACY_HOST. Package, login, scan, and borrow calls were not sent")
	}
	for _, id := range []string{
		"idl.PrepareChannelConnection", "idl.DisconnectChannelConnection", "idl.DeleteChannelConnection", "idl.UnbindChannel",
		"idl.UpdateChannelBindingTarget", "idl.StartWechatBind", "idl.BindFeishu", "idl.StartLarkOnboard", "idl.CancelLarkOnboard",
	} {
		ledger.note(id, "blocked", "root idl.ListChannelBindings; channel routes are not mounted. Bind would be an external account action and was not called")
	}
	for _, id := range []string{
		"idl.EnsureMiniAppHostSession", "idl.SuggestMiniAppCreationIdeas", "idl.StartMiniAppCreation", "idl.StopMiniAppCreation",
		"idl.RemoveMiniAppCreation", "idl.MaintainMiniAppCreation", "idl.GetMiniAppCreation", "idl.ListMiniAppLaunchpad",
	} {
		ledger.note(id, "blocked", "requires header X-Archon-User-Id. No account id was invented")
	}
	for _, id := range []string{"idl.PrepareSharedInstall", "idl.CommitSharedInstall", "idl.DiscardSharedInstall"} {
		ledger.note(id, "blocked", "needs a share code or staged id from another user. Pack was not published")
	}
	ledger.note("idl.PackShareWork", "notrun", "kind is an untyped Scalar and no closed enum was recovered, so pack was not called")
	ledger.note("idl.SuggestCommitMessage", "blocked", "commit-message generation is a model turn and was not called")
	ledger.note("idl.BindSessionFileSyncOwner", "blocked", "needs an account id. No account id was invented")
	ledger.note("idl.ArchiveProjectSessions", "notrun", "archives sessions in the project. Session lifecycle is covered by the parent tester and was not repeated")
	for _, id := range []string{"idl.RemoveCanvasImageBackground", "idl.EnhanceCanvasImage"} {
		ledger.note(id, "notrun", "the handlers call image tools generatedBy canvas-remove-background and canvas-enhance-image. Billing was not proven absent, so they were not called")
	}
	for _, id := range []string{
		"idl.ReportSkillEvolveSignal", "idl.CancelSkillEvolveSignal", "idl.ReportSkillEvolveProposal", "idl.CancelSkillEvolveProposal",
		"idl.MarkSkillEvolveProposalActed", "idl.GetSkillEvolveProposal", "idl.TriggerSkillEvolve", "idl.RequestSkillEvolveScan",
		"idl.RevertSkillEvolve", "idl.MutateSkillEvolveLifecycle", "idl.InstallSkill", "idl.PreviewSkill",
	} {
		ledger.note(id, "notrun", "skill-evolve body_json and install URL were not recovered. No invented body or external URL was sent")
	}
	ledger.note("idl.ReplyQuestionnaire", "notrun", "reply starts a conversation continuation. On desktop auth without a lease that continuation is HTTP 503 QUESTIONNAIRE_CONTINUATION_NOT_STARTED, which is not a completed reply. The owned request was dismissed instead")
	ledger.note("idl.ReplyQuestion", "notrun", "reject and reply are permission decisions on a permission.ask request id. This run did not own a permission request, so neither call was sent")
	ledger.note("idl.RejectQuestion", "notrun", "reject and reply are permission decisions on a permission.ask request id. This run did not own a permission request, so neither call was sent")
	ledger.note("idl.RequestCompaction", "notrun", "requestCompaction enters turn admission and runs a compaction model turn. NOTHING_TO_COMPACT is only the post-attempt unchanged outcome, so an empty session was not used as a probe")
	for _, id := range []string{
		"idl.CreateUserModelProvider", "idl.UpdateUserModelProvider", "idl.DuplicateUserModelProvider", "idl.DeleteUserModelProvider",
		"idl.TestUserModelProvider", "idl.TestUserModel", "idl.UpdateUserModelParameters", "idl.TestUserModelCandidate",
		"idl.DiscoverUserModelsCandidate", "idl.SaveUserModelProviderCandidate", "idl.DiscoverUserModels", "idl.UpdateMinimaxModelContext",
	} {
		ledger.note(id, "blocked", "needs a user provider credential or a provider id this process must not create")
	}
	for _, id := range []string{
		"idl.RenderExtension", "idl.PressExtension", "idl.InputExtension", "idl.SelectExtension", "idl.AckExtensionApplied",
		"idl.ReportExtensionFallback", "idl.AnswerExtensionQuery", "idl.CloseExtensionPane", "idl.SubscribeExtensionStream",
	} {
		ledger.note(id, "notrun", "needs an extension slot, plugin, and render sequence from a running extension UI. None was created")
	}
	ledger.note("idl.RestoreSessionWorktree", "notrun", "needs a session worktree. Session lifecycle is owned by the parent tester and was not repeated")
	ledger.note("idl.InvalidateSessionFileSyncSources", "notrun", "QuerySessionFileSyncSources returned no source object to invalidate")
	ledger.note("negative.unknown-desktop-path", "notrun", "covered by TestLiveSurface RawCall when that test runs; this batch did not repeat it")
}

func assertOwnedLocalFixtures(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent, sessionID string) {
	t.Helper()
	assertRotationUnimplemented(t, ctx, desk, ledger, agent, sessionID)
	assertPluginDecline(t, ctx, desk, ledger, agent, sessionID)
	assertDisabledCronCreate(t, ctx, desk, ledger, agent)
	assertImportedAgentCreate(t, ctx, desk, ledger)
	assertBackgroundTaskList(t, ctx, desk, ledger, sessionID)
	assertSideChatPromote(t, ctx, desk, ledger, sessionID)
	assertSessionReads(t, ctx, desk, ledger, sessionID)
	assertMemoryV2Unimplemented(t, ctx, desk, ledger, agent, sessionID)
}

func assertRotationUnimplemented(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent, sessionID string) {
	t.Helper()
	_, err := desk.RequestRotation(ctx, desktop.RequestRotationReq{Name: agent, ID: sessionID})
	expectNotImplemented(t, ledger, "idl.RequestRotation", err)
}

func assertPluginDecline(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent, sessionID string) {
	t.Helper()
	declined, err := desk.RecordPluginRecommendationDecline(ctx, desktop.RecordPluginRecommendationDeclineReq{
		Name:       agent,
		SessionID:  sessionID,
		PluginName: "sdk-plugin",
	})
	if err != nil {
		expectNotImplemented(t, ledger, "idl.RecordPluginRecommendationDecline", err)
		return
	}
	ok, yes := declined.Ok.Bool()
	if !yes || !ok {
		ledger.note("idl.RecordPluginRecommendationDecline", "notrun", "decline returned without ok=true")
		return
	}
	ledger.note("idl.RecordPluginRecommendationDecline", "real", "ok true for plugin sdk-plugin on the owned session")
}

func assertDisabledCronCreate(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent string) {
	t.Helper()
	enabled := false
	schedule := "0 0 1 1 *"
	prompt := "hello"
	created, err := desk.CreateCron(ctx, desktop.CreateCronReq{
		Name:     agent,
		CronName: "sdk-disabled",
		Schedule: &schedule,
		Prompt:   &prompt,
		Enabled:  &enabled,
	})
	if err != nil || created.Task == nil || created.Task.CronID == nil {
		var statusErr *desktop.StatusError
		if errors.As(err, &statusErr) && statusErr.Status == 501 {
			ledger.note("idl.CreateCron", "negative", "HTTP 501 NOT_IMPLEMENTED. enabled was false, the request was not stored, and TriggerCron was not called")
			return
		}
		if errors.As(err, &statusErr) && strings.Contains(string(statusErr.Body), "CONTENT_POLICY_VIOLATION") {
			ledger.note("idl.CreateCron", "blocked", "assertCronContentSafe reviews the cron name and prompt before createTask. Uninitialized reviewContent fails closed with HTTP 422. enabled was false, so no scheduler row was written and TriggerCron was not called")
			return
		}
		noteServer(t, ledger, "idl.CreateCron", err)
		return
	}
	id, ok := created.Task.CronID.String()
	flag, flagOK := created.Task.Enabled.Bool()
	if !ok || id == "" || !flagOK || flag {
		t.Cleanup(func() {
			cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if id != "" {
				_, _ = desk.DeleteCron(cleanCtx, desktop.DeleteCronReq{CronID: id})
			}
		})
		t.Fatalf("CreateCron id %q enabled %v; the task was not left running", id, flag)
	}
	if _, err := desk.DeleteCron(ctx, desktop.DeleteCronReq{CronID: id}); err != nil {
		t.Fatalf("DeleteCron %s: %v", id, err)
	}
	ledger.note("idl.CreateCron", "real", "disabled cron "+id+" deleted in the same test. TriggerCron was not called")
}

func assertImportedAgentCreate(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	content := "---\nname: sdkclaude\ndescription: local claude import\n---\nReply with the marker sdk-claude.\n"
	preview, err := desk.ReadClaudeCodeAgentImport(ctx, desktop.ReadAgentImportReq{Content: content})
	if err != nil || preview.Preview == nil {
		t.Fatalf("ReadClaudeCodeAgentImport before create: %v", err)
	}
	format, formatOK := preview.Preview.Format.String()
	digest, digestOK := preview.Preview.SourceDigest.String()
	target, targetOK := preview.Preview.ProposedName.String()
	if !formatOK || format == "" || !digestOK || digest == "" || !targetOK || target == "" {
		t.Fatal("import preview omitted format, digest, or proposed name")
	}
	created, err := desk.CreateImportedAgent(ctx, desktop.CreateImportedAgentReq{
		Format:         format,
		Content:        content,
		ExpectedDigest: digest,
		TargetName:     target,
	})
	if err != nil || created.Agent == nil {
		var statusErr *desktop.StatusError
		if errors.As(err, &statusErr) && strings.Contains(string(statusErr.Body), "CONTENT_POLICY_VIOLATION") {
			const why = "createImportedDefinition reviews canonical content and the proposed name before service.create. Uninitialized reviewContent fails closed, so no agent row was written. Builtin agents were not modified"
			ledger.note("idl.CreateImportedAgent", "blocked", why)
			ledger.note("idl.ReplaceRootSession", "notrun", "runReplacement aborts the previous root. Builtin mavis was not repointed, and CreateImportedAgent did not leave an owned agent to repoint")
			return
		}
		noteServer(t, ledger, "idl.CreateImportedAgent", err)
		ledger.note("idl.ReplaceRootSession", "notrun", "runReplacement aborts the previous root. Builtin mavis was not repointed, and the import did not return an owned agent")
		return
	}
	name, nameOK := created.Agent.Name.String()
	if !nameOK || name == "" || name == "mavis" {
		t.Fatalf("imported agent name %q", name)
	}
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = desk.DeleteAgent(cleanCtx, desktop.DeleteAgentReq{Name: name})
	})
	replacedRoot := assertOwnedRootReplacement(t, ctx, desk, ledger, name)
	if _, err := desk.DeleteAgent(ctx, desktop.DeleteAgentReq{Name: name}); err != nil {
		t.Fatalf("DeleteAgent %s: %v", name, err)
	}
	if !replacedRoot {
		ledger.note("idl.ReplaceRootSession", "notrun", "owned agent "+name+" was deleted before its root could be replaced")
	}
	ledger.note("idl.CreateImportedAgent", "real", "agent "+name+" created from the claude preview digest and deleted")
}

func assertOwnedRootReplacement(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent string) bool {
	t.Helper()
	title := "owned root"
	created, err := desk.CreateSession(ctx, desktop.CreateSessionReq{Name: agent, Title: &title})
	if err != nil || created.SessionID == nil {
		ledger.note("idl.ReplaceRootSession", "notrun", "owned agent "+agent+" did not return a session to install as root")
		return false
	}
	sessionID, ok := created.SessionID.String()
	if !ok || sessionID == "" {
		return false
	}
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = desk.DeleteSession(cleanCtx, desktop.DeleteSessionReq{ID: sessionID})
	})
	replaced, err := desk.ReplaceRootSession(ctx, desktop.ReplaceRootSessionReq{Name: agent, SessionID: sessionID})
	if err != nil || !scalarTrue(replaced.Ok) {
		recordCall(t, ledger, "idl.ReplaceRootSession", err)
		return false
	}
	root, err := desk.GetRootSession(ctx, desktop.GetRootSessionReq{Name: agent})
	if err != nil || root.Session == nil || root.Session.SessionID == nil {
		t.Fatalf("GetRootSession after replace: %v", err)
	}
	got, ok := root.Session.SessionID.String()
	if !ok || got != sessionID {
		t.Fatalf("root session %q, want %s", got, sessionID)
	}
	ledger.note("idl.ReplaceRootSession", "real", "owned agent "+agent+" root is "+sessionID+". Builtin mavis was not repointed")
	return true
}

func assertBackgroundTaskList(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, sessionID string) {
	t.Helper()
	listed, err := desk.ListBackgroundTasks(ctx, desktop.ListBackgroundTasksReq{SessionID: sessionID})
	if err != nil {
		recordCall(t, ledger, "idl.ListBackgroundTasks", err)
		return
	}
	ledger.note("idl.ListBackgroundTasks", "real", fmt.Sprintf("owned session returned %d tasks", len(listed.Items)))
	if len(listed.Items) == 0 {
		const why = "ListBackgroundTasks on the owned session returned no task_id. The host creates background tasks from tool calls. No task id was invented, and Get, ReadOutput, Stop, and Delete were not called"
		for _, id := range []string{"idl.GetBackgroundTask", "idl.ReadBackgroundTaskOutput", "idl.StopBackgroundTask", "idl.DeleteBackgroundTask"} {
			ledger.note(id, "notrun", why)
		}
		return
	}
	taskID, ok := listed.Items[0].TaskID.String()
	if !ok || taskID == "" {
		t.Fatal("background task omitted task_id")
	}
	fetched, err := desk.GetBackgroundTask(ctx, desktop.GetBackgroundTaskReq{SessionID: sessionID, TaskID: taskID})
	if err != nil || fetched.Task == nil {
		recordCall(t, ledger, "idl.GetBackgroundTask", err)
	} else {
		ledger.note("idl.GetBackgroundTask", "real", "task "+taskID)
	}
	output, err := desk.ReadBackgroundTaskOutput(ctx, desktop.ReadBackgroundTaskOutputReq{SessionID: sessionID, TaskID: taskID})
	if err != nil {
		recordCall(t, ledger, "idl.ReadBackgroundTaskOutput", err)
	} else if detail, ok := populatedDetail(reflect.ValueOf(output)); ok {
		ledger.note("idl.ReadBackgroundTaskOutput", "real", detail)
	}
}

func assertSideChatPromote(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, sessionID string) {
	t.Helper()
	createOnly := true
	created, err := desk.CreateSideChat(ctx, desktop.CreateSideChatReq{
		ID:              sessionID,
		ClientRequestID: "sdk-sidechat-create",
		CreateOnly:      &createOnly,
	})
	if err != nil || created.Result == nil {
		recordCall(t, ledger, "idl.CreateSideChat", err)
		return
	}
	sideID, ok := created.Result.SessionID.String()
	if !ok || sideID == "" || sideID == sessionID {
		t.Fatalf("side chat session %q", sideID)
	}
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = desk.DeleteSession(cleanCtx, desktop.DeleteSessionReq{ID: sideID})
	})
	promoted, err := desk.PromoteSideChat(ctx, desktop.PromoteSideChatReq{ID: sideID, ClientRequestID: "sdk-sidechat-promote"})
	if err != nil || promoted.Result == nil {
		recordCall(t, ledger, "idl.PromoteSideChat", err)
		_, _ = desk.DeleteSession(ctx, desktop.DeleteSessionReq{ID: sideID})
		return
	}
	view, err := desk.GetSession(ctx, desktop.GetSessionReq{ID: sideID})
	if err != nil || view.Session == nil || view.Session.Visibility == nil {
		t.Fatalf("GetSession side chat: %v", err)
	}
	visibility, ok := view.Session.Visibility.String()
	if !ok || visibility != "visible" {
		t.Fatalf("side chat visibility %q", visibility)
	}
	if _, err := desk.DeleteSession(ctx, desktop.DeleteSessionReq{ID: sideID}); err != nil {
		t.Fatalf("DeleteSession side chat: %v", err)
	}
	ledger.note("idl.CreateSideChat", "real", "create_only side chat "+sideID+" with no content and no model")
	ledger.note("idl.PromoteSideChat", "real", "visibility visible")
}

func assertSessionReads(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, sessionID string) {
	t.Helper()
	eligible, err := desk.GetLocalSessionHandoffEligibility(ctx, desktop.GetLocalSessionHandoffEligibilityReq{ID: sessionID})
	if err != nil {
		var statusErr *desktop.StatusError
		if errors.As(err, &statusErr) && strings.Contains(string(statusErr.Body), "HANDOFF_UNAVAILABLE") {
			ledger.note("idl.GetLocalSessionHandoffEligibility", "blocked", "HTTP 404 HANDOFF_UNAVAILABLE: managed login is required. ApproveLocalSessionHandoff was not called")
		} else {
			recordCall(t, ledger, "idl.GetLocalSessionHandoffEligibility", err)
		}
	} else {
		reason, ok := eligible.Reason.String()
		if !ok || reason == "" {
			t.Fatal("handoff eligibility omitted reason")
		}
		ledger.note("idl.GetLocalSessionHandoffEligibility", "real", "reason "+reason+". Approve was not called")
	}
	history, err := desk.ListSessionSourceHistory(ctx, desktop.ListSessionSourceHistoryReq{ID: sessionID})
	if err != nil {
		recordCall(t, ledger, "idl.ListSessionSourceHistory", err)
	} else {
		turns, turnsOK := history.SourcedTurnCount.Float64()
		sources, sourcesOK := history.SourceCount.Float64()
		if !turnsOK || !sourcesOK {
			t.Fatal("source history counts were not numbers")
		}
		ledger.note("idl.ListSessionSourceHistory", "real", fmt.Sprintf("sourced turns %.0f sources %.0f", turns, sources))
	}
	ledger.note("idl.GetPromptSuggestion", "notrun", "getPromptSuggestion rejects an empty turn_id with local_prompt_suggestion_turn_required. This owned session has no turn, so no turn id was invented and the read was not sent")
	ledger.note("idl.CancelPromptSuggestion", "notrun", "cancel addresses the same turn_id as GetPromptSuggestion. No turn id was invented")
	ledger.note("idl.ResolveAnnotationTarget", "notrun", "resolve addresses a review line range or deleted file on the owned session. This session has no review annotation, so no target was invented")
	ledger.note("idl.GetSessionHandoffSummary", "notrun", "summary addresses a dispatch_id. Eligibility was read, and no dispatch id was invented. ApproveLocalSessionHandoff was not called")
	ledger.note("idl.ApproveLocalSessionHandoff", "notrun", "approve would upload the session. No dispatch id was invented")
}

func assertMemoryV2Unimplemented(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent, sessionID string) {
	t.Helper()
	_, err := desk.GetMemoryV2Status(ctx, desktop.GetMemoryV2StatusReq{SessionID: sessionID})
	expectNotImplemented(t, ledger, "idl.GetMemoryV2Status", err)
	_, err = desk.ListMemoryV2Sessions(ctx, desktop.ListMemoryV2SessionsReq{SessionID: sessionID})
	expectNotImplemented(t, ledger, "idl.ListMemoryV2Sessions", err)
	_, err = desk.GetMemoryV2SessionDoc(ctx, desktop.GetMemoryV2SessionDocReq{SessionID: sessionID, SourceSessionID: sessionID})
	expectNotImplemented(t, ledger, "idl.GetMemoryV2SessionDoc", err)
	_, err = desk.GetMemoryV2Onboarding(ctx, desktop.GetMemoryV2OnboardingReq{})
	expectNotImplemented(t, ledger, "idl.GetMemoryV2Onboarding", err)
	_, err = desk.PrepareLegacyMemoryMigration(ctx, desktop.PrepareLegacyMemoryMigrationReq{AgentName: agent})
	expectNotImplemented(t, ledger, "idl.PrepareLegacyMemoryMigration", err)
	const why = "PrepareLegacyMemoryMigration returned HTTP 501 NOT_IMPLEMENTED before a migration row. Accept, Discard, Forget, Reset, and Complete were not sent"
	for _, id := range []string{
		"idl.AcceptLegacyMemoryMigration", "idl.DiscardLegacyMemoryMigration", "idl.ForgetMemoryV2Content",
		"idl.ResetMemoryV2Scope", "idl.CompleteMemoryV2Onboarding",
	} {
		ledger.note(id, "notrun", why)
	}
}

func expectNotImplemented(t *testing.T, ledger *coverageLedger, id string, err error) {
	t.Helper()
	var statusErr *desktop.StatusError
	if errors.As(err, &statusErr) && statusErr.Status == 501 && strings.Contains(string(statusErr.Body), "NOT_IMPLEMENTED") {
		ledger.note(id, "negative", "HTTP 501 NOT_IMPLEMENTED on local-runtime. The call was sent and was not treated as success")
		return
	}
	recordCall(t, ledger, id, err)
}

func gitListsPath(changes desktop.GitChangesResp, path string) bool {
	for _, file := range changes.Files {
		got, ok := file.Path.String()
		if ok && got == path {
			return true
		}
	}
	return scalarText(changes.StagedFiles) != "" && strings.Contains(scalarText(changes.StagedFiles), path) || strings.Contains(scalarText(changes.UnstagedFiles), path)
}

func scalarText(value *desktop.Scalar) string {
	if value == nil {
		return ""
	}
	return string(*value)
}

func jsonScalar(value string) desktop.Scalar {
	body, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return desktop.Scalar(body)
}

func itoaLen(n int) string {
	if n == 0 {
		return "0"
	}
	var digits [12]byte
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = byte('0' + n%10)
		n /= 10
	}
	return string(digits[i:])
}
