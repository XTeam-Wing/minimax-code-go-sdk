# MiniMax Code Go SDK

A Go client for the **MiniMax Code app-server** shipped in `@minimax-ai/code` **0.7.8**.

The client speaks MiniMax's JSON-RPC session and the DesktopService tunnel inside it. It is not a wrapper around `mcode exec`, and it does not speak the OpenAI Codex app-server wire protocol. The Codex app-server guide is only the model for how this document is organized: connect, lifecycle, methods, events, approvals, errors, examples.

Module: `github.com/XTeam-Wing/minimax-code-go-sdk`

## Install

```bash
go get github.com/XTeam-Wing/minimax-code-go-sdk@latest
```

The SDK does not vendor the CLI. Install `@minimax-ai/code` yourself (Node.js 22.19+, 24, 25, or 26). The 0.7.8 postinstall script checks `better-sqlite3`; do not skip lifecycle scripts if you want the real server to start.

```bash
npm install @minimax-ai/code@0.7.8
```

Verified tarball SHA-256:

`f497f02bac0f8fc8ad3791d59e8827208684ab466bec94cd7702594d19f5735a`

## Quickstart

`examples/quickstart` starts `mcode app-server --stdio --foreground` in a data directory you choose and lists agents.

```bash
export MCODE_BIN=mcode
export MCODE_DATA_DIR="$PWD/.mcode-data"
export MCODE_WORKSPACE="$PWD"
go run ./examples/quickstart
```

When the executable is `node` and the package entry is `cli.js`:

```bash
export MCODE_BIN=node
export MCODE_LEADING=/path/to/node_modules/@minimax-ai/code/cli.js
go run ./examples/quickstart
```

```go
client, err := appserver.Start(ctx, appserver.Options{
    Command:   "mcode",
    DataDir:   dataDir,
    Workspace: workspace,
    // Leave AuthMode empty. The CLI then uses remote-oauth and a headless
    // API-key provider can answer SendMessage. AuthMode "desktop" is a
    // Desktop lease, not the model provider; without that lease SendMessage
    // returns 503 local_runtime_auth_pending.
})
if err != nil {
    log.Fatal(err)
}
defer client.Close()

agents, err := client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{})
```

`Start` owns the child process. `Close` is safe to call more than once and stops that process. The client does not start an updater, a daemon, or an update loop.

## What you get

| Surface | Coverage |
| --- | --- |
| DesktopService methods | **470 / 470** named methods from the 0.7.8 IDL (`k` + `Yi`), each with a request struct and a response or stream type |
| SSE methods | **8 / 8** (`SendMessage`, `ResumeSession`, `ContinueTurn`, `SubscribeGlobalEvents`, `SubscribeExtensionStream`, `WatchWorkspaceFiles`, `StreamWorkspaceFileSearch`, `StreamEcho`) |
| Outer JSON-RPC requests | **8 / 8**: `initialize`, `runtime/authContext`, `account/read`, `account/login/start`, `account/login/cancel`, `account/logout`, `server/prepareRestart`, `desktopService/transport` |
| Client notifications | **3 / 3**: `initialized`, `runtime/authContext`, `desktopService/transport` |
| Server notifications | **3 / 3**: `account/login/completed`, `account/updated`, `desktopService/transport` |
| JSON-RPC error codes | **11 / 11** |
| Unix HTTP | **2 / 2**: `GET /readyz`, `GET /healthz` (unix listener only) |

The full method table is [docs/method-inventory.md](docs/method-inventory.md). The API guide is [docs/api.md](docs/api.md). A Chinese walkthrough is [docs/getting-started.zh-CN.md](docs/getting-started.zh-CN.md).

`SelectModel` stores a session model only when `session_id` is set. The goal scenario uses `provider_id` `minimax_api`, `model_id` `MiniMax-M3.1-Flash`, and `thinking.effort` `high`. `GetSession` reads those values back from `session.model` and `session.effective_model`. A call without `session_id` changes the process default and does not store thinking. `CreateGoal` has no effort field. `examples/sessionmodel` performs that select and readback and does not send a model turn.

User-workflow tests (`TestLiveWorkflow*`) are separate from the 497 API rows. They require the workspace artifact, the reconnected history, or the verifier verdict. An empty list or HTTP 200 is only evidence for that method. Their report is `MCODE_FEATURE_REPORT`, or `$TMPDIR/mcode-feature-report.json`, and it is not committed.

The 470 methods are the frozen IDL surface. The process also serves `/mavis/api/...` dispatcher leaves (permission, content, browser, communication, cron, matrix, usage, fs, file, git, runtime, diagnostics, and the agent routes `cTu` actually calls). Those are named methods on `mavis.Client`. `GET /mavis/api/agent/{name}` with no further segment is implemented inside `routeResolvedAgent` but `cTu` does not call it, so that info route is not on the client. `mavis.Raw` remains for a path outside that list.

