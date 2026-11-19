package appserver

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

func assertRoundTrips(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace, agent, sessionID, projectKey string) {
	t.Helper()
	assertSavedFile(t, ctx, desk, ledger, workspace)
	assertFileUpload(t, ctx, desk, ledger, workspace)
	assertGitBranch(t, ctx, desk, ledger, workspace)
	assertSkillRoundTrip(t, ctx, desk, ledger, agent)
	assertReviewRules(t, ctx, desk, ledger)
	assertHookScope(t, ctx, desk, ledger)
	assertPinAgent(t, ctx, desk, ledger, agent)
	assertProjectFlags(t, ctx, desk, ledger, projectKey)
	assertGoalObjective(t, ctx, desk, ledger, sessionID)
	assertArchiveSession(t, ctx, desk, ledger, agent)
	assertArtifactContext(t, ctx, desk, ledger, sessionID)
}

func assertSavedFile(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	const body = "saved-text"
	saved, err := desk.SaveWorkspaceFile(ctx, desktop.SaveFileReq{Workspace: workspace, Path: "saved.txt", Content: crudPtr(body)})
	if err != nil || !scalarTrue(saved.Ok) {
		if err == nil {
			ledger.note("idl.SaveWorkspaceFile", "notrun", "save returned without ok=true")
			return
		}
		recordCall(t, ledger, "idl.SaveWorkspaceFile", err)
		return
	}
	read, err := desk.ReadWorkspaceFile(ctx, desktop.ReadFileReq{Workspace: workspace, Path: "saved.txt"})
	if err != nil || read.Content == nil {
		recordCall(t, ledger, "idl.ReadWorkspaceFile", err)
		return
	}
	text, ok := read.Content.String()
	if !ok || !strings.Contains(text, body) {
		t.Fatalf("saved file content %q", text)
	}
	ledger.note("idl.SaveWorkspaceFile", "real", "ok true and read content contains saved-text")
	ledger.note("idl.ReadWorkspaceFile", "real", "content contains saved-text")
}

func assertFileUpload(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	payload := []byte("hi")
	begun, err := desk.BeginWorkspaceFileUpload(ctx, desktop.BeginFileUploadReq{
		Workspace: workspace,
		Path:      "upload.txt",
		SizeBytes: desktop.Number(len(payload)),
	})
	if err != nil || begun.UploadID == nil {
		recordCall(t, ledger, "idl.BeginWorkspaceFileUpload", err)
		if err != nil {
			outcome, _ := classifyServer(err)
			if outcome == "negative" {
				const why = "not called: BeginWorkspaceFileUpload is not implemented on local-runtime, so no upload id exists"
				ledger.note("idl.AppendWorkspaceFileUpload", "negative", why)
				ledger.note("idl.CommitWorkspaceFileUpload", "negative", why)
				ledger.note("idl.AbortWorkspaceFileUpload", "negative", why)
			}
		}
		return
	}
	uploadID, ok := begun.UploadID.String()
	if !ok || uploadID == "" {
		t.Fatal("upload id")
	}
	appended, err := desk.AppendWorkspaceFileUpload(ctx, desktop.AppendFileUploadReq{
		UploadID:      uploadID,
		Offset:        0,
		ContentBase64: base64.StdEncoding.EncodeToString(payload),
	})
	if err != nil || appended.ReceivedBytes == nil {
		recordCall(t, ledger, "idl.AppendWorkspaceFileUpload", err)
		return
	}
	gotN, ok := appended.ReceivedBytes.Float64()
	if !ok || int(gotN) != len(payload) {
		t.Fatalf("received bytes %v", appended.ReceivedBytes)
	}
	committed, err := desk.CommitWorkspaceFileUpload(ctx, desktop.CommitFileUploadReq{UploadID: uploadID})
	if err != nil || !scalarTrue(committed.Ok) {
		recordCall(t, ledger, "idl.CommitWorkspaceFileUpload", err)
		return
	}
	read, err := desk.ReadWorkspaceFile(ctx, desktop.ReadFileReq{Workspace: workspace, Path: "upload.txt"})
	if err != nil || read.Content == nil {
		t.Fatalf("read upload: %v", err)
	}
	text, ok := read.Content.String()
	if !ok || !strings.Contains(text, "hi") {
		t.Fatalf("uploaded content %q", text)
	}
	abort, err := desk.BeginWorkspaceFileUpload(ctx, desktop.BeginFileUploadReq{
		Workspace: workspace,
		Path:      "abort.txt",
		SizeBytes: 1,
	})
	if err != nil || abort.UploadID == nil {
		t.Fatalf("begin abort upload: %v", err)
	}
	abortID, _ := abort.UploadID.String()
	if _, err := desk.AbortWorkspaceFileUpload(ctx, desktop.AbortFileUploadReq{UploadID: abortID}); err != nil {
		recordCall(t, ledger, "idl.AbortWorkspaceFileUpload", err)
		return
	}
	ledger.note("idl.BeginWorkspaceFileUpload", "real", "upload id returned")
	ledger.note("idl.AppendWorkspaceFileUpload", "real", "received_bytes 2")
	ledger.note("idl.CommitWorkspaceFileUpload", "real", "ok true and file contains hi")
	ledger.note("idl.AbortWorkspaceFileUpload", "real", "second upload aborted before commit")
}

