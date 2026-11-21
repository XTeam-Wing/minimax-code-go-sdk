# Context, cancel, and streams

Pass a `context.Context` with the deadline you want. `Start` uses that context for the handshake. A desktop call uses the context you pass to that method.

`SendMessage`, `ResumeSession`, and `ContinueTurn` return `*desktop.EventStream`. `Recv` blocks until the next frame, `io.EOF`, or the context ends. The client credits the server as you read. If you stop calling `Recv`, the server stops sending more frames for that response.

Cancel the `Recv` context, or call `EventStream.Close`, to drop that response. The SDK sends a desktop transport abort for that stream. It does not call `AbortSession`, and it does not send the request again.

`Client.Close` closes the socket even when a WebSocket write is blocked. It then signals the owned child. Do not depend on a close frame being written before the socket closes.

One `appserver.Client` is safe for concurrent desktop calls up to `MaxInFlight` (default 64). A nil context is an error.
