package main

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Cycle 281: benchmarks + a guard for the render hot path.
//
// The recovered 1.1.2 changelog entry says the reference fixed
//
//	"Improve responsiveness on large conversations (5000+ steps) in no
//	 flickering mode by switching hot-path line-count methods to pointer
//	 receivers, cutting the per-frame prefix-sum cost and eliminating
//	 sustained 99% CPU and keystroke lag."
//
// The clone's analogue of that prefix-sum pass is transcriptWindow's
// per-frame rebuild of the composed row slice (header + response + live
// rows), its parallel screen-row count slice, and the per-frame scan for
// user-prompt rows. All three are O(total rows) and allocate, so a warm
// scroll frame cost grows with the conversation, not with the viewport.
//
// synthTranscript fills m.response with n user/model step pairs at
// replyBytes each, using the live renderers' row shapes (a prompt row with
// the cWarningBlueOpen prefix, a blank separator, and a reply). It is
// deterministic so benchmark runs are comparable.
func synthTranscript(m *runMode, steps, replyBytes int) {
	unit := "the quick brown fox jumps over the lazy dog "
	reply := strings.Repeat(unit, replyBytes/len(unit)+1)
	reply = reply[:replyBytes]
	for i := 0; i < steps; i++ {
		m.response = append(m.response,
			userPromptRow(fmt.Sprintf("prompt %d", i)),
			"",
			reply,
			"",
		)
	}
}

// warmModel returns an idle model sized 200x40 with the transcript already
// wrapped and the composed-row cache primed, i.e. the state a scroll
// keypress renders from.
func warmModel(steps, replyBytes int) *runMode {
	m := newBenchModel()
	synthTranscript(m, steps, replyBytes)
	m.width = 200
	m.height = 40
	_ = m.View()
	m.scrollBy(1)
	_ = m.View()
	return m
}

// BenchmarkViewWarmScroll5000Steps is the claim's own scale: 5000 steps with
// a short reply (a single wrapped row), the shape of a long agent session
// whose steps are mostly tool chatter.
func BenchmarkViewWarmScroll5000Steps(b *testing.B) {
	m := warmModel(5000, 200)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.scrollBy(1)
		_ = m.View()
	}
}

// BenchmarkViewWarmScroll5000LongSteps uses 5000 steps with a 4 KB reply each
// (~20 MB of transcript), the shape of a long session full of diffs and file
// reads. The viewport is unchanged; only the off-screen row count grows.
func BenchmarkViewWarmScroll5000LongSteps(b *testing.B) {
	m := warmModel(5000, 4096)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.scrollBy(1)
		_ = m.View()
	}
}

// BenchmarkViewKeystrokeRewrap5000LongSteps is the "keystroke lag" shape:
// every non-scroll message (a typed character, a spinner tick) invalidates
// the caches, so View() must re-derive the rows. Before cycle 281 that meant
// re-wrapping all ~20 MB of transcript; the incremental slot only re-wraps
// the entries that changed.
func BenchmarkViewKeystrokeRewrap5000LongSteps(b *testing.B) {
	m := warmModel(5000, 4096)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.rowsCacheDirty = true // what Update() does for any non-scroll msg
		_ = m.View()
	}
}

// TestClipToScreenRowsPrefixMatches pins the O(log n) prefix-sum trim
// introduced in cycle 281 to the O(window²) reference implementation it
// replaced. Both must return the same slice for every window.
func TestClipToScreenRowsPrefixMatches(t *testing.T) {
	counts := []int{1, 3, 1, 1, 7, 2, 1, 1, 1, 5, 1}
	rows := make([]string, len(counts))
	for i := range rows {
		rows[i] = fmt.Sprintf("row-%d", i)
	}
	prefix := make([]int, len(counts)+1)
	for i, c := range counts {
		prefix[i+1] = prefix[i] + c
	}
	all := counts
	for off := 0; off < len(rows); off++ {
		for n := 0; off+n <= len(rows); n++ {
			for maxRows := 0; maxRows <= 12; maxRows++ {
				want := clipToScreenRows(rows[off:off+n], all, off, maxRows)
				got := clipToScreenRowsPrefix(rows[off:off+n], prefix, off, maxRows)
				if len(want) != len(got) {
					t.Fatalf("off=%d n=%d maxRows=%d: length %d != %d", off, n, maxRows, len(got), len(want))
				}
				for i := range want {
					if want[i] != got[i] {
						t.Fatalf("off=%d n=%d maxRows=%d: row %d = %q, want %q", off, n, maxRows, i, got[i], want[i])
					}
				}
			}
		}
	}
}

