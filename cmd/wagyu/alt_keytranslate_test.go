package main

// alt_keytranslate_test.go — cycle alt-keybinding-option-meta.
//
// These tests drive Update() directly with synthetic tea.KeyMsg
// events to verify the Option-as-Meta translation gate.
//
// Each test points ANTIGRAVITY_HOME at an isolated temp dir so the
// user's real ~/.gemini/config/keybindings.json is never read. The
// dispatcher is then either populated from a hand-written JSON
// file or replaced with an in-memory *keybind.Keybindings via
// m.keybindings = … before the first Update() runs.
//
// TERM_PROGRAM is set per-test to flip the terminal gate, and
// t.Cleanup resets it so subsequent tests are not affected.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/keybind"
)

// altTestMode builds a runMode wired for alt-keytranslation tests.
// The caller sets TERM_PROGRAM and (optionally) writes a
// keybindings.json under home before calling this — the helper
// only sets the env and constructs the mode.
//
// Pass an empty home path to skip file I/O entirely; the runMode
// will still pick up the term-driven gates.
func altTestMode(t *testing.T, keybindingsJSON string) *runMode {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	// keybind.DefaultPath() resolves via os.UserHomeDir() which
	// reads $HOME. Pin $HOME to the test root so the dispatcher
	// loads from a temp dir rather than the user's real home.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	geminiDir := filepath.Join(homeDir, ".gemini")
	configDir := filepath.Join(geminiDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if keybindingsJSON != "" {
		if err := os.WriteFile(filepath.Join(configDir, "keybindings.json"),
			[]byte(keybindingsJSON), 0o644); err != nil {
			t.Fatalf("write keybindings: %v", err)
		}
	}
	return NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
}

// altFires returns the letter/action pairs the dispatcher fired
// during a test. Useful for assertions without coupling to
// response-row formatting.
func (m *runMode) altFires() []altKeyFire {
	out := make([]altKeyFire, len(m.altKeyFires))
	copy(out, m.altKeyFires)
	return out
}

// withTerminal sets TERM_PROGRAM for the duration of one test and
// returns a cleanup closure. Tests that don't care about the
// terminal gate should call altTestMode and not bother with this.
func withTerminal(t *testing.T, prog string) {
	t.Helper()
	t.Setenv("TERM_PROGRAM", prog)
}

// TestAlt_TerminalApp_SynthesisesAltS verifies the headline case:
// on Terminal.app with alt+s bound, a bare Esc followed by 's'
// within the release window produces one alt+s dispatch.
//
// The Esc is held by m.altEscBuf; 's' arrives; the buffer
// synthesises alt+s; altDispatchAction records the fire.
func TestAlt_TerminalApp_SynthesisesAltS(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit"}`)

	// Bare Esc arrives first — buffered, no dispatch.
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if cmd == nil {
		t.Error("buffering an Esc should arm the release tick")
	}
	if len(m.altFires()) != 0 {
		t.Error("Esc alone should not fire anything")
	}

	// 's' follows within microseconds (within EscReleaseWindow).
	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))

	fires := m.altFires()
	if len(fires) != 1 {
		t.Fatalf("altFires has %d entries, want 1: %+v", len(fires), fires)
	}
	if fires[0].Letter != 's' || fires[0].Action != "submit" {
		t.Errorf("fire = %+v, want {s submit}", fires[0])
	}
	// The synthesised event must not have leaked into the input —
	// alt+s is not a printable rune.
	if v := m.input.Value(); v != "" {
		t.Errorf("input should not contain 's', got %q", v)
	}
}

// TestAlt_ITerm2_NativeAltS verifies the Meta-native path: on
// iTerm2 (or any terminal that forwards Option as Meta), ultraviolet
// already produces a single KeyPressMsg with Mod=ModAlt. The
// dispatcher's job is to fire the binding — no buffering involved.
func TestAlt_ITerm2_NativeAltS(t *testing.T) {
	withTerminal(t, "iTerm.app")
	m := altTestMode(t, `{"alt+s": "submit"}`)

	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModAlt, Text: "s"}))

	fires := m.altFires()
	if len(fires) != 1 {
		t.Fatalf("altFires has %d entries, want 1", len(fires))
	}
	if fires[0].Letter != 's' || fires[0].Action != "submit" {
		t.Errorf("fire = %+v, want {s submit}", fires[0])
	}
}

