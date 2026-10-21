package main

// slash_cycle197_test.go — cycle 197: deep /config + /model.
//
// The /config catalogue and every value list were recovered by driving the
// reference and diffing settings.json; the probes live in /tmp/agycmp
// (probe_ref_walk2.py is the authoritative row→key map, probe_ref_vocab.py
// the full ←/→ vocabulary, probe_ref_height.py the row-count-vs-height
// table). The tests below pin the two behaviours that changed:
//
//  1. default-by-absence — a setting at its default is stored by *removing*
//     the key. Writing the literal default makes REF 1.4.0 reject the file
//     ("invalid settings: agentMode: unrecognized value \"default\""), so
//     this is a correctness fix, not a cosmetic one.
//  2. the /model panel's `(current)` mark and effort slider derive from the
//     *effective* (post-fallback) model, so an unavailable settings value
//     still lands on the fallback row REF marks current.

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------
// /config catalogue
// ----------------------------------------------------------------------

// TestCycle197_ConfigCatalogueSize pins the recovered catalogue: 20
// top-level rows, plus the "Editor Mode › Insert First" child row while
// Editor Mode is vim.
func TestCycle197_ConfigCatalogueSize(t *testing.T) {
	cycle196TestHome(t, map[string]any{})
	if got := len(configRows()); got != 20 {
		t.Errorf("configRows() with editorMode unset = %d rows, want 20", got)
	}
	cycle196TestHome(t, map[string]any{"editorMode": "vim"})
	rows := configRows()
	if len(rows) != 21 {
		t.Fatalf("configRows() with editorMode=vim = %d rows, want 21", len(rows))
	}
	// The child row sits immediately after Editor Mode.
	if rows[7].name != "Editor Mode" || rows[8].name != "Editor Mode \u203a Insert First" {
		t.Errorf("rows[7]=%q rows[8]=%q, want Editor Mode then its Insert First child",
			rows[7].name, rows[8].name)
	}
}

// TestCycle197_ConfigRowKeys pins the recovered name→settings.json key
// mapping (probe_ref_walk2.py). A wrong key here silently writes the wrong
// setting.
func TestCycle197_ConfigRowKeys(t *testing.T) {
	want := []struct{ name, key string }{
		{"Agent Mode", "agentMode"},
		{"Animation Speed", "runningLightSpeed"},
		{"Artifact Review", "artifactReviewPolicy"},
		{"Color Scheme", "colorScheme"},
		{"Conversation Picker Grouping", "pickerGrouping"},
		{"Copy on Select", "copyOnSelect"},
		{"Editor", "editor"},
		{"Editor Mode", "editorMode"},
		{"Editor Mode \u203a Insert First", "vimInsertFirst"},
		{"Enable Telemetry", "enableTelemetry"},
		{"Non-Workspace Access", "allowNonWorkspaceAccess"},
		{"Notifications", "notifications"},
		{"Queued Messages", "queuedMessages"},
		{"Rendering Mode", "altScreenMode"},
		{"Sandbox Mode", "enableTerminalSandbox"},
		{"Show Active Tasks", "hideActiveItems"},
		{"Show Feedback Survey", "showFeedbackSurvey"},
		{"Show Tips", "showTips"},
		{"Tool Permission", "toolPermission"},
		{"Use AI Credits", "useG1Credits"},
		{"Verbosity", "verbosity"},
	}
	if len(configSettingsRows) != len(want) {
		t.Fatalf("catalogue has %d rows, want %d", len(configSettingsRows), len(want))
	}
	for i, w := range want {
		got := configSettingsRows[i]
		if got.name != w.name || got.settingsKey != w.key {
			t.Errorf("row %d = (%q, %q), want (%q, %q)", i, got.name, got.settingsKey, w.name, w.key)
		}
	}
}

