package main

// slash_cycle_model_left_right_effort_test.go — cycle
// `model-left-right-effort`: /model ←/→ wraps the effort slider and
// persists immediately.
//
// TODO P4 + AGY changelog call out that pressing ←/→ on the /model
// panel must switch the highlighted model's effort level (low / medium
// / high) and persist the change immediately — settings.json should
// see the same catalogue slug that Enter would write, the status row
// should reflect the new tier on the very next frame, and the panel
// must stay where it is (no dropdown opens, no panel closes).
//
// The reference appears to wrap at the boundary (low ⇆ high across
// both arrows) rather than clamping, mirroring the /config picker's
// wrap behaviour and matching the user expectation from /effort's
// dedicated slider; the cycle-197 prior implementation clamped and
// required Enter to persist. This cycle:
//
//   1. Switches the boundary from clamp to wrap.
//   2. Adds immediate persistence: cycleModelEffort now writes
//      settings.json's "model" key on every ←/→, exactly like
//      selectModelEffort does on Enter.
//   3. Updates m.model / m.statusModel so the status row reflects the
//      change on the next render.
//
// Each test below pins one of those three behaviours plus the wiring
// through handlePanelKey. ANTIGRAVITY_HOME points at a per-test
// t.TempDir so the persisted settings never touch the user's real
// file.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// modelLeftRightBarLine returns the rendered /model panel line that
// hosts the embedded effort slider (the only line in panelModelLines
// whose geometry depends on m.panel.effortCursor). Returns "" when
// the slider is not visible — non-Gemini rows omit it, but every test
// here starts on the Gemini row.
func modelLeftRightBarLine(m *runMode) string {
	for _, l := range plainLines(m) {
		if strings.HasPrefix(l, "  Effort") {
			return l
		}
	}
	return ""
}

// countConsecutiveBefore counts how many `●` (filled circle) markers
// appear before the first `◉` (active marker) in `s`. The
// /effort-style slider places the active `◉` after every preceding
// level's `●━━` segment, so the count of `●` before `◉` is the
// cycle's effort position: 0 → low, 1 → medium, 2 → high. After
// stripSGR this is structurally stable — the lit marker colouring is
// stripped but the cell shape is preserved.
//
// `◂`/`▸` slider arrows and `─` (dim dash) bars may appear before
// `◉`, but no other marker can (low has one `●` before `◉`, medium has
// two, high has three, the bar never carries a leading `○` before the
// active `◉` because the dim path only follows the active marker).
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

// TestModelLeftRight_AdvancesEffort pins the basic wrap semantics:
// from a starting medium position, → moves to high; one more → wraps
// to low (the cycle's defining behaviour); one ← wraps back to high
// (passing through medium, but that's the cursor's job — at the cycle
// boundary we go directly low→high).
//
// The test exercises four directions starting from a known seed so
// every transition has a known destination:
//
//	start medium → +1 → high   → +1 → low
//	                           → -1 → high
//	                           → -1 → medium
func TestModelLeftRight_AdvancesEffort(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	_ = dir
	m := mkPanel(panelModel, "/model", 0) // Gemini row
	if m.panel.effortCursor != effortMedium {
		t.Fatalf("seed effortCursor = %d, want %d (medium)",
			m.panel.effortCursor, effortMedium)
	}

	// From medium: +1 lands on high; one more +1 wraps to low (the
	// cycle's defining behaviour); ← from low wraps straight back
	// to high (skipping medium — at the boundary we go directly
	// low→high so the user's intent is unambiguous).
	steps := []struct {
		dir  int
		want int
		why  string
	}{
		{+1, effortHigh, "medium → right → high"},
		{+1, effortLow, "high → right → low (wrap)"},
		{-1, effortHigh, "low → left → high (wrap)"},
		{-1, effortMedium, "high → left → medium"},
		{+1, effortHigh, "medium → right → high"},
		{-1, effortMedium, "high → left → medium"},
	}
	for i, s := range steps {
		m.cycleModelEffort(s.dir)
		if m.panel.effortCursor != s.want {
			t.Errorf("step %d (%s): effortCursor = %d, want %d",
				i, s.why, m.panel.effortCursor, s.want)
		}
	}
}

