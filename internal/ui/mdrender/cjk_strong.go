package mdrender

import (
	"strings"
	"unicode"
)

const cjkSentinel = "\u001f"

// isCJK reports whether r is a Chinese, Japanese, or Korean character.
func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) ||
		unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) ||
		unicode.Is(unicode.Hangul, r)
}

// isPunct reports whether r is punctuation.
func isPunct(r rune) bool {
	return unicode.IsPunct(r) || unicode.IsSymbol(r)
}

// preprocessCJKStrong inserts sentinel spaces around bold delimiter runs adjacent to CJK
// text when the bold part starts or ends with punctuation (AGY 1.5.12 item 14).
// In CommonMark, "**注意：**这里" fails to close strong because the closing "**" is preceded
// by punctuation and followed by non-whitespace/non-punctuation. Similarly, "这里**（注意）**"
// fails to open strong because opening "**" is preceded by non-whitespace and followed by punctuation.
func preprocessCJKStrong(src string) string {
	if !strings.Contains(src, "**") {
		return src
	}

	var sb strings.Builder
	runes := []rune(src)
	n := len(runes)

	for i := 0; i < n; {
		// Check for pattern 1: CJK + "**" + punct ("这里**（注意）**")
		if i+2 < n && runes[i] == '*' && runes[i+1] == '*' && isPunct(runes[i+2]) {
			if i > 0 && isCJK(runes[i-1]) {
				sb.WriteString(cjkSentinel + " ")
			}
			sb.WriteString("**")
			i += 2
			continue
		}

		// Check for pattern 2: punct + "**" + CJK ("**注意：**这里")
		if i+1 < n && runes[i] == '*' && runes[i+1] == '*' {
			if i > 0 && isPunct(runes[i-1]) && i+2 < n && isCJK(runes[i+2]) {
				sb.WriteString("** ")
				sb.WriteString(cjkSentinel)
				i += 2
				continue
			}
		}

		sb.WriteRune(runes[i])
		i++
	}

	return sb.String()
}

// cleanCJKStrongSentinels removes the sentinel markers and spacing inserted by preprocessCJKStrong.
func cleanCJKStrongSentinels(s string) string {
	if !strings.Contains(s, cjkSentinel) {
		return s
	}
	s = strings.ReplaceAll(s, " "+cjkSentinel, "")
	s = strings.ReplaceAll(s, cjkSentinel+" ", "")
	s = strings.ReplaceAll(s, cjkSentinel, "")
	return s
}
