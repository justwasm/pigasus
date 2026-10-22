package main

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
)

func quotaCountdown(t time.Time) string {
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

// usageQuotaTickMsg fires every 100 ms while /usage is open. It
// advances the spinner frame, transitions quotaLoading → quotaLoaded
// after usageQuotaLoadingTicks, and re-arms itself via the cmd return
// (the height-aware dispatch reads m.panel.kind to know whether the
// panel is the /usage one). See also: usageQuotaTick in tui_run.go.
//
// Subsequent ticks keep advancing m.quotaFrame so the loaded doc's
// countdown can re-render at the same cadence (the live render path
// reads time.Until() on every draw, so a per-second flash is enough;
// the 100 ms cadence matches the REF spinner and keeps the digit
// changes close enough to feel live).
type usageQuotaTickMsg struct{}

// compactSpinnerTick fires every 100 ms while auto-compaction is
// running. Cycle 227: the compact message handler arms it in
// tui_run.go's compactStartMsg branch and stops re-arming once a
// compactEndMsg clears the active flag, so the spinner only spins
// while PiG is actually summarising. The frame set is the same
// sp.Dot the streaming spinner uses.
func compactSpinnerTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return compactSpinnerTickMsg{} })
}

// usageQuotaTick arms the next usageQuotaTickMsg. The cycle-225 cadence
// is 100 ms, fast enough that the spinner looks continuous but slow
// enough that the digit-by-digit countdown only repaints once per
// render. The bubbletea tick returns a tea.Msg; we ignore the time
// argument.
func usageQuotaTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return usageQuotaTickMsg{} })
}

// usageQuotaLoadingTicks is the number of 100 ms ticks the loading
// phase lasts. 15 × 100 ms = 1.5 s, long enough for one full
// sp.Dot cycle (8 frames × 150 ms) plus the first half of the next,
// and close to the elapsed time REF showed during the loading phase in
// the few captures where a backend quota call did resolve. 【推测】
const usageQuotaLoadingTicks = 15

// quotaStubRefills stamps m.panel.quota.refreshG / refreshO with two
// stub refill times (168 h / 48 h from now) and flips state to
// quotaLoaded. The values are deterministic so the probe-vs-clone
// tables in the cycle-225 deliverable stay reproducible.
//
// Cycle 225 note: there is no live quota endpoint in internal/backend
// or internal/piagentbridge. The print-mode /usage reply in
// print_slash.go is a frozen placeholder. Wiring a real fetch is
// outside the cycle-225 scope; flip this stub when a real quota
// service lands.
func quotaStubRefills(m *runMode) {
	now := time.Now()
	m.panel.quota.loadedAt = now
	m.panel.quota.refreshG = now.Add(168 * time.Hour)
	m.panel.quota.refreshO = now.Add(48 * time.Hour)
	m.panel.quota.state = quotaLoaded
}
