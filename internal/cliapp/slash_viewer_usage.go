package cliapp

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/pluginsui"
	"github.com/justwasm/pigasus/internal/tuiviewer"
	"github.com/justwasm/pigasus/internal/usagepanel"
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
// derivation in usagePage), the counter quotes the *unwrapped* line
// numbers, and when the whole document fits the counter is omitted
// entirely. Recovered from usage24/28/40/44/50 and usage60/100 captures.
//
// The document, chrome and geometry rules themselves live in
// internal/usagepanel; what stays here is the runMode-coupled window and
// scroll plumbing.

// usageNoteText is the group note. REF wraps it; the clone stores it
// unwrapped and wraps on render. Transcribed from
// ref_usage_220x50.raw.txt rows 33-36 / usage24.ref.raw.txt row 16.
const usageNoteText = usagepanel.NoteText

// usageNoteWrap caps the note's wrap width. The measured captures pin it
// to the range [76,79]: at w=60 the note wraps at width-4 == 56; at w=100
// and w=220 the same lines appear, so the width-4 value (96, 216) is capped.
// The live 1.4.3 note (which adds the 5-hour clause) pins it to 77: line 4
// ends at "capacity" and "across" starts line 5. 【一手】
const usageNoteWrap = usagepanel.NoteWrap

// usageTitle is the fixed bold title row.
func usageTitle() string {
	return usagepanel.Title()
}

// usageChrome is the three fixed rows between the title and the scrolling
// document: a blank, the account line (brand blue email), a blank.
func usageChrome(email string) []string {
	return usagepanel.Chrome(email)
}

// usageWrapWidth is the note's wrap width: width-4, capped at usageNoteWrap.
func usageWrapWidth(width int) int {
	return usagepanel.WrapWidth(width)
}

// usageGroup renders one model group: a bold header, the dim-gray member
// list, a blank, the (indented) "Weekly Limit Remaining" header, the bar
// and the coloured "Refreshes in ..." line.
//
// The percentages are the clone's frozen mock values (100.00% / 0.00%);
// REF's are live and move every run, so the *values* cannot be matched —
// only the structure. The refresh durations are REF's captured values
// (158h 18m / 64h 47m) frozen for the same reason. 【推测】(values only)
func usageGroup(name, members string, pct, fillCells int, refreshColor, refresh string) []string {
	return usagepanel.Group(name, members, pct, fillCells, refreshColor, refresh)
}

// usageDocSummary renders a live retrieveUserQuotaSummary response in the
// 1.4.3 layout: each group carries both its weekly and 5-hour buckets,
// with two blank rows before the next group and before the note.
func usageDocSummary(s *auth.QuotaSummary, width int) []string {
	return usagepanel.DocSummary(s, width)
}

// usagePage is the number of document lines REF shows at once. REF lays the
// panel out as title + 3 chrome rows + window + (counter) + blank + legend
// and starts the panel ten rows above the bottom of the screen, which works
// out to height-13: 11 rows at h=24, 15 at h=28, 20 at h=33 (the last height
// that still shows a counter). Recovered from the h=24/28/40/44/50 captures.
func usagePage(height int) int {
	return usagepanel.Page(height)
}

// panelUsageLinesREF renders /usage the way REF 1.4.0 lays it out. It is
// the height-aware sibling of panelUsageLines; the later falls back to the
// cycle-193 renderer while the terminal size is unknown (unit tests).
// Cycle 225: the document is now built by usageDocLive, so the loading
// doc (height-1) and the live refresh countdown (height-20) flow through
// the same key/scroll pipeline as the loaded doc.
func (m *runMode) panelUsageLinesREF() []string {
	doc := usageDocLive(m)
	total := len(doc)
	page := usagePage(m.height)
	maxOff := total - page
	if maxOff < 0 {
		maxOff = 0
	}
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if m.panel.window > maxOff {
		m.panel.window = maxOff
	}
	off := m.panel.window
	end := off + page
	if end > total {
		end = total
	}
	lines := []string{usageTitle()}
	lines = append(lines, usageChrome(m.email)...)
	lines = append(lines, doc[off:end]...)
	if page < total {
		lines = append(lines, viewerCounter(off+1, off+page, total))
	}
	lines = append(lines, "",
		panelDotHint("  ", "↑/↓", "Scroll", "pgup/pgdown", "Page",
			"ctrl+end", "Bottom", "ctrl+home", "Top", "esc", "Close"))
	return lines
}

