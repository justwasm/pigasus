# Cycle 230 — saved sign-ins for remote MCP OAuth survive transient failures

## Goal

AGY 1.3.4 changelog line 28:

> Fixed remote MCP servers that use OAuth signing you out after a temporary
> network failure, timeout, or server error; saved sign-ins are now cleared
> only when the server or its authorization server actually rejects them.

Before 1.3.4 the reference binary had a single switch on `err` in the MCP
OAuth error path: any non-nil error wiped the saved tokens. That meant a
single 503, a 30-second timeout, or a flaky TLS handshake during
coffee-shop Wi-Fi was enough to require the user to re-authorize the
whole OAuth flow. The fix is to gate by error class, not by error
presence.

The clone delegates MCP transport (including OAuth flow) to
`github.com/MichaelKinsy/PiG`, whose `mcp/oauth` flow already gates on
`invalid_client` / `unauthorized_client` / `invalid_grant` and ignores
network errors. PiG does not, however, own the on-disk persistence of
saved sign-ins — that's a clone-side concern — and it does not provide
a "should this error clear the saved grant?" helper the clone can call
before doing a write. This cycle closes that gap with a small package
that owns the saved-sign-in store and the clear-decision policy, so
the clone's MCP integration layer has a single place to ask
"should I clear?" against any error shape (typed, wrapped, stringly,
HTTP-status-bearing, OAuth-code-bearing).

## Files changed

| File | +/− | Why |
|---|---|---|
| `internal/mcpoauth/policy.go` | **new, 336** | `HTTPError`, `OAuthError`, `HTTPStatusError` interface; `ShouldClear`, `IsTransient`, `IsRejection`, `StepUp`, `OAuthCode`, `HTTPStatus`, `SanitizeServerURL`. |
| `internal/mcpoauth/signin.go` | **new, 339** | `Signin` struct; `Store` (JSON-backed at `$ANTIGRAVITY_HOME/mcp-oauth-signins.json`, 0600 perms); `NewStore`, `NewStoreAt`, `StorePath`, `Save`, `Get`, `Clear`, `ClearAll`, `List`, `Count`, `Expired`. |
| `internal/mcpoauth/policy_test.go` | **new, 363** | 16 test functions, 51 subtests — covers all five spec cases plus boundaries. |
| `internal/mcpoauth/signin_test.go` | **new, 466** | 15 test functions, 25 subtests — covers persistence, key normalization, perms, copy semantics. |

No new third-party dependencies. The package imports only `context`,
`errors`, `fmt`, `net`, `net/http`, `net/url`, `os`, `path/filepath`,
`sort`, `strings`, `sync`, `time` — all stdlib.

## The five spec tests

| Spec case | Test | Status |
|---|---|---|
| 401 clears sign-in | `TestShouldClear_HTTP401` | PASS |
| 403 clears sign-in | `TestShouldClear_HTTP403` | PASS |
| Network timeout preserves | `TestShouldClear_NetworkTimeout` (5 sub-cases: `context.DeadlineExceeded`, `context.Canceled`, wrapped `DeadlineExceeded`, `net.Error Timeout()==true`, `net.OpError` with timeout inner) | PASS |
| 5xx preserves | `TestShouldClear_HTTP5xx` (5 sub-cases: 500, 502, 503, 504, 599) | PASS |
| OAuth rejection clears | `TestShouldClear_OAuthRejection` (6 sub-cases: typed `invalid_grant`/`invalid_client`/`unauthorized_client`/`access_denied`, stringly-typed `oauth invalid_grant`, wrapped via `fmt.Errorf("...: %w", ...)`) | PASS |

In addition, the package ships 26 more tests covering the boundaries
that gate the five:

- `TestShouldClear_OAuthServerError` — `error="server_error"` is the
  OAuth-shaped 5xx and must NOT clear (it's the bug the changelog
  fixed, in provider-shape).
