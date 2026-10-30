package seasonal

import (
	"fmt"
	"strings"
)

// Art is a banner art block: a raster of palette-indexed sub-pixels, two
// per terminal line, rendered with the half-block glyphs the reference
// uses for its logo.
//
// 【一手】 agy 1.4.7's Halloween banner (out/changelog-147/ref/seasonal-prompt.txt)
// is a half-block raster like the normal logo: each cell paints the top
// sub-pixel with the foreground colour and the bottom one with the
// background colour.
type Art struct {
	// Name labels the scene: "pumpkin", "moon", "ghost", "witch-hat"
	// or "bat".
	//
	// 【推测】 The reference does not name its scenes — the binary
	// carries no scene table — so these labels are the clone's own,
	// read off the pictures.
	Name string
	// Palette maps an index to an RGB triple. Index 0 is transparent.
	Palette [][3]byte
	// Width is the art block's width in terminal cells.
	Width int
	// Rows holds the sub-pixel rows, two per terminal line.
	Rows [][]byte
}

// Lines renders the art block as terminal lines.
func (a Art) Lines() []string {
	if len(a.Rows) == 0 {
		return nil
	}
	lines := make([]string, 0, len(a.Rows)/2)
	for i := 0; i+1 < len(a.Rows); i += 2 {
		lines = append(lines, a.line(a.Rows[i], a.Rows[i+1]))
	}
	return lines
}

// line renders one terminal line from its two sub-pixel rows.
//
// 【一手】 The reference paints each cell with its own colour pair and
// closes the pair again right after the glyph, so a cell reads
// "fg bg ▀ 39 49".
//
// A half block paints its *filled* half with the foreground colour and
// leaves the other half to the background: "▀" is the upper half, "▄"
// the lower one. A cell whose bottom sub-pixel alone carries a colour
// is therefore "fg ▄ 39" — the colour sits in the foreground, exactly
// as the reference's own capture reads
// "\x1b[38;2;195;166;255m▄▄" (out/changelog-147/ref/config-seasonal-on.txt).
func (a Art) line(top, bot []byte) string {
	var b strings.Builder
	for x := 0; x < a.Width; x++ {
		t := a.pixel(top, x)
		bo := a.pixel(bot, x)
		switch {
		case t != nil && bo != nil:
			b.WriteString(fg(t))
			b.WriteString(bg(bo))
			b.WriteString("▀")
			b.WriteString("\x1b[39m\x1b[49m")
		case t != nil:
			b.WriteString(fg(t))
			b.WriteString("▀")
			b.WriteString("\x1b[39m")
		case bo != nil:
			b.WriteString(fg(bo))
			b.WriteString("▄")
			b.WriteString("\x1b[39m")
		default:
			b.WriteString(" ")
		}
	}
	return b.String()
}

// pixel returns the colour of one sub-pixel cell, or nil when the cell
// is transparent.
func (a Art) pixel(row []byte, x int) *[3]byte {
	if x >= len(row) {
		return nil
	}
	idx := row[x]
	if idx == 0 || int(idx) >= len(a.Palette) {
		return nil
	}
	c := a.Palette[idx]
	return &c
}

// fg opens a 24-bit foreground colour.
func fg(c *[3]byte) string {
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c[0], c[1], c[2])
}

// bg opens a 24-bit background colour.
func bg(c *[3]byte) string {
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c[0], c[1], c[2])
}

// reset closes every attribute the art opened.
func reset() string { return "\x1b[0m" }