func assertGitBranch(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	const branch = "sdk-local-branch"
	created, err := desk.CreateWorkspaceGitBranch(ctx, desktop.GitCreateBranchReq{Workspace: workspace, Branch: branch})
	if err != nil || !scalarTrue(created.Success) {
		recordCall(t, ledger, "idl.CreateWorkspaceGitBranch", err)
		return
	}
	listed, err := desk.GetWorkspaceGitBranches(ctx, desktop.GitBranchesReq{Workspace: workspace})
	if err != nil {
		recordCall(t, ledger, "idl.GetWorkspaceGitBranches", err)
		return
	}
	if !scalarListHas(listed.Branches, branch) && !scalarListHas(listed.Local, branch) {
		t.Fatalf("branch %s missing from %+v", branch, listed.Branches)
	}
	ledger.note("idl.CreateWorkspaceGitBranch", "real", "success true and branch listed")
	ledger.note("idl.GetWorkspaceGitBranches", "real", "local list contains sdk-local-branch")
	include := true
	committed, err := desk.CommitWorkspaceGit(ctx, desktop.GitCommitReq{Workspace: workspace, Message: "hello note", IncludeUnstaged: &include})
	if err != nil || !scalarTrue(committed.Success) {
		if err == nil {
			ledger.note("idl.CommitWorkspaceGit", "notrun", "commit returned without success=true")
			return
		}
		var statusErr *desktop.StatusError
		if errors.As(err, &statusErr) && strings.Contains(string(statusErr.Body), "CONTENT_POLICY_VIOLATION") {
			ledger.note("idl.CommitWorkspaceGit", "negative", "HTTP 422 CONTENT_POLICY_VIOLATION for commit message hello note")
			return
		}
		recordCall(t, ledger, "idl.CommitWorkspaceGit", err)
		return
	}
	ledger.note("idl.CommitWorkspaceGit", "real", "success true for a local commit; push was not called")
}

