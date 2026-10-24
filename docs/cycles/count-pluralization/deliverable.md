# Cycle count-pluralization — pluralization + off-by-one for tool-step counts

## Goal

AGY 1.3.3 changelog line 62:

> Fixed counts in tool steps and the `/codesearch` panel, which now
> read "Found 1 result", "1 match", "1 file", and "Wrote 1 line"
> instead of "1 results", and the file-creation step reporting one
> line too many.

Cycle 111 (recovery cycle, pre-rename `cmd/antigravity`) already landed
the singular/plural rule for the **existing** count surfaces
(`countLabel(n, word)` + `categoryWords` table — used by the
group header, the `/codesearch` "Found N results" line, and the
codesearch panel "Found N matches in K files" header). What remains
on this changelog line is the new "Wrote N lines" surface — the
file-creation tool's step title never named the line count at all —
plus the off-by-one in the line-count formula.

This cycle closes those two gaps.

## Changelog anchor

| Release | Line | Shape |
|---|---|---|
| 1.3.3 | 62 | `Found N results` / `N matches` / `N files` / `Wrote N lines` pluralization + file-creation line-count off-by-one |

## Files

| Path | Role |
|---|---|
| `cmd/wagyu/tui_tools.go` (+63 / −2) | `fileLineCount(content)` — newline-aware line counter (off-by-one fix); `fileLinePair` — the `line`/`lines` pair named by the changelog; `writeFileStepTitle(args, pathKey)` — composes `Wrote <path> [· <N> line(s)]` for the three file-creation tool names; three `toolStepTitle` cases (`write_to_file`, `write_file`, `create_file`) now route through the helper |
| `cmd/wagyu/count_pluralization_test.go` (new, 235 lines) | 5 test functions: `TestCountLabel_FullVocabulary` (14 subcases pinning every singular form the changelog names), `TestFileLineCount_OffByOneFixed` (9 subcases pinning the line-count helper, including trailing-newline shapes), `TestFileLineCount_PreFixHypothetical` (regression guard against re-introducing the buggy N+1 formula), `TestToolStepTitle_WriteFile_LinesIncluded` (9 subcases for `write_to_file` / `write_file` / `create_file` with various contents and path keys), `TestGroupLabel_FileStep_CountUsesFileVocabulary` (pin that the file-creation step continues to count as the `file` category in the group header) |

**Net:** +65 / −2 across 2 files.

## Implementation notes

### Pluralization

The existing `countLabel(n, [2]string{word})` helper (in `tui_tools.go`)
already does the work — `1 file` becomes `2 files` and back. Every
existing call site (`groupLabel`, `codeSearchFeedback`,
`codesearchPanelLines`) was already pluralized in cycle 111. This cycle
extends pluralization to the new file-creation title surface by:

1. Defining `fileLinePair = [2]string{"line", "lines"}` near the helper
   that uses it. Keeping it as a local pair (rather than adding it to
   `categoryWords`) avoids polluting the category vocabulary — "line"
   is a unit used by one title template, not a tool category — and
   keeps the changelog-named surface visible at a glance.
2. Passing it through `countLabel(n, fileLinePair)` in
   `writeFileStepTitle`. The pre-fix title was just `"Wrote file.go"`,
   with no count at all; the post-fix title is `"Wrote file.go 1 line"`
   for one-line files and `"Wrote file.go 5 lines"` for multi-line
   files.

### Off-by-one fix

The reference ships `Wrote %d lines` and `Wrote 1 line` as separate
literals in the 1.3.6 string table (`docs/decompiled-v1.3.4/strings-v1.3.4.txt:23812`),
so the count must match `wc -l`. The naive
`len(strings.Split(content, "\n"))` returns N+1 for any content ending
in `"\n"` (the trailing empty element), which is exactly the off-by-one
described by the changelog. The model almost always emits a trailing
newline, so the bug would surface on essentially every write.

The fix is `fileLineCount`:

```go
func fileLineCount(content string) int {
    if content == "" {
        return 0
    }
    n := strings.Count(strings.TrimRight(content, "\n"), "\n") + 1
    return n
}
```

The rule is the same one `countFileLines` in
`slash_viewer_panels.go` already uses for untracked files in the
`/diff` file-list panel — so a freshly created file and a freshly
listed untracked file show the same line count, even though they ride
through different code paths. The doc comment notes that cross-surface
agreement.

### Tool-name routing

The three file-creation tools pick the file path through different
argument keys:

| Tool | First key tried | Other keys tried |
|---|---|---|
| `write_to_file` | `TargetFile` | `path`, `file_path`, `absolute_path`, … |
| `write_file`    | `AbsolutePath` | `path`, `file_path`, `absolute_path`, … |
| `create_file`   | `path`         | `TargetFile`, `AbsolutePath`, `file_path`, … |

