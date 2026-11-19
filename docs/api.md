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

Response objects use IDL `jsonName`s. A field whose schema says `kind: scalar` without a recovered `tsType` is `desktop.Scalar`. Call `String`, `Bool`, or `Float64`. Across the generated structs there are 2,145 fields with a declared Go type and 2,818 `Scalar` fields. Request structs have no remaining `Scalar` fields: erased aliases such as `PermissionReply` and `LocalHookScope` are strings, because the binding is a scalar and the alias is not numeric or boolean. A response field stays `Scalar` when no binding and no `structFields` entry names that struct field with a single `tsType`. That is the upstream schema, not a hidden guess.

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

`permission.ask`, `message.rewind`, `subagent.execution`, `ui.request`, and the other publish sites still decode as `InterpretedEvent` with `Payload` set from `payload_json`. They do not have a helper, because the payload expression was not a literal object.

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
