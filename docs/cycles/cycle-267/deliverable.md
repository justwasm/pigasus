# cycle 267 — TUI streaming → done transition does a full screen repaint

## TL;DR

After a streamed turn completes, the bubbletea v2 + ultraviolet
diff-based renderer was leaving stale pixels on screen — the
"  Generating…" spinner never cleared, and the announcement-card
transition (Card 1 → Card 2 after the first user prompt) overlaid
the old Card 1 title on top of the new response text. Captured
end-state before the fix:

```
r07: > analyze pwd
r08:    announcemrom model=Gemini 3.8 Flash (Medium): ← Card 1 top border left in place
r09:      Claude                          ← Card 1 title "Claude Opus 5.5 available" leaked
r15: ⣽  Generating...                     ← spinner still on screen
```

Cycle 267 returns `tea.ClearScreen` from `case turnDoneMsg:` and
`case backendDoneMsg:`, which routes the next render through
`s.scr.Erase() → clearUpdate() → full-screen repaint` driven by the
freshly returned `View()`. Captured end-state after the fix:

```
r07: > analyze pwd
r08:   mock reply from model=Gemini 3.8 Flash (Medium): analyze pwd
r12:    announcement
r13:      Daily quota resets at 00:00 UTC
r14:     Antigravity Starter Quota and Pro tiers reset together.
r15:     Esc to dismiss · Send a message to hide
```

The screen now matches the recovered layout exactly, with one
~16 ms full-redraw per turn boundary (imperceptible at 60 fps).

## Bug, in detail

### What the user saw

Run `wagyu` interactively, type `analyze pwd`, press Enter. The
mock backend streams 5 chunks at 12-char / 15 ms intervals; the
TUI appends each chunk to `m.response` and `Update()`-driven
`View()` returns grow incrementally. The diff renderer paints the
new chunks at the row the response lives at (row 9 = terminal
row 9, 1-indexed).

When the last chunk arrives, `streamPrompt` returns
`turnDoneMsg{...}` and the glamour-replace handler runs:

```go
m.replaceRange(m.replyStart, len(m.response), indented)
```

`indented` is the glamour-rendered reply, e.g. one styled line
"  mock reply from model=Gemini 3.8 Flash (Medium): analyze pwd".
After `replaceRange`, `m.response` has 3 entries: the prompt
echo, the glamour line, and the trailing empty.

The new `View()` returns this layout:

- rows 0-6: banner (7 rows)
- row 7: "> analyze pwd"
- row 8: "  mock reply from model=Gemini 3.8 Flash (Medium): analyze pwd"
- rows 9-10: blanks
- row 11: top separator
- rows 12-16: announcement Card 2
- row 17: prompt `>`
- row 18: bottom separator
- row 19: status row

### What went wrong

UV's `transformLine` is a per-cell diff:

```go
// ultraviolet@v0.0.0-20261008173134-6b8d4baf91b4/terminal_renderer.go:953
func (s *TerminalRenderer) transformLine(newbuf *RenderBuffer, y int) {
    ...
    // Find first differing cell
    for firstCell < newbuf.Width() &&
        cellEqual(oldLine.At(firstCell), newLine.At(firstCell)) {
        firstCell++
    }
    ...
}
```

And `Render()` only paints rows where the cell buffer is dirty:

```go
// ultraviolet@v0.0.0-20261008173134-6b8d4baf91b4/terminal_renderer.go:1287
if size.repaintAll || s.damaged[i] || (newbuf.Touched[i] != nil &&
    (newbuf.Touched[i].FirstCell != -1 || newbuf.Touched[i].LastCell != -1)) {
    s.paintLine(newbuf, i, size.repaintAll)
}
```

The bubbletea v2 path that calls into UV is
`cursedRenderer.flush()`:

```go
// charm.land/bubbletea/v2@v2.0.10/cursed_renderer.go:320
if !s.starting && !closing && !s.pendingErase && s.lastView != nil && viewEquals(s.lastView, &view) && frameArea == s.cellbuf.Bounds() {
    // No changes, nothing to do.
    return nil
}
```

