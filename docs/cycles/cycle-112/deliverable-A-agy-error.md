# Cycle 112 — structured headless error reporting (`AGY_ERROR:` + exit code 3)

**Target:** `agy` 1.3.6 (`~/.local/bin/agy`,
SHA-256 `827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab`).

**Theme:** this cycle is driven by a **live capture of the reference's failure
path**, not by decompilation. The account quota is exhausted, so `agy -p` fails
deterministically on this machine and its exact stderr and exit status can be
read off directly.

---

## 1. Recovered evidence

### 1.1 The capture 【一手】

`agy -p 'say hi'`, quota exhausted, **rc 3**, stdout empty:

```
error: Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 89h28m34s.
AGY_ERROR: {"short_error":"RESOURCE_EXHAUSTED (code 429): Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 89h28m34s.","status":"RESOURCE_EXHAUSTED","error_code":429,"code_kind":"http","retryable":true,"error_id":"890cbab5-0640-47b4-8f5e-b3a650115e7a-1"}
```

So a headless turn that dies on a model API failure prints **two** stderr lines —
`error: <message>` then `AGY_ERROR: <json>` — and exits **3**.

### 1.2 The changelog entry 【一手】

> Improved headless (`-p` / `--prompt`) error reporting when a turn terminates on
> an agent or model API failure: the CLI now prints a structured `AGY_ERROR: {...}`
> JSON line on stderr with canonical status, HTTP or gRPC error code,
> retryability, and error ID (including HTTP status mapping for `GEMINI_API_KEY`
> SDK errors) and exits with code `3` instead of `1`.

### 1.3 Supporting literals 【一手】

| String | Role |
|---|---|
| `AGY_ERROR: ` | the line prefix (trailing space included) |
| `AGY_ERROR: {"short_error":%q}` | a second emission site for the simple form |
| `ShortError json:"short_error"` | the struct tag pair, from the `*printmode.…` type dump |
| `... (truncated)` | the long-body marker |
| `<title>` / `</title>` | HTML title extraction |
| `Your AI credits balance is too low to continue.` | the quota-without-credits message |

### 1.4 The body-to-message rules 【一手】

> Improved error messages: a request that fails with an HTML error page, for
> example from a corporate proxy, now shows the page title instead of the raw
> HTML, other long non-JSON error bodies are cut off after 500 bytes, and
> reaching your individual quota is reported right away instead of being retried.

## 2. Implementation

| File | Change |
|---|---|
| `internal/backend/http_error.go` (new) | `HTTPError{StatusCode, Body, URL}`, `Message()`, `HTMLTitle()`, `MaxErrorBody = 500`, `truncateErrorBody()` |
| `internal/backend/agw_k0s.go` | a non-2xx response now returns `*HTTPError` instead of a pre-formatted string; `OPENAGY_AGW_URL` overrides the endpoint for local verification |
| `internal/agent/loop.go` | `streamOnce` now returns `chunk.Error` (a mid-stream failure used to be swallowed, leaving `Send` returning nil and the process exiting 0) |
| `cmd/antigravity/api_error.go` (new) | `apiError` (JSON field order `short_error`, `status`, `error_code`, `code_kind`, `retryable`, `error_id`), `classifyAPIError`, `statusName`, `retryableStatus`, `Lines()`, `newErrorID()` |
| `cmd/antigravity/main.go` | `runPrintMode` returns `cli.ExitCodeError(3, api.Lines())` for a classified API failure; the `[error] …` stdout line is removed |

`HTTPError.Message()` follows the reference's order of preference: a JSON body
contributes `error.message` then `message`; an HTML body contributes its
`<title>`; anything else is used verbatim, cut to 500 bytes with the recovered
`... (truncated)` marker. An empty body falls back to the status text.

`newErrorID()` returns an RFC 4122 v4 UUID with the reference's `-1` suffix.

## 3. Verification

* `go build ./cmd/antigravity`, `go vet` — clean; `gofmt` clean.
* `go test ./cmd/... ./internal/...` — all packages pass.
* New tests: `TestHTTPErrorMessage_JSON` / `_HTMLTitle` / `_Truncated` / `_Empty`,
  `TestHTTPErrorError`, `TestHTMLTitle_NoTitle`;
  `TestClassifyAPIError_QuotaCapture` (reproduces the captured line pair
  byte-for-byte with the error id pinned), `TestClassifyAPIError_NotAPIError`,
  `TestStatusName`, `TestRetryableStatus`, `TestAPIErrorJSONShape` (field order),
  `TestNewErrorID`.