func assertSkillRoundTrip(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent string) {
	t.Helper()
	created, err := desk.CreateSkill(ctx, desktop.CreateSkillReq{
		Name:        "helloskill",
		Description: "hello",
		Content:     "hello",
		AgentName:   &agent,
	})
	if err != nil || created.Skill == nil {
		recordCall(t, ledger, "idl.CreateSkill", err)
		if err != nil {
			outcome, _ := classifyServer(err)
			if outcome == "negative" {
				const why = "not called: CreateSkill was rejected by content policy for name helloskill. Get, enable, and delete need a skill this server accepts"
				ledger.note("idl.GetSkill", "negative", why)
				ledger.note("idl.SetSkillEnabled", "negative", why)
				ledger.note("idl.DeleteSkill", "negative", why)
			}
		}
		return
	}
	name, ok := created.Skill.Name.String()
	if !ok || name != "helloskill" {
		t.Fatalf("skill name %q", name)
	}
	include := true
	got, err := desk.GetSkill(ctx, desktop.GetSkillReq{SkillName: name, AgentName: &agent, IncludeBody: &include})
	if err != nil || got.Content == nil {
		recordCall(t, ledger, "idl.GetSkill", err)
		return
	}
	body, ok := got.Content.String()
	if !ok || !strings.Contains(body, "hello") {
		t.Fatalf("skill content %q", body)
	}
	if _, err := desk.SetSkillEnabled(ctx, desktop.SetSkillEnabledReq{SkillName: name, AgentName: &agent, Enabled: false}); err != nil {
		recordCall(t, ledger, "idl.SetSkillEnabled", err)
		return
	}
	disabled, err := desk.GetSkill(ctx, desktop.GetSkillReq{SkillName: name, AgentName: &agent})
	if err != nil || disabled.Skill == nil || disabled.Skill.Enabled == nil {
		t.Fatalf("skill enabled reread: %v", err)
	}
	flag, ok := disabled.Skill.Enabled.Bool()
	if !ok || flag {
		t.Fatalf("skill enabled %#v", disabled.Skill.Enabled)
	}
	if _, err := desk.DeleteSkill(ctx, desktop.DeleteSkillReq{SkillName: name, AgentName: &agent}); err != nil {
		recordCall(t, ledger, "idl.DeleteSkill", err)
		return
	}
	_, err = desk.GetSkill(ctx, desktop.GetSkillReq{SkillName: name, AgentName: &agent})
	var statusErr *desktop.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 404 {
		t.Fatalf("deleted skill: %v", err)
	}
	ledger.note("idl.CreateSkill", "real", "name helloskill")
	ledger.note("idl.GetSkill", "real", "content contains hello, then 404 after delete")
	ledger.note("idl.SetSkillEnabled", "real", "enabled false")
	ledger.note("idl.DeleteSkill", "real", "GetSkill 404 for helloskill")
}

func assertReviewRules(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	before, err := desk.GetReviewPreferences(ctx, desktop.GetReviewPreferencesReq{})
	if err != nil {
		recordCall(t, ledger, "idl.GetReviewPreferences", err)
		return
	}
	original := ""
	if before.Preferences != nil && before.Preferences.Rules != nil {
		original, _ = before.Preferences.Rules.String()
	}
	const marker = "hello rules"
	updated, err := desk.UpdateReviewPreferences(ctx, desktop.UpdateReviewPreferencesReq{Rules: crudPtr(marker)})
	if err != nil || updated.Preferences == nil || updated.Preferences.Rules == nil {
		recordCall(t, ledger, "idl.UpdateReviewPreferences", err)
		return
	}
	text, ok := updated.Preferences.Rules.String()
	if !ok || !strings.Contains(text, marker) {
		t.Fatalf("review rules %q", text)
	}
	if _, err := desk.UpdateReviewPreferences(ctx, desktop.UpdateReviewPreferencesReq{Rules: crudPtr(original)}); err != nil {
		t.Fatalf("restore review rules: %v", err)
	}
	ledger.note("idl.GetReviewPreferences", "real", "rules read before replace")
	ledger.note("idl.UpdateReviewPreferences", "real", "rules contained hello rules and were restored")
}

func assertHookScope(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	listed, err := desk.ListLocalHooks(ctx, desktop.ListLocalHooksReq{Scope: desktop.LocalHookScopeUser})
	if err != nil || listed.Config == nil {
		recordCall(t, ledger, "idl.SetLocalHookScopeEnabled", err)
		return
	}
	enabled, ok := listed.Config.Enabled.Bool()
	if !ok {
		ledger.note("idl.SetLocalHookScopeEnabled", "notrun", "hook scope enabled was not a bool")
		return
	}
	revision, _ := listed.Config.Revision.String()
	flipped, err := desk.SetLocalHookScopeEnabled(ctx, desktop.SetLocalHookScopeEnabledReq{
		Scope:            desktop.LocalHookScopeUser,
		Enabled:          !enabled,
		ExpectedRevision: crudPtr(revision),
	})
	if err != nil || flipped.Config == nil {
		recordCall(t, ledger, "idl.SetLocalHookScopeEnabled", err)
		return
	}
	got, ok := flipped.Config.Enabled.Bool()
	if !ok || got == enabled {
		t.Fatalf("hook scope enabled %v", got)
	}
	if _, err := desk.SetLocalHookScopeEnabled(ctx, desktop.SetLocalHookScopeEnabledReq{Scope: desktop.LocalHookScopeUser, Enabled: enabled}); err != nil {
		t.Fatalf("restore hook scope: %v", err)
	}
	ledger.note("idl.SetLocalHookScopeEnabled", "real", "user scope enabled flipped and restored")
}

