// glamour_transforms.go — AST transforms, link formatters, newline preservation,
// and SGR/OSC 8 escape code post-processing for internal/mdrender.
package mdrender

import (
	"regexp"
	"strings"
)

// osc8Re matches an OSC 8 hyperlink open/close token. glamour emits
// these for every link; the AGY 1.4.2 capture contains none (the URL
// is printed as underlined 36 text after the label instead), so the
// ANSI transcript strips them to stay byte-aligned and to keep row
// height math from seeing zero-width control strings. Explicit
// clickable surfaces (elig_inline.go) still build OSC 8 through
// render.Hyperlink.
var osc8Re = regexp.MustCompile(`\x1b\]8;[^\x1b\x07]*(?:\x07|\x1b\\)`)

// stripOSC8 removes OSC 8 hyperlink sequences from rendered markdown output.
func stripOSC8(s string) string {
	return osc8Re.ReplaceAllString(s, "")
}

// parseSGRSequence checks if s[i:] begins an ANSI SGR escape sequence (ESC [ ... m).
// If matched, it returns the full sequence, the parameter string between '[' and 'm',
// the index immediately following 'm', and true.
func parseSGRSequence(s string, i int) (seq, params string, end int, ok bool) {
	if s[i] != '\x1b' || i+1 >= len(s) || s[i+1] != '[' {
		return "", "", i, false
	}
	j := i + 2
	for j < len(s) && (s[j] == ';' || (s[j] >= '0' && s[j] <= '9')) {
		j++
	}
	if j < len(s) && s[j] == 'm' {
		return s[i : j+1], s[i+2 : j], j + 1, true
	}
	return "", "", i, false
}

// coalesceSGR collapses glamour's per-run "reset then re-apply the same
// style" idiom. glamour's ANSI writer wraps every text run (and every
// padding space) in its own escape pair, so a single coloured paragraph
// reaches us as
//
//	ESC[38;5;23mfirstESC[m ESC[38;5;23m lineESC[m ...
//
// The reference binary emits the same visual result as one run per line
// (ESC[38;5;23mfirst lineESC[39m). Coalescing is not cosmetic: the
// interrupt-free text is what makes RestoreMermaidAndMath's
// strings.Contains(key) and the transcript tests' substring assertions
// work, and it shrinks a 120-col reply from ~60 KB to a few hundred
// bytes.
//
// The pass is conservative. It only drops a reset plus the escape(s)
// that immediately follow it when those escapes re-apply exactly the
// style that was active before the reset. Anything that changes the
// active attributes is emitted verbatim, so a bold run followed by a
// non-bold run still carries the intermediate reset.
func coalesceSGR(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	cur := ""  // escape sequences active since the last reset
	prev := "" // cur captured at the last reset
	pendingReset := false
	flushReset := func() {
		if pendingReset {
			out.WriteString("\x1b[m")
			pendingReset = false
			cur = ""
		}
	}
	for i := 0; i < len(s); {
		seq, params, nextI, ok := parseSGRSequence(s, i)
		if ok {
			if params == "" || params == "0" {
				prev = cur
				cur = ""
				pendingReset = true
				i = nextI
				continue
			}
			cur += seq
			if pendingReset && cur == prev {
				pendingReset = false
				cur = prev
				i = nextI
				continue
			}
			if pendingReset {
				out.WriteString("\x1b[m")
				pendingReset = false
			}
			out.WriteString(seq)
			i = nextI
			continue
		}
		flushReset()
		out.WriteByte(s[i])
		i++
	}
	flushReset()
	return out.String()
}

// transformRenderOutput applies variant-specific post-processing to glamour's output string:
// stripping OSC 8 hyperlinks and coalescing SGR escapes for ANSI/User, restoring plain markers
// and trimming whitespace for Plain/Quiet/User, or trimming the lone trailing newline for ANSI.
func transformRenderOutput(v mdVariant, out string) string {
	if v == variantANSI || v == variantUser {
		out = stripOSC8(out)
		out = coalesceSGR(out)
	}
	if v == variantPlain || v == variantQuiet || v == variantUser {
		out = postProcessPlain(out)
	} else {
		// Cycle 269: ANSI variant also trims the lone trailing
		// newline glamour appends so a reply with N content
		// lines renders to N rows, not N+1. Pre-cycle-269 the
		// trailing newline was kept, so splitting
		// "Para\n\nPara\n" produced [Para, blank, Para, ""]
		// — an extra empty row at the end of the transcript.
		out = strings.TrimRight(out, "\n")
	}
	return out
}
