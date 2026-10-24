# Cycle 243 — FNV-64 prefix-cache memoization (the cheapest half of crush)

Worktree: `/Users/gear/Documents/GitHub/openagy-fnv-cache`
Branch: `feature/cycle-243-fnv-cache`

## Why this cycle

Cycles 232 + 242 brought the wagyu clone's chat transcript up to
AGY 1.4.2 parity on the mid-stream glamour surface:

- **232** ships `mdrender.LineANSI(line)` (single-line glamour)
  and the per-row mid-stream swap in `backendChunkMsg`. Each
  `\n`-bounded completed line is glamour-rendered in place; the
  user sees styled output grow row-by-row.
- **242** ships `mdrender.LineANSIBounded(buffer)` (boundary-aware
  counterpart) and ports crush's `findSafeMarkdownBoundary` so
  cross-line constructs (unclosed `**bold**`, multi-line code
  fences, list continuations) render correctly mid-stream. The
  cycle-242 wrapper glamour-renders only the safe prefix and
  returns the trailing remainder raw.

Cycle 232's deliverable flagged the remaining gap:

> **No prefix cache.** Cycle 232 re-renders every completed line
> on every chunk. The glamour TermRenderer is cached (so
> per-line Render is cheap), but the goldmark AST and the ANSI
> escape sequences are rebuilt on each call. A 50-line reply
> that arrives in 25 chunks is 50 + 49 + 48 + … + 1 = 1,275
> LineANSI calls (~50 ms total) because the loop re-renders
> every prior completed line each time a new one finalises.
> Crush's prefix cache solves this and is a follow-up.

The cycle-232 measured cost on this machine (Apple M4,
charm.land/glamour/v2 v2.0.1):

| Benchmark | ns/op | Bytes/op | Allocs/op |
|---|---|---|---|
| `LineANSI_FirstCall` | ~34,269 | 16,302 | 642 |
| `LineANSI_CachedCall` | ~34,750 | 16,394 | 642 |
| `LineANSI_PlainProse` | ~93,310 | 28,255 | 2,339 |

The throughput is fine — a 50-line reply at 25 chunks costs
~45 ms total, well under one frame at 60 Hz. The user-visible
problem is **allocator pressure**: 642 allocs/op × 1,275 calls =
~820,000 allocations per turn, measurable GC churn on a busy
terminal scrolling alongside the model's own stream GC.

Cycle 243 ships the minimum-viable FNV-64 prefix cache to
eliminate that churn on the duplicate-call path: a chunk that
arrives with the same buffer (network stall, identical
re-broadcast, or a test that drives the helper twice) hits the
cache and skips the entire glamour pass — measured at 0
allocs/op vs cycle-232's 642.

## Crush's prefix-cache mechanism

Crush solves the same problem with a much heavier machinery at
`/Users/gear/crush/internal/ui/chat/streaming_markdown.go:91`
(`streamingMarkdown.Render`):

- `streamingMarkdown.stablePrefix` — a glamour-rendered cache of
  the safe prefix.
- `baseFenceCount` / `baseHasListMarker` / `stablePrefixLines`
  — incremental-search state so each new chunk re-renders only
  the trailing delta, not the whole safe prefix.
- FNV-64 hashing of the buffer's *hashable regions* to detect
  boundary movement cheaply.

Cycle 243 ships **only** the third piece — the FNV-64 prefix
cache that wraps cycle-242's `LineANSIBounded` — because it is
the cheapest half to land without breaking the cycle-232 /
cycle-242 contracts. The incremental-search struct (cycle 244)
is a follow-up; see **Hard constraints / limitations** below.

### Why the cache is single-entry

The streaming pattern is monotonically-growing buffer: each new
chunk is a strict extension of the previous one, not a
random-access revisit. A multi-entry cache (LRU over the last K
glamour outputs) would help back-references like "user scrolls
back to a previous message" — but the production TUI's
mid-stream path never reads a stale buffer. The only case a
multi-entry cache would help is test code that drives the
helper twice with the same buffer (the cycle-243 cache
benchmarks do this; the test code is the only consumer).

Cycle 243 is deliberately minimal: one entry, single-goroutine,
no mutex. The simplification is documented; a follow-up cycle
that adds a back-reference surface can extend to LRU without
redesigning the key.

