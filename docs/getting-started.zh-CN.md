# 入门

这个 SDK 连接 MiniMax Code 的 `mcode app-server`，不是 `mcode exec`，也不是 OpenAI Codex 的 app-server。文档结构参考了 Codex 的用法说明（连接、生命周期、方法、事件、审批、错误、示例），线上的消息格式以 `@minimax-ai/code` **0.7.8** 为准。

## 安装 CLI

需要 Node.js 22.19+、24、25 或 26。安装时要跑 postinstall，它会检查 `better-sqlite3`。

```bash
npm install @minimax-ai/code@0.7.8
```

校验过的 tarball SHA-256：

`f497f02bac0f8fc8ad3791d59e8827208684ab466bec94cd7702594d19f5735a`

## 跑起来

SDK 自己拉起子进程，退出时 `Close` 会停掉它。`Close` 可以调用多次。数据目录请用空目录，不要指向你正在使用的登录目录。

```bash
export MCODE_BIN=node
export MCODE_LEADING=/path/to/node_modules/@minimax-ai/code/cli.js
export MCODE_DATA_DIR=/tmp/mcode-data
export MCODE_WORKSPACE=/tmp/mcode-ws
export MCODE_AUTH_MODE=desktop
go run ./examples/quickstart
```

对应的调用：

```go
client, err := appserver.Start(ctx, appserver.Options{
    Command:     "node",
    LeadingArgs: []string{"/path/to/cli.js"},
    DataDir:     dataDir,
    Workspace:   workspace,
    AuthMode:    "desktop",
})
defer client.Close()

_, err = client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{})
```

握手顺序是固定的：先 `initialize`（`protocolVersion` 必须是 1，DesktopService 传输版本必须是 2），再发 `initialized` 通知。SDK 会做完这两步再把 `Client` 交给你。

## 已在 0.7.8 上跑通的只读路径

环境：Linux、glibc 2.39、Node v22.23.4、Go 1.22.2。临时 data dir 和 workspace，`--auth-mode desktop`。没有登录，没有发消息，没有改账号。

- 服务版本 `0.7.8`，协议 `1`
- `ListAgents` 返回 5 个 agent
- 对 agent `mavis` 调用 `CreateSession` 后立刻 `AbortSession` 成功
- `GetEcho` 返回 `get: go-sdk`
- `Ping` 返回 `pong: ping`
- `GET /mavis/health` 返回 `status=ok`、`runtime=local-runtime`
- 打开 `SubscribeGlobalEvents` 后取消
- Unix 监听上 `GET /readyz` 与 `GET /healthz` 为 `ok`（目录权限必须是 `0700`）

## 审批和重连

没有注册回调时，SDK **不会** 自动批准权限。`permission.ask` 从全局事件流里来，由你的代码调用 `ReplyPermission` 或 `BatchReplyPermissions`。

响应丢了不会自动重放。重连也不会把已经发出的写请求再打一遍。

## 方法清单

470 个 DesktopService 方法都有具名 Go API，见 [method-inventory.md](method-inventory.md)。英文说明见 [api.md](api.md) 和仓库根目录的 README。

470 个 IDL 方法是冻结的 DesktopService 表面。进程另外还提供 `/mavis/api/...` 分发叶子（权限、内容安全、浏览器、通信、cron、matrix、用量、文件、git、fs、runtime、diagnostics），在 `mavis.Client` 上有具名方法。`mavis.Raw` 只用于这份叶子清单之外的路径。

请求字段里已经没有 `Scalar`。原先 33 个别名（例如 `PermissionReply`、`LocalHookScope`）在绑定里是 scalar，而且不是 number/boolean，所以 Go 类型是 `string`。响应和嵌套结构里仍有 2818 个 `Scalar`：schema 只写了 `kind: scalar`，绑定和 `structFields` 都没有给出唯一的 `tsType`。Drive 的 13 个方法返回 `DriveGatewayResp`（校验过的 `base_resp`，其余 JSON 在 `Raw`），因为这条路径不走 IDL 响应编码器。