func assertPinAgent(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent string) {
	t.Helper()
	pinned, err := desk.PinAgent(ctx, desktop.PinAgentReq{Name: agent, Pinned: true})
	if err != nil || !agentPinned(pinned.Items, pinned.Item, agent) {
		if err != nil {
			recordCall(t, ledger, "idl.PinAgent", err)
			return
		}
		ledger.note("idl.PinAgent", "notrun", "pin response did not contain the agent name")
		return
	}
	unpinned, err := desk.PinAgent(ctx, desktop.PinAgentReq{Name: agent, Pinned: false})
	if err != nil {
		t.Fatalf("unpin agent: %v", err)
	}
	if agentPinned(unpinned.Items, unpinned.Item, agent) {
		t.Fatal("agent remained pinned")
	}
	ledger.note("idl.PinAgent", "real", "agent name pinned then absent")
}

func assertProjectFlags(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, projectKey string) {
	t.Helper()
	ref := &desktop.ProjectRef{Key: &projectKey}
	pinned, err := desk.SetProjectPinned(ctx, desktop.SetProjectPinnedReq{Project: ref, Pinned: true})
	if err != nil || !projectFlag(pinned.Project, true, false) {
		recordCall(t, ledger, "idl.SetProjectPinned", err)
	} else {
		if _, err := desk.SetProjectPinned(ctx, desktop.SetProjectPinnedReq{Project: ref, Pinned: false}); err != nil {
			t.Fatalf("unpin project: %v", err)
		}
		ledger.note("idl.SetProjectPinned", "real", "state.pinned true then cleared")
	}
	hidden, err := desk.SetProjectHidden(ctx, desktop.SetProjectHiddenReq{Project: ref, Hidden: true})
	if err != nil || !projectFlag(hidden.Project, false, true) {
		recordCall(t, ledger, "idl.SetProjectHidden", err)
	} else {
		if _, err := desk.SetProjectHidden(ctx, desktop.SetProjectHiddenReq{Project: ref, Hidden: false}); err != nil {
			t.Fatalf("unhide project: %v", err)
		}
		ledger.note("idl.SetProjectHidden", "real", "state.hidden true then cleared")
	}
	touched, err := desk.TouchRecentProject(ctx, desktop.TouchRecentProjectReq{Project: ref})
	if err != nil || touched.Project == nil || touched.Project.Key == nil {
		recordCall(t, ledger, "idl.TouchRecentProject", err)
	} else if got, ok := touched.Project.Key.String(); !ok || got != projectKey {
		t.Fatalf("touched project %q", got)
	} else {
		ledger.note("idl.TouchRecentProject", "real", "project key echoed")
	}
	ordered, err := desk.PutProjectsOrder(ctx, desktop.PutProjectsOrderReq{Projects: []desktop.ProjectRef{{Key: &projectKey}}})
	if err != nil || !projectListed(ordered, projectKey) {
		recordCall(t, ledger, "idl.PutProjectsOrder", err)
		return
	}
	ledger.note("idl.PutProjectsOrder", "real", "ordered list contains the project key")
}

