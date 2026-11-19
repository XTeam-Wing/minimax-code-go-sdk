package mavis

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
)

// These methods are the dispatcher leaves enumerated from @minimax-ai/code 0.7.8
// (yUi, oTu, aTu, and the lTu routers). They are not part of the 470 IDL methods.
// Paths under /minimax-desktop/api/v1/drive are the same website gateway and stay
// on desktop.Client. A route the dispatcher forwards to an unlisted handler is
// still available through Raw.

// RuntimeStatus is the object returned by GET /mavis/api/runtime/status.
type RuntimeStatus struct {
	Status       string          `json:"status"`
	Runtime      string          `json:"runtime"`
	Mode         json.RawMessage `json:"mode,omitempty"`
	OwnerKind    json.RawMessage `json:"ownerKind,omitempty"`
	Capabilities json.RawMessage `json:"capabilities,omitempty"`
	Surfaces     json.RawMessage `json:"surfaces,omitempty"`
	DataDir      string          `json:"dataDir,omitempty"`
	StartupToken string          `json:"startupToken,omitempty"`
	Raw          json.RawMessage `json:"-"`
}

// RuntimeStatus calls GET /mavis/api/runtime/status.
func (c *Client) RuntimeStatusTyped(ctx context.Context) (RuntimeStatus, error) {
	raw, err := c.get(ctx, "/mavis/api/runtime/status")
	if err != nil {
		return RuntimeStatus{}, err
	}
	var out RuntimeStatus
	if err := json.Unmarshal(raw, &out); err != nil {
		return RuntimeStatus{}, err
	}
	out.Raw = raw
	return out, nil
}

// MigrationTotals is the totals object from GET /mavis/api/runtime/migration/check.
type MigrationTotals struct {
	LegacySessions int `json:"legacySessions"`
	Pending        int `json:"pending"`
	Metadata       int `json:"metadata"`
	Migrated       int `json:"migrated"`
	Failed         int `json:"failed"`
	Deleted        int `json:"deleted"`
	Records        int `json:"records"`
}

// MigrationCheck is the migration check document. Nested blobs that the handler
// forwards unchanged stay raw.
type MigrationCheck struct {
	Mode         json.RawMessage `json:"mode,omitempty"`
	LegacyImport bool            `json:"legacyImport"`
	AgentName    string          `json:"agentName,omitempty"`
	Totals       MigrationTotals `json:"totals"`
	Failed       json.RawMessage `json:"failed,omitempty"`
	Persistence  json.RawMessage `json:"persistence,omitempty"`
	Messages     json.RawMessage `json:"messages,omitempty"`
	Records      json.RawMessage `json:"records,omitempty"`
	Raw          json.RawMessage `json:"-"`
}

// RuntimeMigrationCheckTyped calls GET /mavis/api/runtime/migration/check.
func (c *Client) RuntimeMigrationCheckTyped(ctx context.Context, q MigrationCheckQuery) (MigrationCheck, error) {
	doc, err := c.RuntimeMigrationCheck(ctx, q)
	if err != nil {
		return MigrationCheck{}, err
	}
	var out MigrationCheck
	if err := json.Unmarshal(doc.Raw, &out); err != nil {
		return MigrationCheck{}, err
	}
	out.Raw = doc.Raw
	return out, nil
}

// PermissionRequests calls GET /mavis/api/permission/requests.
func (c *Client) PermissionRequests(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/permission/requests")
}

// DismissPermissionRequest calls DELETE /mavis/api/permission/requests/{id}.
func (c *Client) DismissPermissionRequest(ctx context.Context, requestID string) (Ack, error) {
	return c.ack(ctx, "DELETE", "/mavis/api/permission/requests/"+url.PathEscape(requestID), nil)
}

// PermissionDecision is the batch-reply decision the handler accepts.
type PermissionDecision string

const (
	PermissionAllowAlways PermissionDecision = "allowAlways"
	PermissionAllowOnce   PermissionDecision = "allowOnce"
	PermissionDeny        PermissionDecision = "deny"
)

// BatchReply is POST /mavis/api/permission/batch-reply.
type BatchReply struct {
	Decision   PermissionDecision `json:"decision"`
	RequestIDs []string           `json:"requestIds"`
}

