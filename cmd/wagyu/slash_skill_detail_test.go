// slash_cycle211_test.go — cycle 211: SKILL.md detail viewer.
//
// Cycle 203 backfilled the /skills picker and shipped a minimal "title +
// description + path + esc Back" place-holder for the enter-View detail
// panel — the reference's real screen is a scrollable viewer over the
// skill's SKILL.md document. Cycle 211 replaces the place-holder with that
// viewer and pins its geometry, body rendering and key handling here.
//
// The viewer-rendering tests exercise a synthetic catalogue row (with
// Path pointing at a t.TempDir()-hosted SKILL.md) so the assertions never
// depend on the host's ~/.gemini tree.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

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
	t.Run("HostCatalogOpensDetail", TestSkillsDetail_OpensViaHandlePanelKey)
}

// detailRunner sets up a runMode with the given height/width and a single
// synthetic catalogue row pointing at path. cursor is parked on that row
// so skillsDetailSelected() returns the fixture.
func detailRunner(t *testing.T, height, width int, path, name, desc string) *runMode {
	t.Helper()
	withSyntheticCatalog(t, []jsonSkillInfo{{
		Name: name, Description: desc, Path: path,
		Builtin: false, ModelInvocable: true,
	}})
	m := &runMode{}
	m.panel = slashPanel{kind: panelSkills, cmd: "/skills", cursor: 0}
	m.height = height
	m.width = width
	return m
}

// detailRunnerAtCursor parks the detail cursor on entry idx.
func detailRunnerAtCursor(t *testing.T, height, width int, cat []jsonSkillInfo, idx int) *runMode {
	t.Helper()
	withSyntheticCatalog(t, cat)
	m := &runMode{}
	m.panel = slashPanel{kind: panelSkills, cmd: "/skills", cursor: idx}
	m.height = height
	m.width = width
	return m
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
		got := skillsDetailVisible(c.h)
		if got != c.expected {
			t.Errorf("skillsDetailVisible(%d) = %d, want %d", c.h, got, c.expected)
		}
	}
}

// TestSkillsDetailVisible_Formula checks the formula on a clean height
// (no floor interference) — vis = h*3/4 - 11.
func TestSkillsDetailVisible_Formula(t *testing.T) {
	// 80*3/4 - 11 = 49 — REF h=80 showed vis=49.
	if got := skillsDetailVisible(80); got != 49 {
		t.Errorf("skillsDetailVisible(80) = %d, want 49", got)
	}
	// 100*3/4 - 11 = 64 — REF h=100 showed vis=64.
	if got := skillsDetailVisible(100); got != 64 {
		t.Errorf("skillsDetailVisible(100) = %d, want 64", got)
	}
	// height ≤ 0 falls back to the 30-row default → 30*3/4 - 11 = 11.
	if got := skillsDetailVisible(0); got != 11 {
		t.Errorf("skillsDetailVisible(0) = %d, want 11", got)
	}
}

// ---- document rendering -----------------------------------------------------

