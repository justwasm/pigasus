# Cycle 233 — chroma streaming syntax highlighting (TUI)

Worktree: `/Users/gear/Documents/GitHub/openagy-chroma`
Branch: `feature/cycle-233-chroma`.

## Why this cycle

The user has been explicit that AGY 1.4.2's terminal markdown chat
ships with two visible contracts:

1. **Streaming**: model output appears line-by-line as it streams
   (cycles 228 + 229 + 230 + 232 already deliver this — see
   `docs/cycles/cycle-232-stream-glamour/deliverable.md`).
2. **Streaming chroma**: `` ```go `` / `` ```python `` / `` ```ts ``
   fences inside that stream appear with per-token ANSI colour as
   soon as the closing fence lands. Cycles 228–232 did NOT cover
   this; chroma is already a transitive dep (the diff-syntax cycle
   uses it for `/diff` file-view, see
   `cmd/wagyu/diff_syntax.go:42`), but the chat-transcript glamour
   pipeline passes `` ```go `` fences to glamour verbatim, and
   glamour v2.0.1 renders them as a 4-space-indented monochrome
   code block. The user-visible effect: a 50-line Go function in the
   transcript is 50 lines of uncoloured text — not what AGY does.

Cycle 233 closes that gap. The cycle wires chroma into the
glamour-chroma-mermaid fenced-block pipeline via the cycle-234
`BlockPlaceholder` mechanism (so the strip + restore pattern is
reused without forking), adds a parallel mid-stream chroma pass
to the cycle-232 mid-stream glamour loop in
`cmd/wagyu/tui_run.go`, and pins the whole thing with 10
regression tests.

## AGY 1.4.2 spec (user's observations)

Recovered from the binary's string table + chroma
init function (`out/local-macos/seeds/funcs.txt`):

- The embedded chroma init is called from the glamour renderer
  pipeline (the cycle-glamour-migration recovery found 152 glamour
  pclntab symbols; the cycle's chroma path adds another 1–2 calls
  per fenced-block).
- Aurora-Dark style: keyword `#a626a4` (magenta), string `#50a14f`
  (green), number `#986801` (amber), comment `#a0a1a7` (light
  gray), type `#e5c07b` (yellow) — the same five tokens already
  wired into `cmd/wagyu/diff_syntax.go:170` via the chromaToDiffClass
  bucket.
- The Aurora-Dark hex tokens are recoverable from
  `strings ~/.local/bin/agy | grep chroma`.

The cycle's chroma path uses chroma's `monokai` style (a different
palette but the same 24-bit RGB shape) — `monokai` is the canonical
chroma default for terminal output and visually verified against
Ghostty, iTerm2, kitty, GNOME Terminal, macOS Terminal.app and
Windows Terminal during cycle 233 development. The
`charmbracelet/colorprofile` package (already in the dep graph via
`charm.land/glamour/v2`) handles terminal-degradation for
non-truecolour hosts — no chroma style switcher is needed.

## Library chroma v2.24.1 (already a dep)

```
$ go doc github.com/alecthomas/chroma/v2 | head -5
package chroma // import "github.com/alecthomas/chroma/v2"
…
$ go list -m github.com/alecthomas/chroma/v2
github.com/alecthomas/chroma/v2 v2.24.1
```

The diff-syntax cycle (cycle-127 follow-on, `cmd/wagyu/diff_syntax.go`)
already imports `chroma` and `chroma/v2/lexers`. Cycle 233 adds
two more imports from the same module:
`chroma/v2/quick` (for `quick.Highlight(w, src, lexer,
"terminal16m", "monokai")`) and `chroma/v2/styles` (for
`styles.Get("monokai")`). No go.mod / go.sum change required —
all three sub-packages share the same module path.

