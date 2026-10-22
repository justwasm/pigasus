package tuilib

func skipEscapeOSC(s string, i int) int {
	i++
	for i < len(s) {
		if s[i] == 0x07 {
			return i + 1
		}
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
			return i + 2
		}
		i++
	}
	return i
}

func skipEscapeDCS(s string, i int) int {
	i++
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
			return i + 2
		}
		i++
	}
	return i
}

func skipEscapeCSI(s string, i int) int {
	i++
	for i < len(s) && s[i] >= 0x20 && s[i] <= 0x3F {
		i++
	}
	if i < len(s) && s[i] >= 0x40 && s[i] <= 0x7E {
		i++
	}
	return i
}

// SkipEscape returns the byte index just past the escape sequence starting at s[i].
func SkipEscape(s string, i int) int {
	if i >= len(s) || s[i] != 0x1b {
		return i
	}
	i++
	if i >= len(s) {
		return i
	}
	switch s[i] {
	case '[':
		return skipEscapeCSI(s, i)
	case ']':
		return skipEscapeOSC(s, i)
	case 'P', 'X', '^', '_':
		return skipEscapeDCS(s, i)
	default:
		return i + 1
	}
}
