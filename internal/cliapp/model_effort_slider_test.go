// Tests for the /model panel's effort slider on a model that declares
// more tiers than the reference's own slider has places.
//
// The reference's picker shows Gemini 3.8 Flash as one row with its
// three tiers on the slider underneath (fin_model_down.raw.txt). A model
// the user configured gets the same shape: one row, the tiers on the
// slider, widened to fit the tier count the configuration declares.

package cliapp

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/configtest"
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

// moveCursorTo drives the panel's ↓ key until the cursor is on the row
// with the given slug.
func moveCursorTo(t *testing.T, m *runMode, slug string) {
	t.Helper()
	for i := 0; i < 32; i++ {
		for _, r := range m.modelRows() {
			if r.Slug == slug && m.panel.cursor == indexOfRow(m, slug) {
				return
			}
		}
		if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
			break
		}
	}
	t.Fatalf("could not move the cursor to %q", slug)
}

// indexOfRow returns the index of the row with the given slug.
func indexOfRow(m *runMode, slug string) int {
	for i, r := range m.modelRows() {
		if r.Slug == slug {
			return i
		}
	}
	return -1
}

// TestModelPanel_TieredModelIsOneRow pins the shape: the configured
// model is one row, not one row per tier.
func TestModelPanel_TieredModelIsOneRow(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	cycle196TestHome(t, map[string]any{"model": "MiniMax M3.1 Flash (Medium)"})

	rows := (&runMode{}).modelRows()
	count := 0
	for _, r := range rows {
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

// TestModelPanel_TieredModelShowsTheSlider pins that the slider appears
// on the configured model's row, with one marker per tier, and that the
// row carries the "(current)" annotation.
func TestModelPanel_TieredModelShowsTheSlider(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	cycle196TestHome(t, map[string]any{"model": "MiniMax M3.1 Flash (Medium)"})

	m := mkPanel(panelModel, "/model", 0)
	m.height = 0
	moveCursorTo(t, m, "minimax-m3.1-flash")

	lines := m.panelLines()
	joined := stripSGR(strings.Join(lines, "\n"))

	// The row itself carries the model name and no tier in brackets.
	row := ""
	for _, ln := range lines {
		if strings.Contains(ln, "MiniMax M3.1 Flash") {
			row = stripSGR(ln)
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

// TestModelPanel_TieredModelCyclesEveryTier pins the ←/→ behaviour on a
// five-tier row: the slider wraps over the model's own tiers and the
// tier rides the persisted catalogue id.
func TestModelPanel_TieredModelCyclesEveryTier(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	dir := cycle196TestHome(t, map[string]any{"model": "MiniMax M3.1 Flash (Medium)"})

	m := mkPanel(panelModel, "/model", 0)
	m.height = 0
	moveCursorTo(t, m, "minimax-m3.1-flash")

	// The slider parks on the running model's own tier (medium).
	if m.panel.effortCursor != 1 {
		t.Fatalf("the slider parked at %d, want 1 (medium)", m.panel.effortCursor)
	}

	want := []struct {
		dir  int
		slug string
	}{
		{+1, "minimax-m3.1-flash-high"},
		{+1, "minimax-m3.1-flash-xhigh"},
		{+1, "minimax-m3.1-flash-max"},
		{+1, "minimax-m3.1-flash-low"}, // wrap
		{-1, "minimax-m3.1-flash-max"}, // wrap back
	}
	for i, s := range want {
		m.cycleModelEffort(s.dir)
		if got := cycle196ValueAt(t, readCycle196Settings(t, dir), "model"); got != s.slug {
			t.Errorf("step %d (%+d): persisted model = %q, want %q", i, s.dir, got, s.slug)
		}
	}
	if m.model != "MiniMax M3.1 Flash (Max)" {
		t.Errorf("the banner model = %q, want %q", m.model, "MiniMax M3.1 Flash (Max)")
	}
	if m.statusModel != "MiniMax M3.1 Flash" {
		t.Errorf("the status model = %q, want %q", m.statusModel, "MiniMax M3.1 Flash")
	}
}

// TestModelPanel_TierlessModelHasNoSlider pins that a model that
// declares no tiers keeps no slider, and that ←/→ stays inert on it.
func TestModelPanel_TierlessModelHasNoSlider(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	dir := cycle196TestHome(t, map[string]any{"model": "MiniMax M3"})

	m := mkPanel(panelModel, "/model", 0)
	m.height = 0
	moveCursorTo(t, m, "minimax-m3")

	if m.modelRowHasEffortSlider() {
		t.Error("a model without tiers must not show the effort slider")
	}
	before := readCycle196Settings(t, dir)
	m.cycleModelEffort(+1)
	after := readCycle196Settings(t, dir)
	if !mapsEqualString(before, after) {
		t.Errorf("←/→ mutated settings.json on a tierless row: %v → %v", before, after)
	}
}
