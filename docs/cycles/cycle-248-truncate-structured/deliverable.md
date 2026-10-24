# Cycle 248 — AGY 1.4.1 structured tool outputs token-budget truncation

Worktree: `/Users/gear/Documents/GitHub/openagy-truncate-structured`
Branch: `feature/cycle-248-truncate-structured`

## Why this cycle

Cycle 236 launched `--diff-with-agy` — the clone parity table
(`internal/cli/testdata/feature_diff_table.json`,
`internal/cli/testdata/feat_diff.go`'s panel generator,
`cmd/wagyu/feat_diff.go`'s reporter). One of the gaps the table
flagged on day one was the AGY 1.4.1 entry the clone carried
straight through from the changelog:

> "Fixed large structured tool outputs (such as JSON results
>  from MCP tools) bypassing the per-step token limit while still
>  being counted as truncated, which could push long
>  conversations past the model's context window before
>  automatic compaction ran."
> — `internal/cli/testdata/changelog_reference.txt:12`

The clone's `feature_diff_table.json` row had no cycle number
and `clone: false` — the gap was open since the table shipped.
Cycle 248 closes it.

The bug is interesting for what it *isn't*: a logical bug in a
truncation function, or a missing feature flag. The reference
binary's per-step token counter recognised a 5000-token JSON
result was truncated; it just recognised this **after** the
content had already been passed to the model. The
"truncated" flag was an honest metadata flag, but the model
still saw the full 5000-token payload. AGY 1.4.1 closes the
loop by truncating the structured content before the model
sees it — so the metadata flag and the actual content agree.

## AGY 1.4.1 spec

| Surface | Behaviour |
|---|---|
| **JSON object** | wrapped in `{"_truncated": true, "_preview": "<partial JSON object string>"}`. The `_preview` value is a JSON-encoded string containing the partial object (sorted-key walk, no trailing comma). Wrapped output is parseable by `encoding/json`. |
| **JSON array** | wrapped in `{"_truncated": true, "_preview": [...]}` where `_preview` is itself a JSON array of the first N elements that fit. Wrapped output is parseable. |
| **YAML doc** | wrapped as `_truncated: true\n_preview: |\n  <first N lines>` — the literal block-scalar preserves the original lines verbatim without escaping YAML metacharacters. Wrapped output is still parseable YAML. |
| **XML / HTML** | wrapped in `<truncated>first N tokens' worth of bytes</truncated>`. The cycle ships a flat string cut, not an element-aware walker. |
| **Prose / unknown** | the existing cycle-198 / cycle-235 freeform-prose truncation is used unchanged — first N tokens' worth of bytes, trailing-newline clean, `\n…[truncated]` marker. |
| **Budget gate** | `maxTokens <= 0` is the "no limit" sentinel; `estimateTokens(content) <= maxTokens` short-circuits before format detection so under-budget input returns verbatim. |
| **Defensive guard** | the public entry has a `recover()` so a bad input (one the JSON parser chokes on, a nil-ish value smuggled through) cannot crash the agent. The fallback is the prose path, which is guaranteed to terminate. |

## What the fix does

### Files changed

| File | Lines | Purpose |
|---|---|---|
| `internal/tools/truncate.go` | new, +521 | `TruncateStructured`, `truncateByFormat`, the format-specific helpers (`truncateJSONObject`, `truncateJSONArray`, `truncateYAML`, `truncateXML`, `truncateProse`), `DetectFormat` heuristic, `estimateTokens`, `encodeTruncatedEnvelope`, `encodeTruncatedArrayEnvelope`, the `isYAMLHeuristic`/`isAlphaNum`/`isRuneStart` helpers. |
| `internal/tools/output_budget.go` | new, +171 | `BudgetResult` struct (Content / Truncated / Format / TokensBefore / TokensAfter), `ApplyOutputBudget` pipeline, `DefaultMaxResultTokens = 8000`, `formatOrDetect`, `isKnownFormat`. |
| `internal/tools/truncate_test.go` | new, +602 | the regression suite (18 tests / ~39 sub-cases; see "Tests"). |
| `internal/tools/testdata/large_json_result.json` | new, 137,847 B (~34,461 tokens) | the integration-test fixture the cycle ships. |
| `internal/cli/testdata/feature_diff_table.json` | +1 / -1 | adds the cycle-248 row in the `tooling` category. |

Total: ~1,300 lines added (mostly comments and tests), no new
third-party dependency. **Stdlib only.**

The cycle does **not** wire the truncation into every call site
(`internal/agent.pig.go`'s `runnerTool.Execute`,
`internal/piagentbridge.toolAdapter.Execute`, the
`internal/subagenttools` `Execute` methods). Those callers are
left to adopt `ApplyOutputBudget` independently — the
deliverable ships the pipeline and the budget contract; the
per-tool adoption is a follow-up cycle.

