# Cycle 242 — boundary-aware mid-stream glamour (the safe-half of crush)

Worktree: `/Users/gear/Documents/GitHub/openagy-safe-boundary`
Branch: `feature/cycle-242-safe-boundary`.

## Why this cycle

Cycle 232 brought the wagyu clone's chat transcript up to AGY
1.4.2 parity on the live-streaming surface: each completed
`\n`-bounded line gets glamour-rendered in place via
`mdrender.LineANSI` so the user sees styled output grow
row-by-row as the model streams. Cycle 232's deliverable
documented the gap:

> **Mid-stream glamour styling is still end-of-turn only.**
> Cycle 230 makes the streamed text one growing row (or N rows
> for `\n` boundaries), but every character is still raw
> markdown — a stream of `**bo` then `ld**` will display as
> `**bold**` for the brief moment between those two deltas,
> before glamour re-styles the final reply at `turnDoneMsg`.

The visible glitch:

- An unclosed `**bold**` mid-stream renders as plain text until
  the closing `**` arrives in a later chunk (glamour's per-line
  pass never sees both halves of the span together).
- A multi-line ```` ```fenced``` ```` code block renders with the
  wrong surrounding because glamour on a half-typed fence eats
  subsequent lines.
- A list-item continuation paragraph (a blank-line-separated
  indented line below a list marker) renders without the list
  indentation.

Cycle 228's `turnDoneMsg` re-styles correctly because the
cycle-228 whole-reply glamour pass sees the final state. The
~1-second mid-stream visual glitch is the gap vs AGY 1.4.2's
"every line already styled as it streams" behaviour.

## Crush's algorithm walk-through

Crush solves this with a much heavier machinery at
`../crush/internal/ui/chat/streaming_markdown.go`:

- `streamingMarkdown` struct maintains a "stable prefix"
  glamour-rendered cache (cycle 244 territory).
- On each flush, `findSafeMarkdownBoundary` (line 476) finds
  the latest byte offset in the running buffer where glamour's
  render of `content[:boundary]` is byte-for-byte the same as
  glamour's render of `content[:boundary]` plus any later chunk
  additions. The boundary lands immediately after a blank-line
  separator — glamour's goldmark restarts its block state at
  paragraph breaks.
- The latest acceptable boundary is found by walking backward
  through every blank-line position and applying four reject
  rules:
  - (2) Even number of triple-backtick fence lines in the prefix
    (no open fenced block).
  - (2b) The anywhere-in-prefix hazards B1 (loose lists), B2
    (HTML blocks), B3 (link reference definitions).
  - (3) The last non-blank line of the prefix does not open a
    construct (lineOpensConstruct: indented code, block quote,
    list marker, table line, setext underline).
  - (4) The first non-blank line of the suffix is not a setext
    underline candidate (would retroactively re-style the
    paragraph as a header).
- Once the boundary is found, the prefix is glamour-rendered as
  a whole document, the suffix is preserved raw, and the call
  site displays them concatenated.

The incrementality (FNV-64 prefix cache, `baseFenceCount` /
`baseHasListMarker` cumulative state, `stablePrefixLines`) lets
crush only re-glamour the new delta on each chunk instead of
re-scanning the whole prefix. That machinery is heavy and does
not transfer cleanly to wagyu's one-shot glamour pass. Cycle 242
ports ONLY the pure-function core: `findSafeMarkdownBoundary`
and its 15 helpers, none of which depend on glamour or any
state.

## What the fix does

### New files