// TestSkillsDetailDocument_Header pins the first four logical lines:
// title (panelTitleOpen/panelTitleClose), description (the panelGray238
// rows), path (cDimGray), blank, then the H1 (whose bg-coloured marker
// pad adds two leading spaces and one trailing space around the text).
func TestSkillsDetailDocument_Header(t *testing.T) {
	body := "# T\n\nplain body line.\n"
	p := writeSkillMD(t, body)
	m := detailRunner(t, 50, 50, p, "demo", "a one-line description")

	doc := m.skillsDetailDocument(jsonSkillInfo{
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
	m := detailRunner(t, 50, 50, p, "demo", "desc text")

	doc := m.skillsDetailDocument(jsonSkillInfo{
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
	m := detailRunner(t, 50, 80, p, "demo", "d")

	doc := m.skillsDetailDocument(jsonSkillInfo{Name: "demo", Path: p})
	// Find the row carrying the H1 logical line — it's the only one
	// whose raw render uses the mdH1Open palette.
	for i, l := range doc {
		for _, row := range l.rows {
			if strings.Contains(row, "Title Here") {
				if !strings.Contains(row, mdH1Open) {
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
	m := detailRunner(t, 50, 80, p, "demo", "d")

	doc := m.skillsDetailDocument(jsonSkillInfo{Name: "demo", Path: p})
	for _, l := range doc {
		for _, row := range l.rows {
			if strings.Contains(row, "## Subhead") {
				if !strings.Contains(row, mdH2Open) {
					t.Errorf("H2 row missing the bold-blue palette: %q", row)
				}
				if strings.Contains(row, mdH1Open) {
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
	m := detailRunner(t, 50, 80, p, "demo", "d")

	doc := m.skillsDetailDocument(jsonSkillInfo{Name: "demo", Path: p})
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
	m := detailRunner(t, 50, 80, p, "demo", "d")

	doc := m.skillsDetailDocument(jsonSkillInfo{Name: "demo", Path: p})
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
	m := detailRunner(t, 50, 80, p, "demo", "d")

	doc := m.skillsDetailDocument(jsonSkillInfo{Name: "demo", Path: p})
	for _, l := range doc {
		for _, row := range l.rows {
			if strings.Contains(row, "agy -p") {
				if !strings.Contains(row, mdCodeOpen) {
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
	m := detailRunner(t, 50, 80, p, "demo", "d")

	doc := m.skillsDetailDocument(jsonSkillInfo{Name: "demo", Path: p})
	for _, l := range doc {
		for _, row := range l.rows {
			if strings.Contains(row, "bold") {
				if !strings.Contains(row, mdBold) {
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
	m := detailRunner(t, 50, 80, p, "demo", "d")

	doc := m.skillsDetailDocument(jsonSkillInfo{Name: "demo", Path: p})
	for _, l := range doc {
		for _, row := range l.rows {
			if strings.Contains(row, mdHRText) {
				if !strings.Contains(row, mdHROpen) {
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
	m := detailRunner(t, 50, 80, p, "demo", "d")

	doc := m.skillsDetailDocument(jsonSkillInfo{Name: "demo", Path: p})
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
	m := detailRunner(t, 50, 80, p, "demo", "desc")

	m.panel.window = 0 // ensure we render the very top
	rows := m.skillsDetailViewLines()
	// viewerCounter wraps the value in cDimGrayOpen / cDimGrayClose;
	// confirm both the styled form and the plain digits are present.
	wantStyled := viewerCounter(1, 26, 88)
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
	m := detailRunner(t, 50, 80, p, "demo", "d")

	rows := m.skillsDetailViewLines()
	plain := joinStrip(rows)
	if strings.Contains(plain, "of ") && strings.Contains(plain, "lines)") {
		t.Errorf("short doc should not show a counter, got:\n%s", plain)
	}
}

// ---- scroll keys -----------------------------------------------------------

func TestSkillsDetailScroll_Down(t *testing.T) {
	m := longDetailRunner(t, 50, 80)
	if m.panel.window != 0 {
		t.Fatalf("window=%d, want 0", m.panel.window)
	}
	if h, _ := m.handleSkillsDetailKey(tea.Key{Code: tea.KeyDown}); !h {
		t.Fatal("down not handled")
	}
	if m.panel.window != 1 {
		t.Errorf("window=%d, want 1", m.panel.window)
	}
}

func TestSkillsDetailScroll_Up(t *testing.T) {
	m := longDetailRunner(t, 50, 80)
	m.panel.window = 5
	if h, _ := m.handleSkillsDetailKey(tea.Key{Code: tea.KeyUp}); !h {
		t.Fatal("up not handled")
	}
	if m.panel.window != 4 {
		t.Errorf("window=%d, want 4", m.panel.window)
	}
}

// TestSkillsDetailScroll_Clamp — Up at window=0 is a no-op; Down at the
// bottom stops at total - vis.
func TestSkillsDetailScroll_Clamp(t *testing.T) {
	m := longDetailRunner(t, 50, 80)
	// Floor test — pressing Up at window=0 keeps window=0.
	m.handleSkillsDetailKey(tea.Key{Code: tea.KeyUp})
	if m.panel.window != 0 {
		t.Errorf("up at 0: window=%d, want 0", m.panel.window)
	}
	// Run down until clamp.
	for i := 0; i < 1000; i++ {
		m.handleSkillsDetailKey(tea.Key{Code: tea.KeyDown})
	}
	vis := skillsDetailVisible(m.height)
	doc := m.skillsDetailDocument(m.skillsDetailSelected())
	maxOff := len(doc) + 1 - vis
	if maxOff < 0 {
		maxOff = 0
	}
	if m.panel.window != maxOff {
		t.Errorf("clamp: window=%d, want %d", m.panel.window, maxOff)
	}
}

// TestSkillsDetailScroll_Page — PgDown steps by vis-1 (one less than the
// window — REF's page rule). PgUp steps up by the same amount.
func TestSkillsDetailScroll_Page(t *testing.T) {
	m := longDetailRunner(t, 50, 80)
	vis := skillsDetailVisible(m.height)
	page := vis - 1
	if page < 1 {
		page = 1
	}

	if h, _ := m.handleSkillsDetailKey(tea.Key{Code: tea.KeyPgDown}); !h {
		t.Fatal("pgdown not handled")
	}
	if m.panel.window != page {
		t.Errorf("after pgdown: window=%d, want %d", m.panel.window, page)
	}
	if h, _ := m.handleSkillsDetailKey(tea.Key{Code: tea.KeyPgUp}); !h {
		t.Fatal("pgup not handled")
	}
	if m.panel.window != 0 {
		t.Errorf("after pgup: window=%d, want 0", m.panel.window)
	}
}

// TestSkillsDetailScroll_CtrlHomeEnd — ctrl+home parks at the top;
// ctrl+end jumps to maxOffset.
func TestSkillsDetailScroll_CtrlHomeEnd(t *testing.T) {
	m := longDetailRunner(t, 50, 80)
	m.panel.window = 5
	if h, _ := m.handleSkillsDetailKey(tea.Key{
		Code: tea.KeyHome, Mod: tea.ModCtrl,
	}); !h {
		t.Fatal("ctrl-home not handled")
	}
	if m.panel.window != 0 {
		t.Errorf("ctrl-home: window=%d, want 0", m.panel.window)
	}

	vis := skillsDetailVisible(m.height)
	doc := m.skillsDetailDocument(m.skillsDetailSelected())
	maxOff := len(doc) + 1 - vis
	if maxOff < 0 {
		maxOff = 0
	}
	if h, _ := m.handleSkillsDetailKey(tea.Key{
		Code: tea.KeyEnd, Mod: tea.ModCtrl,
	}); !h {
		t.Fatal("ctrl-end not handled")
	}
	if m.panel.window != maxOff {
		t.Errorf("ctrl-end: window=%d, want %d", m.panel.window, maxOff)
	}
}

// TestSkillsDetailScroll_EscReturnsToList — Esc from the detail view
// flips m.panel.detail back to false but keeps m.panel.kind == panelSkills.
// The transcript is left untouched.
func TestSkillsDetailScroll_EscReturnsToList(t *testing.T) {
	m := detailRunner(t, 50, 80, writeSkillMD(t, "# T\n\nbody.\n"), "demo", "d")
	m.panel.detail = true
	if h, _ := m.handleSkillsDetailKey(tea.Key{Code: tea.KeyEsc}); !h {
		t.Fatal("esc not handled")
	}
	if m.panel.detail {
		t.Error("esc should close the detail sub-view")
	}
	if m.panel.kind != panelSkills {
		t.Errorf("esc should keep panelSkills open, got %v", m.panel.kind)
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
	m := detailRunner(t, 50, 80, p, "demo", "d")
	rows := m.skillsPanelDetailLines()
	plain := joinStrip(rows)

	if !strings.Contains(plain, "demo") {
		t.Errorf("viewer should show the catalogue name:\n%s", plain)
	}
	if strings.Contains(plain, "Skills") || strings.Contains(plain, "panelTitleOpen") {
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
	withSyntheticCatalog(t, []jsonSkillInfo{{Name: "demo", Description: "d"}})
	m := &runMode{}
	m.panel = slashPanel{kind: panelSkills, cmd: "/skills", cursor: 0}
	m.height = 30
	m.width = 80

	rows := m.skillsPanelDetailLines()
	plain := joinStrip(rows)
	if firstNonBlank(plain) != "Skills" {
		t.Errorf("legacy shape should open with `Skills`, got:\n%s", plain)
	}
	if strings.Contains(plain, "↑/↓") {
		t.Errorf("legacy shape should not show the viewer legend:\n%s", plain)
	}
}

// TestSkillsDetail_OpensViaHandlePanelKey — Enter on a /skills panel row
// (with the host's catalogue) opens the detail view, and Esc returns to
// the list. The panel-level router handles both transitions.
func TestSkillsDetail_OpensViaHandlePanelKey(t *testing.T) {
	p := writeSkillMD(t, "# T\n\nbody.\n")
	withSyntheticCatalog(t, []jsonSkillInfo{{
		Name: "demo", Description: "d", Path: p,
	}})
	m := &runMode{}
	m.panel = slashPanel{kind: panelSkills, cmd: "/skills", cursor: 0}
	m.height = 50
	m.width = 80

	if h, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !h {
		t.Fatal("enter not handled")
	}
	if !m.panel.detail {
		t.Fatal("enter should open the detail view")
	}
	if h, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !h {
		t.Fatal("esc not handled")
	}
	if m.panel.detail || m.panel.kind != panelSkills {
		t.Fatalf("esc should return to the list: detail=%v kind=%v",
			m.panel.detail, m.panel.kind)
	}
}

// ---- helpers ---------------------------------------------------------------

// longDetailRunner sets up a 50-row × 80-col viewer whose SKILL.md has
// ~120 body lines — long enough to require at least one scroll step.
func longDetailRunner(t *testing.T, h, w int) *runMode {
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
	return detailRunner(t, h, w, p, "demo", "A description.")
}

// docToPlain collapses a document into plain text rows by stripping SGR
// from each rendered row.
func docToPlain(doc []skillsDetailLine) []string {
	var out []string
	for _, l := range doc {
		for _, row := range l.rows {
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
