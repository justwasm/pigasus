// panel_test.go — the /config panel's interactive half.
//
// The panel is tested here, not in the host: it owns the rows, the search
// box, the inline option submenu, the keys and the writes, and a test
// that drives those things belongs with the code that answers them.
//
// The pure decisions — the catalogue, the search filter, the display
// mapping, the window sizing, the column padding — are pinned in
// configpanel_test.go next to the code that makes them.

package configpanel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
	"github.com/justwasm/pigasus/palette"
)

// testHome isolates the settings file for one test and returns the home
// directory.
//
// The panel reads and writes the user's own settings.json, so a test
// must not touch it: the home is redirected at a scratch directory and
// the starting document is seeded from `seed`.
func testHome(t *testing.T, seed map[string]any) string {
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

// readSettings parses the test's settings.json into a generic map.
func readSettings(t *testing.T, dir string) map[string]any {
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

// valueAt walks a dot path and returns the leaf as a string. Booleans
// render as "on"/"off".
func valueAt(t *testing.T, m map[string]any, path string) string {
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

// rowIndex resolves a settings row's index by its display name, so the
// tests do not depend on the order of the catalogue.
func rowIndex(t *testing.T, p *Panel, name string) int {
	t.Helper()
	for i, r := range p.Catalogue() {
		if r.Name == name {
			return i
		}
	}
	t.Fatalf("the catalogue has no row named %q", name)
	return -1
}

// stripANSI removes the escape sequences from a rendered line.
func stripANSI(s string) string {
	return ansi.Strip(s)
}

// plainLines renders the panel and strips the colour from every line.
func plainLines(p *Panel, height, width int) []string {
	var out []string
	for _, l := range p.Lines(height, width) {
		out = append(out, stripANSI(l))
	}
	return out
}

// seasonalPanel builds a panel whose seasonal row follows the pinned
// calendar: the host owns the clock, so the package asks for the event.
func seasonalPanel(month time.Month) *Panel {
	now := time.Date(2026, month, 15, 12, 0, 0, 0, time.UTC)
	return New(Seams{
		SeasonalEvent: func() *seasonal.Event { return seasonal.Current(now) },
	})
}

// ---- the panel's layout ---------------------------------------------------

// TestPanelLayout pins the settings picker's frame: the title, the
// search box, ten rows of the twenty, the counter and the legend.
//
// The panel layouts are transcribed from the reference's own screen
// (REF v1.4.0, 220×30, /tmp/agycmp/panels/ppref.config.raw.txt). The
// assertions compare the *plain* (SGR-stripped) text, because tmux's
// capture-pane normalises 24-bit RGB into 256-colour indices.
//
// Cycle 208: the settings home is isolated so the catalogue is always
// the 20-row default (the shared settings.json may set editorMode=vim,
// which adds the nested "Insert First" row and changes the counter
// text).
func TestPanelLayout(t *testing.T) {
	testHome(t, map[string]any{})
	p := New(Seams{})
	plain := plainLines(p, 0, 0)
	if plain[0] != "Settings" {
		t.Errorf("title = %q", plain[0])
	}
	// Cursor-bearing list: 10 setting rows; the counter reads
	// "[1-10 of 20 items]" verbatim from REF.
	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "[1-10 of 20 items]") {
		t.Errorf("missing 1-10 of 20 counter: %s", joined)
	}
	// Cursor movement shifts the "> " marker. Cycle 193 finds it
	// by scanning rather than depending on row-index arithmetic
	// (the exact row depends on the search-box/underline/counter
	// layout).
	hi := -1
	for i, l := range plain {
		if strings.HasPrefix(l, "> ") && i >= 5 {
			hi = i
			break
		}
	}
	if hi != 5 {
		t.Errorf("cursor 0: highlight row = %d, want 5 (first setting)", hi)
	}
	p.SetCursor(4)
	hi = -1
	for i, l := range plainLines(p, 0, 0) {
		if strings.HasPrefix(l, "> ") {
			hi = i
		}
	}
	if hi != 9 {
		t.Errorf("cursor 4: highlight row = %d, want 9", hi)
	}
	// Legend contains "Change" for the ←/→ pair REF renders.
	last := plain[len(plain)-1]
	if !strings.Contains(last, "Change") || !strings.Contains(last, "Clear Search/Exit") {
		t.Errorf("legend = %q", last)
	}
}

// ---- the catalogue's dynamic gate ----------------------------------------

// TestCatalogueGate pins the gate on the "Editor Mode › Insert First"
// child row: the reference draws it immediately after Editor Mode while
// editorMode == "vim" (probe_ref_walk2.py row 8).
func TestCatalogueGate(t *testing.T) {
	testHome(t, map[string]any{})
	p := New(Seams{})
	if got := len(p.Catalogue()); got != 20 {
		t.Errorf("catalogue with editorMode unset = %d rows, want 20", got)
	}

	testHome(t, map[string]any{"editorMode": "vim"})
	p = New(Seams{})
	rows := p.Catalogue()
	if len(rows) != 21 {
		t.Fatalf("catalogue with editorMode=vim = %d rows, want 21", len(rows))
	}
	if rows[7].Name != "Editor Mode" || rows[8].Name != "Editor Mode › Insert First" {
		t.Errorf("rows[7]=%q rows[8]=%q, want Editor Mode then its Insert First child",
			rows[7].Name, rows[8].Name)
	}
}

// ---- the value cycle ------------------------------------------------------

// TestCycle197_ConfigDefaultDeletesKey is the headline correctness fix:
// cycling Agent Mode back to its default removes the key instead of
// writing "default".
func TestCycle197_ConfigDefaultDeletesKey(t *testing.T) {
	dir := testHome(t, map[string]any{})
	p := New(Seams{})
	p.SetCursor(rowIndex(t, p, "Agent Mode"))
	rows := p.Rows()

	// default → accept-edits → auto → plan → default
	for _, want := range []string{"accept-edits", "auto", "plan"} {
		p.cycleValue(rows, +1)
		if got := valueAt(t, readSettings(t, dir), "agentMode"); got != want {
			t.Fatalf("after right: agentMode = %q, want %q", got, want)
		}
	}
	p.cycleValue(rows, +1)
	got := readSettings(t, dir)
	// AGY 1.5.7: "choose Default for Agent Mode in /config, which saves "agentMode": "" to settings.json"
	if v, ok := got["agentMode"].(string); !ok || v != "" {
		t.Errorf("after wrapping to default: agentMode = %v, want empty string", got["agentMode"])
	}
	if d := p.SettingValue(rowIndex(t, p, "Agent Mode")); d != "default" {
		t.Errorf("display after wrap = %q, want %q", d, "default")
	}
}

// TestCycle197_ConfigAnimationSpeedCycle pins the four-step cycle that the
// pre-197 clone got wrong (it had [slow medium fast] and was missing
// "off").
func TestCycle197_ConfigAnimationSpeedCycle(t *testing.T) {
	dir := testHome(t, map[string]any{})
	p := New(Seams{})
	p.SetCursor(rowIndex(t, p, "Animation Speed"))
	rows := p.Rows()

	for _, want := range []string{"slow", "off", "fast"} {
		p.cycleValue(rows, +1)
		if got := valueAt(t, readSettings(t, dir), "runningLightSpeed"); got != want {
			t.Fatalf("after right: runningLightSpeed = %q, want %q", got, want)
		}
	}
	p.cycleValue(rows, +1) // fast → medium (default → key removed)
	if v, present := readSettings(t, dir)["runningLightSpeed"]; present {
		t.Errorf("after wrap: runningLightSpeed = %v, want key absent", v)
	}
}

// TestCycle197_ConfigArtifactReviewDisplay covers the display forms the
// pre-197 clone rendered with hyphens.
func TestCycle197_ConfigArtifactReviewDisplay(t *testing.T) {
	cases := []struct {
		stored  any
		display string
	}{
		{nil, "asks for review"},
		{"always-proceed", "always proceeds"},
		{"agent-decides", "agent decides"},
	}
	for _, c := range cases {
		seed := map[string]any{}
		if c.stored != nil {
			seed["artifactReviewPolicy"] = c.stored
		}
		testHome(t, seed)
		p := New(Seams{})
		if got := p.SettingValue(rowIndex(t, p, "Artifact Review")); got != c.display {
			t.Errorf("stored=%v → %q, want %q", c.stored, got, c.display)
		}
	}
}

// TestCycle197_ConfigToolPermissionFromExplicit stores a value that is the
// row's *display default* but is written explicitly: the reference (and
// the clone) advance it to the next entry rather than stalling.
//
// The row carries the four documented values (request-review,
// proceed-in-sandbox, strict, always-proceed) with the default first, so
// "always-proceed" is the last entry and the next one wraps to the
// default — which is the *absent* key.
func TestCycle197_ConfigToolPermissionFromExplicit(t *testing.T) {
	dir := testHome(t, map[string]any{"toolPermission": "always-proceed"})
	p := New(Seams{})
	idx := rowIndex(t, p, "Tool Permission")
	p.SetCursor(idx)
	if got := p.SettingValue(idx); got != "always-proceed" {
		t.Fatalf("display = %q, want always-proceed", got)
	}
	p.cycleValue(p.Rows(), +1)
	if v, present := readSettings(t, dir)["toolPermission"]; present {
		t.Errorf("after right: toolPermission = %v, want key absent (the default)", v)
	}
}

// TestCycle197_ConfigRenderingModeWrap covers a stored value ("always")
// wrapping to the inline view.
//
// Cycle 294 (AGY 1.4.7, measured 2026-10-25): the reference's own panel
// writes an *empty* value for the inline stop, which its
// `json:"altScreenMode,omitempty"` writer then drops from settings.json —
// so the reference's inline option does not survive a restart, because an
// absent key is the reference's default and its default is the alternate
// screen. The clone writes the reference's own inline value, "never",
// which agy 1.4.7 accepts and honours.
func TestCycle197_ConfigRenderingModeWrap(t *testing.T) {
	dir := testHome(t, map[string]any{"altScreenMode": "always"})
	p := New(Seams{})
	idx := rowIndex(t, p, "Rendering Mode")
	p.SetCursor(idx)
	if got := p.SettingValue(idx); got != "full screen (altscreen)" {
		t.Fatalf("display = %q", got)
	}
	p.cycleValue(p.Rows(), +1)
	if got := valueAt(t, readSettings(t, dir), "altScreenMode"); got != "never" {
		t.Errorf("after right: altScreenMode = %q, want never", got)
	}
}

// TestCycle197_ConfigMemoryOnlyTouchesNoDisk pins Enable Telemetry: the
// reference toggles it but writes nothing to settings.json (probe
// diff was empty), so the clone keeps the value in the panel's memory.
func TestCycle197_ConfigMemoryOnlyTouchesNoDisk(t *testing.T) {
	dir := testHome(t, map[string]any{})
	p := New(Seams{})
	idx := rowIndex(t, p, "Enable Telemetry")
	p.SetCursor(idx)
	if got := p.SettingValue(idx); got != "off" {
		t.Fatalf("initial display = %q, want off", got)
	}
	p.cycleValue(p.Rows(), +1)
	if got := p.SettingValue(idx); got != "on" {
		t.Errorf("after right: display = %q, want on", got)
	}
	if got := readSettings(t, dir); len(got) != 0 {
		t.Errorf("settings.json = %v, want untouched", got)
	}
}

// ---- the rendering ---------------------------------------------------------

// TestCycle197_ConfigPanelNoCounterWhenAllFit pins the counter slot: the
// reference blanks it (rather than collapsing the line) when every row
// fits.
//
// The width is 0 on purpose: the panel wraps its description to the
// terminal's width, and an unset width leaves the description on one
// line — the shape the reference's own capture has.
func TestCycle197_ConfigPanelNoCounterWhenAllFit(t *testing.T) {
	testHome(t, map[string]any{})
	p := New(Seams{})
	lines := plainLines(p, 50, 0)
	if len(lines) != 31 {
		t.Errorf("panel = %d lines at height 50, want 31 (live REF)", len(lines))
	}
	for _, l := range lines {
		if strings.Contains(l, "items]") {
			t.Errorf("unexpected counter at height 50: %q", l)
		}
	}
	// The windowed fallback (height unknown) still shows the counter.
	p2 := New(Seams{})
	joined := strings.Join(plainLines(p2, 0, 0), "\n")
	if !strings.Contains(joined, "[1-10 of 20 items]") {
		t.Errorf("unknown height should keep the windowed counter:\n%s", joined)
	}
}

// The rendered nested row must carry three spaces before its value, which
// is what REF prints (`  Editor Mode › Insert First   off`).
func TestCycle208_ConfigNestedRowAlignment(t *testing.T) {
	testHome(t, map[string]any{"editorMode": "vim"})
	p := New(Seams{})
	// Tall enough that every row fits, so no windowing.
	lines := plainLines(p, 60, 0)
	idx := rowIndex(t, p, "Editor Mode › Insert First")
	got := lines[5+idx]
	want := "  Editor Mode › Insert First" + strings.Repeat(" ", 3) + "off"
	if got != want {
		t.Errorf("nested row:\n got %q\nwant %q", got, want)
	}
}

// The counter follows the description block's single trailing blank.
// Live 1.4.3 at 120x40: rows, ”, desc, ”, [1-18 of 20 items], ”, Keyboard.
func TestCycle208_ConfigWindowedCounterBlanks(t *testing.T) {
	testHome(t, map[string]any{"editorMode": "vim"})

	wl := plainLines(New(Seams{}), 24, 0)
	if !strings.Contains(wl[len(wl)-3], "items]") {
		t.Fatalf("expected a counter at len-3, got %q", wl[len(wl)-3])
	}
	if wl[len(wl)-4] != "" || wl[len(wl)-5] == "" {
		t.Errorf("windowed list needs one blank after the description, got %q %q",
			wl[len(wl)-5], wl[len(wl)-4])
	}

	cl := plainLines(New(Seams{}), 60, 0)
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
	testHome(t, map[string]any{"editorMode": "vim"})
	p := New(Seams{})

	// Agent Mode (10 runes) — the two-span form.
	agent := p.Lines(60, 0)[5]
	wantTwo := palette.BlueOpen + "> Agent Mode" + palette.BlueClose + strings.Repeat(" ", 19) +
		palette.BlueOpen + p.SettingValue(rowIndex(t, p, "Agent Mode")) + palette.BlueClose
	if agent != wantTwo {
		t.Errorf("Agent Mode cursor row:\n got %q\nwant %q", agent, wantTwo)
	}

	// Enable Telemetry (16 runes) — the one-span form.
	p.SetCursor(rowIndex(t, p, "Enable Telemetry"))
	tele := p.Lines(60, 0)[14]
	wantOne := palette.BlueOpen + "> Enable Telemetry" + strings.Repeat(" ", 13) +
		p.SettingValue(rowIndex(t, p, "Enable Telemetry")) + palette.BlueClose
	if tele != wantOne {
		t.Errorf("Enable Telemetry cursor row:\n got %q\nwant %q", tele, wantOne)
	}
}

// TestConfigLeftRight_FooterHint pins the changelog's second clause: "the
// footer shows a '←/→ Change' hint".
func TestConfigLeftRight_FooterHint(t *testing.T) {
	testHome(t, map[string]any{})
	lines := plainLines(New(Seams{}), 0, 0)
	if len(lines) == 0 {
		t.Fatal("the panel rendered no rows")
	}
	legend := lines[len(lines)-1]
	if !strings.Contains(legend, "Change") {
		t.Errorf("legend missing %q label: %q", "Change", legend)
	}
	hasArrowForm := strings.Contains(legend, "←/→")
	hasDashForm := strings.Contains(legend, "<-/->")
	if !hasArrowForm && !hasDashForm {
		t.Errorf("legend missing %q or %q key form: %q", "←/→", "<-/->", legend)
	}
}

// ---- the ←/→ cycle in both directions --------------------------------------

// TestConfigLeftRight_AdvancesValue walks the Verbosity cycle in both
// directions and pins its order. The catalogue's cycle list for
// Verbosity is, verbatim from probe_ref_walk2.py row 17:
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
// numeric-arrow "increment index until end" sanity stub.
func TestConfigLeftRight_AdvancesValue(t *testing.T) {
	testHome(t, map[string]any{}) // empty settings → default
	var applied []string
	p := New(Seams{
		ApplyVerbosity: func(v string) {
			applied = append(applied, v)
		},
	})
	idx := rowIndex(t, p, "Verbosity")
	p.SetCursor(idx)
	rows := p.Rows()

	// Sanity: the default reads as the row's first cycle value.
	if v := p.SettingValue(idx); v != "medium" {
		t.Fatalf("baseline: verbosity = %q, want %q", v, "medium")
	}

	p.cycleValue(rows, +1) // medium → low
	if v := p.SettingValue(idx); v != "low" {
		t.Errorf("after 1× Right: verbosity = %q, want %q", v, "low")
	}

	p.cycleValue(rows, +1) // low → high
	if v := p.SettingValue(idx); v != "high" {
		t.Errorf("after 2× Right: verbosity = %q, want %q", v, "high")
	}

	p.cycleValue(rows, +1) // high → medium (wrap forward)
	if v := p.SettingValue(idx); v != "medium" {
		t.Errorf("after 3× Right (wrap): verbosity = %q, want %q", v, "medium")
	}

	p.cycleValue(rows, -1) // medium → high (wrap backward)
	if v := p.SettingValue(idx); v != "high" {
		t.Errorf("after Left (wrap back): verbosity = %q, want %q", v, "high")
	}

	wantApplied := []string{"low", "high", "medium", "high"}
	if len(applied) != len(wantApplied) {
		t.Fatalf("applied count = %d, want %d: %v", len(applied), len(wantApplied), applied)
	}
	for i := range wantApplied {
		if applied[i] != wantApplied[i] {
			t.Errorf("applied[%d] = %q, want %q", i, applied[i], wantApplied[i])
		}
	}
}

// TestConfigLeftRight_PersistsToDisk is the core persistence pin: after a
// single ←/→ the new value must be visible in settings.json, not just in
// the panel's memory. The "immediate" wording in the AGY 1.2.16
// changelog is what this asserts — the write happens inside the cycle,
// not at some later "save settings" keystroke.
//
// The test seeds with colorScheme="light" (unrelated to verbosity) so
// that the file already exists when the cycle writes, and the helper
// that handles a missing key still has its settings.json to mutate.
func TestConfigLeftRight_PersistsToDisk(t *testing.T) {
	dir := testHome(t, map[string]any{
		"colorScheme": "light",
	})
	p := New(Seams{})
	idx := rowIndex(t, p, "Verbosity")
	p.SetCursor(idx)

	// Snapshot the disk up front so we can prove the *write* happened.
	pre := readSettings(t, dir)
	if _, ok := pre["verbosity"]; ok {
		t.Fatalf("baseline: verbosity already in disk as %v; rewrite the test", pre["verbosity"])
	}

	p.cycleValue(p.Rows(), +1) // medium → low

	got := readSettings(t, dir)
	if v, ok := got["verbosity"].(string); !ok || v != "low" {
		t.Errorf("after Right: settings.json verbosity = %v, want %q", got["verbosity"], "low")
	}

	// The unrelated seed key MUST survive untouched — persistence is
	// additive, not a wholesale rewrite.
	if v := valueAt(t, got, "colorScheme"); v != "light" {
		t.Errorf("colorScheme clobbered: %q, want %q", v, "light")
	}
}

// TestConfigLeftRight_NonEnumSetting pins the safety floor for rows that
// cannot be cycled. The catalogue contains 20 rows and every one of them
// is an enum with len(values) >= 2, but the guard inside the cycle
// (length<2 → return) is the canonical non-enum no-op contract. Two
// complementary checks:
//
//  1. Structural: every catalogue row carries at least two values (so the
//     catalogue never accidentally adds a one-element row that the render
//     would render but the keys could not act on).
//  2. Behavioural: an out-of-range cursor leaves settings.json untouched
//     and does not panic.
func TestConfigLeftRight_NonEnumSetting(t *testing.T) {
	// 1. Structural: every catalogue row has >=2 cycle values.
	for i, r := range New(Seams{}).Catalogue() {
		if len(r.Values) < 2 {
			t.Errorf("row %d (%q): len(values)=%d, want >=2 (←/→ has nothing to cycle)", i, r.Name, len(r.Values))
		}
	}

	// 2. Behavioural: cursor past the end of the catalogue.
	dir := testHome(t, map[string]any{})
	pre := readSettings(t, dir)

	p := New(Seams{})
	p.cursor = len(p.Catalogue()) + 5

	// Must not panic.
	p.cycleValue(p.Rows(), +1)
	p.cycleValue(p.Rows(), -1)

	post := readSettings(t, dir)
	if len(pre) != len(post) {
		t.Errorf("settings.json mutated despite out-of-range cursor: pre=%v post=%v", pre, post)
	}

	// And: a negative cursor does not crash either (which a key press
	// cannot produce, but the guard inside the cycle is the only thing
	// keeping the renderer safe).
	p.cursor = -1
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right at cursor=-1 reported unhandled; want handled (guard returns early)")
	}
}

// ---- the cycle's shapes ----------------------------------------------------

// TestCycle196_ConfigCycleRight_SimpleString is the canonical
// cycle-through case: cursor on Color Scheme (cycle list:
// terminal → light → solarized light → …), right arrow advances one step
// and persists the *stored* form.
func TestCycle196_ConfigCycleRight_SimpleString(t *testing.T) {
	dir := testHome(t, map[string]any{
		"colorScheme": "light",
	})
	p := New(Seams{})
	idx := rowIndex(t, p, "Color Scheme")
	p.SetCursor(idx)

	p.cycleValue(p.Rows(), +1)
	if got := valueAt(t, readSettings(t, dir), "colorScheme"); got != "solarized light" {
		t.Errorf("after right: colorScheme = %q, want %q", got, "solarized light")
	}
	if v := p.SettingValue(idx); v != "solarized light" {
		t.Errorf("the row's display = %q, want %q", v, "solarized light")
	}
}

// TestCycle196_ConfigCycleLeft_ToDefault covers the reference's
// default-by-absence convention: from the second cycle value ("light")
// pressing ← lands on the default ("terminal") and *removes* the key
// rather than writing the string "terminal".
func TestCycle196_ConfigCycleLeft_ToDefault(t *testing.T) {
	dir := testHome(t, map[string]any{
		"colorScheme": "light",
	})
	p := New(Seams{})
	idx := rowIndex(t, p, "Color Scheme")
	p.SetCursor(idx)
	p.cycleValue(p.Rows(), -1)
	got := readSettings(t, dir)
	if _, present := got["colorScheme"]; present {
		t.Errorf("after left from light: colorScheme = %v, want key absent (default)", got["colorScheme"])
	}
	if v := p.SettingValue(idx); v != "terminal" {
		t.Errorf("the row's display = %q, want %q", v, "terminal")
	}
}

// TestCycle196_ConfigCycle_BoolSetting exercises the boolean path on a
// real row from the 1.4.0 catalogue: "Show Tips" is on by default
// (absent) and persists `false` when toggled off.
func TestCycle196_ConfigCycle_BoolSetting(t *testing.T) {
	dir := testHome(t, map[string]any{"showTips": true})
	p := New(Seams{})
	idx := rowIndex(t, p, "Show Tips")
	p.SetCursor(idx)

	p.cycleValue(p.Rows(), -1) // on → off
	got := readSettings(t, dir)
	if v := valueAt(t, got, "showTips"); v != "off" {
		t.Errorf("after left: showTips = %q, want %q", v, "off")
	}
	if v := p.SettingValue(idx); v != "off" {
		t.Errorf("display = %q, want %q", v, "off")
	}

	p.cycleValue(p.Rows(), +1) // off → on (default: key removed)
	got = readSettings(t, dir)
	if _, present := got["showTips"]; present {
		t.Errorf("after right: showTips = %v, want key absent (default on)", got["showTips"])
	}
	if v := p.SettingValue(idx); v != "on" {
		t.Errorf("display = %q, want %q", v, "on")
	}
}

// TestCycle196_ConfigCycle_AbsentKey covers the case where the
// settings.json file lacks the key (first-launch state).
func TestCycle196_ConfigCycle_AbsentKey(t *testing.T) {
	dir := testHome(t, map[string]any{
		"colorScheme": "dark",
	})
	p := New(Seams{})
	p.SetCursor(rowIndex(t, p, "Agent Mode"))
	if v := p.SettingValue(rowIndex(t, p, "Agent Mode")); v != "default" {
		t.Errorf("missing key: the row's display = %q, want %q", v, "default")
	}
	p.cycleValue(p.Rows(), +1) // default → accept-edits
	got := readSettings(t, dir)
	if v, ok := got["agentMode"].(string); !ok || v != "accept-edits" {
		t.Errorf("after right from absent: agentMode = %v, want %q", got["agentMode"], "accept-edits")
	}
}

// TestCycle196_ConfigValue_LiveRead verifies that the rendered panel
// reflects live settings.json state.
func TestCycle196_ConfigValue_LiveRead(t *testing.T) {
	testHome(t, map[string]any{
		"colorScheme": "dark",
	})
	p := New(Seams{})
	if v := p.SettingValue(rowIndex(t, p, "Color Scheme")); v != "dark" {
		t.Errorf("the row's display with dark in disk = %q, want %q", v, "dark")
	}
}

// ---- the search box ---------------------------------------------------------

// TestConfigSearchKeyHandling pins the search box's own keys: the letters
// type into it, backspace removes one rune, and the first Esc clears the
// query.
func TestConfigSearchKeyHandling(t *testing.T) {
	p := New(Seams{})

	// Type 'c'
	p.HandleKey(tea.Key{Code: 'c', Text: "c"})
	if p.Query() != "c" {
		t.Fatalf("expected the query %q, got %q", "c", p.Query())
	}

	// Type 'o'
	p.HandleKey(tea.Key{Code: 'o', Text: "o"})
	if p.Query() != "co" {
		t.Fatalf("expected the query %q, got %q", "co", p.Query())
	}

	// The rendered panel shows the query.
	joined := strings.Join(plainLines(p, 40, 120), "\n")
	if !strings.Contains(joined, "Search: co") {
		t.Fatalf("expected a search line with %q, got:\n%s", "co", joined)
	}

	// Backspace
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.Query() != "c" {
		t.Fatalf("expected the query %q after backspace, got %q", "c", p.Query())
	}

	// Esc once: clears the search query, the panel stays open.
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.Query() != "" {
		t.Fatalf("expected an empty query after the first Esc, got %q", p.Query())
	}
}

// TestConfigSearchFiltersRows pins that the query narrows the rows the
// panel draws.
func TestConfigSearchFiltersRows(t *testing.T) {
	p := New(Seams{})
	for _, r := range "color" {
		p.HandleKey(tea.Key{Code: r, Text: string(r)})
	}
	rows := p.Rows()
	if len(rows) != 1 || rows[0].Name != "Color Scheme" {
		t.Fatalf("the query %q left %d rows, want the Color Scheme row", p.Query(), len(rows))
	}
	// The cursor is parked on the first hit.
	if p.Cursor() != 0 {
		t.Errorf("cursor = %d, want 0 (the first hit)", p.Cursor())
	}
}

// ---- the inline option submenu ---------------------------------------------

// TestConfigSubmenuEnterAndSave pins the submenu: Enter opens it, the
// arrow keys walk the values, Esc cancels, and Enter saves.
func TestConfigSubmenuEnterAndSave(t *testing.T) {
	testHome(t, map[string]any{})
	p := New(Seams{})
	p.SetCursor(rowIndex(t, p, "Agent Mode"))

	// Press Enter to open the submenu.
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.SubmenuOpen() {
		t.Fatal("expected the submenu to be open after Enter")
	}

	joined := stripANSI(strings.Join(p.Lines(40, 120), "\n"))
	if !strings.Contains(joined, "enter Save") || !strings.Contains(joined, "esc Cancel") {
		t.Fatalf("expected the submenu keyboard hint, got:\n%s", joined)
	}

	// Down arrow moves the option cursor.
	prev := p.submenuCursor
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if p.submenuCursor != prev+1 && p.submenuCursor != prev {
		t.Fatalf("expected the cursor to move, was %d now %d", prev, p.submenuCursor)
	}

	// Esc cancels without saving.
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.SubmenuOpen() {
		t.Fatal("expected the submenu to close after Esc")
	}

	// Open again, press Enter to save.
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.SubmenuOpen() {
		t.Fatal("expected the submenu to open again")
	}
	p.submenuCursor = 1 // 'accept-edits'
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.SubmenuOpen() {
		t.Fatal("expected the submenu to close after the save Enter")
	}
	if got := p.SettingValue(rowIndex(t, p, "Agent Mode")); got != "accept-edits" {
		t.Errorf("the row's display = %q, want %q", got, "accept-edits")
	}
}

// TestAllMenuItemsCanExpandWithEnter pins that every catalogue row
// expands on Enter and collapses on Esc.
func TestAllMenuItemsCanExpandWithEnter(t *testing.T) {
	testHome(t, map[string]any{})
	p := New(Seams{})
	rows := p.Rows()
	if len(rows) < 18 {
		t.Fatalf("expected at least 18 settings rows, got %d", len(rows))
	}

	for i, r := range rows {
		p.SetCursor(i)
		p.submenu = false

		// Press Enter to expand.
		p.HandleKey(tea.Key{Code: tea.KeyEnter})
		if !p.SubmenuOpen() {
			t.Fatalf("row %d (%q): expected the submenu to open on Enter", i, r.Name)
		}

		joined := stripANSI(strings.Join(p.Lines(40, 120), "\n"))
		if !strings.Contains(joined, r.Name) {
			t.Fatalf("row %d (%q): expected the name in the expanded output:\n%s", i, r.Name, joined)
		}
		if !strings.Contains(joined, "(current)") {
			t.Fatalf("row %d (%q): expected the (current) marker in the expanded options:\n%s", i, r.Name, joined)
		}

		// Press Esc to collapse.
		p.HandleKey(tea.Key{Code: tea.KeyEsc})
		if p.SubmenuOpen() {
			t.Fatalf("row %d (%q): expected the submenu to close on Esc", i, r.Name)
		}
	}
}

// TestColorSchemePreviewCardRendering pins the Color Scheme submenu's
// preview card and the save that follows it.
//
// 【一手】 agy 1.4.8's preview card writes 24-bit escapes for the code
// roles — it never falls back to a 256-colour index — so the expectation
// is the scheme's keyword colour, not onboarding's index form.
func TestColorSchemePreviewCardRendering(t *testing.T) {
	testHome(t, map[string]any{})
	p := New(Seams{})
	p.query = "color"

	rows := p.Rows()
	if len(rows) != 1 || rows[0].Name != "Color Scheme" {
		t.Fatalf("expected 1 row %q, got %+v", "Color Scheme", rows)
	}

	p.SetCursor(0)
	// Open the Color Scheme submenu.
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.SubmenuOpen() {
		t.Fatal("expected the submenu to be open")
	}

	joined := stripANSI(strings.Join(p.Lines(40, 120), "\n"))

	// Verify the right-side preview box border glyphs.
	if !strings.Contains(joined, "╭─────────────────────────────────────────────────────────────╮") {
		t.Fatalf("expected the top border of the preview box in:\n%s", joined)
	}
	if !strings.Contains(joined, "╰─────────────────────────────────────────────────────────────╯") {
		t.Fatalf("expected the bottom border of the preview box in:\n%s", joined)
	}

	// Verify the sample code snippet's contents.
	if !strings.Contains(joined, "func  greet(name") && !strings.Contains(joined, "func greet(name") {
		t.Fatalf("expected the greet function in the preview box in:\n%s", joined)
	}
	if !strings.Contains(joined, "▾ Thought Process") {
		t.Fatalf("expected Thought Process in the preview box in:\n%s", joined)
	}
	if !strings.Contains(joined, "⚙ tool:") || !strings.Contains(joined, "◉ task:") || !strings.Contains(joined, "✗ error:") {
		t.Fatalf("expected the tool, task and error glyphs in the preview box in:\n%s", joined)
	}

	// Verify the left-side options.
	if !strings.Contains(joined, "terminal") || !strings.Contains(joined, "tokyo night") {
		t.Fatalf("expected the terminal and tokyo night schemes listed on the left side")
	}

	// Verify the live preview update when the cursor moves to Tokyo Night
	// (index 7).
	p.submenuCursor = 7
	joinedTokyo := strings.Join(p.Lines(40, 120), "\n")
	tokyoSyn := palette.SyntaxColorsFor("tokyo night")
	tokyoCodeStr := palette.SeqWith(tokyoSyn.Keyword, tokyoSyn.KeywordAttr)
	if !strings.Contains(joinedTokyo, tokyoCodeStr) {
		t.Fatalf("expected the Tokyo Night keyword color %s in the preview output", tokyoCodeStr)
	}

	// Save Tokyo Night.
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.SubmenuOpen() {
		t.Fatal("expected the submenu to close after the save")
	}
	if got := p.SettingValue(rowIndex(t, p, "Color Scheme")); got != "tokyo night" {
		t.Errorf("the row's display = %q, want %q", got, "tokyo night")
	}
}

// TestColorSchemeLiveThemeUpdate pins the live theme: the submenu
// previews a scheme while the user walks the values, Esc puts the
// original scheme back, and ←/→ cycles the row live and persists it.
//
// The theme is the host's, so the panel asks for it through the seam —
// the panel only decides *when* the scheme changes.
func TestColorSchemeLiveThemeUpdate(t *testing.T) {
	testHome(t, map[string]any{"colorScheme": "terminal"})
	var applied []string
	p := New(Seams{ApplyTheme: func(scheme string) tea.Cmd {
		applied = append(applied, scheme)
		return nil
	}})
	p.query = "color"
	p.SetCursor(0)

	// 1. Open the submenu.
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.SubmenuOpen() {
		t.Fatal("expected the submenu to be open")
	}

	// 2. Move down to "light" (index 1).
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if len(applied) == 0 || applied[len(applied)-1] != "light" {
		t.Fatalf("expected the live theme %q, got %v", "light", applied)
	}

	// 3. Move down to "solarized light" (index 2).
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if applied[len(applied)-1] != "solarized light" {
		t.Fatalf("expected the live theme %q, got %v", "solarized light", applied)
	}

	// 4. Cancel with Esc: the original scheme comes back.
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.SubmenuOpen() {
		t.Fatal("expected the submenu to close")
	}
	if applied[len(applied)-1] != "terminal" {
		t.Fatalf("expected the live theme to revert to %q, got %v", "terminal", applied)
	}

	// 5. ←/→ on the main list cycles live and persists.
	handled, cmd := p.HandleKey(tea.Key{Code: tea.KeyRight})
	if !handled || cmd == nil {
		t.Fatalf("expected KeyRight to return handled=true and a clear-screen command")
	}
	if applied[len(applied)-1] != "light" {
		t.Fatalf("expected the live theme %q after →, got %v", "light", applied)
	}
	if got := p.SettingValue(rowIndex(t, p, "Color Scheme")); got != "light" {
		t.Errorf("the row's display = %q, want %q", got, "light")
	}
}

// ---- the Business-account lock ---------------------------------------------

// TestCycle241_TelemetryBusinessLockedDisplay: with AGY_ADC_AUTH set the
// row paints the literal `off (disabled)` regardless of the in-memory
// state (which would otherwise be "on", the default per cycle 197).
func TestCycle241_TelemetryBusinessLockedDisplay(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")
	// No settings.json seed needed — telemetry is memoryOnly and the
	// default display is "off" (cf. TestCycle197_ConfigMemoryOnlyTouchesNoDisk).
	testHome(t, map[string]any{})
	p := New(Seams{})
	idx := rowIndex(t, p, "Enable Telemetry")
	if got := p.SettingValue(idx); got != auth.TelemetryDisabledValue {
		t.Errorf("AGY_ADC_AUTH set: row value = %q, want %q", got, auth.TelemetryDisabledValue)
	}
}

// TestCycle241_TelemetryPersonalUnchanged: with AGY_ADC_AUTH unset the
// row cycles normally between "on" and "off" (cycle 197 contract, no
// regression).
func TestCycle241_TelemetryPersonalUnchanged(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")
	testHome(t, map[string]any{})
	p := New(Seams{})
	idx := rowIndex(t, p, "Enable Telemetry")
	p.SetCursor(idx)
	if got := p.SettingValue(idx); got != "off" {
		t.Errorf("AGY_ADC_AUTH unset: initial row value = %q, want off", got)
	}
	p.cycleValue(p.Rows(), +1)
	if got := p.SettingValue(idx); got != "on" {
		t.Errorf("AGY_ADC_AUTH unset: after +1 row value = %q, want on", got)
	}
	p.cycleValue(p.Rows(), +1)
	if got := p.SettingValue(idx); got != "off" {
		t.Errorf("AGY_ADC_AUTH unset: after +1 round-trip row value = %q, want off", got)
	}
}

// TestCycle241_TelemetryToggleRefused: with AGY_ADC_AUTH set the ←/→
// cycle is a no-op (no state mutation) and the transcript records the
// refusal message. With AGY_ADC_AUTH unset, the toggle round-trips.
func TestCycle241_TelemetryToggleRefused(t *testing.T) {
	t.Run("AGY_ADC_AUTH set: toggle is a no-op and surfaces the message", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "true")
		testHome(t, map[string]any{})
		var responses []string
		p := New(Seams{Respond: func(cmd, msg string) {
			responses = append(responses, cmd+": "+msg)
		}})
		idx := rowIndex(t, p, "Enable Telemetry")
		p.SetCursor(idx)

		p.cycleValue(p.Rows(), +1)
		if got := p.SettingValue(idx); got != auth.TelemetryDisabledValue {
			t.Errorf("AGY_ADC_AUTH set: after +1 display = %q, want %q",
				got, auth.TelemetryDisabledValue)
		}
		if p.memory != nil {
			if v, ok := p.memory["enableTelemetry"]; ok && v != "on" {
				t.Errorf("AGY_ADC_AUTH set: the memory moved to %q, want absent or %q", v, "on")
			}
		}
		found := false
		for _, r := range responses {
			if strings.Contains(r, auth.TelemetryDisabledResponse) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AGY_ADC_AUTH set: the refusal message %q never arrived: %v",
				auth.TelemetryDisabledResponse, responses)
		}
	})

	t.Run("AGY_ADC_AUTH unset: the toggle round-trips", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "")
		testHome(t, map[string]any{})
		var responses []string
		p := New(Seams{Respond: func(cmd, msg string) {
			responses = append(responses, cmd+": "+msg)
		}})
		idx := rowIndex(t, p, "Enable Telemetry")
		p.SetCursor(idx)

		p.cycleValue(p.Rows(), +1) // off → on
		if got := p.SettingValue(idx); got != "on" {
			t.Errorf("after +1: row = %q, want on", got)
		}
		p.cycleValue(p.Rows(), -1) // on → off
		if got := p.SettingValue(idx); got != "off" {
			t.Errorf("after -1: row = %q, want off", got)
		}
		for _, r := range responses {
			if strings.Contains(r, auth.TelemetryDisabledResponse) {
				t.Errorf("AGY_ADC_AUTH unset: the refusal message must not arrive: %v", responses)
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
	testHome(t, map[string]any{})
	p := New(Seams{})
	p.SetCursor(rowIndex(t, p, "Enable Telemetry"))
	lines := plainLines(p, 0, 0)
	trimmed := strings.TrimLeft(findDescriptionLine(t, lines), " \t")
	if !strings.HasPrefix(trimmed, "Disabled for Business accounts.") {
		t.Errorf("Business desc: got %q, want it to start with %q",
			trimmed, "Disabled for Business accounts.")
	}
	if !strings.Contains(trimmed, auth.TelemetryProductName) {
		t.Errorf("Business desc: got %q, want it to mention %q",
			trimmed, auth.TelemetryProductName)
	}

	// --- Personal-account case (no regression) ----------------------------
	t.Setenv("AGY_ADC_AUTH", "")
	p2 := New(Seams{})
	p2.SetCursor(rowIndex(t, p2, "Enable Telemetry"))
	trimmed2 := strings.TrimLeft(findDescriptionLine(t, plainLines(p2, 0, 0)), " \t")
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
	testHome(t, map[string]any{})
	p := New(Seams{})
	p.SetCursor(rowIndex(t, p, "Enable Telemetry"))
	rowLine := findCursorRowLine(t, plainLines(p, 0, 0), "Enable Telemetry")
	if !strings.Contains(rowLine, auth.TelemetryDisabledValue) {
		t.Errorf("row line = %q, want it to contain %q",
			rowLine, auth.TelemetryDisabledValue)
	}
	if !strings.HasPrefix(rowLine, "> Enable Telemetry") {
		t.Errorf("row line = %q, want it to start with the cursor prefix", rowLine)
	}
}

// TestCycle241_TelemetryLockedRowNotInjectedElsewhere: only the
// `Enable Telemetry` row gets the locked display; every other row in
// the catalogue must keep its pre-241 normal cycle.
func TestCycle241_TelemetryLockedRowNotInjectedElsewhere(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	testHome(t, map[string]any{})
	p := New(Seams{})
	for _, name := range []string{
		"Agent Mode", "Animation Speed", "Color Scheme", "Show Tips",
		"Verbosity", "Notifications", "Non-Workspace Access",
	} {
		if got := p.SettingValue(rowIndex(t, p, name)); got == auth.TelemetryDisabledValue {
			t.Errorf("non-telemetry row %q painted as %q; should be the normal display",
				name, auth.TelemetryDisabledValue)
		}
	}
}

// TestCycle241_TelemetryKeyHandlerRoutesToGate drives the user-facing
// path: ←/→ on the locked row reaches the cycle through the key handler
// and surfaces the refusal message.
func TestCycle241_TelemetryKeyHandlerRoutesToGate(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	testHome(t, map[string]any{})
	var responses []string
	p := New(Seams{Respond: func(cmd, msg string) {
		responses = append(responses, cmd+": "+msg)
	}})
	idx := rowIndex(t, p, "Enable Telemetry")
	p.SetCursor(idx)

	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyRight})
	if !handled {
		t.Fatal("←/→ on /config: the key handler reported unhandled")
	}
	if got := p.SettingValue(idx); got != auth.TelemetryDisabledValue {
		t.Errorf("after the → key: row = %q, want %q", got, auth.TelemetryDisabledValue)
	}
	found := false
	for _, r := range responses {
		if strings.Contains(r, auth.TelemetryDisabledResponse) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("after the → key: the refusal message never arrived: %v", responses)
	}
}

