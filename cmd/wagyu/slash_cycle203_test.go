// slash_cycle203_test.go — cycle 203: the /skills picker.
//
// These tests pin the reference layout recovered from the caps/ref.h*.raw.txt
// probes (agy 1.4.0) and the height→window rule n = height*3/4 - 11. They use
// a synthetic catalogue (withSyntheticCatalog) so they never depend on the
// host's ~/.gemini tree, except TestSkillsPanelLayout_HostCatalogue which is
// skipped when the plugin bundle is absent.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

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
	t.Run("OpensUnselected", TestSkillsPanelOpensUnselected)
}

// cycSkill builds one synthetic catalogue entry.
func cycSkill(name, desc, path string, builtin bool, plugin string) jsonSkillInfo {
	return jsonSkillInfo{
		Name: name, Description: desc, Path: path,
		Builtin: builtin, Plugin: plugin, ModelInvocable: true,
	}
}

// cycCatalogue is a four-entry, three-group catalogue whose group directories
// live under $HOME so the tilde-shortened labels are deterministic. Returned
// in printSlashSkillInfos' display-name order.
func cycCatalogue(t *testing.T) (cat []jsonSkillInfo, home, sharedDir, builtinDir, pluginDir string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("no HOME")
	}
	sharedDir = filepath.Join(home, ".gemini", "skills")
	builtinDir = filepath.Join(home, ".gemini", "antigravity-cli", "builtin", "skills")
	pluginDir = filepath.Join(home, ".gemini", "config", "plugins", "chrome-devtools-plugin", "skills")
	cat = []jsonSkillInfo{
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
	withSyntheticCatalog(t, cat)

	groups := skillsPanelGroups()
	if len(groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(groups))
	}
	if groups[0].left != "Shared skills" || groups[0].label != "From ~/.gemini/skills" {
		t.Errorf("shared header = %q · %q", groups[0].left, groups[0].label)
	}
	if len(groups[0].skills) != 2 || groups[0].skills[0].Name != "alpha" || groups[0].skills[1].Name != "beta" {
		t.Errorf("shared skills = %v", namesOf(groups[0].skills))
	}
	if groups[1].left != builtinDir || groups[1].label != "Built-in" {
		t.Errorf("builtin header = %q · %q (want %q · Built-in)", groups[1].left, groups[1].label, builtinDir)
	}
	if groups[2].left != pluginDir ||
		groups[2].label != "From ~/.gemini/config/plugins/chrome-devtools-plugin" {
		t.Errorf("plugin header = %q · %q", groups[2].left, groups[2].label)
	}
}

func namesOf(ss []jsonSkillInfo) []string {
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
	withSyntheticCatalog(t, cat)

	m := mkPanel(panelSkills, "/skills", -1)
	m.height = 100 // every item fits → no counter
	want := []string{
		panelTitleOpen + "Skills" + panelTitleClose,
		cDimGrayOpen + "4 skills" + cDimGrayClose,
		"",
		"\x1b[1mCreate new skills\x1b[0m",
		"  " + cDimGrayOpen + "Workspace: " + skillsPanelWorkspacePath() + cDimGrayClose,
		"  " + cDimGrayOpen + "Global:    ~/.gemini/antigravity-cli/skills/{skill_name}/SKILL.md" + cDimGrayClose,
		"  " + cDimGrayOpen + "Shared:    ~/.gemini/skills/{skill_name}/SKILL.md" + cDimGrayClose,
		"",
		"\x1b[1mShared skills · " + panelGray238 + "From ~/.gemini/skills" + "\x1b[0m",
		"  " + cBlueOpen + "alpha" + cBlueClose + ": " + panelGray238 + "Alpha desc" + panelGray238C,
		"  " + cBlueOpen + "beta" + cBlueClose + ": " + panelGray238 + "Beta desc" + panelGray238C,
		"",
		"\x1b[1m" + builtinDir + " · " + panelGray238 + "Built-in" + "\x1b[0m",
		"  " + cBlueOpen + "agy-customizations" + cBlueClose + ": " + panelGray238 + "Builtin desc" + panelGray238C +
			" · \x1b[3m\x1b[38;2;101;123;131mbuilt-in\x1b[0m",
		"",
		"\x1b[1m" + pluginDir + " · " + panelGray238 +
			"From ~/.gemini/config/plugins/chrome-devtools-plugin" + "\x1b[0m",
		"  " + cBlueOpen + "chrome-devtools-plugin:a11y" + cBlueClose + ": " + panelGray238 + "A11y desc" + panelGray238C,
		"",
		panelKeyboardHint("↑/↓", "Navigate", "enter", "View", "esc", "Cancel"),
	}
	got := m.panelLines()
	assertLines(t, got, want)
}

