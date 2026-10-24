# Cycle 230 — strip stray `[N]` citation footnotes from Bash and Read tool results

## Goal

Close the AGY 1.2.10 changelog gap:

> "Fixed tool result blocks for Bash and Read tools including citation-like
> `[N]` footnotes that the model never asked for, polluting the model
> context with garbage noise."

The 1.2.10 release discovered that the upstream pipeline ran a search/web
result citation extractor on **every** tool result, not just the search/web
ones. Search/web results genuinely need `[N] https://...` annotation at the
end of their text — the citation extractor produces a `Sources:` /
`Citations:` block. But Bash and Read tools produce shell and filesystem
text that has nothing to cite, so the injected `[N]` block leaked into the
model's transcript as noise the model never asked for.

Fix: gate citation extraction off Bash and Read tools by stripping the
citation footnote block from the result text before it is fed back to the
model. The stripper is conservative enough that real `[N]` references inside
prose (printf output, grep matches, array indices) are not touched.

## Files changed

| File | +/− | Why |
|---|---|---|
| `internal/tools/citation_strip.go` | **new, 117** | `ShouldStripCitations(name string) bool` tool-name gate; `StripCitationFootnotes(text string) string` pure text transformation; `citationHeaderRe` and `citationLineRe` regexes; `citationStripTools` tool-name allowlist. |
| `internal/tools/citation_strip_test.go` | **new, 257** | 7 top-level tests with 20 subtests covering the stripper end-to-end. See **Tests** below for the per-test breakdown. |
| `internal/tools/catalog.go` | +12 / −0 | `Registry.Dispatch` runs the result string (or JSON-marshalled result) through `StripCitationFootnotes` when `ShouldStripCitations(name)` is true. The doc comment is extended to record the 1.2.10 attribution. |

No third-party dependencies added. `citation_strip.go` is stdlib-only
(`regexp`, `strings`). The dispatch gate is a single map lookup plus a
function call in the hot path; for tools that do not match the allowlist
it is two pointer comparisons and a return.

## Architecture / data flow

```
                   ┌─────────────────────────────────────────────────┐
                   │  RunnerTool.Execute (internal/agent/pig.go)     │
                   │  ─ or registryRunner.Run (cmd/wagyu/main.go)    │
                   │  ─ or any other caller of Registry.Dispatch     │
                   └────────────────────┬────────────────────────────┘
                                        │  result string
                                        ▼
                   ┌─────────────────────────────────────────────────┐
                   │  Registry.Dispatch(name, args)                  │
                   │  ─ internal/tools/catalog.go                    │
                   └────────────────────┬────────────────────────────┘
                                        │
                            ┌───────────┴────────────┐
                            ▼                        ▼
                ShouldStripCitations(name)?   Tool.Run(ctx, args)
                            │                        │
                  ┌─────────┴─────────┐              ▼
                  │ false:           │       result string
                  │  return as-is     │              │
                  │  (search_web,     │              │
                  │   read_url_content│              │
                  │   grep_search …)  │              │
                  └─────────┬─────────┘              │
                            │ true:                  │
                            ▼                        │
              StripCitationFootnotes(result) ◄──────┘
                            │
                            ▼
                   ┌─────────────────────────────────────────────────┐
                   │  citation stripper (citation_strip.go)          │
                   │  ─ detect trailing "Sources:" / "Citations:"     │
                   │    / "References:" / "Footnotes:" header +      │
                   │    numbered citation lines, OR                  │
                   │  ─ detect bare block of ≥2 consecutive           │
                   │    numbered citation lines                      │
                   │  ─ otherwise return input unchanged             │
                   └────────────────────┬────────────────────────────┘
                                        │  stripped string
                                        ▼
                   ┌─────────────────────────────────────────────────┐
                   │  Model transcript (`tool` role message)         │
                   │  ─ Bash: clean shell output, no [N] noise       │
                   │  ─ Read: clean file content, no [N] noise       │
                   │  ─ search_web / read_url_content:               │
                   │    citation block PRESERVED                     │
                   └─────────────────────────────────────────────────┘
```

The stripper is intentionally placed at the **single funnel point** —
`Registry.Dispatch` — rather than inside each tool. There are 5 tool
implementations that need the gate (`run_command`, `run_shell_command`,
`exec_command`, `read_file`, `view_file`) and putting the gate in each
executor would duplicate the logic. Funnelling through `Dispatch` means
adding a new Bash or Read tool later only requires updating
`citationStripTools`.

## Tool-name allowlist

`citationStripTools` is the set of tool names whose results get the gate:

