// safe_boundary.go — cycle 242 markdown boundary detector.
//
// Why this exists. Cycle 232's per-completed-line glamour styling
// (stream_glamour.go:68 LineANSI) carries a documented gap: when a
// markdown construct spans a `\n` boundary (an unclosed `**bold**`,
// a multi-line ```fenced``` code block, a list-item continuation
// paragraph), glamour's goldmark AST is asked to interpret the
// half-typed state and renders slightly off mid-stream. Cycle 228's
// turn-end `replaceRange` re-styles correctly, but the visible
// glitch is the gap vs AGY 1.4.2's "every line already styled as
// it streams" behaviour.
//
// Crush solves this with a far heavier machinery: a stable-prefix
// glamour render cache, FNV-64 section hashing, an incremental
// boundary search across streaming chunks, and a constructor-state
// detector
// (`../crush/internal/ui/chat/streaming_markdown.go:91 streamingMarkdown.Render`).
// The cheapest, most transferable half is the pure-function
// boundary detector itself — `findSafeMarkdownBoundary` and its
// twelve helpers — which says, in one shot, "splitting the buffer
// here will not retroactively change how glamour renders the
// prefix." Cycle 242 ports that core as package-private functions
// in the `mdrender` package and threads them through a new
// `LineANSIBounded` helper. The incremental-search struct (crush's
// `streamingMarkdown.stablePrefix` / `stablePrefixRender` /
// `baseFenceCount` / `baseHasListMarker`) is the cycle-244
// follow-up; cycle 242 is deliberately struct-free.
//
// Why pure functions: the boundary detector has zero glamour
// dependency. It scans the raw markdown bytes once and returns a
// byte offset. Calling it once per streaming chunk costs O(n) in
// the running buffer size, and at the cycle-232 typical cadence
// (1–5 lines per second on a real model, a 50-line reply in
// ~10 s), each call is ~5-line work = well under 1 ms. The cost is
// dominated by glamour's render, which is unchanged from cycle
// 232.
//
// Pin in: TestFindSafeMarkdownBoundary_* in safe_boundary_test.go
//
//	(eight regression tests cover the four B1/B2/B3 hazards,
//	fence parity, indented code, list continuation, HTML
//	blocks, link-ref defs, setext underlines, and the
//	no-boundary fallback). TestLineANSIBounded_* pins the
//	stream_glamour.go wrapper contract.
package mdrender

import "strings"

// findSafeMarkdownBoundary returns the byte offset of the end of
// the latest safe boundary in content. A "safe" boundary is one at
// which glamour's render of content[:boundary] is byte-for-byte
// the same as glamour's render of content[:boundary] plus any
// additional content that arrives in a later chunk — i.e. the
// prefix carries no construct that would retroactively re-style on
// the arrival of the suffix.
//
// The returned offset always points immediately after a blank-line
// separator. Returns -1 when no safe boundary exists.
//
// Decision tree (latest boundary wins):
//
//  1. Walk backward through every blank-line position p such that
//     content[:p] ends with "\n\n" (or "\n[ \t]*\n").
//  2. For each candidate, check that content[:p] has an even
//     number of triple-backtick (or triple-tilde) fence lines.
//     2b. Reject if any line in content[:p] (outside fenced
//     blocks) is a list-marker line, an HTML-block opener, or a
//     link reference definition. See [prefixHasOpenHazard] for the
//     reasoning behind these "anywhere in prefix" rejects.
//  3. Reject if the last non-blank line of content[:p] keeps a
//     construct open: list marker, table line, block quote, setext
//     underline, indented code. See [lineOpensConstruct].
//  4. Reject if the line immediately AFTER the boundary (skipping
//     leading blank lines) looks like a setext underline (a line
//     of '=' or '-' only). Rendering the prefix as a paragraph
//     would change once the underline arrived.
//
// SAFETY FIRST: any time we have the slightest doubt we return -1
// and let the caller fall back to a full-document glamour pass.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:476.
func findSafeMarkdownBoundary(content string) int {
	if len(content) == 0 {
		return -1
	}

	// Iterate every blank-line position from latest to earliest.
	for p := blankLineBefore(content, len(content)); p > 0; p = blankLineBefore(content, p-1) {
		if !isSafeBoundaryAt(content, p) {
			continue
		}
		return p
	}
	return -1
}

