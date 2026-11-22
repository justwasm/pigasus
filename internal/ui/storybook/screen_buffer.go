package storybook

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/muesli/termenv"
	"github.com/vito/midterm"
)

// CellAttribute represents the visual display properties of a single terminal character cell.
type CellAttribute struct {
	Fg        string `json:"fg,omitempty"`
	Bg        string `json:"bg,omitempty"`
	Bold      bool   `json:"bold,omitempty"`
	Reverse   bool   `json:"reverse,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Faint     bool   `json:"faint,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
}

// IsZero returns true if no styling or color attributes are applied.
func (a CellAttribute) IsZero() bool {
	return a.Fg == "" && a.Bg == "" && !a.Bold && !a.Reverse && !a.Underline && !a.Faint && !a.Italic
}

// Equals returns true if two cell attributes are identical.
func (a CellAttribute) Equals(o CellAttribute) bool {
	return a == o
}

// String returns a compact representation of the attributes, e.g. "fg=ansi:4,bold".
func (a CellAttribute) String() string {
	if a.IsZero() {
		return "-"
	}
	var parts []string
	if a.Fg != "" {
		parts = append(parts, "fg="+a.Fg)
	}
	if a.Bg != "" {
		parts = append(parts, "bg="+a.Bg)
	}
	if a.Bold {
		parts = append(parts, "bold")
	}
	if a.Reverse {
		parts = append(parts, "rev")
	}
	if a.Underline {
		parts = append(parts, "ul")
	}
	if a.Faint {
		parts = append(parts, "faint")
	}
	if a.Italic {
		parts = append(parts, "italic")
	}
	return strings.Join(parts, ",")
}

// parseCellAttribute parses a string formatted by CellAttribute.String().
func parseCellAttribute(s string) CellAttribute {
	var a CellAttribute
	if s == "" || s == "-" {
		return a
	}
	tokens := strings.Split(s, ",")
	for _, tok := range tokens {
		tok = strings.TrimSpace(tok)
		switch {
		case strings.HasPrefix(tok, "fg="):
			a.Fg = strings.TrimPrefix(tok, "fg=")
		case strings.HasPrefix(tok, "bg="):
			a.Bg = strings.TrimPrefix(tok, "bg=")
		case tok == "bold":
			a.Bold = true
		case tok == "rev":
			a.Reverse = true
		case tok == "ul":
			a.Underline = true
		case tok == "faint":
			a.Faint = true
		case tok == "italic":
			a.Italic = true
		}
	}
	return a
}

// ScreenBuffer represents an absolute 2D terminal character grid and attribute matrix.
type ScreenBuffer struct {
	Width  int               `json:"width"`
	Height int               `json:"height"`
	Text   [][]rune          `json:"text"`  // [Height][Width]rune
	Attrs  [][]CellAttribute `json:"attrs"` // [Height][Width]CellAttribute
}

// NewScreenBuffer allocates a blank screen buffer with default dimensions.
func NewScreenBuffer(width, height int) *ScreenBuffer {
	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}

	text := make([][]rune, height)
	attrs := make([][]CellAttribute, height)
	for y := 0; y < height; y++ {
		rowRunes := make([]rune, width)
		for x := range rowRunes {
			rowRunes[x] = ' '
		}
		text[y] = rowRunes
		attrs[y] = make([]CellAttribute, width)
	}

	return &ScreenBuffer{
		Width:  width,
		Height: height,
		Text:   text,
		Attrs:  attrs,
	}
}

// TextMatrix returns the underlying 2D pure text character grid [H][W]rune.
func (sb *ScreenBuffer) TextMatrix() [][]rune {
	return sb.Text
}

// AttributeMatrix returns the underlying 2D attribute grid [H][W]CellAttribute.
func (sb *ScreenBuffer) AttributeMatrix() [][]CellAttribute {
	return sb.Attrs
}

// TextAt returns the character rune at row y and column x.
func (sb *ScreenBuffer) TextAt(y, x int) rune {
	if y < 0 || y >= sb.Height || x < 0 || x >= sb.Width {
		return ' '
	}
	return sb.Text[y][x]
}

// AttrAt returns the cell styling attribute at row y and column x.
func (sb *ScreenBuffer) AttrAt(y, x int) CellAttribute {
	if y < 0 || y >= sb.Height || x < 0 || x >= sb.Width {
		return CellAttribute{}
	}
	return sb.Attrs[y][x]
}

