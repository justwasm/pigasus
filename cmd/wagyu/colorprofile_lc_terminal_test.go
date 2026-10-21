// colorprofile_lc_terminal_test.go — v1.3.5 changelog gap tests.
//
// Each Test* here pins one of the precedence rules in
// resolveColorProfile's doc comment. Every test passes a synthetic
// env slice so the result is deterministic — none of them touch
// the host shell's COLORTERM, LC_TERMINAL, or NO_COLOR.
//
// A xterm-256color TERM is set in every test so the upstream
// colorprofile.Env base lands at ANSI256 (the realistic
// "typical color terminal" case the v1.3.5 changelog targets),
// and the LC_TERMINAL rule's "upgrade" from 256→TrueColor is
// observable in the test result.
package main

import (
	"testing"

	"github.com/charmbracelet/colorprofile"
)

// env256 returns an env slice that says "I'm a typical 256-color
// terminal, nothing else set". Used as the base for every test
// below; tests then mutate it before calling resolveColorProfile.
func env256() []string {
	return []string{"TERM=xterm-256color"}
}

// TestLC_Terminal_TrueColor confirms the v1.3.5 changelog gap:
// COLORTERM=truecolor alone → TrueColor. This is the behaviour
// upstream colorprofile already provides; the test exists to
// pin the rule at this layer.
func TestLC_Terminal_TrueColor(t *testing.T) {
	env := append(env256(), "COLORTERM=truecolor")
	if got := resolveColorProfile(env); got != colorprofile.TrueColor {
		t.Errorf("COLORTERM=truecolor: want TrueColor, got %s", got)
	}
}

// TestLC_Terminal_ItTerm2Only is the headline test: iTerm2's
// LC_TERMINAL hint is honoured as 24-bit even when COLORTERM is
// absent (the SSH-over-iTerm2 case from the changelog). Without
// this fix upstream colorprofile.Env returns ANSI256 (the TERM
// default), and the CLI downgrades its true-colour palette to
// 256-colour indices.
func TestLC_Terminal_ItTerm2Only(t *testing.T) {
	env := append(env256(), "LC_TERMINAL=iTerm2")
	if got := resolveColorProfile(env); got != colorprofile.TrueColor {
		t.Errorf("LC_TERMINAL=iTerm2 only: want TrueColor, got %s", got)
	}
}

// TestLC_Terminal_NeitherSet pins the negative case: when neither
// COLORTERM nor LC_TERMINAL is set on a typical 256-colour
// terminal, we must NOT spuriously upgrade to TrueColor. The
// expected profile is ANSI256 — the upstream default for
// TERM=xterm-256color.
func TestLC_Terminal_NeitherSet(t *testing.T) {
	env := env256()
	got := resolveColorProfile(env)
	if got == colorprofile.TrueColor {
		t.Errorf("no colorterm/env set: must NOT be TrueColor, got %s", got)
	}
	if got != colorprofile.ANSI256 {
		t.Errorf("no colorterm/env set: want ANSI256 (TERM=xterm-256color base), got %s", got)
	}
}

// TestLC_Terminal_NoColorWins confirms the hard opt-out: NO_COLOR
// wins over LC_TERMINAL=iTerm2 (a user who set NO_COLOR is asking
// for no colour, period). This is rule 1 in resolveColorProfile's
// precedence and must never be overridden by rule 3.
//
// NO_COLOR is checked with strconv.ParseBool — the same call
// charmbracelet/colorprofile makes upstream. That means the
// convention "NO_COLOR is set, regardless of value" (the
// no-color.org wording) is honoured only for ParseBool-truthy
// values: "1", "t", "T", "TRUE", "true", "True". We mirror that
// intentionally so our rule 1 behaves identically to the library
// the rest of the colour code reads from; nothing surprising.
func TestLC_Terminal_NoColorWins(t *testing.T) {
	for _, noCol := range []string{"1", "true", "True", "TRUE"} {
		t.Run("NO_COLOR="+noCol, func(t *testing.T) {
			env := []string{"TERM=xterm-256color", "LC_TERMINAL=iTerm2", "NO_COLOR=" + noCol}
			got := resolveColorProfile(env)
			if got != colorprofile.ASCII {
				t.Errorf("NO_COLOR=%s + LC_TERMINAL=iTerm2: want ASCII (NO_COLOR hard opt-out), got %s", noCol, got)
			}
		})
	}
}

// TestLC_Terminal_ColorTermWins confirms the second-tier rule:
// when both COLORTERM (a truecolor-grade entry like "24bit") and
// LC_TERMINAL=iTerm2 are set, we still resolve to TrueColor. This
// is the easy half of "both say 24-bit, both agree" — the
// interesting half is that no downgrade happens.
func TestLC_Terminal_ColorTermWins(t *testing.T) {
	for _, colorTerm := range []string{"truecolor", "24bit", "yes", "true"} {
		t.Run("COLORTERM="+colorTerm, func(t *testing.T) {
			env := []string{
				"TERM=xterm-256color",
				"COLORTERM=" + colorTerm,
				"LC_TERMINAL=iTerm2",
			}
			if got := resolveColorProfile(env); got != colorprofile.TrueColor {
				t.Errorf("COLORTERM=%s + LC_TERMINAL=iTerm2: want TrueColor, got %s", colorTerm, got)
			}
		})
	}
}

// TestLC_Terminal_LowercaseItTerm2 is a small fuzz test for
// the EqualFold check: iTerm2 is documented as "iTerm2", but some
// shells export "iterm2" or "ITERM2". The upgrade should still
// fire.
func TestLC_Terminal_LowercaseItTerm2(t *testing.T) {
	for _, val := range []string{"iTerm2", "iterm2", "ITERM2", "ItErM2"} {
		t.Run("LC_TERMINAL="+val, func(t *testing.T) {
			env := append(env256(), "LC_TERMINAL="+val)
			if got := resolveColorProfile(env); got != colorprofile.TrueColor {
				t.Errorf("LC_TERMINAL=%s: want TrueColor, got %s", val, got)
			}
		})
	}
}

// TestLC_Terminal_NotItTerm2 confirms a negative case for rule 3:
// arbitrary LC_TERMINAL values that are NOT "iTerm2" must NOT
// trigger the upgrade. Only the iTerm2-specific hint should fire;
// otherwise we'd be doing terminal fingerprinting on the wrong
// variable.
func TestLC_Terminal_NotItTerm2(t *testing.T) {
	for _, val := range []string{"xterm", "tmux", "screen", "wezterm", ""} {
		t.Run("LC_TERMINAL="+val, func(t *testing.T) {
			env := append(env256(), "LC_TERMINAL="+val)
			got := resolveColorProfile(env)
			if got == colorprofile.TrueColor {
				t.Errorf("LC_TERMINAL=%s: must NOT upgrade to TrueColor (only iTerm2 does), got %s", val, got)
			}
		})
	}
}
