package terminalmgr

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// EncodePaste wraps text in bracketed paste delimiters.
func EncodePaste(text string) []byte {
	buf := make([]byte, 0, len(text)+12)
	buf = append(buf, "\x1b[200~"...)
	buf = append(buf, text...)
	buf = append(buf, "\x1b[201~"...)
	return buf
}

// EncodeKeys converts symbolic key names (e.g. "Enter", "Ctrl+C", "Up", "Alt+x")
// into VT/ANSI byte sequences.
func EncodeKeys(keys []string, cursorKeysApp bool) ([]byte, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	var out []byte
	for _, raw := range keys {
		seq, err := encodeSingleKey(raw, cursorKeysApp)
		if err != nil {
			return nil, err
		}
		out = append(out, seq...)
	}
	return out, nil
}

func encodeSingleKey(raw string, cursorKeysApp bool) ([]byte, error) {
	k := strings.TrimSpace(raw)
	if k == "" {
		return nil, fmt.Errorf("terminal: empty key name")
	}
	lower := strings.ToLower(k)

	// Ctrl+<key> or C-<key>
	for _, prefix := range []string{"ctrl+", "ctrl-", "c-"} {
		if strings.HasPrefix(lower, prefix) {
			rest := strings.TrimSpace(k[len(prefix):])
			return encodeCtrlKey(rest)
		}
	}

	// Alt+<key> or M-<key>
	for _, prefix := range []string{"alt+", "alt-", "m-"} {
		if strings.HasPrefix(lower, prefix) {
			rest := strings.TrimSpace(k[len(prefix):])
			if rest == "" {
				return nil, fmt.Errorf("terminal: invalid key %q", raw)
			}
			inner, err := encodeSingleKey(rest, cursorKeysApp)
			if err != nil {
				return nil, err
			}
			return append([]byte{0x1B}, inner...), nil
		}
	}

	switch lower {
	case "enter", "return", "cr":
		return []byte("\r"), nil
	case "tab":
		return []byte("\t"), nil
	case "shift+tab", "s-tab", "backtab", "btab":
		return []byte("\x1b[Z"), nil
	case "backspace", "bs", "bspace":
		return []byte{0x7F}, nil
	case "esc", "escape":
		return []byte{0x1B}, nil
	case "space":
		return []byte(" "), nil
	case "up":
		if cursorKeysApp {
			return []byte("\x1bOA"), nil
		}
		return []byte("\x1b[A"), nil
	case "down":
		if cursorKeysApp {
			return []byte("\x1bOB"), nil
		}
		return []byte("\x1b[B"), nil
	case "right":
		if cursorKeysApp {
			return []byte("\x1bOC"), nil
		}
		return []byte("\x1b[C"), nil
	case "left":
		if cursorKeysApp {
			return []byte("\x1bOD"), nil
		}
		return []byte("\x1b[D"), nil
	case "home":
		if cursorKeysApp {
			return []byte("\x1bOH"), nil
		}
		return []byte("\x1b[H"), nil
	case "end":
		if cursorKeysApp {
			return []byte("\x1bOF"), nil
		}
		return []byte("\x1b[F"), nil
	case "pgup", "pageup", "page_up", "ppage":
		return []byte("\x1b[5~"), nil
	case "pgdn", "pagedown", "page_down", "npage":
		return []byte("\x1b[6~"), nil
	case "delete", "del", "dc":
		return []byte("\x1b[3~"), nil
	case "insert", "ins", "ic":
		return []byte("\x1b[2~"), nil
	}

	// Single printable rune (e.g. "q", "y", ":")
	if utf8.RuneCountInString(k) == 1 {
		r, _ := utf8.DecodeRuneInString(k)
		if unicode.IsPrint(r) {
			return []byte(k), nil
		}
	}

	return nil, fmt.Errorf("terminal: unsupported key %q", raw)
}

func encodeCtrlKey(rest string) ([]byte, error) {
	if rest == "" {
		return nil, fmt.Errorf("terminal: empty Ctrl key")
	}
	lower := strings.ToLower(rest)
	switch lower {
	case "space", "@":
		return []byte{0x00}, nil
	case "[":
		return []byte{0x1B}, nil
	case `\`:
		return []byte{0x1C}, nil
	case "]":
		return []byte{0x1D}, nil
	case "^":
		return []byte{0x1E}, nil
	case "_":
		return []byte{0x1F}, nil
	}
	if len(rest) == 1 {
		b := rest[0]
		if b >= 'a' && b <= 'z' {
			return []byte{b - 'a' + 1}, nil
		}
		if b >= 'A' && b <= 'Z' {
			return []byte{b - 'A' + 1}, nil
		}
	}
	return nil, fmt.Errorf("terminal: unsupported Ctrl key %q", rest)
}

// StripANSI removes ANSI/VT escape sequences from s.
func StripANSI(s string) string {
	if !strings.ContainsRune(s, 0x1b) {
		return s
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			b = append(b, s[i])
			i++
			continue
		}
		if i+1 >= len(s) {
			break
		}
		switch s[i+1] {
		case '[':
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
		case ']':
			j := i + 2
			for j < len(s) {
				if s[j] == 0x07 {
					j++
					break
				}
				if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			i = j
		case '(', ')', '*', '+', '#', '%', ' ':
			i += 3
			if i > len(s) {
				i = len(s)
			}
		default:
			i += 2
		}
	}
	return string(b)
}
