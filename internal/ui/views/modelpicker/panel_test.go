// panel_test.go — the /model picker's interactive half.
//
// The panel is tested here, not in the host: it owns the search box, the
// row list, the effort slider, the keys and the writes, and a test that
// drives those things belongs with the code that answers them.
//
// The pure decisions — the row model, the search filter, the catalogue
// id mapping, the slider geometry — are pinned in modelpicker_test.go,
// slider_test.go and tier_rows_test.go next to the code that makes them.

package modelpicker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
)

// tieredProviders is a configuration with one model that declares five
// reasoning tiers and one that declares none.
const tieredProviders = `{"providers": {"agw": {
  "type": "openai",
  "api_url": "https://gw.example/v1/chat/completions",
  "models": [
    {"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "MiniMax-M3.1-Flash",
     "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
     "reasoning_effort": "medium"},
    {"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3"}
  ]}}}`

// ---- the test's own isolation ---------------------------------------------

// testHome isolates the settings file for one test and returns the home
// directory.
//
// The panel writes the chosen model to settings.json, so a test must not
// touch the user's own file: the home is redirected at a scratch
// directory and the starting document is seeded from `seed`. The seed is
// mandatory — the settings writer refuses to create the file.
func testHome(t *testing.T, seed map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	b, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), b, 0600); err != nil {
		t.Fatalf("write seed: %v", err)
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

// settingsModel returns the persisted "model" value.
func settingsModel(t *testing.T, dir string) string {
	t.Helper()
	m := readSettings(t, dir)
	v, ok := m["model"].(string)
	if !ok {
		t.Fatalf("settings.json has no string \"model\": %v", m)
	}
	return v
}

// isolateCatalogue points the model catalogue at an empty configuration so
// the picker holds only the reference's own rows.
//
// "{}" and not "": an empty file does not parse, and the registry cache
// then falls back to the last good one — the user's own.
func isolateCatalogue(t *testing.T) {
	t.Helper()
	configtest.WriteProviders(t, "{}")
	t.Setenv("AGY_ADC_AUTH", "")
	t.Setenv("AGY_CLAUDE55_PROJECT", "")
}

// ---- the panel under test -------------------------------------------------

// testPanel is a panel plus the record of what it handed the host.
type testPanel struct {
	*Panel
	// applied is the last display label the panel handed to ApplyModel.
	applied string
	// closed counts the Close calls the panel made.
	closed int
}

// newTestPanel builds the /model panel with the seams a test needs.
//
// The seams mirror the host's: the running model's label is the label
// the session would run at — resolved from the persisted settings, the
// way the host resolves it — and the label the panel hands back is
// recorded.
func newTestPanel(t *testing.T) *testPanel {
	t.Helper()
	tp := &testPanel{}
	tp.Panel = New(Seams{
		CurrentModelLabel: func() string {
			lbl, _ := catalog.ResolveEffectiveModel("")
			return lbl
		},
		ApplyModel: func(label string) {
			tp.applied = label
		},
		Close: func() tea.Cmd {
			tp.closed++
			return nil
		},
	})
	return tp
}

// plainLines renders the panel and strips the colour from every line.
func plainLines(p *Panel) []string {
	var out []string
	for _, l := range p.Lines() {
		out = append(out, stripSGR(l))
	}
	return out
}

// effortBarLine returns the rendered line that hosts the embedded effort
// slider. It is "" when the slider is not on screen — the rows that offer
// a single effort omit it.
func effortBarLine(p *Panel) string {
	for _, l := range plainLines(p) {
		if strings.HasPrefix(l, "  Effort") {
			return l
		}
	}
	return ""
}

// countConsecutiveBefore counts how many `●` (filled circle) markers
// appear before the first `◉` (active marker) in `s`.
func countConsecutiveBefore(s string) int {
	runes := []rune(s)
	i := -1
	for k, r := range runes {
		if r == '◉' {
			i = k
			break
		}
	}
	if i < 0 {
		return -1
	}
	n := 0
	for _, r := range runes[:i] {
		if r == '●' {
			n++
		}
	}
	return n
}

// rowIndex resolves a row's index by its slug, so the tests do not depend
// on the order of the catalogue.
func rowIndex(t *testing.T, p *Panel, slug string) int {
	t.Helper()
	for i, r := range p.Rows() {
		if r.Slug == slug {
			return i
		}
	}
	t.Fatalf("the picker has no row with the slug %q", slug)
	return -1
}

// ---- the panel's frame ----------------------------------------------------

// TestPanelMarksTheEffectiveModelAndParksTheSlider pins that a settings
// value the catalogue cannot use still marks — and parks the slider on —
// the fallback model, matching REF's capture.
func TestPanelMarksTheEffectiveModelAndParksTheSlider(t *testing.T) {
	isolateCatalogue(t)
	testHome(t, map[string]any{"model": "gemini-3.8-flash"})
	p := newTestPanel(t)

	lines := plainLines(p.Panel)
	if len(lines) < 6 {
		t.Fatalf("the panel rendered %d lines, want the header and at least one row", len(lines))
	}
	if !strings.Contains(lines[5], "(current)") {
		t.Errorf("fallback row should be marked (current): %q", lines[5])
	}
	if p.EffortCursor() != 2 {
		t.Errorf("effort cursor = %d, want 2 (high, the fallback model's tier)", p.EffortCursor())
	}
}

// TestPanelParksTheSliderOnThePersistedLabel pins that a persisted display
// label parks the slider on its own tier.
func TestPanelParksTheSliderOnThePersistedLabel(t *testing.T) {
	isolateCatalogue(t)
	for label, want := range map[string]int{
		"Gemini 3.8 Flash (Low)":    0,
		"Gemini 3.8 Flash (Medium)": 1,
		"Gemini 3.8 Flash (High)":   2,
	} {
		testHome(t, map[string]any{"model": label})
		p := newTestPanel(t)
		if got := p.EffortCursor(); got != want {
			t.Errorf("%s → effort cursor %d, want %d", label, got, want)
		}
	}
}

// TestPanelLayoutPadsCurrentColumn pins the byte geometry the capture
// comparison validates: 30-column pad then " (current)".
func TestPanelLayoutPadsCurrentColumn(t *testing.T) {
	isolateCatalogue(t)
	testHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	p := newTestPanel(t)

	lines := plainLines(p.Panel)
	if len(lines) < 6 {
		t.Fatalf("the panel rendered %d lines, want the header and at least one row", len(lines))
	}
	// "> " + 16-char label + pad to col 30 + " (current)"
	if lines[5] != "> Gemini 3.8 Flash             (current)" {
		t.Errorf("row = %q", lines[5])
	}
}

// ---- the effort slider ----------------------------------------------------

// TestPanelLeftRightAdvancesEffort pins the basic wrap semantics: from a
// starting medium position, → moves to high; one more → wraps to low (the
// cycle's defining behaviour); one ← wraps back to high.
func TestPanelLeftRightAdvancesEffort(t *testing.T) {
	isolateCatalogue(t)
	testHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	p := newTestPanel(t)
	if p.EffortCursor() != effortpanel.EffortMedium {
		t.Fatalf("seed effort cursor = %d, want %d (medium)",
			p.EffortCursor(), effortpanel.EffortMedium)
	}

	steps := []struct {
		key  rune
		want int
		why  string
	}{
		{tea.KeyRight, effortpanel.EffortHigh, "medium → right → high"},
		{tea.KeyRight, effortpanel.EffortLow, "high → right → low (wrap)"},
		{tea.KeyLeft, effortpanel.EffortHigh, "low → left → high (wrap)"},
		{tea.KeyLeft, effortpanel.EffortMedium, "high → left → medium"},
		{tea.KeyRight, effortpanel.EffortHigh, "medium → right → high"},
		{tea.KeyLeft, effortpanel.EffortMedium, "high → left → medium"},
	}
	for i, s := range steps {
		if handled, _ := p.HandleKey(tea.Key{Code: s.key}); !handled {
			t.Fatalf("step %d: the key %v was not handled", i, s.key)
		}
		if p.EffortCursor() != s.want {
			t.Errorf("step %d (%s): effort cursor = %d, want %d",
				i, s.why, p.EffortCursor(), s.want)
		}
	}
}

// TestPanelLeftRightPersistsToDisk is the load-bearing correctness test:
// each ←/→ must write the new catalogue slug to settings.json.
func TestPanelLeftRightPersistsToDisk(t *testing.T) {
	isolateCatalogue(t)
	dir := testHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	p := newTestPanel(t)

	expected := []struct {
		want string
		why  string
	}{
		{"gemini-3.8-flash-high", "medium → right → high"},
		{"gemini-3.8-flash-low", "high → right → low (wrap)"},
		{"gemini-3.8-flash-medium", "low → right → medium"},
		{"gemini-3.8-flash-high", "medium → right → high"},
		{"gemini-3.8-flash-low", "high → right → low (wrap again)"},
	}
	for i, s := range expected {
		p.HandleKey(tea.Key{Code: tea.KeyRight})
		if got := settingsModel(t, dir); got != s.want {
			t.Errorf("step %d (%s): persisted model = %q, want %q",
				i, s.why, got, s.want)
		}
	}

	wantReverse := []struct {
		want string
		why  string
	}{
		{"gemini-3.8-flash-high", "low → left → high (wrap)"},
		{"gemini-3.8-flash-medium", "high → left → medium"},
		{"gemini-3.8-flash-low", "medium → left → low"},
		{"gemini-3.8-flash-high", "low → left → high (wrap)"},
	}
	for i, s := range wantReverse {
		p.HandleKey(tea.Key{Code: tea.KeyLeft})
		if got := settingsModel(t, dir); got != s.want {
			t.Errorf("reverse step %d (%s): persisted model = %q, want %q",
				i, s.why, got, s.want)
		}
	}
}

// TestPanelLeftRightHandsOverTheLabel pins that the label the panel hands
// the session follows the persisted model on the very next frame.
func TestPanelLeftRightHandsOverTheLabel(t *testing.T) {
	isolateCatalogue(t)
	testHome(t, map[string]any{"model": "Gemini 3.8 Flash (Low)"})
	p := newTestPanel(t)

	p.HandleKey(tea.Key{Code: tea.KeyRight}) // low → medium
	if p.applied != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("after +1 from low: the label = %q, want %q",
			p.applied, "Gemini 3.8 Flash (Medium)")
	}

	p.HandleKey(tea.Key{Code: tea.KeyRight}) // medium → high
	if p.applied != "Gemini 3.8 Flash (High)" {
		t.Errorf("after +1 from medium: the label = %q, want %q",
			p.applied, "Gemini 3.8 Flash (High)")
	}

	p.HandleKey(tea.Key{Code: tea.KeyLeft}) // high → medium
	if p.applied != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("after -1 from high: the label = %q, want %q",
			p.applied, "Gemini 3.8 Flash (Medium)")
	}

	p.HandleKey(tea.Key{Code: tea.KeyRight}) // back to high
	p.HandleKey(tea.Key{Code: tea.KeyRight}) // high → low (wrap)
	if p.applied != "Gemini 3.8 Flash (Low)" {
		t.Errorf("after wrap to low: the label = %q, want %q",
			p.applied, "Gemini 3.8 Flash (Low)")
	}
}

