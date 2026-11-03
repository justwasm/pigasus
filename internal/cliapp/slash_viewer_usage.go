package cliapp

import (
	"time"

	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
)

// ----------------------------------------------------------------------
// /usage — REF 1.4.0 geometry
// ----------------------------------------------------------------------
//
// REF's /usage panel is three fixed rows of chrome (a blank, the account
// line, a blank) under the bold title, then a scrollable document. The
// document is 20 logical lines at a normal width: two model groups of six
// lines each plus two blanks, followed by a four-line note whose text is
// wrapped to min(width-4, 78). The window height is height-13 (see the
// derivation in usagepanel.Page), the counter quotes the *unwrapped* line
// numbers, and when the whole document fits the counter is omitted
// entirely. Recovered from usage24/28/40/44/50 and usage60/100 captures.
//
// Cycle 381: the document, the chrome, the geometry and the legend live
// in internal/ui/views/usagepanel. What stays here is the runMode-coupled
// plumbing: the quota state the panel reads, the scroll offset it
// clamps, and the height-0 fallback the unit tests render.

// panelUsageLinesREF renders /usage the way REF 1.4.0 lays it out. It is
// the height-aware sibling of panelUsageLines; the later falls back to the
// cycle-193 renderer while the terminal size is unknown (unit tests).
// Cycle 225: the document is built from the panel's state, so the loading
// doc (height-1) and the live refresh countdown (height-20) flow through
// the same key/scroll pipeline as the loaded doc.
func (m *runMode) panelUsageLinesREF() []string {
	// Cycle 381: the /usage panel owns its rows. The host hands over
	// the panel's facts and keeps the scroll offset the panel clamped.
	lines, window := usagepanel.Lines(m.usageState(), m.panel.window)
	m.panel.window = window
	return lines
}

// ----------------------------------------------------------------------
// /usage quota state — Cycle 225
// ----------------------------------------------------------------------
//
// REF 1.4.0 shows a one-line "Loading quota summary…" spinner with a Braille
// glyph in dim gray while a backend quota call is in flight, then the two
// model groups with the refresh countdown ticking down once a second. The
// clone has no live quota endpoint — the print-mode /usage reply in
// print_slash.go is a frozen placeholder — so this is a deterministic stub:
// the spinner runs for usageQuotaLoadingTicks (1.5 s at the 100 ms tick)
// before quotaStubRefills stamps 168h / 48h and flips state to quotaLoaded,
// and the two countdown strings tick every render.
//
// Keep this contained: nothing else reads m.panel.quota or m.quotaFrame.

// The loading document is the panel's own (usagepanel.LoadingDoc): one
// row, the Braille glyph and the dim-gray "Loading quota summary...".
// The refresh countdown is usagepanel.QuotaCountdown: it formats time.Until(t)
// as the "Nd Nh" / "Nh Nm" / "Nm Ns" string REF shows in the two refresh
// rows, and a duration that already rolled over clamps to "0m 0s".
//
// ----------------------------------------------------------------------

func (m *runMode) panelUsageLines() []string {
	// Cycle 207: with a real terminal size the panel uses REF 1.4.0's
	// title + chrome + window layout; the cycle-193 renderer below stays
	// the fallback while the size is unknown (the unit tests).
	//
	// Cycle 381: the fallback's rows, its counter and its legend live
	// in internal/ui/views/usagepanel; the host keeps the window offset
	// and the height-0 / height-aware distinction.
	if m.height > 0 {
		return m.panelUsageLinesREF()
	}
	spec, _ := m.getPanelRegistry().ViewerSpec(m.panel.kind)
	rows, start, end := m.viewerWindow(spec)
	return usagepanel.StaticLines(rows, start, end, spec.Total)
}

// The quota tick is the /usage panel's own fact: the message type, the
// command that arms it and the number of ticks the loading phase lasts
// all live in internal/ui/views/usagepanel (see quota.go). The host
// names the message type in its update switch, so the alias is the
// host's only contact with it.
type usageQuotaTickMsg = usagepanel.QuotaTickMsg

const usageQuotaLoadingTicks = usagepanel.QuotaLoadingTicks

func quotaStubRefills(m *runMode) {
	now := time.Now()
	m.panel.quota.loadedAt = now
	m.panel.quota.refreshG = now.Add(168 * time.Hour)
	m.panel.quota.refreshO = now.Add(48 * time.Hour)
	m.panel.quota.state = quotaLoaded
}