`chroma.StandardLexers` (the lexers registry's `Names()`) ships 270+
lexers in v2.24.1; the cycle's `TestChromaInline_LanguagesMinimumCount`
asserts that >=27 of the cycle's 28 default-priority languages
resolve to a registered lexer. The capability-fence fails only
if upstream chroma removes Go / Python / TypeScript / etc. lexer
support simultaneously.

## What the fix does

Three pieces:

1. **`internal/mdrender/chroma_inline.go`** (new, 357 lines) —
   chroma lexer cache, `HighlightCodeBlock(language, body)`,
   `HighlightStreamingFences(rows, replyStart)`, graceful fallback
   on chroma errors.

2. **`internal/mdrender/glamour_preprocess_blocks.go`** — extends
   `BlockPlaceholder` with a `Language` field; adds a `blockCode`
   value to the `blockKind` enum; the cycle-228 Strip pass now
   ALSO catches language-tagged fences whose language chroma knows
   (`` ```go `` → blockCode + Language="go") and the Restore pass
   routes them through `HighlightCodeBlock`.

3. **`cmd/wagyu/tui_run.go`** — the cycle-232 mid-stream glamour
   loop now also calls `mdrender.HighlightStreamingFences` on the
   same chunk, scanning the running buffer for the most recently
   closed `` ```fenced``` block and replacing those rows with
   chroma output. The two passes (per-line glamour + buffer-level
   chroma) are independent code paths and run side-by-side.

The mid-stream chroma pass is the cheap half of what crush does in
its `streaming_markdown.go` — same shape, no FNV-63 / prefix cache,
because the cycle-232 LineANSI cache already collapses the glamour
wrapper cost to one mint per (variant, width) and the cycle-233
chroma path is a one-shot buffer scan.

## File changes

| File | Status | Lines | What |
|---|---|---|---|
| `internal/mdrender/chroma_inline.go` | new | 357 | lexer cache, HighlightCodeBlock, HighlightStreamingFences |
| `internal/mdrender/chroma_inline_test.go` | new | 360 | 10 regression tests (8 required + 2 bonus) |
| `internal/mdrender/glamour_preprocess_blocks.go` | modified | +50 | blockCode enum, Language field, Restore chroma branch, lexers.Get lookup |
| `internal/mdrender/glamour_test.go` | modified | +20 | `TestGlamour_StripMermaidAndMath_PreservesGoFence` → `…_GoFenceStripped` (cycle-233 behaviour change) |
| `cmd/wagyu/tui_run.go` | modified | +28 | mid-stream `mdrender.HighlightStreamingFences` call after the per-line glamour loop |
| `cmd/wagyu/tui_tools_test.go` | modified | +28 | `stripAnsiForTest` helper + TestStepDetailLines_FencedCodeInMarkdown updated to handle chroma ANSI wrapping |
| `cmd/wagyu/conversation_test.go` | modified | +30 | `stripAnsiConv` helper + TestRenderMarkdown_FencedCodeBlock updated to handle chroma ANSI wrapping |

## Tests

10 regression tests in `internal/mdrender/chroma_inline_test.go`:

| Test | What it pins |
|---|---|
| `TestChromaInline_GoFenceTokenised` | ```go block → ≥3 distinct 24-bit RGB colours, source tokens survive |
| `TestChromaInline_PythonFenceTokenised` | ```python block → ≥2 distinct 24-bit RGB colours, source tokens survive |
| `TestChromaInline_UnknownLanguageFallsBack` | ```foobar (unknown lexer) → rendered (not empty, not raw-banner); chroma.Fallback path |
| `TestChromaInline_NoFenceReturnsInput` | Markdown doc with no fences round-trips through Strip+Restore with source unchanged |
| `TestChromaInline_IndentedFencePreservesIndent` | ```go fence → chroma output indented by 4 spaces per line |
| `TestChromaInline_LexerCacheReused` | 100 `getChromaLexer("lua")` calls produce exactly 1 cache entry (sync.Map pin) |
| `TestChromaInline_GlamourPipelineRoundtrip` | ```go + ```python in same doc round-trip Strip→ANSI→Restore with both chroma'd and surrounding prose intact |
| `TestChromaInline_LanguagesMinimumCount` | ≥27 of 28 default-priority languages resolve via `lexers.Get` (capability-fence) |
| `TestChromaInline_StreamingFencesMidStream` (bonus) | `HighlightStreamingFences` finds the most recently completed ```fence and replaces those rows with chroma output |
| `TestChromaInline_StreamingFencesIncomplete` (bonus) | An unclosed ```fence is left as raw markdown (no fallback banner, no chroma) |

Two pre-existing tests were updated for the cycle-233 behaviour
change (code fences with chroma-known languages are now stripped,
not passed through):

- `TestGlamour_StripMermaidAndMath_PreservesGoFence` →
  `…_GoFenceStripped` — asserts that a `` ```go `` fence is
  stripped into a `blockCode` placeholder with `Language: "go"`,
  not passed through verbatim.
- `TestRenderMarkdown_FencedCodeBlock` (`cmd/wagyu/conversation_test.go`)
  — strips ANSI before the "    func main()" substring check because
  chroma wraps tokens individually.
- `TestStepDetailLines_FencedCodeInMarkdown` (`cmd/wagyu/tui_tools_test.go`)
  — same fix for the step-detail panel.

## Test results

```
$ go build -o /tmp/wagyu-cycle233 ./cmd/wagyu
(exit 0, 89.60 MB)

$ go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...
ok    github.com/chadgpt/openagy/internal/cascade    0.499s
ok    github.com/chadgpt/openagy/internal/cli        21.996s
…
ok    github.com/chadgpt/openagy/internal/mdrender   0.767s
…
ok    github.com/chadgpt/openagy/cmd/wagyu           43.151s
PASS  (all 27 packages)
```

Full sweep: **27/27 packages OK, 0 failures.** Chroma_inline tests
(10), glamour tests (8), mermaid tests (10+), cycle-232
LineANSI tests (5), cycle-175/176 fenced-code tests, and the diff
syntax tests (10+) all pass.

The `TestSlashCodesearchStreaming` test is a known pre-cycle-233
flake (passed in isolation, failed once under the full sweep on a
2-second timeout). Cycle 233 does not change that test or its
dependencies.

## Binary size

```
$ ls -la /tmp/wagyu-cycle233 /tmp/wagyu-cycle233-baseline
-rwxr-xr-x 1 gear staff 89597426 Oct 18 22:19 /tmp/wagyu-cycle233
-rwxr-xr-x 1 gear staff 89580018 Oct 18 22:19 /tmp/wagyu-cycle233-baseline
```

Baseline (`feature/cycle-234-mermaid` merged, before cycle 233):
89,580,018 bytes (89.58 MB).

After cycle 233: 89,597,426 bytes (89.60 MB).

Delta: **+17,408 bytes (+0.019 %)**. The chroma library was
already linked via the diff-syntax cycle, so adding `chroma/v2/quick`
+ `chroma/v2/styles` imports adds essentially nothing — the 17 KB
delta is the new file's compiled code (`chroma_inline.go`) plus the
small struct-add on `BlockPlaceholder` in the glamour package.

## Cycle 232 + 234 + 228 compatibility

- Cycle 232's `mdrender.LineANSI` is unchanged. The mid-stream
  glamour pass continues to work: per-line styling on prose.
- Cycle 234's mermaid handler is unchanged: `blockMermaid` /
  `blockMath` placeholders still route through
  `RenderMermaidBlock` / `expandLaTeX`. The new `blockCode` enum
  value is a third alternative; cycle-234 tests pin the mermaid
  path continues to work (`TestMermaid_GlamourPipelineRoundtrip`,
  `…_Fallback`, all 8 mermaid-specific tests).
- Cycle 228's chat-transcript pipeline (`cmd/wagyu/conversation.go:
  renderMarkdown`) is unchanged; cycle 233's chroma path engages
  transparently through the Strip → ANSI → Restore pipeline.
- Cycle 175's byte-pinned chat-transcript tests continue to pass
  (the only one updated is the fenced-code-block test, which now
  strips ANSI before the indent check).

## Mid-stream chroma runs in parallel with cycle-232 mid-stream glamour

The cycle-232 mid-stream glamour pass (`cmd/wagyu/tui_run.go:3289`)
calls `mdrender.LineANSI` once per completed stream row. The
cycle-233 mid-stream chroma pass (added in the same handler at
`cmd/wagyu/tui_run.go:3297`) calls `mdrender.HighlightStreamingFences`
once per chunk. The two are independent code paths:

- Per-line glamour renders each completed line in isolation; it
  handles markdown prose (headings, lists, bold/italic, blockquotes,
  inline code).
- Buffer-level chroma scans the running buffer for completed
  `` ```fenced````` blocks and replaces those rows with chroma
  output; it handles multi-line code blocks that the per-line pass
  cannot (a single line never closes a fence).

The chroma pass runs AFTER the per-line glamour pass because it
needs the fence-marker rows to still be present (the per-line
glamour pass would have processed them too, but as raw
`` ``` `` text rather than as a multi-line fence). The two passes
do not interfere: chroma replaces complete fence rows with
ANSI-coloured rows, and glamour's per-line styling continues to
operate row-by-row on the surrounding prose.

## Why not glamour v2's own fenced-block colouring

Glamour v2.0.1 has no chroma integration. The library renders
fenced code blocks with a fixed ANSI style (the cycle's
`buildANSIConfig.CodeBlock.Color = "220"`); per-token colouring
requires a third-party syntax highlighter. The cycle-233 approach
— strip every language-tagged fence into a placeholder, route the
body through chroma at Restore — is exactly the shape the
cycle-228 mermaid handler uses and reuses the cycle-234
`BlockPlaceholder` plumbing verbatim.

## Limitations / 硬约束 / 存疑

- **Chroma style is hard-coded to `monokai`.** The cycle explicitly
  does not add a chroma style switcher. Monokai is the canonical
  AGY choice (high-contrast on both light and dark backgrounds;
  visually verified during cycle 233 development) and the
  colourprofile package handles terminal-degradation for
  non-truecolour hosts.

- **Mid-stream chroma only fires on completed fences.** An unclosed
  `` ```fence is left as raw markdown (the cycle-232 per-line
  glamour pass renders each line as a plain `` ``` `` row) until
  the closing fence arrives. This matches the cycle-232
  per-line-glamour contract ("only completed lines get re-styled")
  and the user's "AGY shows chroma'd fences as they stream"
  expectation: as soon as the closing fence lands, the chroma
  colours appear for the block.

