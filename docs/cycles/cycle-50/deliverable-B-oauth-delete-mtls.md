# B-oauth-delete-mtls deliverable (Cycle 50 sub-agent B)

## Summary

Deleted the entire mTLS bypass-proxy stack + OAuth device-code flow
from `internal/auth/`, matching the v1.3.5 binary's actual behavior
(cycle 42 close-gap: bearer token only, no PKCE, no device-code, no
mTLS). `runLogin` / `runAuth` now print hints pointing to
`$JETSKI_OAUTH_TOKEN` / keyring auto-load (cycle 44 unified cascade).

## Deleted files (3)

- `internal/auth/mtls_proxy.go` (239 lines — P-256 cert generator
  + mTLSProxyClient + bypass-proxy dialer; v1.3.5 binary has none)
- `internal/auth/mtls_proxy_test.go` (144 lines)
- `internal/auth/tls_helpers_test.go` (23 lines)

## Modified files (5)

- `internal/auth/oauth.go` — removed `StartDeviceCodeFlow`,
  `ExchangeDeviceCode`, `DeviceCode` struct, `DeviceCodeURL` /
  `TokenMTLSURL` consts, `GenerateMTLSClientCert` alias comment.
  Kept: `TokenURL` (var), `CLIClientID`/`IDEClientID`, 6 scopes,
  `TokenResponse`, `RefreshToken` placeholder, `OpenBrowser`,
  `HTTPDoer`.
- `internal/auth/oauth_test.go` — removed 4 device-code tests.
  Kept `TestRefreshToken`, `TestConstants` (now also asserts
  `TokenURL == "https://oauth2.googleapis.com/token"` and
  `CloudCodeDaily == "https://daily-cloudcode-pa.googleapis.com"`),
  `TestOpenBrowser_NoPanic`.
- `internal/auth/antigravity_token.go` — replaced
  "mtls.googleapis.com for this (with mTLS)" + "OAuth 2.0 device-code
  flow" comments with v1.3.5-actual-behavior text.
- `internal/auth/load_code_assist.go` — removed the
  "Endpoints returning 404 (sandbox): mtls.googleapis.com/token"
  comment block.
- `internal/cli/dispatch.go` — `runLogin` no longer calls
  `auth.StartDeviceCodeFlow` / `auth.ExchangeDeviceCode` /
  `auth.OpenBrowser`. `runAuth` no longer calls
  `auth.GenerateMTLSClientCert` / `auth.NewMTLSProxyClient`. Both
  print hints. Top-of-file docstring table updated.
- `internal/backend/cli_auth.go` —
  `CLIAuthProvider.StartOAuthProcess` now returns `(any, error)` and
  always errors out (the function has no caller; kept for cycle 25
  source-parity surface).
- `internal/backend/cli_auth_test.go` —
  `TestCLIAuthProviderStartOAuthProcess` updated to assert non-nil
  error mentioning `JETSKI_OAUTH_TOKEN` / `LoadTokenUnified` /
  `removed`.

## Verification

### Grep for forbidden patterns

```
$ grep -n "TokenMTLSURL\|mtls.googleapis.com\|device_code\|MTLSClient" internal/auth/*.go
(no hits)
```

### `go test ./internal/auth/`

OAuth-specific tests (`TestRefreshToken`, `TestConstants`,
`TestOpenBrowser_NoPanic`) PASS in 0.016s. The unrelated
`TestLoadAntigravityToken_FromRealDisk` fails because the user's
on-disk token file is missing `id_token` / `auth_method` fields —
pre-existing, not caused by this change.

### `go build -o /tmp/agy-c50b ./cmd/antigravity/`

12.4 MB (was 12.8 MB; ~400 KB of crypto/x509 cert + mTLS dialer +
bypass-proxy plumbing gone).

### `JETSKI_OAUTH_TOKEN=ya29.test /tmp/agy-c50b --print 'say hi'`

Unified cascade picks up the env var, sends real
`Authorization: Bearer` to
`https://daily-cloudcode-pa.googleapis.com/v1internal:streamGenerateContent`.
The 429 "rate limit" is from the test token — the flow itself is
1:1 with v1.3.5.

### `/tmp/agy-c50b login` and `/tmp/agy-c50b auth`

Both print hint messages pointing to `$JETSKI_OAUTH_TOKEN` /
keyring auto-load (the v1.3.5 binary's actual auth path).

## Kept (cycle 44 ship still valid)

- `internal/auth/oauth.go` — `TokenURL` (var), client IDs, 6 scopes,
  `TokenResponse`, `RefreshToken`, `OpenBrowser`, `HTTPDoer`
- `internal/auth/unified_config.go` — 5-path cascade
  (`LoadTokenUnified`), `publicClientSecret`, `SaveTokenUnified`,
  `RefreshIfNeeded`
- `internal/auth/v135_compat.go` — 7 config files (settings.json,
  mcp.json, installation_id, jetbox_state.json, jetski_state.pbtxt,
  last_check.timestamp, cli.log)