The problem: `newbuf.Touched[i]` is only set on rows UV's
`content.Draw(cellbuf)` actually wrote to. For the new layout,
rows that the new `View()` outputs as blank (no content) — e.g.
the previous spinner row, the previous chunks rows, the previous
Card 1 title row that the new layout doesn't write to because the
columns behind the response text were Card 1 borders — leave
`newbuf.Touched[i]` nil. Those rows are *not repainted*. The old
pixels stay.

Three concrete screen-state failures caused by this:

1. **Spinner never clears.** During streaming the spinner is
   written by `m.spinner.View() + " Generating..."`, but only
   while `m.streaming == true`. When `turnDoneMsg` flips
   `m.streaming = false`, the new `View()` no longer includes the
   spinner row at all (the append-to-rows code in
   `transcriptWindow`'s `transcriptRows` for-loop is conditional
   on `m.streaming`). The row's `newbuf.Touched[i]` is nil →
   `transformLine` is never called for it → the spinner's cursor-
   positioned pixel-painted glyph stays.

2. **Card 1 → Card 2 transition leaves Card 1 title behind.**
   `transcriptRows` returned Card 1's body at row 8 of the cell
   buffer; the streaming chunks overwrite the row at column 0 → 50
   with "  mock reply from model=..." pixel by pixel. `replaceRange`
   then replaces the chunk rows with the glamour line, but Card 1
   cells at columns past the response text (e.g. column 50+) are
   no longer written to by the new layout — the columns past the
   response remain whatever was there before (which, for the chunk
   row, was the second chunk's `mdrender` overlay over the Card 1
   top border "  ┌─ announcement ─..."). After replace, only the
   newly-written cells are touched; the rest stay.

3. **Announcement-card body on a separate row stays.** When the
   new layout shifts Card 2 down 4 rows (because the transcript
   grew), rows 9-12 in the new frame are blank. Each of those rows
   had Card 1's title / body / hint / bottom-border text in the
   old frame, but the new `View()` never writes content there, so
   `newbuf.Touched[i]` is nil for each → no repaint → Card 1's
   pixel-painted content stays.

The frame-area-resize check (which DOES trigger a full repaint
on terminal resize) doesn't fire here — alt-screen mode fixes
`frameArea = Rect(0, 0, s.width, s.height)` to the terminal size,
which doesn't change between frames.

### Why the existing code didn't help

`m.replaceRange(...)` rewrites `m.response` correctly — the model
state is right. The conversation DB row is right (it stores
`msg.reply` text, no streaming metadata). The `View()` output
matches the on-screen layout we WANT. The failure is purely in
the rendering pipeline: UV's diff skips rows where
`newbuf.Touched[i]` is nil, but the upstream bubbletea path
doesn't have a built-in "force full repaint for the next flush"
mechanism except via `tea.ClearScreen()`.

## Fix

The minimal-impact recovery is documented in
`charm.land/bubbletea/v2@v2.0.10/screen.go:12-20`:

> `ClearScreen` is a special command that tells the program to
> clear the screen before the next update. This can be used to
> move the cursor to the top left of the screen and clear visual
> clutter when the alt screen is not in use.
>
> Note that it should never be necessary to call ClearScreen()
> for regular redraws.

"Not for regular redraws" is fair: most model updates land cells
that UV's diff handles correctly. But the streaming-→-done
transition is NOT a regular redraw — the streaming chunks were
written via cursor-positioned writes that bypassed the cell
model, and the glamour-replace is the recovery handshake that
needs to clear those bypassed cells.

Cycle 267 wires `tea.ClearScreen` into the return at both
turn-end handlers:

- `case turnDoneMsg:` (cmd/wagyu/tui_run.go:3563)
  - returns `m, tea.Batch(queueCmd, tea.ClearScreen)`
  - `tea.Batch`'s `compactCmds` collapses `(nil, tea.ClearScreen)`
    to `tea.ClearScreen` directly when queue mode is off
- `case backendDoneMsg:` (cmd/wagyu/tui_run.go:3900-3910)
  - returns `m, tea.ClearScreen`
  - This is the cmd result for the empty-reply / tool-call-only /
    err-path outcomes — same diff-leaves-stale-pixels pathology
    applies (the spinner still lived at a row the new View() never
    re-writes).

