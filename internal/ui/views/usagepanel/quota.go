// quota.go — the /usage panel's quota tick and its refresh countdown.
//
// # Where the facts live
//
// The panel's spinner ticks every 100 ms while the quota call is in
// flight, and the two refresh rows count down in the same units. Both
// are the panel's own facts, so they live here and not in a generic
// viewer package: the tick's message type and the countdown's format
// are the /usage panel's, and a package that held them for three
// different panels was a package with no owner.
//
// The tick's message type is exported because the host's update switch
// names it — a tick command that produces a message the switch does not
// handle is a spinner that never moves.

package usagepanel

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

// QuotaLoadingTicks is the number of 100 ms ticks the loading phase lasts.
const QuotaLoadingTicks = 15

// QuotaTickMsg fires every 100 ms while /usage is open.
type QuotaTickMsg struct{}

// QuotaTick arms the next QuotaTickMsg.
func QuotaTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return QuotaTickMsg{} })
}

// QuotaCountdown formats the time left until t as the "Nd Nh" / "Nh Nm"
// / "Nm Ns" string the reference shows in the two refresh rows.
//
// A duration that already rolled over clamps to zero, so a bucket that
// has passed its refresh time renders "0s" rather than going backwards.
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
