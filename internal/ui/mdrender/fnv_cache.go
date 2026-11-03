// fnv_cache.go — cycle 243 single-entry prefix cache for the
// mid-stream glamour path.
//
// Why this exists. Cycle 232's per-line glamour pass
// (stream_glamour.go:LineANSI) and cycle 242's boundary-aware
// counterpart (stream_glamour.go:LineANSIBounded) both rebuild the
// goldmark AST and the ANSI escape sequences on every call, even
// when the input buffer hasn't changed. Cycle-232 measured the
// steady-state cost at ~38 µs/op / 642 allocs/op (see
// BenchmarkLineANSI_*). The throughput is fine (a 50-line reply
// streamed in 25 chunks costs ~50 ms total, well under one frame),
// but the 642-allocs-per-call constant produces ~820,000
// allocations per turn on a busy terminal — measurable GC churn
// on top of the model-stream GC.
//
// Crush solves both halves of this with a far heavier machinery at
// /Users/gear/crush/internal/ui/chat/streaming_markdown.go:
//
//   - streamingMarkdown.stablePrefix: a glamour-rendered cache of
//     the safe prefix. Cycle 244.
//   - baseFenceCount / baseHasListMarker / stablePrefixLines:
//     incremental-search state so each new chunk re-renders only
//     the new delta, not the whole safe prefix. Cycle 244.
//   - FNV-64 hashing of the buffer's *hashable regions* to detect
//     boundary movement cheaply. **Cycle 243.**
//
// Cycle 243 ships only the third piece — the FNV-64 prefix cache
// that wraps cycle-242's `LineANSIBounded` — because it is the
// cheapest half to land without breaking the cycle-232 / cycle-242
// contracts. The cache is hit-or-miss: a chunk that arrives with
// the same buffer (network stall, identical re-broadcast, or a
// test that drives the helper twice) hits and skips the entire
// glamour pass; a chunk that is a strict prefix-extension of the
// cached buffer misses (different prefix) and the cache turnover is
// high in practice — but for the cache-hit case the cost is ~0
// allocations (just the FNV-64 hash bookkeeping).
//
// The cycle ships a single-entry cache because the streaming
// pattern is monotonically-growing buffer, not random-access
// revisits. A multi-entry cache (LRU over the last K glamour
// outputs) would help back-references like "user scrolls back to
// a previous message", but the production TUI's mid-stream path
// never reads a stale buffer; a stale `fnvCache` only matters in
// tests that drive the same buffer twice. Cycle 243 is
// deliberately minimal.
//
// Concurrency contract: glamour's `Render` is not safe for
// concurrent calls on a shared TermRenderer; the cycle-232 cache
// (mdCacheMu / rendererLocks in glamour_adapter.go) already
// serialises Render calls. The prefix cache itself has NO mutex:
// it is a single-goroutine struct, and the TUI's Update loop is
// single-threaded so production never contends. Parallel test
// runs (e.g. t.Parallel()) MUST NOT share a *prefixCache
// instance — see fnv_cache_test.go for the contract.
package mdrender

import "hash/fnv"

// prefixCache is a single-entry memoisation for glamour.Render
// calls. Cycle 243 uses ONE slot (the most recent successful
// glamour output); see the file comment for why a multi-entry
// cache adds eviction complexity without measurable gain on the
// streaming pattern.
//
// Field semantics:
//
//   - key / value: the LAST Set's key-value pair. Empty strings
//     for Rendered / TrailingRaw are valid glamour outputs (a
//     glamour error returns "", so Get may legitimately return
//     a non-nil value with rendered="" — but the cache.Set is
//     only called on the success path, so the value's Rendered
//     is always non-empty in practice).
//   - hit: true after a successful Get that matched the key.
//     Not strictly required by the contract — callers compare
//     against the (value, ok) pair from Get — but exposed for
//     tests / benchmarks that want to peek at the cache state
//     without going through Get's second return value.
//
// Exported because cmd/wagyu/tui_run.go declares the cache as a
// field on runMode (single-goroutine, no mutex — see the file
// comment). The lowercase name (prefixCache) was the cycle's
// original design but the call site lives in a different
// package, so the type is exported as PrefixCache.
type PrefixCache struct {
	key   PrefixCacheKey
	value PrefixCacheValue
	hit   bool
}

