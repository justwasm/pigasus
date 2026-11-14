package main

import (
	"fmt"
	"strconv"
	"strings"
)

// ColorType identifies the color encoding format.
type ColorType int

const (
	ColorDefault ColorType = iota
	Color16
	Color256
	ColorRGB
)

// TerminalColor represents a parsed terminal color.
type TerminalColor struct {
	Type  ColorType
	R     uint8
	G     uint8
	B     uint8
	Index uint8
}

// Standard 16-color VGA/xterm RGB lookup table
var standard16RGB = [16][3]uint8{
	{0, 0, 0},       // 0: Black
	{205, 49, 49},   // 1: Red
	{13, 188, 121},  // 2: Green
	{229, 229, 16},  // 3: Yellow
	{36, 114, 200},  // 4: Blue
	{188, 63, 188},  // 5: Magenta
	{17, 168, 205},  // 6: Cyan
	{229, 229, 229}, // 7: White
	{102, 102, 102}, // 8: Bright Black (Gray)
	{241, 76, 76},   // 9: Bright Red
	{35, 209, 139},  // 10: Bright Green
	{245, 245, 67},  // 11: Bright Yellow
	{59, 142, 234},  // 12: Bright Blue
	{214, 112, 214}, // 13: Bright Magenta
	{41, 184, 219},  // 14: Bright Cyan
	{255, 255, 255}, // 15: Bright White
}

// Hex returns the 6-character hex representation (e.g. "#4285F4") or "default".
func (c TerminalColor) Hex() string {
	switch c.Type {
	case ColorRGB:
		return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B)
	case Color16:
		idx := c.Index % 16
		rgb := standard16RGB[idx]
		return fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
	case Color256:
		if c.Index < 16 {
			rgb := standard16RGB[c.Index]
			return fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2])
		}
		if c.Index >= 232 {
			// Grayscale 232..255 -> 24 shades
			v := uint8((int(c.Index)-232)*10 + 8)
			return fmt.Sprintf("#%02X%02X%02X", v, v, v)
		}
		// 6x6x6 color cube: 16 + 36*r + 6*g + b
		idx := int(c.Index) - 16
		r := uint8((idx / 36) * 51)
		g := uint8(((idx % 36) / 6) * 51)
		b := uint8((idx % 6) * 51)
		return fmt.Sprintf("#%02X%02X%02X", r, g, b)
	default:
		return "default"
	}
}

// ANSI returns the ANSI SGR escape sequence to activate this color.
func (c TerminalColor) ANSI(isBg bool) string {
	prefix := "38"
	if isBg {
		prefix = "48"
	}
	switch c.Type {
	case ColorRGB:
		return fmt.Sprintf("\x1b[%s;2;%d;%d;%dm", prefix, c.R, c.G, c.B)
	case Color256:
		return fmt.Sprintf("\x1b[%s;5;%dm", prefix, c.Index)
	case Color16:
		if isBg {
			if c.Index < 8 {
				return fmt.Sprintf("\x1b[%dm", 40+c.Index)
			}
			return fmt.Sprintf("\x1b[%dm", 100+c.Index-8)
		}
		if c.Index < 8 {
			return fmt.Sprintf("\x1b[%dm", 30+c.Index)
		}
		return fmt.Sprintf("\x1b[%dm", 90+c.Index-8)
	default:
		if isBg {
			return "\x1b[49m"
		}
		return "\x1b[39m"
	}
}

// CellStyle holds character-level styling attributes.
type CellStyle struct {
	Fg        TerminalColor
	Bg        TerminalColor
	Bold      bool
	Dim       bool
	Italic    bool
	Underline bool
	Reverse   bool
}

// Equal returns whether two cell styles are visually identical.
func (s CellStyle) Equal(other CellStyle) bool {
	if s.Bold != other.Bold || s.Dim != other.Dim || s.Italic != other.Italic ||
		s.Underline != other.Underline || s.Reverse != other.Reverse {
		return false
	}
	return s.Fg.Hex() == other.Fg.Hex() && s.Bg.Hex() == other.Bg.Hex()
}

// StyledCell represents a single rune with its full styling attributes.
type StyledCell struct {
	Rune  rune
	Style CellStyle
}

// ParseStyledLine parses a line containing ANSI SGR escape sequences into a slice of StyledCells.
func ParseStyledLine(line string) []StyledCell {
	var cells []StyledCell
	current := CellStyle{}

	runes := []rune(line)
	n := len(runes)

	for i := 0; i < n; i++ {
		r := runes[i]
		if r == '\x1b' && i+1 < n && runes[i+1] == '[' {
			// Find end of ANSI sequence
			end := i + 2
			for end < n && !isFinalAnsiByte(runes[end]) {
				end++
			}
			if end < n {
				cmd := runes[end]
				seq := string(runes[i+2 : end])
				if cmd == 'm' {
					applySGR(&current, seq)
				}
				i = end
				continue
			}
		}

		cells = append(cells, StyledCell{
			Rune:  r,
			Style: current,
		})
	}

	return cells
}