Alias fields are typed only when a 0.7.8 handler shows the JSON value. `PermissionReply` and `BatchPermissionDecision` are the numbers 0, 1, and 2, not the strings `deny` or `allowOnce`. `AgentShellPreference` is the number 0 (`auto`), 1 (`powershell`), or 2 (`git-bash`). A 2xx body whose top-level `base_resp.status_code` is not 0 returns `*desktop.BusinessError` and the decoded response. A missing envelope is not that error. `LocalHookScope` is 1 or 2. `SessionKind` stays `Scalar` because the server accepts either the known strings or the numbers 0 through 5. `CloudCronScheduleKind` and `WorkShareKind` stay `Scalar` because no reader was recovered. Request structs still have those `Scalar` fields, plus three `BaseReq` fields whose wire values were not recovered. Nested and response structs still contain `Scalar` fields wherever the schema says `kind: scalar` and no single `tsType` was recovered. Drive methods return `DriveGatewayResp`: `base_resp.status_code` / `status_msg`, plus `Raw` for the rest of the gateway JSON. The IDL response encoder does not run on those 13 paths. The gateway body beyond `base_resp` is opaque on this path.

`desktop.RawCall` is only the escape hatch for a method that is not in this baseline. It is not a substitute for the 470 named methods.

## Protocol, in one page

1. Connect with stdio (newline-delimited JSON) or a Unix socket (WebSocket on `/`, no `Origin` header).
2. Send `initialize` with `protocolVersion: 1` and DesktopService `transportProtocolVersion: 2`. Both must match exactly.
3. Send the `initialized` notification before any other call.
4. DesktopService calls are HTTP requests inside `desktopService/transport` frames: `request`, then `response-start` / `stream-chunk` / `stream-done` / `stream-error`.
5. SSE methods use a stream window (default 16, max 256). The client returns credits as you read. It does not read ahead of that window.

Codex uses thread and turn methods on the outer session. MiniMax keeps session, permission, file, MCP, and model calls behind the DesktopService tunnel. Do not point this client at `codex app-server`.

## Approvals

A nil handler is not an approval. The SDK never calls `ReplyPermission` or `BatchReplyPermissions` for you. Permission prompts arrive on `SubscribeGlobalEvents` (`permission.ask` is one confirmed type, not a closed list). Your code decides, then calls the reply method.

Lost responses are not retried. Reconnect does not replay a call that might already have run.

## Support

- Go **1.22** or newer.
- Operating systems: stdio on Linux, macOS, and Windows. Unix sockets and `/readyz` need a Unix socket (`GOOS=windows` builds, but `ListenUnix` returns an error).
- CLI baseline: **0.7.8** only. `StrictBaseline` rejects other `serverInfo.version` values. Other versions are unverified, not claimed compatible.
- Dependencies: the standard library only.

## Tests

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
GOOS=windows GOARCH=amd64 go test -c -o /dev/null ./appserver
```

Official app-server end-to-end (not a mock) uses a temporary data directory and workspace:

```bash
MCODE_E2E=1 \
MCODE_BIN=node \
MCODE_LEADING=/path/to/@minimax-ai/code/cli.js \
go test -count=1 -run 'TestLive' ./appserver
```

Regenerate the IDL bindings from the pinned tarball. Category labels come from `internal/inventory/categories.json`.

```bash
python3 tools/gen_desktop.py /path/to/minimax-ai-code-0.7.8.tgz
```

Headless API-key model turns are a separate opt-in. Prepare an isolated data directory, with the key only in the `set-minimax-key` child:

```bash
export MINIMAX_DATA_DIR="$PWD/.mcode-data"
export MCODE_PROVIDER_API_KEY=...   # not an argument, and not committed
mcode provider set-minimax-key --api-key-env MCODE_PROVIDER_API_KEY
mcode provider use api-key
mcode provider test minimax_api --json
```

No-auth protocol tests, with `--auth-mode desktop` and a temporary data directory:

```bash
MCODE_E2E=1 MCODE_BIN=node MCODE_LEADING=/path/to/cli.js \
  go test -count=1 -run 'TestLiveAppServer|TestLiveUnixHealth|TestLiveSurface|TestLiveLocalBatch' ./appserver
```

Model scenarios, only after the API key is stored in an isolated data directory. Leave `AuthMode` empty. `CreateGoal` starts a model turn. A subagent is not counted unless `subagent.execution` reports `started` and `GetSession` reads the child.

```bash
MCODE_E2E_LLM=1 MCODE_LLM_DATA_DIR="$MINIMAX_DATA_DIR" \
  go test -count=1 -run 'TestLiveModelTurn|TestLiveGoalAndSubagent' ./appserver
```

`TestCapabilityGate` refuses a complete verdict while rows are still `fakecontract` or `blocked`. GitHub Actions runs the no-auth tests only.

## License

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE). Protocol shapes were recovered from the MIT-licensed `@minimax-ai/code` 0.7.8 package. This repository does not ship that CLI or its native addons.