// usageViewerSpecREF is the viewerSpec the key handler scrolls for a
// height-aware /usage panel: the document is the whole (chrome-less) doc
// and one window is usagePage rows. Cycle 225: backed by usageDocLive so
// the loading/loaded states scroll the same way.
func (m *runMode) usageViewerSpecREF() viewerSpec {
	doc := usageDocLive(m)
	return viewerSpec{lines: doc, visible: usagePage(m.height), total: len(doc)}
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

// usageDocLive is the height-aware /usage scrollable document. It reads
// m.panel.quota.state (set by openSlashPanel and by the tick handler)
// and returns either the one-row loading doc or the 20-row doc with
// live countdown strings. See usageLoadingDoc and quotaCountdown for
// the two pieces.
func usageDocLive(m *runMode) []string {
	if m.panel.quota.state == quotaLoading {
		return usageLoadingDoc(m)
	}
	if m.panel.quota.summary != nil {
		return usageDocSummary(m.panel.quota.summary, m.width)
	}
	doc := usageGroup("GEMINI MODELS", "Gemini Flash, Gemini Pro", 100, 50,
		"\x1b[32m", "Refreshes in "+tuiviewer.QuotaCountdown(m.panel.quota.refreshG))
	doc = append(doc, "", "")
	doc = append(doc, usageGroup("CLAUDE AND GPT MODELS", "Claude Opus, Claude Sonnet, GPT-OSS", 0, 50,
		"\x1b[31m", "Refreshes in "+tuiviewer.QuotaCountdown(m.panel.quota.refreshO))...)
	doc = append(doc, "", "")
	for _, l := range pluginsui.WrapWords(usageNoteText, usageWrapWidth(m.width)) {
		doc = append(doc, "  \x1b[38;2;238;232;213m│\x1b[38;2;101;123;131m"+l+"\x1b[39m")
	}
	return doc
}

// usageLoadingDoc is the document the height-aware panel shows while
// quota.state == quotaLoading. It is one row tall (Braille glyph + two
// spaces + dim gray "Loading quota summary…") and the spinner frame
// cycles through sp.Dot on every tick — see usageSpinnerFrame.
//
// Byte shape (captured from REF 1.4.0 against a live quota call that did
// not resolve during the 50 s probe window):
//
//	⣾  \x1b[38;2;101;123;131mLoading quota summary...\x1b[39m
//
// REF does not render a counter during the loading phase; the offline
// guard in panelUsageLinesREF renders a 1-row window against a 1-row
// doc, so no counter row appears.
func usageLoadingDoc(m *runMode) []string {
	return usagepanel.LoadingDoc(m.quotaFrame)
}

// usageSpinnerFrame picks the nth frame from sp.Dot, the same 8-frame
// Braille set the streaming spinner uses (sp.Dot: ⣾⣽⣻⢿⡿⣟⣯⣷). It is
// pure — no cap on n — so callers increment freely. Returning the glyph
// bare (two trailing spaces follow it in the row builder) keeps the row
// builder in one place for the height-0 test path that never sets
// quotaFrame.
func usageSpinnerFrame(n int) string {
	return usagepanel.SpinnerFrame(n)
}

// quotaCountdown formats time.Until(t) as the "Nd Nh" / "Nh Nm" /
// "Nm Ns" string REF shows in the two refresh rows. Negative durations
// clamp to zero so a bucket that already rolled over renders "0m 0s"
// rather than going backwards.
//
// The three forms are:
//
//	>= 24h: "Nd Nh"  (e.g. 6d 23h)
//	1h..24h: "Nh Nm"
//	<  1h: "Nm Ns"   (so the second-by-second tick is visible to the user)

// ----------------------------------------------------------------------

func (m *runMode) panelUsageLines() []string {
	// Cycle 207: with a real terminal size the panel uses REF 1.4.0's
	// title + chrome + window layout; the cycle-193 renderer below stays
	// the fallback while the size is unknown (the unit tests).
	if m.height > 0 {
		return m.panelUsageLinesREF()
	}
	spec, _ := m.viewerSpecFor()
	rows, start, end := m.viewerWindow(spec)
	lines := append([]string{}, rows...)
	lines = append(lines,
		"",
		viewerCounter(start, end, spec.total),
		"",
		panelDotHint("  ", "↑/↓", "Scroll", "pgup/pgdown", "Page",
			"ctrl+end", "Bottom", "ctrl+home", "Top", "esc", "Close"),
	)
	return lines
}

// usageViewerLines is the /usage viewer's scrollable document. Cycle 225:
// when m.height > 0 the live terminal path delegates to usageDocLive in
// slash_viewer_panels.go so the loading doc and the live refresh countdown
// render. The height-0 fallback (unit tests) keeps the cycle-193 layout
// byte-exact via usageViewerLinesStatic; this matters for
// TestPanelUsageLayout / TestCycle207UsageDocGeometry.
func (m *runMode) usageViewerLines() []string {
	if m.height > 0 {
		return usageDocLive(m)
	}
	return usageViewerLinesStatic()
}

// usageViewerLinesStatic is the cycle-193/201 fallback used by every
// panelUsage-line test. The first 17 rows are byte-identical to the
// cycle-193 render (transcribed from ppref.usage.raw.txt); the trailing
// three rows are the wrapped tail of the group note REF clips behind its
// window — reconstructed, not captured. 【推测】
func usageViewerLinesStatic() []string {
	return usagepanel.ViewerLinesStatic()
}

type usageQuotaTickMsg = tuiviewer.UsageQuotaTickMsg

func compactSpinnerTick() tea.Cmd {
	return tuiviewer.CompactSpinnerTick()
}

func usageQuotaTick() tea.Cmd {
	return tuiviewer.UsageQuotaTick()
}

const usageQuotaLoadingTicks = tuiviewer.UsageQuotaLoadingTicks

func quotaStubRefills(m *runMode) {
	now := time.Now()
	m.panel.quota.loadedAt = now
	m.panel.quota.refreshG = now.Add(168 * time.Hour)
	m.panel.quota.refreshO = now.Add(48 * time.Hour)
	m.panel.quota.state = quotaLoaded
}

