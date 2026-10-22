package main

// slash_cycle196_test.go — cycle 196: deep /config picker.
//   - ←/→ cycles the value of the highlighted setting and persists to
//     settings.json (auth.SetSettingsField).
//   - The next render reads the live value through auth.SettingsField.
//   - Settings with a dot-notation key (statusLine.enabled, title.enabled)
//     are stored as nested objects in settings.json; the helper walks the
//     path on read and creates the object on write.
//
// The tests set ANTIGRAVITY_HOME to a tempdir so they do not touch the
// user's real settings.json. Each test rebuilds a fresh settings file
// with `t.Setenv` so cycleConfigValue has predictable starting state.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
)

// cycle196TestHome returns an isolated config home for one test. It
// writes a starting settings.json with `seed` (raw map) and returns
// the home directory; t.Cleanup resets the env.
func cycle196TestHome(t *testing.T, seed map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if seed != nil {
		b, err := json.MarshalIndent(seed, "", "  ")
		if err != nil {
			t.Fatalf("marshal seed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), b, 0600); err != nil {
			t.Fatalf("write seed: %v", err)
		}
	}
	return dir
}

// configRowIndexForTest resolves a /config row's index by its display
// name, so the tests do not depend on the (inferred, below-the-fold)
// order of the twenty settings rows. Cycle 197 reordered a few of the
// invisible rows, which broke the positional `cursor: 16` lookup.
func configRowIndexForTest(t *testing.T, name string) int {
	t.Helper()
	for i, r := range configRows() {
		if r.name == name {
			return i
		}
	}
	t.Fatalf("configRows has no row named %q", name)
	return -1
}

// readCycle196Settings unmarshals the test's settings.json into a
// generic map. Used by the persistence assertions.
func readCycle196Settings(t *testing.T, dir string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse settings: %v", err)
	}
	return m
}

// cycle196ValueAt walks a dot path and returns the leaf as string.
func cycle196ValueAt(t *testing.T, m map[string]any, path string) string {
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

// TestCycle196_ConfigCycleRight_SimpleString is the canonical
// cycle-through cycle196 case: cursor on Color Scheme (cycle list:
// light → solarized light → …), right arrow advances one step and
// persists the *stored* form (probe_ref_walk2.py row 3).
func TestCycle196_ConfigCycleRight_SimpleString(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "light",
	})
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: 3} // Color Scheme

	m.cycleConfigValue(+1)
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "colorScheme"); got != "solarized light" {
		t.Errorf("after right: colorScheme = %q, want %q", got, "solarized light")
	}
	if v := configSettingValue(3); v != "solarized light" {
		t.Errorf("configSettingValue(3) = %q, want %q", v, "solarized light")
	}
}

// TestCycle196_ConfigCycleLeft_ToDefault covers the reference's
// default-by-absence convention: from the second cycle value ("light")
// pressing ← lands on the default ("terminal") and *removes* the key
// rather than writing the string "terminal".
func TestCycle196_ConfigCycleLeft_ToDefault(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "light",
	})
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: 3}
	m.cycleConfigValue(-1)
	got := readCycle196Settings(t, dir)
	if _, present := got["colorScheme"]; present {
		t.Errorf("after left from light: colorScheme = %v, want key absent (default)", got["colorScheme"])
	}
	if v := configSettingValue(3); v != "terminal" {
		t.Errorf("configSettingValue(3) = %q, want %q", v, "terminal")
	}
}

// TestCycle196_ConfigCycle_BoolSetting exercises the boolean path on a
// real row from the 1.4.0 catalogue: "Show Tips" is on by default
// (absent) and persists `false` when toggled off.
func TestCycle196_ConfigCycle_BoolSetting(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"showTips": true})
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: configRowIndexForTest(t, "Show Tips")}

	m.cycleConfigValue(-1) // on → off
	got := readCycle196Settings(t, dir)
	if v := cycle196ValueAt(t, got, "showTips"); v != "off" {
		t.Errorf("after left: showTips = %q, want %q", v, "off")
	}
	if v := configSettingValue(configRowIndexForTest(t, "Show Tips")); v != "off" {
		t.Errorf("display = %q, want %q", v, "off")
	}

	m.cycleConfigValue(+1) // off → on (default: key removed)
	got = readCycle196Settings(t, dir)
	if _, present := got["showTips"]; present {
		t.Errorf("after right: showTips = %v, want key absent (default on)", got["showTips"])
	}
	if v := configSettingValue(configRowIndexForTest(t, "Show Tips")); v != "on" {
		t.Errorf("display = %q, want %q", v, "on")
	}
}

// TestCycle196_ConfigCycle_KeyHandler verifies that handlePanelKey
// routes ←/→ to cycleConfigValue for the config panel — the user-facing
// path the picker advertises in its keyboard legend.
func TestCycle196_ConfigCycle_KeyHandler(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "light",
	})
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: 3}

	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if !handled {
		t.Fatal("←/→: handlePanelKey reported unhandled")
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "colorScheme"); got != "solarized light" {
		t.Errorf("after ←/→ key: colorScheme = %q, want %q", got, "solarized light")
	}
}

// TestCycle196_ConfigCycle_AbsentKey covers the case where the
// settings.json file lacks the key (first-launch state). Cycling should
// still work, starting from values[0] (the default).
func TestCycle196_ConfigCycle_AbsentKey(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{
		"colorScheme": "dark",
	})
	// Don't seed agentMode
	if _, err := os.Stat(filepath.Join(dir, "settings.json")); err != nil {
		t.Fatal(err)
	}
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: 0} // Agent Mode
	if v := configSettingValue(0); v != "default" {
		t.Errorf("missing key: configSettingValue(0) = %q, want %q", v, "default")
	}
	m.cycleConfigValue(+1) // default → accept-edits
	got := readCycle196Settings(t, dir)
	if v, ok := got["agentMode"].(string); !ok || v != "accept-edits" {
		t.Errorf("after right from absent: agentMode = %v, want %q", got["agentMode"], "accept-edits")
	}
}

// TestCycle196_ConfigValue_LiveRead verifies that the rendered panel
// reflects live settings.json state — opening the panel with a custom
// colorScheme already in settings.json should display that value, not
// the default.
func TestCycle196_ConfigValue_LiveRead(t *testing.T) {
	cycle196TestHome(t, map[string]any{
		"colorScheme": "dark",
	})
	if v := configSettingValue(3); v != "dark" {
		t.Errorf("configSettingValue(3) with dark in disk = %q, want %q", v, "dark")
	}
	_ = auth.AntigravityConfigHome() // sanity: keep import live
}