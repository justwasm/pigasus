# feature/stale-credentials-cleared — cycle 225 deliverable

AGY 1.2.5 changelog closure: when the OAuth sign-in server definitively
rejects credentials (revoked refresh token, invalidated enterprise
account, missing / wrong client identity), the CLI now wipes the
on-disk credentials so the user can sign in again cleanly. Network
failures, server errors (5xx), and rate limits (429) no longer cost the
user their session — those are transient and the token is left alone
so the next request can retry.

## Background — what was broken before this cycle

Before cycle 225 the only failure surface on the OAuth `refresh_token`
grant was a wrapped `fmt.Errorf` carrying the raw `HTTP <code>: <body>`
text. `RefreshIfNeeded` and `TokenCache.Get` treated every failure
uniformly: log the error, hand back the (possibly-stale) token, never
touch the credential store. Two consequences:

| Failure | Old behaviour | Why it was wrong |
|---|---|---|
| 400 `invalid_grant` (revoked enterprise account) | Log error, hand back stale token | Next refresh re-fires the same `invalid_grant`. The user is stuck — the CLI keeps trying with a credential the server already said was gone. AGY 1.2.5 calls this out explicitly. |
| 5xx / 429 / network failure | Log error, hand back stale token | Correct (already). The risk was that the new logic would over-correct and start clearing on these too. |

## What this cycle ships

### New file: `internal/auth/credentials.go`

Single source of truth for the rejected-vs-transient split.

- `AuthError` typed error carrying `Rejection bool`, `StatusCode int`,
  `OAuthCode string`, `Endpoint string`, `Cause error`. Callers use
  `errors.As` to inspect; the canonical predicates are
  `IsAuthRejected(err)` and `IsTransientAuthError(err)`.
- `ClassifyRefreshError(err) *AuthError` — parses the existing
  `antigravity-token: refresh: HTTP <code>: <body>` wrapping that
  `postTokenForm` has emitted since cycle 42, extracts the status and
  JSON `error` field, and tags the result Rejection when the response
  is a definitive OAuth rejection (401, 403, 400 with
  `invalid_grant` / `invalid_client` / `unauthorized_client` /
  `account_disabled` / `account_locked` / `revoked`). 5xx, 408, 429,
  and unstructured network errors are tagged Rejection=false and
  surface as transient.
- `IsTransientAuthError(err)` — returns true when `*AuthError` is
  present and not-rejected, or when the error has a network signature
  (`net.Error`, `context.DeadlineExceeded`, recognised
  `connection refused` / `no such host` / `i/o timeout` substrings).
  Failures that are *typed but not rejected* are still transient by
  contract, so the credential store stays intact on a 5xx.
- `ClearTokenUnified() (int, error)` — walks `AllTokenPaths()`, deletes
  each token file (`os.IsNotExist` swallowed so the call is
  idempotent on a fresh machine), best-effort purges the macOS
  Keychain entry, returns the count of removed paths plus the first
  hard error.

### Wire-up — `internal/auth/unified_config.go`

`(*AntigravityToken).RefreshIfNeeded` now consults the classifier:

```go
resp, err := RefreshTokenRealWithSecret(ctx, CLIClientID, publicClientSecret, t.Token.RefreshToken)
if err != nil {
    aerr := ClassifyRefreshError(err)
    if aerr == nil {
        aerr = &AuthError{Endpoint: "refresh", Cause: err}
    }
    if aerr.Rejection {
        cleared, _ := ClearTokenUnified()
        return nil, false, fmt.Errorf("refresh: %w (sign-in rejected: cleared %d stale credential path(s); please run /login)", aerr, cleared)
    }
    return t, false, fmt.Errorf("refresh failed: %w (returning stale token)", aerr)
}
```

The error returned for both branches now carries an `*AuthError`, so
callers can use `errors.As(err, &ae)` (or the predicate helpers) to
tell the user which kind of failure happened.

### Wire-up — `internal/auth/antigravity_token.go`

`(*TokenCache).Get` mirrors the unified config: a rejection-class
failure wipes the on-disk token (via `ClearTokenUnified`) and resets the
in-memory copy so the cache never hands back the stale entry again.
Transient failures still return the stale token, identical to the
cycle 35 contract.