// RowText returns the plain text of row y as a string.
func (sb *ScreenBuffer) RowText(y int) string {
	if y < 0 || y >= sb.Height {
		return ""
	}
	return string(sb.Text[y])
}

// RenderText returns the entire text matrix joined by newlines.
func (sb *ScreenBuffer) RenderText() string {
	var lines []string
	for y := 0; y < sb.Height; y++ {
		lines = append(lines, sb.RowText(y))
	}
	return strings.Join(lines, "\n")
}

// RenderAttributes returns a human-readable run-length span summary of styled cells per row.
func (sb *ScreenBuffer) RenderAttributes() string {
	var sbOut strings.Builder
	for y := 0; y < sb.Height; y++ {
		spans := sb.rowAttributeSpans(y)
		if len(spans) == 0 {
			sbOut.WriteString(fmt.Sprintf("%02d: (plain)\n", y))
			continue
		}
		var spanStrs []string
		for _, sp := range spans {
			spanStrs = append(spanStrs, fmt.Sprintf("[%d:%d] %s", sp.start, sp.end, sp.attr.String()))
		}
		sbOut.WriteString(fmt.Sprintf("%02d: %s\n", y, strings.Join(spanStrs, " | ")))
	}
	return sbOut.String()
}

type attrSpan struct {
	start int
	end   int
	attr  CellAttribute
}

func (sb *ScreenBuffer) rowAttributeSpans(y int) []attrSpan {
	if y < 0 || y >= sb.Height {
		return nil
	}

	var spans []attrSpan
	rowAttrs := sb.Attrs[y]
	col := 0

	for col < sb.Width {
		attr := rowAttrs[col]
		start := col
		for col < sb.Width && rowAttrs[col] == attr {
			col++
		}
		if !attr.IsZero() {
			spans = append(spans, attrSpan{start: start, end: col, attr: attr})
		}
	}

	return spans
}

// Encode serializes the 2D screen buffer into a deterministic golden snapshot format.
func (sb *ScreenBuffer) Encode() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== 2D SCREEN BUFFER [%dx%d] ===\n", sb.Width, sb.Height))
	b.WriteString("--- TEXT MATRIX ---\n")
	for y := 0; y < sb.Height; y++ {
		b.WriteString(fmt.Sprintf("%02d: |%s|\n", y, string(sb.Text[y])))
	}
	b.WriteString("--- ATTRIBUTE MATRIX ---\n")
	b.WriteString(sb.RenderAttributes())
	return b.String()
}

// Decode deserializes a golden snapshot string into a ScreenBuffer.
func Decode(raw string) (*ScreenBuffer, error) {
	lines := strings.Split(strings.TrimRight(raw, "\r\n"), "\n")
	if len(lines) < 3 {
		return nil, fmt.Errorf("snapshot data too short")
	}

	header := lines[0]
	var w, h int
	if _, err := fmt.Sscanf(header, "=== 2D SCREEN BUFFER [%dx%d] ===", &w, &h); err != nil {
		return nil, fmt.Errorf("invalid header %q: %w", header, err)
	}

	sb := NewScreenBuffer(w, h)

	// Locate sections
	textIdx := -1
	attrIdx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "--- TEXT MATRIX ---" {
			textIdx = i
		} else if strings.TrimSpace(l) == "--- ATTRIBUTE MATRIX ---" {
			attrIdx = i
		}
	}

	if textIdx == -1 || attrIdx == -1 || attrIdx <= textIdx {
		return nil, fmt.Errorf("missing or disordered section headers in snapshot")
	}

	// Read Text Matrix
	textLines := lines[textIdx+1 : attrIdx]
	for y := 0; y < h && y < len(textLines); y++ {
		line := textLines[y]
		pipeStart := strings.Index(line, "|")
		pipeEnd := strings.LastIndex(line, "|")
		if pipeStart >= 0 && pipeEnd > pipeStart {
			content := line[pipeStart+1 : pipeEnd]
			rowRunes := []rune(content)
			for x := 0; x < w; x++ {
				if x < len(rowRunes) {
					sb.Text[y][x] = rowRunes[x]
				} else {
					sb.Text[y][x] = ' '
				}
			}
		}
	}

	// Read Attribute Matrix
	attrLines := lines[attrIdx+1:]
	for y := 0; y < h && y < len(attrLines); y++ {
		line := attrLines[y]
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		body := strings.TrimSpace(line[colonIdx+1:])
		if body == "(plain)" || body == "" {
			continue
		}

		spans := strings.Split(body, " | ")
		for _, sp := range spans {
			sp = strings.TrimSpace(sp)
			bracketStart := strings.Index(sp, "[")
			bracketEnd := strings.Index(sp, "]")
			if bracketStart < 0 || bracketEnd <= bracketStart {
				continue
			}
			rangeStr := sp[bracketStart+1 : bracketEnd]
			attrStr := strings.TrimSpace(sp[bracketEnd+1:])

			parts := strings.Split(rangeStr, ":")
			if len(parts) != 2 {
				continue
			}
			start, err1 := strconv.Atoi(parts[0])
			end, err2 := strconv.Atoi(parts[1])
			if err1 != nil || err2 != nil {
				continue
			}

			cat := parseCellAttribute(attrStr)
			for col := start; col < end && col < w; col++ {
				sb.Attrs[y][col] = cat
			}
		}
	}

	return sb, nil
}

