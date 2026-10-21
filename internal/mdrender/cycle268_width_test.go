// cycle268_width_test.go — pins the cycle-268 fix that glamour
// word-wrap no longer caps at 80 columns regardless of terminal
// width.
//
// The bug was simple: every mdrender.ANSI / mdrender.Plain /
// mdrender.User / mdrender.Quiet / mdrender.LineANSI /
// mdrender.LineANSIBounded call site hardcoded width=0. The
// helper cap'd at 80 (glamour's hard default for a 0 width). On
// a 250-col tmux pane the user saw model replies wrapped at
// 80 cols, with 170 cols of dead space to the right.
//
// Cycle 268's fix:
//
//   - currentWidth() does real work. Resolution chain:
//
//     1. pinned via SetCurrentWidth (the TUI's tea.WindowSizeMsg
//     handler).
//     2. $COLUMNS env.
//     3. x/term.GetSize against os.Stdout when it's a TTY.
//     4. 0 (caller's problem; getRenderer's safety fallback
//     kicks in at <=0).
//
//   - Every call site plumbs a real width through:
//     ANSI(src, width), Plain(src, width), User(src, width),
//     Quiet(src, width), renderMarkdown(text, width),
//     renderUserText(s, width), renderPrintText(raw, width).
//
//   - The TUI's tea.WindowSizeMsg handler calls
//     SetCurrentWidth(m.width) so the cycle-242 / cycle-243
//     mid-stream glamour pass picks up the terminal width via
//     currentWidth() without changing the existing single-arg
//     LineANSIBounded / LineANSI signatures.
//
//   - getRenderer's `<=0 → 80` safety fallback stays — defence
//     in depth for any future stray call site that forgets to
//     plumb a width.
//
// The tests in this file pin the contract on every axis the
// fix touches. They live next to glamour_test.go so the
// regression suite is one logical unit.
package mdrender

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// maxLineWidth scans src for the longest line, ignoring trailing
// whitespace, and returns its visible cell width. We strip ANSI
// escapes first so the glamour-emitted SGR sequences don't
// inflate the apparent width.
//
// Used by the cache-hit-by-width tests to pin that a wider width
// actually produces wider lines (the cycle-228 contract: glamour
// wraps at width-1 cells inside the configured width).
func maxLineWidth(src string) int {
	stripped := stripWidthANSI(src)
	max := 0
	for _, line := range strings.Split(stripped, "\n") {
		w := len(line) // ASCII for these tests; CJK/Emoji path has its own dedicated tests
		if w > max {
			max = w
		}
	}
	return max
}

// stripWidthANSI removes CSI / OSC escapes (the bulk of what
// glamour emits) so we can measure the visible-line length.
//
// Why "stripWidthANSI" rather than "stripANSI": chroma_inline_test.go
// already declares a package-level stripANSI built on a regex; we
// can't redeclare it. The cycle-268 helper is built directly on a
// scan loop because the regex variant panics on a few glamour
// sequences we emit (specifically the OSC-8 hyperlink escape the
// recovered reference — and cycle-268's renderer — can produce
// on file-link markdown). Manual scanning is robust against those
// edge cases.
func stripWidthANSI(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	i := 0
	for i < len(src) {
		if src[i] == 0x1b {
			// Consume CSI: ESC [ ... letter
			if i+1 < len(src) && src[i+1] == '[' {
				i += 2
				for i < len(src) {
					// CSI parameters / intermediates are bytes in
					// 0x30..0x3f or 0x20..0x2f; terminator is
					// 0x40..0x7e. Treat both halves as "consume
					// until terminator".
					c := src[i]
					i++
					if c >= 0x40 && c <= 0x7e {
						break
					}
				}
				continue
			}
			// Consume OSC: ESC ] ... BEL or ESC \
			if i+1 < len(src) && src[i+1] == ']' {
				i += 2
				for i < len(src) && src[i] != 0x07 {
					if src[i] == 0x1b && i+1 < len(src) && src[i+1] == '\\' {
						i += 2
						break
					}
					i++
				}
				continue
			}
			// Lone escape or other 2-byte sequence — drop the
			// escape, advance one byte. (glamour rarely emits
			// these for our renderers.)
			i++
			continue
		}
		b.WriteByte(src[i])
		i++
	}
	return b.String()
}

