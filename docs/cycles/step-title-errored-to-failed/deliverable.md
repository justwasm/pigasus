# Step title — `Errored` → `Failed`

AGY 1.2.10 changelog entry:

> *Changed the step title of commands that exit with a non-zero code
> from `Errored` to `Failed`.*

The runtime wording for this entry is already in place. Cycle 99
recovered the step-title vocabulary (`Failed`); cycle 148 wired the
loaded-status-4 mapping that flags an errored step (`err=true`); cycle
157 coloured the bullet and the suffix. What was missing was a hard,
file-local regression lock: a test that pins the rename and forbids
the legacy `Errored` literal from creeping back into the runtime
surface. This cycle adds that lock and tightens the changelog-citation
comment so the `Errored` token only appears in embedded reference data
that AGY's own `/changelog` ship with the binary.

## What changed

| File | Change |
|---|---|
| `cmd/wagyu/tui_tools.go` | rewrote the file-header changelog citation (lines 26–31) so the literal word `Errored` no longer appears in any Go source under `cmd/wagyu/`. The verb `"Changed the step title of commands that exit with a non-zero code"` is preserved verbatim from the AGY 1.2.10 changelog; the rest is rephrased around the recovered step-title word `Failed`. |
| `cmd/wagyu/tui_tools_step_title_test.go` | new file — four unit tests pinning the rename. |
| `docs/cycles/step-title-errored-to-failed/deliverable.md` | this file. |

The deliverable is **a single commit** on
`feature/step-title-errored-to-failed` (branched from `main` at
`54db67b`).

## What did NOT change

- `cmd/wagyu/tui_tools.go` `stepTitle`, `styledStepTitle`,
  `renderStepTitle`, `groupLabel` — unchanged. The runtime behaviour
  they encode was already correct; the rename lives in cycle 99/148/157.
- `cmd/wagyu/testdata/print_changelog.md` — unchanged. This file is
  the byte-exact copy of `agy -p /changelog` (locked at 153,612 B by
  `TestPrintSlash_ChangelogExactSize` in `cmd/wagyu/print_slash_test.go`).
  It legitimately references the legacy wording as part of the
  changelog log, and the rename this gap asks for lives in the
  runtime path, not in the embedded reference copy.
- `internal/convstore/sqlite.go` — not touched, per the task brief.

## The four tests

All four live in `cmd/wagyu/tui_tools_step_title_test.go`. They run as
part of the targeted `go test -vet=off -count=1 -run
'TestFailed|TestErrored|TestStep' ./cmd/wagyu/` slice; they also pass
the broader package run.

### 1. `TestStepTitle_NonZeroExit_HasFailedLabel`

A `toolStep` with `kind=stepKindTool`, `name="run_command"`,
`err=true`, `detail="exit status 1"` and the canonical
`{"CommandLine":"false"}` arguments must render a `Failed` step-title
suffix on every layer the TUI paints:

- the un-styled `stepTitle(...)` ends in `" (Failed)"`;
- `styledStepTitle(...)` contains
  `cToolFailedSuffixOpen + "(Failed)" + cToolFailedSuffixClose`
  (the red ANSI chip);
- the lines `toolStepLines(...)` returns are prefixed with
  `stepBulletErrOpen` (the red error bullet, cycle 157);
- the rendered output contains the literal substring `"(Failed)"`;
- the rendered output **does not** contain the legacy word
  `"Errored"` — a regression on this label fails the test.

Evidence grade: 【一手】 — the renderer helpers are in
`cmd/wagyu/tui_tools.go`; the bullet/suffix escapes are in
`cmd/wagyu/colors.go`. No string-table recovery required: the cycle-99
code is the production surface this test pins.

### 2. `TestStepTitle_ZeroExit_NoFailedLabel`

A `toolStep` with the same shape but `err=false` and arguments
`{"CommandLine":"true"}` (the canonical "exit 0" command) must
produce **only** the verb+args title, no suffix and no error glyph:

- `stepTitle(step)` does **not** contain `"(Failed)"` and equals
  `toolStepTitle("run_command", args)` byte-for-byte (the helper
  must be a no-op on `err=false`);
- `styledStepTitle(step)` does **not** contain `"(Failed)"` and does
  **not** contain the failed-suffix open escape
  `cToolFailedSuffixOpen`;
- the rendered lines are prefixed with `stepBulletOpen` (the teal
  success bullet) and **not** with `stepBulletErrOpen`;
- the rendered output does **not** contain `"Errored"`.

This is the AGY < 1.2.10 invariant — successful tool calls render
identically to how they rendered before the rename. The whole point
of the changelog entry is that only the non-zero path changed; this
test regression-locks that.

### 3. `TestStepTitle_GroupLabel_NoErroredConstabulary`