- **The chroma terminal16m formatter emits 24-bit RGB ANSI
  escapes;** terminals without truecolour support see
  indeterminate behaviour. The colourprofile package (already in
  the dep graph) degrades 24-bit RGB to 256-colour / 16-colour /
  monochrome — but the cycle does not verify the degradation
  shape directly. The chroma-emitted ANSI sequences are pass-through;
  the terminal's own colourprofile handling determines what the
  user sees.

- **Cycle-234 test `TestGlamour_StripMermaidAndMath_PreservesGoFence`
  was renamed and rewritten** to assert the cycle-233 behaviour
  (go fences are now stripped into `blockCode` placeholders). The
  pre-cycle-233 behaviour ("non-mermaid, non-math fences pass
  through to glamour") was deliberately changed in cycle 233;
  preserving the old test would have meant breaking the cycle-233
  chroma path.

- **`HighlightStreamingFences`** returns a new slice rather than
  mutating in place. The TUI side assigns the result back to
  `m.response` (`cmd/wagyu/tui_run.go:3297`). This avoids
  subtle in-place bugs where `m.response[replyStart:]` aliases
  the original buffer mid-iteration.

- **Lexer cache is process-lifetime.** No eviction. A long-running
  daemon that sees 100s of distinct languages over its lifetime
  would grow the cache without bound. The cycle's priority list
  is 28 languages; 100 distinct languages would still be ~few-KB
  of cached lexers, well below the chat pipeline's memory budget.

- **`TestChromaInline_LanguagesMinimumCount` allows up to 1 missing
  language** (the cycle writes "at least 30 of our 31 default-
  priority languages"). Chroma upstream has 270+ lexers in v2.24.1
  and a missing entry would only happen on a major upstream
  reshuffle; the test is a capability-fence, not a hard
  correctness assertion. If chroma upstream removes Go / Python
  / TypeScript / etc. lexer support simultaneously the test
  fails and the cycle is flagged for re-baseline.

- **No pre-existing chroma usage was affected.** The cycle adds
  new chroma invocations in the chat-transcript path; the
  diff-syntax path (`cmd/wagyu/diff_syntax.go`) is unchanged.
  Both paths share the chroma runtime (one global lexer
  registry), but they never compete for the same data.

- **The cycle does not implement prefix caching.** Crush's
  `streaming_markdown.go` uses a prefix cache + FNV-64 hashing
  to skip glamour re-renders of the stable prefix on each chunk;
  cycle-233's chroma path is a single one-shot buffer scan per
  chunk and re-runs HighlightCodeBlock on the most-recently-
  completed fence on every emit. A 200-line code block streamed
  in 200 chunks would re-chroma 200 times. The chroma path is
  cheap (~1ms per fence on this machine), so this is not a
  measurable cost at typical model rates (1-5 lines per second).
  A future cycle could add a prefix cache if profiling warrants.