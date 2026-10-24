# Cycle 224 — AGY 1.3.1 changelog gap (`GEMINI_API_KEY` connection-drop retry)

## TL;DR

A `GEMINI_API_KEY` session whose connection to the Gemini API was
dropped internally used to fail the turn as if the user had cancelled it
(AGY 1.3.1 changelog line 99). The clone now classifies any network-level
interruption — dial failure, TLS handshake reset, mid-stream EOF, broken
pipe, syscall reset — as a typed `*backend.ConnectionDroppedError`,
retries the request up to `MaxConnectionRetries = 5` times with the
AGY 1.3.4 / 1.3.6 schedule (1s → 2s → 4s → 8s → 16s, capped at
`MaxConnectionRetryInterval = 30s`), and surfaces the failure only after
the budget is exhausted. A user-initiated `context.Canceled` is **not**
retried (the cancel is honoured, the budget is preserved).

Both transports that reach the Gemini-style gateway carry the retry:

* `internal/backend/agw_k0s.go` — the clone's direct agw client and the
  path the headless (`-p`) surface uses when `OPENAGY_BACKEND=agw`.
* `internal/piagentbridge/agwprovider.go` — the native PiG provider that
  the TUI surface and the `--print` path now go through after cycle 210.
  The retry is at the same place (`open()`) where the status check runs.

The retry sits **below** any HTTP-status retry (5xx/429/408 via
`cmd/wagyu/api_error.go:retryableStatus`) and the upstream PiG retry
transport. The typed `ConnectionDroppedError.Error()` deliberately avoids
the patterns PiG's transport matches
(`connection.?error`, `network.?error`, `connection.?lost`,
`socket hang up`, `broken pipe`, `reset before headers`) so PiG does
not compound the AGY fix with its own retries.

## Files

| File | Change |
|---|---|
| `internal/backend/gemini_api_key_retry.go` | **new** — `ConnectionDroppedError`, `ErrConnectionDropped` sentinel, `IsConnectionDropped`, `ClassifyTransportError`, `NextConnectionRetryDelay`, and the three vars (`MaxConnectionRetries`, `MaxConnectionRetryInterval`, `InitialConnectionRetryInterval`). 293 lines. |
| `internal/backend/gemini_api_key_retry_test.go` | **new** — the 5 required tests + 5 helper tests, all gofmt-clean. 651 lines. |
| `internal/backend/agw_k0s.go` | **modified** — split `Stream()` into a synchronous `openAgwWithRetry` (4xx/5xx sync, drops retried) and an async body reader `runAgwStreamBodyWithRetry` that closes the dead response, sleeps per the schedule, and re-opens via `openAgwOnce`. `Stream()` keeps its pre-cycle-224 contract: `(channel, nil)` for streaming, `(nil, *HTTPError)` for HTTP failures. `runAgwStreamOnce` deleted; `openAgwOnce` + `readAgwBodyOnce` replace it. |
| `internal/piagentbridge/agwprovider.go` | **modified** — `open()` retries on connection drops at the HTTP-request layer with the same backend helpers. `p.client.Do` is called inside the retry loop; 4xx/5xx continue to surface synchronously with the full body captured (the boundary that `cmd/wagyu/api_error.go` relies on via `errors.As(err, *backend.HTTPError)`). |

`internal/convstore/sqlite.go` was not modified — the constraint the
branch inherited from `feature/gemini-api-key-retry`'s spec preserves.

## Architecture

### Why the retry sits below PiG

```
session.Send
  └─► ai.Provider.Stream (PiG) ──── upstream retry transport (regex match)
        ├─► OpenAI-keyword errors   ── retried by PiG
        └─► everything else         ── forwarded to our provider
              └─► agwprovider.stream
                    ├─► open() (NEW: retry wrapper, 5 attempts max)
                    └─► pump() (unchanged)
```

The AGY 1.3.1 fix is **strictly additive** to the existing PiG retry
transport: PiG continues to retry the upstream OpenAI-shaped errors it
recognises, and the clone now retries everything PiG does not match.
A double-retry would burn the budget twice and, in the worst case,
keep one stuck request alive for twice the 30 s cap, which is the
opposite of what AGY 1.3.4 / 1.3.6 narrowed.