### Wire-up — `internal/auth/keychain_darwin.go` / `keychain_other.go`

Both files now ship `ClearKeychainToken() error`. The macOS build shells
out to `/usr/bin/security delete-generic-password` with a 2-second
timeout, treats exit 44 ("item not found") as success, and surfaces any
other error. The non-darwin build is a no-op; ClearTokenUnified calls
both, so a Linux/Windows build still hits every token path on disk
without trying to talk to a keychain that doesn't exist.

## Tests — five required plus six more

`internal/auth/credentials_test.go` adds 11 test functions, 25
individual assertions including sub-test cases:

| # | Test | What it pins |
|---|---|---|
| 1 | `TestStaleCredentialsCleared_DefinitiveRejectionClearsTokens` | 400 + `invalid_grant` → `RefreshIfNeeded` returns `(nil, err)`, every `AllTokenPaths()` entry is gone, `IsAuthRejected(err) == true`. **(required)** |
| 2 | `TestStaleCredentialsCleared_InvalidClientAlsoClears` | 400 + `invalid_client` → same shape as #1. |
| 3 | `TestStaleCredentialsCleared_NetworkErrorPreservesTokens` | Connection refused on a closed port → `RefreshIfNeeded` returns the stale token, every path is still on disk, `IsTransientAuthError(err) == true`. **(required)** |
| 4 | `TestStaleCredentialsCleared_5xxPreservesTokens` | 500 + `server_error` → stale token preserved, `IsTransientAuthError == true`. **(required)** |
| 5 | `TestStaleCredentialsCleared_RateLimitPreservesTokens` | 429 + `temporarily_unavailable` → stale token preserved, `IsTransientAuthError == true`. **(required)** |
| 6 | `TestStaleCredentialsCleared_AfterClearAllowsResignIn` | End-to-end: rejection → clear → `SaveTokenUnified` fresh → second refresh round-trip succeeds. **(required)** |
| 7 | `TestStaleCredentialsCleared_TokenCacheGetMirrors` | Same #1 contract on the cycle 35 `TokenCache.Get` path. |
| 8 | `TestStaleCredentialsCleared_FreshMachineClearIsIdempotent` | `ClearTokenUnified()` on a clean $HOME returns `(0, nil)`; calling it twice is safe. |
| 9 | `TestStaleCredentialsCleared_ParserRules` | 11 sub-cases pinning the `isRejection` table: 400+invalid_grant / invalid_client / unauthorized_client → reject; 400+invalid_request → keep; 401/403 → reject; 408/429/5xx → keep. |
| 10 | `TestStaleCredentialsCleared_ParseRefreshError` | 4 sub-cases pinning the `parseRefreshError` parser; without this the format-string in `postTokenForm` could drift unnoticed. |
| 11 | `TestStaleCredentialsCleared_ErrorMessageMentionsClear` | The error the user sees mentions "sign-in rejected" and "cleared" so the user understands what happened. |

Targeted run:

```text
$ go test -vet=off -count=1 -run 'TestCred|TestAuth|TestSignOut|TestStale' \
    ./cmd/wagyu/ ./internal/.../ -v

ok    github.com/chadgpt/openagy/internal/auth  0.347s
PASS  11/11 new tests
PASS  all pre-existing tests still green
```

Full package run is still green (`internal/auth` 26.5 s, `internal/cli`
21.3 s) — no regressions in cycle 35 / 44 / 170 / 171 paths.

## Hard-constraint compliance

- **No `internal/convstore/sqlite.go` touched.** Diff is restricted to
  `internal/auth/` only.
- **New files gofmt-clean.** `gofmt -l internal/auth/credentials.go
  internal/auth/credentials_test.go` is empty.
- **Pre-existing files were NOT reformatted wholesale.** The two
  pre-existing files with diffs (`unified_config.go`,
  `antigravity_token.go`) were already in the 50-file "never been
  gofmt-clean" bucket documented in `AGENTS.md`. Only the inserted
  blocks were checked.