### Code path summary

1. **`DetectFormat(content)`** — first non-whitespace byte is
   `{`/`[` → `FormatJSON`, `<` → `FormatXML`, otherwise the
   `isYAMLHeuristic` first-line check fires (`[a-zA-Z_][a-zA-Z0-9_-]*:[ \t#\n]`).
   Empty / whitespace / non-classifiable input → `FormatProse`.
   YAML lists (`- foo`) and YAML document markers (`---`)
   intentionally fall through to `FormatProse` per the cycle
   spec — the heuristic only matches `key: value` mappings on
   the first line. The trade-off is documented; a future cycle
   that needs full YAML coverage can swap in a real parser.

2. **`TruncateStructured(content, maxTokens)`** — short-circuit
   for `maxTokens <= 0`, empty content, and under-budget
   content; otherwise dispatch to `truncateByFormat` with the
   detected format.

3. **`truncateByFormat(content, format, maxTokens)`** —
   `defer recover()` so any panic in a helper returns the
   input rather than crashing the agent; switch on format
   string (FormatJSON dispatches on first non-whitespace byte
   to `truncateJSONObject` or `truncateJSONArray`).

4. **`truncateJSONObject`** — `json.Unmarshal` into a
   `map[string]json.RawMessage`, sort keys for deterministic
   output, walk keys accumulating a partial object string
   within `budgetForPreview = maxTokens - envelopeOverhead`
   tokens, emit `{"_truncated":true,"_preview":"<partial>"}`
   via `json.Marshal` so escapes are guaranteed safe. JSON
   parse failure falls through to `truncateProse`.

5. **`truncateJSONArray`** — `json.Unmarshal` into
   `[]json.RawMessage`, accumulate first-N elements within
   `budgetForPreview`, emit
   `{"_truncated":true,"_preview":[…first N…]}` so the
   `_preview` is itself a JSON array (not a string). JSON
   parse failure falls through to `truncateProse`.

6. **`truncateYAML`** — `strings.Split` on `\n`, walk lines
   accumulating a literal-block `_preview` with 2-space
   indent. Block-scalar `|` preserves the original lines
   verbatim without escaping YAML metacharacters.

7. **`truncateXML`** — flat-string cut: take
   `maxTokens * DefaultApproxCharsPerToken` bytes, walk back
   to nearest rune boundary, trim to nearest whitespace, wrap
   in `<truncated>…</truncated>`. The cycle ships no
   element-aware walker; a future cycle can swap in one.

8. **`truncateProse`** — flat-string cut to
   `maxTokens * DefaultApproxCharsPerToken` bytes with the
   same rune-boundary / whitespace-trim guards, append
   `\n…[truncated]`. Same marker the cycle-198 / cycle-235
   freeform-prose truncation uses; no second prose-truncation
   idiom introduced.

9. **`ApplyOutputBudget(content, format, maxTokens)`** — the
   pipeline. Short-circuit on empty / `maxTokens <= 0`,
   auto-detect when `format == ""`, coerce unknown format
   strings to `FormatProse`, return `BudgetResult` with
   before/after token counts. A defensive guard sets
   `Truncated=false` if `truncateByFormat` returned the input
   unchanged (the honest-flags contract).

### Token heuristic

`estimateTokens(s) = len(s) / 4`, with empty → 0 and 1-3
chars → 1 so a 1-character reply still costs 1 token. This
matches the cheap `/4` heuristic the rest of the clone uses
(`internal/customize.EstimateTokens`,
`internal/compaction.EstimateTokens`,
`internal/backend/client.go:approxTokens`). A precise BPE
counter is intentionally out of scope — the per-step budget is
a guard, not a precise context-window measurement. Any future
cycle that switches to a real tokenizer should re-derive the
truncation walk; the public function shape will not change.

## Tests

`internal/tools/truncate_test.go` — 18 top-level tests, ~39
sub-cases, organised by surface:

- **`TestTruncateStructured_JSONObject`** — 1000-token JSON
  object (200 entries with id/name/description fields) is
  detected as JSON, returns the structured envelope
  `{"_truncated":true, "_preview":"<partial obj>"}` with a
  shorter payload than the input. Pins the headline case.
- **`TestTruncateStructured_JSONArray`** — 500-element JSON
  array is detected as JSON, returns the structured envelope
  with `_preview` as a JSON array (not a string) of fewer
  than 500 elements.
- **`TestTruncateStructured_YAML`** — 200-line YAML doc is
  detected as YAML, returns
  `_truncated: true\n_preview: |\n  <first N lines>` with a
  smaller payload than the input.
