package appserver

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
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
		path, err := ledger.writeInventory()
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
	if err := os.WriteFile(workspace+"/preview.png", onePixelPNG(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := initTempGit(t, workspace); err != nil {
		t.Fatalf("git init: %v", err)
	}
	title := "batch session"
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
		reason := "batch cleanup"
		_, _ = desk.AbortSession(abortCtx, desktop.AbortSessionReq{ID: sessionID, Reason: &reason})
	})
	ledger.note("idl.CreateSession", "real", "session id returned")
	project, err := desk.CreateProject(ctx, desktop.CreateProjectReq{DisplayName: "sdk batch project", WorkspaceDir: &workspace})
	if err != nil || project.Project == nil {
		t.Fatalf("CreateProject: %v", err)
	}
	projectKey, ok := project.Project.Key.String()
	if !ok || projectKey == "" {
		t.Fatal("project key")
	}
	ledger.note("idl.CreateProject", "real", "project key returned")
	sweepReads(t, ctx, desk, ledger, workspace, agent, sessionID, projectKey)
	assertLocalWrites(t, ctx, desk, ledger, workspace, projectKey)
	assertLocalCRUD(t, ctx, client, desk, ledger, workspace, agent, sessionID, projectKey)
	assertMavisReads(t, ctx, desk, ledger, workspace)
	noteUpstreamGaps(ledger)
	ledger.note("request.initialize", "real", "Start completed the handshake")
	ledger.note("client-notification.initialized", "real", "Start sent initialized")
}

func sweepReads(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace, agent, sessionID, projectKey string) {
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
		fillLocalRead(method.Name, info.Path, req, workspace, agent, sessionID, projectKey)
		if !pathFilled(info.Path, req) {
			ledger.note("idl."+method.Name, "notrun", "path parameter was not synthesized")
			continue
		}
		if field, empty := desktop.EmptyRequired(method.Name, req.Interface()); empty {
			ledger.note("idl."+method.Name, "notrun", "required "+field+" was not synthesized")
			continue
		}
		out := method.Func.Call([]reflect.Value{reflect.ValueOf(desk), reflect.ValueOf(ctx), req})
		id := "idl." + method.Name
		if !out[1].IsNil() {
			err := out[1].Interface().(error)
			outcome, detail := classifyServer(err)
			ledger.note(id, outcome, detail)
			continue
		}
		detail, ok := populatedDetail(out[0])
		if !ok {
			ledger.note(id, "notrun", "response decoded with no populated typed field")
			continue
		}
		ledger.note(id, "real", detail)
	}
}

func classifyServer(err error) (string, string) {
	var statusErr *desktop.StatusError
	if !errors.As(err, &statusErr) {
		return "failed", err.Error()
	}
	body := string(statusErr.Body)
	switch {
	case statusErr.Status == 501 || strings.Contains(body, "NOT_IMPLEMENTED") || strings.Contains(body, "not implemented"):
		return "negative", "HTTP 501 the local runtime does not implement this IDL method"
	case statusErr.Status == 404 && strings.Contains(body, "route not found"):
		return "negative", "IDL path is not mounted on local-runtime"
	case statusErr.Status == 404 && (strings.Contains(body, "UNAVAILABLE") || strings.Contains(body, "required")):
		return "blocked", "HTTP 404 " + clip(body, 160)
	case statusErr.Status == 401 || statusErr.Status == 403 || statusErr.Status == 503:
		return "blocked", "HTTP " + strconv.Itoa(statusErr.Status) + " " + clip(body, 160)
	case strings.Contains(body, "CONTENT_POLICY_VIOLATION"):
		return "negative", "HTTP " + strconv.Itoa(statusErr.Status) + " CONTENT_POLICY_VIOLATION " + clip(body, 160)
	case statusErr.Status == 409:
		return "negative", "HTTP 409"
	case statusErr.Status == 500:
		return "negative", "HTTP 500 on a request whose required fields were set"
	default:
		return "failed", err.Error()
	}
}

