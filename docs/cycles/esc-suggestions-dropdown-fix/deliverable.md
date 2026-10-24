# Cycle 235 — Esc on suggestions dropdown closes it; second Esc interrupts the turn

## Goal

AGY 1.2.10 closes a UX trap on the slash-command suggestions dropdown:

> "Fixed pressing `Esc` while the suggestions dropdown is open
> interrupting the agent's turn; it now closes the dropdown,
> and a second `Esc` interrupts."

Before the fix, a single `Esc` press while the picker was visible
would dispatch to whatever cancel / interrupt path the runMode owned,
so the user lost their in-flight turn to a keystroke they meant as
"close the dropdown". After the fix, the picker takes priority on the
first `Esc`, and only the second `Esc` (with the picker already gone
but the turn still streaming) cancels the model's stream.

This is a focused, low-risk A-class change: one new field on
`runMode`, one new branch in the existing Esc handler, one new defer
in `streamPrompt`. No third-party deps touched, no PiG plumbing
changes, no `internal/` churn.

## Scope (what cycle 235 changes)

1. **New field `turnCancel context.CancelFunc` on `runMode`** —
   populated by `streamPrompt` so the Esc handler can cancel the
   in-flight stream out of band, and cleared by the turn-end handlers
   (`turnDoneMsg`, `backendDoneMsg`, `backendChunkMsg`) so a stale
   `streaming == true` left behind by an early-return backend (the
   test `fakeClient`, a backend error, a network EOF) cannot trigger
   a phantom cancel.
2. **New branch in the Esc handler** — sits *after* the picker-close
   branch and *before* the input-clear branch. The picker-close keeps
   priority (first Esc is always "close the dropdown"), the new
   branch fires only when the picker is already closed but a turn is
   still streaming. Priority order becomes:
   1. `helpMode` dismiss
   2. `picker.Visible()` → close dropdown
   3. **NEW** `streaming && turnCancel != nil` → cancel stream
   4. `input.Value() != ""` → clear input
   5. announcement-card dismiss
   6. silent absorb
3. **`streamPrompt` installs and clears `turnCancel`** — uses the
   `context.WithTimeout` cancel function as the cancel handle. A
   second `defer func() { m.turnCancel = nil }()` runs in the
   deferred order after `defer cancel()`, so by the time the turn
   ends naturally `turnCancel` is already `nil`. Calling `cancel()`
   from Esc is idempotent with the deferred `cancel()`, so the
   timeout still owns the deadline; the Esc cancel just short-circuits
   a slow stream.

The fix is purely additive: the picker-close path was already in
place (cycle 150) and is unchanged. The Esc-while-streaming cancel
behavior was missing before this cycle.

## Files changed

| File | +/− | Why |
|---|---|---|
| `cmd/wagyu/tui_run.go` | **+49 / −1** | `turnCancel` field on `runMode`; new Esc branch; `streamPrompt` install/clear of `turnCancel`. |
| `cmd/wagyu/tui_cycle235_test.go` | **new, 217** | 6 tests pinning every branch of the new Esc handler. |

No `internal/` files touched. No new third-party deps. The clone's
go binary size delta is negligible (one `context.CancelFunc` field
plus a few branches — the linker folds them into the existing
`internal/agent`/`backend`/`pigrt` plumbing unchanged).

## New tests

All six live in `cmd/wagyu/tui_cycle235_test.go`:

| Test | Pins |
|---|---|
| `TestEscPickerOpenClosesDropdownOnly` | First Esc with picker visible + streaming turn closes picker; turnCancel is **NOT** invoked. |
| `TestEscNoPickerInterruptsStreamingTurn` | Esc with picker closed + streaming turn calls turnCancel exactly once; turnCancel cleared afterwards. |
| `TestEscTwoPressesCloseThenInterrupt` | Full user flow: `/` opens picker → first Esc closes picker (turn untouched) → second Esc cancels the turn. |
| `TestEscNoDropdownNoStreamAbsorbsSilently` | Esc with picker closed + no streaming + empty input does nothing. |
| `TestEscStreamingWithoutCancelDoesNotCrash` | Esc with `streaming == true` but stale `turnCancel == nil` does not panic and does not block. |
| `TestEscInputClearOrInterruptPriority` | Esc with input text + streaming + no picker **interrupts the turn** (does not clear input); the input stays intact, the cancel fires. |

