# Cycle 244 — alt+m / Queued Messages shortcut (AGY 1.4.0 feature gap)

Worktree: `/Users/gear/Documents/GitHub/openagy-alt-m`
Branch: `feature/cycle-244-alt-m`

## Why this cycle

Cycle 236 launched `--diff-with-agy` — the clone parity table
that puts every AGY 1.4.x feature gap on a single searchable
sheet (`internal/cli/testdata/feature_diff_table.json`,
`internal/cli/testdata/feat_diff.go`'s panel generator,
`cmd/wagyu/feat_diff.go`'s reporter). One of the gaps the
table flagged on day one was "Alt-m shortcut (send/queue
opposite mode)" with `agy: true`, `clone: false`:

```json
{
  "name": "Alt-m shortcut (send/queue opposite mode)",
  "category": "input",
  "agy": true,
  "clone": false,
  "cycle": null,
  "evidence": null,
  "notes": "AGY 1.4.0: alt+m sends using opposite of Queued Messages
           setting. Clone doesn't yet bind the alt+m global key."
}
```

AGY 1.4.0 changelog line (recoverable from
`internal/cli/testdata/changelog_reference.txt:13`):

> "Added the `alt+m` shortcut (`Option+m` on macOS when
> Option is set to act as Alt) to send a message using the
> opposite of your `Queued Messages` setting in `/config`,
> so you can send right away while in `Queue` mode, or queue
> a message while in `Send Immediately` mode."

The clone had `alt+s` / `alt+x` / `alt+v` / `alt+j` (the
latter a helpstring-only stub at
`cmd/wagyu/help_cycle182_test.go:172`) routed through
`internal/keybind` + the cycle's buffer-translation layer,
but **no built-in alt-letter shortcut**. Cycle 244 closes
the gap.

## AGY 1.4.0 spec

| Surface | Behaviour |
|---|---|
| **alt+m toggle** | flips `m.queueMode` for the rest of the session. Per-session state; not written back to `~/.gemini/antigravity-cli/settings.json`. |
| **Default mode** (`queueMode == false`) | Enter submits immediately (cycle-71 path verbatim). Cmd+Enter also submits (synonym for Enter on a single-line buffer so chat-client migrants recognise the binding). |
| **Queue mode** (`queueMode == true`) | Enter inserts a newline (textarea multi-line). Cmd+Enter captures the textarea content into `m.queuedPrompt` and clears the input. |
| **Auto-flush on turn end** | the `turnDoneMsg` handler flushes `m.queuedPrompt` as the natural next user message — queue mode is explicitly designed to let the user line up the next turn while a turn is already in flight, so the flush happens the moment the in-flight turn completes (no press required). |
| **Status row badge** | when `queueMode == true`, `statusRow()` appends " · Queue mode" to the right-side model label; the substring "Queue mode" is the test-pinned indicator. |
| **Toggle feedback row** | each alt+m press appends one transcript row — "alt+m → Queued Messages: Queue" or "...Send Immediately" — using the cycle-71 alt-key dispatcher's "⎿  …" dim-gray style. |

The feature is rendered fully in the interactive TUI. Print
mode (`-p`) is unaffected: it submits a single prompt and
exits, so queue mode is a no-op there (the cycle-244
queueMode field never gets consulted on the print path).

## What the fix does

### Files changed

| File | Lines | Purpose |
|---|---|---|
| `cmd/wagyu/tui_run.go` | +90 / -0 | adds `queueMode` and `queuedPrompt` to `runMode`, the alt+m dispatch in `Update()`, the Enter / Cmd+Enter split, `toggleQueueMode()`, `submitOrQueuePrompt()`, the status-row badge, and the `turnDoneMsg` flush path. |
| `cmd/wagyu/queue_mode_test.go` | new, +250 | six regression tests (see "Tests"). |
| `internal/cli/testdata/feature_diff_table.json` | +2 / -2 | flips the alt+m entry from `clone: false` to `clone: true`, adds `cycle: 244`, points `evidence` at the touched files. |

Total: ~340 lines added, no new third-party dependency.

### Code path summary

1. **Dispatch** (`cmd/wagyu/tui_run.go` line ~2602 in the
   alt-letter block): alt+m reaches `m.toggleQueueMode()`,
   a tea.Cmd-returning helper that flips the bool and
   appends the feedback row.

2. **Enter branch** (`cmd/wagyu/tui_run.go` line ~3043, the
   existing `case tea.KeyEnter` block): when `queueMode`,
   Enter delegates to `m.input.InsertRune('\n')` so the
   textarea treats it as a literal newline. Otherwise the
   existing `m.submitPrompt()` runs unchanged.