// findDescriptionLine returns the first plain-text line in the rendered
// panel that carries the description slot — the one between the rows and
// the counter / keyboard hint that begins with one of the two known
// descriptions.
func findDescriptionLine(t *testing.T, lines []string) string {
	t.Helper()
	for _, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(trimmed, "Disabled for Business accounts.") ||
			strings.HasPrefix(trimmed, "When toggled on,") {
			return line
		}
	}
	t.Fatalf("could not find a description line in the panel:\n%s",
		strings.Join(lines, "\n"))
	return ""
}

// findCursorRowLine returns the first plain-text line that starts with
// `> <rowName>` — the cursor row in the painted catalogue.
func findCursorRowLine(t *testing.T, lines []string, rowName string) string {
	t.Helper()
	prefix := "> " + rowName
	for _, line := range lines {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	t.Fatalf("could not find the cursor row %q in the panel:\n%s",
		rowName, strings.Join(lines, "\n"))
	return ""
}

// ---- the seasonal row --------------------------------------------------------

// TestSeasonalConfigRowOnlyInSeason pins the row's gate: the reference
// draws it only while the theme is in season (the 1.4.7 changelog says
// "a row shown only while the theme is in season").
func TestSeasonalConfigRowOnlyInSeason(t *testing.T) {
	testHome(t, map[string]any{})
	if seasonalRowIndex(seasonalPanel(time.October).Catalogue()) < 0 {
		t.Fatal("the Seasonal Themes row is missing in October")
	}
	if seasonalRowIndex(seasonalPanel(time.January).Catalogue()) >= 0 {
		t.Error("the Seasonal Themes row is drawn outside the season")
	}
}

// TestSeasonalConfigRowPosition pins where the reference draws the row:
// between "Sandbox Mode" and "Show Active Tasks".
func TestSeasonalConfigRowPosition(t *testing.T) {
	testHome(t, map[string]any{})
	rows := seasonalPanel(time.October).Catalogue()
	idx := seasonalRowIndex(rows)
	if idx < 0 {
		t.Fatal("the Seasonal Themes row is missing in October")
	}
	if idx == 0 || rows[idx-1].Name != "Sandbox Mode" {
		t.Errorf("the row follows %q, want %q", rows[idx-1].Name, "Sandbox Mode")
	}
	if idx+1 >= len(rows) || rows[idx+1].Name != "Show Active Tasks" {
		t.Errorf("the row precedes %q, want %q", rows[idx+1].Name, "Show Active Tasks")
	}
}

// TestSeasonalConfigRowDescription pins the row's help line, which is
// the reference's own sentence.
//
// 【一手】 agy 1.4.7 /config.
func TestSeasonalConfigRowDescription(t *testing.T) {
	testHome(t, map[string]any{})
	rows := seasonalPanel(time.October).Catalogue()
	idx := seasonalRowIndex(rows)
	if idx < 0 {
		t.Fatal("the Seasonal Themes row is missing in October")
	}
	if rows[idx].Description != seasonal.RowDescription {
		t.Errorf("description = %q, want %q", rows[idx].Description, seasonal.RowDescription)
	}
	if rows[idx].SettingsKey != seasonal.OptInKey {
		t.Errorf("settings key = %q, want %q", rows[idx].SettingsKey, seasonal.OptInKey)
	}
}

// TestSeasonalConfigRowValue pins the two stops and the way the
// reference stores them: the event's own id when on, the key absent
// when off.
//
// 【一手】 agy 1.4.7 settings.json: switching the row on writes
// "seasonalThemesOptIn": "halloween-2026"; switching it off removes the
// key again.
func TestSeasonalConfigRowValue(t *testing.T) {
	testHome(t, map[string]any{})
	p := seasonalPanel(time.October)
	rows := p.Catalogue()
	idx := seasonalRowIndex(rows)
	if idx < 0 {
		t.Fatal("the Seasonal Themes row is missing in October")
	}
	row := rows[idx]

	if got := p.displayFor(row, nil); got != "off" {
		t.Errorf("an absent key reads as %q, want %q", got, "off")
	}
	if got := p.displayFor(row, "halloween-2026"); got != "on" {
		t.Errorf("the event id reads as %q, want %q", got, "on")
	}
	if got := p.displayFor(row, "halloween-2025"); got != "off" {
		t.Errorf("a stale event id reads as %q, want %q", got, "off")
	}
}

// TestSeasonalConfigRowPersists pins what the row writes: the event id
// for "on", and the *deletion* of the key for "off" — the reference's
// own "a setting at its default is absent" convention.
func TestSeasonalConfigRowPersists(t *testing.T) {
	dir := testHome(t, map[string]any{})
	p := seasonalPanel(time.October)
	rows := p.Catalogue()
	idx := seasonalRowIndex(rows)
	if idx < 0 {
		t.Fatal("the Seasonal Themes row is missing in October")
	}
	row := rows[idx]

	p.applyValue(row, row.Values[1])
	if got := readSettings(t, dir)["seasonalThemesOptIn"]; got != "halloween-2026" {
		t.Errorf("the opt-in = %v, want %q", got, "halloween-2026")
	}

	p.applyValue(row, row.Values[0])
	if got := readSettings(t, dir)["seasonalThemesOptIn"]; got != nil {
		t.Errorf("the opt-in is still %v, want the key to be gone", got)
	}
}

// seasonalRowIndex returns the index of the "Seasonal Themes" row in
// rows, or -1.
func seasonalRowIndex(rows []Row) int {
	for i, r := range rows {
		if r.Name == seasonal.RowName {
			return i
		}
	}
	return -1
}
