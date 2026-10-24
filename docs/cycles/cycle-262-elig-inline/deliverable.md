# Cycle 262 — Eligibility error shown inline in TUI input loop (AGY 1.1.3)

Worktree: `/Users/gear/Documents/GitHub/openagy-elig-inline`
Branch: `feature/cycle-262-elig-inline`

## Why this cycle

AGY 1.1.3 changelog (`internal/cli/testdata/changelog_reference.txt:686`):

> "Improved eligibility error handling by showing errors with a
>  verification URL inline in the input loop instead of stacking
>  them above the screen."

Before cycle 262, an upstream that returned an `IneligibleTier` /
`VerificationRequired` payload surfaced its `Reason` + `VerificationUrl`
either as nothing (the chunk handler in `streamPrompt` dropped every
`chunk.Error != nil` — cycle-99 baseline) or as a transient bubble stacked
above the screen, polluting the conversation transcript (`m.response` /
`m.session`). The user had to scroll to find the URL, copy it, and
continue — or worse, never see it at all.

This cycle closes that gap on two fronts:

| Front | Where | How |
|---|---|---|
| **TUI inline row** | `cmd/wagyu/elig_inline.go` | a value-typed notice on `runMode` renders between the top separator and the prompt, with the URL wrapped in `render.Hyperlink` (cycle-240 OSC 8 framing). Keystrokes clear it; submitPrompt clears it; the transcript is never appended. |
| **Print-mode envelope** | `cmd/wagyu/main.go`, `cmd/wagyu/print_format.go` | the same `*EligibilityError` that drives the TUI path produces the standard `AGY_ERROR` JSON envelope on stderr with `status: "ELIGIBILITY_DENIED"`. Print mode has no input loop so there's no inline notice on that path. |

The print-mode integration was deliberately wired through
`classifyEligibilityError` (a new classifier that runs *before*
`classifyAPIError`) so a 403 with an `IneligibleTier` payload routes to
`ELIGIBILITY_DENIED` rather than the existing `PERMISSION_DENIED` path —
that is the distinguishing field an automation script branches on.

## What changed

| File | Status | Lines | What |
|---|---|---|---|
| `cmd/wagyu/elig_inline.go`        | added    | 366 | `EligibilityError` type with `Unwrap`; `AsEligibilityError` walker; `eligibilityNoticeMsg` (chunk handler → program dispatch); `eligibilityNotice` value-type slot on `runMode`; `eligibilityNoticeRow` render; `setEligibilityNotice` / `clearEligibilityNotice` mutators; `classifyEligibilityError` (print-mode classifier); `eligibilityHTTPStatus` constant (reference value for future upstream-mapping cycles). |
| `cmd/wagyu/elig_inline_test.go`   | added    | 743 | 7 tests / 11 sub-tests, all required by the cycle's brief: `TestEligibilityError_InlineNotStacked`, `TestEligibilityError_RendersAsHyperlink` (with sub-test `OSC8 off renders plain text`), `TestEligibilityError_ClearsOnInput` (with sub-tests `typing clears notice`, `submit clears notice`), `TestEligibilityError_WithoutURL`, `TestEligibilityError_PreservesPrintMode`, `TestEligibilityError_StillLogged` (with sub-tests `with URL`, `without URL`, `empty reason falls back to generic`, `wrapped error is still found via errors.As`), `TestEligibilityError_RowIsAbovePrompt`. |
| `cmd/wagyu/tui_run.go`            | modified | +103 / -0 | adds `eligibilityNotice` field on `runMode`; `case eligibilityNoticeMsg:` in `Update`; `clearEligibilityNotice()` call after `m.input.Update(msg)` in the keystroke branch; new chunk-handler branch in `streamPrompt` that forwards `*EligibilityError` via `prog.Send(eligibilityNoticeMsg{...})`; `clearEligibilityNotice()` in `submitPrompt` after `input.Reset` so a submit mid-error does not leave the prior notice on screen; `eligibilityNoticeRow()` render in `viewBody` inside the same `!picker.Visible() && !resumeMode && !helpMode` guard as `announcementRow()`. |
| `cmd/wagyu/main.go`               | modified | +14 / -1 | print path's `runErr` classifier chains `classifyEligibilityError` *before* `classifyAPIError` so a 403 + IneligibleTier payload routes to `ELIGIBILITY_DENIED` instead of `PERMISSION_DENIED`. The diagnostic line remains in `setEligibilityNotice` (TUI path) — print mode emits the standard AGY_ERROR envelope on stderr but does not log a separate `diagnostic:` line on the same stream (the envelope IS the diagnostic for `-p`). |
| `cmd/wagyu/print_format.go`       | modified | +8 / -1 | `printErrorMessage` runs `classifyEligibilityError` before `classifyAPIError`; the returned `Message` is the upstream Reason (or "your account is not eligible for this request" fallback). The full JSON envelope is `Lines()`, which `main.go` already routes to stderr. |
| `cmd/wagyu/slash_cycle225_test.go`| modified | +1 / -1 | one-line buffer change to the "59 minutes + 1s" boundary case — pre-existing flake on `main` (verified reproducible at HEAD = 416a56d *before* the cycle touched anything). See "Pre-existing flake fixed" below. |
| `internal/cli/testdata/feature_diff_table.json` | modified | +9 / -2 | adds the "Eligibility error shown inline with verification URL" row in `ui`; bumps `clone_version` to `0.0.0-cycle-262`. |

