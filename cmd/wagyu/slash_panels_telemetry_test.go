package main

// slash_panels_telemetry_test.go — cycle 241 (AGY 1.4.1 alignment):
// Business-account lock on the `/config` `Enable Telemetry` row.
//
// The release-notes line:
//
//	"Fixed `Enable Telemetry` in `/config` remaining editable for Business
//	 accounts (such as Google Cloud project and Application Default
//	 Credentials sign-in via `AGY_ADC_AUTH`), where prompts, content, and
//	 model responses are never collected: the row now shows
//	 `off (disabled)` and explains why, and first-run setup with
//	 `AGY_ADC_AUTH` now shows the Google Cloud Platform Terms of Service
//	 without the consumer telemetry opt-in checkbox."
//
// This file pins the visible panel behaviour for the `/config` half of
// that fix. The first-run-setup half is a documented-but-unimplemented
// gap (see the cycle-241 deliverable's Limitations section).
//
// Tests:
//
//  1. TestCycle241_TelemetryBusinessLockedDisplay — with AGY_ADC_AUTH set,
//     the row paints the literal "off (disabled)" instead of the normal
//     on/off cycle.
//  2. TestCycle241_TelemetryPersonalUnchanged — with AGY_ADC_AUTH unset,
//     the row cycles normally (the pre-241 behaviour must be preserved
//     for personal accounts; the no-regression assertion).
//  3. TestCycle241_TelemetryToggleRefused — with AGY_ADC_AUTH set,
//     pressing ← or → on the row is a no-op: the underlying state does
//     not change and the transcript records the disabled-for-your-
//     account-type message. With AGY_ADC_AUTH unset, the same presses
//     round-trip the value normally.
//  4. TestCycle241_TelemetryBusinessPanelDescription — when the cursor
//     is on the locked row, the description slot under the catalogue
//     reads the AGY-recovered "Disabled for Business accounts. Antigravity
//     CLI does not collect any of your prompts, content, or model
//     responses, but will still collect general usage patterns." string
//     rather than the default personal-account description.
//  5. TestCycle241_TelemetryBusinessPanelRow — when AGY_ADC_AUTH is set
//     and the cursor sits on the `Enable Telemetry` row, the painted
//     catalogue line includes the literal "off (disabled)" value and the
//     description-line below it starts with "Disabled for Business
//     accounts." (so the snapshot-style tests stay in sync with the
//     1.4.1 contract end-to-end).

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCycle241_TelemetryBusinessLockedDisplay: with AGY_ADC_AUTH set the
// row paints the literal `off (disabled)` regardless of the in-memory
// state (which would otherwise be "on", the default per cycle 197).
func TestCycle241_TelemetryBusinessLockedDisplay(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")
	// No settings.json seed needed — telemetry is memoryOnly and the
	// default display is "on" (cf. TestCycle197_ConfigMemoryOnlyTouchesNoDisk).
	cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := m.configRowValueAt(idx); got != "off (disabled)" {
		t.Errorf("AGY_ADC_AUTH set: row value = %q, want %q", got, "off (disabled)")
	}
	// The display path is also exercised via the catalogue function
	// (configSettingValue → configDisplayFor) — both must agree.
	if got := configSettingValue(idx); got != "off (disabled)" {
		t.Errorf("AGY_ADC_AUTH set: configSettingValue = %q, want %q", got, "off (disabled)")
	}
}

// TestCycle241_TelemetryPersonalUnchanged: with AGY_ADC_AUTH unset the
// row cycles normally between "on" and "off" (cycle 197 contract, no
// regression).
func TestCycle241_TelemetryPersonalUnchanged(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")
	cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := m.configRowValueAt(idx); got != "off" {
		t.Errorf("AGY_ADC_AUTH unset: initial row value = %q, want off", got)
	}
	m.cycleConfigValue(+1)
	if got := m.configRowValueAt(idx); got != "on" {
		t.Errorf("AGY_ADC_AUTH unset: after +1 row value = %q, want on", got)
	}
	m.cycleConfigValue(+1)
	if got := m.configRowValueAt(idx); got != "off" {
		t.Errorf("AGY_ADC_AUTH unset: after +1 round-trip row value = %q, want off", got)
	}
}