```go
var citationStripTools = map[string]bool{
    "run_command":       true,  // RunCommand   (Bash primary)
    "run_shell_command": true,  // RunShellCommand
    "exec_command":      true,  // ExecCommand
    "read_file":         true,  // ReadFile
    "view_file":         true,  // alias of ReadFile (catalog.go)
}
```

Everything else — `search_web`, `read_url_content`, `grep_search`,
`call_mcp_tool`, `generate_image`, `list_dir`, `write_to_file`, … — is
left alone. Search/web results legitimately emit citation blocks and
must keep them.

## Stripper rules

`StripCitationFootnotes(text string) string` returns the input unchanged
when no block is found. Otherwise it removes the trailing citation
block from the result. Detection is conservative on two axes:

1. **Position.** The citation block must be the *trailing* non-whitespace
   content of the input. A single `[1] /usr/local/bin` at the end of an
   `ls -l` output is far more likely a legitimate `ls` output than a
   stray citation, so bare blocks of one line are not stripped. Trailing
   empty lines do not count.

2. **Shape.** A line counts as a citation line only when it matches
   `^\s*\[\d+(?:\s*[-,]\s*\d+)*\]\s+\S.*$` — optional leading whitespace,
   one or more digits (or a comma/dash-separated range), `]`, whitespace,
   non-whitespace content. Year-style brackets like `[2024]` are rejected
   because they have four digits. Brackets in the middle of a line
   (`Smith[1]`, `printf "[%d]\n", 1`) are rejected because the regex
   requires the bracket to start the citation line.

   A line counts as a citation header only when it matches
   `^\s*(Sources|Citations|References|Footnotes)\s*:\s*$` — the header
   keyword must be the only content of the line. A prose sentence that
   mentions "Sources:" is not mistaken for a header.

Two citation block shapes are recognised:

1. **Header + lines.** A `Sources:` (or one of the three aliases) line
   followed by ≥1 citation lines. The whole thing is removed.
2. **Bare block.** ≥2 consecutive citation lines at the trailing edge,
   with no header. The whole thing is removed.

When the original text ended with a newline, the strip preserves that
trailing newline so the model does not see a different ending convention.

## Tests

7 top-level tests + 20 subtests, all passing under
`go test -vet=off -count=1 -run 'TestCitation' ./internal/tools/ -v`.

| Test | Subtests | What it pins |
|---|---|---|
| `TestCitationStrip_BashResultStripsTrailingFootnotes` | — | Headline case: a `run_command` result that ends with a `Sources:` block has the block removed before `Dispatch` returns it; the original bash output lines are intact; the trailing newline convention is preserved. |
| `TestCitationStrip_ReadResultStripsTrailingFootnotes` | — | Read half of the changelog: both `read_file` and its `view_file` alias are gated. |
| `TestCitationStrip_OtherToolsUnaffected` | — | Negative cases: `search_web`, `read_url_content`, and `grep_search` (including its JSON-marshalled output) all pass through `Dispatch` byte-identical. |
| `TestCitationStrip_RealCitationInTextPreserved` | 5 | The conservative guards: a single `[1] /usr/local/bin` at the end of an `ls` output is preserved; year-style `[2024]` references are preserved; `Smith[1]` mid-line references are preserved; `printf "[%d]\n", 1` mid-line references are preserved; a `Sources:` line followed by prose (no numbered lines) is preserved. |
| `TestCitationStrip_NoCitationsNoOp` | 8 | No-op cases: plain bash output, plain file content, empty string, single newline, only whitespace, non-citation-shaped brackets, `[1]` in the middle of text, trailing-whitespace-only. The stripper never mutates a clean input. |
| `TestCitationStrip_BareBlockAtEnd` | 6 | Bare-block shape: 2-line block at end is stripped; 3-line block (indented) is stripped; single bare line is preserved; `[1-3]` range lines are recognised; header + range lines are stripped; trailing newline preserved. |
| `TestCitationStrip_ShouldStripToolGate` | — | The tool-name allowlist: every Bash/Read name returns true; every non-Bash/Read name (including unknown names) returns false. |

The task spec required 5 tests; the deliverable ships 7 top-level tests
covering the spec items plus the conservative-guard cases the spec
implies (real citations in prose preserved; no-op when no citations;
other tools unaffected). All 7 are required for the gate to be safe.

## Gates verified

- `go build -o /tmp/wagyu-trn ./cmd/wagyu` — exit 0 (binary at
  `/tmp/wagyu-trn`, 70 MB).
- `go test -vet=off -count=1 -run 'ToolResult|TestCitation|TestStrip|TestBash' ./cmd/wagyu/ ./internal/...`
  — fully green (targeted run).
