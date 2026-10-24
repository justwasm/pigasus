package termcolor

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

func env256() []string {
	return []string{"TERM=xterm-256color"}
}

func TestLC_Terminal_TrueColor(t *testing.T) {
	env := append(env256(), "COLORTERM=truecolor")
	if got := Resolve(env); got != colorprofile.TrueColor {
		t.Errorf("COLORTERM=truecolor: want TrueColor, got %s", got)
	}
}

func TestLC_Terminal_ItTerm2Only(t *testing.T) {
	env := append(env256(), "LC_TERMINAL=iTerm2")
	if got := Resolve(env); got != colorprofile.TrueColor {
		t.Errorf("LC_TERMINAL=iTerm2 only: want TrueColor, got %s", got)
	}
}

func TestLC_Terminal_NeitherSet(t *testing.T) {
	env := env256()
	got := Resolve(env)
	if got == colorprofile.TrueColor {
		t.Errorf("no colorterm/env set: must NOT be TrueColor, got %s", got)
	}
	if got != colorprofile.ANSI256 {
		t.Errorf("no colorterm/env set: want ANSI256 (TERM=xterm-256color base), got %s", got)
	}
}

func TestLC_Terminal_NoColorWins(t *testing.T) {
	for _, noCol := range []string{"1", "true", "True", "TRUE"} {
		t.Run("NO_COLOR="+noCol, func(t *testing.T) {
			env := []string{"TERM=xterm-256color", "LC_TERMINAL=iTerm2", "NO_COLOR=" + noCol}
			got := Resolve(env)
			if got != colorprofile.ASCII {
				t.Errorf("NO_COLOR=%s + LC_TERMINAL=iTerm2: want ASCII (NO_COLOR hard opt-out), got %s", noCol, got)
			}
		})
	}
}

func TestLC_Terminal_ColorTermWins(t *testing.T) {
	for _, colorTerm := range []string{"truecolor", "24bit", "yes", "true"} {
		t.Run("COLORTERM="+colorTerm, func(t *testing.T) {
			env := []string{
				"TERM=xterm-256color",
				"COLORTERM=" + colorTerm,
				"LC_TERMINAL=iTerm2",
			}
			if got := Resolve(env); got != colorprofile.TrueColor {
				t.Errorf("COLORTERM=%s + LC_TERMINAL=iTerm2: want TrueColor, got %s", colorTerm, got)
			}
		})
	}
}

func TestLC_Terminal_LowercaseItTerm2(t *testing.T) {
	for _, val := range []string{"iTerm2", "iterm2", "ITERM2", "ItErM2"} {
		t.Run("LC_TERMINAL="+val, func(t *testing.T) {
			env := append(env256(), "LC_TERMINAL="+val)
			if got := Resolve(env); got != colorprofile.TrueColor {
				t.Errorf("LC_TERMINAL=%s: want TrueColor, got %s", val, got)
			}
		})
	}
}

func TestLC_Terminal_NotItTerm2(t *testing.T) {
	for _, val := range []string{"xterm", "tmux", "screen", "wezterm", ""} {
		t.Run("LC_TERMINAL="+val, func(t *testing.T) {
			env := append(env256(), "LC_TERMINAL="+val)
			got := Resolve(env)
			if got == colorprofile.TrueColor {
				t.Errorf("LC_TERMINAL=%s: must NOT upgrade to TrueColor (only iTerm2 does), got %s", val, got)
			}
		})
	}
}