// TestPanelLeftRightRerendersTheSlider verifies that the panel's frame
// produces a bar whose active marker `◉` position reflects the new cursor.
func TestPanelLeftRightRerendersTheSlider(t *testing.T) {
	isolateCatalogue(t)
	testHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	p := newTestPanel(t)

	at := func(level int) int {
		p.effortCursor = level
		bar := effortBarLine(p.Panel)
		if bar == "" {
			t.Fatalf("effort slider not rendered at level=%d", level)
		}
		return countConsecutiveBefore(bar)
	}

	if got := at(effortpanel.EffortMedium); got != 1 {
		t.Errorf("seeded medium: ●-before-◉ = %d, want 1", got)
	}
	p.HandleKey(tea.Key{Code: tea.KeyRight})
	if got := at(effortpanel.EffortHigh); got != 2 {
		t.Errorf("after right to high: ●-before-◉ = %d, want 2", got)
	}
	p.HandleKey(tea.Key{Code: tea.KeyRight}) // wrap
	if got := at(effortpanel.EffortLow); got != 0 {
		t.Errorf("after wrap to low: ●-before-◉ = %d, want 0", got)
	}
	p.HandleKey(tea.Key{Code: tea.KeyLeft}) // wrap
	if got := at(effortpanel.EffortHigh); got != 2 {
		t.Errorf("after wrap back to high: ●-before-◉ = %d, want 2", got)
	}
}