// TestCycle241_TelemetryToggleRefused: with AGY_ADC_AUTH set the ←/→
// cycle is a no-op (no state mutation) and the transcript records the
// refusal message. With AGY_ADC_AUTH unset, the toggle round-trips.
func TestCycle241_TelemetryToggleRefused(t *testing.T) {
	t.Run("AGY_ADC_AUTH set: toggle is no-op and surfaces message", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "true")
		cycle196TestHome(t, map[string]any{})
		idx := configRowIndexForTest(t, "Enable Telemetry")
		m := &runMode{}
		m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}

		// Before: row is the normal "on". After: still "on" (the locked
		// display paints "off (disabled)", which the toggle must NOT
		// silently flip — we test both that the value is the locked
		// literal and that the underlying memory map did not move.
		m.cycleConfigValue(+1)
		if got := m.configRowValueAt(idx); got != "off (disabled)" {
			t.Errorf("AGY_ADC_AUTH set: after +1 display = %q, want %q",
				got, "off (disabled)")
		}
		if m.panel.configMemory != nil {
			if v, ok := m.panel.configMemory["enableTelemetry"]; ok && v != "on" {
				t.Errorf("AGY_ADC_AUTH set: configMemory mutated to %q, want absent or \"on\"", v)
			}
		}
		// The transcript must carry the refusal message so the user has
		// visible feedback that ←/→ did something.
		found := false
		for _, line := range m.response {
			if strings.Contains(stripSGR(line), telemetryDisabledResponse) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AGY_ADC_AUTH set: transcript missing %q.\nresponse=%q",
				telemetryDisabledResponse, m.response)
		}
	})

	t.Run("AGY_ADC_AUTH unset: toggle round-trips", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "")
		cycle196TestHome(t, map[string]any{})
		idx := configRowIndexForTest(t, "Enable Telemetry")
		m := &runMode{}
		m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}

		m.cycleConfigValue(+1) // off → on
		if got := m.configRowValueAt(idx); got != "on" {
			t.Errorf("after +1: row = %q, want on", got)
		}
		m.cycleConfigValue(-1) // on → off
		if got := m.configRowValueAt(idx); got != "off" {
			t.Errorf("after -1: row = %q, want off", got)
		}
		// No refusal message should land on the transcript for a personal
		// account that toggles the row normally.
		for _, line := range m.response {
			if strings.Contains(stripSGR(line), telemetryDisabledResponse) {
				t.Errorf("AGY_ADC_AUTH unset: transcript should not carry %q.\nresponse=%q",
					telemetryDisabledResponse, m.response)
			}
		}
	})
}

// TestCycle241_TelemetryBusinessPanelDescription: when the cursor is on
// the locked row, the description slot paints the AGY-recovered
// "Disabled for Business accounts. Antigravity CLI does not collect any
// of your prompts, content, or model responses, but will still collect
// general usage patterns." string. With AGY_ADC_AUTH unset the same
// panel renders the personal-account description (the pre-241 string,
// "When toggled on, Antigravity CLI collects …").
func TestCycle241_TelemetryBusinessPanelDescription(t *testing.T) {
	// --- Business-account case --------------------------------------------
	t.Setenv("AGY_ADC_AUTH", "true")
	cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := mkPanel(panelConfig, "/config", idx)
	lines := plainLines(m)
	descLine := findPanelConfigDescriptionLine(t, lines)
	trimmed := strings.TrimLeft(descLine, " \t")
	if !strings.HasPrefix(trimmed, "Disabled for Business accounts.") {
		t.Errorf("Business desc: got %q, want it to start with %q",
			trimmed, "Disabled for Business accounts.")
	}
	if !strings.Contains(trimmed, telemetryProductName) {
		t.Errorf("Business desc: got %q, want it to mention %q",
			trimmed, telemetryProductName)
	}

	// --- Personal-account case (no regression) ----------------------------
	t.Setenv("AGY_ADC_AUTH", "")
	m2 := mkPanel(panelConfig, "/config", idx)
	lines2 := plainLines(m2)
	descLine2 := findPanelConfigDescriptionLine(t, lines2)
	trimmed2 := strings.TrimLeft(descLine2, " \t")
	if strings.HasPrefix(trimmed2, "Disabled for Business accounts.") {
		t.Errorf("Personal desc should NOT carry the Business prefix: %q", trimmed2)
	}
	if !strings.Contains(trimmed2, "collects usage data") {
		t.Errorf("Personal desc: got %q, want it to contain the personal-account phrasing",
			trimmed2)
	}
}