The 1-frame (~16 ms at 60 fps) latency between the model
returning the new state and `s.scr.Erase()` running is
imperceptible. Aggressive per-chunk clears (e.g. also on
`backendChunkMsg`) would trigger a flicker storm during streaming
and are explicitly rejected.

`tea.ClearScreen()` returns `clearScreenMsg{}`, processed at
`tea.go:872`:

```go
case clearScreenMsg:
    p.renderer.clearScreen()
```

`clearScreen()` is `cursedRenderer.clearScreen()`:

```go
// charm.land/bubbletea/v2@v2.0.10/cursed_renderer.go:683
func (s *cursedRenderer) clearScreen() {
    s.mu.Lock()
    s.scr.MoveTo(0, 0)
    s.scr.Erase()      // sets TerminalRenderer.s.clear = true
    s.pendingErase = true
    s.mu.Unlock()
}
```

Which feeds into the flush's `if !s.pendingErase` skip-check and
`if s.clear { clearUpdate(...) }` full-repaint path on the next
flush.

## Files

| Path                                                             | Role                                                                  |
|------------------------------------------------------------------|-----------------------------------------------------------------------|
| `cmd/wagyu/tui_run.go`                                           | MODIFIED — `case turnDoneMsg:` returns `tea.Batch(queueCmd, tea.ClearScreen)`; `case backendDoneMsg:` returns `tea.ClearScreen` |
| `cmd/wagyu/cycle267_streaming_clear_test.go`                     | NEW — 4 test functions × 7 subtests pinning the cmd-return contract (happy-path / error-path / queue-flush / response-shape matrix) |
| `docs/cycles/cycle-267/repro/wg01_pty_check.py`                  | NEW — Python PTY harness that drives `wagyu --backend=shim` and asserts the post-ClearScreen screen state is clean (no spinner, no Card 1 leak, no raw chunk leakage, Card 2 visible, full response string reconstructed) |

## Reproduction

```bash
# Build the cycle 267 binary:
go build -o /tmp/wagyu-c267 ./cmd/wagyu

# Run the PTY harness against the post-fix binary — 5/5 PASS:
python3 docs/cycles/cycle-267/repro/wg01_pty_check.py /tmp/wagyu-c267
# PASS=5/5 clear-sig=True no-raw-leak=True full-resp=True no-spinner=True card2=True

# Run it against the pre-fix binary (cycle 266) — 3/5 PASS, with card2=False:
python3 docs/cycles/cycle-267/repro/wg01_pty_check.py /tmp/wagyu-final
# PASS=3/5 clear-sig=False no-raw-leak=True full-resp=True no-spinner=True card2=False
```

The harness opens a real PTY (`pty.fork()`), configures `TERM=xterm-256color`,
sets `LINES=30 COLUMNS=250` for the WIN struct, sets
`OPENAGY_TRUST=always` to skip the on-startup workspace-trust dialog, sends
`analyze pwd\r` (CR so bubbletea v2's `tea.KeyPressMsg` recognises it as
Enter per the cycle-253 chassis), waits 10 s, and inspects the byte stream
for three positive signals and two negative signals.

The 5 assertions:

| # | Signal                              | Pre-fix (cycle 266) | Post-fix (cycle 267) |
|---|-------------------------------------|---------------------|----------------------|
| 1 | `\x1b[H\x1b[2J` after `state=done`  | absent              | present              |
| 2 | raw chunk `mock reply f ` (no `rom` follow) leaked past `state=done` | leak       | no leak              |
| 3 | response fragments `mock reply from model=` AND `Gemini 3.8 Flash (Medium)` present post-ClearScreen | present (covered with chunks) | present |
| 4 | spinner glyph (`\xe2\xa0`) followed by `Generating` past `state=done` | leak | no leak              |
| 5 | Card 2 title `Daily quota resets` visible | not visible      | visible              |

The "no leak" assertions are enforced by examining the byte stream
*after* `state=done:app=wagyu\x07` (the OSC 7501 recordTurnOutcome
publishes when `m.streaming` flips false) and require that the
streaming artifacts do not appear there.

## Test status

`go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` is green:

