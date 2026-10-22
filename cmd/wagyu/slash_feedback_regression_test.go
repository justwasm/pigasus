package main

// slash_cycle208_test.go — cycle 208: the regression sweep over the four
// panels cycles 196/197/201/202 declared done. Each test pins one of the
// divergences the live re-diff against agy 1.4.0 turned up. The reference
// bytes they assert are quoted in slash_regression.go's header and in the
// cycle-208 report (probe dir fin_*.raw.txt / our_*.raw.txt).
//
// Tests that touch settings.json point ANTIGRAVITY_HOME at a temp dir via
// the cycle-196 helper, so the user's real config is never written.

import (
	"flag"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// --- /config ---------------------------------------------------------------

// REF pads the value column at 29 *runes*; `Editor Mode › Insert First` is
// 26 runes but 28 bytes, so the byte count left it two cells short.
func TestCycle208_ConfigNamePadCountsRunes(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"Agent Mode", 19},
		{"Editor Mode › Insert First", 3},
		{"Use AI Credits", 15},
	}
	for _, tc := range cases {
		if got := configNamePad(tc.name, 29); got != tc.want {
			t.Errorf("configNamePad(%q) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// The rendered nested row must carry three spaces before its value, which
// is what REF prints (`  Editor Mode › Insert First   off`).
func TestCycle208_ConfigNestedRowAlignment(t *testing.T) {
	cycle196TestHome(t, map[string]any{"editorMode": "vim"})
	m := mkPanel(panelConfig, "/config", 0)
	m.height = 60 // tall enough that every row fits, so no windowing
	lines := m.panelLines()
	idx := configRowIndexForTest(t, "Editor Mode › Insert First")
	got := lines[5+idx]
	want := "  Editor Mode › Insert First" + strings.Repeat(" ", 3) + "off"
	if got != want {
		t.Errorf("nested row:\n got %q\nwant %q", got, want)
	}
}

// The counter follows the description block's single trailing blank.
// Live 1.4.3 at 120x40: rows, ”, desc, ”, [1-18 of 20 items], ”, Keyboard.
func TestCycle208_ConfigWindowedCounterBlanks(t *testing.T) {
	cycle196TestHome(t, map[string]any{"editorMode": "vim"})

	windowed := mkPanel(panelConfig, "/config", 0)
	windowed.height = 24
	wl := windowed.panelLines()
	if !strings.Contains(wl[len(wl)-3], "items]") {
		t.Fatalf("expected a counter at len-3, got %q", wl[len(wl)-3])
	}
	if wl[len(wl)-4] != "" || wl[len(wl)-5] == "" {
		t.Errorf("windowed list needs one blank after the description, got %q %q",
			wl[len(wl)-5], wl[len(wl)-4])
	}

	complete := mkPanel(panelConfig, "/config", 0)
	complete.height = 60
	cl := complete.panelLines()
	if strings.Contains(strings.Join(cl, "\n"), "items]") {
		t.Errorf("complete list should have no counter:\n%s", strings.Join(cl, "\n"))
	}
	if cl[len(cl)-3] != "" || cl[len(cl)-4] != "" {
		t.Errorf("complete list blanks: %q %q", cl[len(cl)-4], cl[len(cl)-3])
	}
}

// The cursor row has two measured forms: a name of 16+ runes is emitted as
// one blue span covering name+pad+value, a shorter name leaves the pad
// uncoloured. Measured over all 21 rows in harness/cfg_cursor.py.
func TestCycle208_ConfigCursorRowSpanForms(t *testing.T) {
	cycle196TestHome(t, map[string]any{"editorMode": "vim"})
	if !configCursorRowIsSingleSpan("Enable Telemetry") { // 16 runes
		t.Error("16-rune name must use the single-span form")
	}
	if !configCursorRowIsSingleSpan("Editor Mode › Insert First") { // 26 runes
		t.Error("26-rune name must use the single-span form")
	}
	if configCursorRowIsSingleSpan("Artifact Review") { // 15 runes
		t.Error("15-rune name must use the two-span form")
	}

	m := mkPanel(panelConfig, "/config", 0) // Agent Mode (10 runes)
	m.height = 60
	agent := m.panelLines()[5]
	wantTwo := cBlueOpen + "> Agent Mode" + cBlueClose + strings.Repeat(" ", 19) +
		cBlueOpen + m.configRowValueAt(0) + cBlueClose
	if agent != wantTwo {
		t.Errorf("Agent Mode cursor row:\n got %q\nwant %q", agent, wantTwo)
	}

	m.panel.cursor = 9 // Enable Telemetry (16 runes)
	tele := m.panelLines()[14]
	wantOne := cBlueOpen + "> Enable Telemetry" + strings.Repeat(" ", 13) +
		m.configRowValueAt(9) + cBlueClose
	if tele != wantOne {
		t.Errorf("Enable Telemetry cursor row:\n got %q\nwant %q", tele, wantOne)
	}
}

// --- /model ----------------------------------------------------------------

// The effort slider and its `←/→ Effort` legend entry exist only while the
// cursor is on the three-tier Gemini row.
func TestCycle208_ModelSliderOnlyOnTieredRow(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash-high"})
	m := mkPanel(panelModel, "/model", 0)

	onGemini := strings.Join(plainLines(m), "\n")
	if !strings.Contains(onGemini, "Effort") || !strings.Contains(onGemini, "←/→ Effort") {
		t.Fatalf("Gemini row should show the slider + legend:\n%s", onGemini)
	}
	if !strings.Contains(onGemini, "←/→ Effort  enter Select  esc Go Back") {
		t.Errorf("Gemini legend wording changed:\n%s", onGemini)
	}

	m.panel.cursor = 1 // Claude Sonnet — fixed effort
	off := plainLines(m)
	joined := strings.Join(off, "\n")
	if strings.Contains(joined, "Effort") || strings.Contains(joined, "←/→") {
		t.Errorf("non-tiered row must hide the slider and the ←/→ hint:\n%s", joined)
	}
	if got := off[len(off)-1]; got != "Keyboard: ↑/↓ Navigate  enter Select  esc Go Back" {
		t.Errorf("non-tiered legend = %q", got)
	}
}

// The cursor row is padded to column 30 only when the `(current)` marker
// follows it; an un-annotated cursor row has no trailing spaces.
func TestCycle208_ModelCursorRowPadOnlyWhenCurrent(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash-high"})
	m := mkPanel(panelModel, "/model", 1) // Claude row, not current
	m.height = 0
	lines := m.panelLines()
	got := lines[5+1]
	want := cBlueOpen + "> " + modelRows[1].display + cBlueClose
	if got != want {
		t.Errorf("cursor row:\n got %q\nwant %q", got, want)
	}
	// The annotated Gemini row (settings model == the current slug)
	// carries the column-30 pad. ppref.model.raw.txt has the pad only
	// on the annotated row.
	gem := lines[5]
	if !strings.Contains(gem, strings.Repeat(" ", 12)+panelGray238+" (current)") {
		t.Errorf("annotated Gemini row lost its padding: %q", gem)
	}
}

// Moving the cursor off the Gemini row and back must not lose the
// `(current)` marker or move the slider off the model's current effort
// (REF returns to High: fin_model_up_back).
func TestCycle208_ModelEffortCursorReseeds(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash-high"})
	m := mkPanel(panelModel, "/model", 0) // Gemini row — has the slider
	m.height = 0
	if m.panel.effortCursor != effortHigh {
		t.Fatalf("open cursor = %d, want high", m.panel.effortCursor)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m.panel.effortCursor != effortHigh {
		t.Errorf("after down: effortCursor = %d, want high", m.panel.effortCursor)
	}
	if !strings.Contains(m.panelLines()[5], " (current)") {
		t.Errorf("MiniMax row lost its (current) marker while the cursor is on Claude: %q",
			m.panelLines()[5])
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatal("up not handled")
	}
	lines := m.panelLines()
	if !strings.Contains(lines[12], panelEffHighOpen) {
		t.Errorf("slider did not return to high: %q", lines[12])
	}
	if !strings.Contains(lines[5], " (current)") {
		t.Errorf("MiniMax row lost its (current) marker: %q", lines[5])
	}
}

// --- /effort ---------------------------------------------------------------

// The lit run stops at the active ◉; the 14 cells after it are dim.
func TestCycle208_EffortBarColourBoundary(t *testing.T) {
	on, off := effortBarSegments(effortLow)
	if on != strings.Repeat(" ", 7)+"◉" {
		t.Errorf("low on-run = %q", on)
	}
	if off != strings.Repeat("─", 14)+"○"+strings.Repeat("─", 14)+"○"+strings.Repeat(" ", 7) {
		t.Errorf("low off-run = %q", off)
	}

	on, off = effortBarSegments(effortMedium)
	if !strings.HasSuffix(on, "◉") || strings.Contains(on, "─") {
		t.Errorf("medium on-run = %q", on)
	}
	if !strings.HasPrefix(off, strings.Repeat("─", 14)) {
		t.Errorf("medium off-run = %q", off)
	}

	on, off = effortBarSegments(effortHigh)
	if off != strings.Repeat(" ", 7) {
		t.Errorf("high off-run = %q", off)
	}
	if !strings.HasSuffix(on, "◉") {
		t.Errorf("high on-run = %q", on)
	}
}

// The status row's suffix follows the live effort, not a constant.
func TestCycle208_StatusEffortSuffixTracksEffort(t *testing.T) {
	m := &runMode{}
	if got := m.statusEffortSuffix(); got != " \u00b7 high" {
		t.Errorf("default suffix = %q", got)
	}
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "", "")
	m.flags = fs
	for level, want := range map[string]string{
		"low": " \u00b7 low", "medium": " \u00b7 medium", "high": " \u00b7 high",
	} {
		if err := fs.Set("effort", level); err != nil {
			t.Fatalf("set effort: %v", err)
		}
		if got := m.statusEffortSuffix(); got != want {
			t.Errorf("effort %s: suffix = %q, want %q", level, got, want)
		}
	}
}

// Enter on /effort: an unchanged level closes silently and writes nothing;
// a changed level echoes `Effort set to <level>`, moves the banner/status
// and persists the model label to settings.json.
func TestCycle208_EffortEnterApplies(t *testing.T) {
	home := cycle196TestHome(t, map[string]any{"model": "MiniMax-M3"})
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("effort", "", "")

	// The settings model is unavailable, so the session runs the fallback
	// (High) — applying High is a no-op.
	m := mkPanel(panelEffort, "/effort", effortHigh)
	m.flags = fs
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	if m.panel.kind != panelNone {
		t.Fatalf("panel not closed: kind=%v", m.panel.kind)
	}
	if len(m.response) != 0 {
		t.Errorf("no-op apply must stay silent, got %q", m.response)
	}
	if s := readCycle196Settings(t, home); s["model"] != "MiniMax-M3" {
		t.Errorf("no-op apply wrote settings: model=%v", s["model"])
	}

	// Moving to Medium is a real change.
	m2 := mkPanel(panelEffort, "/effort", effortMedium)
	m2.flags = fs
	if handled, _ := m2.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("enter not handled")
	}
	if m2.panel.kind != panelNone {
		t.Fatalf("panel not closed: kind=%v", m2.panel.kind)
	}
	if got := stripSGR(strings.Join(m2.response, "\n")); !strings.Contains(got, "Effort set to medium") {
		t.Errorf("response = %q", got)
	}
	if s := readCycle196Settings(t, home); s["model"] != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("persisted model = %v", s["model"])
	}
	if m2.model != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("banner model = %q", m2.model)
	}
	if m2.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("status model = %q", m2.statusModel)
	}
	if got := m2.statusEffortSuffix(); got != " \u00b7 medium" {
		t.Errorf("status suffix = %q", got)
	}
}

