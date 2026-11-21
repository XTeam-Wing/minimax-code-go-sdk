// Package appserver talks to MiniMax Code's app-server.
//
// The server is the `mcode app-server` process from @minimax-ai/code. This
// client speaks that process's JSON-RPC session and the DesktopService tunnel
// inside it. The message layout follows the 0.7.8 server, not the OpenAI Codex
// app-server. Codex is a useful guide for how to document a client (connect,
// initialize, events, approvals, errors). It is not the wire format.
//
// Start owns the child process it creates and Close is safe to call more than
// once. A lost response is returned to the caller. The client does not send
// that request again, and it does not approve a permission request unless the
// caller invokes the permission method with an explicit decision.
package appserver
