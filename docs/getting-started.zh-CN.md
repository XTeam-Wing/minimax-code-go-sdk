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

## 测试怎么跑

不带模型的官方进程测试需要临时 data dir 和 workspace，并显式使用 `--auth-mode desktop`。它不会调用 `SendMessage`。`CreateGoal` 在这个模式下可以写入目标记录，这不是模型回合。`token_budget` 不是正在进行的那次请求的硬上限。请求记账之后，主机用 `tokensUsed` 和预算比较；`tokensUsed` 含输入、缓存写入、缓存读取和输出，所以一次响应可以越过预算，然后状态变成 `budget_limited`。活动目标上的 `DismissGoal` 返回 409，直到目标完成。

无界面 API key 要先在隔离的 `MINIMAX_DATA_DIR` 里执行 `provider set-minimax-key --api-key-env MCODE_PROVIDER_API_KEY` 和 `provider use api-key`，启动 SDK 时不要设置 `AuthMode`。带模型的场景是 `TestLiveModelTurn`、`TestLiveGoalAndSubagent` 和 `TestLiveWorkflow*`，只有 `MCODE_E2E_LLM=1` 才会跑。`TestLiveWorkflowSessionContinue` 是回合结束后换进程再继续，不是打断正在返回的流。`TestLiveWorkflowStreamCancel` 必须在 `finished` 之前中止。`SearchMemory` 只扫该 agent 的主记忆行；主题正文用 `SearchMemoryTopics`。主搜索为空不是 Get 失败，也不算搜索通过。`ListSessions` 的路径名要用 `mavis`，空名字会打到空路径段并得到 404，这不是缺路由。`SteerSession` 的路径是会话 id，正文 `queue_item_id` 才是入队返回的 `item_id`。主机只接受仍为 `queued` 的行；行已经不在队列里是 HTTP 404 `local_queue_item_not_found`。空闲会话的 `EnqueueMessage` 会先调度队列再返回 `queued`，所以刚入队的 id 马上 steer 会 404。这不是把 404 当成成功。流还开着且 `GetQueueItem` 仍是 `queued` 时，`SteerSession` 返回 `success` true、同一个 `queue_item_id` 和当前 `turn_id` 才是成功。`ResumeSession` 只重接会话流；中止后只有 `{"type":10}` 再 `DONE` 不是把回合做完。要继续被中止的生成，先看 `InspectTurnContinuation` 的数字状态：`1` 才能 `ContinueTurn`，那是一次新的空文本回合。`0` 是 409 `local_turn_continuation_unavailable`，这时只能再发一条 `SendMessage`。没有 usage 事件不等于这次中止前没有消耗。会话模型用 `SelectModel` 带 `session_id`，字段是 `provider_id=minimax_api`、`model_id=MiniMax-M3.1-Flash`、`thinking.effort=high`。读回是 `GetSession` 的 `session.model.thinking.effort`，不是 `ListModels`。不带 `session_id` 的 `SelectModel` 会改进程默认模型并且不保存 thinking。`CreateGoal` 没有 effort 字段。正式目标场景的 `token_budget` 是 250000。测试在 `tokens_used` 到 230000、总时长 3 分钟、或请求数停住后再等验证器 90 秒时记下轨迹并失败。父回合停在 6 次请求而 token 仍在涨，不算验证失败，也不算通过。通过要 `go test -count=1 ./...` 退出码 0、diff 只改 `share.go`、`share_test.go` 和 `go.mod` 不变、状态 `complete`、`status_reason` 为 `complete(verifier_met)`、`last_verification.verdict` 为 `met`。测试输出里的 `ok` 就是成功，不要求出现 PASS。字面量 `pass` 不是主机写入的结论。目标创建会启动一轮模型。子代理必须看到子会话产物被父会话收进工作区；只收到 `subagent.execution` 不算场景通过。

## 审批和重连

没有注册回调时，SDK **不会** 自动批准权限。`permission.ask` 从全局事件流里来，由你的代码调用 `ReplyPermission` 或 `BatchReplyPermissions`。

响应丢了不会自动重放。重连也不会把已经发出的写请求再打一遍。

## 方法清单

470 个 DesktopService 方法都有具名 Go API，见 [method-inventory.md](method-inventory.md)。英文说明见 [api.md](api.md) 和仓库根目录的 README。

470 个 IDL 方法是冻结的 DesktopService 表面。进程另外还提供 `/mavis/api/...` 分发叶子（权限、内容安全、浏览器、通信、cron、matrix、用量、文件、git、fs、runtime、diagnostics），在 `mavis.Client` 上有具名方法。`mavis.Raw` 只用于这份叶子清单之外的路径。

别名只有在 0.7.8 的处理函数里能证明 JSON 类型时才收窄。`PermissionReply` 和 `BatchPermissionDecision` 是数字 0、1、2，不是字符串 `deny`。`AgentShellPreference` 是数字 0（`auto`）、1（`powershell`）、2（`git-bash`）。字符串 `auto` 会被拒绝。`LocalHookScope` 是 1 或 2。`SessionKind` 仍是 `Scalar`，因为字符串和数字 0 到 5 都能过。`CloudCronScheduleKind` 和 `WorkShareKind` 没有读到校验函数，保持 `Scalar`。`Scalar` 会保留数字和 null。Drive 的 13 个方法返回 `DriveGatewayResp`；`base_resp` 之外的网关 JSON 在 `Raw` 里，这条路径不走 IDL 响应编码器。

`cTu` 实际调用到的 `/mavis/api/agent` 叶子有具名方法。`GET /mavis/api/agent/{name}` 在 `routeResolvedAgent` 里有实现，但分发器要求路径还有下一段，所以这个信息接口到不了。