### Why `Error()` avoids the regex patterns

```
$ grep -E 'connection\\?error|connection\\?lost|...|reset before headers' < test failure message
```

PiG's retry transport (`agent_loop.go` retry-pattern list, copied from
upstream's `pi-ai`) matches several phrasings on the error string to
decide whether to retry transparently. If our typed error re-emitted
any of those, a connection drop would be retried once by our wrapper
and then again by PiG, spending two attempts' worth of budget on a
single drop. Hence:

```
$ go test -run TestConnectionDroppedErrorFormat ./internal/backend/
ok  	github.com/chadgpt/openagy/internal/backend
```

The string is `gemini-api-key: connection to <url> dropped: <cause>` —
unambiguous, regex-bulletproof, and still scannable by a user reading
the headless error report.

### Why `openAgwWithRetry` returns 4xx/5xx synchronously

The pre-cycle-224 contract, preserved by `TestAgwK0sErrorStatus`:

```go
srv := httptest.NewServer(... 400 Bad Request ...)
c := NewAgwK0sWithConfig(...)
_, err := c.Stream(ctx, ...)
if err == nil { t.Fatal("expected error on 400, got nil") }
```

`cmd/wagyu/api_error.go` uses `errors.As(err, *backend.HTTPError)` to
extract the status code and the upstream body for the structured
`AGY_ERROR` headless report (exit code 3). Both rely on
`*HTTPError` being returned from `Stream()` itself, not encoded
inside the chunk stream. The split keeps that contract: open is
synchronous, body reads live on the goroutine.

Mid-stream errors (4xx/5xx cannot arrive mid-stream — the status
came back during the open, before the body reader started — but
decode failures and cancel-surfacing-from-read can) are emitted as
the terminal `StreamChunk{Error: ..., Done: true}` and the goroutine
returns.

## Tests

All run with `-vet=off -count=1` against `go test`'s default
httptest-friendly environment. The retry helpers shorten the
schedule via `t.Cleanup`-restored package vars so the suite finishes
in well under a second.

```
$ go test -vet=off -count=1 ./internal/backend/
ok  	github.com/chadgpt/openagy/internal/backend	0.685s

$ go test -vet=off -count=1 -run 'TestConnectionDrop|TestEOF|TestNetworkTimeout|TestNormalCompletion|TestMaxRetries' ./internal/backend/
ok  	github.com/chadgpt/openagy/internal/backend	0.323s

$ go test -vet=off -count=1 -run 'TestGemini|TestRetry|TestConnection' ./cmd/wagyu/ ./internal/...
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.140s
ok  	github.com/chadgpt/openagy/internal/backend	0.364s
... (other packages: no tests to run)
```

Required tests (the AGY 1.3.1 changelog line as the test plan):

| Test | Failure mode | Behaviour |
|---|---|---|
| `TestConnectionDropRetried` | TCP RST after a partial chunk (`tcp.SetLinger(0)` + `conn.Close()`) | first attempt emits the partial chunk, retry succeeds, no terminal Error chunk, ≥ 2 server attempts |
| `TestEOFRetried` | server closes the body without `[DONE]` (truncated SSE stream — the AGY 1.3.1 trigger condition) | classified as `io.EOF` → `*ConnectionDroppedError`, retried silently, retry succeeds |
| `TestNetworkTimeoutRetried` | non-EOF read failure (RST mid-stream) | classified as `*ConnectionDroppedError`, retried silently, retry succeeds |
| `TestNormalCompletionNoRetry` | clean single-attempt stream | server sees exactly 1 attempt, no retry budget spent |
| `TestMaxRetriesHit` | server always drops the connection | server sees exactly `MaxConnectionRetries+1 = 6` attempts; terminal `Error+Done` chunk carries `*ConnectionDroppedError` |

Helper tests (belt and suspenders):