// TestCycle197_ConfigDefaultDeletesKey is the headline correctness fix:
// cycling Agent Mode back to its default removes the key instead of
// writing "default".
func TestCycle197_ConfigDefaultDeletesKey(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Agent Mode")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}

	// default → accept-edits → auto → plan → default
	for _, want := range []string{"accept-edits", "auto", "plan"} {
		m.cycleConfigValue(+1)
		if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "agentMode"); got != want {
			t.Fatalf("after right: agentMode = %q, want %q", got, want)
		}
	}
	m.cycleConfigValue(+1)
	got := readCycle196Settings(t, dir)
	if v, present := got["agentMode"]; present {
		t.Errorf("after wrapping to default: agentMode = %v, want key absent", v)
	}
	if d := configSettingValue(idx); d != "default" {
		t.Errorf("display after wrap = %q, want %q", d, "default")
	}
}

// TestCycle197_ConfigAnimationSpeedCycle pins the four-step cycle that the
// pre-197 clone got wrong (it had [slow medium fast] and was missing
// "off").
func TestCycle197_ConfigAnimationSpeedCycle(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Animation Speed")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}

	for _, want := range []string{"slow", "off", "fast"} {
		m.cycleConfigValue(+1)
		if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "runningLightSpeed"); got != want {
			t.Fatalf("after right: runningLightSpeed = %q, want %q", got, want)
		}
	}
	m.cycleConfigValue(+1) // fast → medium (default → key removed)
	if v, present := readCycle196Settings(t, dir)["runningLightSpeed"]; present {
		t.Errorf("after wrap: runningLightSpeed = %v, want key absent", v)
	}
}

// TestCycle197_ConfigArtifactReviewDisplay covers the display forms the
// pre-197 clone rendered with hyphens.
func TestCycle197_ConfigArtifactReviewDisplay(t *testing.T) {
	idx := configRowIndexForTest(t, "Artifact Review")
	cases := []struct {
		stored  any
		display string
	}{
		{nil, "asks for review"},
		{"always-proceed", "always proceeds"},
		{"agent-decides", "agent decides"},
	}
	for _, c := range cases {
		cycle196TestHome(t, map[string]any{"artifactReviewPolicy": c.stored})
		if got := configSettingValue(idx); got != c.display {
			t.Errorf("stored=%v → %q, want %q", c.stored, got, c.display)
		}
	}
}

// TestCycle197_ConfigToolPermissionFromExplicit stores a value that is the
// row's *display default* but is written explicitly: the reference (and
// the clone) advance it to the next entry rather than stalling.
func TestCycle197_ConfigToolPermissionFromExplicit(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"toolPermission": "always-proceed"})
	idx := configRowIndexForTest(t, "Tool Permission")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := configSettingValue(idx); got != "always-proceed" {
		t.Fatalf("display = %q, want always-proceed", got)
	}
	m.cycleConfigValue(+1)
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "toolPermission"); got != "strict" {
		t.Errorf("after right: toolPermission = %q, want strict", got)
	}
}

// TestCycle197_ConfigRenderingModeWrap covers a stored value ("always")
// wrapping to the default ("native terminal (inline)"), which deletes the
// key.
func TestCycle197_ConfigRenderingModeWrap(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"altScreenMode": "always"})
	idx := configRowIndexForTest(t, "Rendering Mode")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := configSettingValue(idx); got != "no flickering (altscreen)" {
		t.Fatalf("display = %q", got)
	}
	m.cycleConfigValue(+1)
	if v, present := readCycle196Settings(t, dir)["altScreenMode"]; present {
		t.Errorf("after right: altScreenMode = %v, want key absent", v)
	}
}

// TestCycle197_ConfigMemoryOnlyTouchesNoDisk pins Enable Telemetry: the
// reference toggles it but writes nothing to settings.json (probe
// diff was empty), so the clone keeps the value in panel state.
func TestCycle197_ConfigMemoryOnlyTouchesNoDisk(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{})
	idx := configRowIndexForTest(t, "Enable Telemetry")
	m := &runMode{}
	m.panel = slashPanel{kind: panelConfig, cmd: "/config", cursor: idx}
	if got := m.configRowValueAt(idx); got != "off" {
		t.Fatalf("initial display = %q, want off", got)
	}
	m.cycleConfigValue(+1)
	if got := m.configRowValueAt(idx); got != "on" {
		t.Errorf("after right: display = %q, want on", got)
	}
	if got := readCycle196Settings(t, dir); len(got) != 0 {
		t.Errorf("settings.json = %v, want untouched", got)
	}
}

