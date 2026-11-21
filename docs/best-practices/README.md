# Best practices

These pages describe behavior this module implements for `@minimax-ai/code` 0.7.8. Each page is a task, not a method list.

| Page | Use it when |
| --- | --- |
| [Process lifecycle](lifecycle.md) | You start `mcode app-server` and need to know who stops it |
| [Context, cancel, and streams](context-and-streams.md) | A call should stop, or you are reading an SSE response |
| [Retries](retries.md) | You want to send a call again after an error |
| [Credentials](credentials.md) | You configure an API key or read logs |
| [Permissions](permissions.md) | The server asks before a tool runs |
| [Errors and server limits](errors.md) | You branch on HTTP status, `base_resp`, or a known server refusal |
| [Versions and tests](versions.md) | You upgrade Go, the CLI, or this module |

The API reference is [../api.md](../api.md). The Chinese setup guide is [../getting-started.zh-CN.md](../getting-started.zh-CN.md). The acceptance snapshot is [../acceptance.zh-CN.md](../acceptance.zh-CN.md).