// TestPanelLeftRightIsInertOnAFixedRow pins that ←/→ is a no-op on the
// rows that offer a single effort: the panel must never overwrite the
// user's pinned model because their cursor wandered onto Claude Sonnet.
func TestPanelLeftRightIsInertOnAFixedRow(t *testing.T) {
	isolateCatalogue(t)
	dir := testHome(t, map[string]any{"model": "Claude Sonnet 4.6 (Thinking)"})
	p := newTestPanel(t)

	for _, cursor := range []int{1, 2, 3, 4} {
		p.SetCursor(cursor)
		beforeCursor := p.EffortCursor()
		beforeDisk := readSettings(t, dir)
		p.HandleKey(tea.Key{Code: tea.KeyRight})
		p.HandleKey(tea.Key{Code: tea.KeyLeft})
		if p.EffortCursor() != beforeCursor {
			t.Errorf("cursor=%d: ←/→ shifted the effort cursor from %d to %d (a fixed row must be inert)",
				cursor, beforeCursor, p.EffortCursor())
		}
		afterDisk := readSettings(t, dir)
		if !sameSettings(beforeDisk, afterDisk) {
			t.Errorf("cursor=%d: ←/→ mutated settings.json on a fixed row", cursor)
		}
	}
}

// TestPanelEffortCyclesOnTheTieredRowOnly pins that the slider wraps on
// the row that offers tiers and is inert everywhere else.
func TestPanelEffortCyclesOnTheTieredRowOnly(t *testing.T) {
	isolateCatalogue(t)
	dir := testHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	p := newTestPanel(t)

	p.HandleKey(tea.Key{Code: tea.KeyRight})
	if p.EffortCursor() != effortpanel.EffortHigh {
		t.Errorf("gemini right: effort cursor = %d, want %d", p.EffortCursor(), effortpanel.EffortHigh)
	}
	p.HandleKey(tea.Key{Code: tea.KeyRight}) // wrap high → low
	if p.EffortCursor() != effortpanel.EffortLow {
		t.Errorf("gemini right wrap: effort cursor = %d, want %d (wraps low)",
			p.EffortCursor(), effortpanel.EffortLow)
	}
	p.HandleKey(tea.Key{Code: tea.KeyLeft}) // wrap low → high
	if p.EffortCursor() != effortpanel.EffortHigh {
		t.Errorf("gemini left wrap: effort cursor = %d, want %d (wraps high)",
			p.EffortCursor(), effortpanel.EffortHigh)
	}

	// A row with a single effort is inert: the slider does not move and
	// settings.json keeps the model it had.
	p.SetCursor(1) // Claude Sonnet — a fixed effort
	before := readSettings(t, dir)
	p.HandleKey(tea.Key{Code: tea.KeyLeft})
	if p.EffortCursor() != effortpanel.EffortHigh {
		t.Errorf("a fixed row should be inert: effort cursor = %d, want %d",
			p.EffortCursor(), effortpanel.EffortHigh)
	}
	if !sameSettings(before, readSettings(t, dir)) {
		t.Error("a fixed row mutated settings.json")
	}
}

