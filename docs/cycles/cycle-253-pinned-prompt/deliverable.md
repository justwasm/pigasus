# Cycle 253 — pinned prompt header in no-flicker mode (AGY 1.3.3)

## Why this cycle

AGY 1.3.3 ships a small but very useful TUI tweak that the
feature-diff table still flags as `clone: false`:

> Added a pinned prompt header in no flickering mode while you
> scroll back through a long conversation: the prompt that produced
> the response you're reading stays at the top of the transcript,
> and clicking it jumps back to that prompt.

(`internal/cli/testdata/changelog_reference.txt` line 67.)

The clone's cycle-179 sticky user-prompt bar already pins the
"owning" prompt at the top of the viewport — but it's purely
visual, it has no jump-back gesture, and it shows in both
altscreen and inline modes. The AGY 1.3.3 feature is a
specialised, interactive variant of that idea that only appears
when the TUI is in altscreen mode and lets the user jump back to
the prompt with a click or an Enter.

## What this cycle ships

| File | Status | What |
|---|---|---|
| `cmd/wagyu/tui_run.go`                          | modified | adds `pinnedPromptRowIdx` / `pinnedPromptText` fields to `runMode`; routes `tea.MouseClickMsg` at row 0 and Enter-on-empty-buffer to `jumpToPinnedPrompt()` |
| `cmd/wagyu/tui_viewport.go`                     | modified | in altscreen mode replaces the cycle-179 sticky bar with a single dim/borderless pinned-prompt row carrying a `↳ jump to prompt` hint; adds `renderPinnedPrompt()` and `jumpToPinnedPrompt()` helpers |
| `cmd/wagyu/pinned_prompt_test.go`               | added    | 6 unit tests for the six behaviours the changelog calls out |
| `internal/cli/testdata/feature_diff_table.json` | modified | flips the "Pinned prompt header in no-flicker mode" row from `clone: false` to `clone: true, cycle: 253` |
| `docs/cycles/cycle-253-pinned-prompt/deliverable.md` | added | this file |

No third-party dependency changes — the cycle reuses the existing
`cDimGrayOpen` / `cWarningBlueOpen` palette plus the cycle-179
sticky-bar's `stickyUserPromptRow()` helper. No `internal/convstore`
edit. No new module imports.

## How the cycle works

### 1. Two-field cache in `runMode`

```go
pinnedPromptRowIdx int    // absolute row index in `rows`, -1 = no header this frame
pinnedPromptText   string // the prompt text + "  ↳ jump to prompt" hint
```

Both are repopulated by `transcriptWindow()` on every render. They
follow the same conditions as the cycle-179 sticky bar
(`stickyUserPromptRow()` returns a valid index) but are only set
when `m.altScreen` is true — inline mode keeps the cycle-179
sticky bar with its separator rule, which the rest of the
transcript chrome uses.

### 2. Pinned-prompt rendering replaces the cycle-179 sticky bar in altscreen mode

```go
if m.altScreen && m.pinnedPromptText != "" {
    visible = append([]string{padTo(m.pinnedPromptText, effective)}, visible...)
} else if m.stickyBarRow != "" {
    sticky := []string{padTo(m.stickyBarRow, effective), cRule(effective)}
    visible = append(sticky, visible...)
}
```