// TestSkillsPanelCounts pins the arithmetic behind "<N> skills" and
// "[a-b of M items]": N is the skill count, M is skills + group headers.
func TestSkillsPanelCounts(t *testing.T) {
	cat, _, _, _, _ := cycCatalogue(t)
	withSyntheticCatalog(t, cat)

	model := newSkillsPanelModel()
	if len(model.skills) != 4 {
		t.Errorf("skills = %d, want 4", len(model.skills))
	}
	if len(model.groups) != 3 {
		t.Errorf("groups = %d, want 3", len(model.groups))
	}
	if len(model.items) != 7 { // 4 skills + 3 headers
		t.Errorf("items = %d, want 7", len(model.items))
	}
	if len(model.items) != len(model.skills)+len(model.groups) {
		t.Errorf("items != skills+groups")
	}
	// skillItem maps each skill to its row in the item list.
	wantItem := []int{1, 2, 4, 6}
	if fmt.Sprint(model.skillItem) != fmt.Sprint(wantItem) {
		t.Errorf("skillItem = %v, want %v", model.skillItem, wantItem)
	}

	m := mkPanel(panelSkills, "/skills", -1)
	m.height = 100
	if got := stripSGR(m.panelLines()[1]); got != "4 skills" {
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
		if got := skillsVisibleItemsFor(c.h); got != c.want {
			t.Errorf("skillsVisibleItemsFor(%d) = %d, want %d", c.h, got, c.want)
		}
	}
}

// cycSharedSkills builds a sorted catalogue of n shared skills.
func cycSharedSkills(n int) []jsonSkillInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := make([]jsonSkillInfo, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("s%02d", i)
		out = append(out, cycSkill(name, "d", filepath.Join(home, ".gemini", "skills", name, "SKILL.md"), false, ""))
	}
	return out
}

