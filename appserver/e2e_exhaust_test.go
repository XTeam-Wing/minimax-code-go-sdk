package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

func assertExhaustLocal(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace, agent string) {
	t.Helper()
	assertGitStageRoundTrip(t, ctx, desk, ledger, workspace)
	assertReviewCommits(t, ctx, desk, ledger, workspace)
	assertFileWatchAndSearch(t, ctx, desk, ledger, workspace)
	assertOpenAndReveal(t, ctx, desk, ledger, workspace)
	assertBuiltinAgentRead(t, ctx, desk, ledger, agent)
	assertMarketplaceRefresh(t, ctx, desk, ledger)
	assertSameModelSelection(t, ctx, desk, ledger)
	assertSameModelSource(t, ctx, desk, ledger)
	assertContextCaptureToggle(t, ctx, desk, ledger)
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
	listed, err := desk.ListWorkspaceReviewCommits(ctx, desktop.ListWorkspaceReviewCommitsReq{Workspace: workspace, BaseRef: "master"})
	if err != nil {
		recordCall(t, ledger, "idl.ListWorkspaceReviewCommits", err)
		return
	}
	head, ok := listed.HeadCommit.String()
	if !ok || head == "" {
		ledger.note("idl.ListWorkspaceReviewCommits", "notrun", "head_commit was empty")
		return
	}
	ledger.note("idl.ListWorkspaceReviewCommits", "real", "head_commit "+head)
	source := &desktop.WorkspaceReviewSourceView{Type: "branch", BaseRef: crudPtr("master")}
	summary, err := desk.GetWorkspaceReviewSummary(ctx, desktop.GetWorkspaceReviewSummaryReq{Workspace: workspace, Source: source})
	if err != nil {
		recordCall(t, ledger, "idl.GetWorkspaceReviewSummary", err)
		blockRoot(ledger, "idl.GetWorkspaceReviewSummary", []string{
			"idl.ListWorkspaceReviewFileDiffs", "idl.GetWorkspaceReviewFileContent", "idl.SearchWorkspaceReviewDiffs",
		}, "review summary was not accepted, so snapshot-scoped diff calls were not made")
		return
	}
	if detail, ok := populatedDetail(reflect.ValueOf(summary)); ok {
		ledger.note("idl.GetWorkspaceReviewSummary", "real", detail)
	}
	snapshot, ok := summary.ReviewSnapshotID.String()
	if !ok || snapshot == "" {
		blockRoot(ledger, "idl.GetWorkspaceReviewSummary", []string{
			"idl.ListWorkspaceReviewFileDiffs", "idl.GetWorkspaceReviewFileContent", "idl.SearchWorkspaceReviewDiffs",
		}, "summary did not return review_snapshot_id")
		return
	}
	fileID := firstReviewFileID(summary.Files)
	if reviewResolution(readReviewSnapshot(t, ctx, desk, ledger, workspace, source, snapshot, fileID)) {
		refreshed, err := desk.GetWorkspaceReviewSummary(ctx, desktop.GetWorkspaceReviewSummaryReq{Workspace: workspace, Source: source})
		if err != nil {
			recordCall(t, ledger, "idl.GetWorkspaceReviewSummary", err)
			return
		}
		next, ok := refreshed.ReviewSnapshotID.String()
		if !ok || next == "" {
			return
		}
		readReviewSnapshot(t, ctx, desk, ledger, workspace, source, next, firstReviewFileID(refreshed.Files))
	}
}

func firstReviewFileID(files []desktop.WorkspaceReviewFileView) string {
	if len(files) == 0 {
		return ""
	}
	id, ok := files[0].FileID.String()
	if !ok {
		return ""
	}
	return id
}

