# API guide

This guide follows the same outline as the Codex app-server docs (connect, lifecycle, methods, events, approvals, errors, examples). The messages are MiniMax's. Codex method names such as `thread/start` and `turn/start` are not implemented here and would be rejected by `mcode app-server`.

Baseline: `@minimax-ai/code` 0.7.8. Inventory: [method-inventory.md](method-inventory.md).

## Lifecycle

`appserver.Start` launches the CLI and performs the handshake. `appserver.DialStdio` attaches to a process you already run. `ListenUnix` dials a WebSocket on the Unix socket after `GET /readyz` reports `ok`.

Handshake, in order:

1. Request `initialize`.
2. Require `protocolVersion == 1` and DesktopService `transportProtocolVersion == 2`.
3. Notification `initialized`.

A business call before that notification returns JSON-RPC `-32002`. A second `initialize` returns `-32003`.

```go
params := map[string]any{
    "protocolVersion": 1,
    "clientInfo": map[string]string{
        "name": "example", "title": "Example", "version": "0.1.0",
    },
    "capabilities": map[string]any{
        "desktopService": map[string]any{
            "transportProtocolVersion": 2,
            "binaryPayloadEncodings":   []string{"base64"},
        },
    },
}
```

The SDK sends that payload for you. `Client.InitializeResult` keeps the raw JSON plus the known fields (`serverInfo`, `capabilities.features`, `capabilities.methods`, `reconnect.history`).

`Close` stops a process this client started. It does not restart it and does not replay in-flight calls.

## Methods

Every IDL method is a method on `*desktop.Client`:

```go
resp, err := client.Desktop().GetSession(ctx, desktop.GetSessionReq{ID: sessionID})
stream, err := client.Desktop().SendMessage(ctx, desktop.SendMessageReq{
    ID:      sessionID,
    Content: &text,
})
for {
    frame, err := stream.Recv(ctx)
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
    _ = frame
}
```

Request fields come from the IDL bindings:

| Binding | Encoding |
| --- | --- |
| `path` | `:wire` replaced with `encodeURIComponent` |
| `query` | repeated query keys; struct children use `wire[child]` |
| `header` | string header |
| `body`, `implicitBody` | JSON object keyed by wire name |
| whole-body (`transport: jsonBody`, 73 methods) | one JSON object using `json` names |
| `kind: none` | omitted (this includes most `base_req` fields) |

`AuthMode` is the process flag `--auth-mode`. Leave it empty for a headless API-key data directory. The CLI then reports `remote-oauth`, and `SendMessage` can use a provider selected with `mcode provider use api-key`. `AuthMode: "desktop"` asks for a Desktop authentication lease. Without that lease, `SendMessage` returns HTTP 503 `local_runtime_auth_pending` even when `provider test minimax_api` already succeeded. The flag is not the model provider.

Assistant text uses `msg_content` or `content[].text` only when `role` is `assistant`. A committed `agent_message` replaces earlier chunk text, so a chunk and the committed message are not concatenated. `output_tokens` is usage, not completion. `finish_reason` and `session_status` are the completion signals. `CreateGoal` starts a model turn because the 0.7.8 handler always sets kickoff admission. The request can carry `objective`, `token_budget`, attachments, and `send_behavior` (`queue` or `steer`). It cannot carry a request limit or an active-seconds limit. `token_budget` is not a ceiling that stops the request already in flight. After that request is accounted, the host compares `tokensUsed` with the budget. `tokensUsed` includes input, cache writes, cache reads, and output, and each request re-reads cached context, so one response can jump past the budget. The host then stores `budget_limited` and stops auto-continuation. `update_goal` `complete` is a model proposal. The tool result is `settlement: pending_host_validation` with `goalSnapshotPhase: before_host_settlement`. The host then starts a separate foreground session with `agent_name: verifier` and the worker model key. Verification mode is `subagent` only when the provider source is `minimax_api_key` or `managed_token_plan`; otherwise it is `none`. `GetGoal` `status_reason` `paused(verifier_runtime)` is the map of issue codes `spawn_failed` (parent session unavailable) and `child_crash` (the verifier turn threw or did not finish `succeeded`, including an unknown `verifier` agent). `last_verification` is written only when a `VERDICT` is settled, so a null verification means that failure path ran, not a verdict of fail. `paused(verifier_aborted)` is a different reason, used when the verifier signal is aborted. `budget_limited(token)` is the reason when accounted tokens reach `token_budget`. The verifier task does not receive thinking effort from `CreateGoal` or `PatchGoal`; neither request has an effort field. The 0.7.8 launcher sets the child task to `agent_name: verifier` and `model` to the worker key `provider/model`. That key keeps the provider and model id and drops thinking, so the task selection is `{model}` with no `effort`. When that string does not match the parent session model, the source is `task-model-override` and the selection must carry `effort`, `reasoning`, or `variant`. The options in the error are the model catalog `thinking.effortOptions`. For `minimax_api/MiniMax-M3.1-Flash` the captured list is `default`, `low`, `medium`, `high`, `xhigh`, `max`. The host then appends the instruction not to guess an effort. `SelectModel` is the session request that stores an official effort string: `provider_id`, `model_id`, optional `session_id`, and `thinking.effort`. `CreateSession` and `SendMessage` store the same string at `model.thinking.effort`. The child reads a parent effort only when the task model key matches the parent model, and that match does not set `requireExplicitEffort`. Those fields do not fill the `task-model-override` selection. The caller chooses the effort string. `PatchGoal` of the objective does not complete the goal. Goal status is `active`, `paused`, `complete`, `blocked`, `budget_limited`, or `usage_limited`. A subagent counts only when `subagent.execution` has status `started` and `GetSession` on the child succeeds. Opt-in model scenarios are `TestLiveModelTurn`, `TestLiveGoalAndSubagent`, and the `TestLiveWorkflow*` tests. They are not run without `MCODE_E2E_LLM=1`.

