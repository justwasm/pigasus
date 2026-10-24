// wrap.go — the login screen's line wrapping.
//
// The renderer draws the view into a cell buffer the size of the
// terminal, so a line longer than the terminal is *clipped*, not soft
// wrapped: the tail of every long line simply disappears. The
// authorization URL is ~700 characters long, so on a phone-sized
// terminal (or any window narrower than the URL) the visible part
// stopped after a few dozen characters and the link could not be
// copied at all.
//
// The reference wraps the URL instead, into chunks exactly as wide as
// the terminal, and makes every chunk a hyperlink to the *complete*
// URL. The clone does the same.
//
// The geometry is the reference's, measured first-hand against the
// 1.4.6 binary at 40 and 80 columns:
//
//   - prose is indented by one column and wrapped at width-1;
//   - the URL is indented by one column and hard-wrapped at width-2,
//     so every line is width-1 cells wide.
package loginui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// minWidth is the narrowest the screen is laid out for.
const minWidth = 8

// contentWidth is the width the screen is laid out at.
func (m *Model) contentWidth() int {
	w := m.Width
	if w <= 0 {
		w = 80
	}
	if w < minWidth {
		w = minWidth
	}
	return w
}

// wrapProse wraps a line of prose at the terminal width.
//
// The line's indent is kept on the continuation lines, the way the
// reference's wrap does: at 40 columns
// "Your browser should open automatically. If not:" becomes
//
//	 Your browser should open automatically.
//	 If not:
func wrapProse(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	indent := text[:len(text)-len(strings.TrimLeft(text, " "))]
	body := strings.TrimLeft(text, " ")
	limit := width - len(indent)
	if limit < 1 {
		limit = 1
	}
	parts := strings.Split(ansi.Wrap(body, limit, " "), "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, indent+p)
	}
	return out
}

// urlChunks splits the authorization URL into the chunks the
// reference shows: one column of indent and width-2 characters of URL
// per line.
//
// The URL is one unbroken run of characters, so the break points are
// arbitrary character boundaries — the reference breaks it the same
// way (".../oauth2/a" / "uth?access_type=offline&...").
func urlChunks(url string, width int) []string {
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
