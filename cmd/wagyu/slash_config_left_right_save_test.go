package main

// slash_config_left_right_save_test.go —
// feature/config-left-right-save.
//
// The /config picker's ←/→ keys have, since cycle 196, both advanced the
// highlighted setting's value AND written the new value to
// ~/.gemini/antigravity-cli/settings.json before the next render. The
// deliverable closes TODO.md P4's "`/config ←/→ (change value) ... still
// inert" item and pins the AGY 1.2.16 changelog behaviour:
//
//	Added ←/→ shortcuts to /config: on a highlighted setting they
//	switch to the previous or next value and save it right away,
//	without opening the dropdown, and the footer shows a
//	'←/→ Change' hint
//
// (internal/cli/testdata/changelog_reference.txt line 102.)
//
// These tests are documentation as much as assertion. Each test sets
// ANTIGRAVITY_HOME to a tempdir so the user's real settings file is
// never touched.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// configLeftRightVerbosityIdx returns the row index of the Verbosity
// row. The catalogue is recovered from probe_ref_walk2.py; Verbosity
// is one of the 20 top-level rows in the order produced by the
// REProbe. Rather than depend on that order shifting across cycles,
// look the row up by name (configRows().name).
func configLeftRightVerbosityIdx(t *testing.T) int {
	t.Helper()
	for i, r := range configRows() {
		if r.name == "Verbosity" {
			return i
		}
	}
	t.Fatalf("configRows has no row named \"Verbosity\" (catalogue shrank)")
	return -1
}

// readConfigLRSettings parses the test's settings.json into a map.
// Failures abort the test. Used by the persistence and "no change"
// assertions.
func readConfigLRSettings(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse settings.json: %v", err)
	}
	return m
}

// configLRValueAt walks the dot path and returns the leaf as a string
// (booleans render as "on"/"off"). Failures abort the test. Mirrors
// cycle196ValueAt but is renamed so the deliverable can stand alone
// without bleeding into cycle 196's test scaffold.
func configLRValueAt(t *testing.T, m map[string]any, path string) string {
	t.Helper()
	parts := strings.Split(path, ".")
	var cur any = m
	for _, p := range parts {
		obj, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("path %q: %T is not an object", path, cur)
		}
		cur, ok = obj[p]
		if !ok {
			t.Fatalf("path %q: missing key %q", path, p)
		}
	}
	switch v := cur.(type) {
	case string:
		return v
	case bool:
		if v {
			return "on"
		}
		return "off"
	}
	t.Fatalf("path %q: leaf has unexpected type %T", path, cur)
	return ""
}

// TestConfigLeftRight_AdvancesValue walks the Verbosity cycle in both
// directions and pins its order. The catalogue's cycle list for
// Verbosity is, verbatim from probe_ref_walk2.py row 17 and the
// [medium, low, high] ordering in cmd/wagyu/slash_panels.go:
//
//	Verbosity  medium  low  high
//
// From the absent-key default ("medium"):
//
//	→ Right (dir=+1)      index 0 → 1 → "low"
//	→ Right (dir=+1) again index 1 → 2 → "high"
//	→ Right (dir=+1) again index 2 → 0 → "medium" (wrap)
//	← Left  (dir=-1)      index 0 → 2 → "high" (wrap back)
//
// The wrap assertion is what distinguishes a real cycle from a
// numeric-arrow "increment index until end" sanity stub. A no-op or
// silent-fail cycleConfigValue would keep "medium" forever.
func TestConfigLeftRight_AdvancesValue(t *testing.T) {
	cycle196TestHome(t, map[string]any{}) // empty settings → default

	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: configLeftRightVerbosityIdx(t)}

	// Sanity: the default reads as the row's first cycle value.
	if v := configSettingValue(m.panel.cursor); v != "medium" {
		t.Fatalf("baseline: verbosity = %q, want \"medium\"", v)
	}

	m.cycleConfigValue(+1) // medium → low
	if v := configSettingValue(m.panel.cursor); v != "low" {
		t.Errorf("after 1× Right: verbosity = %q, want \"low\"", v)
	}

	m.cycleConfigValue(+1) // low → high
	if v := configSettingValue(m.panel.cursor); v != "high" {
		t.Errorf("after 2× Right: verbosity = %q, want \"high\"", v)
	}

	m.cycleConfigValue(+1) // high → medium (wrap forward)
	if v := configSettingValue(m.panel.cursor); v != "medium" {
		t.Errorf("after 3× Right (wrap): verbosity = %q, want \"medium\"", v)
	}

	m.cycleConfigValue(-1) // medium → high (wrap backward)
	if v := configSettingValue(m.panel.cursor); v != "high" {
		t.Errorf("after Left (wrap back): verbosity = %q, want \"high\"", v)
	}
}