// sameSettings compares two settings documents member by member.
func sameSettings(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok {
			return false
		}
		sa, _ := va.(string)
		sb, _ := vb.(string)
		if sa != sb {
			return false
		}
	}
	return true
}

// ---- the selection --------------------------------------------------------

// TestPanelSelectPersistsTheSlugAndHandsOverTheLabel pins Enter's effect:
// the catalogue slug goes to settings.json while the session takes the
// display label, so the status row reads "Gemini 3.8 Flash".
func TestPanelSelectPersistsTheSlugAndHandsOverTheLabel(t *testing.T) {
	isolateCatalogue(t)
	dir := testHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	p := newTestPanel(t)

	p.HandleKey(tea.Key{Code: tea.KeyLeft}) // high → medium
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if got := settingsModel(t, dir); got != "gemini-3.8-flash-medium" {
		t.Errorf("persisted model = %q, want gemini-3.8-flash-medium", got)
	}
	if p.applied != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("the label = %q, want %q", p.applied, "Gemini 3.8 Flash (Medium)")
	}
	if p.closed != 1 {
		t.Errorf("the panel closed %d times, want 1", p.closed)
	}
}

// TestPanelArgonIsNotSelectable pins that the dim, slugless row is inert:
// Enter leaves settings.json alone.
func TestPanelArgonIsNotSelectable(t *testing.T) {
	isolateCatalogue(t)
	dir := testHome(t, map[string]any{"model": "Gemini 3.8 Flash (High)"})
	p := newTestPanel(t)

	// The Argon row's index is looked up rather than hardcoded: the
	// picker grows a row whenever a model is added, and a pinned index
	// would then point at a selectable model instead.
	argon := -1
	for i, r := range RowsView() {
		if r.Display == "Gemini 4 Argon" {
			argon = i
			break
		}
	}
	if argon < 0 {
		t.Fatal("the Argon row is missing from the picker")
	}
	p.SetCursor(argon)
	if label := p.selectModel(); label != "" {
		t.Errorf("selectModel on Argon = %q, want \"\"", label)
	}
	if got := settingsModel(t, dir); got != "Gemini 3.8 Flash (High)" {
		t.Errorf("settings model changed to %q; Argon must be inert", got)
	}
	// Enter must still be swallowed (it closes the panel).
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Error("Enter on Argon: the panel reported the key unhandled")
	}
}

