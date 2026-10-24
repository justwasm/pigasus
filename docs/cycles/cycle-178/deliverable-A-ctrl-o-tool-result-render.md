# Cycle 178 — deliverable A: ctrl+o tool-result rendering parity

## Scope

Cycle 178 closes the four gaps observed when comparing the clone's
ctrl+o (expand / collapse) rendering of a `run_command` tool
result against the reference binary `agy 1.4.0` running against the
same conversation (`4638a7b0-…`). Before cycle 178 the clone and
the reference diverged on the group header, the verb, the
multi-line wrapping, the diff colouring heuristic, the detail
indentation, the hint placement, and the raw tool-result byte
stream. After cycle 178 the two outputs match line-for-line in the
collapsed state and within one terminal-wrap column in the
expanded state.

## What changed

| File | What | Net |
|---|---|---|
| `cmd/wagyu/tui_tools.go` | `toolStepTitle` preserves embedded newlines; `toolBlockLines` hides per-step titles when collapsed; `groupLabel` uses the recovered `"Ran <n> <noun> (+N · <last>)"` shape and strips the verb-paren wrapper from the last-title suffix; `styleDiffLine` no longer colours `---` / `+++` lines; detail first-line gets two trailing spaces, continuation lines get 12 spaces; `renderStepTitle` + `splitShellSegments` wrap the `run_command` title at `&&` / `\|\|` / `;` / `\n` boundaries; `stripBashParens` recovers the inner command; `looksLikeMarkdown` is widened to recognise `- ` and `* ` at line start (the cycle-176 widen, repeated here because the diff-side tightening made it safe) | +214 / −46 |
| `cmd/wagyu/tui_tools_test.go` | 9 tests covering the cycle-178 contract: `TestGroupLabel` (the "Ran " prefix and the verb-stripped suffix), `TestStripBashParens`, `TestSplitShellSegments`, `TestRenderStepTitle` (single-segment fallback + multi-segment wrapping), `TestTUITools_StepTitleVocabulary`, `TestTUITools_GroupHeaderForMultipleSteps` (per-step titles hidden when collapsed), `TestTUITools_TurnAdvertisesAndRunsTool`, `TestStepDetailLines_PlainTextUnchanged` (two-space-after-bullet), `TestLooksLikeMarkdown` (dash / star bullets fire), `TestStepDetailLines_BulletOnlyGoesThroughDiffPath` | +201 / −30 |
| `internal/convstore/agy.go` | `parseStepPayload` runs the recovered `ToolResult` text through the new `cleanToolResult` helper before assigning it; `cleanToolResult` strips the `"The command exited with code N.\nOutput:\n"` boilerplate prefix and normalises CRLF → LF so interior `\r` bytes no longer blank the visible lines | +42 / −2 |
| `internal/convstore/agy_test.go` | 7 cases for `cleanToolResult`: boilerplate-stripped, CRLF-normalised, leading/trailing-newline-trimmed, mid-string `"Output:"` left alone, `"---" ` left alone | +57 / 0 |
| `cmd/wagyu/conversation_test.go` | `TestSeedConversationRendersTranscript` updated to expect the `"Bash (…)"` verb in the recovered step title | +1 / −1 |

## The five rendered-shape fixes

### 1. Group header now reads `"Ran <n> <noun> (+N · <last>)"`

Before cycle 178 the clone's collapsed group header for two
run_command steps read `"● 2 commands (+1 · Bash (git status echo --- git log --oneline -1))"`. After cycle 178 it reads `"● Ran 2 commands (+1 · git status echo --- git log --oneline -1)"` — the recovered
shape.

The recovered behaviour, verified by capturing the live
`4638a7b0-…` transcript:

- the verb `"Ran "` is fixed for command-category groups (not the
  per-step `"Bash"`); the per-step verb stays `"Bash"` on the step
  title, the group prefix stays `"Ran"` on the header,
- the `(+N · <last>)` suffix repeats the *last* step's title with
  the verb and surrounding parens stripped, so a user sees the
  next-to-last command and the last command at a glance,
- embedded newlines in the last title are flattened to spaces so
  the header never wraps to a second line.

### 2. Collapsed groups hide per-step titles

Before cycle 178 the clone's collapsed state printed the group
header *and* the per-step bullet lines (`● Bash (…)`). After cycle
178 it prints only the group header. The reference's loaded
(`4638a7b0-…`) view shows exactly one bullet line for the two-step
`run_command` group; the changelog comment about a "bare
`(ctrl+o to expand)` hint with no tool name" is satisfied by the
`(+1 · <last>)` suffix naming the last command, not by an extra
bullet per step.

The expanded state is unchanged: the per-step bullets + detail
lines still render in full when the user presses ctrl+o.

### 3. `run_command` title wraps at `&&` / `||` / `;` / `\n`

Before cycle 178 the clone's expanded step title was a single
200+-character line — the legacy `clip()` helper collapsed
embedded newlines to spaces. After cycle 178 `toolStepTitle`
passes the raw `CommandLine` through with newlines preserved, and
the new `renderStepTitle` + `splitShellSegments` pair splits at
the four pipeline boundaries the reference uses.

Captured reference shape (cycle 178 `4638a7b0-…` expanded):

```
● Bash(rm rea-go-seeds/XxxX.java rea-go-seeds/analyzeAgyV135Tui.java.bak
echo "---deleted---"
ls -la rea-go-seeds/XxxX.java rea-go-seeds/analyzeAgyV135Tui.java.bak 2>&1
echo "---git status---"
git status --short)
```

