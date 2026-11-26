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

// QuotaCountdown formats the time left until t as the "Nd Nh Nm" /
// "Nh Nm" / "Nm Ns" string the reference shows in the two refresh rows.
//
// A duration that already rolled over clamps to zero, so a bucket that
// has passed its refresh time renders "0s" rather than going backwards.
//
// AGY 1.5.17 changed the long form. Changelog 1.5.17: "Improved `/usage`
// to format quota refresh countdowns of 24 hours or longer in days,
// hours, and minutes (such as `in 3d 3h 59m` instead of `in 75h 59m`)."
// The reference's own literal is the format string `%dd %dh %dm`, which
// is present in the 1.5.17 binary (offset 0x3b12e8d, immediately after
// the /usage panel's `Account: ` row and its neighbours) and absent from
// the 1.5.16 binary, whose only countdown form is `%dh %dm`. The 24-hour
// boundary is the changelog's own: at exactly 24h the day form starts, so
// 23h 59m still renders "23h 59m" and 24h renders "1d 0h 0m".
func QuotaCountdown(t time.Time) string {
	return QuotaCountdownFor(time.Until(t))
}

// QuotaCountdownFor formats d as the reference's countdown string. It is
// the whole of QuotaCountdown minus the clock read, so the 24-hour
// boundary can be pinned exactly instead of through a time.Now() that
// loses sub-second precision between the two calls.
func QuotaCountdownFor(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int(d / time.Second)
	minutes := (total % 3600) / 60
	seconds := total % 60
	switch {
	case total >= 24*3600:
		return fmt.Sprintf("%dd %dh %dm", total/86400, (total%86400)/3600, minutes)
	case total >= 3600:
		return fmt.Sprintf("%dh %dm", total/3600, minutes)
	case total >= 60:
		return fmt.Sprintf("%dm", minutes)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}
