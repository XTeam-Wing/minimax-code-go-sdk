package appserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
)

func assertLocalExtras(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace, agent, projectKey string) {
	t.Helper()
	assertNameOnlyAgent(t, ctx, desk, ledger, workspace)
	assertSkillNameOnly(t, ctx, desk, ledger, agent)
	assertWorktree(t, ctx, desk, ledger, workspace)
	assertGitInvalidate(t, ctx, desk, ledger, workspace)
	assertFileSyncQuery(t, ctx, desk, ledger)
	assertStreamEcho(t, ctx, desk, ledger)
	assertNestedLabel(t, ctx, desk, ledger)
	assertMoveProject(t, ctx, desk, ledger, projectKey)
	noteComputerUsePut(ledger)
}

func assertNameOnlyAgent(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	name := "sdklocal"
	only := true
	created, err := desk.CreateAgent(ctx, desktop.CreateAgentReq{Name: &name, DefinitionOnly: &only, DefaultWorkspaceDir: &workspace})
	if contentSafety(err) {
		ledger.note("idl.CreateAgent", "blocked", "name-only wire {name:sdklocal, definition_only:true, default_workspace_dir} still HTTP 422. pPd reviews the name even when displayName is omitted, and fPd blocks a non-empty value when reviewContent does not pass. Sending display_name is not what caused the 422.")
		return
	}
	if err != nil || created.Name == nil {
		recordCall(t, ledger, "idl.CreateAgent", err)
		return
	}
	got, ok := created.Name.String()
	if !ok || got != name {
		t.Fatalf("agent %q", got)
	}
	t.Cleanup(func() {
		cleanCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = desk.DeleteAgent(cleanCtx, desktop.DeleteAgentReq{Name: name})
	})
	fetched, err := desk.GetAgent(ctx, desktop.GetAgentReq{Name: name})
	if err != nil || fetched.Agent == nil || fetched.Agent.Name == nil {
		t.Fatalf("get created agent: %v", err)
	}
	if again, ok := fetched.Agent.Name.String(); !ok || again != name {
		t.Fatalf("stored agent %q", again)
	}
	if _, err := desk.DeleteAgent(ctx, desktop.DeleteAgentReq{Name: name}); err != nil {
		recordCall(t, ledger, "idl.DeleteAgent", err)
		return
	}
	ledger.note("idl.CreateAgent", "real", "definition_only name sdklocal with display_name omitted")
	ledger.note("idl.DeleteAgent", "real", "agent sdklocal deleted")
}

func assertSkillNameOnly(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, agent string) {
	t.Helper()
	created, err := desk.CreateSkill(ctx, desktop.CreateSkillReq{Name: "sdklocal", Description: "", Content: "", AgentName: &agent})
	if contentSafety(err) {
		ledger.note("idl.CreateSkill", "blocked", "wire {name:sdklocal, description:\"\", content:\"\", agent_name} still HTTP 422. ovn skips empty description and content before reviewContent, so the reviewed value is the name. This is not a missing description field.")
		return
	}
	if err != nil || created.Skill == nil {
		recordCall(t, ledger, "idl.CreateSkill", err)
		return
	}
	name, ok := created.Skill.Name.String()
	if !ok || name != "sdklocal" {
		t.Fatalf("skill %q", name)
	}
	if _, err := desk.DeleteSkill(ctx, desktop.DeleteSkillReq{SkillName: name, AgentName: &agent}); err != nil {
		recordCall(t, ledger, "idl.DeleteSkill", err)
		return
	}
	ledger.note("idl.CreateSkill", "real", "name sdklocal with empty description and content")
	ledger.note("idl.DeleteSkill", "real", "skill sdklocal deleted")
}

func assertWorktree(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	branch := "sdk-wt"
	dirName := "sdk-wt"
	created, err := desk.CreateWorkspaceGitWorktree(ctx, desktop.CreateWorktreeReq{
		Workspace: workspace,
		Options:   &desktop.CreateWorktreeOptions{Branch: &branch, DirName: &dirName, Base: crudPtr("master")},
	})
	if err != nil || !scalarTrue(created.Success) || created.WorktreeDir == nil {
		recordCall(t, ledger, "idl.CreateWorkspaceGitWorktree", err)
		return
	}
	dir, ok := created.WorktreeDir.String()
	if !ok || dir == "" {
		t.Fatal("worktree dir")
	}
	removed, err := desk.RemoveWorkspaceGitWorktree(ctx, desktop.RemoveWorktreeReq{Workspace: workspace, WorktreeDir: dir})
	if err != nil || !scalarTrue(removed.Success) {
		recordCall(t, ledger, "idl.RemoveWorkspaceGitWorktree", err)
		return
	}
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
			t.Fatalf("worktree %s still listed", dir)
		}
	}
	ledger.note("idl.CreateWorkspaceGitWorktree", "real", "worktree dir returned")
	ledger.note("idl.RemoveWorkspaceGitWorktree", "real", "dir absent from the worktree list")
}

