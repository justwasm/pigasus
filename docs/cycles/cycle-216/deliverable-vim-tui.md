# Cycle 216 — wiring Vim mode into the TUI, and correcting it against the binary

## TL;DR

Cycle 215 landed the pure `internal/vim` engine with no terminal and no
host. This cycle does two things:

1. **Wires it into the clone's prompt.** `cmd/wagyu/vim_mode.go` is the
   only bridge: it turns a bubbletea key into the engine's normalised
   `Key`, mirrors the engine buffer/cursor back onto the textarea, and
   hands a submit to the shared `submitPrompt()` path. The gate is the
   `editorMode` / `vimInsertFirst` setting pair, read once at TUI
   start-up (and whenever a slash panel closes) so no file I/O happens
   per keystroke and the existing tests stay hermetic.
2. **Corrects the engine against the reference.** A decompiled-code
   review of the reference's own Vim engine (see *Spec confirmation*
   below) **refuted three of cycle 215's rules** and added two features
   the changelog had been read too narrowly to include.

Feature set fixed by the reference's changelog and re-checked against the
decompiled engine: Normal / Insert / Visual / Visual-Line; the
`h j k l`, `w b e`, `W B E` motions; `0 ^ $ gg G`; `f F t T` with `;`/`,`;
the `i a I A` **and `o O`** insert entries; `d c y` plus `x D C`, filling
the unnamed register for `p`; `iw aw iW aW ip ap`; counts everywhere; and
`ZZ` as "submit from Normal mode".

## What changed since cycle 215

### TUI integration (new in this cycle)

| Piece | Where |
|---|---|
| Settings gate + refresh | `reloadVimSettings()` — called at the TUI entry (`tui_run.go`) and in `closePanel()` (`slash_panels.go`) |
| Key routing | `handleVimKey` — the engine gets first refusal when no overlay/panel/picker owns the screen; declining keys fall through to the host (ctrl+c, ctrl+d, Tab, ctrl+o, …) |
| Textarea mirror | `syncVimInput` — `SetValue` then walk the row up and `SetCursorColumn` |
| Mode badge | `vimBadge()` consumed by `statusRow()` |
| `/help` Vim tab | `helpTabs(m)` appends a 4th lowercase `vim` tab **only when vim is on**; `renderHelpVim` draws it; `helpListLen(3) == len(helpVim)` |
| Insert-First | the prompt opens in Insert and a bare Enter submits; modified Enter (shift/alt), `ctrl+j` insert a newline |

The badge is **prepended** to the status row's left label, matching the
reference's own structure: the decompiled `StatusLineModel.RenderBuiltIn`
stores the styled badge as the start of the left accumulator and then
appends the hint, so the row reads `-- INSERT -- ? for shortcuts` (the
hint is blanked while typing, leaving the badge alone). It is applied
after the typing-blank rule and before the overlay-blank rule, so `/help`
and the modal panels still own the row. Normal mode renders **no** badge.

### Engine corrections (the important part)

Confirmed against the decompiled reference and the installed 1.4.0 binary:

| Rule | Cycle 215 | Now | Evidence |
|---|---|---|---|
| **Normal-mode Enter** | swallowed as a no-op | **submits** | `vim.normal.submit` binds only ctrl+s/ctrl+enter, so a bare Enter falls through to the global map; `PromptModel.Update` submits on `KeyEnter` whenever the mode is not Insert. Live: `> hello world` → response. |
| **`o` / `O`** | deliberately absent | **implemented** | The reference's own `/help` Vim tab lists `i/I a/A o/O`, and `o` shows the `-- INSERT --` badge after it. `Parse` handles `o`/`O` at `0x1026cddf0`. |
| **Space** | right-motion | **unbound** | The reference does not bind space as a motion; probe: space then `x` still deletes the first character. |
| **`%`** | absent | absent (confirmed) | Not in the reference parser; probe: `%` then `x` deletes the char under the cursor. |
| **`ZZ`** | absent | **submit** | The reference parser emits a `"submit"` action for `ZZ` and its `/help` labels it "Submit the current query". |

`o`/`O` fold their line insertion into the following insert run, so `o`,
the typed text and `Esc` undo as one step.

Two smaller correctness fixes found while re-reading the engine:

- **Undo cap.** `pushUndo` now keeps a 50-entry ring (`undoLimit`),
  dropping the oldest step, matching the reference's 50-entry history.
- **Visual case undo was broken.** `applyCase` snapped the buffer *after*
  the in-place transform, so `u` after a Visual `~`/`u`/`U` restored the
  already-changed text and did nothing. It now snapshots before the
  transform via the new `recordUndo` helper (covered by
  `TestVisualCaseUndo`).

## Spec confirmation

A decompiled-code pass over the reference's Vim engine
(`out/local-macos/dec/*Vim*`, the reconstructed `editing` API and the
live `keybindings.json`) was run to check every rule. Outcome:

**CONFIRMED** — the submit/newline table (`vim.insert.submit` =
ctrl+enter/ctrl+s; `vim.insert.insert_newline` = enter/shift+enter/alt+enter/ctrl+j;
`vim.normal.submit` = ctrl+enter/ctrl+s); the badge literals
`-- INSERT --` / `-- VISUAL --` / `-- V-LINE --` with **no** Normal badge;
the `/help` tab is `vim`, lowercase, **4th** (index 3), header
`Vim Modal Shortcuts`; `cw` ≈ `ce` on a non-blank; `dw` clamps to
end-of-line inclusive; `cc` keeps the newline and leading indent and enters
Insert; the undo coalescing + 50-entry cap; `i a I A o O`; `f F t T ; ,`.

