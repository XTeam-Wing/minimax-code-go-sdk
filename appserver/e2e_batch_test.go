package appserver

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/XTeam-Wing/minimax-code-go-sdk/desktop"
	"github.com/XTeam-Wing/minimax-code-go-sdk/mavis"
)

// TestLiveLocalBatch calls safe local app-server methods on one process.
// A typed 2xx with a decoded body is a real read. HTTP errors are not
// successes. The outcome file is written outside the repository.
func TestLiveLocalBatch(t *testing.T) {
	client, workspace := startLive(t, "desktop")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	desk := client.Desktop()
	ledger := newCoverageLedger()
	defer func() {
		path, err := ledger.write()
		if err != nil {
			t.Errorf("coverage report: %v", err)
			return
		}
		t.Logf("coverage report %s", path)
	}()

	agents, err := desk.ListAgents(ctx, desktop.ListAgentsReq{})
	if err != nil || len(agents.Agents) == 0 {
		t.Fatalf("ListAgents: %v", err)
	}
	ledger.note("idl.ListAgents", "real", "non-empty agent list")
	agent := ""
	for _, item := range agents.Agents {
		if item.Name == nil {
			continue
		}
		name, ok := item.Name.String()
		if ok && name != "" {
			agent = name
			if name == "mavis" {
				break
			}
		}
	}
	if agent == "" {
		t.Fatal("no agent name")
	}
	gotAgent, err := desk.GetAgent(ctx, desktop.GetAgentReq{Name: agent})
	if err != nil || gotAgent.Agent == nil {
		t.Fatalf("GetAgent: %v", err)
	}
	ledger.note("idl.GetAgent", "real", "agent object returned")

	marker := workspace + "/batch.txt"
	if err := os.WriteFile(marker, []byte("batch"), 0o600); err != nil {
		t.Fatal(err)
	}
	sweepReads(t, ctx, desk, ledger, workspace, agent)
	assertLocalWrites(t, ctx, desk, ledger, workspace)
	assertMavisReads(t, ctx, desk, ledger, workspace)

	for _, method := range desktop.MethodCatalog() {
		id := "idl." + method.Name
		if _, ok := findNote(ledger, id); ok {
			continue
		}
		switch {
		case method.GatewayOverride:
			ledger.note(id, "blocked", "drive gateway can publish or mutate a remote site")
		case method.SideEffect == "auth_credential":
			ledger.note(id, "blocked", "writes or reads a credential; not called with a real key")
		case method.SideEffect == "approval":
			ledger.note(id, "blocked", "permission reply is not auto-approved")
		case method.Transport == "sse" && method.Name != "SubscribeGlobalEvents":
			ledger.note(id, "blocked", "stream would hold the server; opened only for the global event test")
		default:
			ledger.note(id, "notrun", "not called in the local batch")
		}
	}
	ledger.note("rpc.initialize", "real", "Start completed the handshake")
	ledger.note("rpc.initialized", "real", "Start sent initialized")
}

func sweepReads(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace, agent string) {
	t.Helper()
	byName := map[string]desktop.MethodInfo{}
	for _, method := range desktop.MethodCatalog() {
		byName[method.Name] = method
	}
	clientType := reflect.TypeOf(desk)
	for i := 0; i < clientType.NumMethod(); i++ {
		method := clientType.Method(i)
		info, ok := byName[method.Name]
		if !ok || info.HTTPMethod != "GET" || info.Transport != "metadataOnly" || info.GatewayOverride {
			continue
		}
		if info.SideEffect != "read" && info.SideEffect != "" {
			continue
		}
		if method.Type.NumIn() != 3 || method.Type.NumOut() != 2 {
			continue
		}
		reqType := method.Type.In(2)
		if reqType.Kind() != reflect.Struct {
			continue
		}
		req := reflect.New(reqType).Elem()
		fillLocalRead(req, workspace, agent)
		if !pathFilled(info.Path, req) {
			ledger.note("idl."+method.Name, "notrun", "path parameter was not synthesized")
			continue
		}
		out := method.Func.Call([]reflect.Value{reflect.ValueOf(desk), reflect.ValueOf(ctx), req})
		id := "idl." + method.Name
		if !out[1].IsNil() {
			err := out[1].Interface().(error)
			if strings.Contains(err.Error(), "missing required") || strings.Contains(err.Error(), "must be") {
				ledger.note(id, "notrun", "request could not be formed without a synthesized resource")
				continue
			}
			ledger.note(id, "failed", err.Error())
			continue
		}
		ledger.note(id, "real", "GET decoded")
	}
}

func fillLocalRead(req reflect.Value, workspace, agent string) {
	for i := 0; i < req.NumField(); i++ {
		field := req.Field(i)
		name := req.Type().Field(i).Name
		if !field.CanSet() {
			continue
		}
		switch field.Type() {
		case reflect.TypeOf(desktop.LocalHookScope(0)):
			field.Set(reflect.ValueOf(desktop.LocalHookScopeUser))
		case reflect.TypeOf(""):
			switch name {
			case "Workspace":
				field.SetString(workspace)
			case "Name":
				field.SetString(agent)
			case "Path":
				field.SetString("batch.txt")
			}
		}
	}
}

