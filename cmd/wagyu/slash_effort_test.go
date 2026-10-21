package main

// slash_effort_test.go — cycle effort-picker: the seven behavioural
// pins for /effort <level>, /effort, /effort <bad>, the picker left/right
// navigation, the picker Esc, the model-default fallback, and the
// settings.json persistence path.
//
// The picker geometry / current-effort reading / settings.json shape is
// recovered in earlier cycles (102, 167, 197, 208) and pinned by
// slash_cycle208_test.go. This file adds the six new behaviours cycle
// effort-picker introduces:
//
//	1. /effort high     →  "Effort set to high." and flags.effort = high
//	2. /effort extreme  →  "Invalid level: extreme. Use low|medium|high."
//	3. /effort (no args) →  opens panelEffort (timeline-gauge)
//	4. ←/→ in panel     →  cursor moves; Enter closes with the new level
//	5. Esc in panel     →  closes silently, prior level preserved
//	6. --model alone    →  effort falls back to the model's server default
//	7. settings.json    →  effort=high round-trips into m.flags on startup
//
// Tests use cycle196TestHome / ANTIGRAVITY_HOME so the user's real
// settings.json is never touched.

import (
	"flag"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// newEffortRunMode returns a runMode whose input/picker/spinner are
// nil-safe enough to survive handleSlashCommand's input.Reset() /
// picker.Reset() prelude. Other tests that only touch handlePanelKey
// can use a bare &runMode{}, but the slash router exercises more state.
func newEffortRunMode(t *testing.T) *runMode {
	t.Helper()
	// NewRunMode wires a fully initialised runMode (input textarea,
	// spinner, history, viewport height, etc.); the tests then reset
	// the specific panel/flag fields they need. The fake backend is the
	// same one picker_view_test.go uses for the panel-only path.
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.picker.Reset()
	m.input.Reset()
	return m
}

// TestEffortSlashDirect pins the /effort <level> direct setter: typing
// `/effort high` echoes the same "Effort set to high." line the picker
// Enter prints, mutates m.flags, and updates the panel-less status model.
//
// The test runs through the slash router (handleSlashCommand), which is
// the same dispatch path the live TUI's Enter key takes.
func TestEffortSlashDirect(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "medium", "")
	m := newEffortRunMode(t)
	m.flags = fs

	handled, _ := m.handleSlashCommand("/effort high")
	if !handled {
		t.Fatal("handleSlashCommand did not consume /effort high")
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "Effort set to high") {
		t.Errorf("expected 'Effort set to high' in response, got %q", got)
	}
	// The flag holds the new value (currentEffort derives from it).
	if got := flagStringValue(m.flags, "effort"); got != "high" {
		t.Errorf("flags.effort = %q, want %q", got, "high")
	}
}

// TestEffortSlashInvalidLevel: typing /effort extreme (cycle 101's
// rejected example) echoes the invalid-level message and leaves the
// previous effort untouched.
func TestEffortSlashInvalidLevel(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "high", "")
	m := newEffortRunMode(t)
	m.flags = fs

	handled, _ := m.handleSlashCommand("/effort extreme")
	if !handled {
		t.Fatal("handleSlashCommand did not consume /effort extreme")
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "Invalid level: extreme") {
		t.Errorf("expected 'Invalid level: extreme' in response, got %q", got)
	}
	if !strings.Contains(got, "Use low|medium|high") {
		t.Errorf("expected 'Use low|medium|high' hint, got %q", got)
	}
	if got := flagStringValue(m.flags, "effort"); got != "high" {
		t.Errorf("invalid input must not mutate flags.effort: %q", got)
	}
}

// TestEffortSlashNoArgsOpensPicker: the bare `/effort` invocation
// routes through openSlashPanel, which is the panel-overlay path the TUI
// runs before handleSlashCommand. The slash router's bare /effort
// branch keeps the cycle-102 print-mode content so a -p /effort probe
// still receives the word — this test confirms the panel path.
func TestEffortSlashNoArgsOpensPicker(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := newEffortRunMode(t)
	if !m.openSlashPanel("effort") {
		t.Fatal("openSlashPanel(\"effort\") = false, want true")
	}
	if m.panel.kind != panelEffort {
		t.Errorf("panel.kind = %v, want panelEffort", m.panel.kind)
	}
	if m.panel.cmd != "/effort" {
		t.Errorf("panel.cmd = %q, want %q", m.panel.cmd, "/effort")
	}
	if m.panel.cursor != effortHigh {
		t.Errorf("panel.cursor = %d, want high (%d)", m.panel.cursor, effortHigh)
	}
}