// PrefixCacheKey identifies a unique glamour.Render call. Four
// fields cover the inputs that change the rendered output:
//
//   - LineCount: number of "\n" in the buffer + 1. Cheap to
//     compute; lets a future optimisation short-circuit on
//     buffers that are byte-identical except for an extra
//     trailing blank line.
//   - FnvHash: FNV-64 of the buffer contents (FNV-1a, stdlib
//     hash/fnv.New64a). 8 bytes per buffer, computed once per
//     chunk. The hash is the primary discriminator: two buffers
//     with different LineCount but identical FnvHash are still a
//     miss because LineCount differs.
//   - Variant: which glamour StyleConfig the caller will render
//     with ("notty" | "ascii" | "ansi" | "user" | "quiet" —
//     mirror of the four mdVariant constants in
//     glamour_adapter.go).
//   - Width: terminal width, in columns. <= 0 means "let
//     glamour auto-fit via COLUMNS env / terminal size", the
//     cycle-232 / cycle-243 default.
//
// Cycle 243 hardcodes variant="ansi" and width=0 (the values
// LineANSI always uses). The key's other fields exist so a
// future cycle that adds a width parameter to LineANSIBounded
// can plumb it through without re-designing the cache.
type PrefixCacheKey struct {
	LineCount int
	FnvHash   uint64
	Variant   string
	Width     int
}

// PrefixCacheValue is the cached glamour output. The wrapper
// returns both halves — rendered (glamour's output for the safe
// prefix) and trailingRaw (the un-rendered remainder) — as a
// tuple, and the cache holds both so a hit is a byte-identical
// replay of the original miss-path result.
type PrefixCacheValue struct {
	Rendered    string
	TrailingRaw string
}

// Get returns the cached (rendered, trailing) tuple if key
// matches the last Set; otherwise returns hit=false. The boolean
// is the only signal callers should rely on; the hit field on
// the cache struct is for diagnostics / tests.
//
// No mutex — single goroutine only. See the file comment.
func (c *PrefixCache) Get(key PrefixCacheKey) (PrefixCacheValue, bool) {
	if c == nil {
		return PrefixCacheValue{}, false
	}
	if c.key == key {
		c.hit = true
		return c.value, true
	}
	c.hit = false
	return PrefixCacheValue{}, false
}

// Set records the result for key. Overwrites any prior value.
// No mutex — single goroutine only. See the file comment.
//
// The caller is expected to invoke Set only on the success
// path; a glamour error leaves the cache untouched (so a
// subsequent successful chunk still finds the prior successful
// value if the keys match). This matches crush's
// streamingMarkdown.Render convention.
func (c *PrefixCache) Set(key PrefixCacheKey, value PrefixCacheValue) {
	if c == nil {
		return
	}
	c.key = key
	c.value = value
	c.hit = false
}

// Reset clears the cache. Use after a width change, a variant
// change, or any non-prefix-extension (the next chunk's buffer
// is a totally new string with no relationship to the cached
// one — keeping the stale entry around just wastes Get's hash
// comparison).
//
// No mutex — single goroutine only.
func (c *PrefixCache) Reset() {
	if c == nil {
		return
	}
	c.key = PrefixCacheKey{}
	c.value = PrefixCacheValue{}
	c.hit = false
}

// FNV64 returns the FNV-1a 64-bit hash of s using stdlib
// hash/fnv.New64a. Empty string returns the FNV-64 offset basis
// (0xcbf29ce484222325) — the documented FNV-1a behaviour and
// the value the stdlib implementation produces; the cycle's
// regression test pins it explicitly so a future hash/fnv API
// change is caught.
//
// Source: crush's internal/ui/chat/streaming_markdown.go, which
// uses the same stdlib constructor for its boundary-stable-
// prefix hashing.
func FNV64(s string) uint64 {
	h := fnv.New64a()
	// hash.Hash64.Write never returns an error (it's an
	// in-memory hash). The err-return signature is preserved
	// for stdlib API compatibility; ignore the value here.
	_, _ = h.Write([]byte(s))
	return h.Sum64()
}
