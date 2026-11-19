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
go run ./examples/quickstart
```

对应的调用：

```go
client, err := appserver.Start(ctx, appserver.Options{
    Command:     "node",
    LeadingArgs: []string{"/path/to/cli.js"},
    DataDir:     dataDir,
    Workspace:   workspace,
    // 不要默认填 desktop。空字符串会省略 --auth-mode，CLI 报告 remote-oauth。
    // desktop 要 Desktop 租约；没有租约时 SendMessage 返回 503 local_runtime_auth_pending。
    // 这和模型供应商（API key / OAuth）不是一回事。
})
defer client.Close()

_, err = client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{})
```

握手顺序是固定的：先 `initialize`（`protocolVersion` 必须是 1，DesktopService 传输版本必须是 2），再发 `initialized` 通知。SDK 会做完这两步再把 `Client` 交给你。

## 已在 0.7.8 上跑通的只读路径

环境：Linux、glibc 2.39、Node v22.23.4、Go 1.22.2。临时 data dir 和 workspace，`--auth-mode desktop`。没有登录，没有发消息，没有改账号。这条只读路径不能当成 API key 推理的配置。无界面 API key 要先在隔离的 `MINIMAX_DATA_DIR` 里执行 `provider set-minimax-key --api-key-env MCODE_PROVIDER_API_KEY` 和 `provider use api-key`，启动 SDK 时不要设置 `AuthMode`。

外部消费者在 2026-11-19 18:47（东八区）用提交 `869b4ba` 省略 `AuthMode`，完成了 `MiniMax-M3.1-Flash` 的一次调用。现场记录器把用户提示和 `pong` 拼在了一起，而且没有保存原始 `SessionStreamFrameView`。事后用同一次会话账本和已保存的事件、用量记录，按 0.7.8 的形状重建了最小帧（`live_rerun: false`），助手文本是 `pong`，`output_tokens` 3，`input_tokens` 17248。完成依据是流里的 `finished`、`session_status` 和账本 `stopReason=stop`，清理依据是 `aborted: true` 与 abort HTTP 200。这不是第二次推理，也不是逐字节的线上回放。本仓库的测试进程没有重跑这次付费调用。短提示也会带上 mavis 的工具和技能上下文，大约 1.7 万 input tokens。没有明确要花费额度时不要跑 `MCODE_E2E_LLM=1`。设置 `MCODE_E2E_FRAMES_OUT` 才会把脱敏后的原始帧写到文件。

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

别名只有在 0.7.8 的处理函数里能证明 JSON 类型时才收窄。`PermissionReply` 和 `BatchPermissionDecision` 是数字 0、1、2，不是字符串 `deny`。`AgentShellPreference` 是 `auto`、`powershell`、`git-bash`。`LocalHookScope` 是 1 或 2。`SessionKind` 仍是 `Scalar`，因为字符串和数字 0 到 5 都能过。`CloudCronScheduleKind` 和 `WorkShareKind` 没有读到校验函数，保持 `Scalar`。`Scalar` 会保留数字和 null。Drive 的 13 个方法返回 `DriveGatewayResp`；`base_resp` 之外的网关 JSON 在 `Raw` 里，这条路径不走 IDL 响应编码器。

`cTu` 实际调用到的 `/mavis/api/agent` 叶子有具名方法。`GET /mavis/api/agent/{name}` 在 `routeResolvedAgent` 里有实现，但分发器要求路径还有下一段，所以这个信息接口到不了。