// BatchReplyResult is the handler's success body.
type BatchReplyResult struct {
	Processed json.RawMessage `json:"processed"`
	Skipped   json.RawMessage `json:"skipped"`
	Raw       json.RawMessage `json:"-"`
}

// ReplyPermissions calls POST /mavis/api/permission/batch-reply.
// The zero Decision is not sent as allow; an empty decision is rejected
// before the request is written.
func (c *Client) ReplyPermissions(ctx context.Context, body BatchReply) (BatchReplyResult, error) {
	switch body.Decision {
	case PermissionAllowAlways, PermissionAllowOnce, PermissionDeny:
	default:
		return BatchReplyResult{}, errDecision
	}
	rawBody, err := json.Marshal(body)
	if err != nil {
		return BatchReplyResult{}, err
	}
	raw, err := c.call(ctx, "POST", "/mavis/api/permission/batch-reply", rawBody)
	if err != nil {
		return BatchReplyResult{}, err
	}
	var out BatchReplyResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return BatchReplyResult{}, err
	}
	out.Raw = raw
	return out, nil
}

// PermissionRulesQuery is the query string for GET /mavis/api/permission/rules.
type PermissionRulesQuery struct {
	Source    string
	AgentName string
	SessionID string
}

// PermissionRules calls GET /mavis/api/permission/rules.
func (c *Client) PermissionRules(ctx context.Context, q PermissionRulesQuery) (RawDocument, error) {
	v := url.Values{}
	set(v, "source", q.Source)
	set(v, "agentName", q.AgentName)
	set(v, "sessionId", q.SessionID)
	return c.getDoc(ctx, withQuery("/mavis/api/permission/rules", v))
}

// PermissionCheck is POST /mavis/api/permission/check.
type PermissionCheck struct {
	ToolName  string          `json:"tool_name"`
	Input     json.RawMessage `json:"input,omitempty"`
	SessionID string          `json:"session_id,omitempty"`
	AgentName string          `json:"agent_name,omitempty"`
}

// CheckPermission calls POST /mavis/api/permission/check.
func (c *Client) CheckPermission(ctx context.Context, body PermissionCheck) (RawDocument, error) {
	if body.ToolName == "" {
		return RawDocument{}, errTool
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return RawDocument{}, err
	}
	return c.callDoc(ctx, "POST", "/mavis/api/permission/check", raw)
}

// ContentReview is POST /mavis/api/content.
type ContentReview struct {
	Content     string          `json:"content"`
	Attachments json.RawMessage `json:"attachments,omitempty"`
}

// ReviewContent calls POST /mavis/api/content.
func (c *Client) ReviewContent(ctx context.Context, body ContentReview) (RawDocument, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return RawDocument{}, err
	}
	return c.callDoc(ctx, "POST", "/mavis/api/content", raw)
}

// ImageReview is POST /mavis/api/content/image. image_url must be http(s) and
// scene must be a positive integer; the handler enforces both.
type ImageReview struct {
	ImageURL string `json:"image_url"`
	Scene    int    `json:"scene"`
}

// ReviewImage calls POST /mavis/api/content/image.
func (c *Client) ReviewImage(ctx context.Context, body ImageReview) (RawDocument, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return RawDocument{}, err
	}
	return c.callDoc(ctx, "POST", "/mavis/api/content/image", raw)
}

// ReviewText calls POST /mavis/api/content/text.
func (c *Client) ReviewText(ctx context.Context, body json.RawMessage) (RawDocument, error) {
	return c.callDoc(ctx, "POST", "/mavis/api/content/text", body)
}

// BrowserStatus calls GET /mavis/api/browser/status.
func (c *Client) BrowserStatus(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/browser/status")
}

// BrowserInstallInfo calls GET /mavis/api/browser/install-info.
func (c *Client) BrowserInstallInfo(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/browser/install-info")
}

// BrowserProfiles calls GET /mavis/api/browser/profiles.
func (c *Client) BrowserProfiles(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/browser/profiles")
}

// BrowserTools calls GET /mavis/api/browser/tools.
func (c *Client) BrowserTools(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/browser/tools")
}