`SelectModel` with `session_id` is the request that stores the caller-chosen effort for that session. For the current goal scenario the fields are `provider_id` `minimax_api`, `model_id` `MiniMax-M3.1-Flash`, and `thinking.effort` `high`. `high` is one of the catalog `effortOptions` on that model; the SDK does not fill it in. The readback is `GetSession`: `session.effective_model` is `minimax_api/MiniMax-M3.1-Flash`, and `session.model.provider_id`, `session.model.model_id`, and `session.model.thinking.effort` repeat those three values. `ListModels` marks `selected` and returns catalog `effort_options`; it does not return the stored session effort. On a data directory with no API key, that `SelectModel` returns HTTP 400 `NO_API_KEY` and does not store the session. The readback test uses `MCODE_LLM_DATA_DIR` and does not call `SendMessage`. Omitting `session_id` calls `setDefaultModel` with the model key and variant only, so that call is not the session configuration and it does not carry `thinking`. The prefix `minimax_api` is not rewritten to `minimax`. The verifier worker key is the turn's `provider/model`. It matches the session only when both strings canonicalize to the same key. The user-flow tests are a separate report from the 497 API rows. An empty list or HTTP 200 is evidence for that method, not a completed workflow. `TestLiveWorkflowGoalDeliverable` is the owned Go fix: `token_budget` 250000, and the test keeps polling while `tokens_used` is under 230000, the wall clock is under 3 minutes, and the verifier window is under 90 seconds after the request count stops rising. A parent plateau at 6 requests while tokens move from about 122000 to about 148000 stays inside that window. The pass condition is `go test -count=1 ./...` exiting 0, a diff that changes `share.go` only, unchanged `share_test.go` and `go.mod`, status `complete`, `status_reason` `complete(verifier_met)`, and `last_verification.verdict` `met`. Output that says `ok` without the word PASS is a successful test. A literal verdict `pass` is not the host value. A limit writes a harness trace and fails. It does not count as a host verdict.

Response objects use IDL `jsonName`s. A field whose schema says `kind: scalar` without a recovered `tsType`, or an alias whose JSON primitive was not recovered, is `desktop.Scalar`. Call `String`, `Bool`, or `Float64`. Proven aliases are named types: `PermissionReply` and `BatchPermissionDecision` are JSON numbers 0, 1, and 2; `AgentShellPreference` is the number 0 (`auto`), 1 (`powershell`), or 2 (`git-bash`). Sandbox filesystem mode is 1 (`read_only`), 2 (`workspace_write`), 3 (`delete_guard`), or 4 (`full_access`). Sandbox network mode is 1 (`deny`) or 2 (`allow_all`). `SandboxLocalAccess` is 1 (`open`) or 2 (`restricted`). `LocalHookScope` is 1 or 2. `SessionKind` stays `Scalar` because both the known strings and the numbers 0 through 5 are accepted. `CloudCronScheduleKind` and `WorkShareKind` stay `Scalar` because no reader was recovered. A number or null in a `Scalar` is preserved. Drive methods return `DriveGatewayResp`. The gateway body beyond `base_resp` is opaque.

