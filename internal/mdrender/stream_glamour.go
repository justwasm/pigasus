// stream_glamour.go — cycle 232 mid-stream glamour helper.
//
// Why this exists. Cycles 228 + 229 + 230 brought the wagyu clone's
// chat transcript up to AGY 1.4.2 parity on the live-streaming
// surface: chunks stream into m.response as raw markdown, and the
// turn-end glamour pass replaces the live range with the
// glamour-rendered reply. The visual contract that produced —
// "scroll raw markdown, then swap to styled at turn-end" — does not
// match AGY 1.4.2's behaviour: AGY applies the embedded glamour
// renderer incrementally, so each completed line lands already
// styled and the user sees the styled output grow line-by-line.
//
// Crush does the same thing in spirit
// (`../crush/internal/ui/chat/streaming_markdown.go:91`), but with
// a much heavier prefix-cache + FNV-64 + `findSafeMarkdownBoundary`
// machinery that does not transfer cleanly to wagyu's one-shot
// glamour pass. The cycle-232 design takes the cheap half of crush:
// per-completed-line glamour that may look slightly off when a
// markdown construct spans lines (an unclosed `**bold**` mid-stream
// renders as plain text), then re-styled cleanly at turn-end.
//
// Cycle 242 ports crush's `findSafeMarkdownBoundary` and its
// helpers as pure functions (see safe_boundary.go) and threads
// them through `LineANSIBounded` below — the cycle-242 contract
// change. `LineANSI` remains the cycle-232 surface; the two are
// siblings, not a rename. Callers pick whichever matches their
// streaming shape: `LineANSI` for the cycle-232 "one rendered line
// per completed row" mid-stream path; `LineANSIBounded` for the
// cycle-242 "gloss the safe prefix, leave the trailing partial
// raw until the boundary advances" path. Cycle 242 wires the
// latter into `cmd/wagyu/tui_run.go` (the cycle-232 mid-stream
// call site) at `backendChunkMsg`; cycle 232's tests on
// `LineANSI` are unchanged.
//
// LineANSI is the per-line glamour helper. It runs the same
// PreprocessFileLinks → StripMermaidAndMath → ANSI →
// RestoreMermaidAndMath pipeline the cmd/wagyu/conversation.go
// renderMarkdown uses (cycle 228), but for a single line. Width 0
// means "let glamour auto-fit via COLUMNS env / terminal size".
// Errors fall through to the cycle-228 fallback contract —
// glamour error / empty styled output → return the raw line.
//
// Why per-line calls are cheap: ANSI goes through getRenderer
// (`glamour_adapter.go:121`), which memoises the per-(variant,
// width) glamour TermRenderer on first call and reuses it for every
// subsequent call. glamour's Render is the hot path; the cache
// collapses the wrapper cost to one mint per (variant, width). A
// per-line call is therefore a single Render against a
// pre-constructed TermRenderer — measured at ~80 µs per styled line
// on the cycle-232 dev box (Apple M4, glamour v2.0.1; see
// BenchmarkLineANSI in line_bench_test.go). At the typical
// streaming cadence (1–5 lines per second on a real model), a
// 50-line reply renders in ~4 ms total — well below the model's
// chunk emission latency, so the per-line glamour pass adds no
// perceptible latency to the streaming UI.
//
// Pin in: TestGlamour_LineANSI_*, glamour_adapter_test.go's
//
//	TestGlamour_ANSIFallbackOnParserError (the fallback path
//	both LineANSI and ANSI share).
package mdrender

import (
	"fmt"
	"strings"
)