// TestConfigLeftRight_PersistsToDisk is the core persistence pin:
// after a single ←/→ the new value must be visible in
// ~/.gemini/antigravity-cli/settings.json, not just in the in-memory
// runMode. The "immediate" wording in the AGY 1.2.16 changelog is
// what this asserts — the write happens inside cycleConfigValue, not
// at some later "save settings" keystroke.
//
// The test seeds with colorScheme="light" (unrelated to verbosity) so
// that the file already exists when cycleConfigValue writes, and the
// helper that handles a missing key (cycleConfigValue → "default
// cycle list start") still has its settings.json to mutate.
func TestConfigLeftRight_PersistsToDisk(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "light",
	})

	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: configLeftRightVerbosityIdx(t)}

	// Snapshot the disk up front so we can prove the *write* happened.
	pre := readConfigLRSettings(t, dir)
	if _, ok := pre["verbosity"]; ok {
		t.Fatalf("baseline: verbosity already in disk as %v; rewrite the test", pre["verbosity"])
	}

	m.cycleConfigValue(+1) // medium → low (write "low" to settings.json)

	got := readConfigLRSettings(t, dir)
	if v, ok := got["verbosity"].(string); !ok || v != "low" {
		t.Errorf("after Right: settings.json verbosity = %v, want \"low\"", got["verbosity"])
	}

	// The unrelated seed key MUST survive untouched — persistence is
	// additive, not a wholesale rewrite.
	if v := configLRValueAt(t, got, "colorScheme"); v != "light" {
		t.Errorf("colorScheme clobbered: %q, want \"light\"", v)
	}
}

// TestConfigLeftRight_NoDropdownOpen confirms the user-facing
// behaviour called out in the changelog: ←/→ changes the value and
// saves it "without opening the dropdown". The picker must stay in
// its base view — no sub-modal, no detail overlay, no stage bump.
//
// The assertions are on panel state that, if any other cycle had
// tried to open a dropdown, would have flipped:
//
//   - m.panel.kind        stays panelConfig
//   - m.panel.detail      stays false (skills sub-view; different
//                          panel, defensive)
//   - m.panel.stage       stays 0 (feedback free-text stage)
//   - m.panel.diffView    stays 0 (diff inline)
//   - m.panel.cursor      unchanged after Left/Right (Up/Down are
//                          the cursor-bearing keys here)
//   - the panel renderer's line count is unchanged
//
// The line-count check is the most direct "no overlay appeared"
// signal: every panel sub-view in this codebase adds at least one
// row.
func TestConfigLeftRight_NoDropdownOpen(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := mkPanel(panelConfig, "/config", configLeftRightVerbosityIdx(t))
	preLines := plainLines(m)

	// Snapshot the dropdown-opening state before the press.
	preKind := m.panel.kind
	preDetail := m.panel.detail
	preStage := m.panel.stage
	preDiff := m.panel.diffView
	preCursor := m.panel.cursor

	// Right → advances verbosity to "low"; must NOT mutate any of
	// the panel overlay state.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right reported unhandled on /config")
	}

	for name, pair := range map[string]struct {
		pre, got any
	}{
		"kind":     {preKind, m.panel.kind},
		"detail":   {preDetail, m.panel.detail},
		"stage":    {preStage, m.panel.stage},
		"diffView": {preDiff, m.panel.diffView},
		"cursor":   {preCursor, m.panel.cursor},
	} {
		if pair.pre != pair.got {
			t.Errorf("%s changed: pre=%v got=%v", name, pair.pre, pair.got)
		}
	}

	postLines := plainLines(m)
	if len(preLines) != len(postLines) {
		t.Errorf("panel line count changed after Right: pre=%d post=%d\npre:\n%s\npost:\n%s",
			len(preLines), len(postLines),
			strings.Join(preLines, "\n"), strings.Join(postLines, "\n"))
	}

	// And Left, for symmetry.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyLeft}); !handled {
		t.Fatal("Left reported unhandled on /config")
	}
	if m.panel.kind != panelConfig {
		t.Errorf("Left mutated panel.kind = %v, want panelConfig", m.panel.kind)
	}
	post2 := plainLines(m)
	if len(preLines) != len(post2) {
		t.Errorf("panel line count changed after Left: pre=%d post=%d", len(preLines), len(post2))
	}
}

