// safe_boundary_ast.go — cycle 242 AST construct and hazard detectors.
//
// Separated from safe_boundary.go to keep both files modular and <= 500 lines.
// Contains CommonMark construct detectors: loose list continuations,
// HTML blocks, link reference definitions, and open block constructs.
package mdrender

import "strings"

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

	// Type 2, 3, 5: comment "<!--", processing instruction "<?", CDATA "<![CDATA[".
	if strings.HasPrefix(rest, "<!--") || strings.HasPrefix(rest, "<?") || strings.HasPrefix(rest, "<![CDATA[") {
		return true
	}
	// Type 4: declaration "<!" followed by an ASCII letter.
	if len(rest) >= 3 && rest[1] == '!' && isASCIILetter(rest[2]) {
		return true
	}

	// Type 1: <script, <pre, <style, <textarea
	if isHTMLType1Tag(strings.ToLower(rest)) {
		return true
	}

	// Types 6 & 7: open or close of a block-level tag. We collapse
	// both into a single check: the line must start with '<' or
	// '</' followed by an ASCII letter.
	j := 1 // past '<'
	if j < len(rest) && rest[j] == '/' {
		j++
	}
	if j >= len(rest) || !isASCIILetter(rest[j]) {
		return false
	}
	return true
}

// isHTMLType1Tag reports whether low (lower-cased line starting with '<')
// begins one of the Type 1 tags followed by a valid terminator.
func isHTMLType1Tag(low string) bool {
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
	return false
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
