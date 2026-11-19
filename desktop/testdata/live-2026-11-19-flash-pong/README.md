# Live turn fixture (2026-11-19, Asia/Shanghai)

This is **not** a live rerun. The live harness did not persist raw
`SessionStreamFrameView` bytes. The fixture is reconstructed from records of
that same turn plus the official `@minimax-ai/code` 0.7.8 stream mapping:

- Assistant text `pong` comes from the official session ledger
  (`messages.jsonl`, `message.role=assistant`, `content[0].type=text`).
- The private ledger user message also contained a long `system-reminder`
  prefix. That body is omitted from this shareable fixture. The user-role
  frame here carries only the prompt already stored in the live result JSON.
- Stream `type: 2` is `Hn.AgentMessage`. Assistant stream text is
  `agent_message.msg_content` or `content[].text` when `role=assistant`.
- Completion is recorded as stream events `finished` and `session_status`
  (also in the live result `events` list) and ledger `stopReason=stop`.
  It is not inferred from `output_tokens`.
- Cleanup is recorded as the live result `aborted=true` after `AbortSession`,
  and the official CLI log line for `POST .../abort status=200`. The log file
  itself is not included here.

Do not treat `output_tokens` as the assistant text.