// --- /feedback -------------------------------------------------------------

// REF's stage 2 is the "Review Submission Data" form; every line here was
// read off fin_fb_stage2.raw.txt.
func TestCycle208_FeedbackStage2Layout(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 0)
	m.panel.stage = 1
	m.panel.cursor = 0
	assertLines(t, m.panelLines(), []string{
		panelTitleOpen + "Review Submission Data" + panelTitleClose + "  " +
			panelGray241 + "(You can always provide manual feedback via /feedback)" + panelGray241C,
		"",
		"Your feedback helps improve Pigasus CLI. The following sanitized data will be sent:",
		"",
		"Feedback: " + cBlueOpen + "Code doesn't work (e.g., syntax errors, bad logic)" + cBlueClose,
		"",
		"Additional Details (optional):",
		searchCursorBlock,
		"",
		"",
		"",
		"\x1b[7m\x1b[38;2;133;153;0m ctrl+s Submit Feedback \x1b[0m  " + panelGray241 + "esc Back" + panelGray241C,
	})
}

// Cycle 208 originally asserted Enter was inert in stage 2 — REF's
// stage-2 form is a textarea, so Enter inserts a newline and only
// ctrl+s submits (fin_fb_submitted.raw.txt).
//
// Cycle 221 re-binds Enter to submit (see handleFeedbackTextKey): the
// clone has no upload endpoint, so the user can only ever see the
// local-only echo and a single submit key matches the panel's UX. This
// test is kept as the "ctrl+s is a parallel submit key" pin: ctrl+s
// still closes the panel and emits the cycle-192 message.
func TestCycle208_FeedbackStage2CtrlSSubmits(t *testing.T) {
	m := mkPanel(panelFeedback, "/feedback", 0)
	m.panel.stage = 1
	m.panel.text = "the model repeated itself"
	m.handlePanelKey(tea.Key{Code: 's', Mod: tea.ModCtrl})
	if m.panel.kind != panelNone {
		t.Fatalf("ctrl+s did not submit: kind=%v", m.panel.kind)
	}
	if got := stripSGR(strings.Join(m.response, "\n")); !strings.Contains(got, "Feedback recorded") {
		t.Errorf("ctrl+s submit should record the local-only message: %q", got)
	}
}
