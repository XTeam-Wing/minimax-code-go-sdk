# MiniMax Code Go SDK

Go client for the MiniMax Code `mcode app-server` in `@minimax-ai/code` 0.7.8.

This repository is not published by MiniMax. It does not vendor the CLI, and it does not speak the OpenAI Codex app-server protocol. You install `@minimax-ai/code` yourself and this module starts `mcode app-server`.

Module: `github.com/XTeam-Wing/minimax-code-go-sdk`

## Install

Go 1.22 or newer. This module has no release tag yet. Build it from a checkout of this repository:

```bash
go test ./...
```

In another module:

```
require github.com/XTeam-Wing/minimax-code-go-sdk v0.0.0

replace github.com/XTeam-Wing/minimax-code-go-sdk => ../minimax-code-go-sdk
```

The CLI needs Node.js 22.19+, 24, 25, or 26. The 0.7.8 postinstall script checks `better-sqlite3`. Do not skip lifecycle scripts if you want the server to start.

```bash
npm install @minimax-ai/code@0.7.8
```

Verified tarball SHA-256:

`f497f02bac0f8fc8ad3791d59e8827208684ab466bec94cd7702594d19f5735a`

## Quickstart

`examples/quickstart` starts the server, lists agents, and closes the process.

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

The same calls:

```go
client, err := appserver.Start(ctx, appserver.Options{
    Command:   "mcode",
    DataDir:   dataDir,
    Workspace: workspace,
})
if err != nil {
    log.Fatal(err)
}
defer client.Close()

agents, err := client.Desktop().ListAgents(ctx, desktop.ListAgentsReq{})
```

Leave `AuthMode` empty. The CLI then reports `remote-oauth`. `AuthMode: "desktop"` asks for a Desktop lease. Without that lease, `SendMessage` returns HTTP 503 `local_runtime_auth_pending`. The flag is not the model provider.

`Start` owns the child process. `Close` stops that process. See [Process lifecycle](docs/best-practices/lifecycle.md) for what `Close` does on Linux, other Unix systems, and Windows, and what an abrupt crash does not guarantee.

## API key

Do not put a key in source, argv, or logs. Prepare an isolated data directory with the official CLI. The key stays in that directory.

```bash
export MINIMAX_DATA_DIR="$PWD/.mcode-data"
export MCODE_PROVIDER_API_KEY=your-key
mcode provider set-minimax-key --api-key-env MCODE_PROVIDER_API_KEY
mcode provider use api-key
```

`your-key` above is a placeholder. Unset `AuthMode` when you start the SDK against that directory. `examples/sessionmodel` selects `minimax_api` / `MiniMax-M3.1-Flash` / `thinking.effort` `high` and reads it back with `GetSession`. It does not send a model turn. A data directory with no key returns HTTP 400 `NO_API_KEY` for that `SelectModel`.

## Streaming and cancel

`SendMessage`, `ResumeSession`, and `ContinueTurn` return an `EventStream`. `Recv` returns the next frame. Cancel the `Recv` context, or call `EventStream.Close`, to stop reading. That closes the response stream. It does not call `AbortSession`, and it does not by itself submit another turn.

The client does not retry a call after a lost response. A retry of `SendMessage` or a git push can run the mutation twice. See [Retries](docs/best-practices/retries.md).

## Errors

A non-2xx DesktopService response is `*desktop.StatusError`. `Status` and `Body` are the HTTP status and the raw body. An HTTP 200 body whose top-level `base_resp.status_code` is not 0 is also `*desktop.BusinessError`, together with the decoded value. `errors.As` reads both.

Permission prompts are not approvals. A nil handler makes the SDK answer a server request with an error. It does not call `ReplyPermission` for you.

## Documentation

| Document | What it answers |
| --- | --- |
| [docs/api.md](docs/api.md) | Protocol, model selection, goals, memory, and stream behavior |
| [docs/getting-started.zh-CN.md](docs/getting-started.zh-CN.md) | Chinese setup and the same limits |
| [docs/method-inventory.md](docs/method-inventory.md) | Generated method table |
| [docs/best-practices/README.md](docs/best-practices/README.md) | Lifecycle, cancel, retries, keys, permissions, errors, versions |
| [docs/acceptance.zh-CN.md](docs/acceptance.zh-CN.md) | What a private acceptance run covered, and what it did not |

The method table is the generated client surface. It is not a live pass count.

## Compatibility

- CLI baseline is 0.7.8. `StrictBaseline` rejects any other `serverInfo.version`.
- Stdio works on Linux, macOS, and Windows. `ListenUnix` needs a Unix socket and returns an error on Windows.
- The private acceptance snapshot is commit `d907d518f23d5778220906ded6daa83ea7cf8d07`: 305 accepted, 19 remaining. That is not a pass of every later commit, and it is not all 497 inventory rows. Details are in the acceptance note.

## Development

```bash
gofmt -w .
go vet ./...
go test ./...
go test -race ./...
```

GitHub Actions runs those checks and a no-key app-server subset. It does not run model turns. Regenerate bindings from the pinned tarball with `python3 tools/gen_desktop.py /path/to/minimax-ai-code-0.7.8.tgz`.

## License

MIT. See [LICENSE](LICENSE) and [NOTICE](NOTICE). Protocol shapes come from the MIT-licensed `@minimax-ai/code` 0.7.8 package. This repository does not ship that CLI.