func assertLocalWrites(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	before, err := desk.GetGlobalInstructions(ctx, desktop.GetGlobalInstructionsReq{})
	if err != nil {
		t.Fatalf("GetGlobalInstructions: %v", err)
	}
	original, _ := before.Content.String()
	const marker = "sdk-local-instruction"
	put, err := desk.PutGlobalInstructions(ctx, desktop.PutGlobalInstructionsReq{Content: marker})
	if err != nil {
		t.Fatalf("PutGlobalInstructions: %v", err)
	}
	if text, ok := put.Content.String(); !ok || text != marker {
		t.Fatalf("put instructions %q", text)
	}
	again, err := desk.GetGlobalInstructions(ctx, desktop.GetGlobalInstructionsReq{})
	if err != nil {
		t.Fatalf("GetGlobalInstructions after put: %v", err)
	}
	if text, ok := again.Content.String(); !ok || text != marker {
		t.Fatalf("stored instructions %q", text)
	}
	if _, err := desk.PutGlobalInstructions(ctx, desktop.PutGlobalInstructionsReq{Content: original}); err != nil {
		t.Fatalf("restore instructions: %v", err)
	}
	ledger.note("idl.GetGlobalInstructions", "real", "content round-tripped")
	ledger.note("idl.PutGlobalInstructions", "real", "content replaced and restored")

	mode, err := desk.GetPermissionMode(ctx, desktop.GetPermissionModeReq{})
	if err != nil {
		t.Fatalf("GetPermissionMode: %v", err)
	}
	current, ok := mode.Mode.String()
	if !ok || current == "" {
		t.Fatal("permission mode empty")
	}
	set, err := desk.SetPermissionMode(ctx, desktop.SetPermissionModeReq{Mode: current})
	if err != nil {
		t.Fatalf("SetPermissionMode: %v", err)
	}
	if got, ok := set.Mode.String(); !ok || got != current {
		t.Fatalf("permission mode %q", got)
	}
	ledger.note("idl.GetPermissionMode", "real", "mode string")
	ledger.note("idl.SetPermissionMode", "real", "same mode written back")

	index, err := desk.GetWorkspaceIndexingPreference(ctx, desktop.GetWorkspaceIndexingPreferenceReq{})
	if err != nil {
		t.Fatalf("GetWorkspaceIndexingPreference: %v", err)
	}
	enabled, ok := index.Enabled.Bool()
	if !ok {
		t.Fatal("indexing enabled is not a bool")
	}
	flipped, err := desk.PutWorkspaceIndexingPreference(ctx, desktop.PutWorkspaceIndexingPreferenceReq{Enabled: !enabled})
	if err != nil {
		t.Fatalf("PutWorkspaceIndexingPreference: %v", err)
	}
	if got, ok := flipped.Enabled.Bool(); !ok || got == enabled {
		t.Fatalf("indexing did not change: %v", got)
	}
	if _, err := desk.PutWorkspaceIndexingPreference(ctx, desktop.PutWorkspaceIndexingPreferenceReq{Enabled: enabled}); err != nil {
		t.Fatalf("restore indexing: %v", err)
	}
	ledger.note("idl.GetWorkspaceIndexingPreference", "real", "bool read")
	ledger.note("idl.PutWorkspaceIndexingPreference", "real", "toggled and restored")

	hooks, err := desk.ListLocalHooks(ctx, desktop.ListLocalHooksReq{Scope: desktop.LocalHookScopeUser})
	if err != nil || hooks.Config == nil {
		t.Fatalf("ListLocalHooks: %v", err)
	}
	revision, ok := hooks.Config.Revision.String()
	if !ok {
		t.Fatal("hook revision")
	}
	name := "sdkhook"
	created, err := desk.CreateLocalHook(ctx, desktop.CreateLocalHookReq{
		Scope:            desktop.LocalHookScopeUser,
		ExpectedRevision: revision,
		Definition: &desktop.LocalHookDefinition{
			Event:   "SessionStart",
			Command: "true",
			Name:    &name,
		},
	})
	if err != nil {
		t.Fatalf("CreateLocalHook: %v", err)
	}
	hookID, ok := created.HookID.String()
	if !ok || hookID == "" {
		t.Fatal("hook id")
	}
	nextRevision := revision
	if created.Config != nil {
		if rev, ok := created.Config.Revision.String(); ok && rev != "" {
			nextRevision = rev
		}
	}
	found := false
	for _, item := range created.Hooks {
		id, ok := item.HookID.String()
		if ok && id == hookID {
			found = true
		}
	}
	if !found {
		t.Fatal("created hook was not in the returned list")
	}
	if _, err := desk.DeleteLocalHook(ctx, desktop.DeleteLocalHookReq{
		HookID:           hookID,
		Scope:            desktop.LocalHookScopeUser,
		ExpectedRevision: nextRevision,
	}); err != nil {
		t.Fatalf("DeleteLocalHook: %v", err)
	}
	ledger.note("idl.ListLocalHooks", "real", "user scope config")
	ledger.note("idl.CreateLocalHook", "real", "hook id returned and listed")
	ledger.note("idl.DeleteLocalHook", "real", "hook deleted")

	display := "sdk local project"
	createdProject, err := desk.CreateProject(ctx, desktop.CreateProjectReq{
		DisplayName:  display,
		WorkspaceDir: &workspace,
	})
	if err != nil || createdProject.Project == nil {
		t.Fatalf("CreateProject: %v", err)
	}
	key, ok := createdProject.Project.Key.String()
	if !ok || key == "" {
		t.Fatal("project key")
	}
	if got, ok := createdProject.Project.DisplayName.String(); !ok || got != display {
		t.Fatalf("project name %q", got)
	}
	listed, err := desk.ListProjects(ctx, desktop.ListProjectsReq{})
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if !projectListed(listed, key) {
		t.Fatal("ListProjects missed the created project")
	}
	renamed := "sdk local project renamed"
	projectRef := &desktop.ProjectRef{Key: &key}
	updated, err := desk.UpdateProject(ctx, desktop.UpdateProjectReq{Project: projectRef, DisplayName: &renamed})
	if err != nil || updated.Project == nil || updated.Project.DisplayName == nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	if got, ok := updated.Project.DisplayName.String(); !ok || got != renamed {
		t.Fatalf("updated project %q", got)
	}
	archived := true
	_, err = desk.UpdateProject(ctx, desktop.UpdateProjectReq{Project: projectRef, Archived: &archived})
	var statusErr *desktop.StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 400 || !strings.Contains(string(statusErr.Body), "UNSUPPORTED_PROJECT_UPDATE") {
		t.Fatalf("archive project: %v", err)
	}
	ledger.note("idl.CreateProject", "real", "project key returned")
	ledger.note("idl.ListProjects", "real", "created key listed")
	ledger.note("idl.UpdateProject", "real", "display name changed")
	ledger.note("idl.UpdateProject.archive", "negative", "HTTP 400 UNSUPPORTED_PROJECT_UPDATE")
}