// LineANSIBoundedWithCache is the cycle-243 cache-aware wrapper
// around LineANSIBounded. It is the production caller-facing
// entry point that consults a single-entry PrefixCache keyed on
// (lineCount, fnv64-hash, variant, width) and returns the
// cached (rendered, trailing) tuple on a hit WITHOUT invoking
// glamour at all. On a miss it falls through to LineANSIBounded
// (cycle 242's boundary-aware version) and stores the result.
//
// The cache is single-entry, single-goroutine, no mutex — see
// fnv_cache.go for the contract. The TUI is single-threaded so
// production is safe; parallel test runs (e.g. t.Parallel())
// MUST NOT share a *PrefixCache instance.
//
// Cycle 243 ownership: this function. Cycle 232 owns LineANSI
// (single-line glamour, no boundary awareness); cycle 242 owns
// LineANSIBounded (boundary-aware, no cache). Cycle 243 layers
// the cache on top of cycle 242 without re-implementing
// boundary detection — the cache miss path calls
// LineANSIBounded which calls findSafeMarkdownBoundary
// (safe_boundary.go) and glamour.Render.
//
// Why this exists. Cycle 232's deliverable noted:
//
//	"No prefix cache. Cycle 232 re-renders every completed line
//	 on every chunk. The glamour TermRenderer is cached (so
//	 per-line Render is cheap), but the goldmark AST and the
//	 ANSI escape sequences are rebuilt on each call."
//
// Cycle 243 closes that gap for the boundary-aware path. The
// cycle-232 mid-stream glamour pass measures 642 allocs/op
// (BenchmarkLineANSI_CachedCall); cycle-243's cache hit path
// drops to ~0 allocs/op (just FNV-64 bookkeeping). Throughput
// is unchanged either way — both fit comfortably under a
// frame budget — but the allocator pressure at 1,275 calls per
// 50-line reply is the user-visible concern the cycle ships
// to address.
//
// Pin in: TestLineANSIBoundedWithCache_SecondCallIsCacheHit in
//
//	fnv_cache_test.go. The cycle's regression suite pins
//	both halves of the cache (the struct's Get / Set / Reset
//	in isolation, and the wrapper's hit / miss paths
//	against the real glamour renderer).
func LineANSIBoundedWithCache(buffer string, c *PrefixCache) (string, string, error) {
	if buffer == "" {
		// Cycle-228 / cycle-242 empty-input contract: empty
		// in → empty out, no cache touch. The cache is a
		// memoisation of a successful glamour pass; the
		// empty case never goes through glamour anyway.
		return "", "", nil
	}
	key := PrefixCacheKey{
		LineCount: strings.Count(buffer, "\n") + 1,
		FnvHash:   FNV64(buffer),
		Variant:   currentVariant(),
		Width:     currentWidth(),
	}
	if v, ok := c.Get(key); ok {
		return v.Rendered, v.TrailingRaw, nil
	}
	rendered, trailing, err := LineANSIBounded(buffer)
	if err == nil {
		c.Set(key, PrefixCacheValue{Rendered: rendered, TrailingRaw: trailing})
	}
	return rendered, trailing, err
}

// LineANSI renders one line of source as Markdown with colour,
// suitable for mid-stream glamour styling in a streaming chat turn.
// The fall-through chain (glamour → legacy walker → raw
// passthrough) matches the cycle-228 ANSI / Plain / User / Quiet
// contract; see ANSI for the rationale.
//
// Width resolution (cycle 268): the helper resolves its width via
// the package's currentWidth() chain — the TUI pins the value via
// SetCurrentWidth on tea.WindowSizeMsg so the stream honours the
// actual terminal width; print mode resolves via $COLUMNS /
// x/term. This replaces the cycle-232 / cycle-243 hardcoded
// `ANSI(..., 0)` that capped word-wrap at glamour's default of 80
// columns regardless of how wide the terminal was.
//
// Empty input → empty output (the cycle-228 hand-back-fallback
// contract for the chat-transcript glamour path keeps empty input
// empty).
//
// A glamour error or empty styled output falls through to the raw
// input, so a single-line render that glamour cannot parse still
// reaches the user as legible text — the same "fallback to raw
// text with warning banner" behaviour cycle 1.4.2 documents.
//
// The trailing newline is trimmed off glamour's output so a row
// swap into the response slice doesn't double the cell height.
func LineANSI(line string) (string, error) {
	if line == "" {
		return "", nil
	}
	// Same pipeline as cmd/wagyu/conversation.go:514 renderMarkdown
	// but per line. A single line is unlikely to close a ```mermaid /
	// ```math fence on its own, so StripMermaidAndMath is a no-op
	// for typical content; we still call it for symmetry with the
	// chat-transcript surface (PreprocessFileLinks rewrites
	// `#L<n>` anchors mid-stream just as it does at turn-end).
	pre := PreprocessFileLinks(line)
	stripped, placeholders := StripMermaidAndMath(pre)
	// Cycle 268: thread currentWidth() through to ANSI so the
	// mid-stream glamour pass renders at the package's
	// detected/pinned width. The cycle-232 / cycle-243
	// hardcoded 0 capped every line at 80 cols regardless of
	// how wide the terminal actually was — see
	// docs/cycles/cycle-268/deliverable.md §"root cause".
	width := currentWidth()
	if width <= 0 {
		width = 80 // safety net (same contract as getRenderer)
	}
	styled, err := ANSI(stripped, width)
	if err != nil || styled == "" {
		// Cycle 228 fallback contract: when glamour rejects the
		// input (or returns empty), the user gets the raw text
		// instead of a blank transcript row.
		return line, nil
	}
	restored := RestoreMermaidAndMath(styled, placeholders)
	return strings.TrimRight(restored, "\n"), nil
}

