package widthx

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Truncate truncates text so that its visible display width does not exceed maxWidth,
// appending tail (e.g. "...") if truncation occurred. Active ANSI styles are cleanly closed.
func Truncate(text string, maxWidth int, tail string) string {
	return TruncateToWidth(text, maxWidth, tail, false)
}

// TruncateToWidth truncates text to fit within maxWidth visible columns,
// appending ellipsis if truncated. If pad is true, the result is padded with
// trailing spaces to reach exactly maxWidth columns.
func TruncateToWidth(text string, maxWidth int, ellipsis string, pad bool) string {
	if maxWidth <= 0 {
		return ""
	}
	visibleLen := VisibleWidth(text)
	if visibleLen <= maxWidth {
		if pad {
			return text + strings.Repeat(" ", maxWidth-visibleLen)
		}
		return text
	}

	ellipsisWidth := VisibleWidth(ellipsis)
	if ellipsisWidth >= maxWidth {
		// Even ellipsis alone doesn't fit, clip ellipsis
		var b strings.Builder
		cur := 0
		for _, r := range ellipsis {
			rw := runewidth.RuneWidth(r)
			if cur+rw > maxWidth {
				break
			}
			b.WriteRune(r)
			cur += rw
		}
		res := b.String()
		if pad && cur < maxWidth {
			res += strings.Repeat(" ", maxWidth-cur)
		}
		return res
	}

	targetWidth := maxWidth - ellipsisWidth
	var result strings.Builder
	tr := &AnsiCodeTracker{}
	currentWidth := 0

	i := 0
	for i < len(text) {
		if text[i] == 0x1B {
			code, n := ExtractAnsiCode(text, i)
			if n > 0 {
				tr.Process(code)
				result.WriteString(code)
				i += n
				continue
			}
		}

		r, size := utf8.DecodeRuneInString(text[i:])
		if size <= 0 {
			i++
			continue
		}
		rw := runewidth.RuneWidth(r)

		if currentWidth+rw > targetWidth {
			break
		}

		result.WriteString(text[i : i+size])
		currentWidth += rw
		i += size
	}

	result.WriteString(ellipsis)
	currentWidth += ellipsisWidth

	// Close any active ANSI styling
	if rReset := tr.LineEndReset(); rReset != "" {
		result.WriteString(rReset)
	}

	if pad && currentWidth < maxWidth {
		result.WriteString(strings.Repeat(" ", maxWidth-currentWidth))
	}

	return result.String()
}
