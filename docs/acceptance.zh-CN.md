# 兼容性与验收范围

截至 **2026-11-21（Asia/Shanghai）**。

English summary: This note separates four layers. The Go SDK wraps the `@minimax-ai/code` 0.7.8 IDL. Offline `gofmt`, `go vet`, `go test`, and `go test -race` check that wrapper. They are not a live pass of the 497 API inventory. A private acceptance on commit `d907d518f23d5778220906ded6daa83ea7cf8d07` recorded **305 accepted** and **19 remaining**. That snapshot is not a claim that every later commit, including this document, was re-run as full end-to-end, and it is not “497 all passed.” API-key model runs stay on an isolated data directory and were not repeated for this push. `PushWorkspaceGit` and `CommitAndPushWorkspaceGit` were accepted only against an owned local-file bare origin.

## 四层不要混在一张表里

| 层 | 说明 |
| --- | --- |
| SDK 封装 | 按 `@minimax-ai/code` **0.7.8** 的 IDL 生成具名请求和响应。470 个 DesktopService 方法都有 Go API。这是客户端表面，不是现场调用全部成功。tarball SHA-256：`f497f02bac0f8fc8ad3791d59e8827208684ab466bec94cd7702594d19f5735a`。 |
| 离线测试 | `gofmt`、`go vet ./...`、`go test ./...`、`go test -race ./...`，以及 Windows 下只编译 `./appserver`。不启动官方进程，不使用 API key。 |
| 无 key 的官方进程 | `MCODE_E2E=1` 且 `--auth-mode desktop`。覆盖握手、本地读写和已记录的服务端负例。GitHub Actions 的 app-server 作业只跑这条无 key 协议子集。 |
| API key 真实验收 | 隔离的 `MINIMAX_DATA_DIR`，`AuthMode` 留空，模型为 `minimax_api` / `MiniMax-M3.1-Flash` / `thinking.effort` `high`。只有 `MCODE_E2E_LLM=1` 才会跑。本次推送没有 key，也没有重跑这些场景。 |

安装、连接和 API key 的准备步骤仍以 README、[api.md](api.md) 和 [getting-started.zh-CN.md](getting-started.zh-CN.md) 为准。下面的数字不改那些步骤。

## 已记录的验收快照

父侧在隔离环境里对提交 **`d907d518f23d5778220906ded6daa83ea7cf8d07`** 加上官方 `@minimax-ai/code` 0.7.8 做过验收：

- **305** 项接受
- **19** 项仍未验完或失败，保留原状，不改叫 unsupported

这不是官方 497 项清单全部通过，也不是本文件所在的更新提交又做了一遍全量端到端。497 是 API 清单的基线算法（470 个 IDL 方法，加上清单外的请求、通知、错误码和 unix HTTP）。305 与 19 是那一次验收账，不能把两者相加说成 497 已满。

之后的 `acf1b394237bfcefda0e768c0104f40e228eebba` 只补充了 llm-context 在 `app-server` 上的 HTTP 500 合同测试。它没有把下面的失败改成成功。

`MiniMax-M3.1-Flash` 的真实对话在父侧已有回归。本次交付拿不到 key，按约定不重跑。

## 有边界的两项 Git 推送

`PushWorkspaceGit` 和 `CommitAndPushWorkspaceGit` 只在**自有、隔离的 local-file bare origin** 上接受：类型化调用返回 HTTP 200，提交和 tree 里的文件读回与工作区一致。

这不能外推到 HTTPS，也不能外推到 GitHub 或其他网络 remote。

## 仍未验完或失败的 19 项

这些调用保持未执行、失败，或返回了不能当成通过的结果。

| 方法 | 状态 |
| --- | --- |
| `AckExtensionApplied`、`InputExtension`、`CloseExtensionPane`、`PressExtension`、`AnswerExtensionQuery`、`RenderExtension`、`ReportExtensionFallback`、`SelectExtension`、`SubscribeExtensionStream` | 缺真实官方 init 和获准的 preview 夹具。`ListExtensionEntries` 已通过，不能代替这九项。 |
| `ListBunnyTaskTelemetry`、`AckBunnyTaskTelemetry` | 官方实现面向 electron。`app-server` 这个 owner 没有该服务。源码合同是 HTTP 503 `BUNNY_UNAVAILABLE`。没有把这次调用当成成功。 |
| `DownloadSessionResult`、`OverwriteSessionResult`、`BindSessionFileSyncOwner` | 需要真实的云 handoff 或账号数据。未执行。 |
| `SelectAdvisorModel` | 真实 HTTP 403 `ADVISOR_DISABLED`。 |
| `GetAgentLegacyHistoryNotice` | 真实 HTTP 200，正文 `{}`。没有合格的 legacy 历史，不算通过。 |
| `SetAssignmentCompletion` | 没有可预先界定的自动 turn 夹具。未执行。 |
| `UnrestUserModelApiKey` | 没有真实的 rest 状态。该方法只清内存。不制造限流，mock 不算这项真实端到端。 |
| `SendRemoteMessage` | 这是本地 ingress，不是云发送，但会启动 turn。受工具约束的 `CreateAgent` / `PutAgentConfig` 要过官方审核，审核需要 auth。未执行，也不绕过审核。 |

## 四条 llm-context 读取保持失败

在 `mcode app-server` 上，下面四条是真实 HTTP 500，正文为 `text/plain` 的 `Internal Server Error`：

- `GetLlmContextCapture`
- `PutLlmContextCapture`
- `GetLlmContextSessions`
- `GetSessionLlmContextOverview`

`app-server` 的 `runtimeOwnerKind` 是 `app-server`，没有挂上 local-runtime-v2 的 inspector。`Bwe` 抛出 `CronContractError`（状态 501，key `LLM_CONTEXT_INSPECTOR_UNSUPPORTED_ON_LEGACY_HOST`）。这个错误没有 `getResponse`，桌面路由把它收成 HTTP 500。SDK 保留这个状态和原始正文，不把它改记成 501，也不把它解成成功的 `capture_enabled` 或 `revision`。

`GetSessionLlmContextCall` 和 `ClearSessionLlmContextHistory` 没有在这次验收里调用，不写成已观察的 500。

## 本次推送没有做的事

- 不合并，不发布，不把拉取请求标成 ready。
- 不把私有验收报告、原始证据或数据目录放进仓库。
- 不重跑需要 API key 的模型场景。