**REFUTED / corrected** — Normal Enter (submits, not a no-op); space
(unbound, not a right-motion); `%` (absent); `o`/`O` (present).

**`ip` / `ap` — a changelog/binary conflict, kept as-is.** The 1.1.11
changelog (`print_changelog.md:593`) explicitly promises `ip`/`ap`, but the
shipped binary no-ops `dip` (probe on a two-paragraph buffer) and the
reference's own `/help` Vim tab omits them. The clone **keeps** them
because the changelog is the stated spec and they are harmless; the
deviation is recorded rather than silently matched.

## Files

| File | Δ |
|---|---|
| `cmd/wagyu/vim_mode.go` | new — the whole TUI↔engine bridge (gate, key map, badge, `/help` tab) |
| `cmd/wagyu/vim_mode_test.go` | new — 13 TUI tests |
| `cmd/wagyu/tui_run.go` | vim key branch, `submitPrompt()`, badge in `statusRow()`, `renderTabBarTabs`/`renderHelp`, entry-point `reloadVimSettings()` |
| `cmd/wagyu/history.go` | `helpListLen(3)` → `len(helpVim)` |
| `cmd/wagyu/slash_panels.go` | `reloadVimSettings()` in `closePanel()` |
| `internal/vim/engine.go` | Normal Enter submits, `ZZ`, `o`/`O`, `openLine` |
| `internal/vim/parse.go` | `o O`, `ZZ` (`cmdSubmit`), drop space→right |
| `internal/vim/operator.go` | 50-entry undo ring, `recordUndo`, Visual-case undo fix |
| `internal/vim/vim.go` | package doc updated for the corrections |
| `internal/vim/{vim,spec,host}_test.go` | Normal-Enter, `o`/`O`, `ZZ`, space, Visual-case-undo tests |

53 engine test functions + 13 TUI test functions. Stdlib + `uniseg` only in
the engine; the TUI glue uses the existing bubbletea stack.

## Verified

```
$ gofmt -l internal/vim/ cmd/wagyu/vim_mode.go cmd/wagyu/vim_mode_test.go   # (clean)
$ go vet ./internal/vim/ ./cmd/wagyu/                                        # (clean)
$ go build -o /tmp/wagyu ./cmd/wagyu                                         # ok
$ ANTIGRAVITY_HOME=/tmp/vimhome go test -race ./internal/vim/ ./cmd/wagyu/   # ok
$ go test -cover ./internal/vim/            # coverage: 79.8% of statements
$ ANTIGRAVITY_HOME=/tmp/vimhome go test ./cmd/... ./internal/...             # 27 packages ok
```

Live tmux check (`/tmp/wagyu-vim`, `ANTIGRAVITY_HOME=/tmp/vimdemo`,
`editorMode:"vim"`, 200×40):

- **Normal Enter submits** — typed `ihello world`, `Esc`, `Enter`; the
  transcript shows `> hello world` and a model response starts.
- **`o` opens a line below and enters Insert** — after `iabc`, `Esc`, `o`
  the prompt shows `abc` plus a fresh blank line and the left label reads
  `-- INSERT --`.
- Badge transitions (`-- INSERT --`, blank in Normal, `-- VISUAL --`,
  `-- V-LINE --`) and ctrl+s submit were verified in cycle 215's live rig
  and still hold.

## 硬约束 / 存疑

- **Badge text position is inferred.** The reference TUI cannot be probed
  (OAuth expired) and print mode is headless, so the exact column and
  colour of the badge were not byte-diffed. Placement (left side, the left
  accumulator of the status line, before the hint) comes from the
  decompiled `RenderBuiltIn`; the clone renders it in dim gray
  (`cDimGrayOpen`/`cDimGrayClose`) with the badge prepended to the label.
- **The `/help` Vim tab body is not byte-matched.** The header
  (`Vim Modal Shortcuts`) and subtitle (`Standard Vim key bindings active
  in Vim Mode`) are the reference's own strings, but the exact row text of
  the reference's tab was not extracted (only its section names and the
  presence of `o/O`, `u/U`, `zz` were read), so the clone's rows are
  accurate but not a byte-for-byte copy.
- **`ip`/`ap` kept against the binary.** Deliberate; the changelog and the
  binary disagree (above). `dip` therefore deletes in the clone where the
  reference no-ops.
- **Undo coalescing is structural, not timed.** The reference coalesces
  within a 300 ms window (`maybeSnapshot`); the clone coalesces an open
  insert run. Same effect for normal typing, but two insert bursts less
  than 300 ms apart merge in the reference and do not here.
- **Normal-mode `<C-j>`** is implemented as a down-motion (Vim-faithful);
  the reference's Normal map does not list it and its fall-through was not
  traced to a specific action.
- **Version split.** Control flow was read from the 1.3.4 decompile; the
  live probes are the installed 1.4.0. They agree on every behaviour
  tested, but that is an empirical bridge, not a proof for 1.3.4.
- Two `cmd/wagyu` tests (`TestCycle197_ConfigVisibleRows`,
  `TestPanelConfigLayout`) fail without an isolated home because this
  machine's real `settings.json` already has `editorMode: "vim"`; they are
  pre-existing and environmental, and pass under
  `ANTIGRAVITY_HOME=/tmp/vimhome`.
