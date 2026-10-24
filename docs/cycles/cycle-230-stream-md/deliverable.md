# Cycle 230 — line discipline for live-streamed model chunks

## Why this cycle

Cycle 229 wired live streaming into `cmd/wagyu/tui_run.go`'s
`backendChunkMsg` handler (commit `bb152f2`), but the handler still
appended each chunk as a *fresh row* of `m.response`:

```go
case backendChunkMsg:
    ...
    m.response = append(m.response, chunk)
```

If the model streamed token-sized deltas with no `\n` between them
— the common case for a chat agent — the user saw one transcript row
per delta:

```
H
He
Hel
Hell
Hello
```

The final glamour'd output at `turnDoneMsg` was correct (cycle 229's
`replaceRange(m.replyStart, len(m.response), indented)` swapped the
raw text for the styled version), but the *intermediate* state was
unreadable and the user's eye spent the whole turn tracking jitter.

The fix is the same line discipline crush uses in spirit in
`../crush/internal/ui/chat/streaming_markdown.go:91` — keep
streaming text growing on a single line, honour `\n` as a paragraph
boundary, let glamour re-style the whole thing at end-of-turn.

## What crush does

`streamingMarkdown.Render` in
`../crush/internal/ui/chat/streaming_markdown.go:91` accumulates the
full reply in one string and caches a glamour-rendered "stable
prefix" of it; each streaming flush re-renders only the trailing
partial after a markdown-safe boundary check, gluing it back to the
cached prefix.

That's a much heavier machinery than wagyu can borrow without
risk: the clone runs cycle-228's one-shot `renderMarkdown` at
`turnDoneMsg` and has no per-line glamour cache, no incremental
boundary search, and no FNV-64 section hashing. Importing the whole
streaming renderer would change the glamour surface for the cycle-99
single-shot path as well, and cycle 228 + 229 deliberately left that
path alone.

What *is* transferable from crush — the part wagyu actually needs —
is the line discipline: every chunk a model emits belongs to exactly
one line in the user's view; newlines start a new line; same-line
chunks extend, never append. The new helper below enforces that
in `cmd/wagyu/tui_run.go` (cycle-229's `backendChunkMsg`
handler, **lines 3152–3224**).

## What the fix does

The cycle-229 single-`append` is replaced with a two-step split:

```go
parts := strings.Split(chunk, "\n")
for i, part := range parts {
    last := len(m.response) - 1
    if i == 0 && last >= m.replyStart {
        // First segment + a live row exists: extend it in place.
        m.response[last] += part
        continue
    }
    // Subsequent segments → fresh row.
    m.response = append(m.response, part)
}
```

Two cases that drive the current behaviour:

1. **Same-line chunks (no `\n` in any of them).** Three deltas like
   `backendChunkMsg("Here's ")`, `backendChunkMsg("a ")`,
   `backendChunkMsg("test.")` all carry the `i == 0 && last >=
   m.replyStart` branch, so they extend one row in `m.response`.
   `m.response[replyStart]` grows from `""` → `"Here's "` → `"Here's
   a "` → `"Here's a test."` and `len(m.response) - replyStart`
   stays at `1`. The user sees words appearing on a single line.

2. **Chunk with `\n` (paragraph break).** `" line2\n"` splits to
   `[" line2", ""]`. The first segment ` line2` extends the live
   row to `"Here's a test. line2"`; the trailing empty segment
   produces a fresh empty row that the next chunk's first segment
   extends. The model's paragraph break is preserved as a row break.

Crucially, the cycle-229 `m.replyStart`/`m.replaceRange` contract
is preserved: turnDoneMsg still does
`replaceRange(m.replyStart, len(m.response), indented)`, which
just replaces *all* the live-stream rows now (N ≥ 1) with the
glamour-rendered lines. The `m.session` coalescing (`liveReplyIdx`,
`m.session[m.liveReplyIdx].Content += chunk`) is untouched — the
cycle-229 rule that 500 deltas produce one assistant message per
turn remains in force, because that code path concatenates the raw
chunk text, not the row text.

The two constraints called out in the prompt — that the
cycle-229 `replaceRange` swap continues to work and that
`m.session` continues to coalesce into one assistant entry per
turn — both hold without source changes outside `backendChunkMsg`.

## Why not the crush renderer

For completeness, three reasons importing `streamingMarkdown.go`
verbatim would have been the wrong move:

- **Mid-stream glamour styling.** Crush runs glamour per chunk
  via the prefix cache; on a 10-KB reasoning trace this is what
  keeps the TUI responsive. Wagyu's clone defers all glamour to
  turn-end by design (cycle 228's `renderMarkdown` is single-shot,
  cheaper, and shows the raw-then-styled transition AGY itself
  exhibits — see "AGY alignment" in
  `docs/cycles/cycle-229-live-streaming/deliverable.md`). Adding
  per-chunk glamour would change the visual contract the rest of
  the cycle-229 deliverable pins.
- **Stable-prefix boundary search.** crush's `findSafeMarkdownBoundary`
  needs ~150 lines of markdown construct analysis (fence parity,
  list-marker tracking, setext underlines). For a fix that
  prevents the per-token row explosion, the only thing the
  boundary search buys is per-chunk glamour styling, which wagyu
  is not doing.
- **Per-section content hashing.** crush keys its cache by
  FNV-64 of section source text + extras, invalidating only the
  changed section. Wagyu's TUI has one section per assistant turn
  and renders it once at end-of-turn, so the cache would always
  miss.

The line-discipline is the only piece of crush that's actually
load-bearing for the wagyu problem.

## Regression tests

| Test | Pins |
|---|---|
| `TestTUITools_LiveStreamCoalescesChunksIntoOneRow` (new) | Three same-line chunks (`"Here's "`, `"a "`, `"test."`) must end up in **one** row of `m.response` whose content is `"Here's a test."`; a chunk containing `"\n"` (`" line2\n"`) must extend that row to `"Here's a test. line2"` and add one empty trailing row; after `turnDoneMsg`, `"Here's a test."` and `"line2"` each appear **exactly once** in the joined transcript (the glamour-replace nuked the streamed raw, no double-render); `replyStart` and `liveReplyIdx` reset to -1 |
| `TestTUITools_LiveStreamReplacesGlamourAtTurnEnd` (cycle 229) | Three `backendChunkMsg` updates land in `m.response` raw; `replyStart` is in the live-stream range; after `turnDoneMsg`, the raw `**bold**` literal is gone, the glamour `\x1b[1m` escape is present, and `replyStart` + `liveReplyIdx` are reset to -1 (still PASS — joined transcript still contains `**bold**` even when all three chunks land in one row, and the glamour-replace range still covers the live row) |
| `TestTUITools_LiveStreamCoalescesSessionMessages` (cycle 229) | Three chunks `"Hello"`, `", "`, `"world!"` produce exactly one assistant session entry whose content is the concatenation `"Hello, world!"`; `turnDoneMsg` overwrites that single entry instead of appending a duplicate (still PASS — `m.session` append/update logic is untouched) |
| `TestTUITools_TurnDoneRendersMarkdown` (cycle 228) | A non-streamed `turnDoneMsg{reply: "Here is **bold**..."}` reaches `m.response` through the cycle-228 glamour pipeline (still PASS — the glamour path is unchanged; this test doesn't drive any chunks) |
| `TestTUITools_CtrlOTogglesStepDetail` (cycle 99 / 178) | The collapsed/expanded hints around a tool step remain correct, and the model reply text survives ctrl+o toggling (still PASS — cycle 230 doesn't touch `toolStepLines`, `toolBlockLines`, or `replaceRange`) |
| `Test_SessionAccumulatesTurns` (cycle 70b) | Two `submitPrompt` turns each followed by a `backendChunkMsg` → `m.session` accumulates one user + one assistant entry per turn (still PASS — `m.session` indices unaffected by row-count change) |
| `TestTUISessionPersistence` (cycle 89) | Running the TUI saves prompt history per chunk (still PASS — `saveSession` is still called per chunk; the per-chunk cost is unchanged) |

The new test reads the only-differs part of the contract cycle 230
adds (one row per same-line segment, paragraph breaks as new rows)
without re-asserting everything the previous cycles already pin.

## Test results

| Sweep | Result |
|---|---|
| `go build -o /tmp/wagyu-cycle230 ./cmd/wagyu` | exit 0, 88,776,610 B (identical to `bb152f2` baseline) |
| `go test -vet=off -count=1 -v -run 'TestTUITools_LiveStream.*' ./cmd/wagyu/` | 3/3 PASS (`TestTUITools_LiveStreamCoalescesChunksIntoOneRow`, `TestTUITools_LiveStreamCoalescesSessionMessages`, `TestTUITools_LiveStreamReplacesGlamourAtTurnEnd`) |
| `go test -vet=off -count=1 -run 'TestTUITools_CtrlOTogglesStepDetail\|Test_SessionAccumulatesTurns\|TestTUISessionPersistence\|TestTUITools_TurnDoneRendersMarkdown' ./cmd/wagyu/` | PASS / PASS / PASS / PASS |
| `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | 57 packages OK, 0 FAIL |

## Hard constraints / limitations

- **Mid-stream glamour styling is still end-of-turn only.** Cycle
  230 makes the streamed text one growing row (or N rows for `\n`
  boundaries), but every character is still raw markdown — a
  stream of `**bo` then `ld**` will display as `**bold**` for the
  brief moment between those two deltas, before glamour re-styles
  the final reply at `turnDoneMsg`. Bringing per-chunk glamour
  styling in is crush's `streamingMarkdown.Render` in
  `../crush/internal/ui/chat/streaming_markdown.go:91`, which
  cycle 230 deliberately does *not* port — see "Why not the crush
  renderer" above.

- **Empty trailing rows from chunk-ending `\n`.** A chunk like
  `"\n"` or `"abc\n"` produces an additional empty row in
  `m.response` that nothing has yet extended; the *next* chunk's
  first segment will extend it (via the `i == 0` branch). If the
  model emits `"\n"` and the turn ends before the next chunk
  arrives, that empty row survives `replaceRange` only if it's
  inside `[replyStart, len(m.response))` — which it is, by
  construction. Glamour re-rendering in cycle 228 collapses the
  empty row visually because the styled output replaces the whole
  range in one `replaceRange` call. A second-cycle fix could
  special-case the trailing `\n` to avoid the temporary empty
  row, but for the streaming layout the cost is one row that the
  user briefly sees scroll past, which is the same shape AGY
  itself produces.

- **`saveSession` cost is unchanged.** Cycle 229's per-chunk
  `saveSession()` is still called per chunk; cycle 230 does not
  alter the disk-write frequency. The "500-delta turn, 500 writes"
  trade-off documented in cycle 229's deliverable remains the
  active cost.

- **No print-mode streaming fix.** `cmd/wagyu/main.go:885`'s
  print-mode `chunkHandler` is the same out-of-scope path cycle
  229 flagged — it streams raw text to stdout, doesn't touch
  `m.response`, and is the next-cycle work.

- **`m.session` content is the raw concatenated text, not the
  row-rendered text.** `m.session[m.liveReplyIdx].Content +=
  chunk` accumulates the raw chunk string, so `m.session[1]`
  contains `"Here's a test.\n"` after the cycle-230 test's
  four chunks — newlines and all — not the row-broken rendering.
  That matches crush's behaviour (the session DB stores source
  text, not rendered text) and means a future session-resume path
  re-renders the whole reply through glamour on load, not
  line-by-line.

## Files changed

| File | Change |
|---|---|
| `cmd/wagyu/tui_run.go` | `backendChunkMsg` handler rewritten: split chunk on `\n`, extend the last live row for the first segment, append a fresh row for each subsequent segment. Inline comment block documents the line-discipline rationale and references `../crush/internal/ui/chat/streaming_markdown.go:91` as the upstream pattern being adopted (in spirit only). The `replyStart` / `liveReplyIdx` / `m.session` machinery from cycle 229 is unchanged. `replaceRange` itself is **not** touched. |
| `cmd/wagyu/tui_tools_test.go` | New `TestTUITools_LiveStreamCoalescesChunksIntoOneRow` that pins the one-row-per-segment contract and confirms the cycle-229 glamour-replace still works. |
| `docs/cycles/cycle-230-stream-md/deliverable.md` | this report |

## AGY alignment

AGY's TUI streams model text live and applies glamour at the end
of the turn (the raw-then-styled transition is the visible shape
on screen — see `out/local-macos/strings-goldmark.txt`'s
glamour-renderer shapes; cycle 226 investigation also noted
`charm.land/glamour/v2` has no streaming API). Cycle 230 makes
the *intermediate* raw state readable by collapsing same-line
chunks into one growing row, which is what a human reader expects
when watching text appear character-by-character. Cycle 229's
end-of-turn glamour swap and the byte-level visible parity with
AGY's transcript are preserved unchanged.