* **Live end-to-end against a scripted endpoint.** `OPENAGY_AGW_URL` points the
  client at a local HTTP server, so every branch is exercised through the real
  binary. Reproducing the reference capture:

  ```
  $ OPENAGY_AGW_URL=http://127.0.0.1:8791/v1/chat/completions agy -p 'say hi'
  rc=3
  error: Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 89h28m34s.
  AGY_ERROR: {"short_error":"RESOURCE_EXHAUSTED (code 429): Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 89h28m34s.","status":"RESOURCE_EXHAUSTED","error_code":429,"code_kind":"http","retryable":true,"error_id":"035d3208-a2d2-44d6-bcd3-d84a6af5b3a5-1"}
  ```

  This is **byte-identical to the reference** except for the random `error_id`.

  * 502 + a 900-byte HTML body →
    `error: 502 Bad Gateway - corporate proxy` /
    `"status":"UNAVAILABLE","error_code":502`, rc 3 (title extracted, not the
    markup).
  * 500 + a 900-byte plain body → the message is exactly **500 bytes** followed by
    `... (truncated)`, `"status":"INTERNAL"`, rc 3.
  * 503 + `{"message":"plain json message"}` → the top-level `message` is used.

* `harness136.sh` — **86/87** (the documented `/plugin` pre-auth diff).
* `harness137.sh` — **23/25** (both documented cycle-101 diffs). Intermediate runs
  during this cycle read 22/25 and 14/25 purely because the reference itself
  timed out (`124`) on more cases — the quota degradation documented in earlier
  cycles. Rows where the reference's `rc` is `124` and our `stdout` still matches
  are noise, not regressions.

## 4. Divergences and limitations

* **Only `*backend.HTTPError` is classified.** A transport failure (DNS,
  connection reset, TLS) keeps the ordinary exit path (rc 1), because the
  reference's `error_code` / `code_kind` for a non-HTTP failure is unrecovered.
  The changelog's `code_kind` field implies a `"grpc"` variant exists, but no
  capture of one was obtained.
* **The status-name table beyond 429 is 【推测】.** `429 → RESOURCE_EXHAUSTED` is
  from the capture; 400/401/403/404/408/409/412/499/5xx follow the usual
  HTTP↔gRPC correspondence. The changelog's mention of "per-minute `429` rate
  limits" and "`502`, `503`, `504`" as the retryable class is consistent with
  `retryableStatus`, but only 429 was observed with `retryable: true`.
* **`error_id`'s `-1` suffix is unexplained.** The capture shows
  `<uuid>-1`; whether the suffix is an attempt counter, a shard index or a
  constant is unknown, so the clone always emits `-1`.
* **`Your AI credits balance is too low to continue.` is not implemented.** It is
  the message for "plan quota used up and AI credits cannot cover the request",
  which needs the credits balance the clone does not track.
* **The `AGY_ERROR: {"short_error":%q}` literal's second emission site is
  unused.** The capture's richer form (all six fields) is the one reproduced;
  when the simpler form is emitted is unknown.
* **`OPENAGY_AGW_URL` is an openagy-only dev override** (alongside the existing
  `OPENAGY_AGW_KEY`). It exists so the failure paths can be verified against a
  local endpoint; the reference has no equivalent.
* **The `[error] …` stdout line was removed.** It was a cycle-33 observability
  artefact; the reference never writes a failure to stdout on the `-p` path.
* **Mid-stream failures now fail the turn.** Previously a mid-stream
  `StreamChunk.Error` was reported to the handler and then ignored, so `Send`
  returned nil and the process exited 0 with an `[error] …` line on stdout. This
  is a behaviour change beyond the API-error report, and it is the more faithful
  one: the reference's exit code 3 applies to "a turn terminates on an agent or
  model API failure".
* **Still cycle-77 stubs (~8):** `/keybindings`, `/voice`, `/resume`, `/rewind`,
  `/fork`, `/feedback`, `/artifact`, `/btw`, `/context`, `/plugin`.