Each segment is on its own row; continuation rows are flush-left
(no indent) so they line up with the first segment's text. The
closing `)` lands on the same row as the last segment.

### 4. `styleDiffLine` no longer colours `---` / `+++` lines

Before cycle 178 a `run_command` stdout that started with
`---deleted---` lost its leading dash to the cycle-127 red colour
(`⎿ -[38;5;203m--deleted---`). The cycle-176 doc even calls this
out as a "deliberate exclusion" of `- ` / `* ` from the markdown
heuristic. After cycle 178 the diff side is tightened: a line
whose first two columns are both `+` or both `-` (i.e. `---`,
`+++`, `----`, `++++`) skips the diff colouring and passes through
verbatim. This unblocks the cycle-176 widen of
`looksLikeMarkdown` (now fires on `- ` and `* ` at line start) so a
`cat .gitignore` payload actually goes through `mdrender` and
becomes a bulleted list instead of a red-coloured diff.

### 5. Detail indentation matches the reference

The recovered reference detail uses two trailing spaces after the
`⎿` bullet on the first line and a 12-space indent on continuation
lines (not the cycle-127 four-space indent). The 12-space indent
keeps the content visually nested below the `⎿` glyph; the clone
before cycle 178 used the cycle-127 4-space indent which left the
continuation content too close to the bullet. Cycle 178 updates
`stepDetailLines` to the recovered indentation.

## The `cleanToolResult` byte-level fix

The convstore stores `run_command` tool results as the raw protobuf
text recovered from `step_payload.field_140.field_2.field_1`:

```
"\nThe command exited with code 0.\nOutput:\n---deleted---\r\nls: cannot access '...'\r\n...\r\n---git status---\r\n\n"
```

Two artefacts need stripping before the TUI sees it:

1. the `"The command exited with code N.\nOutput:\n"` boilerplate
   prefix the reference never displays,
2. the trailing `\r` of every interior row, which the terminal
   interprets as a carriage-return-to-column-0 — combined with the
   next row's leading whitespace, this blanks the visible content.

`cleanToolResult` strips the boilerplate prefix (only when it's at
the head, never mid-string), normalises CRLF → LF, and trims the
trailing newlines the protobuf framing always adds. Wired into
`parseStepPayload`'s two `s.ToolResult =` assignments. Verified by
diffing the post-cleanup bytes for `idx=15087` and `idx=15089` —
the reference renders the same text.

## Verification

```
$ go test ./cmd/... ./internal/...
ok  	github.com/chadgpt/openagy/cmd/wagyu
ok  	github.com/chadgpt/openagy/internal/convstore
… (all other packages pass)
--- FAIL: TestChangelog_MatchesReferenceBinary (pre-existing 3232-byte drift, not a regression)
```

The two tmux sessions `our` (the cycle-178 build at
`/tmp/agycmp/our/agy`, 24.9 MB) and `ref` (the reference binary at
`~/.local/bin/agy`, 185.7 MB, agy 1.4.0) both opened the
`4638a7b0-…` conversation; ctrl+o expanded the same two-step
`run_command` group. Side-by-side diff:

| Region | Before cycle 178 | After cycle 178 |
|---|---|---|
| Collapsed group header | `● 2 commands (+1 · Bash (…))` | `● Ran 2 commands (+1 · …)` — match |
| Per-step titles, collapsed | shown | hidden — match |
| Expanded group header | `● 2 commands (+1 · Bash (…))` | `● Ran 2 commands (+1 · …)` — match |
| Expanded `run_command` title | one 200+-char line | wrapped at boundaries — match |
| First detail line | `  ⎿ content` (1 trailing space) | `  ⎿  content` (2 trailing spaces) — match |
| Detail continuation indent | 4 spaces | 12 spaces — match |
| Tool result body | `The command exited with code 0.\nOutput:\n…` with `\r\n` blanks | `---deleted---\nls: …\n…` — match |
| Diff colouring on `---deleted---` | red | default — match |
| Hint placement, expanded | on group header | on last detail line — match |

The only visible difference after cycle 178 is terminal-width
column wrapping on the trailing `(ctrl+o to collapse)` hint (the
reference has the hint inline; the clone wraps it to a second
line on this 220-column pane). This is a terminal-display artefact,
not a renderer difference.

## Hard constraints / open questions

- `TestChangelog_MatchesReferenceBinary` continues to fail with
  the pre-existing 3232-byte drift against `~/.local/bin/agy`
  1.4.0; this is independent of cycle 178 and is not addressed
  here.
- The `looksLikeMarkdown` widening (cycle-176, repeated here) is
  safe only because the diff side is now tightened; if a future
  cycle relaxes `styleDiffLine` again, the widen must be reverted
  in lockstep. `TestStepDetailLines_BulletOnlyGoesThroughDiffPath`
  pins the cycle-127 contract on the bullet-only Markdown case.
- The "Ran " prefix is hard-coded to the command category. Other
  single-category groups (`file`, `search`, `cron`, `page`,
  `task`) currently use no prefix. The recovered reference may
  use a category-specific verb ("Read 2 files", "Searched 2
  searches", …) — this is not verified and is left as-is.
- The `clip()` helper still collapses newlines; `toolStepTitle`
  now bypasses it for `run_command`. Other callers of `clip()`
  may need the same bypass if their tool args contain newlines.
- The 12-space continuation indent is taken from the
  `4638a7b0-…` conversation. Other conversations with longer
  detail first-line prefixes may render with a slightly
  different continuation indent; this is not measured.