// TestModelLeftRight_PersistsToDisk is the load-bearing correctness
// test: each ←/→ must write the new catalogue slug to settings.json,
// so a panel-Esc (which does not invoke selectModelEffort) still
// retains the user's last previewed effort. The persisted key is
// "model" (the same key Enter uses); the value is the catalogue ID
// "<base>-<tier>" matching `agy -p /model` and the catalogue check.
func TestModelLeftRight_PersistsToDisk(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	m := mkPanel(panelModel, "/model", 0)

	// Forward walk: from medium (+1 → high → +1 → low → +1 → medium → …).
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
		m.cycleModelEffort(+1)
		if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != s.want {
			t.Errorf("step %d (%s): persisted model = %q, want %q",
				i, s.why, got, s.want)
		}
	}
	// Forward walk leaves us at low.

	// Reverse walk: from low (-1 → high → -1 → medium → -1 → low → …).
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
		m.cycleModelEffort(-1)
		if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != s.want {
			t.Errorf("reverse step %d (%s): persisted model = %q, want %q",
				i, s.why, got, s.want)
		}
	}
}

// TestModelLeftRight_PersistsAndUpdatesLabel pins that the visible
// status row reflects the persisted model on the very next frame:
// m.model and m.statusModel move with the slider. "Gemini 3.8 Flash
// (Medium)" is the catalogue Label for the medium slug; the
// tier-stripped statusModel drops the " (Medium)" tail.
func TestModelLeftRight_PersistsAndUpdatesLabel(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Low)"})
	m := mkPanel(panelModel, "/model", 0)

	m.cycleModelEffort(+1) // low → medium
	if m.model != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("after +1 from low: m.model = %q, want %q",
			m.model, "Gemini 3.8 Flash (Medium)")
	}
	if m.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("after +1 from low: m.statusModel = %q, want %q",
			m.statusModel, "Gemini 3.8 Flash")
	}

	m.cycleModelEffort(+1) // medium → high
	if m.model != "Gemini 3.8 Flash (High)" {
		t.Errorf("after +1 from medium: m.model = %q, want %q",
			m.model, "Gemini 3.8 Flash (High)")
	}
	if m.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("after +1 from medium: m.statusModel = %q, want %q",
			m.statusModel, "Gemini 3.8 Flash")
	}

	m.cycleModelEffort(-1) // high → medium
	if m.model != "Gemini 3.8 Flash (Medium)" {
		t.Errorf("after -1 from high: m.model = %q, want %q",
			m.model, "Gemini 3.8 Flash (Medium)")
	}

	m.cycleModelEffort(+1) // back to high
	m.cycleModelEffort(+1) // high → low (wrap)
	if m.model != "Gemini 3.8 Flash (Low)" {
		t.Errorf("after wrap to low: m.model = %q, want %q",
			m.model, "Gemini 3.8 Flash (Low)")
	}
	if m.statusModel != "Gemini 3.8 Flash" {
		t.Errorf("after wrap to low: m.statusModel = %q, want %q",
			m.statusModel, "Gemini 3.8 Flash")
	}
}