// BrowserExtensionPath calls GET /mavis/api/browser/extension-path.
func (c *Client) BrowserExtensionPath(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/browser/extension-path")
}

// BrowserSessions calls GET /mavis/api/browser/sessions.
func (c *Client) BrowserSessions(ctx context.Context) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/browser/sessions")
}

// CommunicationSend is POST /mavis/api/communication/send.
type CommunicationSend struct {
	FromSession string `json:"fromSession"`
	ToSession   string `json:"toSession,omitempty"`
	Command     string `json:"command"`
	Content     string `json:"content,omitempty"`
}

// SendCommunication calls POST /mavis/api/communication/send.
func (c *Client) SendCommunication(ctx context.Context, body CommunicationSend) (RawDocument, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return RawDocument{}, err
	}
	return c.callDoc(ctx, "POST", "/mavis/api/communication/send", raw)
}

// CommunicationMessages calls GET /mavis/api/communication/messages.
func (c *Client) CommunicationMessages(ctx context.Context, q url.Values) (RawDocument, error) {
	return c.getDoc(ctx, withQuery("/mavis/api/communication/messages", q))
}

// CommunicationPeers calls GET /mavis/api/communication/peers.
func (c *Client) CommunicationPeers(ctx context.Context, q url.Values) (RawDocument, error) {
	return c.getDoc(ctx, withQuery("/mavis/api/communication/peers", q))
}

// CronTask is one task from GET /mavis/api/cron, using the handler's field names.
type CronTask struct {
	CronName   string          `json:"cronName"`
	AgentName  string          `json:"agentName"`
	CronID     string          `json:"cronId,omitempty"`
	Schedule   json.RawMessage `json:"schedule,omitempty"`
	Timezone   json.RawMessage `json:"timezone,omitempty"`
	Enabled    bool            `json:"enabled"`
	Prompt     json.RawMessage `json:"prompt,omitempty"`
	Session    json.RawMessage `json:"session,omitempty"`
	Delivery   json.RawMessage `json:"delivery,omitempty"`
	Status     json.RawMessage `json:"status,omitempty"`
	LastRun    json.RawMessage `json:"lastRun,omitempty"`
	LastResult json.RawMessage `json:"lastResult,omitempty"`
	LastError  json.RawMessage `json:"lastError,omitempty"`
	NextRun    json.RawMessage `json:"nextRun,omitempty"`
}

// CronList is GET /mavis/api/cron.
type CronList struct {
	Tasks []CronTask      `json:"tasks"`
	Count int             `json:"count"`
	Raw   json.RawMessage `json:"-"`
}

// ListCron calls GET /mavis/api/cron. This is the legacy /mavis router, not the IDL cron methods.
func (c *Client) ListCron(ctx context.Context) (CronList, error) {
	raw, err := c.get(ctx, "/mavis/api/cron")
	if err != nil {
		return CronList{}, err
	}
	var out CronList
	if err := json.Unmarshal(raw, &out); err != nil {
		return CronList{}, err
	}
	out.Raw = raw
	return out, nil
}

