package terminalpanel

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// EncodeKey maps a Bubbletea tea.Key event into standard ANSI / VT100
// escape sequences for transmission to a real OS PTY or remote terminal.
func EncodeKey(key tea.Key) []byte {
	// Handle Ctrl key combinations: Ctrl+A..Ctrl+Z -> 0x01..0x1A
	if key.Mod.Contains(tea.ModCtrl) {
		r := key.Code
		if r >= 'a' && r <= 'z' {
			return []byte{byte(r - 'a' + 1)}
		}
		if r >= 'A' && r <= 'Z' {
			return []byte{byte(r - 'A' + 1)}
		}
		switch r {
		case '@', ' ':
			return []byte{0x00}
		case '[':
			return []byte{0x1B} // ESC
		case '\\':
			return []byte{0x1C}
		case ']':
			return []byte{0x1D}
		case '^':
			return []byte{0x1E}
		case '_':
			return []byte{0x1F}
		}
	}

	// Handle Alt key combinations: ESC + character
	if key.Mod.Contains(tea.ModAlt) {
		if key.Text != "" {
			return append([]byte{0x1B}, []byte(key.Text)...)
		}
		if key.Code > 0 && key.Code < 128 && unicode.IsPrint(key.Code) {
			return []byte{0x1B, byte(key.Code)}
		}
	}

	// Standard navigation and control codes
	switch key.Code {
	case tea.KeyEnter:
		return []byte("\r")
	case tea.KeyTab:
		return []byte("\t")
	case tea.KeyBackspace:
		return []byte{0x7F}
	case tea.KeyUp:
		return []byte("\x1b[A")
	case tea.KeyDown:
		return []byte("\x1b[B")
	case tea.KeyRight:
		return []byte("\x1b[C")
	case tea.KeyLeft:
		return []byte("\x1b[D")
	case tea.KeyHome:
		return []byte("\x1b[H")
	case tea.KeyEnd:
		return []byte("\x1b[F")
	case tea.KeyPgUp:
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		return []byte("\x1b[6~")
	case tea.KeyDelete:
		return []byte("\x1b[3~")
	case tea.KeyInsert:
		return []byte("\x1b[2~")
	case tea.KeyEsc:
		return []byte{0x1B}
	}

	// Text / Rune typing
	if key.Text != "" {
		return []byte(key.Text)
	}

	if key.Code > 0 && unicode.IsPrint(key.Code) {
		return []byte(string(key.Code))
	}

	return nil
}

// EncodePaste encodes pasted text with bracketed paste mode delimiters.
func EncodePaste(text string) []byte {
	var buf []byte
	buf = append(buf, []byte("\x1b[200~")...)
	buf = append(buf, []byte(text)...)
	buf = append(buf, []byte("\x1b[201~")...)
	return buf
}
