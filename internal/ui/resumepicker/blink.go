package resumepicker

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// BlinkMsg is the picker's search-cursor blink.
//
// The reference blinks the cursor in the search field; the clone blinks
// it at the same period.
type BlinkMsg struct{}

// BlinkCmd schedules the next blink.
func BlinkCmd() tea.Cmd {
	return tea.Tick(530*time.Millisecond, func(time.Time) tea.Msg { return BlinkMsg{} })
}