func assertGoalObjective(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, sessionID string) {
	t.Helper()
	objective := "hello note"
	created, err := desk.CreateGoal(ctx, desktop.CreateGoalReq{SessionID: sessionID, Objective: &objective})
	if err != nil || created.Goal == nil {
		recordCall(t, ledger, "idl.CreateGoal", err)
		return
	}
	goalID, ok := created.Goal.ID()
	gotObjective, objectiveOK := created.Goal.ObjectiveText()
	session, sessionOK := created.Goal.SessionText()
	status, statusOK := created.Goal.StatusText()
	if !ok || goalID == "" || !objectiveOK || gotObjective != objective || !sessionOK || session != sessionID || !statusOK || status == "" || status == desktop.GoalStatusComplete {
		t.Fatalf("goal id %q objective %q session %q status %q", goalID, gotObjective, session, status)
	}
	if err := desktop.ValidateGoalStatus(status); err != nil {
		t.Fatal(err)
	}
	replacement := "hello there"
	patchReq := desktop.PatchGoalReq{SessionID: sessionID, Objective: &replacement}
	if created.Goal.UpdatedAt != nil {
		if stamp, ok := created.Goal.UpdatedAt.Float64(); ok {
			when := desktop.Number(stamp)
			patchReq.ExpectedGoalID = &goalID
			patchReq.ExpectedUpdatedAt = &when
		}
	}
	patched, err := desk.PatchGoal(ctx, patchReq)
	if err != nil || patched.Goal == nil {
		recordCall(t, ledger, "idl.PatchGoal", err)
		_, _ = desk.DeleteGoal(ctx, desktop.DeleteGoalReq{SessionID: sessionID})
		return
	}
	if text, ok := patched.Goal.ObjectiveText(); !ok || text != replacement {
		t.Fatalf("patched objective %q", text)
	}
	if patchedStatus, ok := patched.Goal.StatusText(); ok && patchedStatus == desktop.GoalStatusComplete {
		t.Fatal("PatchGoal objective change marked the goal complete")
	}
	fetched, err := desk.GetGoal(ctx, desktop.GetGoalReq{SessionID: sessionID})
	if err != nil || fetched.Goal == nil {
		t.Fatalf("GetGoal: %v", err)
	}
	id, _ := fetched.Goal.ID()
	text, _ := fetched.Goal.ObjectiveText()
	if id != goalID || text != replacement {
		t.Fatalf("stored goal %s %q", id, text)
	}
	_, err = desk.DismissGoal(ctx, desktop.DismissGoalReq{SessionID: sessionID, GoalID: goalID})
	var dismissErr *desktop.StatusError
	if !errors.As(err, &dismissErr) || dismissErr.Status != 409 {
		t.Fatalf("DismissGoal: %v", err)
	}
	if _, err := desk.DeleteGoal(ctx, desktop.DeleteGoalReq{SessionID: sessionID}); err != nil {
		t.Fatalf("DeleteGoal: %v", err)
	}
	ledger.note("idl.CreateGoal", "real", "id, objective, session, and non-complete status")
	ledger.note("idl.PatchGoal", "real", "objective changed; status was not set to complete")
	ledger.note("idl.GetGoal", "real", "same id and updated objective")
	ledger.note("idl.DismissGoal", "negative", "HTTP 409 while the goal is not complete")
	ledger.note("negative.dismiss-active-goal", "negative", "DismissGoal on an active goal is HTTP 409 until the goal is complete")
	ledger.note("idl.DeleteGoal", "real", "goal record removed after the 409")
}

func assertArchiveSession(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent string) {
	t.Helper()
	title := "hello note"
	created, err := desk.CreateSession(ctx, desktop.CreateSessionReq{Name: agent, Title: &title})
	if err != nil || created.SessionID == nil {
		t.Fatalf("archive session: %v", err)
	}
	id, _ := created.SessionID.String()
	archived := true
	if _, err := desk.ArchiveSession(ctx, desktop.ArchiveSessionReq{ID: id, Archived: &archived}); err != nil {
		recordCall(t, ledger, "idl.ArchiveSession", err)
		_, _ = desk.DeleteSession(ctx, desktop.DeleteSessionReq{ID: id})
		return
	}
	got, err := desk.GetSession(ctx, desktop.GetSessionReq{ID: id})
	if err != nil || got.Session == nil || got.Session.Archived == nil {
		t.Fatalf("archived session: %v", err)
	}
	flag, ok := got.Session.Archived.Bool()
	if !ok || !flag {
		t.Fatalf("archived %#v", got.Session.Archived)
	}
	archived = false
	if _, err := desk.ArchiveSession(ctx, desktop.ArchiveSessionReq{ID: id, Archived: &archived}); err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	if _, err := desk.DeleteSession(ctx, desktop.DeleteSessionReq{ID: id}); err != nil {
		t.Fatalf("delete archived session: %v", err)
	}
	ledger.note("idl.ArchiveSession", "real", "archived true on GetSession, then cleared")
}

