// Package urlchunk renders a long URL as a wrapped block of terminal
// rows.
//
// Two surfaces show a URL that is far longer than a terminal line: the
// add-account screen (internal/loginui) and the /remote-control row of
// the transcript (internal/cliapp). Both show it the same way — the URL
// is cut into chunks, one chunk per row, and every chunk carries the
// *complete* URL as its OSC 8 hyperlink target.
//
// The renderer draws the view into a cell buffer the size of the
// terminal, so a line longer than the terminal is *clipped*, not soft
// wrapped: the tail of a long line simply disappears. A URL left on
// one line is therefore a URL whose tail the user cannot see, cannot
// select and cannot click. Cutting it here is what makes the whole
// link reachable.
//
// The geometry is the reference's own, measured first-hand against the
// 1.4.6 binary at 40 and 80 columns: the URL is hard-wrapped at the
// width, and every chunk of the wrap is a hyperlink to the complete
// URL.
package urlchunk

import (
	"github.com/justwasm/pigasus/render"
)

// StyleOpen and StyleClose paint a link: blue with an underline.
//
// They are the add-account screen's own link style — the reference
// draws its authorization URL that way — and the two surfaces share
// them so the two displays are the same bytes.
const (
	StyleOpen  = "\x1b[34;4m"
	StyleClose = "\x1b[m"
)

// Chunks splits url into chunks of at most width characters.
//
// The URL is one unbroken run of characters, so the break points are
// arbitrary character boundaries — the reference breaks it the same
// way (".../oauth2/a" / "uth?access_type=offline&...").
//
// An empty url gives one empty chunk. A width below one is treated as
// one, so the chunking always makes progress.
func Chunks(url string, width int) []string {
	if width < 1 {
		width = 1
	}
	runes := []rune(url)
	if len(runes) == 0 {
		return []string{""}
	}
	var out []string
	for len(runes) > width {
		out = append(out, string(runes[:width]))
		runes = runes[width:]
	}
	return append(out, string(runes))
}

// HyperlinkChunk renders one chunk of a URL as an OSC 8 hyperlink to
// the complete URL.
//
// The frame opens, the link style opens, the chunk, the style closes,
// the frame closes:
//
//	ESC ] 8 ; ; URL ESC \ ESC [ 34;4m CHUNK ESC [ m ESC ] 8 ; ; ESC \
//
// The target is the *complete* URL, not the chunk: a cmd+click (or a
// tap) on any part of the displayed URL opens the whole link.
//
// The URL is percent-encoded for the OSC 8 frame by render.Hyperlink
// (";" and "\" only) — a terminal that splits an OSC sequence at the
// first ";" would otherwise truncate the link.
func HyperlinkChunk(url, chunk string) string {
	return render.Hyperlink(url, StyleOpen+chunk+StyleClose, 0, render.HyperlinkCaps{OSC8: true})
}

// Rows renders url as one row per chunk.
//
// Every row starts with indent, and every chunk is a hyperlink to the
// complete URL.
//
// When the terminal does not speak OSC 8 the rows carry the plain
// text. render.Hyperlink's own fallback — "label (url)" — would
// repeat the complete URL on every row; the URL is already on screen,
// row after row, so the plain chunk is the right fallback here.
func Rows(url, indent string, width int, caps render.HyperlinkCaps) []string {
	chunks := Chunks(url, width)
	rows := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if caps.OSC8 {
			rows = append(rows, indent+HyperlinkChunk(url, chunk))
			continue
		}
		rows = append(rows, indent+chunk)
	}
	return rows
}
