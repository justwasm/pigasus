# cycle: diff-cjk-cutoff

**Branch:** `feature/diff-cjk-cutoff`
**Base:** `54db67b` (main)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-diff-cjk-cutoff`
**AGY changelog line closed:** 1.3.6 line 11 — *“Fixed /diff long-line cutoff behaviour with CJK characters — the ellipsis now respects grapheme cluster boundaries so CJK characters at the cutoff edge are not split mid-character.”*
**Files changed (5):**

| File | Status | Note |
|---|---|---|
| `cmd/wagyu/diff_truncate.go` | new | grapheme-cluster-aware line cutter |
| `cmd/wagyu/diff_truncate_test.go` | new | 8 tests, 75+ sub-cases |
| `cmd/wagyu/slash_actions.go` | modified | `diffFeedback(workspace, maxCells int)`; per-line `truncateDiffLine` |
| `cmd/wagyu/slash_commands.go` | modified | TUI passes `m.width` to `diffFeedback` |
| `cmd/wagyu/slash_actions_test.go` | modified | 4 test sites updated to `diffFeedback(..., 0)` |

---

## Behaviour

`truncateDiffLine(s, maxCells)` shortens `s` to at most `maxCells` terminal
cells (ASCII = 1, CJK and wide emoji = 2). When the input already fits the
budget, or `maxCells <= 0` (the tests’ pass-through mode), the string is
returned verbatim. Otherwise the function walks the input with
`uniseg.NewGraphemes`, accumulates each cluster’s display width via
`Graphemes.Width()`, and stops **only** at a cluster boundary whose width
would overflow the reserved `maxCells − 3` budget. The ASCII ellipsis
`"..."` is then appended (the three terminal cells of the literal
three-dot ASCII form).

Cycle diff-cjk-cutoff closes the 1.3.6 line-11 gap by replacing the previous
byte-level slice (or any rune-level slice that broke on the middle of a ZWJ
family emoji) with this cluster walk; the only outputs are
unchanged-input, full-cluster + `"..."`, or `"..."` alone (`maxCells ∈ {1,2,3}`).

## Verification

* `GOFLAGS=-mod=mod go build -o /tmp/wagyu-cjk ./cmd/wagyu` — exit 0,
  70 MB Mach-O arm64 binary.
* `GOFLAGS=-mod=mod go vet ./cmd/wagyu/` — clean.
* `GOFLAGS=-mod=mod go test -vet=off -count=1 -run 'TestTruncateDiffLine|TestDiffFeedback' ./cmd/wagyu/`
  — `ok` (8 tests + 75 sub-cases, ~1.3 s).
* `gofmt -l cmd/wagyu/diff_truncate.go cmd/wagyu/diff_truncate_test.go cmd/wagyu/slash_actions.go cmd/wagyu/slash_commands.go cmd/wagyu/slash_actions_test.go`
  — empty (new + touched files clean; pre-existing-not-clean files in
  `cmd/wagyu/` left untouched per the convention in AGENTS.md).

## Tests added (8)

| Test | Pins |
|---|---|
| `TestTruncateDiffLine_ASCII_Fits` | short-ASCII pass-through |
| `TestTruncateDiffLine_ASCII_Truncates` | long ASCII → `...` at byte boundary, no U+FFFD |
| `TestTruncateDiffLine_CJK_NoMidCharSplit` | 1.3.6 line 11 headline case: CJK never mid-rune split, total width ≤ budget |
| `TestTruncateDiffLine_EmojiZWJ_NoSplit` | ZWJ family-of-three across 8 widths — visible prefix is a byte-aligned prefix of input |
| `TestTruncateDiffLine_MixedASCIIAndCJK` | mixed script fits the cell budget with no overrun |
| `TestTruncateDiffLine_ShortIsUnchanged` | non-ASCII short line passes through with no spurious `"..."` |
| `TestTruncateDiffLine_MultipleCutoffs` | 5 lines × 15 widths (75 sub-cases) regression net — no U+FFFD, valid UTF-8, ellipsis iff truncated |
| `TestTruncateDiffLine_ZeroWidthDisables` | `maxCells = 0` returns verbatim (back-compat for tests) |

## Hard constraints honoured

* `internal/convstore/sqlite.go` — untouched.
* Pre-existing not-gofmt-clean files — left alone (gofmt complaint count
  in `cmd/wagyu/` is unchanged at 8; none of those 8 were touched).
* No branch push.
* Single commit on `feature/diff-cjk-cutoff`.
* All work in the requested worktree path; main worktree’s `git status`
  is clean.
* Single new runtime dep: `github.com/rivo/uniseg` — already pulled in
  (via `internal/vim/vim.go`) so `go.mod`/`go.sum` need no edit.

## 硬约束 / 存疑

* **/diff in print mode is rejected.** `cmd/wagyu/print_slash.go:224`
  sends `"it opens the interactive diff viewer"` for `agy -p /diff`, so
  this cycle only touches the TUI code path. The reference viewer’s
  exact maxCells (the 1.3.6 line-11 picture) is unrecovered; the
  clone passes `m.width` which is the terminal’s full width and
  captures the fix’s intent. 【推测】
* **No byte-exact snapshot verification.** Pre-cycle, `diffFeedback`
  had no truncation at all, so there is no prior truncation output
  to diff against. The behaviour is verified by the new
  `TestTruncateDiffLine_*` suite; the byte-exact `/diff` text
  (`TestDiffFeedback_Git`, `TestDiffFeedback_Edges`) is preserved
  because both call `diffFeedback(dir, 0)` which disables truncation.
* **Reference `m.width` in the absence of a TTY.** When `m.width == 0`
  the per-line cap degenerates to 0 (pass-through). The reference’s
  pre-TUI line cap is undocumented; this matches the behaviour of
  `buildCommandRow` for `/help` (`width <= 0 disables truncation
  entirely`), which is the existing convention.
* **emoji presentation selector flags (U+FE0F/U+FE0E).** A trailing
  Variation Selector-16 after a bare BMP code-point emoji is one
  grapheme cluster under UAX#29; the walk treats it as such. The
  cycle’s tests cover extended-pictographic and regional-indicator
  clusters; VS-15/VS-16 sequences follow the same rule by
  construction.