// TestCycle197_ConfigVisibleRows pins the height→row-count table measured
// with probe_ref_height.py, and the unknown-height fallback.
//
// Cycle 208: the table was measured against a 20-row catalogue, so the
// test now freezes the settings home (editorMode absent = no nested
// "Insert First" child). Without that it depended on whatever the shared
// ~/.gemini/antigravity-cli/settings.json happened to hold.
func TestCycle197_ConfigVisibleRows(t *testing.T) {
	cycle196TestHome(t, map[string]any{})
	cases := []struct{ h, want int }{
		{0, 10}, {-5, 10},
		{20, 3}, {24, 6}, {28, 9}, {32, 12}, {40, 18},
		{44, 20}, {50, 20}, {60, 20}, {1, 1},
	}
	for _, c := range cases {
		if got := configVisibleRowsFor(c.h); got != c.want {
			t.Errorf("configVisibleRowsFor(%d) = %d, want %d", c.h, got, c.want)
		}
	}
}

// TestCycle197_ConfigPanelNoCounterWhenAllFit pins the counter slot: the
// reference blanks it (rather than collapsing the line) when every row
// fits.
func TestCycle197_ConfigPanelNoCounterWhenAllFit(t *testing.T) {
	cycle196TestHome(t, map[string]any{})
	m := mkPanel(panelConfig, "/config", 0)
	m.height = 50
	lines := plainLines(m)
	if len(lines) != 31 {
		t.Errorf("panel = %d lines at height 50, want 31 (live REF)", len(lines))
	}
	for _, l := range lines {
		if contains(l, "items]") {
			t.Errorf("unexpected counter at height 50: %q", l)
		}
	}
	// The windowed fallback (height unknown) still shows the counter.
	m2 := mkPanel(panelConfig, "/config", 0)
	joined := ""
	for _, l := range plainLines(m2) {
		joined += l + "\n"
	}
	if !contains(joined, "[1-10 of 20 items]") {
		t.Errorf("unknown height should keep the windowed counter:\n%s", joined)
	}
}

// ----------------------------------------------------------------------
// /model
// ----------------------------------------------------------------------

// TestCycle197_ModelCurrentFromEffectiveSettings pins that an unavailable
// settings value still marks — and parks the slider on — the fallback
// model, matching REF's capture.
func TestCycle197_ModelCurrentFromEffectiveSettings(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash"})
	m := mkPanel(panelModel, "/model", 0)
	lines := plainLines(m)
	if !contains(lines[5], "(current)") {
		t.Errorf("fallback row should be marked (current): %q", lines[5])
	}
	if m.panel.effortCursor != 2 {
		t.Errorf("effortCursor = %d, want 2 (high, the fallback model's tier)", m.panel.effortCursor)
	}
}

// TestCycle197_ModelEffortFromLabel pins that a persisted display label
// parks the slider on its own tier. Slugs are rejected by the 1.4.3
// settings loader (it falls back to High with a warning); full labels are
// the accepted form.
func TestCycle197_ModelEffortFromSlug(t *testing.T) {
	for label, want := range map[string]int{
		"Gemini 3.8 Flash (Low)":    0,
		"Gemini 3.8 Flash (Medium)": 1,
		"Gemini 3.8 Flash (High)":   2,
	} {
		cycle196TestHome(t, map[string]any{"model": label})
		if got := initialEffortCursorFromSettings(); got != want {
			t.Errorf("%s → effortCursor %d, want %d", label, got, want)
		}
	}
}