func initTempGit(t *testing.T, dir string) error {
	t.Helper()
	run := func(args ...string) error {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=sdk", "GIT_AUTHOR_EMAIL=sdk@example.com", "GIT_COMMITTER_NAME=sdk", "GIT_COMMITTER_EMAIL=sdk@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return errors.New(strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := run("init"); err != nil {
		return err
	}
	if err := run("add", "batch.txt"); err != nil {
		return err
	}
	return run("commit", "-m", "batch")
}

func fillLocalRead(method, httpPath string, req reflect.Value, workspace, agent, sessionID, projectKey string) {
	for i := 0; i < req.NumField(); i++ {
		field := req.Field(i)
		name := req.Type().Field(i).Name
		if !field.CanSet() {
			continue
		}
		switch name {
		case "Workspace", "WorkspaceDir":
			setStringField(field, workspace)
		case "Name", "AgentName":
			setStringField(field, agent)
		case "SessionID":
			setStringField(field, sessionID)
		case "ProjectKey":
			setStringField(field, projectKey)
		case "Query":
			setStringField(field, "main")
		case "ID":
			if strings.Contains(httpPath, "/session/:id") {
				setStringField(field, sessionID)
			}
		}
		if field.Type() == reflect.TypeOf(desktop.LocalHookScope(0)) {
			field.Set(reflect.ValueOf(desktop.LocalHookScopeUser))
		}
	}
	if _, empty := desktop.EmptyRequired(method, req.Interface()); empty {
		filePath := "batch.txt"
		if method == "PreviewWorkspaceFile" {
			filePath = filepath.Join(workspace, "preview.png")
		}
		if field := req.FieldByName("Path"); field.IsValid() && field.CanSet() {
			setStringField(field, filePath)
		}
	}
}

func clip(text string, n int) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= n {
		return text
	}
	return text[:n]
}

func setStringField(field reflect.Value, value string) {
	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Pointer:
		if field.Type().Elem().Kind() == reflect.String {
			copied := value
			field.Set(reflect.ValueOf(&copied))
		}
	}
}

func onePixelPNG() []byte {
	return []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x03, 0x00, 0x01, 0x00, 0x05, 0xfe,
		0xd4, 0xef, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
		0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
}

func assertLocalWrites(t *testing.T, ctx context.Context, desk *desktop.Client, ledger *coverageLedger, workspace, projectKey string) {
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
	renamedCommand := "true"
	updatedHook, err := desk.UpdateLocalHook(ctx, desktop.UpdateLocalHookReq{
		HookID:           hookID,
		Scope:            desktop.LocalHookScopeUser,
		ExpectedRevision: nextRevision,
		Definition: &desktop.LocalHookDefinition{
			Event:   "SessionStart",
			Command: renamedCommand,
			Name:    &name,
		},
	})
	if err != nil {
		t.Fatalf("UpdateLocalHook: %v", err)
	}
	if !hookCommand(updatedHook.Hooks, hookID, renamedCommand) {
		t.Fatal("updated hook command was not returned")
	}
	if updatedHook.Config != nil {
		if rev, ok := updatedHook.Config.Revision.String(); ok && rev != "" {
			nextRevision = rev
		}
	}
	disabled, err := desk.SetLocalHookEnabled(ctx, desktop.SetLocalHookEnabledReq{
		HookID:           hookID,
		Scope:            desktop.LocalHookScopeUser,
		Enabled:          false,
		ExpectedRevision: &nextRevision,
	})
	if err != nil {
		t.Fatalf("SetLocalHookEnabled: %v", err)
	}
	if !hookEnabled(disabled.Hooks, hookID, false) {
		t.Fatal("hook enabled flag did not become false")
	}
	if disabled.Config != nil {
		if rev, ok := disabled.Config.Revision.String(); ok && rev != "" {
			nextRevision = rev
		}
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
	ledger.note("idl.UpdateLocalHook", "real", "command updated to true")
	ledger.note("idl.SetLocalHookEnabled", "real", "enabled became false")
	ledger.note("idl.DeleteLocalHook", "real", "hook deleted")

	key := projectKey
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
	ledger.note("mavis.config.get", "real", "json object")
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

func populatedDetail(v reflect.Value) (string, bool) {
	return populated(v, 0)
}

func populated(v reflect.Value, depth int) (string, bool) {
	if !v.IsValid() || depth > 4 {
		return "", false
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return "", false
		}
		return populated(v.Elem(), depth+1)
	case reflect.Slice, reflect.Map:
		if v.IsNil() {
			return "", false
		}
		return "len=" + strconv.Itoa(v.Len()), true
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			name := v.Type().Field(i).Name
			if name == "BaseResp" {
				continue
			}
			if detail, ok := populated(field, depth+1); ok {
				return name + " " + detail, true
			}
		}
		return "", false
	default:
		if v.IsZero() {
			return "", false
		}
		return "set", true
	}
}

func hookCommand(hooks []desktop.LocalHookItem, id, command string) bool {
	for _, item := range hooks {
		got, ok := item.HookID.String()
		if !ok || got != id || item.Definition == nil {
			continue
		}
		return item.Definition.Command == command
	}
	return false
}

func hookEnabled(hooks []desktop.LocalHookItem, id string, want bool) bool {
	for _, item := range hooks {
		got, ok := item.HookID.String()
		if !ok || got != id {
			continue
		}
		enabled, ok := item.Enabled.Bool()
		return ok && enabled == want
	}
	return false
}
