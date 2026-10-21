package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
)

// synthLargeTranscript populates m.response with N rows: a few long model
// replies (the dominant cost) plus the usual mix of prompts and tool rows.
// The shape matches a resumed long-running conversation without needing the
// 108 MB real database.
func synthLargeTranscript(m *runMode, n int) {
	long := strings.Repeat("the quick brown fox jumps over the lazy dog ", 1600) // ~70 KB
	for i := 0; i < n; i++ {
		switch i % 5 {
		case 0:
			m.response = append(m.response, userPromptRow(fmt.Sprintf("prompt %d", i)))
		case 1, 2:
			m.response = append(m.response, long)
		case 3:
			m.response = append(m.response, "⏺ Ran a command")
		case 4:
			m.response = append(m.response, "")
		}
	}
}

// TestWrapRowsQuadratic guards the cycle-122 single-pass wrap: doubling a
// line's length should roughly double the wrap cost, not quadruple it. The
// pre-fix loop was O(w²/width); the fix is O(w).
func TestWrapRowsQuadratic(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	measure := func(n int) time.Duration {
		line := strings.Repeat("x", n)
		start := time.Now()
		for i := 0; i < 20; i++ {
			_ = wrapRows(line, 100)
		}
		return time.Since(start)
	}
	small := measure(20_000)
	large := measure(40_000)
	ratio := float64(large) / float64(small)
	t.Logf("20k=%v 40k=%v ratio=%.2f (linear≈2, quadratic≈4)", small, large, ratio)
	if ratio > 3 {
		t.Errorf("wrapRows looks super-linear: ratio %.2f for a 2x input", ratio)
	}
}

// BenchmarkViewCold times the first render of a large transcript: the wrap
// cache is dirty and every row is wrapped. This is the worst case the user
// hits on resize or after a mutation.
func BenchmarkViewCold(b *testing.B) {
	m := newBenchModel()
	synthLargeTranscript(m, 4000)
	m.width = 100
	m.height = 30
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.rowsCacheDirty = true // force a cold cache every iteration
		_ = m.View()
	}
}

// BenchmarkViewWarm times a render that the scroll path takes: the cache is
// already warm, so wrapRows is not called at all. This is the per-keypress
// cost the user feels when scrolling.
//
// The warmup covers the overflow path's width-1 wrap too: the first overflow
// render pays one extra wrap to fill the width-1 cache, and every subsequent
// scroll is two cache hits.
func BenchmarkViewWarm(b *testing.B) {
	m := newBenchModel()
	synthLargeTranscript(m, 4000)
	m.width = 100
	m.height = 30
	// Prime the main cache.
	_ = m.View()
	// Prime the overflow (width-1) cache.
	m.scrollBy(1)
	_ = m.View()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.scrollBy(1) // scroll, then re-render
		_ = m.View()
	}
}

// TestSeedThenScroll is the end-to-end shape of the user's complaint: load
// the real conversation, scroll, re-render. It is a smoke test, not a perf
// assertion — the quadratic test and the two benchmarks above carry the
// numbers.
func TestSeedThenScroll(t *testing.T) {
	id := "4638a7b0-e5f4-4961-80c2-d61499f06050"
	if !convstore.HasConversation(id) {
		t.Skip("real conversation db not on this machine")
	}
	c, err := convstore.LoadConversation(convstore.ConversationPath(id))
	if err != nil {
		t.Fatal(err)
	}
	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.seedConversation(c)
	m.width = 100
	m.height = 30
	// Cold render.
	_ = m.View()
	// Warm scroll renders — the path a keypress takes.
	for i := 0; i < 10; i++ {
		m.scrollBy(3)
		_ = m.View()
	}
}

// newBenchModel builds a runMode with the fields View() needs, leaving
// response and size to the caller.
func newBenchModel() *runMode {
	return NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
}
