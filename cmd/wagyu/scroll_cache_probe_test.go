package main

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestScrollCacheHit guards the cycle-122 cache: a scroll render after the
// first one must be two cache hits (the full-width slot and the narrow
// width-1 slot used by the overflow path) and well under a millisecond. If
// it isn't, either the wrap regressed to quadratic or the overflow path
// clobbered the full-width slot.
//
// Cycle 128: the 5 ms threshold (cycle 126) was still tight enough
// to flake under full-suite load — when the tools/rea packages are
// running in parallel they can briefly consume the test goroutine's
// CPU budget. The threshold is bumped to 10 ms; the actual warm
// render on this machine is ~700 µs/op, and the cold render (the
// real regression target of TestWrapRowsQuadratic) is hundreds of
// milliseconds, so 10 ms is still far below the actual failure mode
// but well clear of load-induced noise.
func TestScrollCacheHit(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.width = 100
	m.height = 30
	// Mix of short and long rows. The long ones exercise wrapRows; the
	// short ones keep the total row count realistic.
	long := strings.Repeat("the quick brown fox jumps over the lazy dog ", 800) // ~35 KB
	for i := 0; i < 200; i++ {
		if i%5 == 1 || i%5 == 2 {
			m.response = append(m.response, long)
		} else {
			m.response = append(m.response, fmt.Sprintf("prompt row %d", i))
		}
	}
	// Cold render fills both cache slots.
	_ = m.View()
	// Warmup scroll primes the overflow (width-1) slot if the first render
	// didn't already (it usually does).
	m.scrollBy(1)
	_ = m.View()
	// The actual measurement: 100 scroll renders must average well under
	// the cold render cost. On an M4 they average ~6 ms total per View(),
	// of which the wrap is microseconds.
	start := time.Now()
	for i := 0; i < 100; i++ {
		m.scrollBy(1)
		_ = m.View()
	}
	elapsed := time.Since(start)
	per := elapsed / 100
	t.Logf("100 warm scrolls: %v (avg %v/op)", elapsed, per)
	if per > 10*time.Millisecond {
		t.Errorf("warm scroll render too slow: %v/op (expected cache hits)", per)
	}
}