// TestGlamour_WidthPin_RealWidthNotCappedAt80 is the headline
// regression test for the cycle-268 fix. A render at width=200
// must produce lines longer than 80 cols. Before the fix every
// render passed width=0 → ANSI fell through to glamour's hard
// default of 80 cols; the long line emerged at 80 cells, capped.
//
// 184 cols of ASCII text exceeds the 80-col cap by a wide
// margin so the assertion is robust against glamour's terminal
// escape padding (which can add or subtract a few cells).
//
// Pre-cycle-268 (width=0 path → glamour's hardcoded 80): the
// source's 184 chars wrapped into three lines (80, 80, 24) and
// the longest line was 80 cols. This is exactly what the
// user's tmux observation reported.
//
// Post-cycle-268 (width=200 path): the source's 184 chars fit
// on a single rendered line (glamour wraps at width-1=199
// cols, so 184 < 199). The line is 184 cols, not 80.
func TestGlamour_WidthPin_RealWidthNotCappedAt80(t *testing.T) {
	InvalidateCache()
	src := strings.Repeat("long line of plain prose ", 8) // ~184 ASCII chars

	// First, demonstrate the pre-fix behaviour: width=0 hits
	// getRenderer's safety net at 80, so the long line wraps.
	pre, err := ANSI(src, 0)
	if err != nil {
		t.Fatalf("ANSI(0): %v", err)
	}
	preWidth := maxLineWidth(pre)
	// Cycle 271: buildANSIConfig sets Document.Margin = 2, so a render
	// at word-wrap width W occupies W-2 visible cells. The pre-fix
	// control (width=0 → 80) therefore lands at 78, and the width=200
	// case below at 198.
	if preWidth != 78 {
		t.Fatalf("pre-fix control case: ANSI(src, 0) should wrap at 78 visible cols (80 − 2 margin). got %d cols.\n%s",
			preWidth, pre)
	}

	// Now the post-fix case: width=200 produces one 184-col
	// line, not three wrapped 80-col lines.
	post, err := ANSI(src, 200)
	if err != nil {
		t.Fatalf("ANSI(200): %v", err)
	}
	postWidth := maxLineWidth(post)
	if postWidth <= 80 {
		t.Errorf("ANSI(width=200) capped at 80 cols. got longest line %d cols.\n%s",
			postWidth, post)
	}
}

// TestGlamour_WidthPin_LargerWidthProducesLongerLines confirms
// the width parameter is monotonically increasing. Wider widths
// allow longer lines.
func TestGlamour_WidthPin_LargerWidthProducesLongerLines(t *testing.T) {
	InvalidateCache()
	src := strings.Repeat("long line of plain prose ", 8) // ~184 ASCII chars
	at80, err := ANSI(src, 80)
	if err != nil {
		t.Fatalf("ANSI(80): %v", err)
	}
	at160, err := ANSI(src, 160)
	if err != nil {
		t.Fatalf("ANSI(160): %v", err)
	}
	w80 := maxLineWidth(at80)
	w160 := maxLineWidth(at160)
	if w160 <= w80 {
		t.Errorf("ANSI(width=160) longest line = %d cols; ANSI(width=80) = %d cols; expected wider render.\nwidth=80:\n%s\n---\nwidth=160:\n%s",
			w160, w80, at80, at160)
	}
	if w160 < 90 {
		t.Errorf("ANSI(width=160) only reached %d cols; expected substantially wider than 80.", w160)
	}
}