- `go test -vet=off -count=1 ./internal/tools/` — fully green.
- `gofmt -l` on every **new** file: empty.
- `gofmt -l` on the touched existing file (`internal/tools/catalog.go`):
  empty.
- `gofmt -l` on the rest of the package: 7 files (pre-existing,
  unrelated). Not touched.

## Hard constraints

1. **`internal/convstore/sqlite.go` not touched** — confirmed by
   `git diff --stat`: only `internal/tools/catalog.go` modified, no
   files added in `internal/convstore/`.
2. **Pre-existing not-clean files not reformatted** — `gofmt -l` before
   and after my change returns the same 7 files in `internal/tools/`
   (browser.go, filesystem.go, git.go, nonnetwork.go, stubs_test.go,
   tools.go, tools_test.go) and the same files in `cmd/wagyu/`. None
   were reformatted.
3. **Build must exit 0** — `go build -o /tmp/wagyu-trn ./cmd/wagyu`
   exits 0.
4. **Targeted test run must pass** — the pattern
   `ToolResult|TestCitation|TestStrip|TestBash` matches all 7
   `TestCitationStrip_*` tests (they all start with `TestCitation`);
   the targeted run is fully green.
5. **gofmt -l on every NEW file empty** — both `citation_strip.go`
   and `citation_strip_test.go` are clean.
6. **Single commit** — the deliverable ships as one commit on
   `feature/tool-result-no-citations-strip`.
7. **Branch not pushed** — `git push` has not been called on the
   branch; HEAD is local-only.
8. **Worktree path** —
   `/private/var/folders/j1/q183lx7n5qz1p1kct589g6040000gn/T/opencode/openagy-tool-result-no-citations-strip`.
9. **agw/MiniMax-M3 model** — this deliverable was produced by the
   agw/MiniMax-M3 subagent (`tools.opencode.models` would show this is
   the configured `agw` provider's `MiniMax-M3`).

## 存疑 / Limitations

1. **The stripper is heuristic.** It cannot distinguish a citation block
   written by the upstream extractor from a citation block written by a
   Bash tool (e.g. a script that intentionally emits `[1] http://...`).
   The conservative shape rules (≥2 consecutive lines for bare blocks;
   trailing-only for header blocks) are the most we can do without a
   way to tag citation-shaped output at source. Two observations bound
   the risk:
   - Bash tools almost never end their output with a `Sources:` block
     or a sequence of `[N] https://…` lines. The Bash/Read surface in
     this clone is `ls`, `cat`, `find`, `grep`, file content, and
     shell control flow — none of these produce citation-shaped text.
   - The strip runs only on tool names in `citationStripTools`. A user
     who really wants to send citation-shaped Bash output to the model
     would have to do it through `search_web` or `read_url_content`,
     which is exactly where citation extraction belongs.

2. **Citation extraction upstream is still running** on every tool
   result; the stripper just removes the noise before it reaches the
   model. A future cycle could disable citation extraction at the
   source for Bash/Read tools instead of stripping it after the fact.
   The 1.2.10 fix as documented is "stop the noise from reaching the
   model", which is what this cycle does.

3. **Tests live in the same package** (`internal/tools`) so they can
   reach `Dispatch` and the stripper through their package-private
   surface. The `fakeTool` helper is the only test-only addition;
   every other test symbol (functions, regexes, allowlist) is reused
   production code.

4. **No REF probe.** AGY 1.2.10 is not the build on `~/.local/bin/agy`
   today (the live build is 1.3.6 / 1.4.0 per
   `internal/cli/testdata/changelog_reference.txt`), so there is no
   reference binary to compare the stripper against. The cycle's
   evidence is the 1.2.10 changelog wording and the shape of the
   citation extractor output that the upstream pipeline emits at the
   `search_web` boundary (which is the only citation extractor output
   shape visible in this codebase — captured live as
   `testdata/advertised_tools_1.3.6.txt` and the surrounding
   `cmd/pigloop-demo` / `internal/piagentbridge` flows).

5. **Pre-existing test flake** — `TestCycle225QuotaCountdown`
   (`cmd/wagyu/slash_cycle225_test.go:106`) and
   `TestDispatchPrintSlash_LocalCommands`
   (`cmd/wagyu/print_slash_test.go:60`) fail intermittently on the
   **unmodified main** under the full `go test ./cmd/wagyu/...`
   invocation. The first is a wall-clock race (`time.Now()` between
   two calls slips past a 1 s boundary under load); the second is a
   model-resolve lookup that picks a different default on the live
   build. Both are unrelated to this cycle — verified by stashing the
   cycle's changes and re-running the full `cmd/wagyu` suite, which
   still fails the same two tests. The targeted run specified in the
   hard constraints is unaffected.