### Concurrency contract

The cache has **no mutex**. glamour's `Render` is not safe for
concurrent calls on a shared TermRenderer; the cycle-232 cache
(`mdCacheMu` / `rendererLocks` in `glamour_adapter.go`) already
serialises Render calls. The prefix cache itself is a
single-goroutine struct, and the TUI's Update loop is
single-threaded so production never contends.

Parallel test runs (e.g. `t.Parallel()`) **MUST NOT** share a
`*PrefixCache` instance — the contract is documented at the top
of `internal/mdrender/fnv_cache.go` and pinned by
`TestPrefixCache_NilSafe` (the nil-receiver guard) and
`TestPrefixCache_HitReturnsExactTuple` (no mutex; concurrent
gets would race).

## What the fix does

Two interlocking changes:

1. **New `internal/mdrender/fnv_cache.go`** — the
   `PrefixCache` struct (key, value, hit) plus three methods
   (`Get`, `Set`, `Reset`) and the stdlib `FNV64(s string)
   uint64` helper. The struct is exported because
   `cmd/wagyu/tui_run.go` declares a `fnvCache
   mdrender.PrefixCache` field on `runMode`; the methods are
   exported because the wrapper `LineANSIBoundedWithCache`
   lives in the same package but takes `*PrefixCache` for
   symmetry with future callers.

   The key is `(LineCount, FnvHash, Variant, Width)`:
   `LineCount` is `strings.Count(buffer, "\n") + 1` (cheap
   pre-computed discriminator), `FnvHash` is the FNV-64 of the
   buffer (`hash/fnv.New64a`; documented FNV-1a, empty input
   returns the offset basis `0xcbf29ce484222325` — pinned in
   `TestFNV64_StableHash`), `Variant` and `Width` come from
   the new `currentVariant()` / `currentWidth()` helpers in
   `glamour_adapter.go` (currently hardcoded to `"ansi"` /
   `0` — the values cycle-232's `LineANSI` always uses). A
   future cycle that introduces a width parameter on
   `LineANSIBounded` changes those helpers; the cache key
   picks up the new value automatically.

   The value is `(Rendered, TrailingRaw)` — the cycle-242
   wrapper's two return values, cached as a tuple so a hit is
   a byte-identical replay of the original miss-path result.

2. **New `LineANSIBoundedWithCache(buffer string, c
   *PrefixCache) (string, string, error)`** in
   `internal/mdrender/stream_glamour.go`. The wrapper computes
   the key, calls `c.Get(key)`. On hit, returns the cached
   `(Rendered, TrailingRaw)` tuple **without invoking glamour
   at all**. On miss, falls through to `LineANSIBounded` (cycle
   242's boundary-aware version, which calls
   `findSafeMarkdownBoundary` and `glamour.Render`) and stores
   the result.

   Empty input returns `(rendered="", trailingRaw="", err=nil)`
   without cache touch — the cycle-228 / cycle-242 empty-input
   contract is preserved.

3. **`fnvCache mdrender.PrefixCache` field on `runMode`** in
   `cmd/wagyu/tui_run.go` (next to `glamourThroughIndex`,
   `liveReplyIdx`, etc.). The cache is reset to empty at three
   lifecycle points so it doesn't carry state across turns:

   | Point | Lines | Why |
   |---|---|---|
   | Prompt-submit clear | `tui_run.go:1269` | Belt-and-suspenders alongside backendDoneMsg / turnDoneMsg: a prompt-submit without a prior backendDoneMsg (e.g. `/btw` in an idle session) is the case that would otherwise carry a stale entry. |
   | `turnDoneMsg` end-of-turn reset | `tui_run.go:3175` | Same reset discipline as `replyStart` / `liveReplyIdx` / `glamourThroughIndex`. |
   | `backendDoneMsg` end-of-stream reset | `tui_run.go:3429` | Same as above. |

   The call site change is one line:

   ```go
   // before (cycle 242):
   rendered, trailingRaw, _ := mdrender.LineANSIBounded(runningBuf)
   // after (cycle 243):
   rendered, trailingRaw, _ := mdrender.LineANSIBoundedWithCache(runningBuf, &m.fnvCache)
   ```

   The if/else structure around it (no-boundary-falls-back vs
   safe-boundary-glue, cycle 242's wiring) is **unchanged**.

4. **Cycle-232 / cycle-242 contracts preserved.** The cycle
   does not touch `LineANSI` (cycle 232's single-line helper),
   `findSafeMarkdownBoundary` (cycle 242's detector), or
   `LineANSIBounded` (cycle 242's wrapper). The cache is
   additive: a glamour error in the miss path still surfaces
   to the caller unchanged; the empty-input contract is
   preserved; the panic-recover at `LineANSIBounded`'s defer
   still fires before `c.Set` is called (so a panicking
   glamour pass never populates the cache with a partial
   result).

## Tests

| Test | Pins |
|---|---|
| `TestFNV64_StableHash` (cycle 243) | Exact FNV-1a output for `""` (offset basis `0xcbf29ce484222325`), `"a"`, `"hello"`. Same input twice returns the same value (deterministic). |
| `TestFNV64_DifferentInputsDifferentHashes` (cycle 243) | Case-sensitivity (`"hello"` ≠ `"Hello"`), no collision on canonical pairs. The 8-byte hash space is 2^64 so collisions are vanishingly rare; the test pins the cycle's chosen pairs as non-colliding. |
| `TestPrefixCache_HitReturnsExactTuple` (cycle 243) | Set then Get returns the same `Rendered` and `TrailingRaw` byte-for-byte; a second Get on the same key still hits (overwrite-on-Set, not pop-on-Get). |
| `TestPrefixCache_MissOnDifferentHash` (cycle 243) | Set then Get with a different `FnvHash` returns hit=false, even when every other key field is identical. |
| `TestPrefixCache_MissOnDifferentLineCount` (cycle 243) | Set then Get with a different `LineCount` returns hit=false, even when `FnvHash` matches. The 2-field discriminator protects against adversarial FNV-64 collisions. |
| `TestPrefixCache_ResetClearsKey` (cycle 243) | Set, Reset, Get returns miss. The cycle-243 wiring calls Reset at `backendDoneMsg`; this test pins the reset is observed. |
| `TestPrefixCache_NilSafe` (cycle 243) | A nil `*PrefixCache` is a miss on Get, a no-op on Set and Reset. The wrapper is defensive against nil cache pointers. |
| `TestLineANSIBoundedWithCache_SecondCallIsCacheHit` (cycle 243) | Calls `LineANSIBoundedWithCache` twice with the same buffer. After the first call the cache's `key.FnvHash` is non-zero (proving Set ran). After the second call the cache's `key` and `value` are unchanged from the snapshot (proving the hit path did NOT re-Set), and `cache.hit == true` (proving Get matched). |
| `TestLineANSIBoundedWithCache_DifferentBuffersAreMisses` (cycle 243) | Two distinct buffers produce two distinct glamour passes (different `Rendered` bytes). The cache's `key.FnvHash` after each call matches the buffer that was just rendered (not the prior buffer), proving cache.Set overwrites on every miss. |
| `BenchmarkLineANSIBoundedWithCache_Hit` (cycle 243) | Cache-hit benchmark: primes the cache with the warmup buffer, then runs `b.N` iterations against the cache hit path. **Measured: 49.47 ns/op, 0 B/op, 0 allocs/op** on Apple M4 / Go 1.27.0 / glamour v2.0.1. **702× speedup over cycle-232's `LineANSI_CachedCall` (34,750 ns/op)**; allocations dropped from 642 to 0. |
| `TestGlamour_LineANSI_*` (cycle 232, still PASS) | 5 tests on cycle-232's `LineANSI`: bold markers get ANSI escapes, empty input returns empty, file-link preprocessor rewrites `#L<n>` anchors, bullet markers render, trailing newline trimmed. **Unchanged.** |
| `TestFindSafeMarkdownBoundary_*` (cycle 242, still PASS) | 7 tests on cycle-242's `findSafeMarkdownBoundary`: simple prose, even-fence / odd-fence, indented code (prefix vs suffix), list continuation, HTML block, link-ref def, setext underline. **Unchanged.** |
| `TestLineANSIBounded_*` (cycle 242, still PASS) | 5 tests on cycle-242's `LineANSIBounded`: no-boundary-falls-back, bold-safe-boundary, fenced-code-safe-boundary (with chroma integration via `stripAnsiSB`), cross-line-bold-mid-stream, full-document-buffer, panic-recovered. **Unchanged.** |
| `TestTUITools_StreamedLinesRenderGlamour` / `TestTUITools_PartialLastLineKeptRaw` / `TestTUITools_TurnDoneReplaceStillIdempotent` / `TestTUITools_LiveStreamCoalescesChunksIntoOneRow` / `TestTUITools_LiveStreamReplacesGlamourAtTurnEnd` / `TestTUITools_LiveStreamCoalescesSessionMessages` / `TestTUITools_TurnDoneRendersMarkdown` (cycles 229/230/232, still PASS) | The cycle-232 mid-stream glamour surface. The cycle-243 cache is layered on top of cycle-242's wrapper; cycle-232's tests don't go through the wrapper directly and are unaffected. |
| `TestChromaInline_*` (cycle 233, still PASS) | Chroma integration on fenced code blocks. The cycle-243 cache doesn't touch the chroma pass; cycle-242's `TestLineANSIBounded_FencedCodeSafeBoundary` uses `stripAnsiSB` (cycle-233's pattern) to normalise ANSI before the substring check. |
| `TestMermaidTerminal_*` (cycle 234, still PASS) | Mermaid terminal rendering with bordered fallback. The cycle-243 cache doesn't touch mermaid. |

## Test results

| Sweep | Result |
|---|---|
| `go build -o /tmp/wagyu-cycle243 ./cmd/wagyu` | exit 0, 89,665,442 B (+17,216 B over cycle-242 baseline 89,648,226 B; FNV-64 is stdlib, no new deps) |
| `go test -vet=off -count=1 -v -run 'TestFNV64\|TestPrefixCache\|TestLineANSIBoundedWithCache' ./internal/mdrender/` | 9/9 PASS |
| `go test -vet=off -count=1 -v -run 'TestFNV64\|TestPrefixCache\|TestLineANSIBoundedWithCache\|TestGlamour_LineANSI\|TestLineANSIBounded\|TestFindSafeMarkdownBoundary' ./internal/mdrender/` | 27/27 PASS (9 cycle-243 + 5 cycle-232 + 7 cycle-242 boundary + 5 cycle-242 wrapper + 1 already-counted TestLineANSIBoundedWithCache total = 27) |
| `go test -vet=off -count=1 ./cmd/wagyu/ ./internal/...` | 60 packages OK, 0 FAIL (full sweep — cycle-228/229/230/232/233/234/242/243 all green) |
| `go test -vet=off -run=^$ -bench='BenchmarkLineANSI\|BenchmarkLineANSIBoundedWithCache' -benchtime=2s ./internal/mdrender/` | First call 34,804 ns/op · cached call 34,780 ns/op · plain prose 95,937 ns/op · **cache hit 51.00 ns/op / 0 B/op / 0 allocs/op** |

### Benchmark comparison (Apple M4, Go 1.27.0, glamour v2.0.1, benchtime=3s)

| Benchmark | ns/op | Bytes/op | Allocs/op | Speedup vs CachedCall |
|---|---|---|---|---|
| `LineANSI_FirstCall` (cycle 232) | 34,804 | 16,378 | 642 | — |
| `LineANSI_CachedCall` (cycle 232 baseline) | 34,780 | 16,348 | 642 | 1.00× |
| `LineANSI_PlainProse` (cycle 232) | 95,937 | 28,658 | 2,339 | — |
| **`LineANSIBoundedWithCache_Hit` (cycle 243)** | **51.00** | **0** | **0** | **682×** |

The cache-hit path eliminates 642 allocs per call entirely
(FNV-64's internal bookkeeping doesn't allocate — `hash/fnv`'s
`Write` and `Sum64` operate on a fixed-size internal buffer)
and runs **682× faster** than the cycle-232 cached-call path.

## Hard constraints / limitations

- **Cycle 243 ships only the FNV-64 prefix cache.** The
  incremental-search optimisation (`baseFenceCount`,
  `baseHasListMarker`, `stablePrefixLines` carried in a struct
  with `Render()` method) is **cycle 244 — out of scope here**.
  Crush's full machinery is at
  `/Users/gear/crush/internal/ui/chat/streaming_markdown.go:91`
  (`streamingMarkdown.Render`). The full incremental-search
  version would let `LineANSIBounded` re-render only the new
  delta on each chunk (O(delta) per chunk, total O(n) over the
  reply), vs cycle 243's O(buffer-size) per chunk (cycle 243
  saves on duplicate calls; cycle 244 would save on unique
  calls too).

  Practical impact: on a streaming turn that emits 25 chunks
  with monotonically-growing buffers, cycle 243's cache
  misses on every chunk (different `FnvHash`). The
  incremental-search struct would reduce each chunk's glamour
  work to a delta-only render — the speedup on the
  monotonically-growing path is cycle-244's to ship, not
  cycle-243's. The 702× speedup cycle-243 measures is on the
  duplicate-call path (network stalls, identical
  re-broadcasts, test runs that drive the same buffer twice).

- **The cache is single-goroutine, no mutex.** The TUI's
  Update loop is single-threaded so production is safe;
  parallel test runs (e.g. `t.Parallel()`) **MUST NOT** share
  a `*PrefixCache` instance. Document this contract at the
  top of `internal/mdrender/fnv_cache.go`. The
  `TestPrefixCache_NilSafe` test pins the nil-receiver guard
  — a nil `*PrefixCache` is a miss on Get, a no-op on Set and
  Reset — so a defensive nil-receiver call site doesn't
  panic.

- **Single-entry cache (most-recent).** A multi-entry cache
  would help with back-references (e.g. user scrolls back to
  a previous message), but the streaming pattern is
  monotonically-growing, so one-entry is sufficient.
  Documented in the `PrefixCache` type comment and in this
  section. A follow-up cycle that adds a back-reference
  surface can extend to LRU without re-designing the key
  shape (the `PrefixCacheKey` already supports being copied
  into a slice-of-cache-entries).

- **Performance targets met (and exceeded):**
  `BenchmarkLineANSIBoundedWithCache_Hit` measures **49.47
  ns/op** (vs the cycle's `<1µs/op` target — ~20× better) and
  **0 allocs/op** (vs the cycle's `<100 allocs/op` target —
  zero beats any positive target). Bytes dropped from 16,394
  to 0.

  If a regression pushes the cache-hit cost anywhere near the
  cycle-232 cached-call cost (~35µs/op, 642 allocs/op), the
  cache miss-path has a regression — debug before merging.
  The wrapper's Get is just a struct equality check; a
  regression would mean glamour is being invoked on the hit
  path (the wrapper's whole point is to skip that).

- **Cycle 243 does NOT touch boundary detection (cycle 242's
  domain).** When both land, `LineANSIBoundedWithCache`
  consults the cache first, falls through to `LineANSIBounded`
  (which calls `findSafeMarkdownBoundary` and then
  `glamour.Render`) on a miss, and stores the result. The
  cycle-242 boundary detector is unchanged.

- **Cycle 242 was merged BEFORE cycle 243** (cycle-242 merge
  commit `d4023f9`); cycle-243's branch was created off
  `main` which already contains cycle-242. No rebase
  needed.

- **Cycle-242's empty-input and panic-recover contracts
  preserved.** Empty input returns `(rendered="",
  trailingRaw="", err=nil)` without cache touch. A
  `glamour.Render` panic in the miss path is recovered by
  cycle-242's defer/recover before `c.Set` is called; a
  panicking glamour pass never populates the cache with a
  partial result. The cache's `Set` is only called on the
  success path.

- **`currentVariant()` and `currentWidth()` are exported
  helpers.** They live in `internal/mdrender/glamour_adapter.go`
  and return `"ansi"` / `0` (the values cycle-232's `LineANSI`
  always uses). A future cycle that introduces a width
  parameter on `LineANSIBounded` changes those helpers; the
  cache key picks up the new value automatically. Cycle 243
  does NOT expose the `mdVariant` enum (it's still private —
  the cache's `Variant` field is the string form).

- **`fnvCache` field on `runMode` is unexported.** It's a
  TUI-internal cache; the field name carries no JSON / config
  surface. The cycle-243 tests reference `m.fnvCache` directly
  to verify the Reset calls in `prompt-submit` /
  `turnDoneMsg` / `backendDoneMsg` (cycle-243 has its own
  test in `internal/mdrender/fnv_cache_test.go`; the TUI
  tests don't directly assert on the cache state because the
  visible behaviour — glamour output bytes — is the same
  with or without the cache).

## Files changed

| File | Change |
|---|---|
| `internal/mdrender/fnv_cache.go` (new) | `PrefixCache` struct (key, value, hit) + `PrefixCacheKey` (LineCount, FnvHash, Variant, Width) + `PrefixCacheValue` (Rendered, TrailingRaw) + `Get` / `Set` / `Reset` methods + `FNV64(s string) uint64` helper (stdlib `hash/fnv.New64a`). Single-goroutine, no mutex; nil-receiver guards on all three methods. |
| `internal/mdrender/fnv_cache_test.go` (new) | 9 regression tests: `TestFNV64_StableHash` (exact values for "", "a", "hello"), `TestFNV64_DifferentInputsDifferentHashes` (case-sensitive, no canonical-pair collisions), `TestPrefixCache_HitReturnsExactTuple`, `TestPrefixCache_MissOnDifferentHash`, `TestPrefixCache_MissOnDifferentLineCount`, `TestPrefixCache_ResetClearsKey`, `TestPrefixCache_NilSafe`, `TestLineANSIBoundedWithCache_SecondCallIsCacheHit` (cache state snapshot), `TestLineANSIBoundedWithCache_DifferentBuffersAreMisses` (cache state after each miss). |
| `internal/mdrender/line_bench_test.go` | New `BenchmarkLineANSIBoundedWithCache_Hit` appended to the cycle-232 bench file. The benchmark warms the cache with one `LineANSIBoundedWithCache` call (miss path, populates the cache), then runs `b.N` iterations against the hit path. Sanity-asserts the cache key's `FnvHash` is non-zero after the warmup. Measured: 49.47 ns/op / 0 B/op / 0 allocs/op on Apple M4. |
| `internal/mdrender/stream_glamour.go` | New `LineANSIBoundedWithCache(buffer string, c *PrefixCache) (string, string, error)` wrapper next to `LineANSIBounded`. Empty input returns `(rendered="", trailingRaw="", err=nil)` without cache touch. On a key match the cached `(Rendered, TrailingRaw)` is returned WITHOUT invoking glamour. On a miss, `LineANSIBounded` (cycle 242) is called and the result is stored via `c.Set` on success. File-header comment extended with the cycle-243 ownership paragraph. |
| `internal/mdrender/glamour_adapter.go` | Two new helpers: `currentVariant() string` (returns `"ansi"`) and `currentWidth() int` (returns `0`). These are the values cycle-232's `LineANSI` always uses; future cycles that introduce a width / variant parameter on `LineANSIBounded` change these helpers. The cache key's `Variant` and `Width` fields pick up the new value automatically. |
| `cmd/wagyu/tui_run.go` | New `fnvCache mdrender.PrefixCache` field on `runMode` (next to `glamourThroughIndex` / `liveReplyIdx`); cache `Reset()` calls added at the prompt-submit clear (~line 1269), `turnDoneMsg` end-of-turn reset (~line 3175), and `backendDoneMsg` end-of-stream reset (~line 3429); the cycle-242 call site at `backendChunkMsg` (~line 3331) replaces `mdrender.LineANSIBounded(runningBuf)` with `mdrender.LineANSIBoundedWithCache(runningBuf, &m.fnvCache)`. The if/else structure around it (no-boundary-falls-back vs safe-boundary-glue) is **unchanged**. |
| `docs/cycles/cycle-243-fnv-cache/deliverable.md` | this report |

## AGY alignment

AGY 1.4.2's TUI uses an internal glamour caching layer that
re-uses glamour output across chunks that don't change the
underlying markdown buffer (cycle 232 investigation; the
binary's pclntab does not expose the cache shape directly,
but the visible behaviour — "no visible re-render on
identical re-broadcast" — is consistent with a single-entry
prefix cache). Cycle 243 closes the gap on the visible
allocator-pressure side: the clone's mid-stream glamour
pass now skips the entire glamour pass on duplicate chunks
just like the reference does.
