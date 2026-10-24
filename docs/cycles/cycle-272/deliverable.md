# cycle 272 — live streaming now renders the accumulated buffer

## TL;DR

Two defects in the mid-stream render, both found by observing the running
TUI rather than by reading tests:

1. **Row explosion.** The cycle-232/242 mid-stream pass built its "running
   buffer" by joining the rows of `m.response` — which are the *output* of
   the previous pass (glamour-padded, SGR-wrapped). At every blank-line
   boundary it fed that styled text back through glamour; glamour counted the
   trailing pad as content, word-wrapped it into extra rows and re-padded, so
   the live transcript roughly doubled in row count at each boundary. A
   22-line reply reached **216 rows** mid-stream before the turn-end replace
   collapsed it back.

2. **Unclosed fences were monochrome.** The reference renderer chromas an
   unterminated ```` ``` ```` fence; the clone only chromaed a fence after its
   closer arrived. Since the live frame renders the accumulated (possibly
   unterminated) buffer, a code block was monochrome for its entire streaming
   duration and then "popped" to colour at the closer.

After the change the live frame equals `renderMarkdown(accumulated)` — the
same function `turnDoneMsg` calls — on every chunk.

## Observation (how this was found)

`agy` cannot make live model calls in this environment (its
`loadCodeAssist` endpoint on `localhost:50998` is connection-refused and its
OAuth token is a fixture), so `agy`'s live streaming could not be captured.
Two substitute observations were used instead:

1. **A deterministic chunk-replay harness.** A temporary `cmd/wagyu` test
   drove `backendChunkMsg` line-by-line with a 22-line markdown sample and
   dumped `m.response[replyStart:]` after each chunk, in parallel with
   `renderMarkdown(prefix)` frames. That is how the 216-row explosion was
   measured (before) and reduced to ≤1 row of the ideal (after).
2. **`agy`'s stored-render path as a proxy for its renderer.** A fixture
   conversation whose last model step is an *unterminated* ```` ```go ````
   fence was written to the real store and resumed with
   `~/.local/bin/agy --conversation=<id>`. `agy` rendered the partial body
   **with chroma** (`package` `1m 38;5;166`, `main` `38;5;32`, `println`
   `38;5;166`, `"hi"` `38;5;36`). The clone rendered the same fixture as a
   monochrome 4-space indented block. That is the evidence the unclosed-fence
   fix is built on.

Live re-observation after the fix (real MiniMax M3 stream, 120×45):
90 distinct frames, coherent output, no duplication, and chroma present on
70 frames while the fence was open.

## Fix 1 — render the raw accumulated reply

`backendChunkMsg` now appends the chunk to the in-progress **session**
message first (`m.session[m.liveReplyIdx].Content`), then renders that raw
text with `renderMarkdown` and swaps the whole live range:

```go
if m.liveReplyIdx < 0 {
	m.session = append(m.session, backend.Message{Role: "assistant", Content: chunk})
	m.liveReplyIdx = len(m.session) - 1
} else {
	m.session[m.liveReplyIdx].Content += chunk
}
styled := renderMarkdown(m.session[m.liveReplyIdx].Content, m.width)
m.replaceRange(m.replyStart, len(m.response), strings.Split(styled, "\n"))
```

`renderMarkdown` is the exact function `turnDoneMsg` uses, so the live frame
is a truncated prefix of the final frame: heading chips, bullets,
blockquote glyphs, table rules and fenced chroma all appear mid-stream in the
shape they keep at turn-end, and cross-line constructs are interpreted by
goldmark in one pass over the whole buffer (an unclosed `**bold` shows
literally until it closes, then snaps to bold).

Consequences for the old machinery:

- `mdrender.LineANSIBounded`, `LineANSIBoundedWithCache`,
  `HighlightStreamingFences` and `findSafeMarkdownBoundary` are **no longer
  on the TUI hot path**. They keep their unit tests as library helpers.
- The `runMode` fields `glamourThroughIndex` (cycle 232) and `fnvCache`
  (cycle 243) were removed — there is no partial-render bookkeeping left to
  carry.
- The cycle-242 "safe boundary" precondition (a blank-line separator, and a
  prefix free of list markers / HTML / link-ref defs) is gone: the whole
  buffer is always rendered, so a blank line is no longer needed for
  cross-line constructs to style.

## Fix 2 — close an unterminated fence before the strip pass