Run results on this branch (`go test -vet=off -count=1 -run
'TestEsc' -v ./cmd/wagyu/`):

```
=== RUN   TestEscPickerOpenClosesDropdownOnly
--- PASS: TestEscPickerOpenClosesDropdownOnly (0.00s)
=== RUN   TestEscNoPickerInterruptsStreamingTurn
--- PASS: TestEscNoPickerInterruptsStreamingTurn (0.00s)
=== RUN   TestEscTwoPressesCloseThenInterrupt
--- PASS: TestEscTwoPressesCloseThenInterrupt (0.00s)
=== RUN   TestEscNoDropdownNoStreamAbsorbsSilently
--- PASS: TestEscNoDropdownNoStreamAbsorbsSilently (0.00s)
=== RUN   TestEscStreamingWithoutCancelDoesNotCrash
--- PASS: TestEscStreamingWithoutCancelDoesNotCrash (0.00s)
=== RUN   TestEscInputClearOrInterruptPriority
--- PASS: TestEscInputClearOrInterruptPriority (0.00s)
PASS
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.060s
```

`go test -vet=off -count=1 ./cmd/wagyu/` shows a single pre-existing
failure unrelated to this cycle
(`TestDispatchPrintSlash_LocalCommands` expects the model
registry to default to `minimax-m3`, but on this machine the test's
environment points the registry at `gemini-3.8-flash-medium` —
verified by running the same test on `main` before any change).

## Priority order rationale

Cycle 235 chose the priority order `picker-close > streaming-interrupt
> input-clear` for one reason: the changelog spec says "**a second**
Esc interrupts". The natural reading is:

- First Esc with the dropdown open → close the dropdown (always).
- Second Esc, dropdown closed, turn still streaming → interrupt.

If we put streaming-interrupt *after* input-clear, a user who typed
text into the picker (e.g. `/a`), pressed Esc to close the picker,
and then pressed Esc again would have their input cleared instead of
their turn interrupted — the opposite of what the spec asks for. So
streaming-interrupt sits before input-clear. The picker-close sits
first because closing the dropdown is the unambiguous "first Esc"
intent the changelog calls out.

The silent-absorb branch at the bottom of the Esc handler is
unchanged; the picker-close branch's comment was extended with a
cycle-235 note explaining the priority, and the new branch carries
the same note.

## Lifecycle of `turnCancel`

```
submitPrompt (m.streaming = true)
   │
   ▼
tea.Batch(streamPrompt(prompt), tickCmd)
                          │
                          ▼ (asynchronous goroutine)
                  ctx, cancel := context.WithTimeout(...)
                  m.turnCancel = cancel
                  defer cancel()              // owns the 60 s deadline
                  defer func() { m.turnCancel = nil }()   // clears handle
                          │
                          ▼
                  loop.Send(ctx, ...)         // honours ctx.Done
                          │
                ┌─────────┴─────────┐
                │                   │
   user presses Esc       stream completes naturally
                │                   │
                ▼                   ▼
   m.streaming && m.turnCancel != nil
                │                   turnDoneMsg / backendDoneMsg /
                ▼                   backendChunkMsg → m.streaming = false
   m.turnCancel()                    (m.turnCancel already nil via the
   m.turnCancel = nil                 inner defer)
```

The cancel is idempotent: both the Esc-driven call and the deferred
`cancel()` reach the same `context.CancelFunc`, so calling it twice
is a no-op. When the turn ends naturally, the inner defer clears
`turnCancel` first, then the outer `defer cancel()` runs — but
`cancel()` is still safe to call twice, so the deferred cleanup
finishes cleanly.

