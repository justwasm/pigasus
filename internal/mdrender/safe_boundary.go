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

// prefixHasOpenHazard reports whether prefix contains any of three
// constructs that cannot be safely cut at a blank-line boundary
// even when the immediately preceding line looks fine. Each check
// uses the simplest viable conservative rule:
//
//	B1 (loose lists, refined). A loose list has a blank line
//	   between an item and a continuation paragraph that begins
//	   with indentation but no list marker. If a candidate
//	   boundary lands on that blank line, the prefix's trailing
//	   non-blank line is the continuation paragraph, NOT a list
//	   marker, so the last-line check in lineOpensConstruct would
//	   accept it even though the list is still open.
//
//	   Rule chosen: track whether any list-marker line appears in
//	   the prefix. If one does AND the last non-blank line is
//	   indented but is not itself a list marker (i.e. it is a
//	   continuation paragraph that lineOpensConstruct would miss
//	   because it only flags 4+ leading spaces), the list is
//	   potentially open and we reject. This catches loose-list
//	   continuation paragraphs without forfeiting every boundary
//	   after a CLOSED list.
//
//	B2 (HTML blocks). CommonMark defines seven HTML-block opener
//	   patterns (script/pre/style/textarea, comments, processing
//	   instructions, CDATA, declarations, recognised tag names).
//	   If the prefix opens an HTML block that the suffix closes,
//	   splitting renders the prefix as raw HTML and the suffix as
//	   prose.
//
//	   Rule chosen: any HTML-block opener anywhere in the prefix
//	   forces -1.
//
//	B3 (reference link definitions). A line of the form
//	   "[label]: <url>" defines a link reference that the suffix
//	   may later use as "[text][label]". Splitting the document
//	   loses the definition because each half is rendered as an
//	   independent glamour document.
//
//	   Rule chosen: any reference link definition line anywhere
//	   in the prefix forces -1.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:686.
func prefixHasOpenHazard(prefix string) bool {
	inFence := false
	hasListMarker := false
	var lastNonBlankTrimmed string
	var lastNonBlankRaw string
	for line := range splitLines(prefix) {
		// Track fenced state so list/html/ref patterns inside a
		// fenced code block do not falsely trigger the hazards.
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed == "" {
			continue
		}
		lastNonBlankTrimmed = trimmed
		lastNonBlankRaw = line
		// B1: track list markers.
		if isListItemMarker(trimmed) {
			hasListMarker = true
		}
		// B2: HTML block opener.
		if isHTMLBlockOpener(line) {
			return true
		}
		// B3: link reference definition.
		if isLinkRefDefinition(line) {
			return true
		}
	}
	// B1 (refined): a list is potentially open only when a marker
	// appeared earlier AND the last non-blank line is indented but
	// is not itself a list marker (i.e. it is a continuation
	// paragraph that lineOpensConstruct would miss because it only
	// catches 4+ leading spaces). A non-indented last line means
	// the list was closed by the blank line before it.
	if hasListMarker && lastNonBlankTrimmed != "" && !isListItemMarker(lastNonBlankTrimmed) {
		if len(lastNonBlankRaw) > 0 && (lastNonBlankRaw[0] == ' ' || lastNonBlankRaw[0] == '\t') {
			return true
		}
	}
	return false
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

// lineOpensConstruct reports whether line keeps a markdown
// construct open across the boundary. We err conservatively — any
// case that smells like list/table/quote/setext/indented-code
// returns true.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:825.
func lineOpensConstruct(line string) bool {
	// Indented code: a tab, or 4+ leading spaces.
	if len(line) > 0 && line[0] == '\t' {
		return true
	}
	if strings.HasPrefix(line, "    ") {
		return true
	}

	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" {
		return false
	}

	// Block quote.
	if trimmed[0] == '>' {
		return true
	}

	// List item: "- " "* " "+ " or "<digits>. " or "<digits>) ".
	if isListItemMarker(trimmed) {
		return true
	}

	// Table: any pipe character anywhere in the line. Conservative:
	// pipe-in-prose is rare and the cost of bailing is one slow
	// frame.
	if strings.ContainsRune(line, '|') {
		return true
	}

	// Setext underline candidate as the LAST line of the prefix:
	// this would be a setext header for an even-earlier paragraph.
	if isSetextUnderlineCandidate(trimmed) {
		return true
	}

	return false
}