// TestConfigLeftRight_FooterHint pins the second clause of the
// changelog entry: "the footer shows a '←/→ Change' hint". The
// picker's keyboard legend row is, verbatim from cycle 196's
// panelConfigLines source:
//
//	panelKeyboardHint("↑/↓", "Navigate", "←/→", "Change",
//	    "enter", "Select", "esc", "Clear Search/Exit")
//
// After ANSI stripping the rendered legend should contain both the
// arrow-glyph key form and the "Change" label form. The changelog
// shorthand "<-/-> Change" is the ASCII fallback the reference
// documents use when terminal capture collapses the arrow glyphs;
// we accept either form so the test is not over-coupled to the
// exact rune the renderer emits.
func TestConfigLeftRight_FooterHint(t *testing.T) {
	cycle196TestHome(t, map[string]any{})

	m := mkPanel(panelConfig, "/config", 0)
	plain := plainLines(m)
	if len(plain) == 0 {
		t.Fatal("panelLines returned no rows")
	}
	legend := plain[len(plain)-1]

	if !strings.Contains(legend, "Change") {
		t.Errorf("legend missing \"Change\" label: %q", legend)
	}
	hasArrowForm := strings.Contains(legend, "←/→")
	hasDashForm := strings.Contains(legend, "<-/->")
	if !hasArrowForm && !hasDashForm {
		t.Errorf("legend missing \"←/→\" or \"<-/->\" key form: %q", legend)
	}
}

// TestConfigLeftRight_NonEnumSetting pins the safety floor for
// rows that cannot be cycled. The catalogue contains 20 rows and
// every one of them is an enum with len(values) >= 2, but the
// guard inside cycleConfigValue (length<2 → return) is the
// canonical non-enum no-op contract. Two complementary checks:
//
//   1. Structural: every catalogue row carries at least two values
//      (so the catalogue never accidentally adds a one-element row
//      that the render would render but the keys could not act on).
//   2. Behavioural: an out-of-range cursor leaves settings.json
//      untouched and does not panic.
//
// The structural check guarantees (1) is genuine; the behavioural
// check exercises the early-return guard directly so a future
// regression that adds a non-enum row cannot hide behind "the
// catalogue has no such row today".
func TestConfigLeftRight_NonEnumSetting(t *testing.T) {
	// 1. Structural: every catalogue row has >=2 cycle values.
	for i, r := range configRows() {
		if len(r.values) < 2 {
			t.Errorf("row %d (%q): len(values)=%d, want >=2 (←/→ has nothing to cycle)", i, r.name, len(r.values))
		}
	}

	// 2. Behavioural: cursor past the end of the catalogue.
	dir := cycle196TestHome(t, map[string]any{})
	pre := readConfigLRSettings(t, dir)

	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: len(configRows()) + 5}

	// Must not panic.
	m.cycleConfigValue(+1)
	m.cycleConfigValue(-1)

	post := readConfigLRSettings(t, dir)
	if len(pre) != len(post) {
		t.Errorf("settings.json mutated despite out-of-range cursor: pre=%v post=%v", pre, post)
	}

	// And: handlePanelKey's panelConfig branch does not crash on a
	// negative cursor either (which a key press cannot produce, but
	// the guard inside cycleConfigValue is the only thing keeping
	// the renderer safe).
	m.panel.cursor = -1
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right at cursor=-1 reported unhandled; want handled (guard returns early)")
	}
	if m.panel.kind != panelConfig {
		t.Errorf("cursor=-1 mutated panel.kind=%v, want panelConfig", m.panel.kind)
	}
}
