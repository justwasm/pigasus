# Cycle 263 — OAuth scope error includes `/logout` + `/login` hint (AGY 1.4.x)

## Why this cycle

AGY 1.4.x release note (`internal/cli/testdata/changelog_reference.txt`
line 271):

> Improved authentication error messages when OAuth tokens lack
> required scopes by including explicit instructions to run `/logout`
> and `/login`.

In the clone, an OAuth-token-rejection arriving from the model API
as a non-2xx response falls through `backend.HTTPError` (the typed
non-2xx response used by the agw transport). The headless formatter
(`cmd/wagyu/api_error.go`) renders a structured `AGY_ERROR: {...}`
JSON line on stderr with the canonical status / HTTP code / retryability
/ error-id. The user-facing `error:` line carries the upstream body
verbatim and a `error: <upstream body>` for the TUI. None of those
paths say "your OAuth grant is missing scope X — refresh it." The
freshly-rejected user does not know that `/logout` + `/login` is the
remediation.

The fix is a precise scope-mismatch detector (status 403 + scope
keyword scan) plus the canonical hint constant. The hint is appended
to the user-facing message in **print mode** (the `error:` line) and
to a new `remediation` field on the AGY_ERROR JSON envelope; the
**TUI** path mirrors the change by forwarding `chunk.Error` text
into `m.response` via `backendChunkMsg`, with the same conditional
hint append.

## AGY 1.4.x spec (this is what the cycle pins)

| Property | Value | Source in the cycle |
|---|---|---|
| Hint text | `Run `/logout` then `/login` to refresh your OAuth grant with the required scopes.` | `backend.OAuthScopeHintMessage` |
| Detection: status | HTTP 403 only | `HTTPError.IsScopeMismatch` `StatusCode != http.StatusForbidden` guard |
| Detection: keyword | `insufficient_scope` / `missing scope` / `requires scope` / `scope required`, case-insensitive | `scopeKeywords` slice, lower-cased body + lower-cased `Message()` fallback |
| Detection: precision | The hint is NOT emitted on any 403 — only 403-with-keyword qualifies | predicate named `IsScopeMismatch` (not projected onto a static `Kind` value) |
| Detection: helper | `IsScopeMismatchErr(err error) bool` walks `errors.As` chain | `cmd/wagyu/api_error.go:107`, `internal/agent/*`, `internal/pigrt/*` |
| Print-mode surface — JSON | new `remediation` field, `omitempty`, after `error_id` | `apiError.Remediation`, set only on IsScopeMismatch |
| Print-mode surface — user-facing | `error: <upstream body> <hint>` line | `classifyAPIError` short_error + message |
| TUI surface | `chunk.Error` text forwarded into `m.response` via `backendChunkMsg`, hint appended when in scope | `cmd/wagyu/tui_run.go:4057-4080` |
| Backwards compatibility | `remediation` field omitempty — every other failure mode renders the same JSON shape as before | `json:"remediation,omitempty"` |

## What this cycle changes

| File | Status | What |
|---|---|---|
| `internal/backend/http_error.go` | modified | adds `OAuthScopeHintMessage` constant, `scopeKeywords` list, `(e *HTTPError) IsScopeMismatch()` method, `(err error) IsScopeMismatchErr()` helper |
| `internal/backend/scope_hint_test.go` (new) | added | 6 predicate + wire tests (`TestScopeHint_403InsufficientScope`, `_403MissingScopeKeyword`, `_403Generic`, `_401NotEnough`, `_CaseInsensitive`, `_PreservesOriginalError`) |
| `cmd/wagyu/api_error.go` | modified | adds `Remediation` field to `apiError`; `classifyAPIError` appends hint to message + sets Remediation when `IsScopeMismatch` is true |
| `cmd/wagyu/api_error_test.go` | modified | adds `TestScopeHint_PrintModeRemediation` (envelope check) and `TestScopeHint_NonScopeErrorHasNoRemediation` (4 negative sub-cases) |
| `cmd/wagyu/tui_run.go` | modified | TUI `agent.New` handler now forwards `chunk.Error` text into `m.response` via `backendChunkMsg`, appending the hint when scope mismatch. Adds `"errors"` import. |
| `internal/cli/testdata/feature_diff_table.json` | modified | new entry `OAuth scope error includes /logout + /login hint` under `auth` (`clone: true, cycle: 263`); `clone_version` bumped `0.0.0-cycle-259 → 0.0.0-cycle-263` |
| `docs/cycles/cycle-263-scope-hint/deliverable.md` | added | this file |