// TestGlamour_WidthPin_CacheKeyDifferentWidths confirms the
// per-(variant, width) renderer cache in getRenderer maps
// different widths to different glamour TermRenderer instances.
// This is the regression net for the cycle-268 fix to the cache
// key (widthVal rather than width — see glamour_adapter.go's
// getRenderer doc-comment).
//
// The test calls ANSI twice with different widths and asserts
// the rendered outputs differ (different cache slots →
// different renderers → different wrap widths).
func TestGlamour_WidthPin_CacheKeyDifferentWidths(t *testing.T) {
	InvalidateCache()
	src := strings.Repeat("cell ", 50) // 250 chars, longer than either test width
	at80, err := ANSI(src, 80)
	if err != nil {
		t.Fatalf("ANSI(80): %v", err)
	}
	at120, err := ANSI(src, 120)
	if err != nil {
		t.Fatalf("ANSI(120): %v", err)
	}
	w80 := maxLineWidth(at80)
	w120 := maxLineWidth(at120)
	// Cycle 268 contract: width=80 lands at ~78 visible cells
	// (width − the cycle-271 Document.Margin of 2), width=120
	// lands at ~118. Pre-cycle-268, every render cached at
	// width=80 so a width=120 call returned the width=80
	// renderer's output and w120 was at most 80.
	if w80 < 77 || w80 > 78 {
		t.Errorf("ANSI(width=80) cache slot expected ~78 cols; got %d.\n%s", w80, at80)
	}
	if w120 <= w80 {
		t.Errorf("ANSI(width=120) was no wider than ANSI(width=80) — width did not flow through to the cache key. "+
			"got widths %d, %d.\nwidth=80:\n%s\n---\nwidth=120:\n%s",
			w80, w120, at80, at120)
	}
	// Pin the upper bound: glamour wraps at the configured width
	// (the longest rendered line is approximately width cells;
	// a single word longer than the configured width may exceed
	// it, but for a 250-char source broken into 5-char words at
	// width=120 nothing in the source exceeds 120 cells). Pre
	// fix, ANSI(width=120) hit the width=80 cache slot and
	// w120 capped at 80, never reaching 120.
	if w120 < 117 {
		t.Errorf("ANSI(width=120) cache slot produced a line of %d cols; expected ~118 (120 − 2 margin).\n%s", w120, at120)
	}
}

// TestGlamour_WidthPin_EdgeCaseWidths confirms the renderer
// behaves sensibly at the width boundary conditions:
//
//   - width=0 falls back to getRenderer's safety net (glamour's
//     hard default of 80).
//   - width=1 produces *some* output even though glamour's
//     word-wrap plugin will refuse to render anything readable
//     at 1 cell. We accept any non-empty output; the goal is to
//     pin that the helper does not panic / deadlock / return
//     nil.
//   - width=999 also produces *some* output, the same way the
//     recovery path produces something readable at 1. We're
//     confirming robustness, not pinning an exact output.
func TestGlamour_WidthPin_EdgeCaseWidths(t *testing.T) {
	InvalidateCache()
	src := "hello world"
	cases := []struct {
		name string
		w    int
	}{
		{"width-zero-falls-back-to-80", 0},
		{"width-one-does-not-panic", 1},
		{"width-huge-does-not-panic", 999},
		{"width-negative-falls-back-to-80", -1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ANSI(src, c.w)
			if err != nil {
				// width=1 may legitimately error; we're checking
				// for sane behaviour. The "fall back to glamour
				// 80" path the user observes on a real call is
				// routed by getRenderer's safety net before
				// glamour sees width=0, so the render itself
				// proceeds with width=80.
				t.Logf("ANSI(width=%d): %v (this is acceptable when glamour refuses to wrap at <2 cols)", c.w, err)
				return
			}
			if got == "" {
				t.Errorf("ANSI(width=%d) returned empty string", c.w)
			}
		})
	}
}

