package terminalpanel

import (
	"fmt"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// KeyEncodeOptions configures terminal key encoding modes.
type KeyEncodeOptions struct {
	CursorKeysApp   bool
	ModifyOtherKeys int
}

// EncodeKey maps a Bubbletea tea.Key event into standard ANSI / VT100
// escape sequences for transmission to a real OS PTY or remote terminal.
func EncodeKey(key tea.Key) []byte {
	return EncodeKeyWithOptions(key, KeyEncodeOptions{})
}

// EncodeKeyWithModes maps a Bubbletea tea.Key event into ANSI / VT100 escape
// sequences, respecting DECCKM application cursor keys mode when active.
func EncodeKeyWithModes(key tea.Key, cursorKeysApp bool) []byte {
	return EncodeKeyWithOptions(key, KeyEncodeOptions{CursorKeysApp: cursorKeysApp})
}

func xtermModParam(mod tea.KeyMod) int {
	m := 0
	if mod.Contains(tea.ModShift) {
		m |= 1
	}
	if mod.Contains(tea.ModAlt) {
		m |= 2
	}
	if mod.Contains(tea.ModCtrl) {
		m |= 4
	}
	if mod.Contains(tea.ModMeta) {
		m |= 8
	}
	if m == 0 {
		return 0
	}
	return m + 1
}

// EncodeKeyWithOptions maps a Bubbletea tea.Key event into ANSI / VT100 escape
// sequences, respecting DECCKM cursor keys mode and xterm modifyOtherKeys mode.
func EncodeKeyWithOptions(key tea.Key, opts KeyEncodeOptions) []byte {
	withAlt := func(seq []byte) []byte {
		if key.Mod.Contains(tea.ModAlt) {
			return append([]byte{0x1B}, seq...)
		}
		return seq
	}

	// Standard navigation, editing, and function keys (preserving modifiers)
	switch key.Code {
	case tea.KeyBackspace:
		if opts.ModifyOtherKeys >= 1 && (key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModShift)) {
			return []byte(fmt.Sprintf("\x1b[27;%d;127~", xtermModParam(key.Mod)))
		}
		if key.Mod.Contains(tea.ModCtrl) {
			return withAlt([]byte{0x08})
		}
		return withAlt([]byte{0x7F})
	case tea.KeyEnter:
		if opts.ModifyOtherKeys >= 1 && (key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModShift)) {
			return []byte(fmt.Sprintf("\x1b[27;%d;13~", xtermModParam(key.Mod)))
		}
		return withAlt([]byte("\r"))
	case tea.KeyTab:
		if opts.ModifyOtherKeys >= 1 && key.Mod.Contains(tea.ModCtrl) {
			return []byte(fmt.Sprintf("\x1b[27;%d;9~", xtermModParam(key.Mod)))
		}
		if key.Mod.Contains(tea.ModShift) {
			if key.Mod == tea.ModShift {
				return []byte("\x1b[Z")
			}
			return []byte(fmt.Sprintf("\x1b[1;%dZ", xtermModParam(key.Mod)))
		}
		return withAlt([]byte("\t"))
	case tea.KeyEsc:
		return withAlt([]byte{0x1B})
	case tea.KeyUp:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[1;%dA", mod))
		}
		if opts.CursorKeysApp {
			return []byte("\x1bOA")
		}
		return []byte("\x1b[A")
	case tea.KeyDown:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[1;%dB", mod))
		}
		if opts.CursorKeysApp {
			return []byte("\x1bOB")
		}
		return []byte("\x1b[B")
	case tea.KeyRight:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[1;%dC", mod))
		}
		if opts.CursorKeysApp {
			return []byte("\x1bOC")
		}
		return []byte("\x1b[C")
	case tea.KeyLeft:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[1;%dD", mod))
		}
		if opts.CursorKeysApp {
			return []byte("\x1bOD")
		}
		return []byte("\x1b[D")
	case tea.KeyHome:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[1;%dH", mod))
		}
		if opts.CursorKeysApp {
			return []byte("\x1bOH")
		}
		return []byte("\x1b[H")
	case tea.KeyEnd:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[1;%dF", mod))
		}
		if opts.CursorKeysApp {
			return []byte("\x1bOF")
		}
		return []byte("\x1b[F")
	case tea.KeyInsert:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[2;%d~", mod))
		}
		return []byte("\x1b[2~")
	case tea.KeyDelete:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[3;%d~", mod))
		}
		return []byte("\x1b[3~")
	case tea.KeyPgUp:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[5;%d~", mod))
		}
		return []byte("\x1b[5~")
	case tea.KeyPgDown:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[6;%d~", mod))
		}
		return []byte("\x1b[6~")
	case tea.KeyF1:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[1;%dP", mod))
		}
		return []byte("\x1bOP")
	case tea.KeyF2:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[1;%dQ", mod))
		}
		return []byte("\x1bOQ")
	case tea.KeyF3:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[1;%dR", mod))
		}
		return []byte("\x1bOR")
	case tea.KeyF4:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[1;%dS", mod))
		}
		return []byte("\x1bOS")
	case tea.KeyF5:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[15;%d~", mod))
		}
		return []byte("\x1b[15~")
	case tea.KeyF6:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[17;%d~", mod))
		}
		return []byte("\x1b[17~")
	case tea.KeyF7:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[18;%d~", mod))
		}
		return []byte("\x1b[18~")
	case tea.KeyF8:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[19;%d~", mod))
		}
		return []byte("\x1b[19~")
	case tea.KeyF9:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[20;%d~", mod))
		}
		return []byte("\x1b[20~")
	case tea.KeyF10:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[21;%d~", mod))
		}
		return []byte("\x1b[21~")
	case tea.KeyF11:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[23;%d~", mod))
		}
		return []byte("\x1b[23~")
	case tea.KeyF12:
		if mod := xtermModParam(key.Mod); mod > 0 {
			return []byte(fmt.Sprintf("\x1b[24;%d~", mod))
		}
		return []byte("\x1b[24~")
	}

	// Handle Ctrl key combinations: Ctrl+A..Ctrl+Z -> 0x01..0x1A (with optional Alt ESC prefix)
	if key.Mod.Contains(tea.ModCtrl) {
		r := key.Code
		if opts.ModifyOtherKeys >= 2 && key.Mod.Contains(tea.ModShift) && r > 0 && r < 128 && unicode.IsPrint(r) {
			return []byte(fmt.Sprintf("\x1b[27;%d;%d~", xtermModParam(key.Mod), r))
		}
		if r >= 'a' && r <= 'z' {
			return withAlt([]byte{byte(r - 'a' + 1)})
		}
		if r >= 'A' && r <= 'Z' {
			return withAlt([]byte{byte(r - 'A' + 1)})
		}
		switch r {
		case '@', tea.KeySpace:
			return withAlt([]byte{0x00})
		case '[':
			return withAlt([]byte{0x1B}) // ESC
		case '\\':
			return withAlt([]byte{0x1C})
		case ']':
			return withAlt([]byte{0x1D})
		case '^':
			return withAlt([]byte{0x1E})
		case '_', '/':
			return withAlt([]byte{0x1F})
		}
		if opts.ModifyOtherKeys >= 1 && r > 0 && r < 128 && unicode.IsPrint(r) {
			return []byte(fmt.Sprintf("\x1b[27;%d;%d~", xtermModParam(key.Mod), r))
		}
	}

	// Handle Alt key combinations: ESC + character
	if key.Mod.Contains(tea.ModAlt) {
		if key.Text != "" {
			return append([]byte{0x1B}, []byte(key.Text)...)
		}
		if key.Code > 0 && unicode.IsPrint(key.Code) {
			r := key.Code
			if key.Mod.Contains(tea.ModShift) {
				if key.ShiftedCode > 0 {
					r = key.ShiftedCode
				} else if r >= 'a' && r <= 'z' {
					r = unicode.ToUpper(r)
				}
			}
			return append([]byte{0x1B}, []byte(string(r))...)
		}
	}

	// Text / Rune typing
	if key.Text != "" {
		return []byte(key.Text)
	}

	if key.Code > 0 && unicode.IsPrint(key.Code) {
		r := key.Code
		if key.Mod.Contains(tea.ModShift) && key.ShiftedCode > 0 {
			r = key.ShiftedCode
		}
		return []byte(string(r))
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