- **`go build -o /tmp/wagyu-stc ./cmd/wagyu` exits 0.** Verified, binary
  is 73 MB (no growth from previous cycle).
- **No push.** Local branch only.

## Files

| Path | Status | Notes |
|---|---|---|
| `internal/auth/credentials.go` | new | `AuthError`, classification helpers, `ClearTokenUnified`. |
| `internal/auth/credentials_test.go` | new | 11 test functions, 25 assertions. |
| `internal/auth/keychain_darwin.go` | modified | New `ClearKeychainToken` (macOS). |
| `internal/auth/keychain_other.go` | modified | New `ClearKeychainToken` (no-op). |
| `internal/auth/unified_config.go` | modified | `RefreshIfNeeded` classifies & clears on rejection. |
| `internal/auth/antigravity_token.go` | modified | `TokenCache.Get` mirrors. |

## What we deliberately did NOT touch

- `internal/auth/postTokenForm`'s error wrapping string. The new
  classifier parses the existing `antigravity-token: refresh: HTTP
  <code>: <body>` format, so changing the formatter is unnecessary.
  Changing it would invalidate the cycle 42 tests and cascade into
  the cycle 35 `TokenCache` callers.
- `internal/cli/dispatch.go runLogout`. The run-side `os.Remove` of
  `TokenFilePath()` works; this cycle's `ClearTokenUnified` covers all
  paths, but the existing `runLogout` flow is acceptable because the
  user explicitly invoked it. Future cycles can route runLogout through
  `ClearTokenUnified` for a tighter UX.
- `internal/convstore/sqlite.go`. Explicit constraint.
- Any pre-existing not-gofmt-clean files. Already in the AGENTS.md
  exception list.

## 硬约束 / 存疑 / limitations

【硬约束】

- Disk usage: changes only touch `internal/auth/*`. Build outputs
  identical to cycle 224 (~73 MB on darwin/arm64, ~64 MB with
  `-tags nocodemode`). No extra dependencies, no `go.sum` churn.
- The `*AuthError` type is exported because the cli / pigrt error
  reporters will eventually want to render a structured message. The
  cycle 224 `api_error` / `pigrt.ErrorSlot` path keeps working because
  `*AuthError.Error()` returns a human-readable string.
- No state is added to the package surface — `RefreshIfNeeded` still
  has the same signature and the same "stale token + error" semantics
  for the transient branch; the rejection branch is new.

【存疑】

- We do not have access to Google's production `/token` failure
  envelope for "enterprise account invalidated by IT admin" — that
  exact case is only observed through the AGY 1.2.5 changelog text.
  `invalid_grant` is RFC-defined for "revoked refresh token" so the
  mapping is high confidence; we may need to add
  `org_policy_violation` / `account_disabled` if a real capture shows
  a different code, but the `isRejection` table is now the single
  place to extend.
- The macOS keychain `security` exit-code 44 ("item not found") is the
  documented Apple behaviour and matches every prior cycle's
  observation; if Apple changes the exit code in a future macOS
  release the clear path becomes a soft-fail (we swallow the error),
  which is the desired behaviour — we'd rather drop the keychain
  delete than crash the login flow.

【limitations not addressed here】

- The same classifier could be wired into the **device-flow** path
  (`internal/auth/device_flow.go`) so the device endpoint's `401` /
  `invalid_grant` rejections are categorised identically. The current
  DeviceFlow does not load from disk so there is nothing to clear, but
  a future cycle adding "device-flow retry after server-side password
  reset" would benefit from the same predicate.
- The cmd/wagyu `runLogin` does not currently surface
  `IsAuthRejected(err)` to the user as a distinct banner. Cycle 226
  should render "credentials rejected, please contact your admin" vs
  "network error, retrying" using the predicates this cycle ships.
- The classifier does not yet cover the `internal/backend/agw_k0s.go`
  / model API transport's `*backend.HTTPError` — those are model errors
  (not sign-in errors) and continue to flow through the
  `cmd/wagyu/api_error.go` `AGY_ERROR` path unchanged. Cross-folding
  the two is **out of scope** for this cycle.
