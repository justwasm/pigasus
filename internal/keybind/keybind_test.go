package keybind

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestParseBinding_AltLetter(t *testing.T) {
	mod, r, ok := ParseBinding("alt+s")
	if !ok || mod != tea.ModAlt || r != 's' {
		t.Fatalf("alt+s → (%v, %q, %v)", mod, r, ok)
	}
}

func TestParseBinding_CtrlShiftAltLetter(t *testing.T) {
	mod, r, ok := ParseBinding("ctrl+shift+alt+x")
	if !ok {
		t.Fatal("ctrl+shift+alt+x should parse")
	}
	if !mod.Contains(tea.ModCtrl) || !mod.Contains(tea.ModShift) || !mod.Contains(tea.ModAlt) {
		t.Errorf("missing modifier bits: %v", mod)
	}
	if r != 'x' {
		t.Errorf("rune = %q, want x", r)
	}
}

func TestParseBinding_RejectsBadShapes(t *testing.T) {
	bad := []string{
		"s",         // no modifier
		"alt+shift", // multi-word tail, not a single rune
		"alt+1",     // digit, not a letter
		"meta+s",    // unknown modifier
		"",          // empty
		"alt+",
	}
	for _, name := range bad {
		if _, _, ok := ParseBinding(name); ok {
			t.Errorf("ParseBinding(%q) should reject", name)
		}
	}
}

func TestIsAltLetter_OnlyAltMod(t *testing.T) {
	cases := []struct {
		key  tea.Key
		want bool
		r    rune
	}{
		{tea.Key{Code: 's', Mod: tea.ModAlt}, true, 's'},
		{tea.Key{Code: 'A', Mod: tea.ModAlt}, true, 'a'},
		{tea.Key{Code: 's', Mod: tea.ModAlt | tea.ModCtrl}, false, 0},
		{tea.Key{Code: 's', Mod: tea.ModAlt | tea.ModShift}, false, 0},
		{tea.Key{Code: 's'}, false, 0},
		{tea.Key{Code: tea.KeyEsc, Mod: tea.ModAlt}, false, 0},
		{tea.Key{Code: '1', Mod: tea.ModAlt}, false, 0},
	}
	for _, c := range cases {
		got, ok := IsAltLetter(c.key)
		if ok != c.want {
			t.Errorf("IsAltLetter(%+v) ok=%v want=%v", c.key, ok, c.want)
		}
		if ok && got != c.r {
			t.Errorf("IsAltLetter(%+v) rune=%q want=%q", c.key, got, c.r)
		}
	}
}

func TestLoad_MissingFileIsEmpty(t *testing.T) {
	dir := t.TempDir()
	k, err := Load(filepath.Join(dir, "keybindings.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if k.Has('s') {
		t.Error("missing file should have no alt+s binding")
	}
}

func TestLoad_PopulatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	body := `{"alt+s": "submit", "alt+x": "cancel", "bad": "skip", "alt+1": "skip"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	k, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if action, ok := k.LookupAltLetter('s'); !ok || action != "submit" {
		t.Errorf("alt+s → (%q, %v), want (\"submit\", true)", action, ok)
	}
	if action, ok := k.LookupAltLetter('x'); !ok || action != "cancel" {
		t.Errorf("alt+x → (%q, %v), want (\"cancel\", true)", action, ok)
	}
	if _, ok := k.LookupAltLetter('q'); ok {
		t.Error("alt+q should not be bound")
	}
}

func TestLoad_SkipsEmptyActions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	body := `{"alt+s": "", "alt+x": "  "}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	k, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := k.LookupAltLetter('s'); ok {
		t.Error("empty action should be skipped")
	}
}

func TestLoad_EmptyObjectIsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	k, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if k.Has('s') {
		t.Error("{} should have no bindings")
	}
}

func TestLoad_BadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keybindings.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Error("bad JSON should error")
	}
}