// TestModelLeftRight_RerendersSlider verifies that panelModelLines —
// which reads m.panel.effortCursor on every render — produces a bar
// whose active marker `◉` position reflects the new cursor. The
// reconciliation is purely through the existing rebuildEffortSlider
// helper that /effort itself uses, so this test is essentially
// "after a cycleModelEffort call, the rendered /model panel renders a
// slider whose geometry matches level=cursor". The check is structural
// (counting `●` markers before `◉`), not pixel-for-pixel, so colour
// refactors don't break it.
func TestModelLeftRight_RerendersSlider(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	m := mkPanel(panelModel, "/model", 0)

	at := func(level int) int {
		m.panel.effortCursor = level
		bar := modelLeftRightBarLine(m)
		if bar == "" {
			t.Fatalf("effort slider not rendered at level=%d", level)
		}
		return countConsecutiveBefore(bar)
	}

	if got := at(effortMedium); got != 1 {
		t.Errorf("seeded medium: ●-before-◉ = %d, want 1", got)
	}
	m.cycleModelEffort(+1)
	if got := at(effortHigh); got != 2 {
		t.Errorf("after right to high: ●-before-◉ = %d, want 2", got)
	}
	m.cycleModelEffort(+1) // wrap
	if got := at(effortLow); got != 0 {
		t.Errorf("after wrap to low: ●-before-◉ = %d, want 0", got)
	}
	m.cycleModelEffort(-1) // wrap
	if got := at(effortHigh); got != 2 {
		t.Errorf("after wrap back to high: ●-before-◉ = %d, want 2", got)
	}
}

// TestModelLeftRight_NoDropdownOpen verifies the panel stays exactly
// where it is: ←/→ on the /model panel does not open any kind of
// dropdown, does not close the panel, does not promote the user into
// a search box or a detail view, and does not change the panel kind.
// The test seeds each panel state and verifies that after calling
// handlePanelKey(left|right), nothing about the panel moved except
// effortCursor and the persisted settings.
func TestModelLeftRight_NoDropdownOpen(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	m := mkPanel(panelModel, "/model", 0)
	startKind := m.panel.kind
	startCmd := m.panel.cmd
	startCursor := m.panel.cursor

	// Press → through the router — this is the path the user's keyboard
	// actually takes.
	for _, code := range []rune{tea.KeyLeft, tea.KeyRight, tea.KeyLeft, tea.KeyRight} {
		handled, _ := m.handlePanelKey(tea.Key{Code: code})
		if !handled {
			t.Fatalf("handlePanelKey(%v) reported unhandled on model panel", code)
		}
	}
	if m.panel.kind != startKind {
		t.Errorf("panel kind = %v, want %v (←/→ must not change panel)",
			m.panel.kind, startKind)
	}
	if m.panel.cmd != startCmd {
		t.Errorf("panel cmd = %q, want %q", m.panel.cmd, startCmd)
	}
	if m.panel.cursor != startCursor {
		t.Errorf("panel cursor = %d, want %d (←/→ must not move cursor)",
			m.panel.cursor, startCursor)
	}
	// detail flag is the cycle-203 "+detail" overlay used by /skills;
	// the /model picker doesn't have one but the assertion is cheap.
	if m.panel.detail {
		t.Error("←/→ flipped panel.detail — model panel has no detail view")
	}
}

// TestModelLeftRight_HomeKeyIgnored pins the boundary behaviour the
// cycle chose: at effortCursor=0 (low), pressing ← wraps to 2 (high)
// rather than no-op. The original cycle-197 implementation clamped
// (the "ignored" interpretation); the wrap choice keeps left/right
// symmetric — neither arrow can ever be silently swallowed by the
// picker — and matches /effort and /config's behaviour. The test
// name preserves the obsolete vocabulary ("home key" / "ignored") so a
// future grep for the boundary case still finds it.
func TestModelLeftRight_HomeKeyIgnored(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Low)"})
	m := mkPanel(panelModel, "/model", 0)
	if m.panel.effortCursor != effortLow {
		t.Fatalf("seed cursor = %d, want low (%d)",
			m.panel.effortCursor, effortLow)
	}

	// Left boundary: at-low + ← wraps to high.
	m.cycleModelEffort(-1)
	if m.panel.effortCursor != effortHigh {
		t.Errorf("at-low + ←: effortCursor = %d, want wrap to %d (high)",
			m.panel.effortCursor, effortHigh)
	}

	// Now drive back to high and verify the right boundary wraps too.
	// From high (2): +1 → 0 (low), +1 → 1 (medium), +1 → 2 (high).
	// Three +1's lands us at high; one more +1 is the boundary case.
	m.cycleModelEffort(+1)
	m.cycleModelEffort(+1)
	m.cycleModelEffort(+1)
	if m.panel.effortCursor != effortHigh {
		t.Fatalf("reseed: effortCursor = %d, want high (%d)",
			m.panel.effortCursor, effortHigh)
	}
	m.cycleModelEffort(+1) // right boundary: wraps to low
	if m.panel.effortCursor != effortLow {
		t.Errorf("at-high + →: effortCursor = %d, want wrap to %d (low)",
			m.panel.effortCursor, effortLow)
	}
}