// isListItemMarker reports whether line (already left-trimmed)
// starts with a CommonMark list-item marker followed by a space
// or tab.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:870.
func isListItemMarker(line string) bool {
	if line == "" {
		return false
	}
	c := line[0]
	if c == '-' || c == '*' || c == '+' {
		if len(line) >= 2 && (line[1] == ' ' || line[1] == '\t') {
			return true
		}
		return false
	}
	// Ordered list: digits followed by '.' or ')' and a space.
	i := 0
	for i < len(line) && line[i] >= '0' && line[i] <= '9' {
		i++
	}
	if i == 0 || i > 9 {
		return false
	}
	if i >= len(line) {
		return false
	}
	if line[i] != '.' && line[i] != ')' {
		return false
	}
	if i+1 >= len(line) {
		return false
	}
	return line[i+1] == ' ' || line[i+1] == '\t'
}

// isSetextUnderlineCandidate reports whether line (with optional
// leading whitespace) consists entirely of '=' or entirely of '-'
// characters with optional trailing whitespace. CommonMark requires
// no leading whitespace on the underline; we accept up to three
// spaces for safety so an indented underline still blocks a split.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:907.
func isSetextUnderlineCandidate(line string) bool {
	// Strip leading whitespace.
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	if i == len(line) {
		return false
	}
	c := line[i]
	if c != '=' && c != '-' {
		return false
	}
	j := i
	for j < len(line) && line[j] == c {
		j++
	}
	// Allow trailing whitespace.
	for j < len(line) {
		if line[j] != ' ' && line[j] != '\t' {
			return false
		}
		j++
	}
	// Need at least one underline character.
	return j-i >= 1
}

// isHTMLBlockOpener reports whether line begins one of the seven
// CommonMark HTML block patterns. We accept up to three spaces of
// leading indentation (CommonMark rule). Matching is intentionally
// loose — we only need to know the line "looks like an HTML block
// start", not parse the contained markup.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:942.
func isHTMLBlockOpener(line string) bool {
	// Strip up to 3 spaces of indentation.
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	rest := line[i:]
	if len(rest) < 2 || rest[0] != '<' {
		return false
	}

	// Type 2: HTML comment "<!--".
	if strings.HasPrefix(rest, "<!--") {
		return true
	}
	// Type 3: processing instruction "<?".
	if strings.HasPrefix(rest, "<?") {
		return true
	}
	// Type 5: CDATA "<![CDATA[".
	if strings.HasPrefix(rest, "<![CDATA[") {
		return true
	}
	// Type 4: declaration "<!" followed by an ASCII letter.
	if len(rest) >= 3 && rest[1] == '!' && isASCIILetter(rest[2]) {
		return true
	}

	// Type 1: <script | <pre | <style | <textarea (case-insensitive)
	// followed by whitespace, '>', end-of-line, or other non-name
	// terminators.
	low := strings.ToLower(rest)
	for _, t := range []string{"<script", "<pre", "<style", "<textarea"} {
		if strings.HasPrefix(low, t) {
			next := byte(0)
			if len(low) > len(t) {
				next = low[len(t)]
			}
			if next == 0 || next == ' ' || next == '\t' || next == '>' {
				return true
			}
		}
	}

	// Types 6 & 7: open or close of a block-level tag. We collapse
	// both into a single check: the line must start with '<' or
	// '</' followed by an ASCII letter. Lines like "<3", "<-",
	// "<<", or mid-line "<foo>" do NOT trigger because we require
	// the line to *start* (after up to 3 spaces) with '<letter'
	// or '</letter'.
	j := 1 // past '<'
	if j < len(rest) && rest[j] == '/' {
		j++
	}
	if j >= len(rest) || !isASCIILetter(rest[j]) {
		return false
	}
	return true
}

// isASCIILetter reports whether b is an ASCII letter.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:1008.
func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// isLinkRefDefinition reports whether line matches a CommonMark
// link reference definition opener. The conservative pattern:
//
//	^[ ]{0,3}\[[^\]]+\]:\s*\S+
//
// i.e. up to 3 spaces, then a bracketed label (no nested ']'),
// then a colon, then whitespace, then at least one non-whitespace
// character of destination. We do not validate the destination —
// presence of a ref-def opener anywhere in the prefix is enough
// to forfeit the boundary.
//
// Source: ../crush/internal/ui/chat/streaming_markdown.go:1022.
func isLinkRefDefinition(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) || line[i] != '[' {
		return false
	}
	i++
	labelStart := i
	for i < len(line) && line[i] != ']' {
		i++
	}
	if i >= len(line) || i == labelStart {
		// No closing bracket, or empty label.
		return false
	}
	// i points at ']'.
	i++
	if i >= len(line) || line[i] != ':' {
		return false
	}
	i++
	// Skip required whitespace.
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	// At least one non-whitespace character of destination.
	return i < len(line)
}