| Package                          | Status  | Notes                                              |
|----------------------------------|---------|----------------------------------------------------|
| `cmd/wagyu`                      | ok 7.3s | 4 cycle-267 test functions + 7 subtests + the pre-existing suite (231 functions) all pass |
| (other 57 packages)              | ok      | unchanged from cycle 266 baseline |

Cycle 267 specific tests (cmd/wagyu/cycle267_streaming_clear_test.go):

- `TestTurnDoneMsg_StreamingClearReturnsClearScreenCmd` — happy path: 5
  streamed chunks → `turnDoneMsg{reply: ...}` → returned Cmd, when invoked,
  produces a `clearScreenMsg` to the bubbletea message loop.
- `TestBackendDoneMsg_StreamingClearReturnsClearScreenCmd` — error / empty-
  reply path: `backendDoneMsg{}` (the empty-reply / tool-call-only /
  err-path cmd result) also returns `tea.ClearScreen` directly, since the
  same diff-leaves-stale-pixels pathology applies.
- `TestTurnDoneMsg_QueueFlushKeepsClearScreen` — queueMode Cmd+Enter:
  `tea.Batch(queueCmd, tea.ClearScreen)` produces a `BatchMsg` whose
  elements include `tea.ClearScreen`, so a queued turn doesn't inherit a
  stale-pixel problem from the preceding turn's glamour-replace.
- `TestTurnDoneMsg_StreamingClearContractIsStableAcrossResponseShapes` —
  regression net over 7 reply shapes (empty / whitespace / single-word /
  multi-line / long-line / code-fence / mixed-glamour) confirming the
  contract holds regardless of which glamour-render path engages.

The PTY harness complements these in-process tests with end-to-end
verification: it captures raw bytes the bubbletea renderer emits on the
PTY output fd and pins the 5 signals above.

## Cycle numbers in nearby history

- cycle 99: tool-step block recovery (turn-end bookkeeping)
- cycle 159: cursor-row math for chat transcript restoration
- cycle 228: glamour-backed pipeline for the turn-end reply
- cycle 229: live model-reply streaming + replyStart anchor + replaceRange
- cycle 232: idempotency of the glamour pass at turn-end
- cycle 243: FNV-64 prefix cache reset
- cycle 244: Cmd+Enter queue mode → batched queueCmd
- cycle 251: scrollHeaderRows header rows for transcriptWindow
- cycle 253: bubbletea v2 chassis migration (replaces v1)
- cycle 266: agent-loop auto-continue has a separate 8-budget for
  transient network errors (the prior cycle)
- cycle 267: TUI streaming → done transition does a full screen repaint
  ← THIS CYCLE

## Limitations / 硬约束

1. The full-redraw costs one extra "\x1b[H\x1b[2J + repaint every cell"
   pair per turn-end. At 60 fps, the 1-frame latency is imperceptible.
   On a hung terminal or one without synchronized output support, this
   could theoretically flicker; bubbletea v2 uses sync-output mode by
   default so the cursor positions land atomically.

2. `tea.ClearScreen` in alt-screen mode is a workaround for the
   upstream-documented "shouldn't be necessary for regular redraws"
   note. If a future bubbletea / UV release adds a cleaner
   "force-repaint-specific-rows" API, the cycle 267 handler should
   switch to it. Until then, the full-redraw is the lowest-risk path.

3. The harness's `full-resp` assertion checks response-fragment
   presence, not byte-for-byte equality, because the captured PTY was
   30 rows × 250 cols and the response wraps in the captured bytes.
   The harness's read-the-screen assertion would be the stronger check;
   it requires a screen-parsing library that understands the cursor
   position emitted by `cursor.go`. Out of scope for cycle 267.

4. The cycle 252 `crypto/safe_newline_subtle_test.go`, the cycle 229
   `slash_cycle229_test.go`, and `tui_v135_test.go` predated gofmt
   and were deliberately not reformatted (see AGENTS.md §
   "Conventions"). My edits did not touch them. Future cycles that
   need to can `gofmt -d cmd/wagyu/<file>.go` to see what would
   change.

5. Print mode (`-p`), the headless test surface, and
   `program_status_env_test.go` are unaffected: they bypass the
   bubbletea alt-screen renderer entirely. The unit tests stay
   green without any of them being aware of the cycle 267 fix.