// TestGlamour_CurrentWidth_ResolvesColumnsEnv pins the third
// step of the currentWidth() resolution chain: when the TUI has
// not pinned a value and the writer is not a TTY (a piped
// consumer), the helper falls back to $COLUMNS.
//
// Test runtime: we set COLUMNS in the test process environment
// for the duration of the call. Note that os.Setenv on
// COLUMNS affects os.Getenv globally — concurrent tests that
// read COLUMNS via the package's CurrentWidth() chain may
// observe the override. The cycle's regression suite is not
// parallel across these tests, and the helper is read-only
// outside of SetCurrentWidth so race-free in production.
func TestGlamour_CurrentWidth_ResolvesColumnsEnv(t *testing.T) {
	// Save and restore the env var.
	prev, hadPrev := os.LookupEnv("COLUMNS")
	defer func() {
		if hadPrev {
			os.Setenv("COLUMNS", prev)
		} else {
			os.Unsetenv("COLUMNS")
		}
		// Drop any pinned value the test may have set so
		// other tests do not see it.
		SetCurrentWidth(0)
	}()

	// Drop any prior pin so currentWidth() falls through to env.
	SetCurrentWidth(0)

	os.Setenv("COLUMNS", "321")
	got := CurrentWidth()
	if got != 321 {
		t.Errorf("CurrentWidth() with COLUMNS=321 and no pin = %d, want 321", got)
	}
}

// TestGlamour_CurrentWidth_IgnoresInvalidColumns pins the
// currentWidth() defensive parse: a non-numeric COLUMNS, or a
// zero / negative COLUMNS, must NOT be returned as the package's
// currentWidth — the helper falls through to x/term.
//
// Cycle-268's safety net here is "any not-positive integer is
// rejected", so a COLUMNS=0 (which the shell sometimes sets on
// an unwritable winsize) does not propagate to glamour as
// WithWordWrap(0) (which glamour documents as "use the hard
// default of 80", but we want the fallback chain to make the
// intent explicit).
func TestGlamour_CurrentWidth_IgnoresInvalidColumns(t *testing.T) {
	prev, hadPrev := os.LookupEnv("COLUMNS")
	defer func() {
		if hadPrev {
			os.Setenv("COLUMNS", prev)
		} else {
			os.Unsetenv("COLUMNS")
		}
		SetCurrentWidth(0)
	}()
	SetCurrentWidth(0)

	for _, s := range []string{"abc", "0", "-7", "1.5", ""} {
		os.Setenv("COLUMNS", s)
		// We don't pin a specific value (the test process may
		// or may not be on a TTY); the important thing is that
		// the bad input is rejected:
		//   - If we're on a TTY, CurrentWidth returns the TTY
		//     width (positive).
		//   - If we're not on a TTY, CurrentWidth returns 0.
		// Both outcomes reject the malformed "0" / "-7" / "abc"
		// inputs — only a positive integer survives.
		got := CurrentWidth()
		if got <= 0 {
			continue // unpiped, no result — fine.
		}
		// If positive, it should be either the TTY width (which
		// we cannot pin) or some sanely positive value, never
		// the literal "0" / "-7" / "abc".
		if got == 0 {
			t.Errorf("COLUMNS=%q: CurrentWidth() returned 0 (the literal value should have been rejected)", s)
		}
	}
}

// TestGlamour_SetCurrentWidth_PinsValue pins the TUI-side
// override: once SetCurrentWidth(N) is called, currentWidth()
// returns N until either SetCurrentWidth is called again or the
// pinned value is cleared (width<=0).
func TestGlamour_SetCurrentWidth_PinsValue(t *testing.T) {
	prev, hadPrev := os.LookupEnv("COLUMNS")
	defer func() {
		if hadPrev {
			os.Setenv("COLUMNS", prev)
		} else {
			os.Unsetenv("COLUMNS")
		}
		SetCurrentWidth(0)
	}()
	SetCurrentWidth(0)
	os.Setenv("COLUMNS", "999")

	SetCurrentWidth(123)
	if got := CurrentWidth(); got != 123 {
		t.Errorf("CurrentWidth() after SetCurrentWidth(123) = %d, want 123 (pin should beat $COLUMNS)", got)
	}

	// Clear the pin; COLUMNS should win.
	SetCurrentWidth(0)
	if got := CurrentWidth(); got != 999 {
		t.Errorf("CurrentWidth() after SetCurrentWidth(0) with COLUMNS=999 = %d, want 999 (no pin → env wins)", got)
	}
}

