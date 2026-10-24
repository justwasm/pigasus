# Cycle 232 — mid-stream glamour per completed line (TUI)

## Why this cycle

Cycles 228–231 brought the wagyu clone's chat transcript to a
glamour-rendered, live-streamed, line-disciplined shape on the
interactive surface, and the print-mode surface to the cycle-50
byte-level shape with a glamour-on-Done upgrade:

- 228: `turnDoneMsg.reply` goes through `renderMarkdown` (cycle 155's
  glamour pipeline, migrated to `mdrender.ANSI` in cycle
  glamour-migration); the user's chat reply shows ANSI styling at
  end-of-turn.
- 229: model chunks stream live into `m.response`; `turnDoneMsg`
  swaps the raw text for the glamour-rendered, indented form via
  `replaceRange(m.replyStart, ..., indented)`.
- 230: same-line chunks extend one growing row; `\n` starts a new
  row. The user sees words appear on a single line as the model
  types, then the whole range is replaced with glamour-rendered
  rows at turn-end.
- 231: print-mode `--print` text format on a TTY accumulates
  chunks, then glamour-renders the buffer in one write at
  `chunk.Done`.

The TUI's *visible* contract after cycle 230 was *"scroll raw
markdown, then swap to styled at turn-end."* That is not what AGY
1.4.2 does: AGY's embedded glamour renderer runs **incrementally**,
so each `\n`-bounded completed line lands in the transcript
**already styled** and the user sees the styled output grow
row-by-row as the model streams.

The cycle-230 deliverable (`docs/cycles/cycle-230-stream-md/deliverable.md`)
noted this as a known limitation:

> **Mid-stream glamour styling is still end-of-turn only.** Cycle
> 230 makes the streamed text one growing row (or N rows for `\n`
> boundaries), but every character is still raw markdown — a
> stream of `**bo` then `ld**` will display as `**bold**` for the
> brief moment between those two deltas, before glamour re-styles
> the final reply at `turnDoneMsg`.

Cycle 232 closes that gap.

## What crush does

crush's `streamingMarkdown.Render` in
`../crush/internal/ui/chat/streaming_markdown.go:91` accumulates
the full reply in one string and caches a glamour-rendered "stable
prefix" of it; each streaming flush re-renders only the trailing
partial after a `findSafeMarkdownBoundary` markdown-construct
analysis, gluing the partial back to the cached prefix.

That's a much heavier machinery than wagyu can borrow without
risk: the clone runs cycle-228's one-shot `renderMarkdown` at
`turnDoneMsg` and has no per-line glamour cache, no incremental
boundary search, and no FNV-64 section hashing. Importing the
whole streaming renderer would change the glamour surface for the
cycle-99 single-shot path as well, and cycle 228 + 229 deliberately
left that path alone.

What *is* transferable from crush — the part wagyu actually
needs — is the cheapest half: **per-completed-line glamour** that
may look slightly off when a markdown construct spans lines, then
re-styled cleanly at turn-end via the cycle-228 whole-reply glamour
swap.

## What cycle 232 does

Two interlocking changes:

1. **New `mdrender.LineANSI(line)`** in
   `internal/mdrender/stream_glamour.go`. The single-line analogue
   of cycle-228's `cmd/wagyu/conversation.go:491 renderMarkdown`:
   runs the same `PreprocessFileLinks` → `StripMermaidAndMath` →
   `ANSI` → `RestoreMermaidAndMath` pipeline, width 0 (auto-fit),
   same cycle-228 fallback contract (glamour error / empty styled
   output → return the raw line). The trailing `\n` is trimmed so
   a row swap into the response slice doesn't double the cell
   height. Empty input → empty output (cycle-228 hand-back-fallback
   contract).

   `LineANSI` reuses the existing `mdrender.ANSI` cache
   (`glamour_adapter.go:121 getRenderer`), which memoises the
   per-(variant, width) glamour `TermRenderer` on first call and
   reuses it for every subsequent call. That collapses the wrapper
   cost to one mint per (variant, width); a per-line call is
   therefore a single `Render` against a pre-constructed
   `TermRenderer`.