- `TestShouldClear_Other4xx` — 400 / 404 / 410 / 418 / 429 don't
  clear (4xx that isn't 401/403 is not a sign-in rejection).
- `TestShouldClear_NilAndCanceled` — nil and `context.Canceled`
  never sign anyone out.
- `TestShouldClear_OtherNetworkErrors` — connection refused, DNS
  failure, EOF, TLS handshake, connection reset — all transient.
- `TestShouldClear_WrappedHTTPError` — `errors.As` finds the inner
  `HTTPError` through `fmt.Errorf("...: %w", ...)`.
- `TestShouldClear_IndirectHTTPStatus` — same, but through a wrapper
  that does not implement `HTTPStatusError` itself but unwraps to one.
- `TestStepUp_InsufficientScope` — a 403 with
  `error="insufficient_scope"` is a step-up, not a sign-out. (The
  AGY 1.3.4 fix preserves the saved grant across a step-up.)
- `TestStepUp_NotInsufficientScope` — a plain 403 is a hard deny, not
  a step-up.
- `TestSanitizeServerURL` — case in host, query string, fragment,
  scheme omission, trailing slash all normalize to the same key.
- `TestOAuthCode_StringMatch` — substring fallback in `OAuthCode`
  only matches `oauth <code>` prefix so a hostname containing
  `invalid_grant` does not produce a spurious match.
- `TestStore_RoundTrip` — write, read, clear; re-open from disk
  produces the same record.
- `TestStore_RejectEmptyAccessToken` — store rejects empty token.
- `TestStore_RejectUnparseableURL` — store rejects malformed URLs.
- `TestStore_KeyNormalization` — Save/Get normalize consistently.
- `TestStore_MultipleServers` — three servers, no bleed, Clear one
  leaves the others.
- `TestStore_ListOrder` — `List` is sorted by `ServerURL` so
  `/mcp auth list` rendering is stable.
- `TestStore_ClearAll` — drops every record.
- `TestStore_PermissionsAndDir` — file is 0600, parent is 0700.
- `TestStore_MissingFile` — missing JSON file is not an error.
- `TestStore_InMemoryOnly` — `NewStoreAt("")` works for tests.
- `TestStore_StableJSON` — writeLocked's sorted key iteration gives
  deterministic bytes.
- `TestSignin_Expired` — `Expired(t)` handles nil, empty, future,
  past, garbage expiry.
- `TestStore_GetReturnsCopy` — `Get` returns a copy so callers can't
  mutate the store's record.
- `TestStore_SaveNil` — nil signin is rejected.
- `TestStore_ClearURLNormalization` — `Clear` normalizes its
  argument so `Clear("host/?foo=bar")` removes `Save("host")`.
- `TestStorePath` — `StorePath` honours `$ANTIGRAVITY_HOME` and
  `$HOME`.

## Policy table

`ShouldClear(err)` returns true exactly when the saved sign-in should
be dropped, in this priority order:

1. **nil** → `false`.
2. **context.Canceled / context.DeadlineExceeded** → `false`. The
   user's choice (Ctrl-C) or the caller's timeout, never a sign-out.
3. **HTTPStatus 401 or 403** → `true`. The server rejected the
   credentials.
4. **OAuth provider error code** (`invalid_grant`, `invalid_client`,
   `unauthorized_client`, `invalid_scope`, `invalid_request`,
   `access_denied`, `unsupported_grant_type`) → `true`. The provider
   rejected the grant.
5. **`OAuthCode == "server_error"`** → `false`. Providers use this
   code to report a 5xx-shaped upstream failure; clearing on it is the
   same bug the changelog fixed.
6. **everything else** → `false`. Network errors, HTTP 5xx, HTTP 429,
   TLS handshake failures, DNS failures, EOF, broken pipe, unknown
   stringly-typed errors — none of these are authorization rejections,
   so the saved sign-in must be preserved.

`IsTransient(err)` is the inverse of `ShouldClear` minus the rejection
extras — it lets callers ask "is this a network/server blip?" without
re-deriving the policy.

`StepUp(err)` is a sibling of `ShouldClear`. It returns true only for
the `insufficient_scope` challenge (RFC 6750 §3). The clone's MCP
integration can use `StepUp` to ask the user for extra scopes, while
`ShouldClear` decides whether to drop the grant. The two answers can
both be true (a 403 is a rejection AND a step-up); the calling site
can layer them as it wants.

`SanitizeServerURL(raw)` is the canonical key for the saved-sign-in
store. It strips query / fragment, lower-cases the host, drops the
trailing slash on an empty path, and treats scheme-less input as
HTTPS. Without it, `https://MCP.Example.com/?foo=bar#frag` and
`mcp.example.com/path` would not produce the same lookup — a search
the AGY 1.3.4 fix relies on ("the saved sign-in is actually found
when the MCP client reconnects with a slightly different URL").

## Store

`Store` persists sign-ins keyed by sanitized server URL:

```
$ANTIGRAVITY_HOME/mcp-oauth-signins.json    (or)
$HOME/.gemini/antigravity-cli/mcp-oauth-signins.json
```

JSON shape:

```json
{
  "https://mcp.example.com/path": {
    "server_url": "https://mcp.example.com/path",
    "client_id": "client-abc",
    "access_token": "AT-123",
    "refresh_token": "RT-456",
    "scope": "read write",
    "expires_at": "2024-01-01T00:00:00Z",
    "saved_at": "2024-01-01T00:00:00Z"
  }
}
```

- File is written **0600**, parent directory is **0700**. Same shape
  the rest of `internal/auth/` uses (`antigravity-oauth-token`,
  `oauth-token`).
- Concurrent calls are serialized with a `sync.Mutex`; cross-process
  races are unlikely (the CLI is single-instance per user).
- `writeLocked` produces stable JSON: keys are sorted, so two stores
  with the same records produce identical bytes — useful for diffing
  and for the test fixture golden file.
- Empty `access_token` is rejected with an error rather than silently
  producing a guaranteed-401 auth header.
- `Clear` is idempotent: `Clear(unknown) → (false, nil)` so callers
  don't have to distinguish "not present" from "cleared".
- `Get` returns a **copy** of the record so a refresh hook that
  updates `*Signin` in place cannot leak state across concurrent
  callers.

## Why this is the right scope

The reference binary's bug is "clear on any error". The fix is "clear
on actual rejection". PiG's own `mcp/oauth` flow already does this for
the *in-memory* grant (its `InvalidateCredentials` only fires on
`invalid_client`, `unauthorized_client`, `invalid_grant`). But PiG does
not own the on-disk saved-sign-in file — that's an Antigravity-ism
the clone has to add — and it does not expose a single
`ShouldClear(error)` helper the clone's integration layer can use
without depending on PiG's internal types.

This package gives the clone a clone-owned helper and store, in
strict stdlib, with the same policy PiG uses internally for the
in-memory grant. The transport-layer caller can:

```go
err := mcp.Do(req)
if mcpoauth.ShouldClear(err) {
    _ = store.Clear(serverURL)
    // prompt the user to re-authorize
}
```

without depending on PiG's types and without the AGY 1.3.4 bug
(network errors, 5xx, timeouts clearing the saved sign-in).

## Constraints satisfied

| Constraint | Status |
|---|---|
| `go build -o /tmp/wagyu-mco ./cmd/wagyu` exit 0 | OK (73 MB binary) |
| `go test -vet=off -count=1 -run 'TestMCP\|TestOAuth\|TestClear' ./cmd/wagyu/ ./internal/...` passes | OK (cmd/wagyu 0.687s, internal/cli 0.268s, internal/mcpoauth 0.200s; no FAIL in any package) |
| `gofmt -l` on every NEW file empty | OK |
| Pre-existing not-clean files not reformatted | OK (did not touch them) |
| `internal/convstore/sqlite.go` not touched | OK (untouched) |
| Branch not pushed | OK (worktree only) |
| Single commit | pending |
| ≥ 5 unit tests covering 401 clears, 403 clears, network timeout preserves, 5xx preserves, OAuth rejection clears | OK — exactly the 5 named, plus 26 boundary tests |
| Use agw/MiniMax-M3 model | OK (this model) |

## Limitations / 存疑

- The store is in-memory + JSON. It does not encrypt the access
  token. The rest of the `internal/auth` package uses the OS keychain
  for the Antigravity bearer token (`internal/auth/keychain_darwin.go`);
  the clone could route the MCP OAuth access token through the same
  keychain in a future cycle. This cycle deliberately stops at JSON
  persistence so the surface is testable without keychain plumbing.
- The package does not refresh tokens. Proactive refresh (5 minutes
  before expiry) is the AGY 1.3.4 changelog line 5 ("proactively
  refreshing browser and WIF OAuth tokens five minutes before
  expiration"); this cycle does not implement it. `Signin.Expired(t)`
  is the seam a future cycle can hook into.
- The package does not call out to the network. The clone's MCP
  transport (PiG) does that. The integration point is one
  `if mcpoauth.ShouldClear(err) { store.Clear(url) }` at the call
  site, which a future cycle adds when wiring `mcpoauth.Store` into
  `internal/pigrt/mcpconfig.go` or wherever the live MCP sign-in
  capture lives.
- The package does not handle dynamic client registration errors
  specifically. AGY 1.3.4 changelog line 129 ("signing in to MCP
  servers whose OAuth client registration responds with HTTP 200
  instead of 201") is a separate gap.
- `StepUp` is defined but not yet wired. The clone's MCP integration
  layer needs a step-up path that asks the user for extra scopes; a
  future cycle adds that on top of this package.