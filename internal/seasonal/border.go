package seasonal

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Border describes the themed prompt-box rule: a 40-cell segment made of
// a plain run, a face motif and the remainder of the plain run, with the
// plain run's colour cycling through a gradient.
//
// 【一手】 agy 1.4.7 with the Halloween event on
// (out/changelog-147/ref/seasonal-prompt.txt, the two rules around the
// prompt box):
//
//	──────────────────/^v^\───────────────────────────────────\\|//──…
//
// The top rule's five segments place their motif 18 cells in (17 for the
// wider motif), the bottom rule's 28 cells in (27 for the wider motif);
// both cycle the gradient once per segment and both start their gradient
// at the first colour. The bottom rule starts one motif later than the
// top rule.
type Border struct {
	// Gradient holds the plain run's colours, cycled per segment.
	Gradient [][3]byte
	// Motifs are the face motifs, cycled per segment.
	Motifs []string
	// MotifColors holds each motif's own colour, parallel to Motifs.
	MotifColors [][3]byte
	// Segment is the segment width in cells.
	Segment int
	// TopTrailing and BottomTrailing are the plain cells that follow the
	// motif inside a segment.
	TopTrailing, BottomTrailing int
	// TopStart and BottomStart are the first motif's index in the cycle.
	TopStart, BottomStart int
}

// Rule renders one themed prompt-box rule of the given width.
//
// top selects the box's top rule; the bottom rule uses the other phase.
func (b *Border) Rule(width int, top bool) string {
	if width <= 0 || len(b.Motifs) == 0 || len(b.Gradient) == 0 {
		return ""
	}
	trailing := b.BottomTrailing
	start := b.BottomStart
	if top {
		trailing = b.TopTrailing
		start = b.TopStart
	}
	var out strings.Builder
	cell := 0
	seg := 0
	for cell < width {
		motif := b.Motifs[(start+seg)%len(b.Motifs)]
		colour := b.Gradient[seg%len(b.Gradient)]
		lead := b.Segment - trailing - utf8.RuneCountInString(motif)
		if lead < 0 {
			lead = 0
		}
		run := lead
		if cell+run > width {
			run = width - cell
		}
		if run > 0 {
			out.WriteString(fg(&colour))
			out.WriteString(strings.Repeat("─", run))
			cell += run
		}
		if cell >= width {
			break
		}
		mc := b.MotifColors[(start+seg)%len(b.MotifColors)]
		out.WriteString(fg(&mc))
		out.WriteString(motif)
		cell += utf8.RuneCountInString(motif)
		if cell >= width {
			break
		}
		run = trailing
		if cell+run > width {
			run = width - cell
		}
		out.WriteString(fg(&colour))
		out.WriteString(strings.Repeat("─", run))
		cell += run
		seg++
	}
	out.WriteString(reset())
	return out.String()
}

// halloweenSpinner is the moon-phase spinner the reference shows while a
// turn runs with the Halloween event on.
//
// 【一手】 agy 1.4.7 (the "◓  Generating…" row, out/changelog-147/ref/turn/)
// and the binary's string table, which holds the four frames in this
// order.
//
// Each frame carries its own trailing space: the reference's row reads
// "◐  Generating…" — glyph, space, space, text — and the string table
// holds the four frames as four four-byte literals, "◐ ", "◓ ", "◑ ",
// "◒ ", laid out back to back.
var halloweenSpinner = []string{"◐ ", "◓ ", "◑ ", "◒ "}

// halloweenBorder is the themed prompt-box rule.
//
// 【一手】 agy 1.4.7, measured on the live themed session at 200 and at
// 120 columns (out/changelog-147/ref/seasonal-prompt.txt and
// out/changelog-147/ref/border-120.txt).
var halloweenBorder = Border{
	Gradient: [][3]byte{
		{194, 65, 12},
		{173, 59, 63},
		{152, 53, 115},
		{130, 46, 166},
		{109, 40, 217},
	},
	Motifs:      []string{"/^v^\\", "\\\\|//", "/\\oo/\\"},
	MotifColors: [][3]byte{{78, 46, 150}, {138, 90, 11}, {38, 112, 58}},
	Segment:     40,
	// The top rule leaves 17 cells after its motif, the bottom rule 7.
	TopTrailing:    17,
	BottomTrailing: 7,
	// The bottom rule starts one motif later than the top rule.
	TopStart:    0,
	BottomStart: 1,
}

// SpinnerFrames returns the spinner's frames for the event, or nil when
// the event has none.
func (e *Event) SpinnerFrames() []string {
	if e == nil {
		return nil
	}
	return e.Spinner
}

// String renders the event's id, for logs.
func (e *Event) String() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("seasonal.Event(%s)", e.ID)
}