**Net:** +1 new file (`scope_hint_test.go`), 4 modified files
(`http_error.go`, `api_error.go`, `api_error_test.go`, `tui_run.go`),
+1 entry in `feature_diff_table.json`. No third-party dependencies.
No edits to `internal/convstore/sqlite.go` (hard constraint).

### 1. `internal/backend/http_error.go` (the predicate)

The new code sits at the bottom of the file, after
`IsQuotaReachedErr`. Three exported-within-package declarations:

- `OAuthScopeHintMessage` — the canonical constant. The verbatim
  string starts with `Run `/logout`` so a downstream formatter that
  must split the hint from the upstream body has a stable leading
  token to slice on.
- `scopeKeywords` — a 4-element slice of lower-cased substrings.
  Case-insensitive scan over `bytes.ToLower(bytes.TrimSpace(body))`
  plus a fallback scan over `strings.ToLower(e.Message())` for the
  edge case where the JSON parser rejected the body but the
  extracted message still carries the keyword.
- `IsScopeMismatch()` — the precision predicate. The two guards
  (status == 403 AND keyword in body-or-message) keep the rule
  exact: a plain "403 forbidden" does NOT get the hint, an
  IP-allowlist denial does NOT get the hint, a 401 with a scope
  keyword in its body does NOT get the hint (it gets the
  AuthExpiredMessage from cycle 100 instead).

The asymmetry with `IsQuotaReached` is intentional: quota has a
canonical error message ("Individual quota reached") that 429 alone
signals, so the predicate can rely on status + substring. A 403 is
ambiguous by itself — IP block, region block, model rate-limit, and
scope mismatch all return 403. The keyword scan is the only thing
that disambiguates them, so we make it mandatory.

### 2. `cmd/wagyu/api_error.go` (the print path)

Adds `Remediation string `json:"remediation,omitempty"`` to
`apiError`. `classifyAPIError` reads `he.IsScopeMismatch()` once
and, when true:

- sets `out.Remediation = OAuthScopeHintMessage`,
- appends `" <hint>"` to the user-facing `out.Message` (which
  becomes the `error: <msg>` line),
- rebuilds `out.ShortError` as `"<status> (code <code>): <msg> <hint>"`.

The `Remediation` field is omitted from the JSON when empty, so
every other failure mode (quota, expired auth, generic 5xx,
non-API errors) renders the canonical AGY_ERROR envelope shape
exactly as before — verified by `TestScopeHint_NonScopeErrorHasNoRemediation`.

### 3. `cmd/wagyu/tui_run.go` (the TUI path)

The previous TUI handler returned `nil` when `chunk.Error != nil`
and silently swallowed the upstream error. The fix forwards the
error text into the bubble-tea program via `prog.Send(backendChunkMsg(...))`,
which the existing `backendChunkMsg` handler renders into
`m.response`. The hint is appended in the same conditional shape
as the print path — only when `errors.As(chunk.Error, &he) && he.IsScopeMismatch()`.

The handler still returns `nil` (errors are surfaced through the
chunk stream, not propagated), so the `agent.Loop.Send` return value
remains the typed error for the print path's `classifyAPIError`.
The TUI surface and the print path share the same predicate and
the same canonical hint constant.

## What the AGY_ERROR JSON envelope looks like now

Before (cycle 100 + 112 shape):

```
error: insufficient_scope
AGY_ERROR: {"short_error":"PERMISSION_DENIED (code 403): insufficient_scope","status":"PERMISSION_DENIED","error_code":403,"code_kind":"http","retryable":false,"error_id":"...-1"}
```

After (cycle 263):

```
error: insufficient_scope Run `/logout` then `/login` to refresh your OAuth grant with the required scopes.
AGY_ERROR: {"short_error":"PERMISSION_DENIED (code 403): insufficient_scope Run `/logout` then `/login` to refresh your OAuth grant with the required scopes.","status":"PERMISSION_DENIED","error_code":403,"code_kind":"http","retryable":false,"error_id":"...-1","remediation":"Run `/logout` then `/login` to refresh your OAuth grant with the required scopes."}
```

For every other failure mode (quota, auth-expired, generic) the
`remediation` field is absent and the envelope is byte-identical to
what cycle 100 + 112 produced. `TestClassifyAPIError_QuotaCapture` and
the other pre-cycle 263 envelope tests continue to pass without
changes.

## Tests