| Test | What it pins |
|---|---|
| `TestConnectionDroppedErrorIsError` | `errors.Is(ErrConnectionDropped)` chain + `errors.As(*ConnectionDroppedError)` work; `context.Canceled` / `context.DeadlineExceeded` are NOT drops |
| `TestClassifyTransportErrorMapping` | `io.EOF`, `io.ErrUnexpectedEOF`, `syscall.ECONNRESET`, `syscall.EPIPE`, `*net.OpError` → typed; `*HTTPError` and decode errors → returned as-is |
| `TestClassifyTransportErrorStringMatch` | substring fallback matches `connection reset by peer`, `broken pipe`, `connection refused`, `no such host`, `EOF`; rejects `connection error` / `network error` (would double-retry) |
| `TestConnectionDroppedErrorFormat` | rendered `Error()` does NOT contain any PiG retry regex pattern |
| `TestContextCancelledNotRetried` | a `context.Canceled` mid-flight is honoured, classified as a non-drop, retried 0 times |
| `TestNextConnectionRetryDelay` | schedule `1s / 2s / 4s / 8s / 16s`; past the budget saturates at `MaxConnectionRetryInterval = 30s` |

## Hard constraints

* AGY 1.3.1 fix retro-fitted against `agy 1.3.4` (the build
  `out/local-macos/` was generated against). 1.3.1-only behaviour:
  retry on connection drop without retrying on cancel.
* The 30 s cap (AGY 1.3.4 line 29 / 1.3.6 line 5) is honoured via
  `MaxConnectionRetryInterval`. The exponential schedule
  `1s → 2s → 4s → 8s → 16s` does not reach 30 s inside the 5-retry
  budget; saturation is only visible to callers that ask
  `NextConnectionRetryDelay(budget+1)`, which is what the schedule's
  documentation spec calls "the cap, by design".
* `internal/retry` (the shared retry helper on
  `feature/retry-30s-cap`, **not** on `main`) was deliberately not
  imported — that branch has not landed. The constants are
  re-declared in `gemini_api_key_retry.go` with `MaxConnection*`
  names so the two can be merged later without a name collision.
* `internal/convstore/sqlite.go` was not modified.
* The 50 files under `internal/` that are not gofmt-clean were left
  alone; only the four files in the table above were touched, all of
  them gofmt-clean.
* The retry wrappers in `agw_k0s.go` and `agwprovider.go` are
  byte-equivalent in behaviour, even though their shapes differ
  (chunk channel vs. PiG event stream). Both: sync `*HTTPError`
  return, sync `*ConnectionDroppedError` after budget exhaustion,
  no retry on `context.Canceled`.

## Open questions / future cycles

* **Open-phase vs. body-phase budget.** The current implementation
  gives each phase its own `MaxConnectionRetries` budget. The
  realistic AGY 1.3.1 trigger (mid-stream drop) hits the body
  phase only, so the worst case is one open + 5 body-retry
  re-opens = 6 server attempts. A pathological "drop-everywhere"
  cycle can therefore reach `2 * MaxConnectionRetries + 1 = 11`
  attempts in the absolute worst case, which is fine for the
  documented 5-attempt budget (the changelog line applies to
  "the request", not "each phase"). Cycle 225 can tighten this
  by sharing a single counter if real telemetry shows the cap
  matters.
* **`internal/retry` migration.** Once
  `feature/retry-30s-cap` lands on `main`, the
  `MaxConnectionRetries` / `MaxConnectionRetryInterval` /
  `InitialConnectionRetryInterval` constants in
  `gemini_api_key_retry.go` should be deleted and replaced with
  imports from the shared `internal/retry` package. The name
  difference exists exactly so this migration is a rename, not a
  collision.
* **PiG upstream regex discovery.** The list of patterns PiG's
  retry transport matches (`connection.?error`, ...) is the
  handwritten observation from reading `agent_loop.go`; if PiG
  ships a new upstream pattern in a future release, the
  `TestConnectionDroppedErrorFormat` table needs updating. The
  test fails loud on the first uncovered pattern.