func TestEscBuffer_NextLetterSynthesises(t *testing.T) {
	var b EscBuffer
	if !b.Start(time.Now()) {
		t.Fatal("Start should consume a bare Esc when not pending")
	}
	synth := rune(0)
	released := 0
	consumed := b.Feed(
		tea.Key{Code: 's'},
		func(r rune) bool { return r == 's' },
		func(r rune) { synth = r },
		func() { released++ },
		time.Now(),
	)
	if !consumed {
		t.Error("Feed should return consumed=true on synthesis")
	}
	if synth != 's' {
		t.Errorf("synth=%q, want s", synth)
	}
	if released != 0 {
		t.Errorf("released=%d, want 0 (synth should not release)", released)
	}
}

func TestEscBuffer_NoBindingReleases(t *testing.T) {
	var b EscBuffer
	b.Start(time.Now())
	released := 0
	consumed := b.Feed(
		tea.Key{Code: 's'},
		func(r rune) bool { return false }, // no binding for alt+s
		func(r rune) {},
		func() { released++ },
		time.Now(),
	)
	if consumed {
		t.Error("Feed should return consumed=false when no binding")
	}
	if released != 1 {
		t.Errorf("released=%d, want 1", released)
	}
	if b.Pending {
		t.Error("Pending should be cleared")
	}
}

func TestEscBuffer_NonLetterReleases(t *testing.T) {
	var b EscBuffer
	b.Start(time.Now())
	released := 0
	consumed := b.Feed(
		tea.Key{Code: '1'},
		func(r rune) bool { return true },
		func(r rune) {},
		func() { released++ },
		time.Now(),
	)
	if consumed {
		t.Error("digit should not be consumed")
	}
	if released != 1 {
		t.Errorf("released=%d, want 1", released)
	}
}

func TestEscBuffer_WindowElapsedReleases(t *testing.T) {
	var b EscBuffer
	past := time.Now().Add(-100 * time.Millisecond)
	b.At = past
	b.Pending = true
	released := 0
	consumed := b.Feed(
		tea.Key{Code: 's'},
		func(r rune) bool { return true },
		func(r rune) {},
		func() { released++ },
		time.Now(),
	)
	if consumed {
		t.Error("stale buffer should not synthesise")
	}
	if released != 1 {
		t.Errorf("released=%d, want 1", released)
	}
	if b.Pending {
		t.Error("Pending should be cleared")
	}
}

func TestEscBuffer_UppercaseLetter(t *testing.T) {
	var b EscBuffer
	b.Start(time.Now())
	synth := rune(0)
	consumed := b.Feed(
		tea.Key{Code: 'S'},
		func(r rune) bool { return r == 's' },
		func(r rune) { synth = r },
		func() {},
		time.Now(),
	)
	if !consumed {
		t.Fatal("uppercase S should also synthesise (lowercased)")
	}
	if synth != 's' {
		t.Errorf("synth=%q, want s", synth)
	}
}

func TestEscBuffer_NoPendingPassesThrough(t *testing.T) {
	var b EscBuffer
	released := 0
	consumed := b.Feed(
		tea.Key{Code: 's'},
		func(r rune) bool { return true },
		func(r rune) {},
		func() { released++ },
		time.Now(),
	)
	if consumed {
		t.Error("no pending Esc → pass through")
	}
	if released != 0 {
		t.Errorf("released=%d, want 0", released)
	}
}

func TestEscBuffer_StartIsIdempotentWhilePending(t *testing.T) {
	var b EscBuffer
	if !b.Start(time.Now()) {
		t.Fatal("first Start should consume")
	}
	if b.Start(time.Now()) {
		t.Error("second Start while pending should not consume (caller forgot to dispatch)")
	}
}

func TestTermNeedsOptionMeta(t *testing.T) {
	cases := []struct {
		prog string
		want bool
	}{
		{"Apple_Terminal", true},
		{"vscode", true},
		{"iTerm.app", false},
		{"WezTerm", false},
		{"kitty", false},
		{"Alacritty", false},
		{"ghostty", false},
		{"", false},
	}
	for _, c := range cases {
		t.Setenv("TERM_PROGRAM", c.prog)
		if got := TermNeedsOptionMeta(); got != c.want {
			t.Errorf("TERM_PROGRAM=%q → %v, want %v", c.prog, got, c.want)
		}
	}
}