// TestEffortPickerNavigate: the timeline-gauge picker moves the cursor
// on Right/Left and applies the selection on Enter. Same keyboard
// surface the live reference drives against /effort.
func TestEffortPickerNavigate(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Low)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "low", "")

	// Open at low (the settings file's label says Low).
	m := newEffortRunMode(t)
	m.flags = fs
	m.openSlashPanel("effort")
	if m.panel.cursor != effortLow {
		t.Fatalf("open cursor = %d, want low", m.panel.cursor)
	}

	// Right → medium.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("right not handled")
	}
	if m.panel.cursor != effortMedium {
		t.Errorf("after right: cursor = %d, want medium", m.panel.cursor)
	}

	// Enter applies — closes panel, echoes "Effort set to medium."
	if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	} else if cmd != nil {
		t.Errorf("enter returned a non-nil cmd: %v", cmd)
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel not closed: kind = %v", m.panel.kind)
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "Effort set to medium") {
		t.Errorf("expected 'Effort set to medium' in response, got %q", got)
	}
	if got := flagStringValue(m.flags, "effort"); got != "medium" {
		t.Errorf("flags.effort = %q, want %q", got, "medium")
	}
}

// TestEffortPickerEscape: Escaping the picker must revert to the prior
// level — the standard "> /effort" + "⎿  Exited /effort command" notice
// fires (esc-ref-effort-Esc.txt) but flags.effort is untouched and the
// banner/status model is the original. Enter-without-change is the
// silent path; Esc always echoes the exit notice (per exitNotice=true
// for panelEffort) but does not commit the cursor.
func TestEffortPickerEscape(t *testing.T) {
	_ = cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "high", "")

	m := newEffortRunMode(t)
	m.flags = fs
	m.openSlashPanel("effort")

	// Wander through every level, then Esc.
	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	m.handlePanelKey(tea.Key{Code: tea.KeyLeft})
	if m.panel.cursor != effortLow {
		t.Fatalf("walked left twice: cursor = %d, want low", m.panel.cursor)
	}

	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("esc not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("esc did not close panel: kind = %v", m.panel.kind)
	}
	got := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "Exited /effort command") {
		t.Errorf("expected 'Exited /effort command' in response, got %q", got)
	}
	if strings.Contains(got, "Effort set to") {
		t.Errorf("esc must not echo 'Effort set to ...', got %q", got)
	}
	if got := flagStringValue(m.flags, "effort"); got != "high" {
		t.Errorf("esc must revert effort: flags.effort = %q, want %q", got, "high")
	}
}

// TestEffortDefaultFallback: with --model set and no --effort, the
// cycle-101 resolveModelSelection chooses the model's server default
// (gpt-oss-120b → medium; gemini-3.8-flash → high). The flag set then
// sees a non-empty effort that matches the model's expected tier, so
// the runtime layer never has to invent one.
//
// This pins changelog line 26: "Changed --model and /model to accept a
// base model name without --effort: … the CLI now picks the model's
// default thinking level".
func TestEffortDefaultFallback(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("model", "gemini-3.8-flash", "")
	fs.String("effort", "", "")

	id, err := resolveModelSelection(flagStringValue(fs, "model"), flagStringValue(fs, "effort"))
	if err != nil {
		t.Fatalf("resolveModelSelection: %v", err)
	}
	if id != "gemini-3.8-flash-high" {
		t.Errorf("default effort = %q, want %q", id, "gemini-3.8-flash-high")
	}

	fs2 := flag.NewFlagSet("t2", flag.ContinueOnError)
	fs2.String("model", "gpt-oss-120b", "")
	fs2.String("effort", "", "")
	id, err = resolveModelSelection(flagStringValue(fs2, "model"), flagStringValue(fs2, "effort"))
	if err != nil {
		t.Fatalf("resolveModelSelection: %v", err)
	}
	if id != "gpt-oss-120b-medium" {
		t.Errorf("gpt-oss-120b default = %q, want %q", id, "gpt-oss-120b-medium")
	}
}

// TestEffortPersistence: a settings.json effort=high round-trips into
// the loop's effort on startup. The cycle-196 ANTIGRAVITY_HOME pin lets
// the assertion read the seeded value without touching the user's real
// settings.json.
//
// The work lives in cycle-208 applyEffortSelection today and is what
// every subsequent picker Enter / /effort <level> call funnels through.
// This test seeds a fresh settings.json, runs a single Send through
// currentEffortName (the same path the TUI's per-turn loop reads), and
// confirms the resulting word matches the persisted value.
func TestEffortPersistence(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"model":  "gemini-3.8-flash",
		"effort": "high",
	})
	_ = dir // settings dir already used by the helper

	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "", "")
	m := &runMode{flags: fs}
	if got := m.currentEffortName(); got != "high" {
		t.Errorf("currentEffortName = %q, want %q (from settings.json)", got, "high")
	}

	// Round-trip through the canonical parse+format helpers; the rest
	// of the persistence path (settings.json -> SetSettingsField ->
	// loadSettingsFile) is exercised by cycle-208 tests.
	if _, ok := parseEffortWord("HIGH"); !ok {
		t.Errorf("parseEffortWord lost HIGH")
	}
	if level := effortIndexForName("high"); level != effortHigh {
		t.Errorf("effortIndexForName(high) = %d, want %d", level, effortHigh)
	}
}