func isFinalAnsiByte(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '~'
}

func applySGR(style *CellStyle, paramsStr string) {
	if paramsStr == "" || paramsStr == "0" {
		*style = CellStyle{}
		return
	}

	parts := strings.Split(paramsStr, ";")
	var params []int
	for _, p := range parts {
		if p == "" {
			params = append(params, 0)
			continue
		}
		val, _ := strconv.Atoi(p)
		params = append(params, val)
	}

	for i := 0; i < len(params); i++ {
		p := params[i]
		switch {
		case p == 0:
			*style = CellStyle{}
		case p == 1:
			style.Bold = true
		case p == 2:
			style.Dim = true
		case p == 3:
			style.Italic = true
		case p == 4:
			style.Underline = true
		case p == 7:
			style.Reverse = true
		case p == 22:
			style.Bold = false
			style.Dim = false
		case p == 23:
			style.Italic = false
		case p == 24:
			style.Underline = false
		case p == 27:
			style.Reverse = false

		case p >= 30 && p <= 37:
			style.Fg = TerminalColor{Type: Color16, Index: uint8(p - 30)}
		case p == 39:
			style.Fg = TerminalColor{Type: ColorDefault}
		case p >= 40 && p <= 47:
			style.Bg = TerminalColor{Type: Color16, Index: uint8(p - 40)}
		case p == 49:
			style.Bg = TerminalColor{Type: ColorDefault}

		case p >= 90 && p <= 97:
			style.Fg = TerminalColor{Type: Color16, Index: uint8(p - 90 + 8)}
		case p >= 100 && p <= 107:
			style.Bg = TerminalColor{Type: Color16, Index: uint8(p - 100 + 8)}

		case p == 38: // Extended foreground color
			if i+1 < len(params) {
				if params[i+1] == 5 && i+2 < len(params) {
					// 38;5;<n>
					style.Fg = TerminalColor{Type: Color256, Index: uint8(params[i+2])}
					i += 2
				} else if params[i+1] == 2 && i+4 < len(params) {
					// 38;2;<r>;<g>;<b>
					style.Fg = TerminalColor{
						Type: ColorRGB,
						R:    uint8(params[i+2]),
						G:    uint8(params[i+3]),
						B:    uint8(params[i+4]),
					}
					i += 4
				}
			}

		case p == 48: // Extended background color
			if i+1 < len(params) {
				if params[i+1] == 5 && i+2 < len(params) {
					// 48;5;<n>
					style.Bg = TerminalColor{Type: Color256, Index: uint8(params[i+2])}
					i += 2
				} else if params[i+1] == 2 && i+4 < len(params) {
					// 48;2;<r>;<g>;<b>
					style.Bg = TerminalColor{
						Type: ColorRGB,
						R:    uint8(params[i+2]),
						G:    uint8(params[i+3]),
						B:    uint8(params[i+4]),
					}
					i += 4
				}
			}
		}
	}
}

// StyleMismatch describes a styling difference at a specific position.
type StyleMismatch struct {
	Pos        int
	Char       rune
	LeftStyle  CellStyle
	RightStyle CellStyle
	Reason     string
}

// CompareStyledLines compares two parsed cell arrays and returns any styling mismatches.
func CompareStyledLines(left, right []StyledCell) []StyleMismatch {
	minLen := len(left)
	if len(right) < minLen {
		minLen = len(right)
	}

	var mismatches []StyleMismatch
	for i := 0; i < minLen; i++ {
		l := left[i]
		r := right[i]

		if l.Rune != r.Rune {
			// Character difference, handled by text diff
			continue
		}

		if !l.Style.Equal(r.Style) {
			reason := ""
			if l.Style.Fg.Hex() != r.Style.Fg.Hex() {
				reason = fmt.Sprintf("fg: %s vs %s", l.Style.Fg.Hex(), r.Style.Fg.Hex())
			} else if l.Style.Bg.Hex() != r.Style.Bg.Hex() {
				reason = fmt.Sprintf("bg: %s vs %s", l.Style.Bg.Hex(), r.Style.Bg.Hex())
			} else if l.Style.Bold != r.Style.Bold {
				reason = fmt.Sprintf("bold: %v vs %v", l.Style.Bold, r.Style.Bold)
			} else if l.Style.Dim != r.Style.Dim {
				reason = fmt.Sprintf("dim: %v vs %v", l.Style.Dim, r.Style.Dim)
			} else {
				reason = "attributes mismatch"
			}

			mismatches = append(mismatches, StyleMismatch{
				Pos:        i,
				Char:       l.Rune,
				LeftStyle:  l.Style,
				RightStyle: r.Style,
				Reason:     reason,
			})
		}
	}

	return mismatches
}

