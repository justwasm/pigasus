# Cycle 229 — live streaming + session coalescing for chat replies

## Why this cycle

Cycle 228 fixed the markdown rendering in the live chat path but did
not address the user's second complaint: "会话的输出不是 streaming".
The TUI's `agent.Loop` Handler at `cmd/wagyu/tui_run.go:3366`
(`streamPrompt`'s closure) was accumulating each chunk's sanitised text
into a private `full` `strings.Builder` and emitting the entire reply
as one `turnDoneMsg{reply: full.String()}` at end-of-turn — the
bubble-tea `*tea.Program` was never told a chunk arrived. The user
saw a frozen spinner until the turn finished, then the glamour'd
reply appeared all at once.

## What the fix does

Four interlocking changes to `cmd/wagyu/tui_run.go`:

1. **New fields on `runMode`**:
   - `replyStart int` — index in `m.response` where the streamed raw
     reply begins. -1 when no stream is in flight.
   - `liveReplyIdx int` — index in `m.session` where the in-progress
     assistant message sits, used by `turnDoneMsg` to overwrite (not
     append) the entry with `msg.reply`. -1 when no session entry
     exists yet.

2. **Chunk handler in `streamPrompt`** (line 3402 area): after
   sanitising each delta, also call
   `prog.Send(backendChunkMsg(sanitized))` so the bubble-tea
   `*tea.Program` sees a per-delta message. `prog == nil` is the
   headless-test path (no-op).

3. **`backendChunkMsg` handler** (line 3141 area): rewritten to do
   what the cycle-99 stub was trying to do. Anchors `replyStart` on
   the first chunk, appends the raw delta to `m.response` so the
   user sees the text scroll in live, and coalesces the in-progress
   assistant message into `m.session`:
   - first chunk → `m.session = append(... assistant entry)`,
     `liveReplyIdx = len(m.session) - 1`,
   - subsequent chunks → `m.session[liveReplyIdx].Content += chunk`
     (in-place update; multiple chunks produce ONE assistant
     message per turn, not N as cycle-99 did).
   `saveSession` fires per chunk so tests that drive only
   `backendChunkMsg` (no `backendDoneMsg` / `turnDoneMsg` follow)
   still see the assistant step land on disk.

4. **`turnDoneMsg` handler** (line 3056 area): when
   `m.replyStart >= 0`, replace `[replyStart, len(m.response))`
   with the glamour-rendered, indented lines via the existing
   `m.replaceRange`. When `m.liveReplyIdx >= 0`, overwrite
   `m.session[liveReplyIdx]` with `msg.reply` instead of
   appending a duplicate. The user sees raw text scroll in live,
   then re-styled in place at turn-end — no double-rendering.

`backendDoneMsg` clears both flags (`replyStart = -1`,
`liveReplyIdx = -1`) and persists once. The prompt-submit path
also clears both so a stale range from a prior turn never bleeds
into the next.

## Regression tests

| Test | Pins |
|---|---|
| `TestTUITools_LiveStreamReplacesGlamourAtTurnEnd` | Three `backendChunkMsg` updates land in `m.response` raw; `replyStart` is in the live-stream range; after `turnDoneMsg`, the raw `**bold**` literal is gone, the glamour `\x1b[1m` escape is present, and `replyStart` + `liveReplyIdx` are reset to -1 |
| `TestTUITools_LiveStreamCoalescesSessionMessages` | Three chunks `"Hello"`, `", "`, `"world!"` produce exactly one assistant session entry whose content is the concatenation `"Hello, world!"`; `turnDoneMsg` overwrites that single entry instead of appending a duplicate |
| `TestTUISessionPersistence` (existing) | `submitPrompt` + `backendChunkMsg` → DB on disk has user + assistant steps (relied on per-chunk `saveSession`; this commit preserves cycle-99's save-on-each-chunk cost in exchange) |
| `Test_SessionAccumulatesTurns` (existing) | Two `submitPrompt` turns each followed by a `backendChunkMsg` → `m.session` accumulates one user + one assistant entry per turn |
| `TestTUITools_CtrlOTogglesStepDetail` (existing) | Cycle-228's glamour-render-at-turn-end behaviour continues to work when no `backendChunkMsg` is in flight (i.e. `replyStart = -1` falls through to the legacy append path) |

## Test results

| Sweep | Result |
|---|---|
| `go build -o /tmp/wagyu-stream ./cmd/wagyu` | exit 0, 85 MB |
| `go test -vet=off -count=1 -run 'TestTUITools_LiveStream.*' ./cmd/wagyu/` | PASS / PASS |
| `go test -vet=off -count=1 -run 'TestTUISessionPersistence|Test_SessionAccumulatesTurns|TestTUITools_CtrlOTogglesStepDetail|TestTUITools_TurnDoneRendersMarkdown' ./cmd/wagyu/` | PASS / PASS / PASS / PASS |
| `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | 57 packages OK, 0 FAIL |

## Hard constraints / limitations

- **`saveSession` runs per chunk**, not per turn. A 500-delta turn
  therefore performs 500 disk writes through the SQLite-backed
  convstore. In practice the model stream latency dominates the
  disk cost (the writes are debounced at the SQLite WAL level),
  but a follow-up cycle can debounce explicitly (e.g. save every
  Nth chunk + at `backendDoneMsg` / `turnDoneMsg`) without
  changing the on-disk schema. Documented in the inline comment
  on `saveSession()` in the `backendChunkMsg` handler.

- **Streaming text appears raw, then re-styled in place.** Glamour
  needs full input to render markdown correctly; per-chunk renders
  would mangle unclosed `**bold**` and unclosed list markers.
  Buffering-and-restyling is the cheapest correct shape. The
  visual effect (raw typing → glamour-coloured re-render at
  end-of-turn) is visible to the user; it is the same trade-off
  AGY's own glamour adapter makes (cycle 226 investigation:
  charm.land/glamour/v2 has no streaming API).

- **No print-mode streaming fix.** `cmd/wagyu/main.go:890`'s
  `chunkHandler` (the print-mode path through `pigrt.RunWith`)
  already streams chunks live to stdout but writes raw text.
  Routing print-mode through `mdrender.ANSI` per line would need
  TTY detection (ANSI escapes are noise on piped stdout) and is
  the next cycle.

## Files changed

| File | Change |
|---|---|
| `cmd/wagyu/tui_run.go` | `replyStart` + `liveReplyIdx` fields; prompt-submit reset; chunk-handler `prog.Send(backendChunkMsg(...))`; `backendChunkMsg` rewritten to live-append + coalesce session entry; `turnDoneMsg` glamour-replace + session overwrite; `backendDoneMsg` clears flags |
| `cmd/wagyu/tui_tools_test.go` | `TestTUITools_LiveStreamReplacesGlamourAtTurnEnd` + `TestTUITools_LiveStreamCoalescesSessionMessages` |
| `docs/cycles/cycle-229-live-streaming/deliverable.md` | this report |

## AGY alignment

AGY's TUI streams model text live and applies glamour at the end of
the turn (the visual raw-then-styled transition is intentional —
see `out/local-macos/strings-goldmark.txt`'s glamour-renderer
shapes). This cycle brings the wagyu clone to byte-level parity
on that surface, building directly on cycle 228's glamour wiring.