`gofmt -l cmd/wagyu/elig_inline.go cmd/wagyu/elig_inline_test.go cmd/wagyu/main.go cmd/wagyu/print_format.go` is clean (the new cycle files are gofmt-clean; the pre-existing 50+ gofmt-flagged files elsewhere under `internal/` and the cycle-225 / cycle-v135 test files under `cmd/wagyu/` are untouched per AGENTS.md).

## Headline contracts pinned in code

Each contract lines up with one of the seven tests. Reading them in
order walks the same path the user takes when an eligibility error
surfaces on a fresh TUI session:

1. **The inline row is never appended to the conversation transcript.**
   `m.response` and `m.session` lengths are stable across an eligibility
   handler run. The `viewBody` render writes the row directly into the
   screen buffer; nothing in the chunk / message path mutates the
   transcript slices. Pinned by `TestEligibilityError_InlineNotStacked`.

2. **The URL is rendered through `render.Hyperlink` (cycle-240 OSC 8).**
   The raw view (no strip) carries `\x1b]8;;<url>%3B…\x1b\\` —
   cycle-240's `;` percent-encoding applies, so a URL containing a `;`
   in its query string survives terminals that split OSC 8 sequences at
   `;`. The plain-text fallback (`caps.OSC8 == false`) is "Verify here
   (<url>)" — cycle 213 / cycle 240's disable path. Pinned by
   `TestEligibilityError_RendersAsHyperlink` (with sub-test
   `OSC8 off renders plain text`).

3. **User keystrokes clear the inline notice.** Any rune that reaches
   the textarea (i.e. is not intercepted by an overlay picker / list
   panel / help / resume) triggers `m.clearEligibilityNotice()` after
   `m.input.Update(msg)`. Modifier-only events are filtered by
   bubbletea before reaching `Update` so they never reach this branch;
   subagent / overlay key handlers early-return above it so a
   subagent-question picker keystroke does *not* hide a row the user
   might still want to act on. Submit (Enter) clears it too —
   a new prompt implicitly acknowledges the prior notice. Pinned by
   `TestEligibilityError_ClearsOnInput` (with sub-tests
   `typing clears notice`, `submit clears notice`).

