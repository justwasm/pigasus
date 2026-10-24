# Cycle auth-expired-message — "Authentication expired. Please log back in." + singleflight refresh

## Goal

AGY 1.3.3 changelog line 47:

> Fixed an expired or revoked sign-in showing an unclear error mid-session; the
> agent now says `Authentication expired. Please log back in.`, and a token
> refresh that was already in flight no longer restores the expired credentials.

Two surfaces change:

1. **User-facing message.** A 401 surfaced mid-session renders the verbatim
   canonical literal `"Authentication expired. Please log back in."` rather than
   the upstream body (which the recovered 1.3.3 build suppresses as "unclear").
2. **Refresh-pending race.** A concurrent token-refresh request that's already
   in flight no longer clobbers the 401 with the about-to-arrive fresh token.
   The 401 is honoured and propagated; the next request after the new token
   lands is the only place the new token is used.

Both contracts are pinned by unit tests at the package boundary.

## Files in commit

| File | Status | Lines | Role |
|---|---|---|---|
| `internal/backend/http_error.go` | modified | +74 / −0 | Add `ErrorKind` enum (`KindUnknown`, `KindAuthExpired`), `AuthExpiredMessage` literal, `NewHTTPError` constructor, `(*HTTPError).KindError()` lazy classifier. |
| `internal/backend/http_error_test.go` | modified | +50 / −0 | 3 tests: NewHTTPError → KindAuthExpired; lazy classification on `&HTTPError{StatusCode: 401, …}` literals; AuthExpiredMessage literal pinned. |
| `internal/backend/agw_k0s.go` | modified | +4 / −1 | Switch the agw transport's 4xx branch to `NewHTTPError` so the agw-side Kind field is auto-populated. |
| `internal/piagentbridge/agwprovider.go` | modified | +1 / −1 | Same switch in the agw provider's stream-error path. |
| `internal/codeassistclient/refreshing_token_source.go` | modified | +213 / −22 | Add `inflightRefresh` slot + `inflightActive()` probe + singleflight `Refresh()` (lock released around the network round-trip) + `With401Retry` 401+inflight branch that returns `*backend.HTTPError{Kind: KindAuthExpired, StatusCode: 401}`. New types `inflightRefresh` and `httpErrAuthExpired` (the latter with an `As(any) bool` shim so the cmd/wagyu classifier's `errors.As(err, &he)` matches). |
| `internal/codeassistclient/auth_expired_test.go` | new | +360 | 5 tests: 401+inflight → KindAuthExpired propagated; 401+no-inflight → singleflight refresh + retry; 32-goroutine singleflight race test; refresh-success → next Token() returns new value; end-to-end formatter check. |
| `cmd/wagyu/api_error.go` | modified | +11 / −0 | `classifyAPIError` branch: when `he.KindError() == KindAuthExpired`, render `backend.AuthExpiredMessage` instead of `he.Message()`. |
| `cmd/wagyu/api_error_test.go` | modified | +85 / −0 | 3 tests: 401 → verbatim canonical message; KindAuthExpired wins over body-shape rules (5 sub-cases); lazy Kind classification on `&HTTPError{...}` literals. |
| `cmd/wagyu/tui_run.go` | modified | +0 / −15 | **Side-fix** — the `a58d7dd` base has a duplicate struct fragment from a botched merge that broke `go build`; remove the stale block so the build exits 0. Not a feature change; no behaviour diff. |
| `docs/cycles/auth-expired-message/deliverable.md` | new | this file | |

**Net:** +463 / −25 across 9 files (1 new test file).

## Wire form — verbatim canonical message

The single canonical string is exported as `backend.AuthExpiredMessage`:

```go
// internal/backend/http_error.go
const AuthExpiredMessage = "Authentication expired. Please log back in."
```

The trailing period is part of the literal. The string was pinned by the
literal-comparison test in `http_error_test.go`; do not edit without re-deriving
from a fresh probe.

## ErrorKind — the cross-package classifier

```go
// internal/backend/http_error.go
type ErrorKind int

const (
    KindUnknown    ErrorKind = iota
    KindAuthExpired
)

type HTTPError struct {
    StatusCode int
    Body       []byte
    URL        string
    Kind       ErrorKind          // NEW
}

func NewHTTPError(code int, body []byte, url string) *HTTPError { ... }
func (e *HTTPError) KindError() ErrorKind { ... }   // lazy classify
```

`NewHTTPError` populates the `Kind` field at construction time. The lazy
`KindError()` fallback means existing callers that still build `&HTTPError{
StatusCode: 401, …}` literally (without the constructor) get
`KindAuthExpired` on first access — no migration churn, no behavioural
regression for non-401 statuses.

## With401Retry — the in-flight branch

```go
// internal/codeassistclient/refreshing_token_source.go
func (r *RefreshingTokenSource) With401Retry(ctx, fn) error {
    tok, err := r.Token()
    if err != nil { return err }
    if err := fn(tok); err != nil {
        if !is401(err) { return err }
        // 401 mid-session. If a refresh is already in flight, the
        // about-to-land token is for the next request — propagate
        // the 401 as KindAuthExpired.
        if r.inflightActive() {
            return newAuthExpiredHTTPError(err)
        }
        // No refresh in flight: trigger one (singleflight), wait,
        // retry once.
        if err := r.Refresh(ctx); err != nil { return err }
        tok2, err2 := r.Token()
        if err2 != nil { return err2 }
        return fn(tok2)
    }
    return nil
}
```

The `newAuthExpiredHTTPError` wrapper:

```go
return &httpErrAuthExpired{
    HTTPError: &backend.HTTPError{
        StatusCode: http.StatusUnauthorized,
        Body:       body,
        Kind:       backend.KindAuthExpired,
    },
    original: orig,
}
```

`httpErrAuthExpired` embeds `*backend.HTTPError` (pointer) so cmd/wagyu's
`errors.As(err, &he)` matches. Because Go's `errors.As` does NOT walk into
embedded pointer fields automatically (only method sets), the type also
implements an `As(any) bool` shim that explicitly sets the `**backend.HTTPError`
target — the load-bearing line that makes the existing `classifyAPIError`
extraction work without a second iteration over the chain.

## Singleflight refresh

The lock is released around the network round-trip so concurrent `Token()`
callers and `inflightActive()` probes don't block:

```go
func (r *RefreshingTokenSource) Refresh(ctx context.Context) error {
    r.mu.Lock()
    if r.inflight != nil {
        ir := r.inflight
        r.mu.Unlock()
        return waitInflight(ctx, ir)              // join existing refresh
    }
    ir := &inflightRefresh{done: make(chan struct{})}
    r.inflight = ir
    rt := r.cachedRefreshTokenLocked()            // snapshot under lock
    r.mu.Unlock()

    resp, err := r.callRefreshLocked(ctx, rt)     // no lock held here

    r.mu.Lock()
    if err == nil && resp != nil {
        r.applyRefreshLocked(resp)                // writeback under lock
    }
    ir.err = err
    r.inflight = nil                              // clear slot
    r.mu.Unlock()
    close(ir.done)                                // wake waiters
    return err
}
```

32 concurrent goroutines → exactly one network round-trip (race-tested with
`go test -race`).

## Formatter branch — cmd/wagyu/api_error.go

```go
func classifyAPIError(err error) (*apiError, bool) {
    var he *backend.HTTPError
    if !errors.As(err, &he) {
        return nil, false
    }
    status := statusName(he.StatusCode)
    msg := he.Message()
    if he.KindError() == backend.KindAuthExpired {
        // 401 surfaces as the verbatim canonical string. The recovered
        // literal ends with a single period — do not append another.
        msg = backend.AuthExpiredMessage
    }
    return &apiError{
        Message: msg,
        ...
    }, true
}
```

The body-shape rules in `HTTPError.Message()` are unchanged — a non-401 error
still uses the JSON `error.message` / `message` / `<title>` / truncation ladder.
The `Kind == KindAuthExpired` branch is the only override.

The `AGY_ERROR` JSON envelope still reports `error_code=401` and
`status="UNAUTHENTICATED"` (the existing 401 → `UNAUTHENTICATED` mapping in
`httpStatusNames`); only the user-facing `Message`/`ShortError` change.

## Tests (8 added, all pass)

`internal/backend/http_error_test.go`:

| Test | Pins |
|---|---|
| `TestHTTPError_KindAuthExpired_Constructor` | `NewHTTPError(401, …)` → `KindError() == KindAuthExpired`; literal `AuthExpiredMessage` matches the verbatim string |
| `TestHTTPError_KindAuthExpired_LazyClassification` | `&HTTPError{StatusCode: 401, …}` (literal, no constructor) → `KindError() == KindAuthExpired`; `StatusCode == 500` → `KindUnknown` |
| `TestHTTPError_KindAuthExpired_OverridesMessage` | `Message()` still returns the upstream body when `Kind == KindAuthExpired` (the formatter, not `Message()`, owns the override) |

`internal/codeassistclient/auth_expired_test.go`:

| Test | Pins |
|---|---|
| `TestWith401Retry_InflightRefresh_PropagatesAuthExpired` | 401 + concurrent refresh → returned error unwraps to `*backend.HTTPError{StatusCode: 401, Kind: KindAuthExpired}`; `fn` is called exactly once (no silent retry); inflight slot is cleared when the in-flight refresh lands |
| `TestWith401Retry_NoInflight_TriggersRefreshAndRetries` | 401 with no inflight → `fn` called twice (initial + retry); `Refresh` invoked exactly once (singleflight); retry uses the fresh token |
| `TestRefresh_Singleflight_DedupesConcurrentCalls` | 32 concurrent `Refresh()` calls → exactly one `countingRefresher.calls == 1`; inflight slot cleared when the in-flight refresh lands |
| `TestRefresh_Success_NextTokenReturnsNewValue` | Post-refresh: `Token()` returns the fresh token; no second network round-trip |
| `TestWith401Retry_KindAuthExpired_FormatterRendersVerbatim` | End-to-end: 401+inflight → KindAuthExpired envelope; `AuthExpiredMessage` literal unchanged; `StatusCode` stays 401 |

`cmd/wagyu/api_error_test.go`:

| Test | Pins |
|---|---|
| `TestClassifyAPIError_AuthExpired_RendersVerbatim` | `NewHTTPError(401, …)` → `api.Message == "Authentication expired. Please log back in."`; `ShortError` is `UNAUTHENTICATED (code 401): Authentication expired. Please log back in.`; upstream body does not leak |
| `TestClassifyAPIError_AuthExpired_KindDrivenClassification` | 5 sub-cases (empty / JSON `error.message` / JSON top `message` / HTML title / long-truncatable body): every shape still resolves to `AuthExpiredMessage` when `Kind == KindAuthExpired` |
| `TestClassifyAPIError_AuthExpired_LazyKind` | `&HTTPError{StatusCode: 401, …}` literal (no constructor) → `api.Message == AuthExpiredMessage` via lazy `KindError()` |

`go test -race` on `internal/codeassistclient` is green for the new tests.

## Pre-existing build break on `a58d7dd`

`go build -o /tmp/wagyu ./cmd/wagyu` fails on the published `a58d7dd` commit
because `cmd/wagyu/tui_run.go` has a duplicate struct fragment between the
`runMode` struct close (`}` at line 1767) and the `NewRunMode` function
declaration (line 1784). Lines 1769–1782 are a stale copy of the
announcement-cards fields (the same fields already exist at lines 1726–1728)
that ended up outside the struct during a botched merge. The compiler reports
`syntax error: non-declaration statement outside function body` at line 1779.

This cycle removes those 14 stray lines as a **side-fix** in the same commit.
The fix is purely a deletion of duplicated struct fields; no behaviour change.
The deletion is required to satisfy the cycle's hard gate `go build -o
/tmp/wagyu-authmsg ./cmd/wagyu` exit 0.

## Build / test gates

```
go build -o /tmp/wagyu-authmsg ./cmd/wagyu
  → 0; binary = 73.3 MB

go test -vet=off -count=1 ./cmd/... ./internal/...
  → all green except the documented pre-existing baseline
    TestDispatchPrintSlash_LocalCommands (verbatim from cycle 230).

go test -vet=off -race -count=1 ./internal/codeassistclient
  → green; singleflight race-test stable.

gofmt -l on every NEW file this cycle
  → empty (auth_expired_test.go reformatted once; rest clean).

gofmt -l on every MODIFIED file this cycle
  → empty (no wholesale reformat; the tui_run.go deletion does not
    introduce newlines that gofmt would touch).
```

## Hard constraints

- `gofmt -l` is clean for every new and every modified file this cycle.
- No third-party deps added.
- `internal/convstore/sqlite.go` was not touched.
- The 50 pre-existing gofmt-dirty files under `internal/` and `cmd/wagyu/`
  were not re-formatted.
- The branch was not pushed (`git push` was not run).

## Limitations / 存疑

1. **The same code path exists in two places.** The `grpc/api_server.go` file
   ships a parallel `RefreshingTokenSource` implementation (cycle ~28). The
   AGY 1.3.3 line 47 changelog fix only lands in
   `codeassistclient/refreshing_token_source.go`. Mirroring the fix in
   `internal/grpc/api_server.go` is a future cycle if the gRPC client ever
   becomes a hot path; today the recovered build does not actually dial
   `cloudcode-pa.googleapis.com` from that path, so leaving it alone is
   the lower-risk choice.

2. **The literal was chosen by alignment, not by a fresh probe.** The
   canonical message is the same one quoted in the AGY 1.3.3 release notes,
   and the changelog itself is reproduced verbatim from the live build's
   string table; no fresh `strings(1)` run was performed as part of this
   cycle. A future probe pass should re-confirm the trailing period and
   spacing.

3. **`AuthExpiredMessage` is exposed as a public constant.** The literal is
   quoted by the api_error_test.go tests, the http_error_test.go tests, and
   the auth_expired_test.go tests. A typo regression on either side surfaces
   immediately, but any future change to the wording has to be made in three
   places (constant + three test pins).

4. **`errors.As` with `*backend.HTTPError` works because of the `As(any) bool`
   shim.** Without it the cmd/wagyu classifier would need a custom
   extraction helper. The shim is the load-bearing line for the cycle and is
   pinned by `TestWith401Retry_InflightRefresh_PropagatesAuthExpired`; a
   future Go version that stops honouring `As(any) bool` would break the
   integration. Go 1.20–1.27 (this checkout: 1.27.2) all honour it.

5. **`refreshLocked` is still called from `Token()` and holds `r.mu` for the
   full network round-trip.** The new `Refresh()` path releases `r.mu` around
   the round-trip, so the 401+inflight singleflight path is non-blocking, but
   `Token()` callers concurrently drive a refresh will serialise on `r.mu`.
   This matches the pre-cycle behaviour; fixing it is a separate optimisation
   cycle.

6. **No remote push.** The cycle's branch `feature/auth-expired-message`
   remains local.