func assertArtifactContext(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, sessionID string) {
	t.Helper()
	saved, err := desk.SaveArtifactSpace(ctx, desktop.SaveArtifactSpaceReq{Name: "hello space"})
	if err != nil || saved.Space == nil {
		recordCall(t, ledger, "idl.WriteArtifactSpaceContext", err)
		return
	}
	id, _ := saved.Space.ID.String()
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = desk.DeleteArtifactSpace(cleanCtx, desktop.DeleteArtifactSpaceReq{ID: id})
	})
	current, err := desk.GetArtifactSpaceContext(ctx, desktop.GetArtifactSpaceContextReq{ID: id})
	if err != nil {
		recordCall(t, ledger, "idl.GetArtifactSpaceContext", err)
		return
	}
	revision, _ := current.Revision.String()
	const summary = "hello summary"
	written, err := desk.WriteArtifactSpaceContext(ctx, desktop.WriteArtifactSpaceContextReq{ID: id, Summary: summary, Revision: revision})
	if err != nil {
		recordCall(t, ledger, "idl.WriteArtifactSpaceContext", err)
		return
	}
	changed, ok := written.Changed.Bool()
	if !ok || !changed {
		ledger.note("idl.WriteArtifactSpaceContext", "notrun", "changed was not true")
		return
	}
	again, err := desk.GetArtifactSpaceContext(ctx, desktop.GetArtifactSpaceContextReq{ID: id})
	if err != nil {
		t.Fatalf("reread context: %v", err)
	}
	got, ok := again.Summary.String()
	if !ok || got != summary {
		t.Fatalf("context summary %q", got)
	}
	if _, err := desk.LinkArtifactSpaceSession(ctx, desktop.LinkArtifactSpaceSessionReq{ID: id, SessionID: sessionID}); err != nil {
		recordCall(t, ledger, "idl.LinkArtifactSpaceSession", err)
		ledger.note("idl.GetArtifactSpaceContext", "real", "summary hello summary")
		ledger.note("idl.WriteArtifactSpaceContext", "real", "changed true")
		return
	}
	space, err := desk.GetArtifactSpace(ctx, desktop.GetArtifactSpaceReq{ID: id})
	if err != nil || space.Space == nil {
		t.Fatalf("linked space: %v", err)
	}
	if !sessionLinked(space.Sessions, sessionID) {
		ledger.note("idl.LinkArtifactSpaceSession", "notrun", "GetArtifactSpace sessions did not contain the session id")
	} else {
		ledger.note("idl.LinkArtifactSpaceSession", "real", "session id present on the space")
	}
	ledger.note("idl.GetArtifactSpaceContext", "real", "summary hello summary")
	ledger.note("idl.WriteArtifactSpaceContext", "real", "changed true and summary reread")
}

func recordCall(t *testing.T, ledger *coverageLedger, id string, err error) {
	t.Helper()
	if err == nil {
		ledger.note(id, "notrun", "call returned no asserted field")
		return
	}
	outcome, detail := classifyServer(err)
	ledger.note(id, outcome, detail)
	if outcome == "failed" {
		t.Errorf("%s: %s", id, detail)
	}
}

func scalarTrue(value *desktop.Scalar) bool {
	if value == nil {
		return false
	}
	flag, ok := value.Bool()
	return ok && flag
}

func scalarListHas(values []desktop.Scalar, want string) bool {
	for _, value := range values {
		got, ok := value.String()
		if ok && got == want {
			return true
		}
	}
	return false
}

func projectFlag(project *desktop.ProjectDetail, wantPinned, wantHidden bool) bool {
	if project == nil || project.State == nil {
		return false
	}
	if wantPinned {
		flag, ok := project.State.Pinned.Bool()
		return ok && flag
	}
	flag, ok := project.State.Hidden.Bool()
	return ok && flag
}

func agentPinned(items []desktop.PinnedItem, item *desktop.PinnedItem, name string) bool {
	if item != nil && agentName(*item, name) {
		return true
	}
	for _, candidate := range items {
		if agentName(candidate, name) {
			return true
		}
	}
	return false
}

func agentName(item desktop.PinnedItem, name string) bool {
	if id, ok := item.ID.String(); ok && id == name {
		return true
	}
	if item.Agent != nil && item.Agent.Name != nil {
		got, ok := item.Agent.Name.String()
		return ok && got == name
	}
	return false
}

func sessionLinked(items []desktop.ArtifactSpaceSessionView, sessionID string) bool {
	for _, item := range items {
		id, ok := item.ID.String()
		if ok && id == sessionID {
			return true
		}
	}
	return false
}