A nil error is not business success by itself. After a 2xx decode, `callUnary` reads only the top-level `base_resp`. A missing `base_resp`, a non-object `base_resp`, a missing `status_code`, and a `status_code` that is not a finite number or a decimal integer are left alone. `status_code` 0 or `"0"` is success. Any other parsed `status_code` returns `*desktop.BusinessError` together with the decoded response. `errors.As` yields `StatusCode`, `Code`, `Message`, and the raw `Body`. The memory service uses this on HTTP 200: `status_code` 1, `status_msg` the sentence, and `code` such as `MEMORY_DISABLED` or `OLD_STRING_AMBIGUOUS`. A `status_code` elsewhere in the JSON is not that envelope.

`GetMemory` and `SearchMemory` do not read the same place when `agent_id` is 0 and `agent_name` is omitted: Get reads user memory, and Search resolves the name to `mavis`. With `agent_name` set, both use that agent's main file. Search splits `query` on whitespace and returns a line only when the line contains a query token. An empty query returns no lines. Topic text is `GetMemoryTopic` and `SearchMemoryTopics` (`keyword` against the topic name and body). An empty `SearchMemory` result is a completed call with no matching line. It does not show that Get lost the file, and it is not a search pass. `ListSessions` is `GET /minimax-desktop/api/v1/agent/:name/session`. `Name` `mavis` is the path segment. An empty name is sent as an empty segment and the server answers 404. That 404 is the empty segment, not a missing SDK route.

`TestLiveWorkflowSessionContinue` is a finished turn, a new process, then another turn on the committed history. `TestLiveWorkflowStreamCancel` aborts while the stream has not finished. It requires the user prompt once in `GetMessages` and one file hash, so an in-flight chunk is not replayed and the tool does not run again.

`Number` is a float64 because the server decodes `number` and `ThriftI64` with JavaScript `Number`.

Drive routes under `/minimax-desktop/api/v1/drive` are rewritten to the website gateway (`EUi` / `YZ`) before the IDL registrar. The 13 methods validate the gateway rules (offset `0..2147483647`, limit `1..100`, `sort_by` `deployed_at|updated_at`, `sort_order` `asc|desc`, `days` `7|30`, non-empty `node_id`, `display_name`, and `alias`) and return `DriveGatewayResp`. `BaseResp.StatusCode` is a number or a decimal string. `Raw` is the full JSON body. The IDL response struct is not what this path returns.

`RawCall` sends an arbitrary method and URL on the same tunnel.

## Events

`SubscribeGlobalEvents` is a `GET /minimax-desktop/api/v1/events` SSE stream. Frames are `GlobalEventFrame`:

| Field | Required in the IDL |
| --- | --- |
| `kind` | yes |
| `timestamp` | yes |
| `type` | no |
| `source` | no |
| `payload_json` | no |

`type` is not a closed enum. `EventType()` returns the string when the literal is a JSON string. Any other literal stays on the frame and does not stop the stream.

Confirmed publish sites, not an exhaustive list:

- `agent.updated`
- `config.permission_mode.changed`
- `message.rewind`
- `permission.ask`
- `permission.resolved`
- `subagent.execution`
- `thread_goal.objective_updated_steering`
- `ui.request`

A second subscriber to the same events stream supersedes the first on the server. This client does not open that stream for you.

SSE bytes are `data: <json>\n\n`. A `data: [DONE]` line ends the stream. `stream-error` is an error, not a frame.

## Approvals

Permission prompts are events and DesktopService calls, not outer JSON-RPC server requests. The outer server can still send a JSON-RPC request. A nil `Handlers.OnServerRequest` answers `-32601`. It does not answer success.

To allow or deny a tool, call `ReplyPermission` or `BatchReplyPermissions` yourself. Do not treat a zero value as allow. The live test subscribes, observes the stream, aborts it, and asserts that no `/permission/` or `batch-reply` request was sent.

Account and API-key methods exist (`AccountLoginStart`, `UpsertMiniMaxApiKey`, and the model-access login calls). The SDK does not call them during startup and does not write tokens to disk.

`PrepareRestart` is available and is never called automatically.

## Errors