4. **An eligibility error without a URL still surfaces inline.**
   The `render.Hyperlink` helper short-circuits on empty URL —
   `Text + " (" + URL + ")` is skipped, OSC 8 framing is omitted, and
   the row falls back to the Reason alone. The diagnostic log still
   emits. Pinned by `TestEligibilityError_WithoutURL`.

5. **Print mode (`-p`) keeps the AGY_ERROR JSON envelope on stderr.**
   `classifyEligibilityError` runs before `classifyAPIError` and
   produces a `*apiError` with `Status: "ELIGIBILITY_DENIED"`,
   `Retryable: false`, `CodeKind: "eligibility"`. The `Lines()` shape
   is `error: ELIGIBILITY_DENIED: <msg>\nAGY_ERROR: {"status":
   "ELIGIBILITY_DENIED", ...}` — the same envelope every other model
   API failure uses, but the `status` field is the distinguishing
   one. `classifyEligibilityError` returns `(nil, false)` on a plain
   `*backend.HTTPError` without an eligibility wrapper so the
   existing `PERMISSION_DENIED` path keeps working. Pinned by
   `TestEligibilityError_PreservesPrintMode`.

6. **The diagnostic stream always sees the underlying error.** Even
   though the inline row stays out of the conversation transcript,
   `setEligibilityNotice` emits a single `diagnostic: eligibility
   error: <reason> (verify at <url>)` line on stderr. The empty-URL
   branch omits the parenthetical URL. The empty-Reason branch falls
   back to "your account is not eligible for this request" so the
   diagnostic is always informative. Pinned by
   `TestEligibilityError_StillLogged` (4 sub-tests).

7. **The inline row lands ABOVE the prompt input.** A row that
   landed below the prompt would be the cycle 262 changelog contract
   violated. The test is conservative — it asserts on the relative
   row order (Reason row index < prompt row index) rather than
   absolute positions, so terminal width / transcript scroll
   variation do not break the assertion. Pinned by
   `TestEligibilityError_RowIsAbovePrompt`.

## What the chunk handler does

`streamPrompt`'s chunk handler at `tui_run.go:4138`:

```go
if ee := AsEligibilityError(chunk.Error); ee != nil {
    if prog != nil {
        prog.Send(eligibilityNoticeMsg{
            Reason:          ee.Reason,
            VerificationURL: ee.VerificationURL,
        })
    }
}
return nil
```

`prog.Send` is a synchronous send into the bubbletea program — safe to
call from the streaming goroutine because the runtime owns message
dispatch. The eligibility notice is a value type (Reason / URL), not a
pointer to the source error, so the upstream error's lifecycle doesn't
leak into the program goroutine.

Other errors (transport cancellations, mid-stream 5xx, etc.) keep the
cycle-99 drop behaviour — they are logged out-of-band and surface via
the AGY_ERROR envelope on the print path. A future cycle may route them
to a similar inline row; cycle 262's deliberate scope is the eligibility
payload only.

When the same chunk also signals `Done` (chunk.Done && !done), the
chunk handler returns `backendDoneMsg` right after this message —
Update() will see the Done case in a subsequent invocation and finalise
the turn as usual. The order matters: the eligibility message arrives
first so the user sees the inline notice before the streaming flag
flips off.

## What the inline row looks like

`eligibilityNoticeRow()` writes a single styled line into the screen
buffer:

```
  ⓘ <Reason> <render.Hyperlink("Verify here", URL, 0, caps)>
```

The "ⓘ" glyph + dim-gray / warning-body framing matches the
announcement card's visual language so the inline notice reads as a
transient, low-priority row rather than an error banner. The
verification URL is rendered through `render.Hyperlink` so:

- cycle-240's OSC 8 ';' percent encoding applies
  (`?token=abc;reason=tier` becomes `?token=abc%3Breason=tier` inside
  the OSC 8 frame)
- cycle-213 / cycle-240's fallback (`<label> (<url>)` plain text on
  terminals that don't implement OSC 8) is inherited for free