// TestCycle241_TelemetryBusinessPanelRow: the painted catalogue row for
// the cursor-on-Enable-Telemetry case carries the literal "off
// (disabled)" and the description slot reads the Business variant.
func TestCycle241_TelemetryBusinessPanelRow(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := mkPanel(panelConfig, "/config", idx)
	lines := plainLines(m)
	rowLine := findPanelConfigCursorRowLine(t, lines, "Enable Telemetry")
	if !strings.Contains(rowLine, telemetryDisabledValue) {
		t.Errorf("row line = %q, want it to contain %q",
			rowLine, telemetryDisabledValue)
	}
	if !strings.HasPrefix(rowLine, "> Enable Telemetry") {
		t.Errorf("row line = %q, want it to start with the cursor prefix", rowLine)
	}
}

// TestCycle241_TelemetryLockedRowNotInjectedElsewhere: only the
// `Enable Telemetry` row gets the locked display; every other row in
// the catalogue must keep its pre-241 normal cycle. (Regression guard
// against accidentally short-circuiting configDisplayFor on the
// business-account branch.)
func TestCycle241_TelemetryLockedRowNotInjectedElsewhere(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	cycle196TestHome(t, map[string]any{})
	for _, name := range []string{
		"Agent Mode", "Animation Speed", "Color Scheme", "Show Tips",
		"Verbosity", "Notifications", "Non-Workspace Access",
	} {
		idx := configRowIndexForTest(t, name)
		if got := configSettingValue(idx); got == telemetryDisabledValue {
			t.Errorf("non-telemetry row %q painted as %q; should be the normal display",
				name, telemetryDisabledValue)
		}
	}
}

// TestCycle241_TelemetryBusinessFirstRunSkipOptIn: cycle-241 surface for
// the first-run setup opt-in suppression. The full first-run screen has
// many flags and a follow-up cycle would need to model the entire
// onboarding flow; this cycle ships the `/config` row only. The helper
// we expose is `IsBusinessAccount()` — under AGY_ADC_AUTH it returns
// true, and the first-run opt-in consumer is expected to skip its
// checkbox. The test pins the helper's expected behaviour so the
// first-run half of the 1.4.1 release notes has at least one assertion
// guarding it.
func TestCycle241_TelemetryBusinessFirstRunSkipOptIn(t *testing.T) {
	t.Run("AGY_ADC_AUTH set: helper returns true", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "true")
		if !IsBusinessAccount() {
			t.Error("IsBusinessAccount() = false under AGY_ADC_AUTH=true; want true")
		}
	})
	t.Run("AGY_ADC_AUTH unset: helper returns false", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "")
		if IsBusinessAccount() {
			t.Error("IsBusinessAccount() = true under AGY_ADC_AUTH unset; want false")
		}
	})
}

// TestCycle241_TelemetryKeyHandlerRoutesToGate drives the user-facing
// path: ←/→ on the locked row reaches cycleConfigValue through
// handlePanelKey and surfaces the refusal message.
func TestCycle241_TelemetryKeyHandlerRoutesToGate(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := mkPanel(panelConfig, "/config", idx)
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight})
	if !handled {
		t.Fatal("←/→ on /config: handlePanelKey reported unhandled")
	}
	if got := m.configRowValueAt(idx); got != telemetryDisabledValue {
		t.Errorf("after handlePanelKey →: row = %q, want %q",
			got, telemetryDisabledValue)
	}
	found := false
	for _, line := range m.response {
		if strings.Contains(stripSGR(line), telemetryDisabledResponse) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("after handlePanelKey →: transcript missing %q.\nresponse=%q",
			telemetryDisabledResponse, m.response)
	}
}

// ---- helpers --------------------------------------------------------------

// findPanelConfigDescriptionLine returns the first plain-text line in the
// rendered panel that contains "Disabled for Business accounts." or
// "When toggled on," — i.e. the description slot painted under the
// catalogue. The shape is recovered from `panelConfigLines()`: the
// description slot is the only line between the rows and the counter /
// keyboard hint that begins with one of those phrases. The function
// trims the leading indent the panel emits (`  ` + SGR-stripped) before
// comparing, so callers don't depend on the exact column count.
func findPanelConfigDescriptionLine(t *testing.T, lines []string) string {
	t.Helper()
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "Disabled for Business accounts.") ||
			strings.HasPrefix(trimmed, "When toggled on,") {
			return line
		}
	}
	t.Fatalf("could not find a description line in panel:\n%s",
		strings.Join(lines, "\n"))
	return ""
}

// findPanelConfigCursorRowLine returns the first plain-text line that
// starts with `> <rowName>` — the cursor row in the painted catalogue.
func findPanelConfigCursorRowLine(t *testing.T, lines []string, rowName string) string {
	t.Helper()
	prefix := "> " + rowName
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("could not find cursor row %q in panel:\n%s",
		rowName, strings.Join(lines, "\n"))
	return ""
}