## Verification

```
$ go build -o /tmp/wagyu-esd ./cmd/wagyu
$ ls -la /tmp/wagyu-esd
-rwxr-xr-x  1 gear  staff  73306626  Oct 18 11:35  /tmp/wagyu-esd

$ gofmt -l cmd/wagyu/tui_cycle235_test.go cmd/wagyu/tui_run.go
(no output — both files clean)

$ go vet ./cmd/wagyu/...
(no output)

$ go test -vet=off -count=1 -run 'TestEsc|TestDropdown|TestSuggestion' \
    ./cmd/wagyu/ ./internal/.../
ok  	github.com/chadgpt/openagy/cmd/wagyu	1.025s
ok  	github.com/chadgpt/openagy/internal/agent	0.586s  [no tests to run]
... (33 internal packages, all ok or "no tests to run")
```

`gofmt -l cmd/wagyu/` reports the same 8 pre-existing dirty files
that `AGENTS.md` already calls out (none of them this cycle's
changes). The new file `tui_cycle235_test.go` is gofmt-clean.

## Hard constraints — checked

- `internal/convstore/sqlite.go` — **untouched**.
- Pre-existing not-clean files (`internal/...` + `picker_view.go`,
  `tui_v135_test.go`, etc.) — **not reformatted wholesale**.
- `go build -o /tmp/wagyu-esd ./cmd/wagyu` — **exit 0** (binary 73 MB).
- `go test -vet=off -count=1 -run 'TestEsc|TestDropdown|TestSuggestion' ./cmd/wagyu/ ./internal/.../`
  — **all ok**.
- `gofmt -l` on every NEW file — **empty**.
- agw/MiniMax-M3 model used (this session).
- Branch **not pushed**.
- Worktree at
  `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-esc-suggestions-dropdown-fix`.
- Single commit (this cycle is one commit, message TBD by the
  integrating session).
- New tests added: **6** (4 required minimum: dropped-down-on-Esc,
  no-dropdown-interrupts, two-Escs-flow, dropdown-state-tracked;
  plus 2 bonus tests for the streaming-without-cancel crash guard
  and the input-vs-interrupt priority).

## 存疑 / Limitations

1. **No end-to-end verification against the live AGY binary.**
   The clone's `streamPrompt` calls `loop.Send(ctx, ...)` and the
   `agent.Loop` honours `ctx.Done()`. The reference binary's exact
   behaviour on Esc-while-streaming (does it cancel the HTTP/2
   stream? close the SSE connection? send a graceful-shutdown
   packet?) was not byte-recovered in this cycle. The clone's
   behaviour matches the changelog text; the wire-level details
   might diverge and a future cycle can tighten them with a
   `/tmp/agycmp/tui/esc_interrupts.ref.raw.txt` capture.

2. **The cancel is silent.** No "Cancelled" feedback row is appended
   to `m.response` when Esc interrupts. The reference's visible
   behaviour on Esc-while-streaming was not captured, so the clone
   absorbs the cancel cleanly. A future cycle can add a
   `⎿  Cancelled` row if / when the ref capture proves it.

3. **Esc-after-picker-close + streaming is the only path tested.**
   The picker cannot be open while a turn is streaming in the current
   code (because `submitPrompt` returns nil when `m.streaming` is
   true), so the spec's "second Esc interrupts" naturally reduces
   to "Esc with picker closed + streaming → cancel". The picker +
   streaming overlap (which the spec also calls out) is therefore
   not reachable today; the priority order documents the precedence
   in case a future cycle opens the picker while streaming.

4. **Pre-existing `TestDispatchPrintSlash_LocalCommands` failure
   on this machine is unrelated to cycle 235** — verified by
   checking out `main` and running the same test (same failure).
   It is left alone per the "do not reformat / do not touch
   unrelated code" hard constraint.