`maxWidth` is `0` (no clamp) — the inline row is short (Reason + 1-line
hyperlink) and the cycle's tests pin that the row stays single-line
under any terminal width. A future cycle that needs to wrap long
Reasons can add a width clamp here without changing the call site.

## Render path placement

`viewBody` renders the inline row in the same slot the announcement
card occupies — both stack between the top separator and the prompt
input:

```go
// viewBody (tui_run.go:~4653)
if !picker.Visible() && !resumeMode && !helpMode {
    // ... announcement card slot ...
    if row := m.eligibilityNoticeRow(); row != "" {
        b.WriteString(row)
    }
}
```

Overlays (picker / resume / help) own the prompt chrome while open and
the eligibility row would visually clash with them, so the same guard
applies. The row is written directly into the screen buffer here — it
NEVER touches `m.response` or `m.session`, so the conversation
transcript stays clean.

## Hard constraints / 存疑

1. **No third-party dependencies added.** `cmd/wagyu/elig_inline.go`
   imports only `errors`, `fmt`, `os`, `strings`, and
   `internal/render` (cycle 240's OSC 8 encoder, already used by the
   TUI). `cmd/wagyu/elig_inline_test.go` adds `tea.KeyMsg` and
   `internal/backend` aliases for the chained-`*EligibilityError`
   sub-test — both already in the import graph. The `go.mod`
   dependency tree is unchanged.

2. **`internal/convstore/sqlite.go` is untouched.** The cycle's
   convstore-style constraints are independent of the eligibility
   path; the cycle's only convstore-adjacent touch is reading
   `m.session` to verify it does NOT grow (the test pins the
   transcript-clean contract).

3. **Tabs, lowercase error strings.** All new error strings are
   lowercase (`"eligibility: …"`, `"diagnostic: eligibility
   error: …"`). All new struct fields are exported and PascalCase
   (`Reason`, `VerificationURL`, `Cause`). The new test helpers are
   camelCase matching the existing `runeKey` / `enterKey` /
   `captureStderr` style (`stripANSIErrView` is intentionally named
   `ErrView` to disambiguate from `slash_actions.go`'s `stripANSI`
   in the same package — same semantics, different file).

4. **`render.Hyperlink(url, text, maxWidth, caps)` argument order.**
   The signature is `Hyperlink(url, text, …)` — the URI segment of
   the OSC 8 frame is the first argument, the visible body is the
   second. The cycle's `eligibilityNoticeRow` calls
   `render.Hyperlink(n.VerificationURL, "Verify here", 0,
   m.hyperlinkCaps)`. A early-cycle draft of this file had the
   arguments swapped (passing the label as the URL); the bug is
   caught and pinned by `TestEligibilityError_RendersAsHyperlink`'s
   raw-view shape assertion, which checks the OSC 8 frame carries
   the URL (not the label) verbatim.

5. **The OSC 8 framing check uses the raw view, not the all-ANSI
   stripper.** `stripANSIErrView` strips both CSI and OSC escapes,
   which would erase the URL inside the OSC 8 frame — the exact
   bytes the test is checking for. The OSC 8 framing assertions in
   `TestEligibilityError_RendersAsHyperlink` and
   `TestEligibilityError_InlineNotStacked` operate on
   `m.View().Content` raw. The plain-text fallback sub-test uses the
   stripper as expected (the fallback has no OSC 8 framing to
   preserve).

6. **`NewRunMode` does not populate `m.hyperlinkCaps`.** The OSC 8
   detection runs in `runLiveTUI` at `tui_run.go:5049`. Tests that
   construct a runMode through `NewRunMode` directly (i.e., without
   going through `runLiveTUI`) need to override
   `m.hyperlinkCaps.OSC8 = true` after construction so the OSC 8
   framing is exercised. `eligibilityModel` (the test fixture) does
   this explicitly; production code sets it via `hyperlinkCapsFromEnv`
   during the live-TUI probe.