// TestAlt_TerminalApp_ArbitraryLetter verifies that 'alt+x'
// synthesises correctly — same path as alt+s, but exercises the
// letter parameter rather than a hard-coded 's'.
func TestAlt_TerminalApp_ArbitraryLetter(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+x": "cancel"}`)

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))

	fires := m.altFires()
	if len(fires) != 1 {
		t.Fatalf("altFires has %d entries, want 1", len(fires))
	}
	if fires[0].Letter != 'x' || fires[0].Action != "cancel" {
		t.Errorf("fire = %+v, want {x cancel}", fires[0])
	}
}

// TestAlt_NoAltsConfigured_FallsThrough is the regression guard:
// when the user has no keybindings.json (or it has no alt+letter
// entries), Esc+s must NOT swallow the Esc — the input gets
// cleared as before, and 's' inserts as text.
func TestAlt_NoAltsConfigured_FallsThrough(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, "" /* no keybindings.json */)

	m.Update(runeKey('h'))
	m.Update(runeKey('i'))
	if v := m.input.Value(); v != "hi" {
		t.Fatalf("setup: input should hold 'hi', got %q", v)
	}

	// Esc arrives: with no alts configured, the buffer gate
	// short-circuits and the Esc clears the input as before.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if v := m.input.Value(); v != "" {
		t.Errorf("Esc should clear input, got %q", v)
	}

	// 's' arrives: insert as text.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))
	if v := m.input.Value(); v != "s" {
		t.Errorf("'s' should insert into cleared input, got %q", v)
	}
	if len(m.altFires()) != 0 {
		t.Errorf("no bindings configured → no fires, got %d", len(m.altFires()))
	}
}

// TestAlt_VimMode_NotDoubleHandled verifies that an alt+s on
// Terminal.app reaches vim mode as a single alt+s event, not as
// two events (Esc-to-Normal-mode followed by s-as-insert-text).
// The vim engine's own alt+s handling runs as normal; what we
// check is the input integrity: 's' must not leak into the buffer
// as a separate rune insert.
func TestAlt_VimMode_NotDoubleHandled(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	cycle196TestHome(t, map[string]any{"editorMode": "vim"})
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.reloadVimSettings()
	if !m.vimEnabled() {
		t.Fatal("vim mode should be on")
	}
	// Inject a keybindings with alt+s.
	path := filepath.Join(filepath.Dir(os.Getenv("ANTIGRAVITY_HOME")), "config", "keybindings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"alt+s": "submit"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if kb, err := keybind.Load(path); err == nil {
		m.keybindings = kb
	}

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))

	if got := len(m.altFires()); got != 1 {
		t.Fatalf("altFires has %d entries, want 1", got)
	}
	// Vim engine owns the buffer when on; the input mirror should
	// not show 's' inserted as a literal rune.
	if v := m.input.Value(); strings.Contains(v, "s") {
		t.Errorf("input should not contain 's' from a synthesised alt+s, got %q", v)
	}
}

// TestAlt_LinuxNative_ModAlt verifies the Linux equivalent of
// iTerm2's path: any terminal that emits ModAlt directly works
// without the buffer (TERM_PROGRAM is unset on Linux so the
// NeedsOptionMeta gate returns false; we just exercise the
// dispatcher in isolation).
func TestAlt_LinuxNative_ModAlt(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	m := altTestMode(t, `{"alt+s": "submit"}`)

	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModAlt, Text: "s"}))

	if got := len(m.altFires()); got != 1 {
		t.Fatalf("altFires has %d entries, want 1", got)
	}
	if fire := m.altFires()[0]; fire.Letter != 's' || fire.Action != "submit" {
		t.Errorf("fire = %+v, want {s submit}", fire)
	}
}

