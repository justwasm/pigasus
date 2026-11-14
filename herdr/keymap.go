package herdr

import (
	tea "charm.land/bubbletea/v2"
)

// FocusMode controls which panel receives keyboard events.
type FocusMode int

const (
	// FocusTerminal routes keystrokes to the active terminal PTY.
	FocusTerminal FocusMode = iota
	// FocusSidePanel routes keystrokes to side tab panel navigation.
	FocusSidePanel
)

func (f FocusMode) String() string {
	if f == FocusTerminal {
		return "Terminal"
	}
	return "Side Panel"
}

// EncodeKeyToPTY translates a Bubbletea key event into standard terminal bytes for PTY consumption.
func EncodeKeyToPTY(msg tea.KeyMsg) []byte {
	k := msg.Key()

	// 1. Control combos
	if k.Mod.Contains(tea.ModCtrl) || k.Mod&tea.ModCtrl != 0 {
		code := k.Code
		if code >= 'a' && code <= 'z' {
			return []byte{byte(code - 'a' + 1)}
		}
		if code >= 'A' && code <= 'Z' {
			return []byte{byte(code - 'A' + 1)}
		}
		switch code {
		case ' ':
			return []byte{0x00}
		case '[':
			return []byte{0x1b}
		case '\\':
			return []byte{0x1c}
		case ']':
			return []byte{0x1d}
		case '^':
			return []byte{0x1e}
		case '_':
			return []byte{0x1f}
		}
	}

	// 2. Special terminal keys
	switch msg.String() {
	case "enter":
		return []byte("\r")
	case "backspace":
		return []byte{0x7f}
	case "tab":
		return []byte("\t")
	case "esc":
		return []byte("\x1b")
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	case "home":
		return []byte("\x1b[H")
	case "end":
		return []byte("\x1b[F")
	case "delete":
		return []byte("\x1b[3~")
	case "pgup":
		return []byte("\x1b[5~")
	case "pgdown":
		return []byte("\x1b[6~")
	}

	// 3. Printable text characters
	if k.Text != "" {
		return []byte(k.Text)
	}
	if k.Code != 0 {
		return []byte(string(k.Code))
	}
	return nil
}

// IsCtrlB tests whether a key message matches the default Herdr / tmux prefix (Ctrl+B).
func IsCtrlB(msg tea.KeyMsg) bool {
	if msg == nil {
		return false
	}
	s := msg.String()
	if s == "ctrl+b" || s == "ctrl+B" || s == "^B" || s == "^b" {
		return true
	}
	k := msg.Key()
	if k.Code == 'b' || k.Code == 'B' {
		return k.Mod.Contains(tea.ModCtrl) || k.Mod&tea.ModCtrl != 0
	}
	if k.Code == 2 { // ASCII 0x02 (Ctrl+B)
		return true
	}
	return false
}