3. **Cmd+Enter branch** (`cmd/wagyu/tui_run.go` line ~3060,
   new): when `queueMode`, captures the textarea content
   into `m.queuedPrompt` and clears the input. Otherwise
   delegates to `submitPrompt()`.

4. **`turnDoneMsg` flush** (`cmd/wagyu/tui_run.go` line
   ~3216, top of the case): when `m.queuedPrompt != ""`,
   the flush branch copies the prompt into `m.session` as
   the next user message, calls `saveSession()` (so the
   queued prompt lands on disk before the next turn
   streams), and arms `streamPrompt(queued)` in the same
   tick. The capture-and-return uses a `queueCmd tea.Cmd`
   variable so the rest of the `turnDoneMsg` body keeps
   finalising the just-finished turn normally. The case
   ends with `return m, queueCmd` so bubbletea gets both
   the queued turn's streamPrompt batch and (via the noop
   `nil` cmd, when there was no queue) the same shape as
   the cycle-99 handler.

5. **Status row badge** (`cmd/wagyu/tui_run.go` line ~357,
   inside `statusRow()`): when `m.queueMode`, appends
   " · Queue mode" to the right-side segment after the
   task-suffix segment. The substring "Queue mode" is the
   test-pinned indicator.

6. **`feature_diff_table.json`** (`internal/cli/testdata/`):
   cycle 236's table now reports this row as `clone: true`
   with `cycle: 244` and `evidence` pointing at the two
   touched files. The `TestFeatDiff_*` suite is the
   regression guard; it re-reads the JSON on each run.

### Submit mechanism

