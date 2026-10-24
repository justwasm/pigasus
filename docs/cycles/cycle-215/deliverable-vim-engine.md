# Cycle 215 — the Vim editing engine (`internal/vim`)

## TL;DR

The reference added a Vim editing mode in 1.1.11 and kept extending it
(count multipliers in 1.2.9, `?` as a search argument in 1.2.8, Enter
behaviour in 1.1.17). The clone had none of it. This cycle lands the part
that can be built and proven without a terminal: a pure, self-contained
modal editing engine with **no bubbletea/lipgloss dependency**, so the
whole surface is unit-testable.

The engine is the source of truth for mode, buffer and cursor; the host
will hand it one normalised key at a time and read the text/mode/cursor
back out. TUI wiring (key routing, status badge, `/help` tab, `/config`
rows) is a separate cycle so the state machine can be reviewed on its own.

## Feature set

Fixed by the reference's own changelog, not by guesswork:

| Source | Feature |
|---|---|
| 1.1.11 | Normal / Insert / Visual / Visual-Line; badge strings `-- INSERT --` / `-- VISUAL --` / `-- V-LINE --` (Normal has **no** badge) |
| 1.1.11 | Motions `h j k l`, `w b e`, `W B E`, `0 ^ $ gg G` |
| 1.1.11 | `f F t T` with `;` / `,` repeats |
| 1.1.11 | Insert entries `i a I A` |
| 1.1.11 | Operators `d c y`, plus `x D C`, filling the unnamed register for `p` |
| 1.1.11 | Text objects `iw aw iW aW ip ap` |
| 1.2.9 | Counts everywhere: `3dw`, `2d3w`, `3dd`, `3x`, `3rX`, `3p`, `3u`, `[count]G`/`gg`/`$`, `2di(` |
| 1.2.8 | `f?` / `t?` / `df?` (the `?` argument is a normal rune to the parser) |
| 1.1.17 | Enter while the prompt is in Vim Insert mode |

`o` / `O` are **deliberately absent**: the changelog never lists them, so
adding them would be invention rather than parity. `?`-opening-shortcuts
and Enter-opening-a-background-task are host-routing decisions and belong
to the TUI cycle.

## Design

- **`State` is authoritative.** `Lines []string` (never contains `\n`) plus
  cursor `(Row,Col)` in **rune** indices. Motions and single-character
  edits step whole **grapheme clusters** via `github.com/rivo/uniseg`, so
  `👩‍💻` or `e` + combining acute are never split. `uniseg` was already in
  the module graph; it is promoted from indirect to direct.
- **Two-phase parsing.** `parse(seq, visual)` accumulates keys and returns
  `incomplete` / `invalid` / `complete`, which is how a lone `d`, `g`, `r`
  or `f` can wait for its argument without the host tracking state.
- **Motions resolve twice.** `applyMotion` moves the cursor; `motionTarget`
  reports where an operator would land plus how to consume it
  (inclusive/exclusive, charwise/linewise, direction). This is what makes
  `dw` vs `de` vs `d$` vs `dd` correct, including Vim's documented `cw` ≈
  `ce` exception.
- **Undo is whole-buffer snapshots** with an open coalescing group, so a
  run of typed characters (including inserted newlines) undoes as one
  step. `dropNoopUndo` discards a snapshot equal to the current buffer, so
  a no-op `D`/`C`/Visual case change records no empty step, as the
  changelog pins.
- **Submit semantics** come from the recovered default `keybindings.json`:
  `vim.normal.submit` / `vim.insert.submit` are `ctrl+enter` and `ctrl+s`;
  `vim.insert.insert_newline` is `enter`, `shift+enter`, `alt+enter`,
  `ctrl+j`. Under `Insert First` a bare `Enter` submits and the modified
  variants insert a newline instead.
- A linewise change (`cc`, `V c`) empties the covered line and keeps its
  leading indentation rather than removing it, matching Vim.

## Files

| File | Δ |
|---|---|
| `internal/vim/vim.go` | new — modes, badges, `Key`/`Result`, grapheme helpers, clamping |
| `internal/vim/motion.go` | new — motions, `motionTarget`, `operatorRange`, `f/F/t/T` |
| `internal/vim/object.go` | new — text objects, bracket/quote pairs with nesting |
| `internal/vim/operator.go` | new — register, yank/delete/change, paste, `r`, case |
| `internal/vim/parse.go` | new — Normal/Visual key-sequence parser |
| `internal/vim/engine.go` | new — `Handle`, Insert/Normal dispatch, command execution |
| `internal/vim/vim_test.go` | new — 21 tests |
| `internal/vim/spec_test.go` | new — 14 tests (the changelog list) |
| `internal/vim/host_test.go` | new — 11 tests (host surface + ranges) |
| `go.mod` | `github.com/rivo/uniseg` indirect → direct |

2,456 lines of engine, 846 lines of tests. Stdlib + `uniseg` only.

## Verified

```
$ ANTIGRAVITY_HOME=/tmp/vimhome go test -race -cover ./internal/vim/
ok  github.com/chadgpt/openagy/internal/vim  1.5s  coverage: 79.1% of statements
```

- 46 test functions / 52 cases, all passing, `-race` clean.
- `gofmt -l internal/vim/` clean, `go vet ./internal/vim/` clean.
- `go build -o /tmp/wagyu ./cmd/wagyu` succeeds.
- `ANTIGRAVITY_HOME=/tmp/vimhome go test ./cmd/... ./internal/...` —
  27 packages ok (the engine is not imported by the clone yet, so
  nothing else moves).

Two real bugs were found by the tests and fixed during the cycle: the
`objectKind` zero value had to be `objNone` (otherwise every operator
took the text-object branch), and `prevClusterCol` returned the
second-to-last cluster boundary.

## Limitations / not verified

- **No reference cross-check.** The reference TUI cannot be probed (OAuth
  expired) and print mode is headless, so the semantics are pinned to
  Vim's documented behaviour plus the changelog, not to byte-diffed
  reference output. The decompiled `VimEngine.Parse` surface
  (`out/local-macos/dec/1026cddf0_Parse.c`) agrees on the parser shape (a
  ~15-field command struct) but was not read field-by-field.
- `ip`/`ap` use the blank/non-blank run rule; nested-paragraph counts are
  not implemented (only pairs honour counts).
- `2$` is treated as charwise to the end of the target line; whether the
  reference forces linewise for a multi-line `d2$` is unconfirmed.
- The engine never touches the terminal; badge rendering, key routing and
  the `/help` Vim tab are the next cycle.
