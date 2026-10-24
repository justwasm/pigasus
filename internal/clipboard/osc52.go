// Package clipboard provides terminal clipboard read and write utilities.
package clipboard

import (
	"io"
	"os"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

// SupportsOSC52 reports whether the host terminal can interpret an OSC 52 escape.
func SupportsOSC52() bool {
	return supportsOSC52With(os.Getenv)
}

func supportsOSC52With(getenv func(string) string) bool {
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" || getenv("SSH_TTY") != "" || getenv("MOSH_CONNECTION") != "" {
		return true
	}

	if termProgram := strings.ToLower(getenv("TERM_PROGRAM")); termProgram != "" {
		switch termProgram {
		case "apple_terminal":
			return false
		case "iterm.app", "wezterm", "ghostty", "kitty", "vscode", "warp", "alacritty":
			return true
		default:
			return true
		}
	}

	if strings.ToLower(getenv("LC_TERMINAL")) == "iterm2" {
		return true
	}

	term := strings.ToLower(getenv("TERM"))
	switch term {
	case "":
		return false
	case "screen", "screen-256color", "screen.xterm-256color":
		return false
	case "xterm", "xterm-mono":
		return false
	case "vt100", "vt220", "dumb":
		return false
	}

	return true
}

// WriteOSC52 writes text to the terminal clipboard via the OSC 52 escape sequence.
func WriteOSC52(w io.Writer, text string) bool {
	if text == "" || len(text) > 100000 {
		return false
	}
	seq := osc52.New(text).Limit(100000)
	_, err := seq.WriteTo(w)
	return err == nil
}