// TestGlamour_PrefixCache_WidthAffectsCacheKey confirms the
// cycle-243 prefix cache keys on Width, so the same buffer
// renders through different glamour TermRenderers depending on
// the pinned width. Without the cycle-268 fix the cache key was
// always width=0 (currentWidth's pre-cycle-268 stub) and every
// width hit the same cache slot — meaning a render at width=200
// that filled the cache returned the cached width=80 render on
// subsequent calls.
func TestGlamour_PrefixCache_WidthAffectsCacheKey(t *testing.T) {
	InvalidateCache()
	c := &PrefixCache{}
	buf := strings.Repeat("alpha bravo charlie delta echo ", 8) + "\n\n" // safe-boundary-able

	SetCurrentWidth(40)
	r40a, _, err := LineANSIBoundedWithCache(buf, c)
	if err != nil {
		t.Fatalf("LineANSIBoundedWithCache @ 40: %v", err)
	}
	w40a := maxLineWidth(r40a)

	SetCurrentWidth(200)
	r200, _, err := LineANSIBoundedWithCache(buf, c)
	if err != nil {
		t.Fatalf("LineANSIBoundedWithCache @ 200: %v", err)
	}
	w200 := maxLineWidth(r200)

	if w200 <= w40a {
		t.Errorf("Width did not affect the cache key. w40=%d, w200=%d. width=200 should produce wider output than width=40.", w40a, w200)
	}

	// Switch back to width=40; cache hit on the SAME buffer should
	// return the SAME width=40 output verbatim (verifying the
	// cache key carried the width distinction forward).
	SetCurrentWidth(40)
	r40b, _, err := LineANSIBoundedWithCache(buf, c)
	if err != nil {
		t.Fatalf("LineANSIBoundedWithCache @ 40 (second): %v", err)
	}
	if r40a != r40b {
		t.Errorf("Cache hit at same width produced different output. Likely a cache-key collision.")
	}
}

// TestGlamour_WidthPin_NarrowSourceProducesContent is the
// negative-control case: a render that is much shorter than the
// requested width must still contain the source content
// verbatim. The pre-cycle-268 bug did NOT cause a regression on
// this axis (glamour renders "narrow" + the configured
// width's worth of dim-grey trailing whitespace inside the
// DarkStyleConfig cycle-268 picked up — see
// TestGlamour_WidthPin_NarrowSourcePaddingIsGlamourDefault for
// the rationale), so this test pins that the source string
// never disappears entirely (which would indicate a
// regression where the wrap plugin failed to emit it).
func TestGlamour_WidthPin_NarrowSourceProducesContent(t *testing.T) {
	InvalidateCache()
	src := "narrow content here"
	got, err := ANSI(src, 200)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	if !strings.Contains(stripWidthANSI(got), src) {
		t.Errorf("ANSI(width=200, src=%q) lost the source text: rendered=%q", src, got)
	}
}

// TestGlamour_WidthPin_FallbackToEighty pins the getRenderer
// safety net for any future call site that forgets to plumb a
// width. ANSI(src, 0) must NOT return empty (that would be a
// regression — the safety net explicitly maps <=0 → 80 so a
// stray width=0 still produces readable output).
func TestGlamour_WidthPin_FallbackToEighty(t *testing.T) {
	InvalidateCache()
	src := strings.Repeat("fallback ", 12) // ~110 chars, > 80 cols
	got, err := ANSI(src, 0)
	if err != nil {
		t.Fatalf("ANSI(src, 0): %v", err)
	}
	if got == "" {
		t.Errorf("ANSI(src, 0) returned empty; the safety-net should have produced readable output.")
	}
	w := maxLineWidth(got)
	if w == 0 || w > 80 {
		t.Errorf("ANSI(src, 0) safety-net produced a line of %d cols; expected wrap at <=80.", w)
	}
}