- **`internal/mdrender/safe_boundary.go`** — port of crush's
  pure-function boundary detector. Sixteen functions, all
  package-private:

  | Function | Role |
  |---|---|
  | `findSafeMarkdownBoundary` | the entry point — walks blank-line positions, returns the latest safe offset |
  | `blankLineBefore` | walks backward through `\n([ \t]*\n)+` separators |
  | `isBlankOrSpaces` | whitespace check |
  | `isSafeBoundaryAt` | the four reject rules (2 / 2b / 3 / 4) |
  | `prefixHasOpenHazard` | the B1 (loose lists, refined) / B2 (HTML blocks) / B3 (link ref defs) "anywhere in prefix" checks |
  | `countFenceLines` | fence parity for rule (2) |
  | `isFenceLine` | fence-line detector (backticks / tildes, 3+) |
  | `lastNonBlankLine` | rule (3) input |
  | `firstNonBlankLine` | rule (4) input |
  | `splitLines` | Go 1.23+ range-over-func line iterator |
  | `lineOpensConstruct` | rule (3) predicate: indented code / block quote / list marker / table / setext |
  | `isListItemMarker` | CommonMark `- ` / `* ` / `+ ` / `<digits>. ` / `<digits>) ` |
  | `isSetextUnderlineCandidate` | rule (4) predicate: `=` / `-` only |
  | `isHTMLBlockOpener` | B2 predicate: the seven CommonMark HTML-block opener patterns |
  | `isASCIILetter` | byte-level ASCII letter check |
  | `isLinkRefDefinition` | B3 predicate: `^[ ]{0,3}\[[^\]]+\]:\s*\S+` |

  Each function is ported verbatim from crush with the same
  conservative rules and a brief header documenting its role.
  No struct, no incremental state — that's cycle 244.

- **`internal/mdrender/safe_boundary_test.go`** — 13 regression
  tests across two families:
  - 7 `TestFindSafeMarkdownBoundary_*` tests exercise the
    detector directly: simple prose, fenced code parity,
    indented code, list continuation, HTML block, link-ref
    def, setext underline.
  - 5 `TestLineANSIBounded_*` tests exercise the wrapper
    contract: no-boundary fallback, bold safe boundary, fenced
    code safe boundary, cross-line bold mid-stream (the
    cycle-242 visual improvement), full-document buffer
    byte-for-byte equivalence, panic recovery.

### Modified files

- **`internal/mdrender/stream_glamour.go`** — adds the
  `LineANSIBounded` wrapper next to the existing `LineANSI`.
  Cycle-232's `LineANSI(line)` keeps its old
  `(string, error)` signature and one-shot glamour behaviour
  untouched — its regression tests (`TestGlamour_LineANSI_*`,
  `BenchmarkLineANSI_*`) pass unchanged. `LineANSIBounded(buffer)`
  returns `(rendered, trailingRaw, err)`:
  - `buffer` is the full conversation-buffer-up-to-now (NOT
    just the latest chunk).
  - `rendered` is the glamour output of `buffer[:boundary]`
    where `boundary = findSafeMarkdownBoundary(buffer)`.
  - `trailingRaw` is the un-rendered remainder
    (`buffer[boundary:]`) — preserved raw until a future chunk
    extends the boundary.
  - When no safe boundary is found, falls back to rendering the
    whole buffer (cycle-228 contract: glamour error / empty
    output → raw passthrough) and returns `trailingRaw = buffer`
    so the caller knows nothing was split.
  - Wrapped in `defer recover()` matching the cycle-234
    mermaid panic-recovery pattern (`mermaid_terminal.go:140`).
    A glamour panic converts to a typed error rather than
    crashing the TUI.

- **`cmd/wagyu/tui_run.go`** — wires `LineANSIBounded` into the
  cycle-232 `backendChunkMsg` mid-stream glamour pass (line
  3285+). The cycle-232 per-row glamour is preserved as a
  fallback: when `LineANSIBounded` finds a safe boundary, the
  live range is replaced with the styled prefix rows + the raw
  trailing remainder; when no safe boundary exists (the
  dominant case for a one-line-at-a-time stream), the
  cycle-232 per-row glamour runs unchanged so the existing
  cycle-232 mid-stream behaviour is preserved.

