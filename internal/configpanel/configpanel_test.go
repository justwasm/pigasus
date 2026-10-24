package configpanel

import (
	"strings"
	"testing"
)

// TestCatalogueSize pins the recovered catalogue: 20 top-level rows plus
// the "Editor Mode › Insert First" child row, which the gate adds when
// Editor Mode is vim.
func TestCatalogueSize(t *testing.T) {
	if got := len(SettingsRows); got != 21 {
		t.Fatalf("catalogue has %d rows, want 21", got)
	}
	gated := 0
	for _, r := range SettingsRows {
		if r.ShowWhenKey != "" {
			gated++
		}
	}
	if gated != 1 {
		t.Errorf("gated rows = %d, want 1", gated)
	}
}

// TestCatalogueKeys pins the recovered name→settings.json key mapping
// (probe_ref_walk2.py). A wrong key here silently writes the wrong
// setting.
func TestCatalogueKeys(t *testing.T) {
	want := []struct{ name, key string }{
		{"Agent Mode", "agentMode"},
		{"Animation Speed", "runningLightSpeed"},
		{"Artifact Review", "artifactReviewPolicy"},
		{"Color Scheme", "colorScheme"},
		{"Conversation Picker Grouping", "pickerGrouping"},
		{"Copy on Select", "copyOnSelect"},
		{"Editor", "editor"},
		{"Editor Mode", "editorMode"},
		{"Editor Mode › Insert First", "vimInsertFirst"},
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
	for i, w := range want {
		got := SettingsRows[i]
		if got.Name != w.name || got.SettingsKey != w.key {
			t.Errorf("row %d = (%q, %q), want (%q, %q)", i, got.Name, got.SettingsKey, w.name, w.key)
		}
	}
}

// TestDefaultIsAbsent pins the absence convention: the default entry of
// every row stores nil, meaning "delete the key".
func TestDefaultIsAbsent(t *testing.T) {
	for _, r := range SettingsRows {
		if len(r.Values) == 0 {
			t.Errorf("row %q has no values", r.Name)
			continue
		}
		if r.Values[0].Stored != nil {
			t.Errorf("row %q default stores %v, want nil (absent)", r.Name, r.Values[0].Stored)
		}
	}
}

// TestRowsFiltered pins the case-insensitive substring search over the
// name, the description and the option display values.
func TestRowsFiltered(t *testing.T) {
	if got := len(RowsFiltered(SettingsRows, "")); got != len(SettingsRows) {
		t.Errorf("empty query = %d rows, want %d", got, len(SettingsRows))
	}
	got := RowsFiltered(SettingsRows, "color")
	if len(got) != 1 || got[0].Name != "Color Scheme" {
		t.Errorf("query 'color' = %v, want the Color Scheme row", got)
	}
	// The description of Copy on Select mentions "system clipboard".
	got = RowsFiltered(SettingsRows, "system clipboard")
	if len(got) != 1 || got[0].Name != "Copy on Select" {
		t.Errorf("query 'system clipboard' = %v, want the Copy on Select row", got)
	}
	// An option's display value matches too ("accept-edits").
	got = RowsFiltered(SettingsRows, "accept-edits")
	if len(got) != 1 || got[0].Name != "Agent Mode" {
		t.Errorf("query 'accept-edits' = %v, want the Agent Mode row", got)
	}
	if got := RowsFiltered(SettingsRows, "nonexistent_random_setting_xyz"); len(got) != 0 {
		t.Errorf("bogus query = %v, want none", got)
	}
}

// TestRawString pins the gating comparison form.
func TestRawString(t *testing.T) {
	if got := RawString("vim"); got != "vim" {
		t.Errorf("RawString(vim) = %q", got)
	}
	if got := RawString(true); got != "on" {
		t.Errorf("RawString(true) = %q", got)
	}
	if got := RawString(false); got != "off" {
		t.Errorf("RawString(false) = %q", got)
	}
	if got := RawString(nil); got != "" {
		t.Errorf("RawString(nil) = %q", got)
	}
}

// TestJSONLeafEqual pins the nil-only-equals-nil rule.
func TestJSONLeafEqual(t *testing.T) {
	if !JSONLeafEqual(nil, nil) {
		t.Error("nil != nil")
	}
	if JSONLeafEqual(nil, "x") || JSONLeafEqual("x", nil) {
		t.Error("nil matched a non-nil leaf")
	}
	if !JSONLeafEqual("a", "a") || JSONLeafEqual("a", "b") {
		t.Error("string comparison wrong")
	}
}

// TestDisplayFor pins the display mapping and its fallbacks.
func TestDisplayFor(t *testing.T) {
	row := SettingsRows[0] // Agent Mode
	if got := DisplayFor(row, nil, ""); got != "default" {
		t.Errorf("absent = %q, want default", got)
	}
	if got := DisplayFor(row, "plan", ""); got != "plan" {
		t.Errorf("plan = %q", got)
	}
	// An unknown value falls back to the first entry.
	if got := DisplayFor(row, "nonsense", ""); got != "default" {
		t.Errorf("unknown = %q, want default", got)
	}
	// Tool Permission: the raw value names a *stored* string even though
	// the list's default entry stores nil.
	tp := SettingsRows[18]
	if tp.Name != "Tool Permission" {
		t.Fatalf("row 18 = %q, want Tool Permission", tp.Name)
	}
	if got := DisplayFor(tp, "strict", ""); got != "strict" {
		t.Errorf("strict = %q", got)
	}
}

// TestDisplayForTelemetryLock pins the Business-account lock.
func TestDisplayForTelemetryLock(t *testing.T) {
	var row Row
	for _, r := range SettingsRows {
		if IsTelemetryRow(r) {
			row = r
		}
	}
	if row.Name == "" {
		t.Fatal("no telemetry row")
	}
	if got := DisplayFor(row, true, "off (disabled)"); got != "off (disabled)" {
		t.Errorf("locked telemetry = %q", got)
	}
	// The lock must not touch the other rows.
	agent := SettingsRows[0]
	if got := DisplayFor(agent, nil, "off (disabled)"); got != "default" {
		t.Errorf("unrelated row = %q, want default", got)
	}
}

// TestVisibleRowsFor pins the measured height formula.
func TestVisibleRowsFor(t *testing.T) {
	for _, tc := range []struct{ height, want int }{
		{20, 3},
		{24, 6},
		{28, 9},
		{32, 12},
		{40, 18},
		{44, 21},
		{0, 10},
		{-1, 10},
	} {
		if got := VisibleRowsFor(tc.height, 21); got != tc.want {
			t.Errorf("VisibleRowsFor(%d) = %d, want %d", tc.height, got, tc.want)
		}
	}
	// The count is clamped to the catalogue size.
	if got := VisibleRowsFor(100, 5); got != 5 {
		t.Errorf("clamped = %d, want 5", got)
	}
	if got := VisibleRowsFor(10, 21); got != 1 {
		t.Errorf("floor = %d, want 1", got)
	}
}

// TestNamePad pins the rune-counted column padding.
func TestNamePad(t *testing.T) {
	if got := NamePad("Agent Mode", 29); got != 19 {
		t.Errorf("NamePad(Agent Mode) = %d, want 19", got)
	}
	// The nested row is 26 runes but 28 bytes — a byte count leaves it
	// two cells short.
	if got := NamePad("Editor Mode › Insert First", 29); got != 3 {
		t.Errorf("NamePad(nested) = %d, want 3", got)
	}
	// The pad never drops below 1.
	if got := NamePad("a very long setting name indeed", 29); got != 1 {
		t.Errorf("NamePad(long) = %d, want 1", got)
	}
}

// TestCursorRowIsSingleSpan pins the measured 16-rune boundary.
func TestCursorRowIsSingleSpan(t *testing.T) {
	if !CursorRowIsSingleSpan("Enable Telemetry") { // 16 runes
		t.Error("16 runes should be a single span")
	}
	if !CursorRowIsSingleSpan("Editor Mode › Insert First") { // 26 runes
		t.Error("26 runes should be a single span")
	}
	if CursorRowIsSingleSpan("Artifact Review") { // 15 runes
		t.Error("15 runes should not be a single span")
	}
}

// TestWrapText pins the description word-wrap.
func TestWrapText(t *testing.T) {
	// width <= 2 leaves the text unwrapped.
	if got := WrapText("a b c", 0); len(got) != 1 {
		t.Errorf("width 0 = %v, want one line", got)
	}
	got := WrapText("one two three four", 9)
	if len(got) < 2 {
		t.Fatalf("wrap = %v, want multiple lines", got)
	}
	for _, l := range got {
		if len([]rune(l)) > 7 {
			t.Errorf("line %q exceeds the 7-column budget", l)
		}
	}
	joined := strings.Join(got, " ")
	if joined != "one two three four" {
		t.Errorf("wrap lost words: %q", joined)
	}
}