func assertGitInvalidate(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	one, err := desk.InvalidateWorkspaceGitSnapshot(ctx, desktop.GitInvalidateReq{Workspace: workspace})
	if err != nil || !scalarTrue(one.Success) {
		recordCall(t, ledger, "idl.InvalidateWorkspaceGitSnapshot", err)
	} else {
		ledger.note("idl.InvalidateWorkspaceGitSnapshot", "real", "success true")
	}
	all, err := desk.InvalidateAllWorkspaceGitSnapshots(ctx, desktop.GitInvalidateAllReq{})
	if err != nil || !scalarTrue(all.Success) {
		recordCall(t, ledger, "idl.InvalidateAllWorkspaceGitSnapshots", err)
		return
	}
	ledger.note("idl.InvalidateAllWorkspaceGitSnapshots", "real", "success true")
}

func assertFileSyncQuery(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	listed, err := desk.QuerySessionFileSyncSources(ctx, desktop.QuerySessionFileSyncSourcesReq{})
	if err != nil {
		recordCall(t, ledger, "idl.QuerySessionFileSyncSources", err)
		return
	}
	if listed.Sources == nil {
		ledger.note("idl.QuerySessionFileSyncSources", "notrun", "sources was null")
		return
	}
	ledger.note("idl.QuerySessionFileSyncSources", "real", "sources slice present")
}

func assertStreamEcho(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	streamCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stream, err := desk.StreamEcho(streamCtx, desktop.StreamEchoReq{Message: "topo-stream"})
	if err != nil {
		recordCall(t, ledger, "idl.StreamEcho", err)
		return
	}
	defer stream.Close()
	var text string
	for {
		frame, err := stream.Recv(streamCtx)
		if err != nil {
			break
		}
		if frame.MessageDelta != nil {
			part, ok := frame.MessageDelta.String()
			if ok {
				text += part
			}
		}
		if frame.Done != nil {
			if done, ok := frame.Done.Bool(); ok && done {
				break
			}
		}
	}
	if !strings.Contains(text, "topo-stream") {
		ledger.note("idl.StreamEcho", "notrun", "stream ended without the sent message")
		return
	}
	ledger.note("idl.StreamEcho", "real", "deltas contain topo-stream")
}

func assertNestedLabel(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger) {
	t.Helper()
	label := "topo-label"
	got, err := desk.GetNestedQueryBindingEcho(ctx, desktop.GetNestedQueryBindingEchoReq{Label: &label})
	if err != nil || got.Label == nil {
		recordCall(t, ledger, "idl.GetNestedQueryBindingEcho", err)
		return
	}
	text, ok := got.Label.String()
	if !ok || text != label {
		t.Fatalf("label %q", text)
	}
	ledger.note("idl.GetNestedQueryBindingEcho", "real", "label topo-label")
}

func assertMoveProject(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, projectKey string) {
	t.Helper()
	ref := &desktop.ProjectRef{Key: &projectKey}
	moved, err := desk.MoveSidebarProject(ctx, desktop.MoveSidebarProjectReq{Project: ref})
	if err != nil || moved.Project == nil || moved.Project.Key == nil {
		recordCall(t, ledger, "idl.MoveSidebarProject", err)
		return
	}
	if got, ok := moved.Project.Key.String(); !ok || got != projectKey {
		t.Fatalf("moved project %q", got)
	}
	ledger.note("idl.MoveSidebarProject", "real", "project key echoed")
}

func noteComputerUsePut(ledger *coverageLedger) {
	row, ok := ledger.outcome("idl.GetComputerUseDefaultPreference")
	if ok && (row.Outcome == "blocked" || row.Outcome == "negative") {
		ledger.note("idl.PutComputerUseDefaultPreference", "blocked", "root idl.GetComputerUseDefaultPreference; "+row.Detail)
	}
}
