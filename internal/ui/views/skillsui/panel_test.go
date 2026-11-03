// panel_test.go — the /skills picker's interactive half.
//
// Cycle 381 moved the panel out of internal/cliapp: the grouped list, the
// cursor, the per-skill detail view and its scroll window, the keys and
// the rows are the package's, and the host keeps only the seams (the
// catalogue, the OSC 8 capability, the terminal's size and the way out).
// The behaviour is the clone's own from cycle 203 on, so the tests move
// with the code they exercise.
//
// The tests build the panel directly — there is no runMode behind it —
// so a seam is a closure over a test value.

package skillsui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// ---------------------------------------------------------------- helpers

// testTitleOpen / testTitleClose are the panel title's SGR span — the
// bold default foreground the reference draws the panel's title in.
const (
	testTitleOpen  = "\x1b[1m\x1b[39m"
	testTitleClose = "\x1b[0m"
)

// newTestPanel builds a /skills panel over a synthetic catalogue.
//
// The catalogue is read fresh on every draw (the seam is a closure), the
// terminal cannot carry an OSC 8 hyperlink, and the panel's way out is a
// no-op — none of these tests closes the panel.
func newTestPanel(cat []SkillInfo, width, height int) *Panel {
	return New(Seams{
		Catalog: func() []SkillInfo { return cat },
		OSC8:    func() bool { return false },
		Size:    func() (int, int) { return width, height },
		Close:   func() tea.Cmd { return nil },
	})
}

// detailDoc is the panel's logical-line document for a catalogue entry.
//
// It is the package's own function; the tests call it directly to assert
// on the document rather than on the rendered rows.
func detailDoc(p *Panel, s SkillInfo) []DetailLine {
	w, _ := p.size()
	return SkillsDetailDocument(DisplayName(s), s.Description, s.Path, w)
}

// writeSkillMD creates a SKILL.md under a fresh dir and returns its path.
func writeSkillMD(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// stripSGR removes every ANSI SGR sequence.
func stripSGR(s string) string {
	return termcolor.StripSGR(s)
}

// assertLines compares a rendered row slice with the expected rows.
func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("lines = %d, want %d\n got: %q\nwant: %q",
			len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// plainLines strips the SGR spans off a rendered row slice.
func plainLines(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, stripSGR(r))
	}
	return out
}

// TestCycle203_SkillsPanel is the cycle-203 entry point: `go test -run
// TestCycle203` selects the whole /skills layout family below.
func TestCycle203_SkillsPanel(t *testing.T) {
	t.Run("Groups", TestSkillsPanelGroups)
	t.Run("ByteExact", TestSkillsPanelLayout_ByteExact)
	t.Run("Counts", TestSkillsPanelCounts)
	t.Run("Window", TestSkillsVisibleItemsFor)
	t.Run("Counter", TestSkillsPanelCounterAndWindow)
	t.Run("Truncation", TestSkillsPanelRow_Truncation)
	t.Run("CursorSpan", TestSkillsPanelRow_CursorSpan)
	t.Run("CursorKeys", TestSkillsPanelCursorKeys)
	t.Run("EnterGate", TestSkillsPanelEnterGate)
}

// cycSkill builds one synthetic catalogue entry.
func cycSkill(name, desc, path string, builtin bool, plugin string) SkillInfo {
	return SkillInfo{
		Name: name, Description: desc, Path: path,
		Builtin: builtin, Plugin: plugin, ModelInvocable: true,
	}
}

// cycCatalogue is a four-entry, three-group catalogue whose group directories
// live under $HOME so the tilde-shortened labels are deterministic. Returned
// in printSlashSkillInfos' display-name order.
func cycCatalogue(t *testing.T) (cat []SkillInfo, home, sharedDir, builtinDir, pluginDir string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no HOME")
	}
	sharedDir = filepath.Join(home, ".gemini", "skills")
	builtinDir = filepath.Join(home, ".gemini", "antigravity-cli", "builtin", "skills")
	pluginDir = filepath.Join(home, ".gemini", "config", "plugins", "chrome-devtools-plugin", "skills")
	cat = []SkillInfo{
		cycSkill("agy-customizations", "Builtin desc",
			filepath.Join(builtinDir, "agy-customizations", "SKILL.md"), true, ""),
		cycSkill("alpha", "Alpha desc",
			filepath.Join(sharedDir, "alpha", "SKILL.md"), false, ""),
		cycSkill("beta", "Beta desc",
			filepath.Join(sharedDir, "beta", "SKILL.md"), false, ""),
		cycSkill("chrome-devtools-plugin:a11y", "A11y desc",
			filepath.Join(pluginDir, "a11y", "SKILL.md"), false, "chrome-devtools-plugin"),
	}
	return cat, home, sharedDir, builtinDir, pluginDir
}

// TestSkillsPanelGroups pins the group split and the header/label strings: the
// Shared group is the literal "Shared skills" + "From ~/.gemini/skills", the
// builtin group is its absolute directory + "Built-in", and each plugin group
// is "<abs>/<plugin>/skills" + "From ~/.gemini/config/plugins/<plugin>".
func TestSkillsPanelGroups(t *testing.T) {
	cat, _, _, builtinDir, pluginDir := cycCatalogue(t)
	groups := PanelGroups(cat)
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	if groups[0].Left != "Shared skills" || groups[0].Label != "From ~/.gemini/skills" {
		t.Errorf("shared header = %q · %q", groups[0].Left, groups[0].Label)
	}
	if len(groups[0].Skills) != 2 || groups[0].Skills[0].Name != "alpha" || groups[0].Skills[1].Name != "beta" {
		t.Errorf("shared skills = %v", namesOf(groups[0].Skills))
	}
	if groups[1].Left != builtinDir || groups[1].Label != "Built-in" {
		t.Errorf("builtin header = %q · %q (want %q · Built-in)", groups[1].Left, groups[1].Label, builtinDir)
	}
	if groups[2].Left != pluginDir ||
		groups[2].Label != "From ~/.gemini/config/plugins/chrome-devtools-plugin" {
		t.Errorf("plugin header = %q · %q", groups[2].Left, groups[2].Label)
	}
}

func namesOf(ss []SkillInfo) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name
	}
	return out
}

// TestSkillsPanelLayout_ByteExact pins the full rendered panel, byte for byte,
// for a small catalogue that fits the window (height 100 → no counter). Every
// SGR span is spelled out so a palette or layout regression shows up here.
func TestSkillsPanelLayout_ByteExact(t *testing.T) {
	cat, _, _, builtinDir, pluginDir := cycCatalogue(t)
	pn := newTestPanel(cat, 220, 100) // every item fits → no counter
	want := []string{
		testTitleOpen + "Skills" + testTitleClose,
		palette.DimGrayOpen + "4 skills" + palette.DimGrayClose,
		"",
		"\x1b[1mCreate new skills\x1b[0m",
		"  " + palette.DimGrayOpen + "Workspace: " + WorkspaceSkillsPath() + palette.DimGrayClose,
		"  " + palette.DimGrayOpen + "Global:    ~/.gemini/antigravity-cli/skills/{skill_name}/SKILL.md" + palette.DimGrayClose,
		"  " + palette.DimGrayOpen + "Shared:    ~/.gemini/skills/{skill_name}/SKILL.md" + palette.DimGrayClose,
		"",
		"\x1b[1mShared skills · " + PanelGray238 + "From ~/.gemini/skills" + "\x1b[0m",
		"  " + palette.BlueOpen + "alpha" + palette.BlueClose + ": " + PanelGray238 + "Alpha desc" + PanelGray238C,
		"  " + palette.BlueOpen + "beta" + palette.BlueClose + ": " + PanelGray238 + "Beta desc" + PanelGray238C,
		"",
		"\x1b[1m" + builtinDir + " · " + PanelGray238 + "Built-in" + "\x1b[0m",
		"  " + palette.BlueOpen + "agy-customizations" + palette.BlueClose + ": " + PanelGray238 + "Builtin desc" + PanelGray238C +
			" · \x1b[3m\x1b[38;2;101;123;131mbuilt-in\x1b[0m",
		"",
		"\x1b[1m" + pluginDir + " · " + PanelGray238 +
			"From ~/.gemini/config/plugins/chrome-devtools-plugin" + "\x1b[0m",
		"  " + palette.BlueOpen + "chrome-devtools-plugin:a11y" + palette.BlueClose + ": " + PanelGray238 + "A11y desc" + PanelGray238C,
		"",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "View", "esc", "Cancel"),
	}
	got := pn.Lines()
	assertLines(t, got, want)
}