// ---- the search box -------------------------------------------------------

// TestPanelSearchFiltersAndParksTheCursorOnTheFirstHit pins that the typed
// text filters the rows and the cursor lands on the first hit.
func TestPanelSearchFiltersAndParksTheCursorOnTheFirstHit(t *testing.T) {
	isolateCatalogue(t)
	p := newTestPanel(t)
	for _, r := range "cl" {
		if handled, _ := p.HandleKey(tea.Key{Text: string(r)}); !handled {
			t.Fatalf("the key %q was not handled by the /model panel", string(r))
		}
	}
	if p.Query() != "cl" {
		t.Fatalf("query = %q, want cl", p.Query())
	}
	if n := p.RowCount(); n != 2 {
		t.Fatalf("row count = %d, want 2 (the two Claude rows)", n)
	}
	if p.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0 (the first hit)", p.Cursor())
	}
	lines := strings.Join(plainLines(p.Panel), "\n")
	if !strings.Contains(lines, "Search: cl") {
		t.Fatalf("the panel does not show the query:\n%s", lines)
	}
	if strings.Contains(lines, "GPT-OSS") {
		t.Fatalf("the filter did not drop GPT-OSS:\n%s", lines)
	}
}

// TestPanelSearchPrintableKeysAreNotNavigation pins that a letter goes into
// the search box instead of moving the cursor.
func TestPanelSearchPrintableKeysAreNotNavigation(t *testing.T) {
	isolateCatalogue(t)
	p := newTestPanel(t)
	// "j" moves the cursor down in a plain list panel. The reference's
	// /model panel types it into the search box instead.
	if handled, _ := p.HandleKey(tea.Key{Text: "j"}); !handled {
		t.Fatal("the key j was not handled by the /model panel")
	}
	if p.Cursor() != 0 {
		t.Fatalf("cursor = %d, want 0 (j must not navigate)", p.Cursor())
	}
	if p.Query() != "j" {
		t.Fatalf("query = %q, want j", p.Query())
	}
}

// TestPanelSearchBackspaceRemovesOneCharacter pins the search box's own
// editing key.
func TestPanelSearchBackspaceRemovesOneCharacter(t *testing.T) {
	isolateCatalogue(t)
	p := newTestPanel(t)
	p.HandleKey(tea.Key{Text: "c"})
	p.HandleKey(tea.Key{Text: "l"})
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.Query() != "c" {
		t.Fatalf("query = %q, want c", p.Query())
	}
	p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if p.Query() != "" {
		t.Fatalf("query = %q, want the empty string", p.Query())
	}
}