**`internal/backend/scope_hint_test.go` (new, 6 tests):**

- `TestScopeHint_403InsufficientScope`: `403 insufficient_scope: missing models.read` → hint available, body preserved.
- `TestScopeHint_403MissingScopeKeyword`: `missing scope models.read` → hint available.
- `TestScopeHint_403Generic` (5 sub-cases): plain forbidden, IP allowlist, region block, rate-limit-shaped 403, empty body → hint NOT available.
- `TestScopeHint_401NotEnough` (4 sub-cases): plain 401, 401 with scope mention, JSON 401 with scope keyword, empty 401 → hint NOT available (the status guard pins the rule).
- `TestScopeHint_CaseInsensitive` (4 sub-cases): `INSUFFICIENT_SCOPE`, `Missing Scope`, mixed casing, Message()-only path → hint available in all of them.
- `TestScopeHint_PreservesOriginalError`: the upstream body stays verbatim; `errors.As` walks chains; plain non-HTTPError errors stay negative.

**`cmd/wagyu/api_error_test.go` (additions, 2 tests):**

- `TestScopeHint_PrintModeRemediation`: 403 with insufficient_scope body → AGY_ERROR JSON contains the remediation field and the user-facing Message has the hint appended; Status/PermissionDenied, CodeKind/http, Retryable=false are pinned.
- `TestScopeHint_NonScopeErrorHasNoRemediation` (4 sub-cases): plain 403 forbidden, 403 with quota message, 503 retryable 5xx, 401 auth-expired → Remediation stays empty and the `remediation` field is absent from the envelope.

Total cycle-263 test count: 7 (`6 + 2` header tests with 4 + 5 + 4 + 4 = 17 sub-test cases). The 2 pre-existing envelope-shape tests (`TestClassifyAPIError_QuotaCapture`, `TestAPIErrorJSONShape`) continue to pass without changes — the `remediation` field is `omitempty` so it does not perturb the canonical shape.

### Build & sweep

```
go build -o /tmp/wagyu-cycle263 ./cmd/wagyu   # exit 0, 86 MB binary
go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...
  # 58 packages: all ok, 0 FAIL
```

The `TestCycle225QuotaCountdown` flake (a `time.Now()`-sensitive
boundary test for `quotaCountdown`) was observed once during this
cycle's full sweep. Confirmed pre-existing — `-count=10` reproduces
it on `main` without my changes, and a fresh `go test -count=1`
on my branch passes. Documented here for traceability; out of scope
for cycle 263 (will be picked up under a separate test-stability
cycle).

## 硬约束 / Limitations

1. **Detection precision is body-keyword-dependent.** We match four
   substrings (`insufficient_scope`, `missing scope`, `requires
   scope`, `scope required`). A future OAuth implementation that
   emits a different sentence (e.g. "you need scope X" without
   one of the exact keywords) would not trip the predicate. The
   fallback scan over `Message()` lowers the false-negative rate
   significantly, but any upstream that wraps the error in a
   localized string the body-parser strips will reach the user
   without the hint. The list is recorded in
   `scopeKeywords` so a future cycle can extend it without
   hunting through bodies.
2. **The TUI mirror is text-only.** The TUI handler appends the
   error text to `m.response` via `backendChunkMsg`. There is
   no special "error banner" styling yet — the text shows up in
   the chat reply with the same glamour styling as a model
   response. Cycle 263 closes the user-facing message gap; an
   "error banner" overlay is a separate cycle per AGENTS.md
   "A future cycle will route chunk.Error / ErrAutoContMaxDepth
   through a turnErrorMsg".
3. **No retry policy change.** The hint is informational. A scope
   mismatch is still non-retryable (429 / 5xx retry set stays
   unchanged). The user is expected to act on the hint, not the
   auto-continuer.
4. **`apiError.Remediation` is unconditionally tied to the
   IsScopeMismatch predicate.** A future HTTPError failure mode
   that needs a different remediation (e.g. "rotate your API key"
   for an expired API-key 401) would need a new method + a new
   field, not a generic extension. The current shape is purpose-
   built for the OAuth scope case and does not pretend to be a
   general recovery-recommendation channel.
5. **No edits to `internal/convstore/sqlite.go`.** The hard
   constraint holds — the sqlite reader is unchanged.
6. **Pre-existing `TestCycle225QuotaCountdown` flake.** Documented
   in the Build & sweep section. Not a regression of this cycle;
   reproduced on `main` with `-count=10`.