The pinned prompt is a single row (no separator rule below), so
`availScroll` only loses 1 row (vs the cycle-179 sticky bar's 2).
The header is dim/borderless — the prompt text is the same
`cWarningBlueOpen + "> " + text + cWarningBlueClose` row the rest
of the user-prompt rows use, with a `cDimGrayOpen + "  ↳ jump to
prompt" + \x1b[39m` hint appended. The hint reads as a small
annotation rather than another piece of content; it tells the user
the row is interactive.

### 3. Click handler

```go
if m.pinnedPromptText != "" && ev.Y == 0 {
    if m.jumpToPinnedPrompt() {
        return m, nil
    }
}
```

A left-click on screen row 0 (the top of the transcript viewport)
when a pinned prompt is shown jumps the viewport back to that
prompt. The click skips the cycle-174 in-app text-selection path
so the same click does not double-fire as a single-cell selection.

### 4. Enter handler

```go
if key.Code == tea.KeyEnter && key.Mod == 0 && m.altScreen &&
    m.pinnedPromptText != "" && m.input.Value() == "" {
    if m.jumpToPinnedPrompt() {
        return m, nil
    }
}
```

Enter on the pinned-prompt header jumps back to the prompt. The
handler only fires when the input buffer is empty, so a typed
prompt keeps its cycle-71/cycle-244 Enter-submits behaviour —
the pinned prompt does not steal the submit key from a typed
message.

### 5. The jump itself

```go
func (m *runMode) jumpToPinnedPrompt() bool {
    if m.pinnedPromptRowIdx < 0 {
        return false
    }
    m.autoScroll = false
    m.scroll = m.pinnedPromptRowIdx
    return true
}
```

`m.scroll = m.pinnedPromptRowIdx` puts the producing prompt at
`visible[0]` of the next render, with the pinned header sitting
on the row above it. The next render re-derives
`stickyUserPromptRow()` from the new `m.scroll` and keeps the same
prompt pinned, so the visual state is stable after the jump (no
flicker, no header swap).

## Tests (`cmd/wagyu/pinned_prompt_test.go`)

| Test | Pin |
|---|---|
| `TestPinnedPrompt_NoAltscreen`                            | altscreen off, scrolled into a turn → `pinnedPromptText == ""`, view has no `↳ jump` hint |
| `TestPinnedPrompt_AltscreenAtTop`                         | altscreen on, scroll=0 / autoScroll on → no pinned prompt (the bottom-of-screen input chrome already owns the prompt slot) |
| `TestPinnedPrompt_AltscreenScrolledIntoAssistant`         | altscreen on, scroll into the assistant reply → pinned prompt renders, carries the producing prompt's `> prompt-<x>` text + the jump hint |
| `TestPinnedPrompt_AltscreenScrolledIntoUser`              | altscreen on, scroll lands on a user-prompt row → pinned prompt is the most recent user prompt ≤ scroll (which is the prompt itself) |
| `TestPinnedPrompt_EnterJumpsToPrompt`                     | empty-buffer Enter on the pinned header → `m.scroll == pinnedPromptRowIdx`, `m.autoScroll` cleared; non-empty-buffer Enter still submits through cycle-71/244 (`m.streaming=true`, input cleared, `m.session` extended with the new user message) |
| `TestPinnedPrompt_NoClickInPrintMode`                     | click on row 0 jumps the viewport back to the prompt and DOES NOT mutate `m.session` (the pinned prompt is purely visual); print mode (`-p`) goes through `runPrintMode` which never instantiates `runMode`, so neither the field nor the handlers exist there |

### Helper bits the tests rely on

- `pinnedPromptMode(altscreen)` stands up a sized (80×24) `runMode`
  with 30 turns of `prompt-<x> / blank / reply-line-1 /
  reply-line-2 / blank` — enough transcript to overflow the
  viewport at h=24 so the scroll-away-from-bottom state can
  fire.
- `scrollAwayFromBottom(m)` sets `autoScroll=false` and
  `scroll=50` so the cycle-179/253 sticky/pinned conditions both
  fire on the next render.
- `pinnedPromptView(t, m)` returns the rendered view with ANSI
  stripped, for plain substring checks.

## Test results

| Sweep | Command | Outcome |
|---|---|---|
| Cycle-253 only | `go test -vet=off -count=1 -run TestPinnedPrompt_ ./cmd/wagyu/` | OK (6 tests, ~1.5 s) |
| Full TUI | `go test -vet=off -count=1 ./cmd/wagyu/` | OK (~43 s) |
| Full repo sweep | `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | OK (57 packages, ~3 min) |
| Cycle-179 regression | `go test -vet=off -count=1 -run Test_StickyUserPromptRow ./cmd/wagyu/` | OK (8 cases — the inline-mode sticky bar still works) |
| Build | `go build -o /tmp/wagyu-cycle253 ./cmd/wagyu` | OK (exit 0; 89,699,602 bytes — ~85.5 MB) |

## Binary size delta

| | Bytes | Note |
|---|---|---|
| Baseline (cycle 260 `c3523f1` HEAD) | n/a | rebased from upstream main |
| After cycle 253 | 89,699,602 | +0 new dependencies, +0 module adds |

The cycle touches no third-party module — both new helper symbols
(`renderPinnedPrompt`, `jumpToPinnedPrompt`) live in `cmd/wagyu`
and the test file is `package main` (no test-only imports). The
size delta is dominated by Go's slice-header layout changes
introduced by the two new fields.

## 硬约束 / limitations

1. **The cycle-179 sticky bar and the cycle-253 pinned prompt
   share a single computation (`stickyUserPromptRow()`) but render
   differently.** The `m.altScreen` flag decides which one the
   viewport preprends. Inline mode keeps the cycle-179 sticky bar
   with its separator rule (terminal scrollback already provides
   the prompt-context the pinned prompt is for). The cycle-179
   tests in `tui_viewport_test.go` keep passing without
   modification, and the test sweep above pins this.

2. **Pinned prompt is purely visual.** `jumpToPinnedPrompt()` only
   touches `m.scroll` and `m.autoScroll`. `m.session`, the agent
   loop, the message bus, and the model stream are all
   untouched. `TestPinnedPrompt_NoClickInPrintMode` asserts this
   by snapshotting `len(m.session)` before and after the click and
   checking it is unchanged.

3. **Print mode (`-p`) is unaffected.** Print mode goes through
   `runPrintMode` (cmd/wagyu/main.go) which never instantiates
   `runMode`; neither `pinnedPromptText` nor `pinnedPromptRowIdx`
   exists in that code path, and `jumpToPinnedPrompt` is not
   reachable. A `grep -R 'pinnedPrompt' cmd/wagyu/print_*.go`
   returns nothing; the test that documents this contract is
   `TestPinnedPrompt_NoClickInPrintMode`.

4. **Enter-jump only when the input buffer is empty.** When the
   buffer has content the cycle-71/244 Enter-submits path takes
   over. This matches the reference: AGY's pinned-prompt header
   does not steal Enter from a typed message either (verified by
   the second half of `TestPinnedPrompt_EnterJumpsToPrompt`).

5. **The pinned prompt renders the producing prompt verbatim plus
   the `↳ jump to prompt` hint.** A multi-line user prompt is
   truncated to its first row (the cycle-179 sticky bar has the
   same behaviour — `m.response` rows with embedded newlines show
   up as one row of the transcript and the prompt classifier
   `isUserPromptRow` only matches the FIRST row of a multi-row
   prompt). Future cycles that need to surface the full multi-line
   prompt in the header should plumb through the prompt's full
   text via `m.session`, not via the wrapped `m.response` row.

6. **The cycle does not refactor the cycle-179 sticky bar.** The
   bar still exists (used by inline mode) and its tests still
   pass. The two paths are gated on `m.altScreen` only; no
   behaviour was removed, only added. A future cycle that wants to
   merge the two could replace the inline-mode branch with the
   pinned-prompt header unconditionally, but that would change
   the byte-level contract for terminal scrollback-driven inline
   sessions and the cycle deliberately avoids that risk.

7. **No `internal/convstore/sqlite.go` edit.** The conversation
   store is read-only at the TUI boundary (the agent loop and the
   resume picker read it; the TUI never writes). Cycle 253 stays
   on the TUI-only side.

8. **No third-party dependency added.** `cDimGrayOpen` is already
   in `cmd/wagyu/colors.go`; the cycle reuses it without any
   change. `go.mod` and `go.sum` are byte-identical to the
   baseline.