package cliapp

import (
	"flag"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// cycle294AltScreenFlags builds the two clone-only rendering flags the
// way the CLI registers them: `--alt-screen` defaults to true,
// `--no-alt-screen` defaults to false.
func cycle294AltScreenFlags(t *testing.T, args ...string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.Bool("alt-screen", true, "")
	fs.Bool("no-alt-screen", false, "")
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return fs
}

// TestCycle294_ResolveAltScreenReadsSetting pins the precedence the
// reference gives the rendering mode:
//
//	flags  >  the `altScreenMode` setting  >  the default
//
// Measured against agy 1.4.7 (2026-10-25, raw capture of the terminal
// bytes under a pty): the reference enters the alternate buffer for
// "always" and for an absent key, and stays inline for "never". Every
// other value is rejected by the reference's settings validation and
// falls back to the default.
func TestCycle294_ResolveAltScreenReadsSetting(t *testing.T) {
	cases := []struct {
		name string
		seed map[string]any
		args []string
		want bool
	}{
		{name: "absent key means alt screen", seed: map[string]any{}, want: true},
		{name: "always means alt screen", seed: map[string]any{"altScreenMode": "always"}, want: true},
		{name: "never means inline", seed: map[string]any{"altScreenMode": "never"}, want: false},
		{name: "unknown value means alt screen", seed: map[string]any{"altScreenMode": "off"}, want: true},
		{name: "no-alt-screen wins", seed: map[string]any{"altScreenMode": "always"}, args: []string{"--no-alt-screen"}, want: false},
		{name: "explicit alt-screen wins over never", seed: map[string]any{"altScreenMode": "never"}, args: []string{"--alt-screen"}, want: true},
		{name: "explicit alt-screen=false wins", seed: map[string]any{}, args: []string{"--alt-screen=false"}, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cycle196TestHome(t, c.seed)
			fs := cycle294AltScreenFlags(t, c.args...)
			if got := resolveAltScreen(fs); got != c.want {
				t.Errorf("resolveAltScreen = %v, want %v", got, c.want)
			}
		})
	}
}

// TestCycle294_ConfigRenderingModeSwitchesSession pins the user's
// report: switching the /config Rendering Mode row to the inline view
// has to switch the *session*, not just the persisted setting.
//
// The reference does it in the same keystroke — agy 1.4.7 leaves the
// alternate buffer (?1049l) right after the row is switched.
func TestCycle294_ConfigRenderingModeSwitchesSession(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"altScreenMode": "always"})
	idx := configRowIndexForTest(t, "Rendering Mode")
	m := &runMode{altScreen: true}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := m.configRowValueAt(idx); got != "no flickering (altscreen)" {
		t.Fatalf("initial display = %q, want no flickering (altscreen)", got)
	}
	m.cycleConfigValue(+1)
	if m.altScreen {
		t.Error("after switching to inline: altScreen = true, want false")
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "altScreenMode"); got != "never" {
		t.Errorf("persisted altScreenMode = %q, want never", got)
	}
	if v := m.applyTerminalSettings(tea.NewView("body")); v.AltScreen {
		t.Error("view still carries AltScreen after the inline switch")
	}
	// And back to the alternate screen.
	m.cycleConfigValue(+1)
	if !m.altScreen {
		t.Error("after switching back: altScreen = false, want true")
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "altScreenMode"); got != "always" {
		t.Errorf("persisted altScreenMode = %q, want always", got)
	}
	if v := m.applyTerminalSettings(tea.NewView("body")); !v.AltScreen {
		t.Error("view dropped AltScreen after the altscreen switch")
	}
}

// TestCycle294_ConfigRenderingModeDefaultIsReachable pins that the
// inline stop is reachable from the default state.
//
// An absent `altScreenMode` means the alternate screen (the reference's
// default), so the row has to *display* that state — otherwise the row
// labels the default "native terminal (inline)" and both ← and → walk
// away from it, which is how the reference's own panel ends up unable
// to express the inline view.
func TestCycle294_ConfigRenderingModeDefaultIsReachable(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Rendering Mode")
	m := &runMode{altScreen: true}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := m.configRowValueAt(idx); got != "no flickering (altscreen)" {
		t.Fatalf("initial display = %q, want no flickering (altscreen)", got)
	}
	m.cycleConfigValue(-1)
	if m.altScreen {
		t.Error("after switching to inline: altScreen = true, want false")
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "altScreenMode"); got != "never" {
		t.Errorf("persisted altScreenMode = %q, want never", got)
	}
}

// TestCycle294_AltScreenModeValue pins the value → flag mapping on its
// own, including the values the reference rejects.
func TestCycle294_AltScreenModeValue(t *testing.T) {
	cases := []struct {
		stored any
		want   bool
	}{
		{stored: nil, want: true},
		{stored: "", want: true},
		{stored: "always", want: true},
		{stored: "never", want: false},
		{stored: "off", want: true},
		{stored: "inline", want: true},
		{stored: "default", want: true},
		{stored: true, want: true},
	}
	for _, c := range cases {
		if got := altScreenModeValue(c.stored); got != c.want {
			t.Errorf("altScreenModeValue(%v) = %v, want %v", c.stored, got, c.want)
		}
	}
}