func readReviewSnapshot(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string, source *desktop.WorkspaceReviewSourceView, snapshot, fileID string) error {
	t.Helper()
	diffs, err := desk.ListWorkspaceReviewFileDiffs(ctx, desktop.ListWorkspaceReviewFileDiffsReq{
		Workspace: workspace, Source: source, ReviewSnapshotID: snapshot,
	})
	if err != nil {
		recordCall(t, ledger, "idl.ListWorkspaceReviewFileDiffs", err)
	} else if got, idOK := diffs.ReviewSnapshotID.String(); !idOK || got != snapshot {
		t.Fatalf("diff snapshot %q", got)
	} else {
		ledger.note("idl.ListWorkspaceReviewFileDiffs", "real", "snapshot "+snapshot+" diffs "+itoaLen(len(diffs.Diffs)))
	}
	tracked := false
	searched, searchErr := desk.SearchWorkspaceReviewDiffs(ctx, desktop.SearchWorkspaceReviewDiffsReq{
		Workspace: workspace, Source: source, ReviewSnapshotID: snapshot, Query: "batch", IncludeUntrackedFiles: &tracked,
	})
	if searchErr != nil {
		recordCall(t, ledger, "idl.SearchWorkspaceReviewDiffs", searchErr)
	} else if got, idOK := searched.ReviewSnapshotID.String(); !idOK || got != snapshot {
		t.Fatalf("search snapshot %q", got)
	} else {
		ledger.note("idl.SearchWorkspaceReviewDiffs", "real", "snapshot "+snapshot)
	}
	if fileID == "" {
		ledger.note("idl.GetWorkspaceReviewFileContent", "notrun", "review summary listed no file id")
		return searchErr
	}
	content, err := desk.GetWorkspaceReviewFileContent(ctx, desktop.GetWorkspaceReviewFileContentReq{
		Workspace: workspace, Source: source, ReviewSnapshotID: snapshot, FileID: fileID, Side: "new",
	})
	if err != nil {
		recordCall(t, ledger, "idl.GetWorkspaceReviewFileContent", err)
		if searchErr != nil {
			return searchErr
		}
		return err
	}
	if got, idOK := content.FileID.String(); !idOK || got != fileID {
		t.Fatalf("content file %q", got)
	}
	ledger.note("idl.GetWorkspaceReviewFileContent", "real", "file id "+fileID+" side new")
	return searchErr
}

func reviewResolution(err error) bool {
	var statusErr *desktop.StatusError
	return errors.As(err, &statusErr) && strings.Contains(string(statusErr.Body), "WORKSPACE_REVIEW_RESOLUTION_FAILED")
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
		ledger.note("idl.GetAgentLegacyHistoryNotice", "notrun", "builtin "+agent+" returned no session_id; the read did not modify the agent")
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
	for _, id := range []string{"idl.PrepareCanvasFiles", "idl.ApplyCanvasOperation", "idl.RemoveCanvasImageBackground", "idl.EnhanceCanvasImage"} {
		ledger.note(id, "notrun", "canvas mutation kind and image node id were not recovered from 0.7.8. Image edit was not called")
	}
	for _, id := range []string{
		"idl.ReportSkillEvolveSignal", "idl.CancelSkillEvolveSignal", "idl.ReportSkillEvolveProposal", "idl.CancelSkillEvolveProposal",
		"idl.MarkSkillEvolveProposalActed", "idl.GetSkillEvolveProposal", "idl.TriggerSkillEvolve", "idl.RequestSkillEvolveScan",
		"idl.RevertSkillEvolve", "idl.MutateSkillEvolveLifecycle", "idl.InstallSkill", "idl.PreviewSkill",
	} {
		ledger.note(id, "notrun", "skill-evolve body_json and install URL were not recovered. No invented body or external URL was sent")
	}
	for _, id := range []string{
		"idl.ReplyQuestionnaire", "idl.DismissQuestionnaire", "idl.GetQuestionnaire", "idl.RecordPluginRecommendationDecline",
		"idl.ReplyQuestion", "idl.RejectQuestion", "idl.BeginQuestionnaire", "idl.ReplaceRootSession", "idl.RequestRotation", "idl.RequestCompaction",
		"idl.CreateImportedAgent", "idl.ReadClaudeCodeAgentImport", "idl.ReadCodexAgentImport", "idl.CreateCron",
	} {
		ledger.note(id, "notrun", "needs a live questionnaire, question, import document, or compaction turn. Builtin agent state was not modified")
	}
	for _, id := range []string{
		"idl.UpsertLocalMcpRuntimeServer", "idl.GetLocalMcpRuntimeTool", "idl.CallLocalMcpRuntimeTool",
	} {
		ledger.note(id, "notrun", "the stdio server command true exposed no tool name, so runtime upsert and tool call were not invented")
	}
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
	ledger.note("idl.RemoveManagedWorkspaceGitWorktree", "notrun", "the worktree created in this run was removed with RemoveWorkspaceGitWorktree; no second managed worktree was left to remove")
	ledger.note("idl.RemoveManagedWorkspaceGitWorktreesBatch", "notrun", "no batch of managed worktrees was created")
	ledger.note("idl.RestoreSessionWorktree", "notrun", "needs a session worktree. Session lifecycle is owned by the parent tester and was not repeated")
	ledger.note("idl.InvalidateSessionFileSyncSources", "notrun", "QuerySessionFileSyncSources returned no source object to invalidate")
	ledger.note("idl.PutSandboxConfig", "notrun", "sandbox status modes were not mapped onto SandboxConfig enums, so the current config was not rewritten")
	ledger.note("negative.unknown-desktop-path", "notrun", "covered by TestLiveSurface RawCall when that test runs; this batch did not repeat it")
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
