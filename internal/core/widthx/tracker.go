package widthx

import (
	"strconv"
	"strings"
)

// AnsiCodeTracker tracks SGR styling state and active OSC 8 hyperlinks across a
// stream of text and ANSI codes. This allows styles (colors, bold, links) to be
// safely carried across line breaks without leaking into adjacent lines.
type AnsiCodeTracker struct {
	bold      bool
	dim       bool
	italic    bool
	underline bool
	blink     bool
	inverse   bool
	hidden    bool
	strike    bool
	fgColor   string // raw SGR parameters for foreground, e.g. "38;5;123" or "31"
	bgColor   string // raw SGR parameters for background, e.g. "48;2;10;20;30"

	activeHyperlinkOpen  string // e.g. "\x1b]8;;https://example.com\x07"
	activeHyperlinkClose string // e.g. "\x1b]8;;\x07"
}

// Reset clears all tracked style attributes.
func (t *AnsiCodeTracker) Reset() {
	t.resetSGR()
	t.activeHyperlinkOpen = ""
	t.activeHyperlinkClose = ""
}

func (t *AnsiCodeTracker) resetSGR() {
	t.bold = false
	t.dim = false
	t.italic = false
	t.underline = false
	t.blink = false
	t.inverse = false
	t.hidden = false
	t.strike = false
	t.fgColor = ""
	t.bgColor = ""
}

// Process updates the tracker with a single ANSI escape code.
func (t *AnsiCodeTracker) Process(code string) {
	if strings.HasPrefix(code, "\x1b]8;") {
		body := code[len("\x1b]8;"):]
		var terminator, closeTerm string
		if strings.HasSuffix(code, "\x07") {
			terminator = "\x07"
			closeTerm = "\x07"
			body = strings.TrimSuffix(body, "\x07")
		} else {
			terminator = "\x1b\\"
			closeTerm = "\x1b\\"
			body = strings.TrimSuffix(body, "\x1b\\")
		}
		if params, after, ok := strings.Cut(body, ";"); ok {
			if after == "" {
				t.activeHyperlinkOpen = ""
				t.activeHyperlinkClose = ""
			} else {
				t.activeHyperlinkOpen = "\x1b]8;" + params + ";" + after + terminator
				t.activeHyperlinkClose = "\x1b]8;;" + closeTerm
			}
		}
		return
	}

	if !strings.HasSuffix(code, "m") || !strings.HasPrefix(code, "\x1b[") {
		return
	}

	params := code[2 : len(code)-1]
	if params == "" || params == "0" {
		t.resetSGR()
		return
	}

	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); {
		part := parts[i]
		if part == "" {
			i++
			continue
		}
		c, err := strconv.Atoi(part)
		if err != nil {
			i++
			continue
		}

		switch c {
		case 0:
			t.resetSGR()
			i++
		case 1:
			t.bold = true
			i++
		case 2:
			t.dim = true
			i++
		case 3:
			t.italic = true
			i++
		case 4:
			t.underline = true
			i++
		case 5, 6:
			t.blink = true
			i++
		case 7:
			t.inverse = true
			i++
		case 8:
			t.hidden = true
			i++
		case 9:
			t.strike = true
			i++
		case 21, 22:
			t.bold = false
			t.dim = false
			i++
		case 23:
			t.italic = false
			i++
		case 24:
			t.underline = false
			i++
		case 25:
			t.blink = false
			i++
		case 27:
			t.inverse = false
			i++
		case 28:
			t.hidden = false
			i++
		case 29:
			t.strike = false
			i++
		case 39:
			t.fgColor = ""
			i++
		case 49:
			t.bgColor = ""
			i++
		case 38: // Extended foreground: 38;5;N or 38;2;R;G;B
			if i+2 < len(parts) && parts[i+1] == "5" {
				t.fgColor = parts[i] + ";" + parts[i+1] + ";" + parts[i+2]
				i += 3
			} else if i+4 < len(parts) && parts[i+1] == "2" {
				t.fgColor = strings.Join(parts[i:i+5], ";")
				i += 5
			} else {
				i++
			}
		case 48: // Extended background: 48;5;N or 48;2;R;G;B
			if i+2 < len(parts) && parts[i+1] == "5" {
				t.bgColor = parts[i] + ";" + parts[i+1] + ";" + parts[i+2]
				i += 3
			} else if i+4 < len(parts) && parts[i+1] == "2" {
				t.bgColor = strings.Join(parts[i:i+5], ";")
				i += 5
			} else {
				i++
			}
		default:
			if (c >= 30 && c <= 37) || (c >= 90 && c <= 97) {
				t.fgColor = strconv.Itoa(c)
			} else if (c >= 40 && c <= 47) || (c >= 100 && c <= 107) {
				t.bgColor = strconv.Itoa(c)
			}
			i++
		}
	}
}

// UpdateFromText scans text for all ANSI escape sequences and updates the tracker.
func (t *AnsiCodeTracker) UpdateFromText(text string) {
	for i := 0; i < len(text); {
		if text[i] == 0x1B {
			code, n := ExtractAnsiCode(text, i)
			if n > 0 {
				t.Process(code)
				i += n
				continue
			}
		}
		i++
	}
}

// ActiveCodes emits the ANSI escape sequences needed to restore the current style
// at the beginning of a newly wrapped line.
func (t *AnsiCodeTracker) ActiveCodes() string {
	var b strings.Builder
	var sgrParams []string

	if t.bold {
		sgrParams = append(sgrParams, "1")
	}
	if t.dim {
		sgrParams = append(sgrParams, "2")
	}
	if t.italic {
		sgrParams = append(sgrParams, "3")
	}
	if t.underline {
		sgrParams = append(sgrParams, "4")
	}
	if t.blink {
		sgrParams = append(sgrParams, "5")
	}
	if t.inverse {
		sgrParams = append(sgrParams, "7")
	}
	if t.hidden {
		sgrParams = append(sgrParams, "8")
	}
	if t.strike {
		sgrParams = append(sgrParams, "9")
	}
	if t.fgColor != "" {
		sgrParams = append(sgrParams, t.fgColor)
	}
	if t.bgColor != "" {
		sgrParams = append(sgrParams, t.bgColor)
	}

	if len(sgrParams) > 0 {
		b.WriteString("\x1b[" + strings.Join(sgrParams, ";") + "m")
	}
	if t.activeHyperlinkOpen != "" {
		b.WriteString(t.activeHyperlinkOpen)
	}
	return b.String()
}

// LineEndReset emits the ANSI sequences to cleanly close all active styles and links
// at the end of a line, ensuring styling does not bleed into the margin or next line.
func (t *AnsiCodeTracker) LineEndReset() string {
	var b strings.Builder
	hasStyle := t.bold || t.dim || t.italic || t.underline || t.blink ||
		t.inverse || t.hidden || t.strike || t.fgColor != "" || t.bgColor != ""
	if hasStyle {
		b.WriteString("\x1b[0m")
	}
	if t.activeHyperlinkClose != "" {
		b.WriteString(t.activeHyperlinkClose)
	}
	return b.String()
}
