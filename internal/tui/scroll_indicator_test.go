package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/palette"
)

func TestScrollPositionIndicator_1512(t *testing.T) {
	m := &RootModel{
		viewport: uiState{
			altScreen:      true,
			autoScroll:     false,
			width:          80,
			height:         24,
			scroll:         5,
			userPromptRows: []int{0, 10, 25},
			rowsView: composedRows{
				Rows: make([]string, 50),
			},
		},
	}
	// Populate dummy rows
	for i := range m.viewport.rowsView.Rows {
		m.viewport.rowsView.Rows[i] = "line"
	}

	// 1. autoScroll = true -> indicator should be empty
	m.viewport.autoScroll = true
	if got := m.scrollPositionIndicator(); got != "" {
		t.Errorf("autoScroll=true got %q, want empty", got)
	}
	m.viewport.autoScroll = false

	// 2. altScreen = false -> indicator should be empty
	m.viewport.altScreen = false
	if got := m.scrollPositionIndicator(); got != "" {
		t.Errorf("altScreen=false got %q, want empty", got)
	}
	m.viewport.altScreen = true

	// 3. scrolled up in altscreen:
	// total = 50 rows, height = 24.
	// promptRule(true) should contain the indicator and down arrow.
	rule := m.promptRule(true)
	stripped := ansi.Strip(rule)
	if !strings.Contains(stripped, "↓") {
		t.Errorf("promptRule(true) stripped = %q, want containing '↓'", stripped)
	}
	if !strings.Contains(stripped, "lines") {
		t.Errorf("promptRule(true) stripped = %q, want containing 'lines'", stripped)
	}
	if !strings.HasPrefix(rule, palette.DimGrayOpen) {
		t.Errorf("promptRule(true) = %q, want starting with DimGrayOpen", rule)
	}

	// 4. promptRule(false) should remain the bottom rule separator
	bottomRule := m.promptRule(false)
	if strings.Contains(ansi.Strip(bottomRule), "↓") {
		t.Errorf("promptRule(false) should not contain indicator: %q", bottomRule)
	}
}