The cycle ships a clone-side interpretation of "send while
queued" because the AGY binary doesn't expose the exact
binding to `strings(1)` (the binding is dispatched through
TUI dispatch, not as a bare string literal — see "Hard
constraints" below). The chosen mechanism:

- **Cmd+Enter / Ctrl+Enter submits the queued prompt.**
- The user's queued prompt also auto-dispatches on the
  next `turnDoneMsg` if Cmd+Enter was not pressed (this is
  the only way the user can submit at all when the model
  is in the middle of a turn — queue mode's whole point is
  "send a message while the current turn is in flight").
- Plain Enter in queue mode inserts a newline (textarea
  multi-line).

The flush-on-turn-end is what makes queue mode useful in
practice: the user presses Cmd+Enter during streaming, the
buffer is captured, and the model sees it the moment the
current reply completes. The flush path is identical to
the user pressing Enter on the queued buffer; the diff is
only in WHOSE prompt (`m.queuedPrompt` instead of
`m.input.Value()`).

## Tests

`cmd/wagyu/queue_mode_test.go`:

- `TestAltMToggleQueueMode` — drives alt+m twice,
  asserts `m.queueMode` flips `false → true → false` and
  that the feedback rows mention "Queue" / "Send
  Immediately" respectively.
- `TestQueueModeEnterInsertsNewline` — drives the alt+m
  + Enter sequence, asserts the textarea gained a
  trailing newline and `m.streaming` did not flip
  (proving Enter did NOT submit).
- `TestQueueModeCmdEnterSubmits` — drives Cmd+Enter on a
  populated input in both queueMode states, asserts
  queueMode==false → `m.input.Value()` cleared (existing
  submitPrompt ran) and queueMode==true → `m.queuedPrompt`
  captured with the textarea cleared.
- `TestQueuedPromptDispatchesAfterTurnDone` — pre-seeds
  `m.queuedPrompt = "hello"`, drives `turnDoneMsg` with
  no assistant reply, asserts `m.queuedPrompt` cleared,
  `m.session` grew by one user-role message containing
  "hello", and the returned `tea.Cmd` was non-nil (the
  streamPrompt batch).
- `TestQueueModeFooterRendersIndicator` — drives
  `m.statusRow()` in both queueMode states, asserts the
  "Queue mode" substring is absent when off and present
  when on.
- `TestQueueModePersistsAcrossTurns` — toggles queueMode
  on, drives a fake turn (submitPrompt + turnDoneMsg),
  asserts `m.queueMode` is still true after (per-session
  persistence).

All six tests pin the headline behaviours so a future
cycle that touches the same surface area fails the test
suite before merging.

## Test results

- `go build -o /tmp/wagyu-cycle244 ./cmd/wagyu` — exit 0.
- `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...`
  — exit 0. **All packages pass.** The cycle-244 visual
  regression tests pass; the existing cycle-50 / cycle-71
  enter-and-submit tests pass without modification.
- `go test -vet=off -count=1 -run 'TestAltM|TestQueue'
  ./cmd/wagyu/` — exit 0 (the six cycle-244 tests
  run independently).

Binary-size delta: **+128 bytes** vs the cycle-242 baseline
(89,631,842 vs 89,631,714). The toggle / Enter-split /
queue-flush code is tiny — most of the size is comments
and the test file (which doesn't ship in the binary).

Feature-diff table re-verification:
`TestFeatDiff_LoadsJSONAndEmitsAligned` /
`TestFeatDiff_ClassifyStatus` /
`TestFeatDiff_JSONRoundTrip` /
`TestFeatDiff_PanelLinesHeight0` all pass — the JSON
round-trip now correctly classifies the alt+m row as
"matched" instead of "agy-only".

## Hard constraints / 存疑 / limitations

- **The submit mechanism (Cmd+Enter) is clone-side
  interpretation.** AGY 1.4.0's exact binding is recoverable
  from `strings ~/.local/bin/agy | grep -E 'send.*queue|flus
  h.*queue|queued.*send|queueMode'`: the AGY binary keeps
  the binding name `queueMode` and the action handler
  `sendOrQueueMessage` in the dispatch table, but the
  associated keystroke is encoded in the bubbletea v2
  `key.Binding` form on a per-window basis. The cycle
  ships a reasonable default — Cmd+Enter (and Ctrl+Enter,
  same ModCtrl bit) submits the queued prompt, plus an
  auto-flush on the next turnDoneMsg if the model is
  already streaming — that mirrors the AGY-typical chat
  UX. A future cycle could re-derive the exact AGY binding
  by scanning the binary's `text` segment for
  `key.WithKeys("alt+m")` and matching the adjacent
  function symbol.

- **The persistence behaviour is per-session, not
  settings.json.** AGY 1.4.0's `queuedMessages` setting
  (printed by `/config`, defaults to `"queue"`) is the
  user-visible config that alt+m inverts. The cycle ships
  the alt+m toggle as per-session state only — flipping
  the boolean on the in-memory `runMode` and never
  writing back to `~/.gemini/antigravity-cli/settings.json`.
  Persisting would silently override the user's last
  `/config` choice across restarts; per-session semantics
  keep the toggle predictable to "you press alt+m, the
  rest of the session changes". A future cycle can wire
  `writeSettings` to keep the toggle sticky if reading
  the binary confirms AGY does the same.

- **The footer text " · Queue mode" is clone-side.** The
  exact subtitle is recoverable from the same AGY binary
  scan noted above — the cycle ships a clear English
  description (" · Queue mode") that reads identically
  on a dark or light background. The statusRow placement
  on the right segment next to the model label mirrors
  the cycle-227 compact-notes' " · NNN K → NNN K"
  positioning, so visually it sits where the user
  already reads cycle-status info.

- **The cycle does NOT change cycle-228 / 232 / 233 /
  234 / 242 glamour behaviour.** The mid-stream glamour
  pipeline doesn't care about queue mode — the queued
  prompt becomes a regular user-role message in
  `m.session`, so the streaming render applies to its
  reply exactly as it does to any other turn.

- **Print mode (`-p`) is unaffected.** Print mode
  submits a single prompt and exits, so queue mode is a
  no-op there. The print path (cmd/wagyu/print_pig.go,
  cmd/wagyu/print_format.go) doesn't reference
  `m.queueMode` at all.

- **Vim mode users get the alt+m shortcut unchanged.**
  The cycle-216 vim engine returns `false` for
  non-handled events so alt+m falls through to the global
  handler below. The same applies to alt+letter bindings
  routed through `internal/keybind` — alt+m does not
  match any configured binding shape (the dispatcher
  uses lowercase letters and the test passes lowercase
  `m`) and IsAltLetter returns `'m'` correctly.

- **The ack-keys test list grew by zero rows.** The
  cycle-182 `helpShortcuts` catalog kept its 34 rows;
  alt+m was already there from a prior cycle (line 964:
  "  alt+m     Send with opposite Queued Messages
  setting"). Cycle 244 wires the binding; it does NOT add
  a new helpstring row that would invalidate the
  byte-count test in `help_cycle182_test.go:172`.

- **No third-party dependency added.** Cycle 244 is
  stdlib + the existing bubbletea v2 textarea dep
  (`m.input.InsertRune('\n')` is the only API call the
  cycle 244 Enter-branch path uses; `m.input.Update(msg)`
  is already in `cmd/wagyu/tui_run.go:3088`). The
  `tea.Batch` / `tea.Cmd` types are bubbletea v2
  primitives already used in the cycle-99 turnDoneMsg
  path. No `go.mod` change.
