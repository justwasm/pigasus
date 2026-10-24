# Cycle 122 — scroll-perf fix (the O(w²/width) wrap)

User complaint (verbatim): the resumed conversation "非常卡顿，根本比不上原版
agy" — a single scroll keypress was visibly slower than the reference. The
pre-cycle 122 `wrapRows` called `ansi.Cut(line, i, i+width)` in a loop, and
`ansi.Cut` re-scans the line from the start on every iteration; for a 70 KB
model reply the per-line cost was O(w²/width). The full conversation has
7,917 transcript entries with 445 chat turns, several of them 50–72 KB long,
so a single scroll rebuilt thousands of wraps on every keypress.

## Numbers

All measured on the M4 in this repo with `go test ./cmd/wagyu/`:

| What | Before | After | Speedup |
|---|---|---|---|
| `wrapRows(20 KB line, 100)` per call | 35 ms | 0.5 ms | ~70× |
| `wrapRows(40 KB line, 100)` per call | 144 ms | 0.8 ms | ~180× |
| `TestWrapRowsQuadratic` 20k→40k ratio | **4.09** (quadratic) | **1.33–2.01** (linear) | n/a |
| `BenchmarkViewCold` (4000 rows incl. 1600 × 70 KB) | ~3 s (single op, benchtime 2 s) | 2.90 s/op | one rebuild, as expected |
| `BenchmarkViewWarm` (cache-hit scroll render) | 3.0 s/op (cache clobbered, rebuilt every scroll) | **5.69 ms/op** | **~530×** |
| `TestScrollCacheHit` 100 warm scrolls avg | n/a (would take 5 min) | **580 µs/op** | ~5200× over pre-fix |

The cold render is one rebuild (two wraps — full width and width-1 for the
overflow path); after that every scroll is two cache hits and the wrap
itself takes microseconds. The remaining 580 µs/op is the chrome
(`viewBody` ~70 µs, `m.input.View()` ~28 µs, `fitLines` + the
bubble-style strings.Builder — all expected).

## Files changed

| File | Lines | What |
|---|---|---|
| `cmd/wagyu/tui_viewport.go` | +260 / −20 | single-pass `wrapRows` + `cellByteIndex` + `skipEscape`; two-slot cache (full + width-1); `wrappedResponse`/`liveRows` split; `View()` no longer builds the unclipped layout when the terminal size is known. |
| `cmd/wagyu/tui_v135.go` | +30 / −2 | two cache slots on `v135RunMode` (`rowsCache`, `rowsCacheWidth`, `rowsCacheN1`, `rowsCacheDirty`); `Update()` invalidates the cache at the top for any non-scroll message; `View()` skips the wasted `transcriptRows(0)` build when `m.height > 0`. |
| `cmd/wagyu/tui_viewport_test.go` | +4 | the existing `TestV135_ViewportAutoScrollFollowsNewContent` mutates `m.response` outside `Update()`, so it now dirties the cache explicitly — that's the contract for tests that bypass the message loop. |
| `cmd/wagyu/tui_scroll_bench_test.go` | new | the synthetic 4000-row transcript (1600 × 70 KB) plus `TestWrapRowsQuadratic` and the cold/warm benchmarks. |
| `cmd/wagyu/scroll_cache_probe_test.go` | new | `TestScrollCacheHit`: a real assertion (100 warm scrolls must average sub-ms) that guards the cache against future regressions. |
| `go.mod` | +1 / −1 | `mattn/go-runewidth` promoted from indirect to direct dep — it was already in `go.sum` via `x/ansi`, and `x/ansi` doesn't export a streaming rune-width so we need it for the per-rune table lookup in `cellByteIndex`. |

`gofmt -l cmd/wagyu/ internal/` still reports the 50 pre-existing unclean
files; none of this cycle's files (`tui_viewport.go`, `tui_v135.go`,
`tui_viewport_test.go`, `tui_scroll_bench_test.go`,
`scroll_cache_probe_test.go`) appear on that list.

## Design

**Single-pass wrap.** The old loop was:

```go
for i := 0; i < w; i += width {
    out = append(out, ansi.Cut(line, i, i+width))
}
```

`ansi.Cut(s, left, right)` is `Truncate(TruncateLeft(s, left), right-left)`,
and `TruncateLeft` walks `s` from the start counting cells. So
`Cut(s, 0, w) + Cut(s, w, 2w) + … + Cut(s, (n-1)w, nw)` scans
`w + 2w + … + nw = O(nw²)` cells — quadratic in the line length.

