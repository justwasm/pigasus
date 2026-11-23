package terminalpanel

import (
	"fmt"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// EncodeKey maps a Bubbletea tea.Key event into standard ANSI / VT100
// escape sequences for transmission to a real OS PTY or remote terminal.
func EncodeKey(key tea.Key) []byte {
	return EncodeKeyWithModes(key, false)
}

// EncodeKeyWithModes maps a Bubbletea tea.Key event into ANSI / VT100 escape
// sequences, respecting DECCKM application cursor keys mode when active.
func EncodeKeyWithModes(key tea.Key, cursorKeysApp bool) []byte {
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
		if cursorKeysApp {
			return []byte("\x1bOA")
		}
		return []byte("\x1b[A")
	case tea.KeyDown:
		if cursorKeysApp {
			return []byte("\x1bOB")
		}
		return []byte("\x1b[B")
	case tea.KeyRight:
		if cursorKeysApp {
			return []byte("\x1bOC")
		}
		return []byte("\x1b[C")
	case tea.KeyLeft:
		if cursorKeysApp {
			return []byte("\x1bOD")
		}
		return []byte("\x1b[D")
	case tea.KeyHome:
		if cursorKeysApp {
			return []byte("\x1bOH")
		}
		return []byte("\x1b[H")
	case tea.KeyEnd:
		if cursorKeysApp {
			return []byte("\x1bOF")
		}
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

// EncodeMouse encodes a mouse event at 0-based terminal body coordinates (col, row)
// into SGR (1006) or X10/normal VT mouse escape sequences.
func EncodeMouse(ev tea.Mouse, col, row int, isRelease, isMotion, sgr bool) []byte {
	if col < 0 {
		col = 0
	}
	if row < 0 {
		row = 0
	}
	cb := -1
	switch ev.Button {
	case tea.MouseLeft:
		cb = 0
	case tea.MouseMiddle:
		cb = 1
	case tea.MouseRight:
		cb = 2
	case tea.MouseWheelUp:
		cb = 64
	case tea.MouseWheelDown:
		cb = 65
	case tea.MouseWheelLeft:
		cb = 66
	case tea.MouseWheelRight:
		cb = 67
	case tea.MouseNone:
		if isMotion || isRelease {
			cb = 3
		}
	}
	if cb < 0 {
		return nil
	}
	if isMotion {
		cb |= 32
	}
	if ev.Mod.Contains(tea.ModShift) {
		cb |= 4
	}
	if ev.Mod.Contains(tea.ModAlt) {
		cb |= 8
	}
	if ev.Mod.Contains(tea.ModCtrl) {
		cb |= 16
	}

	cx := col + 1
	cy := row + 1
	if sgr {
		suffix := 'M'
		if isRelease {
			suffix = 'm'
		}
		return []byte(fmt.Sprintf("\x1b[<%d;%d;%d%c", cb, cx, cy, suffix))
	}

	// X10 / normal mouse encoding (coordinates clamped to 223 so byte+32 <= 255)
	if isRelease {
		cb = (cb &^ 3) | 3
	}
	if cx > 223 {
		cx = 223
	}
	if cy > 223 {
		cy = 223
	}
	return []byte{0x1B, '[', 'M', byte(32 + cb), byte(32 + cx), byte(32 + cy)}
}

// EncodePaste encodes pasted text with bracketed paste mode delimiters.
func EncodePaste(text string) []byte {
	var buf []byte
	buf = append(buf, []byte("\x1b[200~")...)
	buf = append(buf, []byte(text)...)
	buf = append(buf, []byte("\x1b[201~")...)
	return buf
}