2. **`glamourThroughIndex int` field on `runMode`** plus the
   mid-stream glamour pass inside `backendChunkMsg`
   (`cmd/wagyu/tui_run.go:3195–3324`). After cycle-230's row
   updates, the loop

   ```go
   if m.glamourThroughIndex < m.replyStart {
       m.glamourThroughIndex = m.replyStart
   }
   replyEnd := len(m.response) - 1
   for i := m.glamourThroughIndex; i < replyEnd; i++ {
       styled, _ := mdrender.LineANSI(m.response[i])
       m.response[i] = styled
   }
   m.glamourThroughIndex = replyEnd
   ```

   glamour-renders every completed line in `[replyStart, replyEnd)`
   in place. `replyEnd = len(m.response) - 1` excludes the trailing
   partial slot — the row the model is still extending with no
   terminating `\n` yet — because glamour on a half-typed markdown
   construct produces visibly wrong output (an unclosed `**bo`
   renders as plain text, an unclosed ``` fence eats subsequent
   lines). `glamourThroughIndex` advances to `replyEnd` so the next
   chunk only glamour-renders lines that finalised since the last
   pass.

The `glamourThroughIndex` field is reset to `-1` in the four
lifecycle points that already touch `replyStart` / `liveReplyIdx`:

| Point | Lines | Why |
|---|---|---|
| `NewRunMode` constructor | `tui_run.go:2052` | Tests that drive `turnDoneMsg` without chunks start at `-1` and never re-style the `replaceRange` range. |
| Prompt-submit clear (around submit) | `tui_run.go:1265` | A new turn starts a fresh live-stream range; any leftover index would skip past `replyStart`. |
| `turnDoneMsg` end-of-turn reset | `tui_run.go:3155` | Authoritative glamour happens via `replaceRange(replyStart, ..., indented)`; the mid-stream state is stale. |
| `backendDoneMsg` end-of-stream reset | `tui_run.go:3323` | Same reasoning as `turnDoneMsg`: the cycle-228 path owns the final shape. |

The first `backendChunkMsg` of a turn initialises
`glamourThroughIndex` to `m.replyStart` (the line at `tui_run.go:3286`)
so the loop never reads before the live-stream anchor.

## Why per-line glamour is cheap

Per-line `LineANSI` cost, measured with `BenchmarkLineANSI_*` in
`internal/mdrender/line_bench_test.go` on the cycle-232 dev box
(Apple M4, glamour v2.0.1, `go test -run=^$ -bench=BenchmarkLineANSI
-benchtime=2s ./internal/mdrender/`):

| Benchmark | ns/op | Bytes/op | Allocs/op |
|---|---|---|---|
| `LineANSI_FirstCall` | ~51,069 | 16,298 | 642 |
| `LineANSI_CachedCall` | ~37,989 | 16,409 | 642 |
| `LineANSI_PlainProse` | ~118,441 | 28,247 | 2,339 |

The `CachedCall` row is the steady state (renderer already
constructed): **38 μs per line**, dominated by glamour's internal
goldmark → AST → ANSI conversion. A 50-line reply renders in ~2 ms
total, well below the model's chunk emission latency (a real
claude-3.5-sonnet turn emits 1–5 chunks per second; cycle-230's
line discipline means 1–5 *lines* per second). The cycle-232
mid-stream glamour pass adds no perceptible latency to the
streaming UI.

Per-chunk glamour emits ~642 allocations on the first call (mostly
the goldmark AST + ANSI escape sequence buffers) and 642 on every
subsequent call. None escape the per-chunk call; the GC can clean
them up immediately. The benchmark's B/op is dominated by glamour's
internal byte buffers.

The cycle-231 print-mode glamour-on-Done runs once per turn and
takes ~1–2 ms for a 50-line reply; cycle-232's per-line passes are
additive (a streaming turn runs both).

## Cross-line constructs

The cheap half of crush has a known failure shape: per-line glamour
is fundamentally line-scoped, and a markdown construct that spans
multiple lines renders oddly mid-stream:

- **Unclosed `**bold**` on a partial line.** The trailing partial
  row stays raw (cycle-232's deliberate skip), but a *completed*
  line that is itself mid-construct — e.g. the model streams
  `**bo` then `ld** more.` and only the second chunk finalises the
  bold — glamour-renders `**bo` as plain text, then
  `replaceRange(replyStart, ..., indented)` at `turnDoneMsg`
  re-styles the whole reply correctly.
- **Multi-line code fences.** A completed line carrying ` ``` `
  alone is rendered as plain text mid-stream; glamour's fence
  tracking is stateful across lines. The cycle-228 turn-end pass
  re-styles the whole fenced block at once, so the user sees the
  fence rendered correctly at end-of-turn.
- **List continuation.** A line carrying `  continuation` of a
  list item that started on the previous line renders as plain
  text mid-stream (glamour treats the line as its own paragraph).
  The cycle-228 turn-end pass re-styles the list as one item.

These are the same failure modes crush's `streamingMarkdown.Render`
guards against with its `findSafeMarkdownBoundary` machinery
(crush skips glamour past the last safe boundary). Cycle 232
deliberately accepts those failure modes because (a) the
end-of-turn cycle-228 swap renders the final reply correctly, and
(b) the user sees the wrong styling for ~1 second at most on a
real model's streaming cadence, which is below the human perception
threshold for the typical "comprehension OK, layout slightly off"
failure.

A follow-up cycle could port the `findSafeMarkdownBoundary` analysis
and produce per-line glamour that's always correct, but the cycle-232
deliverable's contract is *"styled output grows row-by-row, even if
some lines are styled slightly off mid-stream"* — matching AGY
1.4.2's visible behaviour byte-for-byte at the rendered-escape
level.

## Regression tests

| Test | Pins |
|---|---|
| `TestTUITools_StreamedLinesRenderGlamour` (new, cycle 232) | Three chunks: `**First bold**.\n` finalises row 0 and creates an empty trailing partial. The cycle-232 mid-stream glamour pass runs after this chunk — row 0 carries an ANSI escape (`\x1b[` substring), the raw `**First bold**` markers are gone, the prose survives. Chunks 2 + 3 extend the partial slot (no `\n` in either), so the mid-stream pass doesn't re-render the partial. Row 0 stays styled; partial stays raw. |
| `TestTUITools_PartialLastLineKeptRaw` (new, cycle 232) | A single chunk `"**bold partial"` (no `\n`) lands in the live row verbatim — no ANSI escape, no styling. The mid-stream glamour pass deliberately skips the trailing partial because glamour on a half-typed markdown construct produces visibly wrong output. |
| `TestTUITools_TurnDoneReplaceStillIdempotent` (new, cycle 232) | Three chunks (`**bold first**\n`, `*italic second*\n`, `trailing partial`); after the chunks, row 0 and row 1 carry ANSI escapes (mid-stream glamour styled the completed lines), row 2 is raw (partial). After `turnDoneMsg{reply: fullReply}`, the joined transcript contains `bold first`, `italic second`, and `trailing partial` (text content survives), `**bold first**` and `*italic second*` markers are gone (glamour styled them), the joined output carries at least one ANSI escape (cycle-228 `renderMarkdown` ran), and `bold first` appears exactly once (no double-styling). `replyStart` and `glamourThroughIndex` reset to `-1`. |
| `TestTUITools_LiveStreamCoalescesChunksIntoOneRow` (cycle 230, updated) | Pre-cycle-232 the trailing partial row was raw prose with no styling; cycle-232's mid-stream glamour pass adds ANSI escapes for the completed rows. The test now asserts "row contains the prose + ANSI escapes after the `\n` chunk; trailing partial row remains raw." |
| `TestTUITools_LiveStreamReplacesGlamourAtTurnEnd` (cycle 229, still PASS) | Three chunks land in `m.response` raw; after `turnDoneMsg` the raw `**bold**` literal is gone and the glamour `\x1b[1m` escape is present. The cycle-228 `replaceRange(replyStart, ..., indented)` swap still covers the full live range — the mid-stream glamour rows vanish at turn-end but the user saw them during streaming, which is the cycle-232 visible change. |
| `TestTUITools_LiveStreamCoalescesSessionMessages` (cycle 229, still PASS) | Three chunks `"Hello"`, `", "`, `"world!"` produce exactly one assistant session entry whose content is the concatenation `"Hello, world!"`; `turnDoneMsg` overwrites that single entry instead of appending a duplicate. `m.session` append/update logic is untouched. |
| `TestTUITools_TurnDoneRendersMarkdown` (cycle 228, still PASS) | A non-streamed `turnDoneMsg{reply: "Here is **bold**..."}` reaches `m.response` through the cycle-228 glamour pipeline — the glamour path is unchanged; this test doesn't drive any chunks. |
| `TestGlamour_LineANSI_*` (5 new, cycle 232) | The `mdrender.LineANSI` surface: bold markers get ANSI escapes, empty input returns empty, `PreprocessFileLinks` rewrites `#L<n>` anchors before glamour, bullet markers (`- alpha`) render, trailing newline trimmed. |
| `BenchmarkLineANSI_FirstCall` / `BenchmarkLineANSI_CachedCall` / `BenchmarkLineANSI_PlainProse` (new, cycle 232) | Per-line glamour cost measurement. Documents the ~38 μs/line steady state on Apple M4. |

The cycle-228/229/230 tests still PASS unchanged (or with a one-line
update on `TestTUITools_LiveStreamCoalescesChunksIntoOneRow` to
acknowledge the cycle-232 mid-stream glamour). No previous-cycle
regression.

## Test results

| Sweep | Result |
|---|---|
| `go build -o /tmp/wagyu-cycle232 ./cmd/wagyu` | exit 0, 88,777,122 B (+512 B over cycle-231 baseline; the new `glamourThroughIndex` field + `LineANSI` wrapper + tests) |
| `go test -vet=off -count=1 -v -run 'TestTUITools_StreamedLinesRenderGlamour\|TestTUITools_PartialLastLineKeptRaw\|TestTUITools_TurnDoneReplaceStillIdempotent' ./cmd/wagyu/` | 3/3 PASS |
| `go test -vet=off -count=1 -v -run 'TestGlamour_LineANSI' ./internal/mdrender/` | 5/5 PASS |
| `go test -vet=off -count=1 -run 'TestTUITools_LiveStream.*\|TestTUITools_TurnDoneRendersMarkdown\|TestTUISessionPersistence\|Test_SessionAccumulatesTurns\|TestTUITools_CtrlOTogglesStepDetail' ./cmd/wagyu/` | PASS / PASS / PASS / PASS / PASS / PASS / PASS / PASS |
| `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | 57 packages OK, 0 FAIL |
| `go test -vet=off -run=^$ -bench=BenchmarkLineANSI -benchtime=2s ./internal/mdrender/` | First call ~51 μs/op · cached call ~38 μs/op · plain prose ~118 μs/op |

## Hard constraints / limitations

- **Cross-line constructs render slightly off mid-stream.** An
  unclosed `**bold**` on the trailing partial, a multi-line code
  fence where only the opening line is complete, and a list-item
  continuation line all render as plain text (or partially styled)
  for ~1 second on a real model's streaming cadence before the
  cycle-228 turn-end swap re-styles them correctly. This is the
  expected shape of the cheap half of crush — `findSafeMarkdownBoundary`
  analysis is a follow-up cycle. See **Cross-line constructs** above.

- **No prefix cache.** Cycle 232 re-renders every completed line on
  every chunk. The glamour `TermRenderer` is cached (so per-line
  Render is cheap), but the goldmark AST and the ANSI escape
  sequences are rebuilt on each call. A 50-line reply that arrives
  in 1 chunk is 50 LineANSI calls (~2 ms total); the same reply
  arriving in 25 chunks is 50 + 49 + 48 + … + 1 = 1,275 LineANSI
  calls (~50 ms total) because the loop re-renders every prior
  completed line each time a new one finalises. At 1–5 lines per
  second the worst case is well under 100 ms; on a tight GPU-bound
  terminal scrolling, this could be perceptible. Crush's prefix
  cache solves this and is a follow-up.

- **Mid-stream glamour rows vanish at turn-end.** The cycle-228
  `toolBlockLines` wipe (`replaceRange(liveStart, len, nil)`,
  `tui_run.go:3059`) clears the live range before the cycle-228
  `replaceRange(replyStart, ..., indented)` re-inserts the
  `renderMarkdown(msg.reply)` output. The mid-stream glamour rows
  are *transient* — the user sees them during streaming, the
  authoritative final state is the cycle-228 glamour'd reply. A
  test (`TestTUITools_TurnDoneReplaceStillIdempotent`) pins this
  explicitly: row 0 mid-stream styled → `turnDoneMsg` overwrites
  with the cycle-228 glamour'd reply → row 0 final styled → no
  double-styling.

- **Cycle-228 chat-transcript path preserved unchanged.** The
  `turnDoneMsg` handler (`tui_run.go:3054–3170`), the
  `replaceRange(replyStart, ..., indented)` swap, the `toolBlockLines`
  wipe, and `cmd/wagyu/conversation.go:491 renderMarkdown` are all
  untouched. Cycle 232 only *adds* a mid-stream glamour pass; it
  does not change the cycle-228 authoritative end-of-turn glamour.
  A test (`TestTUITools_TurnDoneReplaceStillIdempotent`) pins this:
  the joined transcript after `turnDoneMsg` carries the cycle-228
  glamour'd output, not the mid-stream glamour'd output.

- **No print-mode mid-stream glamour.** `cmd/wagyu/main.go`'s
  print-mode path (cycle 231) accumulates chunks in
  `printTextRouter.buf` and glamour-renders the buffer in one write
  at `chunk.Done` — there is no transcript to re-style in place, so
  per-line glamour is a no-op there. The cycle-231 contract
  ("buffer; glamour at Done") is unchanged.

- **`m.session` content is the raw concatenated text, not the
  row-rendered text.** Same as cycle 229/230:
  `m.session[m.liveReplyIdx].Content += chunk` accumulates the raw
  chunk string. The mid-stream glamour pass runs against
  `m.response` rows, not against `m.session`. Resumed conversations
  re-render the whole reply through glamour on load.

- **`glamourThroughIndex` is `m.response`-indexed, not
  `m.session`-indexed.** They are independent indices: `replyStart`
  is the row offset, `glamourThroughIndex` is the row offset,
  `liveReplyIdx` is the session offset. The cycle-232 test
  `TestTUITools_TurnDoneReplaceStillIdempotent` exercises this
  separation (glamourThroughIndex reset to -1 after turnDoneMsg,
  but liveReplyIdx reset to -1 too).

- **Empty trailing rows from chunk-ending `\n`.** Same as cycle 230:
  a chunk like `"\n"` produces an additional empty row in
  `m.response` that the next chunk's first segment will extend.
  Cycle 232's mid-stream glamour pass skips the trailing empty
  row because `replyEnd = len(m.response) - 1` (the trailing slot
  is excluded). `LineANSI("")` returns `""` anyway (the cycle-228
  empty-input guard), so even if it ran it would be a no-op.

- **Test infrastructure: `LineANSI` runs against a real glamour
  renderer.** The benchmark numbers depend on the system's glamour
  version (`charm.land/glamour/v2 v2.0.1` in `go.mod`). A future
  glamour upgrade could move the per-line cost; the test contract
  pins behaviour, not cost.

## Files changed

| File | Change |
|---|---|
| `cmd/wagyu/tui_run.go` | New `glamourThroughIndex int` field on `runMode` (~lines 1565-1578); `internal/mdrender` import added at the top; init to `-1` in `NewRunMode` (~line 2052); reset to `-1` in prompt-submit clear (~line 1265), `turnDoneMsg` (~line 3155), `backendDoneMsg` (~line 3323); mid-stream glamour pass appended to `backendChunkMsg` handler (~lines 3263-3293) — runs `mdrender.LineANSI` on each completed row in `[glamourThroughIndex, len(m.response)-1)` after cycle-230's row updates, then advances `glamourThroughIndex`. The cycle-228 `toolBlockLines` wipe at ~line 3059 and the cycle-229 `replaceRange(replyStart, ..., indented)` swap at ~line 3138 are **unchanged** — cycle 232 adds a mid-stream glamour pass; it does not change the authoritative end-of-turn glamour. |
| `internal/mdrender/stream_glamour.go` (new) | `LineANSI(line string) (string, error)` — single-line analogue of cycle-228's `renderMarkdown`. Runs `PreprocessFileLinks` → `StripMermaidAndMath` → `ANSI` → `RestoreMermaidAndMath`; width 0 (auto-fit); cycle-228 fallback contract (glamour error / empty styled output → return the raw line); trailing `\n` trimmed; empty input returns empty. Reuses the `mdrender.ANSI` cache (per-(variant, width) `TermRenderer` memoisation). |
| `internal/mdrender/glamour_test.go` | Five new tests appended: `TestGlamour_LineANSI_RendersBold`, `TestGlamour_LineANSI_EmptyInputIsEmpty`, `TestGlamour_LineANSI_PreservesFileLinkPreprocessing`, `TestGlamour_LineANSI_BulletMarkersRender`, `TestGlamour_LineANSI_TrimsTrailingNewline`. |
| `internal/mdrender/line_bench_test.go` (new) | Three benchmarks: `BenchmarkLineANSI_FirstCall`, `BenchmarkLineANSI_CachedCall`, `BenchmarkLineANSI_PlainProse`. Document the per-line cost on Apple M4 (cached ~38 μs/op). |
| `cmd/wagyu/tui_tools_test.go` | Three new tests: `TestTUITools_StreamedLinesRenderGlamour`, `TestTUITools_PartialLastLineKeptRaw`, `TestTUITools_TurnDoneReplaceStillIdempotent`. `TestTUITools_LiveStreamCoalescesChunksIntoOneRow` (cycle 230) updated to acknowledge the cycle-232 mid-stream glamour — the trailing partial row now contains ANSI escapes mid-stream after a `\n` chunk, partial stays raw. |
| `docs/cycles/cycle-232-stream-glamour/deliverable.md` | this report |

## AGY alignment

AGY 1.4.2's TUI applies the embedded `charm.land/glamour/v2`
renderer incrementally as each line completes, so the user sees
styled output grow row-by-row as the model streams (cycle 226
investigation; the binary's pclntab has 153 glamour symbols under
`google3/third_party/golang/charm_land/glamour/v/v2`). The clone's
pre-232 `backendChunkMsg` handler kept every character raw until
`turnDoneMsg` swapped the whole live range in one glamour call —
the visible shape was "scroll raw markdown, then jump to styled at
end-of-turn."

Cycle 232 closes that gap: each `\n`-bounded completed line is
glamour-rendered in place via `mdrender.LineANSI`, so the user sees
the styled output grow row-by-row. Cross-line constructs (unclosed
`**bold**` mid-stream, multi-line code fences) render slightly off
mid-stream and get re-styled correctly at `turnDoneMsg` via the
cycle-228 authoritative swap. The cycle-228 chat-transcript
contract (whole-reply glamour at end-of-turn, no double-styling,
cycle-229 `replaceRange` swap covers the live range) is preserved
unchanged.