// TestSkillsPanelCounterAndWindow pins the "[a-b of M items]" counter, its
// dim-gray span and the rule that it disappears once every item fits.
func TestSkillsPanelCounterAndWindow(t *testing.T) {
	withSyntheticCatalog(t, cycSharedSkills(20)) // 20 items + 1 header = 21

	// Height 24 → n = 7; cursor at -1 → window [1-7 of 21].
	m := mkPanel(panelSkills, "/skills", -1)
	m.height = 24
	joined := strings.Join(plainLines(m), "\n")
	if !strings.Contains(joined, "  [1-7 of 21 items]") {
		t.Fatalf("counter at top missing:\n%s", joined)
	}
	// The counter is the third row from the bottom: it, a blank, then the
	// keyboard legend.
	plain := plainLines(m)
	if got := plain[len(plain)-3]; !strings.Contains(got, "[1-7 of 21 items]") {
		t.Errorf("counter row = %q (want the 3rd-from-last)", got)
	}
	raw := m.panelLines()
	if !containsSGR(raw, cDimGrayOpen+"  [1-7 of 21 items]"+cDimGrayClose) {
		t.Errorf("counter raw span wrong: %q", raw[len(raw)-3])
	}

	// Cursor on the last skill (index 19 → item 20) scrolls the window to
	// [15-21]; start = 20-(7-1) = 14.
	m = mkPanel(panelSkills, "/skills", 19)
	m.height = 24
	if got := strings.Join(plainLines(m), "\n"); !strings.Contains(got, "  [15-21 of 21 items]") {
		t.Fatalf("scrolled counter wrong:\n%s", got)
	}

	// Every item fits at height 100 → no counter row at all.
	m = mkPanel(panelSkills, "/skills", -1)
	m.height = 100
	if got := strings.Join(plainLines(m), "\n"); strings.Contains(got, "items]") {
		t.Fatalf("counter should be absent when everything fits:\n%s", got)
	}
	for _, l := range m.panelLines() {
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
	got := stripSGR(skillsPanelRow(s, false, 220))
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
	gotB := stripSGR(skillsPanelRow(b, false, 220))
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
	if got := stripSGR(skillsPanelRow(short, false, 220)); got != "  uv: short description" {
		t.Errorf("short row = %q", got)
	}

	// Narrow terminal: a 100-wide window yields 99 columns.
	narrow := stripSGR(skillsPanelRow(s, false, 100))
	if visLen(narrow) != 99 {
		t.Errorf("narrow row width = %d, want 99", visLen(narrow))
	}
}

// TestSkillsPanelRow_CursorSpan pins that the cursor glyph sits inside the
// blue name span, while a plain row's two-space prefix sits outside it.
func TestSkillsPanelRow_CursorSpan(t *testing.T) {
	s := cycSkill("agents-sdk", "d", "/x/SKILL.md", false, "")
	if got := skillsPanelRow(s, true, 220); !strings.HasPrefix(got, cBlueOpen+"› agents-sdk"+cBlueClose+": ") {
		t.Errorf("cursor row span = %q", got)
	}
	if got := skillsPanelRow(s, false, 220); !strings.HasPrefix(got, "  "+cBlueOpen+"agents-sdk"+cBlueClose+": ") {
		t.Errorf("plain row span = %q", got)
	}
}

// TestSkillsPanelCursorKeys pins the ↑/↓ semantics: the cursor opens at -1
// (no selection), ↓ from -1 selects the first skill, and both ends clamp —
// ↑ on the first skill stays put (it does not clear the selection).
func TestSkillsPanelCursorKeys(t *testing.T) {
	withSyntheticCatalog(t, cycSharedSkills(5))
	m := mkPanel(panelSkills, "/skills", -1)

	down := tea.Key{Code: tea.KeyDown}
	up := tea.Key{Code: tea.KeyUp}

	if h, _ := m.handlePanelKey(up); !h {
		t.Fatal("up not handled")
	}
	if m.panel.cursor != -1 {
		t.Fatalf("up at -1 moved the cursor to %d", m.panel.cursor)
	}
	if h, _ := m.handlePanelKey(down); !h {
		t.Fatal("down not handled")
	}
	if m.panel.cursor != 0 {
		t.Fatalf("down from -1 → %d, want 0", m.panel.cursor)
	}
	m.handlePanelKey(down)
	if m.panel.cursor != 1 {
		t.Fatalf("second down → %d, want 1", m.panel.cursor)
	}
	m.handlePanelKey(up)
	if m.panel.cursor != 0 {
		t.Fatalf("up → %d, want 0", m.panel.cursor)
	}
	m.handlePanelKey(up)
	if m.panel.cursor != 0 {
		t.Fatalf("up on the first skill → %d, want 0 (clamp)", m.panel.cursor)
	}
	// Down clamps at the last skill.
	m.panel.cursor = 4
	for i := 0; i < 3; i++ {
		m.handlePanelKey(down)
	}
	if m.panel.cursor != 4 {
		t.Fatalf("down past the end → %d, want 4", m.panel.cursor)
	}
}

// TestSkillsPanelEnterGate pins that Enter opens the detail only when a row is
// selected, and that panelRowCount tracks the catalogue size.
func TestSkillsPanelEnterGate(t *testing.T) {
	withSyntheticCatalog(t, cycSharedSkills(3))
	enter := tea.Key{Code: tea.KeyEnter}

	m := mkPanel(panelSkills, "/skills", -1)
	if h, _ := m.handlePanelKey(enter); !h {
		t.Fatal("enter not handled")
	}
	if m.panel.detail {
		t.Fatal("enter with no selection must not open the detail")
	}

	m = mkPanel(panelSkills, "/skills", 1)
	m.handlePanelKey(enter)
	if !m.panel.detail {
		t.Fatal("enter on a selected skill should open the detail")
	}
	if !strings.Contains(panelText(m), "s01") {
		t.Errorf("detail should show the selected skill:\n%s", panelText(m))
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled || m.panel.detail {
		t.Fatal("esc should return from the detail")
	}

	if got := (&runMode{}).panelRowCount(panelSkills); got != 3 {
		t.Errorf("panelRowCount(skills) = %d, want 3", got)
	}
}

// TestSkillsPanelOpensUnselected pins that opening /skills leaves the cursor at
// -1 (the reference shows no "› " until the first ↓).
func TestSkillsPanelOpensUnselected(t *testing.T) {
	withSyntheticCatalog(t, cycSharedSkills(3))
	m := &runMode{}
	if !m.openSlashPanel("skills") {
		t.Fatal("openSlashPanel(skills) = false")
	}
	if m.panel.cursor != -1 {
		t.Errorf("cursor = %d, want -1", m.panel.cursor)
	}
}

// assertPanelSkillsLayout backs the delegation stub TestPanelSkillsLayout in
// slash_panels_test.go. It is host-tolerant: it uses the real catalogue and
// asserts the shape rather than the exact rows.
func assertPanelSkillsLayout(t *testing.T) {
	t.Helper()
	cat := skillsPanelCatalog()
	m := mkPanel(panelSkills, "/skills", -1) // height 0 → the 220x30 default
	plain := plainLines(m)
	if len(plain) < 5 {
		t.Fatalf("panel too short:\n%s", strings.Join(plain, "\n"))
	}
	if plain[0] != "Skills" {
		t.Errorf("title = %q", plain[0])
	}
	if want := fmt.Sprintf("%d skills", len(cat)); plain[1] != want {
		t.Errorf("subtitle = %q, want %q", plain[1], want)
	}
	joined := strings.Join(plain, "\n")
	if !strings.Contains(joined, "Create new skills") {
		t.Errorf("missing the create block:\n%s", joined)
	}
	if len(cat) == 0 {
		return
	}
	if !strings.Contains(joined, "Shared skills · ") {
		t.Errorf("missing the shared group header:\n%s", joined)
	}
	if n := len(newSkillsPanelModel().items); n > 11 {
		if want := fmt.Sprintf("  [1-11 of %d items]", n); !strings.Contains(joined, want) {
			t.Errorf("missing counter %q:\n%s", want, joined)
		}
	}
	last := stripSGR(plain[len(plain)-1])
	if !strings.Contains(last, "Navigate") || !strings.Contains(last, "View") {
		t.Errorf("legend = %q", last)
	}
}

// TestSkillsPanelLayout_HostCatalogue is the live-catalogue smoke test: on a
// host with the plugin bundle it asserts the reference arithmetic (60 skills,
// 66 items) and that no rendered line exceeds the window width.
func TestSkillsPanelLayout_HostCatalogue(t *testing.T) {
	cat := skillsPanelCatalog()
	if len(cat) == 0 {
		t.Skip("no skills installed on this host")
	}
	plugins := map[string]bool{}
	for _, s := range cat {
		if s.Builtin {
			plugins["<builtin>"] = true
		} else if s.Plugin != "" {
			plugins[s.Plugin] = true
		} else {
			plugins["<shared>"] = true
		}
	}
	if !plugins["science"] {
		t.Skip("plugin bundle not installed; reference set incomplete")
	}
	if got := len(cat); got != 60 && got != 66 {
		t.Errorf("skills = %d, want 60 or 66", got)
	}
	model := newSkillsPanelModel()
	expectedItems := len(cat) + len(model.groups)
	if got := len(model.items); got != expectedItems {
		t.Errorf("items = %d, want %d (%d skills + %d headers)", got, expectedItems, len(cat), len(model.groups))
	}
	if got := len(model.groups); got != 6 {
		t.Errorf("groups = %d, want 6", got)
	}

	m := mkPanel(panelSkills, "/skills", -1)
	m.height = 50
	m.width = 220
	for _, l := range m.panelLines() {
		if n := len([]rune(stripSGR(l))); n > m.width-1 && strings.Contains(stripSGR(l), ": ") {
			t.Errorf("row exceeds %d columns (%d): %q", m.width-1, n, stripSGR(l)[:40])
		}
	}
	joined := strings.Join(plainLines(m), "\n")
	expectedCounter := fmt.Sprintf("[1-26 of %d items]", expectedItems)
	if !strings.Contains(joined, expectedCounter) {
		t.Errorf("missing 220x50 counter:\n%s", joined)
	}
}