`StripMermaidAndMath` (`internal/mdrender/glamour_preprocess_blocks.go`) now
calls a new `ensureFenceClosed(src)` first. If the number of lines starting
with ```` ``` ```` is odd, it appends a synthetic closing fence, letting
`fencedRe` match the partial block so `RestoreMermaidAndMath` chromas it.
Complete input has an even fence count and is returned unchanged, so every
turn-end render is a no-op.

## Repro

**Row accounting** (`cmd/wagyu/stream_row_accounting_test.go`): feeds the
22-line sample line-by-line and requires, after every chunk, that
`len(m.response) - m.replyStart == len(strings.Split(renderMarkdown(accumulated), "\n"))`.
Pre-272 this fails at the first blank-line boundary (3 → 6 → 11 → 23 → 48 →
216).

**Unclosed fence** (`internal/mdrender/unclosed_fence_test.go`): asserts
`StripMermaidAndMath` produces a `blockCode` placeholder for an unterminated
`go` fence, and that the strip → glamour → restore pipeline emits chroma
`38;5;166`. Includes the degenerate "opener only" frames (`"```go"`,
`"```go\n"`, `"```"`) to prove the synthetic closer neither panics nor leaks a
fence marker.

Manual proxy repro:

```bash
# write a stored reply ending in an open ```go fence, then
~/.local/bin/agy --conversation=<id>          # chroma in the partial body
/tmp/wagyu --conversation=<id>                # pre-272: monochrome
```

## Files

| File | Change |
|---|---|
| `cmd/wagyu/tui_run.go` | `backendChunkMsg` renders the raw accumulated reply; removed `glamourThroughIndex` / `fnvCache` |
| `cmd/wagyu/tui_tools_test.go` | six streaming pins updated to the new contract; `TestTUITools_PartialLastLineKeptRaw` → `...RendersLiterally` |
| `cmd/wagyu/stream_row_accounting_test.go` | new row-explosion regression |
| `internal/mdrender/glamour_preprocess_blocks.go` | `ensureFenceClosed` + call |
| `internal/mdrender/unclosed_fence_test.go` | new fence-closing pins |

## Verification

```
$ go test -count=1 ./cmd/... ./internal/...
RC=0
```

Live clone stream (real model), 120×45: 90 distinct frames, chroma in the
body from the moment the fence opener and first body line arrive (70 of the
frames carry a chroma token colour), no row duplication, mid-stream rows
already styled.

## 硬约束 / 存疑 / limitations

- **`agy`'s live streaming was never observed.** The environment cannot give
  `agy` a model. The design is therefore "the live frame converges to the
  turn-end render", which is the strongest self-consistent target, but it is
  an inference about `agy`, not a capture of it. If `agy` turns out to render
  token-by-token with its own transient shapes, this will still be closer
  than the pre-272 behaviour but not identical. 1:1 is not claimed.
- **Cost.** The whole buffer is re-rendered on every chunk: O(n) per chunk,
  O(n²) per reply. Observed chunk cadence on the real backend is low (large
  SSE deltas), so this is not an issue in practice, and no time-based
  debounce was added because a debounce would make the test-driven pins
  discard most renders. If a future backend streams token-sized deltas at
  high frequency, add a coalescing debounce (render at most every ~80 ms plus
  always on the final chunk) — the on-disk schema and the render function do
  not need to change.
- **Open-fence chroma is inferred from `agy`'s stored-render path.** The
  stored-render path and the streaming path are not guaranteed to share a
  renderer. The evidence is strong (the same glamour-chroma byte shape) but
  it is a proxy. Note also that a bare (untagged) open fence still renders
  indented/monochrome on both sides — only *tagged* fences chroma, because
  that is where the strip pass creates a `blockCode` placeholder.
- **`ensureFenceClosed` counts fence lines naively.** A line inside a fenced
  block that itself begins with ```` ``` ```` (e.g. documentation showing
  fences) can flip the parity and cause a stray synthetic closer to be
  appended. The appended closer is usually swallowed by the very block it
  closes, but the ambiguity is inherent to the existing `fencedRe` too. No
  escaping/indent handling is attempted.
- **Removed dead state is a behaviour-free cleanup.** `LineANSIBounded`
  family and `HighlightStreamingFences` remain exported and unit-tested but
  are unused in production. They were kept rather than deleted so their
  boundary-detector tests keep guarding the pure functions; if a later cycle
  wants them gone, delete both the functions and their tests together.
- **One test is flaky under full-suite load.**
  `TestRunStatusLineScript_SpawnEcho` fails intermittently when the whole
  suite runs in parallel and passes in isolation; it pre-dates this cycle and
  is unrelated (status-line subprocess spawn timing).
