// line_bench_test.go — cycle 232 per-line glamour cost benchmark
// + cycle 243 cache-hit benchmark.
//
// Run with:
//   go test -vet=off -run=^$ -bench='BenchmarkLineANSI|BenchmarkLineANSIBoundedWithCache' -benchtime=1s ./internal/mdrender/
//
// The first iteration pays the getRenderer mint cost (the per-(variant,
// width) TermRenderer memoisation in glamour_adapter.go:121); every
// subsequent iteration is a one-shot glamour.Render call. The table
// reports ns/op so a 50-line reply at 1 ms/line is 50 ms total.
//
// BenchmarkLineANSIBoundedWithCache_Hit is the cycle-243
// cache-hit benchmark: the iteration body invokes
// LineANSIBoundedWithCache on the same buffer N times. After
// the first iteration (which warms the cache via a miss path),
// every subsequent iteration is a cache hit — glamour.Render
// is NOT called. The benchmark is the proof that the cycle-243
// hit path is dramatically cheaper than cycle-232's miss path
// (target: <1 µs/op vs cycle-232's 35 µs/op, and <100 allocs/op
// vs cycle-232's 642).
package mdrender

import (
	"strings"
	"testing"
)

func BenchmarkLineANSI_FirstCall(b *testing.B) {
	line := "**First bold** paragraph — glamour emits ANSI escapes for the bold only."
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := LineANSI(line)
		if err != nil || out == "" {
			b.Fatalf("LineANSI failed: err=%v out=%q", err, out)
		}
	}
}

func BenchmarkLineANSI_CachedCall(b *testing.B) {
	line := "**First bold** paragraph — glamour emits ANSI escapes for the bold only."
	// Warm the renderer cache so b.N measures steady-state per-line cost.
	if _, err := LineANSI(line); err != nil {
		b.Fatalf("warmup: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := LineANSI(line)
		if err != nil || out == "" {
			b.Fatalf("LineANSI failed: err=%v out=%q", err, out)
		}
	}
}

func BenchmarkLineANSI_PlainProse(b *testing.B) {
	// Plain prose: glamour emits no ANSI escapes, but the
	// PreprocessFileLinks + ANSI pipeline still runs. Used by
	// models that stream token-sized prose deltas.
	line := strings.Repeat("plain prose without markdown constructs. ", 8)
	if _, err := LineANSI(line); err != nil {
		b.Fatalf("warmup: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := LineANSI(line)
		if err != nil || out == "" {
			b.Fatalf("LineANSI failed: err=%v out=%q", err, out)
		}
	}
}

// BenchmarkLineANSIBoundedWithCache_Hit is the cycle-243
// cache-hit benchmark. The first iteration runs the wrapper on
// a miss path (calls LineANSIBounded, populates the cache);
// every subsequent iteration is a hit and skips glamour.Render
// entirely. The benchmark is the proof the cycle-243 hit path
// is dramatically cheaper than the cycle-232 miss path: the
// hit path is ~0 allocs (just the FNV-64 hash's internal
// bookkeeping) compared to cycle-232's 642 allocs/op.
//
// Run with:
//   go test -vet=off -run=^$ -bench=BenchmarkLineANSIBoundedWithCache_Hit -benchtime=1s ./internal/mdrender/
//
// Expected output (cycle-243 targets):
//
//	BenchmarkLineANSIBoundedWithCache_Hit-10   10000000   ~100 ns/op   ~0 B/op   ~0 allocs/op
//
// vs cycle-232's BenchmarkLineANSI_CachedCall:
//   ~35,000 ns/op, 16,313 B/op, 642 allocs/op
//
// The 350× speedup is the whole point of the cycle.
func BenchmarkLineANSIBoundedWithCache_Hit(b *testing.B) {
	buffer := "# Heading\n\nSome prose.\n\n- item1\n- item 2\n"
	c := &PrefixCache{}
	// Warm: prime the cache with this buffer (miss path
	// runs once, populates the cache).
	if _, _, err := LineANSIBoundedWithCache(buffer, c); err != nil {
		b.Fatalf("warmup: %v", err)
	}
	// Sanity check: the warmup should have populated the
	// cache. Without this guard the benchmark would silently
	// measure the miss path on every iteration (each
	// iteration would re-mint glamour output).
	if c.key.FnvHash == 0 {
		b.Fatalf("warmup did not populate cache key — the hit path would not be exercised")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rendered, trailing, err := LineANSIBoundedWithCache(buffer, c)
		if err != nil || rendered == "" {
			b.Fatalf("cache hit failed: err=%v rendered=%q trailing=%q", err, rendered, trailing)
		}
	}
}