// blankLineBefore returns the byte offset of the first character
// AFTER the latest blank-line separator that ends strictly before
// `until`. A blank-line separator is a sequence "\n([ \t]*\n)+"
// — one newline, then one or more lines containing only spaces or
// tabs and terminated by another newline. The returned offset is
// the start of the first non-blank line that follows the
// separator.
//
// Returns -1 when no blank-line separator exists before `until`.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:501.
func blankLineBefore(content string, until int) int {
	if until <= 0 {
		return -1
	}
	// Walk backward looking for a newline followed (after optional
	// blank-line content) by another newline. We track the latest
	// newline we've seen; if the next earlier newline has only
	// blank chars between them, we have a blank-line separator
	// and the boundary sits immediately after the latest newline.
	end := until
	for end > 0 {
		nl := strings.LastIndexByte(content[:end], '\n')
		if nl < 0 {
			return -1
		}
		// Look for an earlier newline whose gap to nl is empty
		// or whitespace only.
		prev := strings.LastIndexByte(content[:nl], '\n')
		for prev >= 0 {
			gap := content[prev+1 : nl]
			if isBlankOrSpaces(gap) {
				return nl + 1
			}
			// Gap had non-whitespace; nl is not a blank-line
			// separator. Move up: try with the earlier newline as
			// the new "nl" candidate.
			break
		}
		end = nl
	}
	return -1
}

// isBlankOrSpaces reports whether s consists entirely of spaces
// and tabs (or is empty).
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:536.
func isBlankOrSpaces(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != ' ' && s[i] != '\t' {
			return false
		}
	}
	return true
}

// isSafeBoundaryAt reports whether content[:p] is a safe stable
// prefix. p must be a blank-line boundary (start of a line, with a
// blank line immediately preceding).
//
// Beyond the last-line checks, three "anywhere in the prefix"
// hazards force a reject because they cannot be reliably reasoned
// about by inspecting the trailing line alone. See
// [prefixHasOpenHazard] for each.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:553.
func isSafeBoundaryAt(content string, p int) bool {
	prefix := content[:p]

	// (2) Even number of triple-backtick fence lines.
	if countFenceLines(prefix)%2 != 0 {
		return false
	}

	// (2b) Anywhere-in-prefix hazards: open list (B1), HTML block
	// opener (B2), reference link definition (B3).
	if prefixHasOpenHazard(prefix) {
		return false
	}

	// (3) Inspect the last non-blank line of the prefix.
	lastLine := lastNonBlankLine(prefix)
	if lastLine != "" && lineOpensConstruct(lastLine) {
		return false
	}

	// (4) If anything follows, make sure it doesn't look like a
	// setext underline that would retroactively turn the last
	// paragraph of the prefix into a header.
	if rest := content[p:]; rest != "" {
		first := firstNonBlankLine(rest)
		if isSetextUnderlineCandidate(first) {
			return false
		}
	}

	return true
}

// countFenceLines counts lines that begin a fenced code block in
// the CommonMark sense: a line whose first non-whitespace run is
// at least three consecutive backticks (or tildes). Each such
// line toggles the fenced state, so an even count means every
// opened fence has been closed.
//
// We accept up to three leading spaces of indentation (CommonMark
// rule) and require the fence characters to be the FIRST
// non-whitespace content of the line. We deliberately do NOT
// attempt to parse info-strings or differentiate opener from
// closer beyond toggling — a closing fence is just any line whose
// first non-whitespace run is >=3 of the same fence char.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:746.
func countFenceLines(s string) int {
	n := 0
	for line := range splitLines(s) {
		if isFenceLine(line) {
			n++
		}
	}
	return n
}

// isFenceLine reports whether line opens or closes a fenced code
// block.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:758.
func isFenceLine(line string) bool {
	// Strip up to 3 spaces of indentation.
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) {
		return false
	}
	c := line[i]
	if c != '`' && c != '~' {
		return false
	}
	run := 0
	for i < len(line) && line[i] == c {
		i++
		run++
	}
	return run >= 3
}

// lastNonBlankLine returns the last non-blank line of s, or ""
// when every line is blank.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:781.
func lastNonBlankLine(s string) string {
	last := ""
	for line := range splitLines(s) {
		if strings.TrimSpace(line) != "" {
			last = line
		}
	}
	return last
}

// firstNonBlankLine returns the first non-blank line of s, or ""
// when every line is blank.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:793.
func firstNonBlankLine(s string) string {
	for line := range splitLines(s) {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// splitLines yields the lines of s without their terminators. The
// final segment is yielded even if not newline-terminated.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:804.
func splitLines(s string) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		start := 0
		for i := 0; i < len(s); i++ {
			if s[i] == '\n' {
				if !yield(s[start:i]) {
					return
				}
				start = i + 1
			}
		}
		if start <= len(s)-1 {
			yield(s[start:])
		}
	}
}