// CronSession is one row from GET /mavis/api/cron/sessions.
type CronSession struct {
	ID        string `json:"id"`
	AgentID   string `json:"agentId"`
	Purpose   string `json:"purpose"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

// ListCronSessions calls GET /mavis/api/cron/sessions.
func (c *Client) ListCronSessions(ctx context.Context, limit int) (RawDocument, error) {
	v := url.Values{}
	if limit > 0 {
		v.Set("limit", strconv.Itoa(limit))
	}
	return c.getDoc(ctx, withQuery("/mavis/api/cron/sessions", v))
}

// MatrixImage is one images_understand image_info entry.
type MatrixImage struct {
	Data     string `json:"data"`
	MimeType string `json:"mime_type,omitempty"`
	Prompt   string `json:"prompt,omitempty"`
}

// UnderstandImages calls POST /mavis/api/matrix for tool images_understand.
func (c *Client) UnderstandImages(ctx context.Context, images []MatrixImage) (RawDocument, error) {
	body, err := json.Marshal(map[string]any{
		"tool": "images_understand",
		"arguments": map[string]any{
			"image_info": images,
		},
	})
	if err != nil {
		return RawDocument{}, err
	}
	return c.callDoc(ctx, "POST", "/mavis/api/matrix", body)
}

// UsageQuery is the query string for GET /mavis/api/usage.
type UsageQuery struct {
	From      string
	To        string
	Group     string
	AgentName string
}

// UsageSummaryTyped calls GET /mavis/api/usage/summary.
func (c *Client) UsageSummaryTyped(ctx context.Context, q UsageQuery) (RawDocument, error) {
	return c.getDoc(ctx, withQuery("/mavis/api/usage/summary", usageValues(q)))
}

// UsageTyped calls GET /mavis/api/usage.
func (c *Client) UsageTyped(ctx context.Context, q UsageQuery) (RawDocument, error) {
	return c.getDoc(ctx, withQuery("/mavis/api/usage", usageValues(q)))
}

func usageValues(q UsageQuery) url.Values {
	v := url.Values{}
	set(v, "from", q.From)
	set(v, "to", q.To)
	set(v, "group", q.Group)
	set(v, "agentName", q.AgentName)
	return v
}

// FsDirsQuery is GET /mavis/api/fs/dirs.
type FsDirsQuery struct {
	Directory    string
	Query        string
	MaxDepth     int
	Offset       int
	Limit        int
	HideDotFiles *bool
}

// ListFsDirs calls GET /mavis/api/fs/dirs.
func (c *Client) ListFsDirs(ctx context.Context, q FsDirsQuery) (RawDocument, error) {
	v := url.Values{}
	set(v, "directory", q.Directory)
	set(v, "query", q.Query)
	if q.MaxDepth > 0 {
		v.Set("maxdepth", strconv.Itoa(q.MaxDepth))
	}
	if q.Offset > 0 {
		v.Set("offset", strconv.Itoa(q.Offset))
	}
	if q.Limit > 0 {
		v.Set("limit", strconv.Itoa(q.Limit))
	}
	if q.HideDotFiles != nil {
		v.Set("hideDotFiles", strconv.FormatBool(*q.HideDotFiles))
	}
	return c.getDoc(ctx, withQuery("/mavis/api/fs/dirs", v))
}

// FsGitInfo is GET /mavis/api/fs/git-info.
type FsGitInfo struct {
	IsGitWorkspace   bool            `json:"isGitWorkspace"`
	IsInsideWorktree bool            `json:"isInsideWorktree"`
	HasHead          bool            `json:"hasHead"`
	Raw              json.RawMessage `json:"-"`
}

// FsGitInfo calls GET /mavis/api/fs/git-info.
func (c *Client) FsGitInfo(ctx context.Context, path string) (FsGitInfo, error) {
	raw, err := c.get(ctx, withQuery("/mavis/api/fs/git-info", url.Values{"path": {path}}))
	if err != nil {
		return FsGitInfo{}, err
	}
	var out FsGitInfo
	if err := json.Unmarshal(raw, &out); err != nil {
		return FsGitInfo{}, err
	}
	out.Raw = raw
	return out, nil
}

// WorkspaceQuery is the workspace and optional path query used by /mavis/api/file.
type WorkspaceQuery struct {
	Workspace string
	Path      string
	Extra     url.Values
}

// FileTree calls GET /mavis/api/file/tree.
func (c *Client) FileTree(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "tree", q)
}

// FileInfo calls GET /mavis/api/file/info.
func (c *Client) FileInfo(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "info", q)
}

// FileContent calls GET /mavis/api/file/content.
func (c *Client) FileContent(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "content", q)
}

// FileThumbnail calls GET /mavis/api/file/thumbnail.
func (c *Client) FileThumbnail(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "thumbnail", q)
}

// FileMedia calls GET /mavis/api/file/media.
func (c *Client) FileMedia(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "media", q)
}

// FilePreview calls GET /mavis/api/file/preview.
func (c *Client) FilePreview(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "preview", q)
}

// FileDiff calls GET /mavis/api/file/diff.
func (c *Client) FileDiff(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "diff", q)
}

// FileDiffContext calls GET /mavis/api/file/diff-context.
func (c *Client) FileDiffContext(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "diff-context", q)
}

// FileDiffs calls GET /mavis/api/file/diffs.
func (c *Client) FileDiffs(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "diffs", q)
}

// FileStatus calls GET /mavis/api/file/status.
func (c *Client) FileStatus(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "status", q)
}

// FileSearch calls GET /mavis/api/file/search.
func (c *Client) FileSearch(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "search", q)
}

// FileOpenApp calls GET /mavis/api/file/open-app.
func (c *Client) FileOpenApp(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "open-app", q)
}

// FileIdeApps calls GET /mavis/api/file/ide-apps.
func (c *Client) FileIdeApps(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "ide-apps", q)
}

// GitProbe calls GET /mavis/api/file/git/probe.
func (c *Client) GitProbe(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "git/probe", q)
}

// GitChanges calls GET /mavis/api/file/git/changes.
func (c *Client) GitChanges(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "git/changes", q)
}

// GitMetadata calls GET /mavis/api/file/git/metadata.
func (c *Client) GitMetadata(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "git/metadata", q)
}

// GitBranches calls GET /mavis/api/file/git/branches.
func (c *Client) GitBranches(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "git/branches", q)
}

// GitBranchPicker calls GET /mavis/api/file/git/branch-picker.
func (c *Client) GitBranchPicker(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "git/branch-picker", q)
}

// GitDefaultBranch calls GET /mavis/api/file/git/default-branch.
func (c *Client) GitDefaultBranch(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "git/default-branch", q)
}

// GitWorktrees calls GET /mavis/api/file/git/worktrees.
func (c *Client) GitWorktrees(ctx context.Context, q WorkspaceQuery) (RawDocument, error) {
	return c.fileGet(ctx, "git/worktrees", q)
}

// DebugSessionHistory calls GET /mavis/api/debug/session/{id}/pi-history.jsonl.
func (c *Client) DebugSessionHistory(ctx context.Context, sessionID string) (RawDocument, error) {
	return c.getDoc(ctx, "/mavis/api/debug/session/"+url.PathEscape(sessionID)+"/pi-history.jsonl")
}

// DiagnosticsUpload calls POST /mavis/api/diagnostics/upload.
func (c *Client) DiagnosticsUpload(ctx context.Context, body json.RawMessage) (RawDocument, error) {
	return c.callDoc(ctx, "POST", "/mavis/api/diagnostics/upload", body)
}

// PermissionModeOptionsTyped calls GET /mavis/api/config/permission-mode-options.
func (c *Client) PermissionModeOptionsTyped(ctx context.Context) (RawDocument, error) {
	return c.PermissionModeOptions(ctx)
}

// DiscoverLLMProviders calls GET /mavis/api/external/llm-provider-discovery.
func (c *Client) DiscoverLLMProviders(ctx context.Context) (RawDocument, error) {
	return c.LLMProviderDiscovery(ctx)
}

func (c *Client) fileGet(ctx context.Context, leaf string, q WorkspaceQuery) (RawDocument, error) {
	v := url.Values{}
	set(v, "workspace", q.Workspace)
	set(v, "path", q.Path)
	for key, values := range q.Extra {
		for _, value := range values {
			v.Add(key, value)
		}
	}
	return c.getDoc(ctx, withQuery("/mavis/api/file/"+leaf, v))
}

// Ack is a handler body that is exactly {"success":true} plus any extra fields.
type Ack struct {
	Success bool            `json:"success"`
	Raw     json.RawMessage `json:"-"`
}

func (c *Client) ack(ctx context.Context, method, path string, body []byte) (Ack, error) {
	raw, err := c.call(ctx, method, path, body)
	if err != nil {
		return Ack{}, err
	}
	var out Ack
	if err := json.Unmarshal(raw, &out); err != nil {
		return Ack{}, err
	}
	out.Raw = raw
	return out, nil
}

func withQuery(path string, v url.Values) string {
	if len(v) == 0 {
		return path
	}
	return path + "?" + v.Encode()
}

func set(v url.Values, key, value string) {
	if value != "" {
		v.Set(key, value)
	}
}