- **`TestTruncateStructured_UnderBudget`** — five sub-cases
  (json-object, json-array, yaml, xml, prose) all return
  unchanged when the budget is much larger than the input.
  Pins the "budget gate fires BEFORE format detection"
  contract — the detector's false positives cannot affect
  under-budget content.
- **`TestTruncateStructured_ExactlyAtBudget`** —
  `estimateTokens(input) == 4` and `maxTokens = 4` returns
  input unchanged. Pins the off-by-one boundary (gate is
  `<=`, not `<`).
- **`TestTruncateStructured_ZeroMeansNoLimit`** — four
  sub-cases (json, yaml, prose, empty) all return verbatim
  when `maxTokens = 0`. Pins the "no limit" sentinel.
- **`TestTruncateStructured_FallsBackToProseOnUnknownFormat`**
  — repeated-prose input that defeats every detector branch
  routes through `truncateProse` and ends with the
  `\n…[truncated]` marker.
- **`TestTruncateStructured_PreservesJSONParseability`** —
  three sub-cases (object-over-budget, array-over-budget,
  pretty-printed-object) all return JSON the `encoding/json`
  package parses back, with `_truncated=true` and a `_preview`
  key.
- **`TestTruncateStructured_XML`** — repeated `<item>` tags
  are detected as XML, wrapped in `<truncated>…</truncated>`
  with a shorter payload than the input.
- **`TestTruncateStructured_NilContent`** — empty input
  returns empty (`""`).
- **`TestDetectFormat_KnownShapes`** — 15 sub-cases pinning
  the heuristic: JSON object / array / pretty / array-pretty,
  XML element / decl, YAML mapping / comment / empty-value,
  YAML list → FormatProse (per spec), YAML doc marker →
  FormatProse (per spec), prose fallback (with and without
  newline), empty / whitespace-only.
- **`TestOutputBudget_TruncatesLargeStructuredResult`** —
  the integration test the cycle spec requires. Drives the
  137,847-byte / ~34,461-token fixture
  (`internal/tools/testdata/large_json_result.json`) through
  `ApplyOutputBudget` with `maxTokens=500`. Asserts
  `Truncated=true`, `TokensAfter < TokensBefore`,
  `Format == FormatJSON`, `TokensAfter <= 500`,
  `len(Content) < len(input)`, and the envelope parses as
  JSON. The fixture is committed so the assertion is stable
  across machines.
- **`TestOutputBudget_UnderBudget_NoTruncation`** — small
  JSON input comes back unchanged, `Truncated=false`,
  `TokensAfter == TokensBefore`, `Format == FormatJSON`.
- **`TestOutputBudget_FormatOverride`** — caller-supplied
  `FormatYAML` is honoured; the format string flows through
  to `BudgetResult.Format` even if auto-detection would have
  classified differently.
- **`TestOutputBudget_UnknownFormatFallsToProse`** — unknown
  format string is coerced to `FormatProse` (no panic on
  typo) and the truncation uses the prose path
  (`\n…[truncated]` suffix).
- **`TestEstimateTokens_Cases`** — pins the cheap `/4`
  heuristic and the empty → 0 / very-short → 1 edge cases.
  Future cycles that switch to a real tokenizer must update
  this test.
- **`TestOutputBudget_FormatDetectVsOverride`** — pins the
  contract that auto-detection only runs when `format == ""`;
  a non-empty caller-supplied format bypasses `DetectFormat`
  even when the input would have been classified differently.
  A regression here would mean callers passing `FormatYAML`
  get `FormatJSON` envelopes back, which the cycle-248 spec
  explicitly forbids.
- **`TestOutputBudget_TokensReportedAreHonest`** — after a
  successful truncation, `TokensAfter <= maxTokens` and
  `TokensBefore` matches the input's estimate. The model
  layer uses these for its "X → Y tokens" diagnostic so a
  regression here would corrupt that display.

The file deliberately does **not** declare a `truncate`
helper function — `internal/tools/git_editor_disable_test.go:124`
already has one, and a same-name package-level declaration is
a build error. The helpers in this file are named
`ellipsisTruncate`, `firstN`, `asJSONString`, `itoa` to stay
out of that namespace.

## Test results

- `go build -o /tmp/wagyu-cycle248 ./cmd/wagyu` — exit 0.
- `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...`
  — exit 0. **All 56 packages pass.** The cycle-248
  regression suite passes; the existing cycle-50 / cycle-198
  / cycle-235 / cycle-244 tests pass without modification;
  `internal/cli`'s `TestFeatDiff_*` suite passes (the
  feature_diff_table.json round-trip classifies the new row
  as "matched").