// TestAlt_PlainLetter_NotMistranslated is the negative control:
// a bare 's' with no preceding Esc inserts as text. The buffer
// must not fire any alt binding, and the input must contain 's'.
func TestAlt_PlainLetter_NotMistranslated(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit", "alt+x": "cancel"}`)

	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))

	if v := m.input.Value(); v != "s" {
		t.Errorf("plain 's' should insert into input, got %q", v)
	}
	if got := len(m.altFires()); got != 0 {
		t.Errorf("plain 's' should not fire any alt binding, got %d", got)
	}
}

// TestAlt_EscTimeout_ReleasesStaleBuffer verifies the 50 ms tick
// path: a buffered Esc with no follow-up key gets released by
// EscTimeoutMsg. We simulate the timeout by clearing the buffer
// state directly (the tick fires asynchronously, but the
// production handler is exactly what we test below).
func TestAlt_EscTimeout_ReleasesStaleBuffer(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit"}`)
	m.input.SetValue("hi")

	// Buffer an Esc.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if v := m.input.Value(); v != "hi" {
		t.Errorf("Esc should not yet clear input (still buffered), got %q", v)
	}

	// Simulate the tick firing after the window by manually
	// expiring the buffer's timestamp. The handler runs the
	// Esc branch on the buffer's behalf.
	m.altEscBuf.At = time.Now().Add(-100 * time.Millisecond)
	m.Update(keybind.EscTimeoutMsg{})

	// The Esc branch fired — input cleared.
	if v := m.input.Value(); v != "" {
		t.Errorf("EscTimeoutMsg should clear the input via handleEscKey, got %q", v)
	}
}

// TestAlt_StaleBufferFallsThrough confirms that an Esc+s where the
// window has elapsed (synthetic test: clear At before the 's'
// arrives) drops the buffer and processes both keys normally.
func TestAlt_StaleBufferFallsThrough(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit"}`)
	m.input.SetValue("hi")

	// Buffer an Esc, then expire it.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m.altEscBuf.At = time.Now().Add(-100 * time.Millisecond)

	// 's' arrives; the buffer sees the window elapsed, releases
	// the Esc (clearing input), and the 's' inserts.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))

	if v := m.input.Value(); v != "s" {
		t.Errorf("after stale Esc + 's', input should be 's', got %q", v)
	}
	if got := len(m.altFires()); got != 0 {
		t.Errorf("stale buffer must not synthesise, got %d fires", got)
	}
}

// TestAlt_DoubleLetterEscapesBuffer verifies the modifier guard:
// Ctrl+Alt+s (not just Alt+s) does not match the dispatcher's
// plain alt+s shape and must not fire. This is the guard
// cmd/wagyu/alt_keytranslate.go's IsAltLetter check enforces.
func TestAlt_DoubleLetterEscapesBuffer(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit"}`)

	// Ctrl+Alt+s — extra modifier means IsAltLetter returns
	// ok=false; the dispatcher does not see this as alt+s.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModAlt | tea.ModCtrl, Text: "s"}))
	if got := len(m.altFires()); got != 0 {
		t.Errorf("Ctrl+Alt+s must not match plain alt+s, got %d fires", got)
	}
}

// TestAlt_AltAfterBufferReleasesGuard confirms a non-letter
// follow-up releases the buffered Esc without synthesising:
// the third case of EscBuffer.Feed (non-letter).
func TestAlt_NonLetterFollowUpReleases(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit"}`)
	m.input.SetValue("hi")

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	// digit follows — not a letter candidate, release the Esc.
	m.Update(tea.KeyPressMsg(tea.Key{Code: '1', Text: "1"}))

	if v := m.input.Value(); v != "1" {
		t.Errorf("Esc should clear 'hi', then '1' inserts, got %q", v)
	}
	if got := len(m.altFires()); got != 0 {
		t.Errorf("Esc+1 must not synthesise alt+letter, got %d fires", got)
	}
}