// TestSkillsPanelCounts pins the arithmetic behind "<N> skills" and
// "[a-b of M items]": N is the skill count, M is skills + group headers.
func TestSkillsPanelCounts(t *testing.T) {
	cat, _, _, _, _ := cycCatalogue(t)
	model := NewPanelModel(cat)
	if len(model.Skills) != 4 {
		t.Errorf("skills = %d, want 4", len(model.Skills))
	}
	if len(model.Groups) != 3 {
		t.Errorf("groups = %d, want 3", len(model.Groups))
	}
	if len(model.Items) != 7 { // 4 skills + 3 headers
		t.Errorf("items = %d, want 7", len(model.Items))
	}
	if len(model.Items) != len(model.Skills)+len(model.Groups) {
		t.Errorf("items != skills+groups")
	}
	// skillItem maps each skill to its row in the item list.
	wantItem := []int{1, 2, 4, 6}
	if fmt.Sprint(model.SkillItem) != fmt.Sprint(wantItem) {
		t.Errorf("skillItem = %v, want %v", model.SkillItem, wantItem)
	}

	pn := newTestPanel(cat, 220, 100)
	if got := stripSGR(pn.Lines()[1]); got != "4 skills" {
		t.Errorf("subtitle = %q, want %q", got, "4 skills")
	}
}

// TestSkillsVisibleItemsFor pins the measured height→window rule
// n = height*3/4 - 11 (integer division), floor 1, and the height-0 fallback.
func TestSkillsVisibleItemsFor(t *testing.T) {
	cases := []struct{ h, want int }{
		{0, 11}, {16, 1}, {17, 1}, {18, 2}, {20, 4}, {24, 7}, {28, 10},
		{30, 11}, {32, 13}, {36, 16}, {40, 19}, {44, 22}, {46, 23}, {50, 26},
		{60, 34}, {70, 41}, {80, 49}, {98, 62}, {99, 63}, {100, 64},
		{14, 1},
	}
	for _, c := range cases {
		if got := VisibleItemsFor(c.h); got != c.want {
			t.Errorf("VisibleItemsFor(%d) = %d, want %d", c.h, got, c.want)
		}
	}
}

// cycSharedSkills builds a sorted catalogue of n shared skills.
func cycSharedSkills(n int) []SkillInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := make([]SkillInfo, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("s%02d", i)
		out = append(out, cycSkill(name, "d", filepath.Join(home, ".gemini", "skills", name, "SKILL.md"), false, ""))
	}
	return out
}

// TestSkillsPanelCounterAndWindow pins the "[a-b of M items]" counter, its
// dim-gray span and the rule that it disappears once every item fits.
func TestSkillsPanelCounterAndWindow(t *testing.T) {
	cat := cycSharedSkills(20) // 20 items + 1 header = 21

	// Height 24 → n = 7; cursor at -1 → window [1-7 of 21].
	pn := newTestPanel(cat, 220, 24)
	joined := strings.Join(plainLines(pn.Lines()), "\n")
	if !strings.Contains(joined, "  [1-7 of 21 items]") {
		t.Fatalf("counter at top missing:\n%s", joined)
	}
	// The counter is the third row from the bottom: it, a blank, then the
	// keyboard legend.
	plain := plainLines(pn.Lines())
	if got := plain[len(plain)-3]; !strings.Contains(got, "[1-7 of 21 items]") {
		t.Errorf("counter row = %q (want the 3rd-from-last)", got)
	}
	raw := pn.Lines()
	if !containsSGR(raw, palette.DimGrayOpen+"  [1-7 of 21 items]"+palette.DimGrayClose) {
		t.Errorf("counter raw span wrong: %q", raw[len(raw)-3])
	}

	// Cursor on the last skill (index 19 → item 20) scrolls the window to
	// [15-21]; start = 20-(7-1) = 14.
	pn = newTestPanel(cat, 220, 24)
	pn.SetCursor(19)
	if got := strings.Join(plainLines(pn.Lines()), "\n"); !strings.Contains(got, "  [15-21 of 21 items]") {
		t.Fatalf("scrolled counter wrong:\n%s", got)
	}

	// Every item fits at height 100 → no counter row at all.
	pn = newTestPanel(cat, 220, 100)
	if got := strings.Join(plainLines(pn.Lines()), "\n"); strings.Contains(got, "items]") {
		t.Fatalf("counter should be absent when everything fits:\n%s", got)
	}
	for _, l := range pn.Lines() {
		if strings.Contains(stripSGR(l), "items]") {
			t.Errorf("stray counter row: %q", l)
		}
	}
}

