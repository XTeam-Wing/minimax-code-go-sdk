# Errors and server limits

Use `errors.As` and keep the raw body.

| What you see | What it means |
| --- | --- |
| `*desktop.StatusError` | HTTP status is not 2xx. `Body` is the raw response. |
| `*desktop.BusinessError` | HTTP 200 and top-level `base_resp.status_code` is not 0. The decoded value is still returned. |
| `*appserver.Error` | Outer JSON-RPC error. The SDK does not retry `-32001`. |

A finite non-zero `status_code` is a business error. An integer that fits in int64 is `StatusCode`. Any other non-zero finite number is still an error, and `RawStatus` keeps the original literal. Exact `0` or `"0"` is success. `0.5` is not success.

Some HTTP errors are the 0.7.8 app-server, not a bad SDK request:

- `GetLlmContextCapture`, `PutLlmContextCapture`, `GetLlmContextSessions`, and `GetSessionLlmContextOverview` return HTTP 500 with body `Internal Server Error` on `mcode app-server`. The service throws `CronContractError` status 501, and the desktop router has no `getResponse` for that error. Do not treat the 500 as a successful capture read.
- `ListBunnyTaskTelemetry` and `AckBunnyTaskTelemetry` are HTTP 503 `BUNNY_UNAVAILABLE` when the owner is app-server.
- `CreateSkill`, `CreateAgent`, and cron creates can return HTTP 422 `CONTENT_POLICY_VIOLATION`. Do not change the name to get past review.
- `SelectAdvisorModel` can return HTTP 403 `ADVISOR_DISABLED`.

The acceptance note lists calls that were not run. Those limits stay limits.
