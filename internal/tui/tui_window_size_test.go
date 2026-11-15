package tui

import (
	"testing"

	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

func TestWindowSizeMsg_IgnoreImplausibleSize(t *testing.T) {
	initialW, initialH := 80, 24
	m := &RootModel{
		viewport: uiState{
			width:  initialW,
			height: initialH,
		},
		prompt: PromptModel{input: tas.New()},
	}

	badSizes := []tea.WindowSizeMsg{
		{Width: -10, Height: 24},
		{Width: 80, Height: -5},
		{Width: 0, Height: 24},
		{Width: 80, Height: 0},
		{Width: -1, Height: -1},
		{Width: 2001, Height: 24},
		{Width: 50000, Height: 24},
		{Width: 80, Height: 1001},
		{Width: 80, Height: 99999},
	}

	for _, sz := range badSizes {
		m.Update(sz)
		if m.viewport.width != initialW || m.viewport.height != initialH {
			t.Errorf("WindowSizeMsg %+v corrupted viewport to %dx%d; want %dx%d",
				sz, m.viewport.width, m.viewport.height, initialW, initialH)
		}
	}

	// Valid size updates properly
	valid := tea.WindowSizeMsg{Width: 120, Height: 40}
	m.Update(valid)
	if m.viewport.width != 120 || m.viewport.height != 40 {
		t.Errorf("Valid WindowSizeMsg %+v failed to update viewport, got %dx%d",
			valid, m.viewport.width, m.viewport.height)
	}
}