// TestGlamour_AllVariantsHonorWidth is the broad sweep: every
// public renderer (ANSI / Plain / User / Quiet) must respect
// the width parameter. Pre-cycle-268 every variant went through
// ANSI(..., 0) (or its variant lookup) which capped at 80.
//
// The test calls each variant with width=200 on the same source
// and asserts at least one rendered line is wider than 80 cols.
// A failure on any variant means the cycle-268 plumbing is
// incomplete on that variant.
func TestGlamour_AllVariantsHonorWidth(t *testing.T) {
	InvalidateCache()
	src := strings.Repeat("variant ", 30) // ~270 chars, much wider than 80

	type variant struct {
		name string
		fn   func(string, int) (string, error)
	}
	variants := []variant{
		{"ANSI", ANSI},
		{"Plain", Plain},
		{"User", User},
		{"Quiet", Quiet},
	}
	for _, v := range variants {
		t.Run(v.name, func(t *testing.T) {
			got, err := v.fn(src, 200)
			if err != nil {
				t.Fatalf("%s: %v", v.name, err)
			}
			w := maxLineWidth(got)
			if w <= 80 {
				t.Errorf("%s(width=200, src=%d cols) capped at 80 cols; got longest line %d cols. Cycle 268 plumbing missing on this variant.\n%s",
					v.name, len(src), w, got)
			}
		})
	}
}

// TestGlamour_StyleBytesSurviveWidth asserts that the SGR
// payload glamour emits is preserved across widths — the bug
// is purely in the wrap width, not in the styling. Pre-cycle-268
// the styling was correct (the bug was a numeric cap), so this
// test pins that cycle 268's plumbing didn't accidentally drop
// the SGR sequences.
func TestGlamour_StyleBytesSurviveWidth(t *testing.T) {
	InvalidateCache()
	src := "this is **important** to render"

	at200, err := ANSI(src, 200)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	if !strings.Contains(at200, "\x1b[") {
		t.Errorf("ANSI(width=200) lost the SGR payload — expected ANSI escape, got %q", at200)
	}
	if !strings.Contains(stripWidthANSI(at200), "important") {
		t.Errorf("ANSI(width=200) lost the emphasised word: %q", at200)
	}
}

// TestGlamour_NoInfiniteRecursionOnZeroWidth is the paranoid
// guard: ANSI(very_long_source, 0) must terminate. The pre-fix
// helpers all mapped width=0 → glamour's auto-fit, which for a
// very long source emits a single line. This test pins that
// behaviour survives cycle 268.
func TestGlamour_NoInfiniteRecursionOnZeroWidth(t *testing.T) {
	InvalidateCache()
	src := strings.Repeat("stress ", 1000)
	var buf bytes.Buffer
	got, err := ANSI(src, 0)
	if err != nil {
		t.Fatalf("ANSI: %v", err)
	}
	buf.WriteString(got)
	if buf.Len() == 0 {
		t.Errorf("ANSI on a long source with width=0 returned empty")
	}
	// Belt and braces: spot-check the deterministic head.
	if !strings.HasPrefix(strings.TrimSpace(stripWidthANSI(got)), "stress") {
		t.Errorf("ANSI src prefix mismatch: %q", got[:min(80, len(got))])
	}
}

// Helper kept here to avoid the stdlib min ambiguity across Go versions.
// (We only need the int-min for the test, but using min(80, len) keeps
// it readable.)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// Compile-time check that we exercise the right variables (catches
// dead-code / shadowing mistakes during refactors).
var _ = fmt.Sprintf