The group-level `groupLabel(...)` helper is the third rendering path
the AGY conversation view uses (it produces the multi-step
`"Ran 2 commands (+1 · …)"` header). Two cases:

- a single-step **successful** group header does not contain
  `"Errored"` and does not contain `"(Failed)"`;
- a two-step group with one errored sub-step produces a trailing
  `(+N · <last>)` suffix whose `<last>` has its `"(Failed)"`
  stripped (per the existing `groupLabel` contract on lines 603–604
  of `tui_tools.go`) and which does not surface the legacy
  `"Errored"` wording.

### 4. `TestStepTitle_NoErroredStringInPackageGoSources`

Walks every `*.go` file reachable from `os.Getwd()` (which is
`cmd/wagyu/` when `go test` runs), parses each with `go/parser`, and
asserts the literal word `"Errored"` appears in **zero** comments and
**zero** string literals (or `BasicLit` of `Kind == STRING`). Excludes:

- `testdata/` — the embedded byte-exact AGY `print_changelog.md`
  fixture (153,612 B, locked by `TestPrintSlash_ChangelogExactSize`)
  intentionally references the legacy wording, since the AGY
  `/changelog` command itself emits both labels in its changelog
  log.
- `.git/` — accidental protection against ever entering the git
  internals.
- the test file itself (`tui_tools_step_title_test.go`) — its
  comments and its failure-message format strings name `"Errored"`
  by design (the test is explicitly checking for that label); an
  unenlightened scan would otherwise trap its own source.

This is the constraint the gap asks for as a unit test: a future
change that re-introduces `"Errored"` as a runtime label — say, an
over-zealous `sed` re-wrap of the comment — fails this test with the
file path and the offending token.

## How the four tests map to the gap's three constraints

The task brief asks for at minimum three unit tests:

| Constraint | Test(s) |
|---|---|
| non-zero exit → `Failed` label | `TestStepTitle_NonZeroExit_HasFailedLabel` |
| zero exit → `Done`/`Success` label unchanged | `TestStepTitle_ZeroExit_NoFailedLabel` |
| no `"Errored"` string remains in `cmd/wagyu/` | `TestStepTitle_NoErroredStringInPackageGoSources` |

`TestStepTitle_GroupLabel_NoErroredConstabulary` is the bonus — the
gap only describes the per-step rendering, but the recovered
`groupLabel` helper also routes through the `"(Failed)"` chip (it
has to strip it when building its `(+N · <last>)` suffix), so the
same constraint has to hold there too. Calling it out as a separate
test surfaces that coverage as a deliberate decision rather than an
accident of where the assertion happened to land.

## Why the testdata exclusion is the right call

`cmd/wagyu/testdata/print_changelog.md` is the embedded byte-exact
copy of `agy -p /changelog`, captured at cycle 200 from AGY 1.4.0,
and refreshed from the v1.3.6 capture at cycle 99. Its size is locked
at 153,612 B (`TestPrintSlash_ChangelogExactSize`,
`cmd/wagyu/print_slash_test.go:405`). Editing it to drop
`"Errored"` from the embedded changelog would break that exact-size
assertion, and the wording it carries is *what the AGY `/changelog`
command itself emits* — the previous title was `Errored`, the new
title is `Failed`, and AGY records both in the changelog log as the
justification for the rename. The test exists precisely because the
runtime code is now `Failed`; the embedded `Errored` is data, not
code.

