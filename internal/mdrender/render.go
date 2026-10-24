package mdrender

import (
	"bytes"
	"strings"

	"github.com/justwasm/pigasus/internal/palette"
)

// Indent prepends an indent prefix to each non-empty line of a styled block.
func Indent(styled, indent string) string {
	if styled == "" {
		return styled
	}
	lines := strings.Split(styled, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

// RenderMarkdown renders Markdown text to ANSI for terminal display.
func RenderMarkdown(text string, width int) string {
	if text == "" {
		return text
	}
	pre := PreprocessFileLinks(text)
	stripped, placeholders := StripMermaidAndMath(pre)
	convWidth := width
	if convWidth <= 0 {
		convWidth = CurrentWidth()
	}
	if convWidth > 0 {
		convWidth--
	}
	rendered, err := ANSI(stripped, convWidth)
	if err != nil || rendered == "" {
		var buf bytes.Buffer
		if wErr := RenderANSI(text, &buf); wErr != nil {
			return text
		}
		rendered = buf.String()
	} else {
		rendered = RestoreMermaidAndMath(rendered, placeholders)
	}
	out := strings.TrimRight(rendered, "\n")

	var kept []string
	for _, line := range strings.Split(out, "\n") {
		line = StyleInlineBackticks(line)
		if LineIsBlankANSI(line) {
			line = ""
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// LineIsBlankANSI reports whether an ANSI-escaped line contains only whitespace.
func LineIsBlankANSI(line string) bool {
	for i := 0; i < len(line); {
		if line[i] == 0x1b && i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && line[j] != 'm' {
				j++
			}
			if j < len(line) {
				i = j + 1
				continue
			}
		}
		switch line[i] {
		case ' ', '\t', '\r':
			i++
			continue
		default:
			return false
		}
	}
	return true
}

// StyleInlineBackticks wraps inline backtick spans with true-colour code chips.
func StyleInlineBackticks(line string) string {
	if !strings.ContainsRune(line, '`') {
		return line
	}
	const maxPairs = 8
	open := false
	pairs := 0
	for i := 0; i < len(line); i++ {
		if line[i] == '`' {
			if open {
				pairs++
				open = false
			} else {
				open = true
			}
		}
	}
	if open || pairs == 0 || pairs > maxPairs {
		return line
	}
	var sb strings.Builder
	sb.Grow(len(line) + pairs*len(palette.InlineCodeOpen))
	open = false
	for i := 0; i < len(line); i++ {
		if line[i] == '`' {
			if open {
				sb.WriteString(palette.InlineCodeClose)
				open = false
			} else {
				sb.WriteString(palette.InlineCodeOpen)
				open = true
			}
			continue
		}
		sb.WriteByte(line[i])
	}
	return sb.String()
}
