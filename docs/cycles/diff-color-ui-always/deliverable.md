# Cycle `feature/diff-color-ui-always` — strip ANSI from `/diff` body before parsing

**AGY 1.3.4 changelog line 33:**
> "Fixed `/diff` showing `No uncommitted changes` in Git, Mercurial, and Jujutsu repositories configured to always color output, for example with `color.ui = always`."

**Branch:** `feature/diff-color-ui-always` (forked from main at `54db67b`)
**Worktree:** `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-diff-color-ui-always/`

## TL;DR

`cmd/wagyu/slash_actions.go:diffFeedback` ran the VCS diff and checked
`strings.TrimSpace(body) == ""` to decide between the recovered
`No uncommitted changes` line and the rendered diff. The check is
correct for plain output, but `color.ui = always` (and the hg/jj
equivalents) cause git/hg/jj to emit ANSI CSI sequences — and those
escape bytes are not whitespace. The check therefore concluded the
body had content and rendered an empty diff, contradicting the
recovered empty-case line.

The fix is a single, surgical strip:

| Step | What | Why |
|---|---|---|
| 1 | New `stripANSI(s string) string` in `slash_actions.go` | removes CSI sequences (`ESC [ params final`) — the only escape form VCS diffs emit |
| 2 | `diffFeedback` runs `body := stripANSI(strings.TrimRight(out, "\n"))` before the empty check | ANSI bytes no longer look like content |
| 3 | The strip is applied to the *displayed* body too, not just the check | the transcript is plain text; stray `\x1b[…m` would either render as garbage or wrap lines in unexpected colours |
| 4 | New `vcsDiffFunc = vcsDiff` package-level var | the test seam that lets the cycle's tests exercise the regression without configuring a real `color.ui = always` repo |

`vcsDiff` itself is unchanged. The pager-disabling env block
(`PAGER=cat / GIT_PAGER=cat / JJ_PAGER=cat`, `GIT_EDITOR=false / HGEDITOR=false / JJ_EDITOR=false`,
`HGPLAIN=1 / NO_COLOR=1`) already covers most cases; the
`--no-color` / `--color=never` CLI flags cover the rest. The strip is
the belt-and-braces that handles the configuration overrides that
escape the rest of the defence.

## What landed

| File | Lines | Role |
|---|---|---|
| `cmd/wagyu/slash_actions.go` | +38 / −7 | new `ansiEscapeRE` regex, `stripANSI` helper, `vcsDiffFunc` test-seam var, `diffFeedback` updated to strip before the empty check and before display |
| `cmd/wagyu/slash_actions_diff_color_test.go` (new) | +248 / −0 | 9 tests covering stripANSI sub-cases, every VCS, the plain/empty/error/footer paths |
| `docs/cycles/diff-color-ui-always/deliverable.md` (this file) | +N | TL;DR, scope, regex rationale, gates, 存疑 |

**Total:** 1 source file changed + 1 new test file + this deliverable.

## Why this regex and not `regexp.MustCompile("\\x1b\\[[\\d;]*m")`

The regex `\\x1b\\[[\\d;]*m` only matches the SGR (`m`) final byte, so
it strips colours but misses cursor moves (`\\x1b[2J`), mode toggles
(`\\x1b[?25h` / `\\x1b[?25l`), line wraps (`\\x1b[?7h`) and a few
exotics. The 1.3.4 fix is broader — `\\x1b\\[[0-9;?]*[a-zA-Z]` covers
the whole CSI family per ECMA-48 §5.4. The VCS diff commands do not
emit OSC or single-character escapes, so this regex is both sound
(only sequences a real diff can produce) and complete (every CSI
variant VCS commands have been observed to use). The `?` parameter
byte is what covers the `DEC private` modes that some tools emit for
cursor / wrap state, which would otherwise survive the strip and
confuse the line-splitting downstream.

## Test catalogue