// CaptureConfig controls snapshot capture behavior.
type CaptureConfig struct {
	Sanitize  bool
	Sanitizer *Sanitizer
}

// CaptureOption modifies capture settings.
type CaptureOption func(*CaptureConfig)

// WithoutSanitization skips deterministic text masking.
func WithoutSanitization() CaptureOption {
	return func(c *CaptureConfig) {
		c.Sanitize = false
	}
}

// WithCustomSanitizer configures a custom sanitizer instance.
func WithCustomSanitizer(s *Sanitizer) CaptureOption {
	return func(c *CaptureConfig) {
		c.Sanitizer = s
	}
}

// CaptureScreen feeds view text into a virtual midterm.Terminal and returns the captured 2D ScreenBuffer.
func CaptureScreen(view string, width, height int, opts ...CaptureOption) *ScreenBuffer {
	cfg := CaptureConfig{
		Sanitize:  true,
		Sanitizer: defaultSanitizer,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	if width < 1 {
		width = 80
	}
	if height < 1 {
		height = 24
	}

	input := view
	if cfg.Sanitize {
		input = cfg.Sanitizer.Sanitize(view)
	}

	vt := midterm.NewTerminal(height, width)
	vt.Raw = false // cooked mode ensures \n resets column position to 0

	_, _ = vt.Write([]byte(input))

	sb := NewScreenBuffer(width, height)

	// 1. Text Matrix: copy vt.Content [H][W]rune
	for y := 0; y < height; y++ {
		var srcRow []rune
		if y < len(vt.Content) {
			srcRow = vt.Content[y]
		}
		for x := 0; x < width; x++ {
			if x < len(srcRow) && srcRow[x] != 0 {
				sb.Text[y][x] = srcRow[x]
			} else {
				sb.Text[y][x] = ' '
			}
		}
	}

	// 2. Attribute Matrix: decode Canvas regions for each row
	if vt.Format != nil {
		for y := 0; y < height && y < len(vt.Format.Rows); y++ {
			col := 0
			for r := vt.Format.Rows[y]; r != nil && col < width; r = r.Next {
				if r.Size <= 0 {
					continue
				}
				cat := formatToCellAttribute(r.F)
				for i := 0; i < r.Size && col < width; i++ {
					sb.Attrs[y][col] = cat
					col++
				}
			}
		}
	}

	return sb
}

// CaptureStory renders a story instance variation and captures its 2D screen buffer.
func CaptureStory(inst StoryInstance, stateIndex, width, height int, opts ...CaptureOption) *ScreenBuffer {
	if inst == nil {
		return NewScreenBuffer(width, height)
	}
	inst.SetState(stateIndex)
	inst.SetSize(width, height)
	return CaptureScreen(inst.View(), width, height, opts...)
}

func formatToCellAttribute(f midterm.Format) CellAttribute {
	return CellAttribute{
		Fg:        colorToString(f.Fg),
		Bg:        colorToString(f.Bg),
		Bold:      f.IsBold(),
		Reverse:   f.IsReverse(),
		Underline: f.IsUnderline(),
		Faint:     f.IsFaint(),
		Italic:    f.IsItalic(),
	}
}

func colorToString(c termenv.Color) string {
	if c == nil {
		return ""
	}
	switch col := c.(type) {
	case termenv.ANSIColor:
		return fmt.Sprintf("ansi:%d", col)
	case termenv.ANSI256Color:
		return fmt.Sprintf("color256:%d", col)
	case termenv.RGBColor:
		return string(col)
	default:
		return fmt.Sprintf("%v", c)
	}
}