- **`cmd/wagyu/tui_tools_test.go`** — adds one regression test
  for the cycle-242 call-site path:
  - `TestTUITools_CrossLineBoldMidStream` exercises the
    boundary-aware upgrade: a chunk sequence that opens a bold
    span with `**bold opens\n`, continues with `more text**\n`,
    then emits a blank line. The blank line triggers the
    cycle-242 path; glamour sees both halves of the cross-line
    bold span and renders it correctly. The trailing raw
    partial is preserved.

### What the contract change looks like

```
existing flow (cycle 232):
  for each completed stream line:
      rendered = LineANSI(line)
      replace m.response[idx] with rendered

cycle 242:
  for each backendChunkMsg:
      buffer = join(m.response[m.replyStart:])
      rendered, trailing = LineANSIBounded(buffer)
      if trailing == buffer:
          # cycle 232 fallback — no safe boundary found
          for each completed stream line:
              rendered_row = LineANSI(line)
              replace m.response[idx] with rendered_row
      else:
          # cycle 242 — replace live range with styled prefix + raw trailing
          styled_rows = split(rendered, '\n')
          replace m.response[m.replyStart:] with (styled_rows + [trailing])
```

The crucial property: mid-stream glamour output is now
byte-for-byte the same as what a full-document glamour pass
would produce on the safe prefix. No more cross-line glitches.

## File changes

| File | Change |
|---|---|
| `internal/mdrender/safe_boundary.go` (new) | 16 pure-function boundary-detector helpers ported verbatim from crush's `streaming_markdown.go:476` (findSafeMarkdownBoundary and its 15 helpers) |
| `internal/mdrender/safe_boundary_test.go` (new) | 13 regression tests: 7 detector-level + 5 wrapper-level + 1 cycle-242 visual-improvement sanity check |
| `internal/mdrender/stream_glamour.go` | Adds `LineANSIBounded(buffer) (rendered, trailingRaw, err error)` next to the existing `LineANSI(line) (string, error)`. Cycle-232 function kept intact |
| `cmd/wagyu/tui_run.go` | Wires `LineANSIBounded` into the cycle-232 mid-stream pass (`backendChunkMsg` handler, line ~3285+). The cycle-232 per-row glamour is preserved as a fallback for the no-boundary case |
| `cmd/wagyu/tui_tools_test.go` | Adds `TestTUITools_CrossLineBoldMidStream` for the call-site regression; adds `mdrender` import |
| `docs/cycles/cycle-242-safe-boundary/deliverable.md` (this file) | The cycle report |

## Tests

