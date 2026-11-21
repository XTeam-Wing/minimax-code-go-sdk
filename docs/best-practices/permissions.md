# Permissions

The SDK does not approve a tool call. If `Handlers.OnServerRequest` is nil, a server request such as `permission/ask` gets JSON-RPC method-not-found. The error data is `{"decision":"deny"}`. There is no `result`.

When you handle `permission.ask`, call `ReplyPermission` or `BatchReplyPermissions` with the decision you intend. `AllowOnce` is the narrow allow. Do not send `AllowAlways` unless you have decided that the same command may run later without another ask.

A missing permission event is not a denied command. Echo under the default mode can finish with no ask. Treat that as "the server did not ask", not as "the SDK blocked the command".