| Code | Name |
| --- | --- |
| -32700 | parseError |
| -32600 | invalidRequest |
| -32601 | methodNotFound |
| -32602 | invalidParams |
| -32603 | internalError |
| -32001 | overloaded (retryable flag only; the client does not retry) |
| -32002 | notInitialized |
| -32003 | alreadyInitialized |
| -32004 | unsupportedTransport |
| -32005 | unsupportedProtocol |
| -32010 | authOperationFailed |

`errors.Is(err, appserver.ErrNotInitialized)` matches the code. A non-2xx DesktopService HTTP status is `*desktop.StatusError` and includes the body. A JSON body that is not the IDL object is `*desktop.DecodeError` and includes the bytes. A tunnel `stream-error` frame is `*desktop.StreamError`.

Messages larger than 16 MiB are rejected. The client also drops the connection on a line that large.

## Events that are known, not closed

`Interpret` accepts every frame. `HasType` is false when `type` is missing or is not a JSON string; the frame is still returned. A live 0.7.8 subscription produced `kind=heartbeat` with no `type` and the stream stayed open.

Helpers exist only for payloads whose publisher object was recovered:

| type | Fields seen at the publish site |
| --- | --- |
| `permission.resolved` | `requestId`, `sessionId`, `decision` |
| `config.permission_mode.changed` | `permissionMode` |
| `agent.updated` | `agentName` |
| `questionnaire.dismiss` | `requestId`, `sessionId`, `agentName`, `status` |
| `thread_goal.objective_updated_steering` | `sessionId`, `goalId` |

`permission.ask`, `message.rewind`, `subagent.execution`, and `ui.request` have helpers. Unknown event types still stay on `InterpretedEvent`. The type set is not closed.

## Dispatcher routes outside the 470

These are reached through the same tunnel. They are not IDL methods. Leaf routes under the other `/mavis` routers are not a closed set; `mavis.Client.Raw` is the escape hatch for those, and they are listed as incomplete rather than omitted from the product.

| Method | Path | Body |
| --- | --- | --- |
| `Health` | `GET /mavis/health` | stub: `status`, `runtime`, optional `startupToken` |
| `Version` | `GET /mavis/api/version` | stub: `version=local-runtime` |
| `UpdateStub` | `POST /mavis/api/update` | stub `{ok:true}`. Does not start an updater in 0.7.8. Not called unless you call it. |
| `RuntimeCapabilities` | `GET /mavis/api/runtime/capabilities` | raw document |
| `RuntimeStatus` | `GET /mavis/api/runtime/status` | raw document |
| `RuntimeDoctor` | `GET /mavis/api/runtime/doctor` | raw document |
| `RuntimeMigrationCheck` | `GET /mavis/api/runtime/migration/check` | raw document |
| `GetConfig` | `GET /mavis/api/config` | raw document |
| `PutConfig` | `PUT /mavis/api/config` | caller-supplied JSON |
| `ConfigAPIKeyStatus` | `GET /mavis/api/config/api-key-status` | raw document |
| `PermissionModeOptions` | `GET /mavis/api/config/permission-mode-options` | raw document |
| `LLMProviderDiscovery` | `GET /mavis/api/external/llm-provider-discovery` | raw document |
| `Usage`, `UsageSummary` | `GET /mavis/api/usage[/summary]` | raw document |
| `LegacySessionSearch` | `GET /mavis/api/session/search` | 0.7.8 returns 503 for the V1 session fallthrough |

The live stdio run called `Health` and got `status=ok`, `runtime=local-runtime`.

## Examples

List agents and abort a session without sending a model turn:

```go
agents, err := client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{})
name, _ := agents.Agents[0].Name.String()
created, err := client.Desktop().CreateSession(ctx, desktop.CreateSessionReq{
    Name: name,
})
id, _ := created.SessionID.String()
reason := "stop"
_, err = client.Desktop().AbortSession(ctx, desktop.AbortSessionReq{ID: id, Reason: &reason})
```

On the recorded 0.7.8 run this returned a session id for agent `mavis` and `AbortSession` succeeded. No message was sent.

Unix health:

```go
ready, err := client.Readyz(ctx)   // only after ListenUnix
health, err := client.Healthz(ctx)
```

The listener rejects an `Origin` header and requires the control directory to be mode `0700`.

## Compatibility

Supported server version: **0.7.8**. `Options.StrictBaseline` fails the handshake when `serverInfo.version` is anything else. Leaving it false still records `VersionVerified() == false` for any other version. Do not treat that as a compatibility promise.

Go 1.22+. No third-party modules. Windows builds omit Unix sockets. The client does not read `~/.mcode` unless you pass that path as `DataDir`.
