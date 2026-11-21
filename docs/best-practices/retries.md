# Retries

The SDK does not retry. A lost response is not proof that the server skipped the call.

Before you send the same call again, decide whether a second run is safe.

- `SendMessage`, `ResumeSession` with `drain_queued` or `continue_paused_queue`, `ContinueTurn`, and goal creation start work. A second call can start more work.
- `PushWorkspaceGit` and `CommitAndPushWorkspaceGit` mutate git state. A checked acceptance of those two methods used an owned local-file bare origin only. Do not treat that result as an HTTPS or GitHub push.
- A read such as `ListAgents` or `GetSession` can be sent again.

`ResumeSession` with only the session id reattaches a stream. It does not submit a turn unless you set a queue flag. `ContinueTurn` submits a new empty-text turn only when `InspectTurnContinuation` state is `1`.