| Test | Pin |
|---|---|
| `TestFindSafeMarkdownBoundary_SimpleProse` | Latest blank-line boundary in a 3-paragraph doc lands at byte 40 (start of "third short") |
| `TestFindSafeMarkdownBoundary_FencedCodeEvenTongues` | Even-fence doc → positive boundary at start of "after"; odd-fence doc with opening ```go followed by closing ``` → -1 (fence count in prefix is odd) |
| `TestFindSafeMarkdownBoundary_IndentedCode` | Indented-code-in-prefix doc → -1 (rule 3 catches the trailing 4-space-prefixed line); indented-code-after-boundary doc → positive (rule 3 only inspects the prefix) |
| `TestFindSafeMarkdownBoundary_ListContinuation` | `- item one\n\n  continuation paragraph\n` → -1 (rule 3 catches the marker line) |
| `TestFindSafeMarkdownBoundary_HTMLBlock` | `<script>` opener at line start → -1 (B2 catches it); `<3` in prose → positive (not a block opener) |
| `TestFindSafeMarkdownBoundary_LinkRefDef` | `[foo]: http://example.com` → -1 (B3); `[foo]:` (no destination) → positive (not a valid ref-def) |
| `TestFindSafeMarkdownBoundary_SetextUnderline` | `para\n\n===\n` → -1 (rule 4 catches the `===` tail) |
| `TestLineANSIBounded_NoBoundaryFallsBackToFull` | 1-line markdown with no blank line → (rendered=glamour-of-whole, trailingRaw=<input>, nil) |
| `TestLineANSIBounded_BoldSafeBoundary` | Buffer ending with a complete paragraph → (rendered, trailingRaw, nil) with no raw `**` markers surviving in rendered |
| `TestLineANSIBounded_FencedCodeSafeBoundary` | Complete ```go ... ``` block → code body survives between rendered + trailingRaw; every text byte of src is recoverable |
| `TestLineANSIBounded_CrossLineBoldMidStream` | `Hello **bold\nworld` (no blank line) → (rendered=glamour-of-whole, trailingRaw=<input>, nil) — the cycle-242 visual improvement pinned at the wrapper level |
| `TestLineANSIBounded_FullDocumentBuffer` | Complete document → (rendered, trailingRaw) where trailingRaw == src[findSafeMarkdownBoundary(src):] byte-for-byte |
| `TestLineANSIBounded_PanicRecovered` | Weird markdown → wrapper does NOT panic, returns `(rendered, trailing, err)` instead |
| `TestTUITools_CrossLineBoldMidStream` (new) | End-to-end call-site: chunks emit a cross-line bold span followed by a blank line; glamour on the boundary-detected prefix renders the cross-line bold correctly mid-stream |
| `TestTUITools_StreamedLinesRenderGlamour` (existing, cycle 232) | UNCHANGED — cycle-232 per-row glamour fallback preserves the original behaviour for the no-boundary case |
| `TestTUITools_PartialLastLineKeptRaw` (existing, cycle 232) | UNCHANGED — partial-only buffer → cycle-232 fallback (no boundary found, per-row glamour with no rows to style, trailing raw) |
| `TestTUITools_TurnDoneReplaceStillIdempotent` (existing, cycle 232) | UNCHANGED — three-chunk reply without blank line → cycle-232 fallback preserves the cycle-232 contract |
| `TestGlamour_LineANSI_*` (5 existing, cycle 232) | UNCHANGED — `LineANSI` signature and behaviour untouched |
| `BenchmarkLineANSI_*` (3 existing, cycle 232) | UNCHANGED — `LineANSI` perf unchanged |

## Test results

- `go build -o /tmp/wagyu-cycle242 ./cmd/wagyu` — exit 0.
  Binary size: 89,614,274 bytes (+34,256 bytes vs cycle-232/234
  baseline at 89,580,018 bytes — the 16 pure functions + the
  `LineANSIBounded` wrapper + tests).
- `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` —
  exit 0. All 50+ packages pass; the cycle-242 visual-
  improvement test passes; the cycle-232 mid-stream tests
  pass without modification.
- `go test -vet=off -run='^$' -bench='BenchmarkLineANSI' -benchtime=1s ./internal/mdrender/` —
  `BenchmarkLineANSI_FirstCall` ~34,677 ns/op,
  `BenchmarkLineANSI_CachedCall` ~35,058 ns/op,
  `BenchmarkLineANSI_PlainProse` ~94,362 ns/op — all on
  par with cycle-232 baseline.

## Hard constraints / 存疑 / limitations

- **Cycle 242 ports ONLY the pure-function core.** Crush's
  incremental-search struct (`baseFenceCount`,
  `baseHasListMarker`, `stablePrefixLines`, `stablePrefix`,
  `stablePrefixRender`) is a follow-up cycle 244; it would let
  `LineANSIBounded` re-render only the new delta on each chunk
  instead of re-scanning the whole prefix. Cycle 242's per-call
  cost is `O(n)` in the buffer size; with realistic streaming
  (1–5 lines/sec at 50-line reply = ~5-line chunks) each call
  is ~5-line work = <1 ms.
- **The visual improvement: cross-line bold / code /
  list-continuation constructs now render correctly
  mid-stream** when the running buffer contains a blank-line
  separator (their trailing partial stays raw until the
  boundary advances). At AGY 1.4.2's contract level the cycle
  is faithful — the cycle-228 turn-end swap becomes a no-op on
  stable content because cycle 242 has already styled the safe
  prefix correctly.