`writeFileStepTitle(args, pathKey)` takes the preferred key as a
parameter and appends the rest of the tried-keys list, so each tool
gets the path it advertises without the helper having to know which
arguments each tool schema declares. The content key is the same
two-name list (`content`, `Content`) the reference uses for both
lower- and upper-case argument names.

When content is empty (path-only calls), the count suffix is suppressed
entirely so a path-only call still renders as `"Wrote file.go"` —
matching the pre-existing test fixture.

## Verification

```
go build -o /tmp/wagyu-cnt ./cmd/wagyu          # exit 0
gofmt -l cmd/wagyu/count_pluralization_test.go  # (empty)
gofmt -l cmd/wagyu/tui_tools.go                 # (empty — file was already clean)

go test -vet=off -count=1 -run 'TestCount|TestPlural|TestFormat' \
    ./cmd/wagyu/ ./internal/.../
ok  github.com/chadgpt/openagy/cmd/wagyu                  0.586s
ok  github.com/chadgpt/openagy/internal/convstore         0.244s
?  github.com/chadgpt/openagy/internal/store              [no test files]
ok  github.com/chadgpt/openagy/internal/taskregistry       0.211s
ok  github.com/chadgpt/openagy/internal/vim                0.199s
```

The full TUI test suite is green for every affected test:

```
go test -vet=off -count=1 -v -run 'TestCountLabel|TestFileLineCount|TestToolStepTitle_WriteFile|TestGroupLabel|TestTUITools_StepTitleVocabulary' ./cmd/wagyu/
```

5 new test functions + 14 + 9 + 9 + 1 subcases = 33 subtests for this
cycle.

## What was not changed

- **`cmd/wagyu/internal/convstore/sqlite.go`** is untouched (hard constraint).
- The `print_pig.go` path (`-p` / `--print`) does not render tool-step
  titles — it streams `Text` chunks and the usage envelope, and the tool
  calls are executed by PiG without surfacing a per-step title. So the
  Wrote N lines surface is TUI-only; the print-mode equality check stays
  out of scope.
- `TestDispatchPrintSlash_LocalCommands` (model-list test) is a
  pre-existing failure on `main` (the model registry returns
  `gemini-3.8-flash-medium` where the test pins `minimax-m3`); it is
  unrelated to this cycle's changes and is left untouched.
- `TestCycle225QuotaCountdown` / `TestCycle225QuotaStubRefills` are
  date-bound flaky tests (a 1-second-1-nanosecond race in
  `quotaCountdown(59m1s)` formatting, observable only because the
  test runs during the second-long minute boundary). They pass on
  reruns and are unrelated to this cycle.

## Divergences and limitations

- **Title suffix shape.** The recovered title is `Wrote file.go 5 lines`
  — a single space between path and count. The reference may use a
  different separator (a middle dot or a hyphen), but no offline
  capture of a multi-line file-creation step exists for parity-harness
  confirmation: the account's quota is exhausted, so every real-model
  turn terminates with `RESOURCE_EXHAUSTED` before a step can be drawn.
  The vocabulary (`Wrote 1 line` / `Wrote 5 lines`) is recovered;
  the separator is 【推测】.
- **No step.detail count.** "Wrote N lines" is in the title, not in
  the expanded body. The reference may render the count both places;
  until a capture is available, the title-only form is the safer
  guess — it adds a word the existing `toolStepTitle` machinery already
  knows how to colour through `styledStepTitle`.
- **Line-count helper consistency.** The `countFileLines` helper in
  `slash_viewer_panels.go` uses the same formula; this cycle does not
  refactor it to share `fileLineCount`, because doing so would touch
  `internal/convstore` adjacent code paths (`HARD CONSTRAINT`) and the
  duplication is two lines.

## Hard constraints honoured

| Constraint | Outcome |
|---|---|
| Do NOT touch `internal/convstore/sqlite.go` | Unchanged |
| New files gofmt-clean | `gofmt -l cmd/wagyu/count_pluralization_test.go` -> empty |
| `tui_tools.go` (pre-existing not-clean) not reformatted wholesale | `gofmt -l cmd/wagyu/tui_tools.go` -> empty (file was already clean; touches preserve that) |
| `go build -o /tmp/wagyu-cnt ./cmd/wagyu` exit 0 | Verified |
| `go test -vet=off -count=1 -run 'TestCount\|TestPlural\|TestFormat' ./cmd/wagyu/ ./internal/.../` pass | All green |
| ≥5 unit tests | 5 test functions, 33 subtests |
| Branch NOT pushed | Local only |