// TestComposedCacheMatchesColdRender is the byte-identity guard for the
// cycle-281 caches: a warm model (incremental wrap + composed-row cache
// reused across frames) must render exactly what a cold model with the same
// state renders. It covers the two ways the cache can go stale: a response
// mutation (an appended row) and a header change (the fallback warning
// block) that does not touch m.response at all.
func TestComposedCacheMatchesColdRender(t *testing.T) {
	warm := newBenchModel()
	synthTranscript(warm, 120, 300)
	warm.width, warm.height = 120, 24
	warm.altScreen = true
	_ = warm.View()

	render := func(m *runMode) string {
		m.width, m.height = 120, 24
		m.altScreen = true
		m.autoScroll = false
		return m.View().Content
	}
	cold := func(scroll int, fallback bool, extra []string) string {
		c := newBenchModel()
		c.response = append([]string(nil), warm.response...)
		c.response = append(c.response, extra...)
		if fallback {
			c.fallbackFrom, c.fallbackTo = "Gemini 9.9 Pro", "Gemini 3.8 Flash (High)"
		}
		c.scroll = scroll
		return render(c)
	}

	check := func(scroll int, fallback bool) {
		warm.scroll = scroll
		warm.autoScroll = false
		if fallback {
			warm.fallbackFrom, warm.fallbackTo = "Gemini 9.9 Pro", "Gemini 3.8 Flash (High)"
		} else {
			warm.fallbackFrom, warm.fallbackTo = "", ""
		}
		got := render(warm)
		want := cold(scroll, fallback, nil)
		if got != want {
			t.Fatalf("scroll=%d fallback=%v: warm render differs from a cold render (%d vs %d bytes)",
				scroll, fallback, len(got), len(want))
		}
	}

	for _, scroll := range []int{0, 1, 7, 100, 400} {
		check(scroll, false)
		check(scroll, true)
	}

	// A response mutation must invalidate the composition: append the way
	// the stream does, flip the dirty flag the way Update() does, and the
	// bottom-anchored frame must include the new row.
	extra := "a streamed reply that was not there when the cache was built"
	warm.response = append(warm.response, extra, "")
	warm.rowsCacheDirty = true
	warm.width, warm.height = 120, 24
	warm.altScreen = true
	warm.autoScroll = true
	warm.scroll = 1 << 30
	if got := warm.View().Content; !strings.Contains(got, extra) {
		t.Fatal("warm render after an append does not contain the appended row (stale composed cache)")
	}
}

// TestWarmFrameAllocsIndependentOfTranscriptLength pins the hot path: a
// warm frame may only pay for the viewport, not for the transcript. Doubling
// the step count must not change the bytes a frame allocates.
//
// Before cycle 281 a warm frame allocated ~2 allocations per off-screen row
// (the composed `rows` slice plus its `counts` slice), so the ratio tracked
// the step count exactly.
func TestWarmFrameAllocsIndependentOfTranscriptLength(t *testing.T) {
	if testing.Short() {
		t.Skip("allocations over a large transcript")
	}
	measure := func(steps int) (bytesPerFrame float64) {
		m := warmModel(steps, 200)
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		const frames = 200
		for i := 0; i < frames; i++ {
			m.scrollBy(1)
			_ = m.View()
		}
		runtime.ReadMemStats(&after)
		return float64(after.TotalAlloc-before.TotalAlloc) / frames
	}
	small := measure(1000)
	large := measure(8000)
	t.Logf("warm frame: 1000 steps = %.0f B/frame, 8000 steps = %.0f B/frame (ratio %.2f)",
		small, large, large/small)
	if small <= 0 {
		t.Fatalf("no allocation measured (%.0f B/frame); the measurement is broken", small)
	}
	// 8x the steps. Anything that copies or recounts the transcript per
	// frame lands near 8x; a viewport-bound frame stays flat. Allow 1.6x
	// for the small per-frame chrome allocations that do grow with the
	// visible slice's string contents.
	if ratio := large / small; ratio > 1.6 {
		t.Errorf("warm frame cost scales with transcript length: 1000 steps = %.0f B/frame, "+
			"8000 steps = %.0f B/frame (ratio %.2f); the frame is copying the transcript",
			small, large, ratio)
	}
}

// TestWarmFrameTimeIndependentOfTranscriptLength is the timing counterpart,
// with a generous bound so it only fires on the O(steps) regression, not on
// scheduler noise.
func TestWarmFrameTimeIndependentOfTranscriptLength(t *testing.T) {
	if testing.Short() {
		t.Skip("timing over a large transcript")
	}
	measure := func(steps int) time.Duration {
		m := warmModel(steps, 200)
		const frames = 100
		start := time.Now()
		for i := 0; i < frames; i++ {
			m.scrollBy(1)
			_ = m.View()
		}
		return time.Since(start) / frames
	}
	small := measure(1000)
	large := measure(8000)
	ratio := float64(large) / float64(small)
	t.Logf("warm frame: 1000 steps = %v, 8000 steps = %v (ratio %.2f)", small, large, ratio)
	if ratio > 3 {
		t.Errorf("warm frame time scales with transcript length: %v vs %v (ratio %.2f)", small, large, ratio)
	}
}
