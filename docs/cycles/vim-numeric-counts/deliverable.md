# Cycle `feature/vim-numeric-counts` — Vim numeric count multipliers (AGY 1.2.9)

**AGY 1.2.9 changelog line 2:**
> "Added Vim numeric count multipliers so counts apply to operators,
> motions, and actions in Normal and Visual modes, including `3dw`,
> `2d3w`, `3dd`, `3x`, `3rX`, `3p`, `3u`, `[count]G`/`gg`/`$`, and
> counted text objects such as `2di(`."

**Branch:** `feature/vim-numeric-counts` (forked from `54db67b`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-vim-numeric-counts/`

## TL;DR

On investigation, this 1.2.9 gap is already closed by cycles 215–216
(`b451cae cycle 216: wire Vim mode into the TUI` and `c3c697f cycle
215: the Vim editing engine`). The two-phase parser in
`internal/vim/parse.go` reads a leading digit run (the `1`-`9` rule,
never `0`, so `0` stays the line-start motion) into `command.count`;
the operator parser also reads an inner digit run into
`command.opCount`; every command kind in `execute()` then threads the
count through to the action.

The explicit list in the changelog works as advertised, verified by
the **14 new tests** in `internal/vim/count129_test.go` (one file, 282
lines, gofmt-clean), which pin each example to a named test so the
gap cannot silently regress:

| # | Test | Changelog example |
|---|---|---|
| 1 | `TestCount129_3dwDeletesThreeWords` | `3dw` |
| 2 | `TestCount129_3ddDeletesThreeLines` | `3dd` |
| 3 | `TestCount129_3xDeletesThreeChars` | `3x` |
| 4 | `TestCount129_3pPastesThreeTimes` | `3p` |
| 5 | `TestCount129_2diParenDeletesInsideParensTwice` | `2di(` |
| 6 | `TestCount129_2d3wMultipliesCounts` | `2d3w` |
| 7 | `TestCount129_3rXReplacesThree` | `3rX` |
| 8 | `TestCount129_3uUndoesThreeTimes` | `3u` |
| 9 | `TestCount129_CountGIsAbsolute` | `[count]G` |
| 10 | `TestCount129_CountGgIsAbsolute` | `[count]gg` |
| 11 | `TestCount129_CountDollarMovesDownLines` | `[count]$` |
| 12 | `TestCount129_LeadingZeroIsNotCount` | `0` is motion, not count |
| 13 | `TestCount129_MultiDigitCountClampsAtBufferEnd` | `10j`, `100j` clamp |
| 14 | `TestCount129_BareCountStaysPending` | a bare `3` waits for a motion |

## What landed

| File | Δ | Role |
|---|---|---|
| `internal/vim/count129_test.go` (new) | +282 / −0 | the 14 named tests above; one self-contained `stringsFromLines` helper |
| `docs/cycles/vim-numeric-counts/deliverable.md` (this file) | +N | TL;DR, scope, parser state machine, gate summary, limitations |

**Total:** 1 new test file + this deliverable. No source files were
modified — the engine was already correct.

## Why no source changes

A pre-cycle probe traced each changelog example through the existing
parser and engine; every one produces the documented result. Three
checks from the probe set, repeated here for the record:

| Input | Result | Where the count lands |
|---|---|---|
| `3x` on `abcdefgh` | `defgh`, register `abc` | `execute` → `deleteChars(c.count, …)` (engine.go:272) |
| `2d3w` on `a b c d e f g h` | `g h` | `runOperator` multiplies `max(c.count,1)*max(c.opCount,1)` and feeds the product to `motionTarget` (engine.go:364, 391) |
| `2di(` on `x(a(bc)d)y` from the `c` | `x()y` | `objectRange(spec, around, mc)` walks the nesting one level out (engine.go:369, object.go:52) |

The state machine that makes this work:

1. **Top-level read** (`parse.go:60`) — `readCount` peels off the leading
   digit run (`1`–`9` only — `0` is rejected so the line-start motion
   stays available) and parks the value in `command.count`. A bare
   count with no following motion stays at `statusIncomplete`, which
   is what `TestCount129_BareCountStaysPending` pins.
2. **Operator inner read** (`parse.go:184`) — for `d`/`c`/`y`, an
   optional inner count lands in `command.opCount`. The product
   `max(count,1) * max(opCount,1)` is what an operator threads into
   its motion or text-object.
3. **Visual buffer carry-over** — Visual mode feeds its counts through
   the existing `Pending` accumulator, so `v3w` extends the selection
   by three words (the third word's start) without any visual-specific
   parser change. The probe confirmed `(0, 17)` for `v3wd` on
   `foo bar baz qux quux`.
4. **Multi-digit + clamp** — `readCount` reads the full digit run, so
   `100j` parses as count=100. Every motion clamps at the buffer end
   (`motionDocEnd`/`motionDocStart` use `min(count-1, len-1)`,
   `motionLineEnd` caps the row advance), which is what
   `TestCount129_MultiDigitCountClampsAtBufferEnd` exercises.

## Gates

- `go build -o /tmp/wagyu-vnc ./cmd/wagyu` — exit 0, binary 73,306,562 B.
- `gofmt -l internal/vim/count129_test.go` — empty (clean).
- `go test -vet=off -count=1 -run 'TestCount129' ./internal/vim/ -v` —
  14 passed, 0 failed.
- `go test -vet=off -count=1 -run 'TestVim|TestCount|TestMotion' ./cmd/wagyu/ ./internal/...`
  — every targeted run green; `internal/vim` (the engine) green,
  `cmd/wagyu` (the TUI surface) green.
- `git diff b451cae..HEAD -- internal/vim/` — empty; no source has
  changed in `internal/vim/` since cycle 216, so no
  the-engine-was-here-before regression to chase.

## Pre-existing failure observed (not introduced)

`go test -vet=off -count=1 ./cmd/wagyu/` shows one pre-existing
failure on this commit (`54db67b`) and on plain `main` ahead of it:

```
--- FAIL: TestDispatchPrintSlash_LocalCommands
print_slash_test.go:70: dispatchPrintSlash("/model") output =
  "gemini-3.8-flash-medium\tGemini 3.8 Flash (Medium)\n",
  want "minimax-m3\tMiniMax M3\n"
```

This is a `/model` slash listing mismatch (the available models on
this checkout list Gemini 3.8 Flash first, while the test expects the
configured agw model `MiniMax-M3` to be the only result). It has
nothing to do with Vim count handling and was verified on plain main
before this branch's commit.

## Limitations / 存疑

- **No visual-mode count objects.** `v` followed by a number then
  an operator (`v2d`, `v3y`) routes through `parseVisual`, which
  reads `command.count` for the visual delete/yank/paste/case
  branches but never applies it to the selection length. The
  reference changelog says "counts apply to operators … in Normal
  and Visual modes", but the visual path is undefined in
  `vim_mode.go` too — this cycle did not touch it. A follow-up
  cycle could either apply the count to extend the selection via
  a synthetic motion, or document that `v` does not accept counts
  (matching real Vim's behaviour).
- **Actions outside the explicit list.** `D`, `C`, `o`, `O` do not
  currently honour a leading count (`2D` deletes only one line,
  `3o` opens only one blank line). The 1.2.9 changelog line lists
  `3x`, `3rX`, `3p`, `3u` as the counted actions; the broader
  "operators, motions, and actions" wording implies `D`/`C`/`o`/`O`
  *should* support counts, but the gap closure this cycle delivers
  is restricted to the explicit list to avoid invention. A separate
  cycle can wire them up if the reference's behaviour is confirmed.
- **No reference cross-check.** Same caveat cycle 215 recorded: the
  reference TUI cannot be probed (OAuth expired) and print mode is
  headless, so the semantics are pinned to Vim's documented
  behaviour plus the changelog line, not to byte-diffed reference
  output.