// TestPanelSearchEscClearsTheQueryThenClosesThePanel pins the reference's
// two-stage Esc: a live query is cleared first, an empty box closes the
// panel.
func TestPanelSearchEscClearsTheQueryThenClosesThePanel(t *testing.T) {
	isolateCatalogue(t)
	p := newTestPanel(t)
	p.HandleKey(tea.Key{Text: "c"})
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.Query() != "" {
		t.Fatalf("query = %q, want the empty string", p.Query())
	}
	if p.closed != 0 {
		t.Fatalf("the panel closed %d times, want 0 (esc clears the search first)", p.closed)
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.closed != 1 {
		t.Fatalf("the panel closed %d times, want 1", p.closed)
	}
}

// TestPanelSearchNoMatchRendersTheReferenceLine pins the reference's
// empty-result line, byte for byte.
func TestPanelSearchNoMatchRendersTheReferenceLine(t *testing.T) {
	isolateCatalogue(t)
	p := newTestPanel(t)
	p.HandleKey(tea.Key{Text: "z"})
	p.HandleKey(tea.Key{Text: "z"})
	p.HandleKey(tea.Key{Text: "z"})
	lines := strings.Join(plainLines(p.Panel), "\n")
	want := "No models match \"zzz\""
	if !strings.Contains(lines, want) {
		t.Fatalf("the panel does not show the reference's empty-result line:\n%s", lines)
	}
	if !strings.Contains(lines, "Clear search") {
		t.Fatalf("the legend does not read \"Clear search\":\n%s", lines)
	}
}

// TestPanelSearchLegendReadsClearSearchWhileFiltering pins that the
// legend's last entry changes while a search is active.
func TestPanelSearchLegendReadsClearSearchWhileFiltering(t *testing.T) {
	isolateCatalogue(t)
	p := newTestPanel(t)
	p.HandleKey(tea.Key{Text: "c"})
	lines := strings.Join(plainLines(p.Panel), "\n")
	if !strings.Contains(lines, "Clear search") {
		t.Fatalf("the legend does not read \"Clear search\":\n%s", lines)
	}
	if strings.Contains(lines, "Go Back") {
		t.Fatalf("the legend still reads \"Go Back\":\n%s", lines)
	}
}

// ---- the row list ---------------------------------------------------------

// TestPanelRowCountFollowsTheRows pins that the row count follows the
// rows: two configured models put the picker at seven rows, and the
// count used to be a hard-coded 6, which left the seventh row
// unreachable.
func TestPanelRowCountFollowsTheRows(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://gateway.example/v1",
		"models": [
			{"id": "minimax-m3", "name": "MiniMax M3", "model": "Upstream-1"},
			{"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "Upstream-2"}
		]}}}`)
	t.Setenv("AGY_ADC_AUTH", "")
	t.Setenv("AGY_CLAUDE55_PROJECT", "")
	testHome(t, map[string]any{})
	p := newTestPanel(t)

	n := p.RowCount()
	if n != 7 {
		t.Fatalf("row count = %d, want 7 (five reference rows + two configured)", n)
	}
	// Walk to the last row the way a user does.
	for i := 0; i < n+3; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	if p.Cursor() != n-1 {
		t.Fatalf("cursor = %d, want %d (the last row must be reachable)", p.Cursor(), n-1)
	}
	rows := p.Rows()
	if rows[p.Cursor()].Slug != "minimax-m3.1-flash" {
		t.Fatalf("the last row is %q, want minimax-m3.1-flash", rows[p.Cursor()].Slug)
	}
}

// ---- a configured model that declares tiers -------------------------------

// TestPanelTieredModelIsOneRow pins the shape: the configured model is
// one row, not one row per tier.
func TestPanelTieredModelIsOneRow(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	testHome(t, map[string]any{"model": "MiniMax M3.1 Flash (Medium)"})
	p := newTestPanel(t)

	count := 0
	for _, r := range p.Rows() {
		if r.Slug == "minimax-m3.1-flash" {
			count++
			if r.Display != "MiniMax M3.1 Flash" {
				t.Errorf("the row's label = %q, want %q (the tier is on the slider)",
					r.Display, "MiniMax M3.1 Flash")
			}
			if len(r.Tiers) != 5 {
				t.Errorf("the row has %d tiers, want 5", len(r.Tiers))
			}
		}
	}
	if count != 1 {
		t.Errorf("the tiered model has %d rows, want 1", count)
	}
}

// TestPanelTieredModelShowsTheSlider pins that the slider appears on the
// configured model's row, with one marker per tier, and that the row
// carries the "(current)" annotation.
func TestPanelTieredModelShowsTheSlider(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	testHome(t, map[string]any{"model": "MiniMax M3.1 Flash (Medium)"})
	p := newTestPanel(t)

	p.SetCursor(rowIndex(t, p.Panel, "minimax-m3.1-flash"))
	lines := plainLines(p.Panel)
	joined := strings.Join(lines, "\n")

	// The row itself carries the model name and no tier in brackets.
	row := ""
	for _, ln := range lines {
		if strings.Contains(ln, "MiniMax M3.1 Flash") {
			row = ln
			break
		}
	}
	if row == "" {
		t.Fatalf("the panel has no row for the configured model:\n%s", joined)
	}
	if strings.Contains(row, "(Medium)") || strings.Contains(row, "(Xhigh)") {
		t.Errorf("the row still carries a tier in brackets: %q", row)
	}
	if !strings.Contains(row, "(current)") {
		t.Errorf("the running model's row lost its (current) marker: %q", row)
	}

	// The slider has one marker per tier.
	markers := strings.Count(joined, "◉") + strings.Count(joined, "○") + strings.Count(joined, "●")
	if markers != 5 {
		t.Errorf("the slider has %d markers, want 5:\n%s", markers, joined)
	}
	// The five tier words are on the slider's label row.
	for _, word := range []string{"low", "medium", "high", "xhigh", "max"} {
		if !strings.Contains(joined, word) {
			t.Errorf("the slider is missing the %q tier:\n%s", word, joined)
		}
	}
}

// TestPanelTieredModelCyclesEveryTier pins the ←/→ behaviour on a
// five-tier row: the slider wraps over the model's own tiers and the
// tier rides the persisted catalogue id.
func TestPanelTieredModelCyclesEveryTier(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	dir := testHome(t, map[string]any{"model": "MiniMax M3.1 Flash (Medium)"})
	p := newTestPanel(t)

	p.SetCursor(rowIndex(t, p.Panel, "minimax-m3.1-flash"))

	// The slider parks on the running model's own tier (medium).
	if p.EffortCursor() != 1 {
		t.Fatalf("the slider parked at %d, want 1 (medium)", p.EffortCursor())
	}

	want := []struct {
		key  rune
		slug string
	}{
		{tea.KeyRight, "minimax-m3.1-flash-high"},
		{tea.KeyRight, "minimax-m3.1-flash-xhigh"},
		{tea.KeyRight, "minimax-m3.1-flash-max"},
		{tea.KeyRight, "minimax-m3.1-flash-low"}, // wrap
		{tea.KeyLeft, "minimax-m3.1-flash-max"},  // wrap back
	}
	for i, s := range want {
		p.HandleKey(tea.Key{Code: s.key})
		if got := settingsModel(t, dir); got != s.slug {
			t.Errorf("step %d: persisted model = %q, want %q", i, got, s.slug)
		}
	}
	if p.applied != "MiniMax M3.1 Flash (Max)" {
		t.Errorf("the handed-over label = %q, want %q", p.applied, "MiniMax M3.1 Flash (Max)")
	}
}

// TestPanelTierlessModelHasNoSlider pins that a model that declares no
// tiers keeps no slider, and that ←/→ stays inert on it.
func TestPanelTierlessModelHasNoSlider(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	dir := testHome(t, map[string]any{"model": "MiniMax M3"})
	p := newTestPanel(t)

	p.SetCursor(rowIndex(t, p.Panel, "minimax-m3"))

	if row, ok := p.currentRow(); ok && row.HasEffortSlider() {
		t.Error("a model without tiers must not show the effort slider")
	}
	if bar := effortBarLine(p.Panel); bar != "" {
		t.Errorf("the panel drew an effort slider for a tierless row: %q", bar)
	}
	before := readSettings(t, dir)
	p.HandleKey(tea.Key{Code: tea.KeyRight})
	if !sameSettings(before, readSettings(t, dir)) {
		t.Error("←/→ mutated settings.json on a tierless row")
	}
}

func TestPanelPageKeys(t *testing.T) {
	testHome(t, map[string]any{"model": "gemini-2.5-pro"})
	p := newTestPanel(t)

	p.SetCursor(0)
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyPgDown})
	if !handled {
		t.Errorf("expected KeyPgDown to be handled")
	}
	if p.cursor <= 0 {
		t.Errorf("expected cursor to advance on KeyPgDown, got %d", p.cursor)
	}

	handled, _ = p.HandleKey(tea.Key{Code: tea.KeyPgUp})
	if !handled {
		t.Errorf("expected KeyPgUp to be handled")
	}
	if p.cursor != 0 {
		t.Errorf("expected cursor to return to 0 on KeyPgUp, got %d", p.cursor)
	}
}

// TestVisibleRowsForFormula pins the measured height formula against agy 1.4.12.
func TestVisibleRowsForFormula(t *testing.T) {
	for _, tc := range []struct {
		height int
		total  int
		want   int
	}{
		{15, 5, 1},
		{20, 5, 3},
		{24, 5, 5},
		{30, 10, 7},
		{40, 20, 7},
		{0, 20, 20},
		{-1, 15, 15},
	} {
		if got := VisibleRowsFor(tc.height, tc.total); got != tc.want {
			t.Errorf("VisibleRowsFor(%d, %d) = %d, want %d", tc.height, tc.total, got, tc.want)
		}
	}
}

// TestModelPickerWindowingPagination pins that the model picker caps its visible
// items when total items exceed visible items, rendering the exact reference
// "[start-end of total items]" indicator and keeping the display compact.
func TestModelPickerWindowingPagination(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://gateway.example/v1",
		"models": [
			{"id": "model-1", "name": "Model 1", "model": "Upstream-1"},
			{"id": "model-2", "name": "Model 2", "model": "Upstream-2"},
			{"id": "model-3", "name": "Model 3", "model": "Upstream-3"},
			{"id": "model-4", "name": "Model 4", "model": "Upstream-4"},
			{"id": "model-5", "name": "Model 5", "model": "Upstream-5"}
		]}}}`)
	t.Setenv("AGY_ADC_AUTH", "")
	t.Setenv("AGY_CLAUDE55_PROJECT", "")
	testHome(t, map[string]any{})
	p := newTestPanel(t)

	// Total rows = 5 reference + 5 configured = 10 rows.
	if p.RowCount() != 10 {
		t.Fatalf("row count = %d, want 10", p.RowCount())
	}

	// At height 20, VisibleRowsFor(20, 10) = 3 rows.
	lines := p.Lines(20)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "[1-3 of 10 items]") {
		t.Errorf("height 20 lines missing pagination indicator:\n%s", joined)
	}

	// At height 20, Effort slider should be omitted because height < 24.
	if strings.Contains(joined, "Effort") && strings.Contains(joined, "←/→ Effort") {
		t.Errorf("height 20 should hide effort slider:\n%s", joined)
	}

	// Move cursor down past row 2 to trigger window scrolling.
	p.SetCursor(4) // 5th row (0-indexed)
	scrolledLines := p.Lines(20)
	scrolledJoined := strings.Join(scrolledLines, "\n")
	if !strings.Contains(scrolledJoined, "[3-5 of 10 items]") {
		t.Errorf("after moving cursor, pagination indicator not updated:\n%s", scrolledJoined)
	}

	// Reset cursor to 0. At height 40, max visible is 7. Total 10 rows -> [1-7 of 10 items].
	p.SetCursor(0)
	lines40 := p.Lines(40)
	joined40 := strings.Join(lines40, "\n")
	if !strings.Contains(joined40, "[1-7 of 10 items]") {
		t.Errorf("height 40 lines missing [1-7 of 10 items]:\n%s", joined40)
	}
	// At height 40, Effort slider should be present for Gemini row (cursor at 0).
	if !strings.Contains(joined40, "Effort") || !strings.Contains(joined40, "←/→") {
		t.Errorf("height 40 cursor on Gemini row should show effort slider:\n%s", joined40)
	}
}