// StyledCellsToHTML renders cells into an HTML snippet with inline CSS styling.
func StyledCellsToHTML(cells []StyledCell) string {
	var b strings.Builder

	escapeChar := func(r rune) string {
		switch r {
		case '<':
			return "&lt;"
		case '>':
			return "&gt;"
		case '&':
			return "&amp;"
		case '"':
			return "&quot;"
		case ' ':
			return "&nbsp;"
		default:
			return string(r)
		}
	}

	buildStyleCSS := func(s CellStyle) string {
		var styles []string
		if s.Fg.Type != ColorDefault {
			styles = append(styles, fmt.Sprintf("color:%s", s.Fg.Hex()))
		}
		if s.Bg.Type != ColorDefault {
			styles = append(styles, fmt.Sprintf("background-color:%s", s.Bg.Hex()))
		}
		if s.Bold {
			styles = append(styles, "font-weight:bold")
		}
		if s.Dim {
			styles = append(styles, "opacity:0.65")
		}
		if s.Italic {
			styles = append(styles, "font-style:italic")
		}
		if s.Underline {
			styles = append(styles, "text-decoration:underline")
		}
		return strings.Join(styles, ";")
	}

	i := 0
	for i < len(cells) {
		currentStyle := cells[i].Style
		css := buildStyleCSS(currentStyle)

		// Collect run of characters with identical style
		var textRun strings.Builder
		for i < len(cells) && cells[i].Style.Equal(currentStyle) {
			textRun.WriteString(escapeChar(cells[i].Rune))
			i++
		}

		if css != "" {
			b.WriteString(fmt.Sprintf(`<span style="%s">%s</span>`, css, textRun.String()))
		} else {
			b.WriteString(textRun.String())
		}
	}

	return b.String()
}

// RenderHTMLComparison generates a standalone side-by-side HTML file highlighting visual & color parity.
func RenderHTMLComparison(leftRaw, rightRaw, label string) string {
	leftLines := strings.Split(strings.TrimRight(leftRaw, "\n"), "\n")
	rightLines := strings.Split(strings.TrimRight(rightRaw, "\n"), "\n")

	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}

	var rows strings.Builder
	for i := 0; i < maxLines; i++ {
		var lRaw, rRaw string
		if i < len(leftLines) {
			lRaw = leftLines[i]
		}
		if i < len(rightLines) {
			rRaw = rightLines[i]
		}

		lCells := ParseStyledLine(lRaw)
		rCells := ParseStyledLine(rRaw)

		lHTML := StyledCellsToHTML(lCells)
		rHTML := StyledCellsToHTML(rCells)

		mismatches := CompareStyledLines(lCells, rCells)
		statusClass := "match"
		badge := "✓"
		// Banner rows are judged on their text, not on the product's own
		// logo art — the same rule the text report applies (see
		// diff.go bannerText), so the two reports never disagree.
		lBanner, lIsBanner := bannerText(StripANSI(lRaw))
		rBanner, rIsBanner := bannerText(StripANSI(rRaw))
		switch {
		case lIsBanner && rIsBanner:
			if lBanner != rBanner {
				statusClass = "text-diff"
				badge = "✗ text"
			}
		case StripANSI(lRaw) != StripANSI(rRaw):
			statusClass = "text-diff"
			badge = "✗ text"
		case len(mismatches) > 0:
			statusClass = "color-diff"
			badge = "✗ color"
		}

		rows.WriteString(fmt.Sprintf(`
      <tr class="%s">
        <td class="ln">%d</td>
        <td class="badge">%s</td>
        <td class="pane">%s</td>
        <td class="pane">%s</td>
      </tr>`, statusClass, i+1, badge, lHTML, rHTML))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>tuialign: %s</title>
<style>
  body { background: #1e1e1e; color: #d4d4d4; font-family: ui-monospace, Menlo, Monaco, monospace; margin: 20px; }
  h1 { font-size: 1.2rem; color: #61afef; margin-bottom: 8px; }
  table { border-collapse: collapse; width: 100%%; table-layout: fixed; }
  th { background: #252526; color: #9cdcfe; text-align: left; padding: 8px; border-bottom: 2px solid #333; font-size: 0.85rem; }
  td { padding: 2px 8px; vertical-align: top; font-size: 0.85rem; line-height: 1.35; white-space: pre-wrap; word-break: break-all; }
  td.ln { width: 40px; color: #5c6370; text-align: right; user-select: none; }
  td.badge { width: 65px; font-weight: bold; font-size: 0.75rem; text-align: center; }
  tr.match td.badge { color: #98c379; }
  tr.color-diff { background: rgba(229, 192, 123, 0.12); }
  tr.color-diff td.badge { color: #e5c07b; }
  tr.text-diff { background: rgba(224, 108, 117, 0.15); }
  tr.text-diff td.badge { color: #e06c75; }
  td.pane { border-left: 1px solid #333; font-family: monospace; }
</style>
</head>
<body>
  <h1>tuialign Visual Parity Report: %s</h1>
  <table>
    <thead>
      <tr>
        <th class="ln">#</th>
        <th class="badge">Parity</th>
        <th>Reference (agy 1.5.8)</th>
        <th>Target (pigasus)</th>
      </tr>
    </thead>
    <tbody>
      %s
    </tbody>
  </table>
</body>
</html>
`, label, label, rows.String())
}

