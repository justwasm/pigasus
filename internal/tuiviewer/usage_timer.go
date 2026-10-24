// Package tuiviewer provides generic scrollable viewer panel windows,
// counters, timer countdowns, and inline diff views.
package tuiviewer

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

// UsageQuotaLoadingTicks is the number of 100 ms ticks the loading phase lasts.
const UsageQuotaLoadingTicks = 15

// UsageQuotaTickMsg fires every 100 ms while /usage is open.
type UsageQuotaTickMsg struct{}

// CompactSpinnerTickMsg fires every 100 ms while auto-compaction is running.
type CompactSpinnerTickMsg struct{}

// QuotaCountdown formats remaining time until t into human-readable duration.
func QuotaCountdown(t time.Time) string {
	d := time.Until(t)
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	minutes := (total % 3600) / 60
	seconds := total % 60
	switch {
	case total >= 3600:
		return fmt.Sprintf("%dh %dm", total/3600, minutes)
	case total >= 60:
		return fmt.Sprintf("%dm", minutes)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// UsageQuotaTick arms the next UsageQuotaTickMsg.
func UsageQuotaTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return UsageQuotaTickMsg{} })
}

// CompactSpinnerTick arms the next CompactSpinnerTickMsg.
func CompactSpinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return CompactSpinnerTickMsg{} })
}