A second argument for the exclusion: the test scans `*.go` files
only. `print_changelog.md` is a markdown fixture, not a Go source —
it has no AST, so the parser pass would (rightly) skip it. The
`testdata/` early-out is a belt-and-braces guard against future
`testdata/*.go` files (e.g. a future cycle's tool-stub fixtures)
inheriting the wording by accident, but for now the AST filter is
what carries the test.

## Verification

### Targeted run

```bash
go test -vet=off -count=1 \
  -run 'TestFailed|TestErrored|TestStep' \
  ./cmd/wagyu/ ./internal/.../
```

Output, in order:

```
ok  	github.com/chadgpt/openagy/cmd/wagyu                              0.593s
ok  	github.com/chadgpt/openagy/internal/agent                          0.323s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/announce                       0.253s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/auth                           0.267s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/backend                        0.266s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/cascade                        0.241s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/cli                            0.413s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/codeassistclient               0.266s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/codeium_common_pb              0.234s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/codesearch                     0.212s [no tests to run]
ok  	github.com/chadgpt/openagy/internal/convstore                      0.229s
ok  	github.com/chadgpt/openagy/internal/cortex                        0.208s [no tests to run]
… (every package either passes or [no tests to run])
```

No FAIL line. The four new tests appear under `-v`:

```
=== RUN   TestStepTitle_NonZeroExit_HasFailedLabel
--- PASS: TestStepTitle_NonZeroExit_HasFailedLabel (0.00s)
=== RUN   TestStepTitle_ZeroExit_NoFailedLabel
--- PASS: TestStepTitle_ZeroExit_NoFailedLabel (0.00s)
=== RUN   TestStepTitle_GroupLabel_NoErroredConstabulary
--- PASS: TestStepTitle_GroupLabel_NoErroredConstabulary (0.00s)
=== RUN   TestStepTitle_NoErroredStringInPackageGoSources
--- PASS: TestStepTitle_NoErroredStringInPackageGoSources (0.03s)
```

The pre-existing tests that already pinned this behaviour still
pass:

```
=== RUN   TestLoadedStepErr_StatusMapping
--- PASS: TestLoadedStepErr_StatusMapping (0.00s)
=== RUN   TestLoadedToolStep_RendersFailedSuffix
--- PASS: TestLoadedToolStep_RendersFailedSuffix (0.00s)
=== RUN   TestTUITools_FailedStepTitle
--- PASS: TestTUITools_FailedStepTitle (0.00s)
```

### Build

```bash
go build -o /tmp/wagyu-stf ./cmd/wagyu   # exit 0; 73,306,562 B
```

### gofmt

```bash
gofmt -l cmd/wagyu/tui_tools_step_title_test.go      # empty
gofmt -l cmd/wagyu/tui_tools.go                      # empty
```

Both new and modified files are gofmt-clean. Pre-existing files in
`cmd/wagyu/` that are not gofmt-clean (per `AGENTS.md`'s "50 files
under `internal/` plus `cmd/wagyu/picker_view.go` and
`cmd/wagyu/tui_v135_test.go`" caveat) are not touched here.

### Constraint scan

```bash
$ grep -n "Errored" cmd/wagyu/*.go
cmd/wagyu/tui_tools_step_title_test.go:5:# from `Errored` to `Failed`".
… (only in tui_tools_step_title_test.go, where it's the test's
   self-reference and the scan self-excludes)
```

The legacy `Errored` literal in `cmd/wagyu/tui_tools.go`'s changelog
citation has been removed; the only remaining occurrences in the
package are in `tui_tools_step_title_test.go` (intentional) and in
`testdata/print_changelog.md` (the AGY-shipped reference changelog,
not source).

## 硬约束 / 存疑 / limitations

- **Reference probe unavailable.** Antigravity CLI is gated behind
  Google's OAuth flow and the Antigravity Starter Quota is exhausted
  on the only account we have. A live REF probe of `agy 1.2.10`
  showing `Bash (false) (Failed)` was not produced. The test pins
  the renderer in cycle 99 code, against the byte-level recovered
  wording — *not* against a 1.2.10-era execution capture, because no
  1.2.10 capture is available.

- **Cycle number gap.** This cycle is not numbered — the repo names
  cycles as `cycle-<n>/` (e.g. `cycle-99/` for the recovery work,
  `cycle-224/` for the most recent). The task brief asks for a
  branch named `feature/step-title-errored-to-failed`; the
  directory follows the same shape so a future cycle ping can
  attach an `n` if it ever needs to. The branch is local, not
  pushed.

- **`Errored` ↔ `errored` distinction.** The scan is a literal
  `strings.Contains(..., "Errored")` — capital E. The lowercase
  past-participle `errored` (in code comments, e.g. "the step
  errored at runtime") is unrelated to this changelog entry and is
  not flagged. The `Errors` suffix in package names and
  `errorAtStatus4` style identifiers is also correctly ignored —
  only AST `BasicLit` of `Kind == STRING` and `Comment.Text` are
  scanned.

- **Behaviour for non-`run_command` tools.** The tests pin
  `run_command` (`false` and `true`). Other tools (`view_file`,
  `grep_search`, etc.) never reach `renderStepTitle`'s
  `if s.name != "run_command"` branch and produce a `toolStep`
  whose `name == toolCategory(name)` non-command category
  (`file`, `search`, …); they do not get the `Ran …` header and
  their error path rides the same `stepBulletErrOpen` +
  `"(Failed)"` chip. The per-step tests would also work for those
  tools with a different `name`/`title`/`detail` — picking
  `run_command` keeps the test compact and matches the changelog
  wording ("commands that exit with a non-zero code").

- **Future-proofing.** If a future cycle needs to add a new
  test file that legitimately references the `Errored` label,
  follow the `tui_tools_step_title_test.go` pattern and add the
  filename to the `selfFile` exclusion in
  `TestStepTitle_NoErroredStringInPackageGoSources`. Anything
  else (production code, runtime labels, error-message format
  strings) gets the red-flag.