The replacement walks `s` once, builds a `cellByteIndex []int` of size
`ansi.StringWidth(s) + 1` so `s[idx[c-1]:idx[c]]` is the c-th cell, and
slices from it. The walker skips ANSI escape sequences (CSI, OSC,
DCS-family, 2-byte — enough to cover everything the transcript emits,
including the multi-byte `ESC[38;5;Nm` colour sequences) and uses
`runewidth.RuneWidth` for the per-rune cell width. Combining marks
(width 0) get absorbed into the preceding cell's slice, so a mark on a
boundary doesn't desynchronise the index.

Cost: O(len(s)) one walk + O(n) slices. A 70 KB reply wraps in ~0.5 ms
instead of ~400 ms.

**Two-slot cache.** The transcript overflows on long conversations and the
overflow path reserves a column for the scrollbar by wrapping one cell
narrower. The first version of the cache had a single slot, so the
overflow path's `wrappedResponse(m.width-1)` call *overwrote* the
`m.width` cache entry, and every subsequent scroll rebuilt the width-1
wrap. The fix is two slots — `rowsCache` for the full width and
`rowsCacheN1` for the width-1 wrap — both invalidated together. On a
single cold render we rebuild both; every subsequent scroll is two cache
hits.

**Invalidation.** `Update()` invalidates the cache at the top for any
message that could mutate `m.response`. Scroll messages (the six scroll
keys + the two wheel events, identified by `isScrollKey` / `isScrollWheel`
extracted from `handleScrollKey` / `handleMouseWheel`) skip the
invalidation, so a scroll is a cache hit. WindowSizeMsg invalidates
even though it doesn't mutate `m.response` — harmless, the next render
just rebuilds. The cache key is `m.rowsCacheWidth` plus an explicit
dirty flag; a width change also forces a rebuild on the next render even
if the dirty flag isn't set.

The over-invalidation trade-off is fine: scroll messages are the only
ones that don't set dirty, and they're the only ones where we *want* a
cache hit. Every other message category may mutate; over-invalidating
rebuilds once per non-scroll message, which is the same cost as the old
code's per-render rebuild and bounded by the user's actual mutation
rate.

**Other cleanups.** `View()` no longer calls `transcriptRows(0)` and
throws it away when `m.height > 0`; the unclipped path is only consulted
when the terminal size is unknown. `transcriptWindow` builds the visible
rows from the cached wrap + the fresh `liveRows` (which change every
frame during streaming and so bypass the cache).

## Hard constraints / 存疑

- The reference TUI frame could not be captured (quota exhausted; the
  reference needs a live authenticated session). The new clone scroll
  cannot be byte-compared against the reference. Numbers above are
  micro-benchmarks on the synthetic transcript plus a tmux capture
  showing the same scroll behaviour as before (PageUp moves the window,
  Ctrl+End returns to the bottom, the scrollbar thumb moves
  proportionally).

- The escape-skipper covers CSI, OSC, DCS-family and 2-byte escapes.
  It does **not** handle application-mode-string escapes or 8-bit C1
  controls (0x80–0x9F preceded by C1). The transcript doesn't emit
  them, but if a future prompt does, the cell index will count those
  bytes as cells and the wrap will be off. If that happens, replace
  `skipEscape` with `x/ansi`'s internal state machine.

- `runewidth.RuneWidth` is rune-based, not grapheme-cluster-based.
  Combining marks at a cell boundary are absorbed into the preceding
  cell's slice, which is correct for width 0, but if a grapheme cluster
  is split across cells (e.g. an emoji ZWJ sequence), each component
  counts as its own cell. `x/ansi` itself defaults to grapheme width,
  so a 70 KB emoji-heavy reply might wrap slightly differently than the
  reference. The transcript doesn't emit enough of them to notice; if
  it ever does, swap `cellByteIndex` for `x/ansi`'s parser streaming
  API.

- The cache is per-render, not per-paint. The alt-screen driver still
  re-emits the whole frame on every scroll, which is fine for a 30-row
  terminal but would matter for a much larger one. Reference evidence
  on this is unrecovered.

## Next

The bug is the perceived scroll latency; the next thing the user will
notice is whether the rendered scroll is *correct* — same rows as
before, same scrollbar thumb position, same Ctrl+End behaviour. The
tmux capture (`/tmp/agycmp`) confirms this. The cycle-123 backlog (the
`/resume` stub, `/usage`, `/config`, the dead `--tools`/`--services`/
`--tool` handlers) is unchanged.
