# Credentials

Keep the API key in an environment variable and in the CLI data directory. Do not pass it as an argument, and do not commit it.

```bash
export MCODE_PROVIDER_API_KEY=your-key
mcode provider set-minimax-key --api-key-env MCODE_PROVIDER_API_KEY
mcode provider use api-key
```

`your-key` is a placeholder. The `set-minimax-key` command reads the variable. The SDK process should use that data directory with `AuthMode` left empty.

`StderrTail` redacts JSON fields named `accessToken`, `access_token`, `refreshToken`, `refresh_token`, `apiKey`, `api_key`, `clientSecret`, `client_secret`, `authorization`, and `token`. It also redacts a `Bearer` token and a token that starts with `sk-` and continues with more than eight letters or digits. Redaction is not a reason to log raw provider responses.

`NotifyRuntimeAuthContext` rejects `logged_out` when an access token is set, and it rejects `authenticated` when the token or real user id is empty. The SDK does not fetch an account token for you.