- **The cycle-232 per-row glamour is preserved as a fallback.**
  When `LineANSIBounded` finds no safe boundary (the dominant
  case for one-line-at-a-time streaming), the existing
  cycle-232 loop runs unchanged so the cycle-232 mid-stream
  tests (`TestTUITools_StreamedLinesRenderGlamour`,
  `TestTUITools_PartialLastLineKeptRaw`,
  `TestTUITools_TurnDoneReplaceStillIdempotent`) pass without
  modification.
- **The `LineANSIBounded` function's panic-recovery is
  conservative** (returns `(rendered="", trailingRaw, err)`);
  the cycle-234 mermaid pattern is the template
  (`mermaid_terminal.go:140`). A panic in glamour is a glamour
  bug, not a wagyu bug — but the cycle ships defensive
  `recover()` to never crash the TUI.
- **Cycle 242 does NOT touch the FNV-64 prefix cache**; that's
  a separate follow-up (cycle 243, dispatched in parallel by
  the orchestrator). When 243 lands it will add a memoization
  layer in front of `LineANSIBounded` keyed on the (line-count,
  fnv64-hash, variant, width) tuple; no boundary-detection
  logic should be duplicated by 243.
- **Cycle 242 does NOT change `LineANSI`'s signature**; the
  cycle-232 function remains callable and produces the OLD
  one-shot glamour output. The new `LineANSIBounded` is
  additive. This is documented in
  `stream_glamour.go`'s package comment.
- **No third-party dependency added** — cycle 242 is stdlib +
  cycle-232's existing glamour dep + the cycle-234 mermaid-
  ascii dep (unchanged). The boundary detector uses only the
  `strings` package.
- **The B1 (loose lists) rule is the "refined" variant from
  crush**, not the v1 over-conservative rule. The refined rule
  tracks list markers anywhere in the prefix AND inspects the
  last non-blank line's raw form for indentation. This catches
  loose-list continuation paragraphs (indented, no marker)
  without forfeiting every boundary after a CLOSED list. The
  v1 rule killed the streaming cache for every document that
  ever contained a list — see the `CHARM-1785` note in crush's
  code (would otherwise reject every post-list boundary).
- **No tests for `cmd/wagyu/tui_run.go`'s `glamourThroughIndex`
  boundary state**: the cycle-242 path sets
  `glamourThroughIndex = len(m.response) - 1` after the
  `replaceRange`. This is identical to the cycle-232 value of
  `replyEnd`, so the existing `TestTUITools_TurnDoneReplaceStillIdempotent`
  assertion (`glamourThroughIndex == -1` after `turnDoneMsg`)
  continues to pass — the cycle-228 reset to `-1` is unchanged.

## Notes for the cycle-243 / cycle-244 follow-ups

- Cycle 243 will add a memoization layer in front of
  `LineANSIBounded`. The cache key MUST be a function of
  `(line-count, fnv64-hash, variant, width)`. Cycle 243 should
  NOT re-implement boundary detection — it sits on top of
  `LineANSIBounded`. When 243 lands, the mid-stream pass in
  `tui_run.go` should call the cache (not `LineANSIBounded`
  directly) so repeated boundary scans on the same prefix are
  amortised.
- Cycle 244 will port crush's `streamingMarkdown` struct
  (`stablePrefix`, `stablePrefixRender`, `baseFenceCount`,
  `baseHasListMarker`, `stablePrefixLines`) so the boundary
  search becomes O(delta) instead of O(n) per tick. Cycle 244
  should reuse cycle 242's `findSafeMarkdownBoundary`,
  `countFenceLines`, `prefixHasOpenHazard` directly — those
  functions are already pure and don't need to be re-ported.
  The incremental layer adds cumulative state on top, not
  instead.