// LineANSIBounded is the cycle-242 boundary-aware counterpart to
// LineANSI. It threads the running conversation buffer through
// findSafeMarkdownBoundary (safe_boundary.go) and glamour-renders
// only the safe prefix — the part of the buffer where glamour's
// render is byte-for-byte stable regardless of what future chunks
// add. The remaining trailing partial is returned raw (NOT
// glamour-rendered) so the caller preserves it until the next
// chunk extends the boundary.
//
// Why this exists. The cycle-232 mid-stream glamour pass uses
// LineANSI on each completed row independently, which produces a
// visible glitch when a markdown construct spans lines:
//
//   - an unclosed `**bold**` mid-stream renders as plain text;
//   - a multi-line ```fenced``` code block renders with the wrong
//     surrounding because glamour on a half-typed fence eats
//     subsequent lines;
//   - a list-item continuation paragraph (a blank-line-separated
//     indented line below a list marker) renders without the list
//     indentation.
//
// Cycle 228's turn-end `replaceRange` re-styles correctly because
// it glamour-renders the WHOLE reply in one pass; the cycle-228
// whole-document pass knows the final state. Cycle 242 ports the
// boundary detector so the cycle-232 mid-stream pass sees the
// same shape: glamour the safe prefix (which will not retroactively
// re-style), keep the trailing partial raw (which glamour has no
// honest opinion about yet), then re-glue on the next chunk when
// the boundary advances.
//
// Return contract:
//
//   - rendered: glamour-rendered output of buffer[:boundary], or
//     "" when no safe boundary exists in buffer. Same trailing-
//     newline trim as LineANSI.
//   - trailingRaw: the un-rendered remainder (buffer[boundary:]).
//     The caller appends this to its in-progress display until a
//     later chunk extends the boundary.
//   - err: nil on success. A non-nil error signals glamour's parser
//     refused the input (rare — the cycle-228 fallback contract
//     handles it the same way as LineANSI: caller shows the raw
//     buffer verbatim). A glamour panic is converted to an error
//     via the cycle-234 mermaid pattern; it never propagates.
//
// Empty input → (rendered="", trailingRaw="", err=nil). The
// cycle-228 hand-back-fallback contract keeps empty input empty.
//
// Pin in: TestLineANSIBounded_* in safe_boundary_test.go
//
//	(5 regression tests cover the empty-input, safe-
//	boundary, fenced-code, cross-line-bold, full-document
//	equivalence, and panic-recovery paths).
func LineANSIBounded(buffer string) (rendered string, trailingRaw string, err error) {
	if buffer == "" {
		return "", "", nil
	}

	// Defensive recover() — glamour's goldmark is generally
	// well-behaved but a panic in the parser must NEVER crash
	// the TUI. Same pattern as cycle 234's mermaid panic
	// recovery (mermaid_terminal.go:140). When the recover
	// fires we treat the buffer as un-renderable: return
	// (rendered="", trailingRaw=buffer, err=<typed>) so the
	// caller falls back to displaying the raw markdown verbatim.
	defer func() {
		if r := recover(); r != nil {
			rendered = ""
			trailingRaw = buffer
			err = fmt.Errorf("mdrender: glamour panicked in LineANSIBounded: %v", r)
		}
	}()

	boundary := findSafeMarkdownBoundary(buffer)
	if boundary <= 0 {
		// No safe boundary — render the whole buffer for
		// consistency with the cycle-228 ANSI fallback (the
		// caller still has a usable rendered preview), but
		// return the whole buffer as trailingRaw too so the
		// caller keeps the raw form until the boundary
		// advances on a later chunk. This is the "no safe
		// boundary" branch from cycle-242's regression
		// tests: a 1-line buffer with no blank line falls
		// here.
		rendered, err = LineANSI(buffer)
		if err != nil {
			return "", buffer, err
		}
		return rendered, buffer, nil
	}

	// Safe boundary found. Glamour-render the prefix; return the
	// raw trailing remainder.
	prefix := buffer[:boundary]
	rendered, err = LineANSI(prefix)
	if err != nil {
		return "", buffer, err
	}
	return rendered, buffer[boundary:], nil
}