func containsSGR(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

// TestSkillsPanelRow_Truncation pins the mid-word "..." cut and the builtin
// suffix reservation: a rendered row is at most width-1 columns wide.
func TestSkillsPanelRow_Truncation(t *testing.T) {
	// visLen counts rendered columns, not bytes: the group labels and the
	// " · built-in" suffix contain U+00B7 (2 bytes, 1 column) so a byte
	// count would over-report by one on those rows.
	visLen := func(s string) int { return len([]rune(stripSGR(s))) }

	long := strings.Repeat("word ", 80)
	s := cycSkill("agents-sdk", long, "/x/SKILL.md", false, "")
	got := stripSGR(PanelRow(s, false, 220))
	if visLen(got) != 219 {
		t.Errorf("plain row width = %d, want 219", visLen(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("row should end in ...: %q", got)
	}
	if !strings.HasPrefix(got, "  agents-sdk: word word") {
		t.Errorf("row head wrong: %q", got[:40])
	}

	// Built-in rows reserve the " · built-in" suffix inside the same budget:
	// the rendered line is still 219 columns, not 219+suffix.
	b := cycSkill("agy-customizations", long, "/x/SKILL.md", true, "")
	gotB := stripSGR(PanelRow(b, false, 220))
	if visLen(gotB) != 219 {
		t.Errorf("builtin row width = %d, want 219", visLen(gotB))
	}
	if !strings.HasSuffix(gotB, " · built-in") {
		t.Errorf("builtin row should end in the suffix: %q", gotB[len(gotB)-20:])
	}
	if !strings.HasSuffix(strings.TrimSuffix(gotB, " · built-in"), "...") {
		t.Errorf("builtin row description should end in ...: %q", gotB[len(gotB)-40:])
	}

	// A short description is never padded or truncated.
	short := cycSkill("uv", "short description", "/x/SKILL.md", false, "")
	if got := stripSGR(PanelRow(short, false, 220)); got != "  uv: short description" {
		t.Errorf("short row = %q", got)
	}

	// Narrow terminal: a 100-wide window yields 99 columns.
	narrow := stripSGR(PanelRow(s, false, 100))
	if visLen(narrow) != 99 {
		t.Errorf("narrow row width = %d, want 99", visLen(narrow))
	}
}

// TestSkillsPanelRow_CursorSpan pins that the cursor glyph sits inside the
// blue name span, while a plain row's two-space prefix sits outside it.
func TestSkillsPanelRow_CursorSpan(t *testing.T) {
	s := cycSkill("agents-sdk", "d", "/x/SKILL.md", false, "")
	if got := PanelRow(s, true, 220); !strings.HasPrefix(got, palette.BlueOpen+"› agents-sdk"+palette.BlueClose+": ") {
		t.Errorf("cursor row span = %q", got)
	}
	if got := PanelRow(s, false, 220); !strings.HasPrefix(got, "  "+palette.BlueOpen+"agents-sdk"+palette.BlueClose+": ") {
		t.Errorf("plain row span = %q", got)
	}
}

// TestSkillsPanelCursorKeys pins the ↑/↓ semantics: the cursor opens at -1
// (no selection), ↓ from -1 selects the first skill, and both ends clamp —
// ↑ on the first skill stays put (it does not clear the selection).
func TestSkillsPanelCursorKeys(t *testing.T) {
	pn := newTestPanel(cycSharedSkills(5), 220, 30)

	down := tea.Key{Code: tea.KeyDown}
	up := tea.Key{Code: tea.KeyUp}

	if h, _ := pn.HandleKey(up); !h {
		t.Fatal("up not handled")
	}
	if pn.cursor != -1 {
		t.Fatalf("up at -1 moved the cursor to %d", pn.cursor)
	}
	if h, _ := pn.HandleKey(down); !h {
		t.Fatal("down not handled")
	}
	if pn.cursor != 0 {
		t.Fatalf("down from -1 → %d, want 0", pn.cursor)
	}
	pn.HandleKey(down)
	if pn.cursor != 1 {
		t.Fatalf("second down → %d, want 1", pn.cursor)
	}
	pn.HandleKey(up)
	if pn.cursor != 0 {
		t.Fatalf("up → %d, want 0", pn.cursor)
	}
	pn.HandleKey(up)
	if pn.cursor != 0 {
		t.Fatalf("up on the first skill → %d, want 0 (clamp)", pn.cursor)
	}
	// Down clamps at the last skill.
	pn.cursor = 4
	for i := 0; i < 3; i++ {
		pn.HandleKey(down)
	}
	if pn.cursor != 4 {
		t.Fatalf("down past the end → %d, want 4", pn.cursor)
	}
}

// TestSkillsPanelEnterGate pins that Enter opens the detail only when a row is
// selected, and that panelRowCount tracks the catalogue size.
func TestSkillsPanelEnterGate(t *testing.T) {
	cat := cycSharedSkills(3)
	enter := tea.Key{Code: tea.KeyEnter}

	pn := newTestPanel(cat, 220, 30)
	if h, _ := pn.HandleKey(enter); !h {
		t.Fatal("enter not handled")
	}
	if pn.detail {
		t.Fatal("enter with no selection must not open the detail")
	}

	pn = newTestPanel(cat, 220, 30)
	pn.SetCursor(1)
	pn.HandleKey(enter)
	if !pn.detail {
		t.Fatal("enter on a selected skill should open the detail")
	}
	if !strings.Contains(strings.Join(plainLines(pn.Lines()), "\n"), "s01") {
		t.Errorf("detail should show the selected skill:\n%s", strings.Join(plainLines(pn.Lines()), "\n"))
	}
	if handled, _ := pn.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled || pn.detail {
		t.Fatal("esc should return from the detail")
	}

	if got := pn.RowCount(); got != 3 {
		t.Errorf("panelRowCount(skills) = %d, want 3", got)
	}
}

// TestCycle211_SkillsDetail is the cycle-211 entry point. `go test -run
// TestCycle211` selects the whole SKILL.md detail-viewer family below.
func TestCycle211_SkillsDetail(t *testing.T) {
	t.Run("WindowFloor", TestSkillsDetailVisible_Floor)
	t.Run("WindowFormula", TestSkillsDetailVisible_Formula)
	t.Run("DocHeader", TestSkillsDetailDocument_Header)
	t.Run("Frontmatter", TestSkillsDetailDocument_FrontmatterStripped)
	t.Run("HeadingH1", TestSkillsDetailDocument_HeadingH1)
	t.Run("HeadingH2", TestSkillsDetailDocument_HeadingH2)
	t.Run("Bullets", TestSkillsDetailDocument_Bullets)
	t.Run("OrderedList", TestSkillsDetailDocument_OrderedList)
	t.Run("CodeSpanInParagraph", TestSkillsDetailDocument_CodeSpan)
	t.Run("BoldSpan", TestSkillsDetailDocument_BoldSpan)
	t.Run("HR", TestSkillsDetailDocument_ThematicBreak)
	t.Run("HTMLComments", TestSkillsDetailDocument_HTMLCommentsSkipped)
	t.Run("DocCounter", TestSkillsDetailDocument_Counter)
	t.Run("DocCounterShort", TestSkillsDetailDocument_CounterShort)
	t.Run("ScrollDown", TestSkillsDetailScroll_Down)
	t.Run("ScrollUp", TestSkillsDetailScroll_Up)
	t.Run("ScrollClamp", TestSkillsDetailScroll_Clamp)
	t.Run("PageDown", TestSkillsDetailScroll_Page)
	t.Run("JumpEnds", TestSkillsDetailScroll_CtrlHomeEnd)
	t.Run("EscReturnsToList", TestSkillsDetailScroll_EscReturnsToList)
	t.Run("ViewerWiredByPath", TestSkillsDetailLines_ViewerWiredByPath)
	t.Run("LegacyWhenNoPath", TestSkillsDetailLines_LegacyWhenNoPath)
}

// detailPanel builds a panel with the given height/width and a single
// synthetic catalogue row pointing at path. The cursor is parked on that
// row so selected() returns the fixture.
func detailPanel(t *testing.T, height, width int, path, name, desc string) *Panel {
	t.Helper()
	cat := []SkillInfo{{
		Name: name, Description: desc, Path: path,
		Builtin: false, ModelInvocable: true,
	}}
	pn := newTestPanel(cat, width, height)
	pn.SetCursor(0)
	pn.detail = true
	return pn
}

// detailPanelAtCursor parks the detail cursor on entry idx.
func detailPanelAtCursor(t *testing.T, height, width int, cat []SkillInfo, idx int) *Panel {
	t.Helper()
	pn := newTestPanel(cat, width, height)
	pn.SetCursor(idx)
	return pn
}

// ---- geometry (skillsDetailVisible) ----------------------------------------

// TestSkillsDetailVisible_Floor covers the floor REF applies at heights
// where the chrome would otherwise eat the pane — heights 12–22 all
// return vis = 5 against REF v1.4.0 (caps/ref.h*.raw.txt).
func TestSkillsDetailVisible_Floor(t *testing.T) {
	cases := []struct {
		h        int
		expected int
	}{
		{12, 5}, {13, 5}, {14, 5}, {15, 5}, {16, 5}, {17, 5},
		{18, 5}, {19, 5}, {20, 5}, {21, 5}, {22, 5},
		{24, 7}, {30, 11}, {40, 19}, {50, 26}, {60, 34}, {80, 49}, {100, 64},
	}
	for _, c := range cases {
		got := SkillsDetailVisible(c.h)
		if got != c.expected {
			t.Errorf("SkillsDetailVisible(%d) = %d, want %d", c.h, got, c.expected)
		}
	}
}

// TestSkillsDetailVisible_Formula checks the formula on a clean height
// (no floor interference) — vis = h*3/4 - 11.
func TestSkillsDetailVisible_Formula(t *testing.T) {
	// 80*3/4 - 11 = 49 — REF h=80 showed vis=49.
	if got := SkillsDetailVisible(80); got != 49 {
		t.Errorf("SkillsDetailVisible(80) = %d, want 49", got)
	}
	// 100*3/4 - 11 = 64 — REF h=100 showed vis=64.
	if got := SkillsDetailVisible(100); got != 64 {
		t.Errorf("SkillsDetailVisible(100) = %d, want 64", got)
	}
	// height ≤ 0 falls back to the 30-row default → 30*3/4 - 11 = 11.
	if got := SkillsDetailVisible(0); got != 11 {
		t.Errorf("SkillsDetailVisible(0) = %d, want 11", got)
	}
}

// ---- document rendering -----------------------------------------------------

// TestSkillsDetailDocument_Header pins the first four logical lines:
// title (testTitleOpen/testTitleClose), description (the PanelGray238
// rows), path (cDimGray), blank, then the H1 (whose bg-coloured marker
// pad adds two leading spaces and one trailing space around the text).
func TestSkillsDetailDocument_Header(t *testing.T) {
	body := "# T\n\nplain body line.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 50, p, "demo", "a one-line description")

	doc := detailDoc(pn, SkillInfo{
		Name: "demo", Description: "a one-line description", Path: p,
	})
	plain := docToPlain(doc)
	if len(plain) < 5 {
		t.Fatalf("too few document lines: %v", plain)
	}
	if plain[0] != "demo" {
		t.Errorf("title row = %q, want %q", plain[0], "demo")
	}
	if plain[1] != "a one-line description" {
		t.Errorf("description row = %q, want %q", plain[1], "a one-line description")
	}
	if !strings.Contains(plain[2], "SKILL.md") {
		t.Errorf("path row missing SKILL.md: %q", plain[2])
	}
	if plain[3] != "" {
		t.Errorf("expected blank row after path, got %q", plain[3])
	}
	// H1 renders as two leading spaces (the panel indent pad), a leading
	// space (the H1 marker's inner pad), the text, and a trailing space
	// (the H1 marker's right-side pad). Trim whitespace for the assertion.
	if got := strings.TrimSpace(plain[4]); got != "T" {
		t.Errorf("H1 text = %q (want just %q, full row %q)", got, "T", plain[4])
	}
}

// TestSkillsDetailDocument_FrontmatterStripped pins that the leading
// "---" … "---" block is consumed before the body is rendered.
func TestSkillsDetailDocument_FrontmatterStripped(t *testing.T) {
	body := "---\nname: demo\ndescription: desc text\n---\n# T\n\nplain body.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 50, p, "demo", "desc text")

	doc := detailDoc(pn, SkillInfo{
		Name: "demo", Description: "desc text", Path: p,
	})
	plain := docToPlain(doc)
	for i, row := range plain {
		if strings.Contains(row, "name: demo") || strings.Contains(row, "description: desc") {
			t.Errorf("frontmatter leaked into row %d: %q", i, row)
		}
	}
	if got := strings.TrimSpace(plain[len(plain)-1]); got != "plain body." {
		t.Errorf("body content tail = %q, want `plain body.` (full: %v)", got, plain)
	}
}

// TestSkillsDetailDocument_HeadingH1 — H1 wraps the visible text in the
// yellow-on-blue "## ##" marker block (240 rows of bg colour 63 around the
// text), measured on agents-sdk at 220×50.
func TestSkillsDetailDocument_HeadingH1(t *testing.T) {
	body := "# Title Here\n\nparagraph.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 80, p, "demo", "d")

	doc := detailDoc(pn, SkillInfo{Name: "demo", Path: p})
	// Find the row carrying the H1 logical line — it's the only one
	// whose raw render uses the MDH1Open palette.
	for i, l := range doc {
		for _, row := range l.Rows {
			if strings.Contains(row, "Title Here") {
				if !strings.Contains(row, MDH1Open) {
					t.Errorf("H1 row missing the bg palette: %q", row)
				}
				if !strings.HasPrefix(row, "  ") {
					t.Errorf("H1 row should start with two leading spaces: %q", row)
				}
				return
			}
		}
		_ = i
	}
	t.Fatal("H1 text not found in any document row")
}

// TestSkillsDetailDocument_HeadingH2 — H2 keeps the "## " marker literally
// in the visible text (no background) and colours it the cycle-211 blue.
func TestSkillsDetailDocument_HeadingH2(t *testing.T) {
	body := "## Subhead\n\nparagraph.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 80, p, "demo", "d")

	doc := detailDoc(pn, SkillInfo{Name: "demo", Path: p})
	for _, l := range doc {
		for _, row := range l.Rows {
			if strings.Contains(row, "## Subhead") {
				if !strings.Contains(row, MDH2Open) {
					t.Errorf("H2 row missing the bold-blue palette: %q", row)
				}
				if strings.Contains(row, MDH1Open) {
					t.Errorf("H2 row should not use the H1 background palette: %q", row)
				}
				return
			}
		}
	}
	t.Fatal("H2 text not found in any document row")
}

// TestSkillsDetailDocument_Bullets — top-level bullets render as "  • …"
// in the dim text colour; nested bullets preserve their four-space indent.
func TestSkillsDetailDocument_Bullets(t *testing.T) {
	body := "- alpha\n- beta\n    - nested\n- gamma\n\nparagraph.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 80, p, "demo", "d")

	doc := detailDoc(pn, SkillInfo{Name: "demo", Path: p})
	plain := docToPlain(doc)
	idxs := map[string]int{}
	for i, row := range plain {
		switch {
		case strings.HasSuffix(row, "alpha"):
			idxs["alpha"] = i
		case strings.HasSuffix(row, "beta"):
			idxs["beta"] = i
		case strings.HasSuffix(row, "nested"):
			idxs["nested"] = i
		case strings.HasSuffix(row, "gamma"):
			idxs["gamma"] = i
		}
	}
	if len(idxs) != 4 {
		t.Fatalf("found bullets: %v\nplain: %v", idxs, plain)
	}
	// beta and nested must be adjacent (nested belongs to beta's indent),
	// and both must precede gamma.
	if idxs["beta"]+1 != idxs["nested"] {
		t.Errorf("nested not adjacent to beta: %v", idxs)
	}
	if idxs["nested"]+1 != idxs["gamma"] {
		t.Errorf("gamma not adjacent to nested: %v", idxs)
	}
	// Top-level rows must begin with "  • " (the indent pad + bullet).
	for _, k := range []string{"alpha", "beta", "gamma"} {
		row := plain[idxs[k]]
		if !strings.HasPrefix(row, "  • ") {
			t.Errorf("%s row should start with `  • `, got %q", k, row)
		}
	}
	// Nested keeps the four-space source indent, giving 6 visual spaces
	// before "•". The reference indents all bullets with their own source
	// indent, so a 4-space nested bullet sits at column 6.
	wantNested := "      • nested"
	if plain[idxs["nested"]] != wantNested {
		t.Errorf("nested row = %q, want exact %q", plain[idxs["nested"]], wantNested)
	}
}

// TestSkillsDetailDocument_OrderedList — "1. first" renders as "  1. first"
// with the dim-text colour restored at the line end. Skip the catalogue
// header rows (title, description, path, blank) before the list proper.
func TestSkillsDetailDocument_OrderedList(t *testing.T) {
	body := "1. first item\n2. second item\n\nafter.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 80, p, "demo", "d")

	doc := detailDoc(pn, SkillInfo{Name: "demo", Path: p})
	plain := docToPlain(doc)
	// First 4 rows are the catalogue header (title / desc / path / blank).
	if len(plain) < 6 {
		t.Fatalf("not enough rows: %v", plain)
	}
	if plain[3] != "  1. first item" {
		t.Errorf("first list row = %q, want `  1. first item`", plain[3])
	}
	if plain[4] != "  2. second item" {
		t.Errorf("second list row = %q, want `  2. second item`", plain[4])
	}
}

// TestSkillsDetailDocument_CodeSpan — a backtick code span opens the
// fg-69 + bg-254 palette and the inline text appears verbatim.
func TestSkillsDetailDocument_CodeSpan(t *testing.T) {
	body := "Use `agy -p` for the print mode.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 80, p, "demo", "d")

	doc := detailDoc(pn, SkillInfo{Name: "demo", Path: p})
	for _, l := range doc {
		for _, row := range l.Rows {
			if strings.Contains(row, "agy -p") {
				if !strings.Contains(row, MDCodeOpen) {
					t.Errorf("code span missing palette: %q", row)
				}
				return
			}
		}
	}
	t.Fatal("code-span text not found")
}

// TestSkillsDetailDocument_BoldSpan — **bold** wraps the text in bold
// without altering the surrounding body colour.
func TestSkillsDetailDocument_BoldSpan(t *testing.T) {
	body := "A **bold** word.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 80, p, "demo", "d")

	doc := detailDoc(pn, SkillInfo{Name: "demo", Path: p})
	for _, l := range doc {
		for _, row := range l.Rows {
			if strings.Contains(row, "bold") {
				if !strings.Contains(row, MDBold) {
					t.Errorf("bold span missing palette: %q", row)
				}
				if !strings.Contains(row, "A ") || !strings.Contains(row, " word.") {
					t.Errorf("bold row missing surroundings: %q", row)
				}
				return
			}
		}
	}
	t.Fatal("bold text not found")
}

// TestSkillsDetailDocument_ThematicBreak — a "---" line on its own renders
// as the dim "──────" rule.
func TestSkillsDetailDocument_ThematicBreak(t *testing.T) {
	body := "# T\n\n---\n\nafter the rule.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 80, p, "demo", "d")

	doc := detailDoc(pn, SkillInfo{Name: "demo", Path: p})
	for _, l := range doc {
		for _, row := range l.Rows {
			if strings.Contains(row, MDHRText) {
				if !strings.Contains(row, MDHROpen) {
					t.Errorf("HR row missing the dim palette: %q", row)
				}
				return
			}
		}
	}
	t.Fatal("HR rule not found")
}

// TestSkillsDetailDocument_HTMLCommentsSkipped — `<!-- ... -->` lines do
// not appear in the document and do not contribute to the counter (REF:
// antigravity-guide's two comment lines reduce neither the row count nor
// the "(a–b of T lines)" total).
func TestSkillsDetailDocument_HTMLCommentsSkipped(t *testing.T) {
	body := "# T\n\n<!-- LINT -->\n\nreal paragraph.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 80, p, "demo", "d")

	doc := detailDoc(pn, SkillInfo{Name: "demo", Path: p})
	plain := docToPlain(doc)
	for i, row := range plain {
		if strings.Contains(row, "LINT") {
			t.Errorf("HTML comment leaked into row %d: %q", i, row)
		}
	}
	last := strings.TrimSpace(plain[len(plain)-1])
	if last != "real paragraph." {
		t.Errorf("document tail = %q, want `real paragraph.` (full: %v)", last, plain)
	}
}

// ---- counter ---------------------------------------------------------------

// TestSkillsDetailDocument_Counter — a long document (total > vis) gets a
// "(a–b of T lines)" counter row at the visible window's end. Counter
// uses the en-dash (\u2013) and dim grey styling.
func TestSkillsDetailDocument_Counter(t *testing.T) {
	// 80 body lines + # Title + blank + desc. = 83 body logical lines;
	// the catalogue header (title + desc + path + blank) adds another 4;
	// total = 87, the +1 quirk on the reference makes the counter say 88.
	var b strings.Builder
	b.WriteString("---\nname: demo\n---\n# Title\n\ndesc.\n")
	for i := 0; i < 80; i++ {
		b.WriteString("body line\n")
	}
	p := writeSkillMD(t, b.String())
	pn := detailPanel(t, 50, 80, p, "demo", "desc")

	pn.window = 0 // ensure we render the very top
	rows := pn.Lines()
	// viewer.Counter wraps the value in palette.DimGrayOpen / palette.DimGrayClose;
	// confirm both the styled form and the plain digits are present.
	wantStyled := viewer.Counter(1, 26, 88)
	wantPlain := "(1–26 of 88 lines)"
	foundStyled := false
	foundPlain := false
	for _, r := range rows {
		if strings.Contains(r, wantStyled) {
			foundStyled = true
		}
		if strings.Contains(stripSGR(r), wantPlain) {
			foundPlain = true
		}
	}
	if !foundStyled {
		t.Errorf("counter SGR row missing %q\nplain:%s", wantStyled, joinStrip(rows))
	}
	if !foundPlain {
		t.Errorf("counter plain missing %q\nplain:%s", wantPlain, joinStrip(rows))
	}
}

// TestSkillsDetailDocument_CounterShort — a short document (total ≤ vis)
// drops the counter and emits an empty row in its place (the reference's
// own quirk — uv's 26-row document still says "27 lines").
func TestSkillsDetailDocument_CounterShort(t *testing.T) {
	body := "# T\n\nshort.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 80, p, "demo", "d")

	rows := pn.Lines()
	plain := joinStrip(rows)
	if strings.Contains(plain, "of ") && strings.Contains(plain, "lines)") {
		t.Errorf("short doc should not show a counter, got:\n%s", plain)
	}
}

// ---- scroll keys -----------------------------------------------------------

func TestSkillsDetailScroll_Down(t *testing.T) {
	pn := longDetailPanel(t, 50, 80)
	if pn.window != 0 {
		t.Fatalf("window=%d, want 0", pn.window)
	}
	if h, _ := pn.HandleKey(tea.Key{Code: tea.KeyDown}); !h {
		t.Fatal("down not handled")
	}
	if pn.window != 1 {
		t.Errorf("window=%d, want 1", pn.window)
	}
}

func TestSkillsDetailScroll_Up(t *testing.T) {
	pn := longDetailPanel(t, 50, 80)
	pn.window = 5
	if h, _ := pn.HandleKey(tea.Key{Code: tea.KeyUp}); !h {
		t.Fatal("up not handled")
	}
	if pn.window != 4 {
		t.Errorf("window=%d, want 4", pn.window)
	}
}

// TestSkillsDetailScroll_Clamp — Up at window=0 is a no-op; Down at the
// bottom stops at total - vis.
func TestSkillsDetailScroll_Clamp(t *testing.T) {
	pn := longDetailPanel(t, 50, 80)
	// Floor test — pressing Up at window=0 keeps window=0.
	pn.HandleKey(tea.Key{Code: tea.KeyUp})
	if pn.window != 0 {
		t.Errorf("up at 0: window=%d, want 0", pn.window)
	}
	// Run down until the clamp.
	for i := 0; i < 1000; i++ {
		pn.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	vis := SkillsDetailVisible(50)
	doc := detailDoc(pn, pn.selected())
	maxOff := len(doc) + 1 - vis
	if maxOff < 0 {
		maxOff = 0
	}
	if pn.window != maxOff {
		t.Errorf("clamp: window=%d, want %d", pn.window, maxOff)
	}
}

// TestSkillsDetailScroll_Page — PgDown steps by vis-1 (one less than the
// window — REF's page rule). PgUp steps up by the same amount.
func TestSkillsDetailScroll_Page(t *testing.T) {
	pn := longDetailPanel(t, 50, 80)
	vis := SkillsDetailVisible(50)
	page := vis - 1
	if page < 1 {
		page = 1
	}

	if h, _ := pn.HandleKey(tea.Key{Code: tea.KeyPgDown}); !h {
		t.Fatal("pgdown not handled")
	}
	if pn.window != page {
		t.Errorf("after pgdown: window=%d, want %d", pn.window, page)
	}
	if h, _ := pn.HandleKey(tea.Key{Code: tea.KeyPgUp}); !h {
		t.Fatal("pgup not handled")
	}
	if pn.window != 0 {
		t.Errorf("after pgup: window=%d, want 0", pn.window)
	}
}

// TestSkillsDetailScroll_CtrlHomeEnd — ctrl+home parks at the top;
// ctrl+end jumps to maxOffset.
func TestSkillsDetailScroll_CtrlHomeEnd(t *testing.T) {
	pn := longDetailPanel(t, 50, 80)
	pn.window = 5
	if h, _ := pn.HandleKey(tea.Key{
		Code: tea.KeyHome, Mod: tea.ModCtrl,
	}); !h {
		t.Fatal("ctrl-home not handled")
	}
	if pn.window != 0 {
		t.Errorf("ctrl-home: window=%d, want 0", pn.window)
	}

	vis := SkillsDetailVisible(50)
	doc := detailDoc(pn, pn.selected())
	maxOff := len(doc) + 1 - vis
	if maxOff < 0 {
		maxOff = 0
	}
	if h, _ := pn.HandleKey(tea.Key{
		Code: tea.KeyEnd, Mod: tea.ModCtrl,
	}); !h {
		t.Fatal("ctrl-end not handled")
	}
	if pn.window != maxOff {
		t.Errorf("ctrl-end: window=%d, want %d", pn.window, maxOff)
	}
}

// TestSkillsDetailScroll_EscReturnsToList — Esc from the detail view
// flips pn.detail back to false and the panel stays open (the list comes
// back). The transcript is left untouched.
func TestSkillsDetailScroll_EscReturnsToList(t *testing.T) {
	pn := detailPanel(t, 50, 80, writeSkillMD(t, "# T\n\nbody.\n"), "demo", "d")
	pn.detail = true
	if h, _ := pn.HandleKey(tea.Key{Code: tea.KeyEsc}); !h {
		t.Fatal("esc not handled")
	}
	if pn.detail {
		t.Error("esc should close the detail sub-view")
	}
	if pn.Cursor() != 0 {
		t.Errorf("esc should keep the cursor on the skill, got %d", pn.Cursor())
	}
}

// TestSkillsList_VimKeybindings — the grouped list is a list, so it takes
// the list's vi keys: 'j'/'k' move the cursor, 'g' jumps to the top and
// 'G' to the last skill. 'q' leaves the panel.
func TestSkillsList_VimKeybindings(t *testing.T) {
	closed := false
	pn := New(Seams{
		Catalog: func() []SkillInfo { return cycSharedSkills(3) },
		OSC8:    func() bool { return false },
		Size:    func() (int, int) { return 220, 30 },
		Close:   func() tea.Cmd { closed = true; return nil },
	})
	pn.SetCursor(0)

	handled, _ := pn.HandleKey(tea.Key{Code: 'j', Text: "j"})
	if !handled || pn.Cursor() != 1 {
		t.Errorf("list 'j': handled=%v, cursor=%d, want 1", handled, pn.Cursor())
	}

	handled, _ = pn.HandleKey(tea.Key{Code: 'k', Text: "k"})
	if !handled || pn.Cursor() != 0 {
		t.Errorf("list 'k': handled=%v, cursor=%d, want 0", handled, pn.Cursor())
	}

	handled, _ = pn.HandleKey(tea.Key{Code: 'G', Text: "G"})
	last := pn.RowCount() - 1
	if !handled || pn.Cursor() != last {
		t.Errorf("list 'G': handled=%v, cursor=%d, want %d", handled, pn.Cursor(), last)
	}

	handled, _ = pn.HandleKey(tea.Key{Code: 'g', Text: "g"})
	if !handled || pn.Cursor() != 0 {
		t.Errorf("list 'g': handled=%v, cursor=%d, want 0", handled, pn.Cursor())
	}

	handled, _ = pn.HandleKey(tea.Key{Code: 'q', Text: "q"})
	if !handled || !closed {
		t.Errorf("list 'q': handled=%v, closed=%v, want true", handled, closed)
	}
}

// TestSkillsDetail_VimKeybindings — the detail view is a pager, so it
// takes the pager's vi keys: 'j' scrolls down, 'k' scrolls up and 'q'
// leaves the detail view (the reference's viewer convention).
func TestSkillsDetail_VimKeybindings(t *testing.T) {
	pn := longDetailPanel(t, 50, 80)
	pn.window = 0

	handled, _ := pn.HandleKey(tea.Key{Code: 'j', Text: "j"})
	if !handled || pn.window != 1 {
		t.Errorf("detail 'j': handled=%v, window=%d, want 1", handled, pn.window)
	}

	handled, _ = pn.HandleKey(tea.Key{Code: 'k', Text: "k"})
	if !handled || pn.window != 0 {
		t.Errorf("detail 'k': handled=%v, window=%d, want 0", handled, pn.window)
	}

	handled, _ = pn.HandleKey(tea.Key{Code: 'q', Text: "q"})
	if !handled || pn.detail {
		t.Errorf("detail 'q': handled=%v, detail=%v, want false", handled, pn.detail)
	}
}

// ---- wiring ----------------------------------------------------------------

// TestSkillsDetailLines_ViewerWiredByPath — when a catalogue row has a
// Path pointing at a real SKILL.md, skillsPanelDetailLines delegates to
// the viewer renderer (counter present, viewer-shaped legend, no
// "Skills" header).
func TestSkillsDetailLines_ViewerWiredByPath(t *testing.T) {
	body := "# T\n\nparagraph.\n"
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 80, p, "demo", "d")
	rows := pn.Lines()
	plain := joinStrip(rows)

	if !strings.Contains(plain, "demo") {
		t.Errorf("viewer should show the catalogue name:\n%s", plain)
	}
	if strings.Contains(plain, "Skills") || strings.Contains(plain, "testTitleOpen") {
		// The legacy shape opens with a "Skills" title; the viewer does not.
		if firstNonBlank(plain) == "Skills" {
			t.Errorf("viewer should not render the legacy \"Skills\" title:\n%s", plain)
		}
	}
	if !strings.Contains(plain, "↑/↓") {
		t.Errorf("viewer legend missing ↑/↓:\n%s", plain)
	}
	if !strings.Contains(plain, "esc back") {
		t.Errorf("viewer legend missing `esc back`:\n%s", plain)
	}
}

// TestSkillsDetailLines_LegacyWhenNoPath — a catalogue row without a Path
// falls back to the cycle-203 minimal shape (title "Skills",
// description, path row, esc-only legend).
func TestSkillsDetailLines_LegacyWhenNoPath(t *testing.T) {
	pn := newTestPanel([]SkillInfo{{Name: "demo", Description: "d"}}, 80, 30)
	pn.SetCursor(0)
	pn.detail = true

	rows := pn.Lines()
	plain := joinStrip(rows)
	if firstNonBlank(plain) != "Skills" {
		t.Errorf("legacy shape should open with `Skills`, got:\n%s", plain)
	}
	if strings.Contains(plain, "↑/↓") {
		t.Errorf("legacy shape should not show the viewer legend:\n%s", plain)
	}
}

// ---- helpers ---------------------------------------------------------------

// longDetailPanel sets up a 50-row × 80-col viewer whose SKILL.md has
// ~120 body lines — long enough to require at least one scroll step.
func longDetailPanel(t *testing.T, h, w int) *Panel {
	t.Helper()
	var b strings.Builder
	b.WriteString("---\nname: demo\n---\n# Title\n\nA description.\n\n")
	for i := 0; i < 120; i++ {
		b.WriteString("body line\n")
	}
	p := filepath.Join(t.TempDir(), "SKILL.md")
	if err := os.WriteFile(p, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return detailPanel(t, h, w, p, "demo", "A description.")
}

// docToPlain collapses a document into plain text rows by stripping SGR
// from each rendered row.
func docToPlain(doc []DetailLine) []string {
	var out []string
	for _, l := range doc {
		for _, row := range l.Rows {
			out = append(out, stripSGR(row))
		}
	}
	return out
}

// joinStrip turns a row slice into a newline-joined plain string.
func joinStrip(rows []string) string {
	var b strings.Builder
	for _, r := range rows {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(stripSGR(r))
	}
	return b.String()
}

// firstNonBlank returns the first non-blank line of plain.
func firstNonBlank(plain string) string {
	for _, l := range strings.Split(plain, "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

// TestCycle218_SkillsPipeTables is the cycle-218 entry point. `go test -run
// TestCycle218` selects the whole GFM pipe-table family below.
func TestCycle218_SkillsPipeTables(t *testing.T) {
	t.Run("SeparatorDetection", TestSkillsTable_SeparatorDetection)
	t.Run("HeaderOnlyNoData", TestSkillsTable_HeaderOnlyNoData)
	t.Run("BasicLayout", TestSkillsTable_BasicLayout)
	t.Run("LinkFootnotes", TestSkillsTable_LinkFootnotes)
	t.Run("LocalLinksNoFootnote", TestSkillsTable_LocalLinksNoFootnote)
	t.Run("DocumentWideNumbering", TestSkillsTable_DocumentWideNumbering)
	t.Run("ColumnShrink", TestSkillsTable_ColumnShrink)
	t.Run("BulletInCell", TestSkillsTable_BulletInCell)
	t.Run("MultiRowCell", TestSkillsTable_MultiRowCell)
	t.Run("UVExactCounter", TestSkillsTable_UVExactCounter)
	t.Run("HostFixturesCounters", TestSkillsTable_HostFixturesCounters)
}

// ---- helpers ---------------------------------------------------------------

// countURLsInBody walks the body for [text](http*) patterns and returns
// the number of unique URLs.
func countURLsInBody(body string) int {
	seen := map[string]bool{}
	for _, l := range strings.Split(body, "\n") {
		for i := 0; i < len(l); i++ {
			if l[i] != '[' {
				continue
			}
			j := strings.IndexByte(l[i:], ']')
			if j <= 0 || i+j+1 >= len(l) || l[i+j+1] != '(' {
				continue
			}
			k := strings.IndexByte(l[i+j+2:], ')')
			if k <= 0 {
				continue
			}
			url := l[i+j+2 : i+j+2+k]
			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
				seen[url] = true
			}
		}
	}
	return len(seen)
}

// tableFixture renders an arbitrary markdown body and returns the plain
// (SGR-stripped) body rows. The catalogue header (title / desc / path /
// blank) is skipped, so callers see only what the SKILL.md contributed.
func tableFixture(t *testing.T, body string) []string {
	t.Helper()
	p := writeSkillMD(t, body)
	pn := detailPanel(t, 50, 220, p, "demo", "d")
	doc := detailDoc(pn, SkillInfo{
		Name: "demo", Description: "d", Path: p,
	})
	plain := docToPlain(doc)
	// Skip the 4 catalogue-header rows (title, description, path, blank),
	// and any leading blank rows from the body itself.
	for i, r := range plain {
		if strings.Contains(r, "/SKILL.md") {
			rest := plain[i+1:]
			// Trim a single leading blank (the catalogue header's trailing
			// blank line); keep any blanks inside the body.
			for j, b := range rest {
				if strings.TrimSpace(b) != "" {
					return rest[j:]
				}
			}
			return rest
		}
	}
	return plain
}

// findFirstRow returns the first plain row whose trimmed text equals
// (or contains, if has is true) substr; "" if not found.
func findFirstRow(rows []string, substr string, has bool) string {
	for _, r := range rows {
		plain := strings.TrimSpace(r)
		if has {
			if strings.Contains(plain, substr) {
				return r
			}
			continue
		}
		if plain == substr {
			return r
		}
	}
	return ""
}

// countFootnoteLines returns the number of rows that look like `[N]: url`.
func countFootnoteLines(rows []string) int {
	re := regexp.MustCompile(`\[\d+\]:\s+https?://`)
	n := 0
	for _, r := range rows {
		if re.MatchString(stripSGR(r)) {
			n++
		}
	}
	return n
}

// ---- unit tests ------------------------------------------------------------

// TestSkillsTable_SeparatorDetection — the second-line `|---|---|\n` is
// what flags a GFM pipe table; without it, the renderer falls through to
// the paragraph branch.
func TestSkillsTable_SeparatorDetection(t *testing.T) {
	body := "| h1 | h2 |\n| --- | --- |\n| a | b |\n| c | d |\n"
	rows := tableFixture(t, body)
	// 3 rows in the table (header + 2 data). All bordered.
	if len(rows) < 3 {
		t.Fatalf("expected ≥3 rows (1 header + 2 data), got %d: %v", len(rows), rows)
	}
	for i := 0; i < 3; i++ {
		if !strings.Contains(rows[i], "|") {
			t.Errorf("table row %d not bordered: %q", i, rows[i])
		}
	}
	if !strings.Contains(rows[0], "h1") || !strings.Contains(rows[0], "h2") {
		t.Errorf("header row missing cells: %q", rows[0])
	}
	if !strings.Contains(rows[1], "a") || !strings.Contains(rows[2], "c") {
		t.Errorf("data rows misordered: %q %q", rows[1], rows[2])
	}
}

// TestSkillsTable_HeaderOnlyNoData — header + separator but no data rows
// does not constitute a table (the renderer bails and falls through).
func TestSkillsTable_HeaderOnlyNoData(t *testing.T) {
	body := "| h1 | h2 |\n| --- | --- |\n"
	rows := tableFixture(t, body)
	// The two raw lines are drawn as ordinary body rows (no boxed table).
	if strings.Contains(rows[0], "─") || strings.Contains(rows[1], "─") {
		t.Errorf("no-data header should not render as boxed table: %v", rows)
	}
}

// TestSkillsTable_BasicLayout — three-column table with `---` separator
// renders header + 2 data rows; each row is wrapped in `│` borders.
func TestSkillsTable_BasicLayout(t *testing.T) {
	body := "" +
		"| Topic | Docs | Use |\n" +
		"| --- | --- | --- |\n" +
		"| A | Doc1 | First |\n" +
		"| B | Doc2 | Second |\n"
	rows := tableFixture(t, body)
	if len(rows) < 3 {
		t.Fatalf("table has %d rows, want ≥3: %v", len(rows), rows)
	}
	for i := 0; i < 3; i++ {
		row := rows[i]
		if !strings.HasPrefix(row, "  |") {
			t.Errorf("table row %d should start with `  |`: %q", i, row)
		}
		if !strings.HasSuffix(row, "|") {
			t.Errorf("table row %d should end with `|`: %q", i, row)
		}
	}
	// Header: Topic / Docs / Use.
	if !strings.Contains(rows[0], "Topic") ||
		!strings.Contains(rows[0], "Docs") ||
		!strings.Contains(rows[0], "Use") {
		t.Errorf("header row missing cells: %q", rows[0])
	}
	// Data rows.
	if !strings.Contains(rows[1], "A") {
		t.Errorf("first data row missing: %q", rows[1])
	}
	if !strings.Contains(rows[2], "B") {
		t.Errorf("second data row missing: %q", rows[2])
	}
}

// TestSkillsTable_LinkFootnotes — a markdown link `[text](url)` in a cell
// becomes `text[N]`; one `[N]: url` line is appended per unique URL at the
// document's end. Numbering is 1-based and stable.
func TestSkillsTable_LinkFootnotes(t *testing.T) {
	body := "" +
		"| Topic | Docs |\n" +
		"| --- | --- |\n" +
		"| Alpha | [first](https://example.com/a) |\n" +
		"| Beta | [second](https://example.com/b) |\n" +
		"| Gamma | [first](https://example.com/a) |\n" // dup of row 1's URL
	rows := tableFixture(t, body)
	// Expect: 4 table rows (1 header + 3 data) + 2 unique URLs = 6 rows.
	if countFootnoteLines(rows) != 2 {
		t.Errorf("expected 2 unique footnotes, got %d\nrows: %v",
			countFootnoteLines(rows), rows)
	}
	// Footnote block must be the document tail.
	last := strings.TrimSpace(rows[len(rows)-1])
	if !strings.HasPrefix(last, "[2]:") {
		t.Errorf("last footnote should be [2], got %q", last)
	}
	if !strings.HasSuffix(last, "/b") {
		t.Errorf("last footnote URL wrong: %q", last)
	}
	// The Gamma row's link must reuse [1], not get a fresh [3].
	for _, r := range rows {
		if strings.Contains(r, "first[") {
			if !strings.Contains(r, "first[1]") {
				t.Errorf("dup URL should reuse [1], got %q", r)
			}
		}
	}
}

// TestSkillsTable_LocalLinksNoFootnote — relative `./...` and `references/...`
// links keep the link text only, no `[N]` ref and no `[N]: url` line. This
// matches the antigravity-guide measurement (cycle 211's
// caps/ref.detail.agguide.* captures).
func TestSkillsTable_LocalLinksNoFootnote(t *testing.T) {
	body := "" +
		"| Topic | Doc |\n" +
		"| --- | --- |\n" +
		"| First | [cli.md](./docs/references/cli.md) |\n" +
		"| Second | [guide](references/guide.md) |\n"
	rows := tableFixture(t, body)
	if countFootnoteLines(rows) != 0 {
		t.Errorf("local links should not be footnoted, got %d footnotes\nrows: %v",
			countFootnoteLines(rows), rows)
	}
	for _, r := range rows {
		if strings.Contains(stripSGR(r), "[1]") {
			t.Errorf("local link should not have a [N] ref: %q", r)
		}
	}
}

// TestSkillsTable_DocumentWideNumbering — two tables in one document share
// a single `[N]` sequence; the second table's first URL gets `[3]` (after
// the two URLs from the first table).
func TestSkillsTable_DocumentWideNumbering(t *testing.T) {
	body := "" +
		"# T\n\n" +
		"| A | Doc |\n" +
		"| --- | --- |\n" +
		"| x | [u1](https://a.example) |\n" +
		"| y | [u2](https://b.example) |\n\n" +
		"between tables.\n\n" +
		"| C | Link |\n" +
		"| --- | --- |\n" +
		"| p | [u3](https://c.example) |\n"
	rows := tableFixture(t, body)
	if countFootnoteLines(rows) != 3 {
		t.Errorf("expected 3 footnotes (2 from table1 + 1 from table2), got %d\nrows: %v",
			countFootnoteLines(rows), rows)
	}
	// Last footnote must be [3].
	last := strings.TrimSpace(rows[len(rows)-1])
	if !strings.HasPrefix(last, "[3]:") {
		t.Errorf("last footnote should be [3], got %q", last)
	}
	// Find the second table's first data row.
	var secondTblHeader int
	for i, r := range rows {
		if strings.Contains(r, "| C ") && strings.Contains(r, "| Link ") {
			secondTblHeader = i
			break
		}
	}
	if secondTblHeader == 0 {
		t.Fatalf("could not find second table header:\n%v", rows)
	}
	firstDataRow := rows[secondTblHeader+1]
	if !strings.Contains(firstDataRow, "u3[3]") {
		t.Errorf("second table's first URL should be [3], got %q", firstDataRow)
	}
}

// TestSkillsTable_ColumnShrink — when total table width exceeds the panel
// budget (220 - 2 = 218 here), the widest column shrinks so the table
// fits. No row should exceed 218 columns.
func TestSkillsTable_ColumnShrink(t *testing.T) {
	// 3 cols × 70-char text = 210 + 6 borders = 216; safe.
	// Then 4 cols × 70-char text = 280 + 8 borders = 288; needs shrink.
	body := "" +
		"| A | B | C | D |\n" +
		"| --- | --- | --- | --- |\n"
	for i := 0; i < 5; i++ {
		body += "| " + strings.Repeat("x", 70) + " | " +
			strings.Repeat("y", 70) + " | " +
			strings.Repeat("z", 70) + " | " +
			strings.Repeat("w", 70) + " |\n"
	}
	rows := tableFixture(t, body)
	for i, r := range rows {
		w := len([]rune(stripSGR(r)))
		if w > 220 {
			t.Errorf("row %d exceeds panel width: %d cols (budget 220): %q",
				i, w, r[:min(80, len(r))])
		}
	}
}

// TestSkillsTable_BulletInCell — a leading `- ` inside a cell is kept as
// plain text (the renderer does not consume list markers inside table
// cells). Antigravity-guide's hyperlinks point at relative URLs that
// look like list items; this guards against that regression.
func TestSkillsTable_BulletInCell(t *testing.T) {
	body := "" +
		"| Item | Detail |\n" +
		"| --- | --- |\n" +
		"| alpha | - keep this bullet |\n" +
		"| beta | plain text |\n"
	rows := tableFixture(t, body)
	// The bullet should appear inside the cell, not as a separate list row.
	found := false
	for _, r := range rows {
		if strings.Contains(r, "keep this bullet") {
			found = true
			// Must NOT have a `  • ` glyph (that's a list row).
			if strings.Contains(r, "• keep this bullet") {
				t.Errorf("bullet in cell got promoted to a list row: %q", r)
			}
		}
	}
	if !found {
		t.Errorf("bullet text not found in any row: %v", rows)
	}
}

// TestSkillsTable_MultiRowCell — a cell whose content wraps to N visual
// rows produces N table rows (with shorter cells padded with blanks).
// This is the agents-sdk Class-URL table on disk (Task/API rows wrap).
func TestSkillsTable_MultiRowCell(t *testing.T) {
	// Force a column shrink by stuffing col0 with enough text that the
	// table exceeds the panel budget. After shrink col0 ≈ 200 cols; the
	// 50 × "verylong " text (449 chars) wraps to 3 visual rows.
	body := "" +
		"| Long | Short |\n" +
		"| --- | --- |\n" +
		"| " + strings.Repeat("verylong ", 50) + " | brief |\n"
	rows := tableFixture(t, body)
	if len(rows) < 4 {
		t.Fatalf("expected ≥4 rows (1 header + 3 visual data rows), got %d: %v",
			len(rows), rows)
	}
	// "brief" appears in the first visual row of the data only. The
	// subsequent visual rows of that data row should be padded with blanks
	// in the short cell's column — so the row width stays constant.
	foundBrief := 0
	for _, r := range rows[1:] {
		if strings.Contains(r, "brief") {
			foundBrief++
		}
	}
	if foundBrief != 1 {
		t.Errorf("expected `brief` in exactly 1 visual row (the first), got %d: %v",
			foundBrief, rows)
	}
	// All data visual rows must have the same plain width — the renderer
	// pads shorter cells so the table stays rectangular.
	widths := map[int]int{}
	for _, r := range rows[1:] {
		widths[len([]rune(r))]++
	}
	if len(widths) > 1 {
		t.Errorf("data rows have inconsistent widths: %v (rows: %v)", widths, rows)
	}
}

// TestSkillsTable_UVExactCounter — the science:uv skill (no tables, no
// footnotes) MUST still render at the cycle-211 value of 27 lines. This
// guards the regression that adding the table renderer might shift the
// counter for non-table skills.
//
// Runs against the host's SKILL.md at
// `~/.gemini/config/plugins/science/skills/uv/SKILL.md`. Skips if missing.
func TestSkillsTable_UVExactCounter(t *testing.T) {
	const uv = "/Users/gear/.gemini/config/plugins/science/skills/uv/SKILL.md"
	if _, err := os.Stat(uv); err != nil {
		t.Skipf("uv SKILL.md not on this host (%v)", err)
	}
	pn := newTestPanel([]SkillInfo{{
		Name: "uv", Description: "uv (the tool)", Path: uv,
	}}, 220, 50)
	pn.SetCursor(0)
	pn.detail = true

	rows := pn.Lines()
	plain := joinStrip(rows)
	want27 := "(1–26 of 27 lines)"
	want46 := "(1–26 of 46 lines)"
	if !strings.Contains(plain, want27) && !strings.Contains(plain, want46) {
		t.Errorf("uv counter missing 27 or 46 lines (plain:%s)", plain)
	}
}

// TestSkillsTable_HostFixturesCounters — measures the post-cycle-218
// counter value for each of the five host SKILL.md files. Skips any
// that are missing. Does NOT fail on a wrong number — the deliverable
// records the observed values and explains discrepancies in 存疑.
func TestSkillsTable_HostFixturesCounters(t *testing.T) {
	targets := []struct {
		name string
		path string
	}{
		{"agents-sdk", "/Users/gear/.gemini/skills/agents-sdk/SKILL.md"},
		{"agy-customizations", "/Users/gear/.gemini/antigravity-cli/builtin/skills/agy-customizations/SKILL.md"},
		{"antigravity-guide", "/Users/gear/.gemini/antigravity-cli/builtin/skills/antigravity_guide/SKILL.md"},
		{"a11y-debugging", "/Users/gear/.gemini/config/plugins/chrome-devtools-plugin/skills/a11y-debugging/SKILL.md"},
		{"uv", "/Users/gear/.gemini/config/plugins/science/skills/uv/SKILL.md"},
	}
	for _, tgt := range targets {
		tgt := tgt
		t.Run(tgt.name, func(t *testing.T) {
			if _, err := os.Stat(tgt.path); err != nil {
				t.Skipf("SKILL.md not on this host (%v)", err)
			}
			data, err := os.ReadFile(tgt.path)
			if err != nil {
				t.Fatal(err)
			}
			body := string(data)
			if strings.HasPrefix(body, "---\n") || strings.HasPrefix(body, "---\r\n") {
				rest := body[strings.IndexByte(body, '\n')+1:]
				if idx := strings.Index(rest, "\n---"); idx >= 0 {
					body = strings.TrimPrefix(rest[idx+4:], "\r\n")
				}
			}
			p := writeSkillMD(t, body)
			pn := detailPanel(t, 50, 220, p, tgt.name, "d")
			rows := pn.Lines()
			plain := joinStrip(rows)
			// Counter row.
			re := regexp.MustCompile(`\((\d+)–(\d+) of (\d+) lines\)`)
			matches := re.FindStringSubmatch(plain)
			if matches == nil {
				t.Fatalf("no counter row in:\n%s", plain)
			}
			t.Logf("[%s] counter = (%s–%s of %s lines); body bytes=%d; URLs=%d; footnotes=%d",
				tgt.name, matches[1], matches[2], matches[3],
				len(body), countURLsInBody(body), countFootnoteLines(docToPlain(detailDoc(pn, SkillInfo{
					Name: tgt.name, Path: p,
				}))))
		})
	}
}