func pathFilled(path string, req reflect.Value) bool {
	for _, part := range strings.Split(path, "/") {
		if !strings.HasPrefix(part, ":") {
			continue
		}
		name := exportWire(part[1:])
		field := req.FieldByName(name)
		if !field.IsValid() || field.Kind() != reflect.String || field.String() == "" {
			return false
		}
	}
	return true
}

func exportWire(wire string) string {
	parts := strings.Split(wire, "_")
	var b strings.Builder
	for _, part := range parts {
		switch part {
		case "id":
			b.WriteString("ID")
		case "url":
			b.WriteString("URL")
		case "json":
			b.WriteString("JSON")
		default:
			if part == "" {
				continue
			}
			b.WriteString(strings.ToUpper(part[:1]))
			b.WriteString(part[1:])
		}
	}
	return b.String()
}

func assertMavisReads(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace string) {
	t.Helper()
	mv := mavis.New(desk)
	health, err := mv.Health(ctx)
	if err != nil || health.Status == "" {
		t.Fatalf("mavis health: %v", err)
	}
	ledger.note("mavis.health", "real", health.Status)
	version, err := mv.Version(ctx)
	if err != nil || version.Version == "" {
		t.Fatalf("mavis version: %v", err)
	}
	ledger.note("mavis.version", "real", version.Version)
	status, err := mv.RuntimeStatusTyped(ctx)
	if err != nil || status.Status == "" {
		t.Fatalf("runtime status: %v", err)
	}
	ledger.note("mavis.runtime.status", "real", status.Status)
	caps, err := mv.RuntimeCapabilities(ctx)
	if err != nil || !jsonObject(caps.Raw) {
		t.Fatalf("runtime capabilities: %v", err)
	}
	ledger.note("mavis.runtime.capabilities", "real", "json object")
	cfg, err := mv.GetConfig(ctx)
	if err != nil || !jsonObject(cfg.Raw) {
		t.Fatalf("config: %v", err)
	}
	ledger.note("mavis.config", "real", "json object")
	info, err := mv.FsGitInfo(ctx, workspace)
	if err != nil {
		t.Fatalf("fs git info: %v", err)
	}
	ledger.note("mavis.fs.git-info", "real", "three booleans decoded")
	_ = info
	dirs, err := mv.ListFsDirs(ctx, mavis.FsDirsQuery{})
	if err != nil || !jsonObject(dirs.Raw) {
		t.Fatalf("fs dirs: %v", err)
	}
	ledger.note("mavis.fs.dirs", "real", "json object")
}

func projectListed(list desktop.ListProjectsResp, key string) bool {
	for _, group := range list.Projects {
		if group.Project == nil {
			continue
		}
		got, ok := group.Project.Key.String()
		if ok && got == key {
			return true
		}
	}
	return false
}

func jsonObject(raw []byte) bool {
	return len(raw) > 0 && raw[0] == '{'
}

func findNote(l *coverageLedger, id string) (CoverageOutcome, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	row, ok := l.rows[id]
	return row, ok
}
