package render

import (
	"strings"
)

// Hyperlink returns a clickable link, OSC 8-framed when caps.OSC8 is
// true, plain-text "label (url)" otherwise.
//
// Shape when OSC 8 enabled (the canonical "click here" hyperlink):
//
//	ESC ] 8 ; ; URL ESC \ TEXT ESC ] 8 ; ; ESC \
//
// Shape when OSC 8 disabled (the AGY 1.4.0 fallback path —
// recovered from the strings table row "Fixed links printing as raw
// escape sequences on terminals that do not implement OSC 8
// hyperlinks, such as Terminal.app, and extended the same detection
// to command output and alert bodies, which still emitted
// hyperlinks unconditionally"):
//
//	LABEL (URL)
//
// Cycle terminal-rendering-bbt-v209: bubbletea v2's internal
// hyperlink emitter can drop the closing ST when the text overflows
// the column width, which leaks the underline attribute into the
// next line — the "OSC 8 underflow" symptom. Hyperlink clamps the
// text to maxWidth before emitting the closing ST so the OSC 8
// frame always terminates even if the caller passed an overlong
// label. maxWidth <= 0 disables the clamp (use only when the caller
// has already validated the width).
//
// Cycle osc-hyperlinks (cycle 2 of the OSC pair): the function
// signature is keyed on caps (HyperlinkCaps). Production callers
// thread caps from cmd/wagyu/main.go's start-up path; the linked
// cosmetics (text width, OSC 8 frame shape, clamping) are unchanged
// for the OSC 8 enabled branch.
//
// If url is empty, Hyperlink returns text unchanged — the
// no-hyperlink case is a plain string and the OSC 8 frame would just
// produce two ESC ]8;;ST sequences around the text. The cycle-218
// markdown renderer uses this escape hatch.
func Hyperlink(url, text string, maxWidth int, caps HyperlinkCaps) string {
	if url == "" {
		return text
	}
	if !caps.OSC8 {
		// Plain-text fallback: AGY 1.4.0 emits "<label> (<url>)" so
		// the URL is still visible (and copy-pasteable) when the
		// terminal cannot parse OSC 8. The space-and-paren form
		// matches the recovered Terminal detection log's
		// "osc8=false" branch.
		return text + " (" + url + ")"
	}
	body := text
	if maxWidth > 0 {
		w := cellWidth(body)
		if w > maxWidth {
			body = truncateToCells(body, maxWidth)
		}
	}
	// ESC ] 8 ; ; URL ST   TEXT   ESC ] 8 ; ; ST
	const osc = "\x1b]8;;"
	const st = "\x1b\\"
	// Cycle osc8-semicolon-1.4.1 (cycle 240): percent-encode `;` and
	// `\\` in the URI segment before embedding it in the OSC 8 frame.
	// Some terminals / multiplexers (tmux without hyperlink-passthrough,
	// the GNU screen fork, certain web-based emulators) split OSC
	// sequences at the first `;` they see inside the URI, which would
	// otherwise truncate the URL at e.g. `https://example.com/?a=1;b=2`
	// to `https://example.com/?a=1` and silently drop the query string.
	// The label text (body) is left verbatim — semicolons in display
	// text are not an OSC 8 framing hazard. See
	// internal/render/url_osc8_encode.go for the full rationale and
	// the explicit non-goals (no full RFC 3986 pass).
	urlForOSC := urlForOSC8(url)
	var b strings.Builder
	b.Grow(len(osc)*2 + len(urlForOSC) + len(body) + len(st)*2 + 4)
	b.WriteString(osc)
	b.WriteString(urlForOSC)
	b.WriteString(st)
	b.WriteString(body)
	b.WriteString(osc)
	b.WriteString(st)
	return b.String()
}