func Test_ModelPickerPureMessagePassing(t *testing.T) {
	// Zero seams configured: pure Elm message passing
	p := New(Seams{})

	// Esc on empty query produces DismissCmd
	handled, cmd := p.Update(tea.Key{Code: tea.KeyEsc})
	if !handled || cmd == nil {
		t.Fatal("expected Esc to be handled with tea.Cmd")
	}
	if _, ok := cmd().(DismissedMsg); !ok {
		t.Fatalf("expected DismissedMsg, got %#v", cmd())
	}

	// Press Enter to select current model row
	handled, cmd = p.Update(tea.Key{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatal("expected Enter to be handled with tea.Cmd")
	}
	msg := cmd()
	// Since tea.Batch produces a batch msg, or single msg:
	foundSelection := false
	if sel, ok := msg.(ModelSelectedMsg); ok {
		if sel.Label != "" {
			foundSelection = true
		}
	} else if batch, ok := msg.(tea.BatchMsg); ok {
		for _, bCmd := range batch {
			if bCmd != nil {
				if sel, ok := bCmd().(ModelSelectedMsg); ok && sel.Label != "" {
					foundSelection = true
				}
			}
		}
	}
	if !foundSelection {
		t.Fatalf("expected ModelSelectedMsg in batch or directly, got %#v", msg)
	}
}