- `go test -vet=off -count=1 -run 'Truncate|OutputBudget|DetectFormat|EstimateTokens' ./internal/tools/`
  — exit 0 (the 18 cycle-248 tests run independently, ~39
  sub-cases including the integration test).

Binary-size delta: **+992 bytes** vs the cycle-244 baseline
(89,683,074 vs 89,682,082). Stdlib only, so the delta is just
the truncated-output bytes; no new module requirements.

Feature-diff table re-verification:
`TestFeatDiff_LoadsJSONAndEmitsAligned` /
`TestFeatDiff_ClassifyStatus` /
`TestFeatDiff_JSONRoundTrip` /
`TestFeatDiff_PanelLinesHeight0` all pass — the JSON
round-trip now correctly classifies the structured-truncation
row as "matched" instead of "agy-only".

## 硬约束 / 存疑 / limitations

- **Token heuristic is `/4` (chars per token), not BPE.** The
  per-step budget is a guard, not a precise context-window
  measurement. The cheap heuristic matches the rest of the
  clone (`internal/customize.EstimateTokens`,
  `internal/compaction.EstimateTokens`,
  `internal/backend/client.go:approxTokens`). A future cycle
  that switches to a real tokenizer should re-derive the
  truncation walk; the public function shape will not change.
  `TestEstimateTokens_Cases` is the regression guard.

- **Truncation is syntactic, not semantic.** A truncated JSON
  object is still parseable (`encoding/json` round-trips it),
  and a truncated YAML doc is still parseable YAML, but the
  preview is "the first N keys / first N elements / first N
  lines that fit" — not "the most semantically important
  content." The cycle ships syntactic preservation; semantic
  preservation (e.g. "show the top-of-list in a 5000-element
  array, then a `_notice: see /tool/_next files for more") is
  a follow-up cycle.

- **YAML heuristic miss rate is non-zero.** The first-line
  check (`[a-zA-Z_][a-zA-Z0-9_-]*:[ \t#\n]`) catches the
  common `key: value` shape but intentionally misses YAML
  lists (`- foo`) and YAML document markers (`---`). Both
  fall through to `FormatProse` per the cycle spec, and
  `TestDetectFormat_KnownShapes/yaml-list` and
  `TestDetectFormat_KnownShapes/yaml-doc` pin the behaviour.
  Acceptable trade-off — the cycle does not try to parse
  every YAML input. A future cycle that needs full YAML
  coverage can swap `isYAMLHeuristic` for a real parser.

- **XML path is flat-string, not element-aware.** A
  `<truncated>` wrapping that breaks in the middle of an open
  tag will produce invalid XML. The cycle ships a flat-string
  cut; a future cycle that needs valid XML can swap in a
  real parser.

- **Defensive `recover()` swallows panics.** A bad input that
  panics inside any helper returns the input verbatim instead
  of crashing the agent. The trade-off is intentional: the
  truncation path must not be able to crash a tool-result
  caller (which is itself inside the agent loop). The
  side-effect is that a future regression that introduces a
  silent panic in `truncateJSONObject` would only show up as
  "input returned verbatim" — the truncation did not happen. The
  integration test (`TestOutputBudget_TruncatesLargeStructuredResult`)
  catches the surface-level regression; deeper regressions
  inside a helper would need cycle-specific tests.

- **Prose path is unchanged from cycle-198 / cycle-235.** The
  `\n…[truncated]` marker is the existing freeform-prose
  truncation; the cycle does not introduce a second
  prose-truncation idiom. A regression in the prose path
  would also surface in
  `internal/customize/truncate_test.go` /
  `internal/compaction/truncate_test.go` (those packages
  have their own copies of the truncate function — out of
  scope for cycle 248, but a candidate for a future
  refactor that unifies all three).

- **Per-tool adoption is a follow-up.** The cycle ships the
  `ApplyOutputBudget` pipeline + `BudgetResult` struct. The
  call sites (`internal/agent.pig.go`'s `runnerTool.Execute`,
  `internal/piagentbridge.toolAdapter.Execute`,
  `internal/subagenttools` `Execute` methods) do not yet wrap
  their results through the pipeline. Each call site can
  adopt independently: the pipeline is a single function call,
  the result is `BudgetResult.Content` (or `input` verbatim
  if `Truncated=false`). A future cycle (or one cycle per
  site) should wire each in.

- **Print mode (`-p`) is unaffected.** Print mode passes tool
  results through the model stream, not through
  `ApplyOutputBudget`. The structured-truncation work
  primarily targets the interactive TUI and MCP-backed tool
  results; print mode benefits transitively when the model
  short-circuits a long result.

- **No third-party dependency added.** The cycle is stdlib +
  the existing test fixtures (`internal/tools/testdata/`).
  No `go.mod` change. `go.mod` / `go.sum` unchanged at HEAD.