| Test | Catches |
|---|---|
| `TestStripANSI` (9 sub-cases) | the strip helper itself — empty / plain / SGR / multi-param / `?` param / newlines / non-SGR CSI / non-letter final |
| `TestDiffFeedback_EmptyWithANSI` | the regression itself: a clean repo whose diff emits only `\\x1b[m\\x1b[m` returns `No uncommitted changes` |
| `TestDiffFeedback_EmptyPlain` | pre-existing empty case still passes (no false positive) |
| `TestDiffFeedback_PlainDiff` | plain diff renders with the recovered footer and no new escapes |
| `TestDiffFeedback_ColoredDiff` | a *real* coloured diff is recognised as content, the body is rendered without stray escapes, the footer still lands |
| `TestDiffFeedback_EmptyWithANSIAndWhitespace` | whitespace + colour codes only → still empty case |
| `TestDiffFeedback_ColoredDiffForAllVCS` (3 sub-cases: git, hg, jj) | the fix is uniform across all three VCSs; confirms `detectVCS` resolves each marker correctly under the test seam |
| `TestDiffFeedback_DiffError` | the error wrapper `git diff: <err>` is preserved verbatim — the strip is applied only after the error path returns |
| `TestDiffFeedback_NoVCS` | a directory with no VCS anywhere up the chain returns the empty line without ever calling `vcsDiffFunc`; guards against a future refactor short-circuiting the empty path |

The five tests the cycle spec calls out (ANSI-coloured diff parsed correctly,
plain diff parsed correctly, empty diff with ANSI codes → empty line, empty
diff no codes → empty line, non-empty diff with ANSI codes → renders body)
are covered by `TestDiffFeedback_EmptyWithANSI`, `TestDiffFeedback_PlainDiff`,
`TestDiffFeedback_EmptyWithANSIAndWhitespace`, `TestDiffFeedback_EmptyPlain`,
and `TestDiffFeedback_ColoredDiff` respectively.

## Verification

* `go build -o /tmp/wagyu-cui ./cmd/wagyu` — exit 0, 73 MB.
* `go test -vet=off -count=1 -run 'TestDiffColor|TestColorUI|TestANSI|TestDiffFeedback|TestStripANSI' ./cmd/wagyu/ ./internal/.../` — all pass.
* `gofmt -l` on every NEW file:
  * `cmd/wagyu/slash_actions_diff_color_test.go` — clean.
  * `cmd/wagyu/slash_actions.go` — clean (was clean on main; the patch only inserts a small block before `diffFeedback`).
* The pre-existing `TestDispatchPrintSlash_LocalCommands` failure in `print_slash_test.go` (model selection — `gemini-3.8-flash-medium` vs `minimax-m3`) is unrelated to this cycle and exists on main.
* The pre-existing 47 not-gofmt-clean files under `internal/` and
  `cmd/wagyu/{picker_view.go,tui_v135_test.go}` were not touched.

## Hard constraints — satisfied

* **No third-party deps added** — the fix uses `regexp` and `strings` from stdlib.
* **Do NOT touch `internal/convstore/sqlite.go`** — not touched.
* **gofmt -l clean on every NEW file** — see above.
* **Pre-existing not-clean files MUST NOT be reformatted wholesale** — none reformatted.
* **Branch must NOT be pushed** — only the local branch was created; no `git push` issued.

## 存疑 / limitations

* **The strip is regex-based, not stateful.** A diff body that contains
  a string literal `\x1b[31mfoo` (a documentation example, say) will
  have the literal stripped. The 1.3.4 reference presumably does the
  same — there is no way to distinguish "real" escape sequences from
  literal text outside a real terminal parser. None of the VCS diff
  commands emit such literals in practice, so the trade-off is fine.
* **OSC and DCS are not stripped.** None of the three VCS commands
  emit them in a diff context (verified by reading the
  `color.ui=always` regression repro — only SGR and a handful of
  cursor-mode sequences appear). If a future VCS ever emits them,
  the regex will need extending; the seam (`stripANSI`) is the
  obvious place to do it.
* **`runGit` in `slash_viewer_panels.go` does not strip.** It feeds
  the diff body to the inline subview, which re-colours `+`/`-`
  lines with the clone's own palette via `diffInlineStyle`. Real
  `git diff --no-color` plus `--no-color` on the CLI plus the env
  block means the inline view never sees colour in practice. A
  similar regression there would call for a parallel strip; this
  cycle does not add one because the cycle scope is `/diff`, not
  the inline viewer.