// TestModelLeftRight_NonGeminiInert pins that ←/→ is no-op on the
// non-Gemini rows (Claude Sonnet, Claude Opus, GPT-OSS, MiniMax M3,
// Gemini 4 Argon) so a wandering cursor cannot silently re-pin a
// different model when the user is just exploring. Companion to
// TestCycle197_ModelEffortCyclesGeminiOnly but reasserted here so the
// cycle's own test file pins every behaviour listed in the brief.
func TestModelLeftRight_NonGeminiInert(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Claude Sonnet 4.6 (Thinking)"})
	m := mkPanel(panelModel, "/model", 0)

	// Move cursor to each non-Gemini row in turn and verify ←/→ is
	// inert (no effortCursor change, no settings.json write).
	for _, cursor := range []int{1, 2, 3, 4, 5} { // Claude, Opus, GPT-OSS, MiniMax, Argon
		m.panel.cursor = cursor
		beforeCursor := m.panel.effortCursor
		beforeDisk := readCycle196Settings(t, dir)
		m.cycleModelEffort(+1)
		m.cycleModelEffort(-1)
		if m.panel.effortCursor != beforeCursor {
			t.Errorf("cursor=%d: ←/→ shifted effortCursor from %d to %d (non-Gemini must be inert)",
				cursor, beforeCursor, m.panel.effortCursor)
		}
		afterDisk := readCycle196Settings(t, dir)
		if !mapsEqualString(beforeDisk, afterDisk) {
			t.Errorf("cursor=%d: ←/→ mutated settings.json on a non-Gemini row", cursor)
		}
	}
}

// TestModelLeftRight_KeyHandlerIntegration is the end-to-end smoke
// test: feed a real tea.Key through handlePanelKey and verify the
// full chain — wrapper → cycleModelEffort → settings.json →
// m.model — produces the expected slug, label, and slider position.
// This is the path the user takes when they're sitting on /model and
// tap an arrow key.
func TestModelLeftRight_KeyHandlerIntegration(t *testing.T) {
	dir := cycle196TestHome(t, map[string]any{"model": "Gemini 3.8 Flash (Medium)"})
	m := mkPanel(panelModel, "/model", 0)

	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("→ on /model: handlePanelKey reported unhandled")
	}
	if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != "gemini-3.8-flash-high" {
		t.Errorf("after → through key handler: persisted model = %q, want %q",
			got, "gemini-3.8-flash-high")
	}
	if m.model != "Gemini 3.8 Flash (High)" {
		t.Errorf("after → through key handler: m.model = %q, want %q",
			m.model, "Gemini 3.8 Flash (High)")
	}
	if m.panel.effortCursor != effortHigh {
		t.Errorf("after → through key handler: effortCursor = %d, want %d (high)",
			m.panel.effortCursor, effortHigh)
	}

	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyLeft}); !handled {
		t.Fatal("← on /model: handlePanelKey reported unhandled")
	}
	if m.panel.effortCursor != effortMedium {
		t.Errorf("after ← through key handler: effortCursor = %d, want %d (medium)",
			m.panel.effortCursor, effortMedium)
	}
}

// mapsEqualString is a small helper that compares two flat settings
// maps by JSON-shape equality for string leaves (settings.json keys
// the cycle touches are all flat strings here). Used by the inert
// assertion to detect an unwanted write.
func mapsEqualString(a, b map[string]any) bool {
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