// TestCycle197_ModelEffortCyclesGeminiOnly verifies ←/→ wraps the slider
// on the Gemini row and is inert elsewhere. Cycle
// `model-left-right-effort` switched the boundary from "clamp" to
// "wrap": pressing → at high wraps back to low, and ← at low wraps to
// high. The non-Gemini rows still leave effortCursor alone — pressing
// an arrow while the cursor is on Claude Sonnet must not change the
// persisted model.
func TestCycle197_ModelEffortCyclesGeminiOnly(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	m := mkPanel(panelModel, "/model", 0)
	m.cycleModelEffort(+1)
	if m.panel.effortCursor != 2 {
		t.Errorf("gemini right: effortCursor = %d, want 2", m.panel.effortCursor)
	}
	m.cycleModelEffort(+1) // wrap high → low
	if m.panel.effortCursor != 0 {
		t.Errorf("gemini right wrap: effortCursor = %d, want 0 (wraps low)", m.panel.effortCursor)
	}
	m.cycleModelEffort(-1) // wrap low → high
	if m.panel.effortCursor != 2 {
		t.Errorf("gemini left wrap: effortCursor = %d, want 2 (wraps high)", m.panel.effortCursor)
	}
	// Non-Gemini rows are inert: the cursor moves to Claude Sonnet
	// after a separate ↓/↑, but cycleModelEffort early-returns because
	// the row's slug is not gemini-3.8-flash. Persisted model is
	// whatever it was at the time of the inert call.
	m.panel.effortCursor = 2
	m.panel.cursor = 1 // Claude Sonnet — fixed effort
	m.cycleModelEffort(-1)
	if m.panel.effortCursor != 2 {
		t.Errorf("non-gemini row should be inert: effortCursor = %d, want 2", m.panel.effortCursor)
	}
}

// TestCycle197_ModelSelectPersistsSlugAndUpdatesLabel pins Enter's effect:
// the catalogue slug goes to settings.json while m.model / m.statusModel
// take the display label so the status row reads "Gemini 3.8 Flash".
func TestCycle197_ModelSelectPersistsSlugAndUpdatesLabel(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := mkPanel(panelModel, "/model", 0)
	m.cycleModelEffort(-1) // high → medium
	label := m.selectModelEffort()
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != "gemini-3.8-flash-medium" {
		t.Errorf("persisted model = %q, want gemini-3.8-flash-medium", got)
	}
	if label != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("label = %q", label)
	}
	if m.model != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("m.model = %q, want the label", m.model)
	}
	if m.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("m.statusModel = %q, want tier-stripped label", m.statusModel)
	}
}

// TestCycle197_ModelArgonNotSelectable pins that the dim, slugless row is
// inert: Enter leaves settings.json alone.
func TestCycle197_ModelArgonNotSelectable(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := mkPanel(panelModel, "/model", 5) // Gemini 4 Argon
	if got := m.selectModelEffort(); got != "" {
		t.Errorf("selectModelEffort on Argon = %q, want \"\"", got)
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != "Gemini 3.8 Flash (High)" {
		t.Errorf("settings model changed to %q; Argon must be inert", got)
	}
	// handlePanelKey(Enter) must still swallow the key (close the panel).
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Error("Enter on Argon: handlePanelKey reported unhandled")
	}
}

// TestCycle197_ModelEnterKeyHandlerClosesPanel drives the user-facing path.
func TestCycle197_ModelEnterKeyHandlerClosesPanel(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := mkPanel(panelModel, "/model", 0)
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyLeft}) // high → medium
	if !handled {
		t.Fatal("← unhandled on /model")
	}
	handled, _ = m.handlePanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("Enter unhandled on /model")
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != "gemini-3.8-flash-medium" {
		t.Errorf("persisted model = %q, want gemini-3.8-flash-medium", got)
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel kind = %v, want closed", m.panel.kind)
	}
}

// TestCycle197_ModelLayoutPadsCurrentColumn pins the byte geometry the
// capture comparison validates: 30-column pad then " (current)".
func TestCycle197_ModelLayoutPadsCurrentColumn(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	m := mkPanel(panelModel, "/model", 0)
	line := plainLines(m)[5]
	// "> " + 16-char label + pad to col 30 + " (current)"
	if line != "> Gemini 3.8 Flash             (current)" {
		t.Errorf("row = %q", line)
	}
}

// contains is a tiny readability helper for the assertions above.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
