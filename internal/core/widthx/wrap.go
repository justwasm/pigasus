package widthx

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// WrapTextWithAnsi wraps text to fit within `width` visible columns per line,
// preserving active ANSI SGR styles and OSC 8 hyperlink state across line breaks.
//
// Guaranteed properties:
//   - Active colors/styles are reset at the end of wrapped lines (via LineEndReset)
//   - Active colors/styles are re-applied at the start of continued lines (via ActiveCodes)
//   - Text with explicit newlines (\n or \r\n) is preserved per logical line
func WrapTextWithAnsi(text string, width int) []string {
	if text == "" {
		return []string{""}
	}
	if width <= 0 {
		return []string{text}
	}

	src := text
	if strings.ContainsRune(src, '\r') {
		src = strings.ReplaceAll(src, "\r\n", "\n")
		src = strings.ReplaceAll(src, "\r", "\n")
	}

	inputLines := strings.Split(src, "\n")
	var result []string
	tr := &AnsiCodeTracker{}

	for _, line := range inputLines {
		prefix := ""
		if len(result) > 0 {
			prefix = tr.ActiveCodes()
		}
		wrapped := wrapSingleLine(prefix+line, width, tr)
		result = append(result, wrapped...)
		tr.UpdateFromText(line)
	}

	if len(result) == 0 {
		return []string{""}
	}
	return result
}

func wrapSingleLine(line string, width int, tr *AnsiCodeTracker) []string {
	if line == "" {
		return []string{""}
	}
	if VisibleWidth(line) <= width {
		return []string{line}
	}

	var wrapped []string
	tokens := splitIntoTokensWithAnsi(line)

	var current strings.Builder
	currentVisible := 0

	for _, token := range tokens {
		// Update tracker with any ANSI escapes in the token
		tr.UpdateFromText(token)

		tokenWidth := VisibleWidth(token)
		isWhitespace := strings.TrimSpace(StripAnsi(token)) == ""

		// Token alone is longer than width: break by character/rune
		if tokenWidth > width && !isWhitespace {
			if current.Len() > 0 {
				if r := tr.LineEndReset(); r != "" {
					current.WriteString(r)
				}
				wrapped = append(wrapped, current.String())
				current.Reset()
				currentVisible = 0
			}
			broken := breakLongWord(token, width, tr)
			if len(broken) > 1 {
				wrapped = append(wrapped, broken[:len(broken)-1]...)
			}
			if len(broken) > 0 {
				last := broken[len(broken)-1]
				current.WriteString(last)
				currentVisible = VisibleWidth(last)
			}
			continue
		}

		if currentVisible+tokenWidth > width && currentVisible > 0 {
			// Line break: trim trailing whitespace, close ANSI styling
			lineStr := strings.TrimRight(current.String(), " ")
			if r := tr.LineEndReset(); r != "" {
				lineStr += r
			}
			wrapped = append(wrapped, lineStr)
			current.Reset()

			// Re-open active styling on the new line
			current.WriteString(tr.ActiveCodes())
			if !isWhitespace {
				current.WriteString(token)
				currentVisible = tokenWidth
			} else {
				currentVisible = 0
			}
		} else {
			current.WriteString(token)
			currentVisible += tokenWidth
		}
	}

	if current.Len() > 0 {
		wrapped = append(wrapped, current.String())
	}
	if len(wrapped) == 0 {
		return []string{""}
	}
	return wrapped
}

func splitIntoTokensWithAnsi(line string) []string {
	var tokens []string
	var current strings.Builder
	inWhitespace := false
	started := false

	i := 0
	for i < len(line) {
		if line[i] == 0x1B {
			_, n := ExtractAnsiCode(line, i)
			if n > 0 {
				current.WriteString(line[i : i+n])
				i += n
				continue
			}
		}

		c := line[i]
		isSpace := c == ' ' || c == '\t'

		if !started {
			inWhitespace = isSpace
			started = true
		} else if isSpace != inWhitespace {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			inWhitespace = isSpace
		}

		current.WriteByte(c)
		i++
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func breakLongWord(word string, width int, tr *AnsiCodeTracker) []string {
	var lines []string
	var current strings.Builder
	currentWidth := 0

	i := 0
	for i < len(word) {
		if word[i] == 0x1B {
			code, n := ExtractAnsiCode(word, i)
			if n > 0 {
				tr.Process(code)
				current.WriteString(code)
				i += n
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(word[i:])
		if size <= 0 {
			i++
			continue
		}
		rw := runewidth.RuneWidth(r)

		if currentWidth+rw > width && currentWidth > 0 {
			if rReset := tr.LineEndReset(); rReset != "" {
				current.WriteString(rReset)
			}
			lines = append(lines, current.String())
			current.Reset()
			current.WriteString(tr.ActiveCodes())
			currentWidth = 0
		}

		current.WriteString(word[i : i+size])
		currentWidth += rw
		i += size
	}

	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}