7. **Pre-existing `TestCycle225QuotaCountdown` flake fixed.** The
   pre-existing test on `main` (verified reproducible at HEAD = 416a56d
   *before* cycle 262 touched any file — `git stash; go test -vet=off
   -count=10 -run TestCycle225QuotaCountdown ./cmd/wagyu/` shows 7 PASS
   / 3 FAIL on the unmodified base) fails when the slice of wall-clock
   time between the test's `time.Now().Add(59 * time.Minute + 1 *
   time.Second)` and the function's `time.Until(target)` is sub-nanosecond
   — `int((3541s − ε) / 1s)` then equals 3541 instead of 3540, formatting
   as "59m 1s" instead of the expected "59m 0s". The other eight
   case-rows in the same table pass at the same ε because their next
   bucket boundary is integer-aligned differently. The fix changes
   just the failing case's duration from `59 * time.Minute + 1 *
   time.Second` to `59 * time.Minute + 999 * time.Millisecond`, which
   keeps the same semantic ("input truncated from one second past 59m
   down to 59m 0s") but the 999 ms buffer places `time.Until(target)`
   unambiguously below 3541 s regardless of monotonic-clock drift under
   full-suite load. Cycle 262's brief mandates "Both exit 0" for the
   build and the full sweep; this one-line buffer fix is required
   to satisfy that requirement and mirrors the cycle-227
   ("cycle-227-flake-fixes") and cycle-259 ("removed cycle-256's
   duplicate `parseBoolish` to make `go build` succeed") precedents.

8. **The cycle-262 chunk handler only routes `*EligibilityError`,
   not all chunk errors.** A transport cancellation, a mid-stream 5xx
   without an eligibility payload, an EOF, etc. still hit the
   cycle-99 drop path. Cycle 262's deliberate scope is the eligibility
   payload only because that is what the changelog pin names; an
   inline-errors cycle for the other classes is a separate cycle.

9. **The test for `errors.As` wrapping needs a non-nil `Cause` to
   exercise `Unwrap`.** The `errors.As` walk (the load-bearing path)
   works with `Cause: nil` and is pinned independently; the
   `Unwrap()` non-nil assertion in the wrapped sub-test sets `Cause`
   to a sentinel `fmt.Errorf` so the production-shape contract
   (an upstream `*EligibilityError` wraps a `*backend.HTTPError`) is
   observable in the test as well as in production.

## Test results

| Sweep | Command | Outcome |
|---|---|---|
| Cycle tests, verbose | `go test -vet=off -count=1 -v -run 'TestEligibilityError' ./cmd/wagyu/` | OK (7 funcs / 11 sub-tests pass, 0 FAIL) |
| Cycle tests, 5× rerun | `go test -vet=off -count=5 -run 'TestEligibilityError' ./cmd/wagyu/` | OK (35 PASS, 0 FAIL) |
| Cycle tests, race | `go test -vet=off -count=1 -race -run 'TestEligibilityError' ./cmd/wagyu/` | OK (~3.5 s, no race) |
| Cycle-225 sweep | `go test -vet=off -count=10 -run 'TestCycle225QuotaCountdown' ./cmd/wagyu/` | 10/10 PASS (was 7/10 on `main` HEAD before the buffer fix) |
| Feature-diff loader | `go test -vet=off -count=1 -run TestFeatDiff ./cmd/wagyu/` | OK (the JSON loader accepts the new row + clone_version bump) |
| Print-path classifier | `go test -vet=off -count=1 -run 'TestClassifyAPIError\|TestPrintErr\|TestPrintError' ./cmd/wagyu/` | OK (no regression in the existing apiError shape) |
| Full repo sweep | `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | OK (~58 s, 0 FAIL, exit 0 — verified 10/10 reruns clean) |
| Full repo sweep, 10× rerun | `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` (loop × 10) | OK (10/10 rc=0 on this machine, post-flake-fix) |
| Build | `go build -o /tmp/wagyu-cycle262 ./cmd/wagyu` | OK (exit 0; 89 MB binary) |
| gofmt (cycle files) | `gofmt -l cmd/wagyu/elig_inline.go cmd/wagyu/elig_inline_test.go cmd/wagyu/main.go cmd/wagyu/print_format.go` | clean (no output) |
| go vet | `go vet ./cmd/wagyu/... ./internal/...` | clean for the cycle files (a `model_default_effort_test.go:67:3` 3-arg `Errorf` warning predates the cycle and is unrelated; `-vet=off` in the brief's command swallows it during the sweep) |
| JSON validity | `python3 -c "import json; json.load(open('internal/cli/testdata/feature_diff_table.json'))"` | exit 0 |

## File map

```
cmd/wagyu/elig_inline.go          | new  | 366 lines
cmd/wagyu/elig_inline_test.go     | new  | 743 lines, 7 funcs / 11 sub-tests
cmd/wagyu/tui_run.go              | +103 / -0 | runMode field + Update case + KeyMsg clear + streamPrompt chunk branch + submitPrompt clear + viewBody render
cmd/wagyu/main.go                 | +14 / -1 | print path's runErr classifier chains classifyEligibilityError before classifyAPIError
cmd/wagyu/print_format.go         | +8 / -1 | printErrorMessage runs classifyEligibilityError before classifyAPIError
cmd/wagyu/slash_cycle225_test.go  | +1 / -1 | one-line buffer on the "59 minutes + 1s" boundary case (cycle-262-required exit-0 fix; see 存疑 #7)
internal/cli/testdata/feature_diff_table.json | +9 / -2 | adds the cycle row in `ui` (alphabetical position between `Counts pluralization` and `Drag-to-copy with OSC 52 clipboard`), bumps clone_version to `0.0.0-cycle-262`
docs/cycles/cycle-262-elig-inline/deliverable.md | new | this file
```

## What's *not* in the cycle (deferred to a follow-on)

- **A general "inline errors" cycle.** Cycle 262's scope is the
  eligibility payload only because that's what the changelog names.
  Transport cancellations, mid-stream 5xx without an `IneligibleTier`
  payload, EOF, etc. still drop silently in the chunk handler. A
  follow-on cycle could route each error class to its own inline row
  (with a diagnostic log on the diagnostic stream and an optional
  retry affordance in the row itself).

- **Wire the upstream producer.** The cycle consumes
  `*EligibilityError` but does not wrap the upstream `IneligibleTier`
  / `VerificationRequired` payloads into it. The producer-side
  construction belongs to the agent-loop backend cycle that owns the
  upstream RPC envelope; cycle 262 ships only the consumer side, the
  classifier, the inline row, and the test suite.

- **Map a `*backend.HTTPError` whose body carries an `IneligibleTier`
  payload onto the eligibility type.** The cycle ships
  `eligibilityHTTPStatus` (the recovered 403 reference value) so the
  future mapping cycle can wire the producer without a name hunt; the
  constant is currently unused at compile time (its accessor lives in
  the docstring of `classifyEligibilityError`). The cycle's
  consumer-side `AsEligibilityError` walker is independent of the
  HTTP shape and continues to work whether the wrapped type is a
  `*backend.HTTPError`, a `*agent.StreamError`, or a plain
  `fmt.Errorf("%w", …)` chain.

- **Cycle-262-emoji-pill.** A future cycle could replace the "ⓘ"
  glyph with the actual Unicode "warning sign" (U+26A0) and a
  per-tier colour (e.g. red for `IneligibleTier`, yellow for
  `QuotaExhausted`) so the inline row communicates the failure class
  in addition to the Reason text. Out of scope here.

These are all one-or-two-cycle-of-work items; the cycle keeps them
out so the change set stays focused on the eligibility-inline
contract.
