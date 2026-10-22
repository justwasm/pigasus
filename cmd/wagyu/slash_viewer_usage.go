package main

import (
	"fmt"
	"math"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
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

// usageNoteText is the group note. REF wraps it; the clone stores it
// unwrapped and wraps on render. Transcribed from
// ref_usage_220x50.raw.txt rows 33-36 / usage24.ref.raw.txt row 16.
const usageNoteText = "Within each group, models share a weekly limit. Quota is consumed proportionally to the cost of the tokens. Thus, limits will last longer with shorter tasks or using more cost-effective models. Your weekly limit is tied directly to your individual tier."

// usageNoteWrap caps the note's wrap width. The measured captures pin it
// to the range [76,79]: at w=60 the note wraps at width-4 == 56; at w=100
// and w=220 the same lines appear, so the width-4 value (96, 216) is capped.
// The live 1.4.3 note (which adds the 5-hour clause) pins it to 77: line 4
// ends at "capacity" and "across" starts line 5. 【一手】
const usageNoteWrap = 77

// usageTitle is the fixed bold title row.
func usageTitle() string {
	return "\x1b[1m" + panelGray238 + "└ Models & Quota" + "\x1b[0m"
}

// usageChrome is the three fixed rows between the title and the scrolling
// document: a blank, the account line (brand blue email), a blank.
func usageChrome(email string) []string {
	if email == "" {
		email = auth.EffectiveUserEmail()
	}
	if email == "" {
		email = "navigaid@gmail.com"
	}
	return []string{
		"",
		"  Account: " + cBlueOpen + email + cBlueClose,
		"",
	}
}

// usageWrapWidth is the note's wrap width: width-4, capped at usageNoteWrap.
func usageWrapWidth(width int) int {
	w := width - 4
	if w <= 0 || w > usageNoteWrap {
		w = usageNoteWrap
	}
	return w
}

// The greedy word-wrap primitive used below lives in slash_plugin_search.go
// (cycle 204's `wrapWords`). Cycles 204 and 207 developed one independently
// each; they were merged into the single helper in that file. Its contract is
// the union of both: it breaks on single spaces, fills to exactly `width`
// columns, emits an over-long word whole, and always returns at least one
// element (so `wrapWords("", n)` is `[""]`) — which the /plugin Discover
// detail block indexes unconditionally. For single-spaced input — both call
// sites here — the two implementations produced identical output.

// usageBar renders one quota bar: an opening bracket, the green filled
// portion, the dim-gray remainder, a reset and the closing bracket, then
// the percentage in dim gray. REF omits the green SGR when the fill is
// empty and omits the gray SGR when the bar is full.
// (ref_usage_220x50.raw.txt rows 21 and 29.)
func usageBar(pct float64, cells int) string {
	fill := int(math.Round(pct / 100.0 * float64(cells)))
	if fill < 0 {
		fill = 0
	}
	if fill > cells {
		fill = cells
	}
	var b strings.Builder
	b.WriteString("    [")
	if fill > 0 {
		b.WriteString("\x1b[32m")
		b.WriteString(strings.Repeat("█", fill))
	}
	if cells-fill > 0 {
		b.WriteString("\x1b[38;2;101;123;131m")
		b.WriteString(strings.Repeat("░", cells-fill))
	}
	b.WriteString("\x1b[39m]")
	b.WriteString("\x1b[38;2;101;123;131m")
	fmt.Fprintf(&b, " %.2f%%", pct)
	b.WriteString("\x1b[39m")
	return b.String()
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
	return []string{
		"\x1b[1m" + name + "\x1b[0m",
		"  " + cDimGrayOpen + "Models within this group: " + members + cDimGrayClose,
		"",
		"  \x1b[1mWeekly Limit Remaining\x1b[0m",
		usageBar(float64(pct), fillCells),
		"    " + refreshColor + refresh + "\x1b[39m",
	}
}

// usageDoc is the /usage scrollable document. It is 20 lines at a normal
// width (the note wrapping to four lines) and grows when a narrow terminal
// wraps the note further.
func usageDoc(width int) []string {
	doc := usageGroup("GEMINI MODELS", "Gemini Flash, Gemini Pro", 100, 50,
		"\x1b[32m", "Refreshes in 158h 18m")
	doc = append(doc, "", "")
	doc = append(doc, usageGroup("CLAUDE AND GPT MODELS", "Claude Opus, Claude Sonnet, GPT-OSS", 0, 50,
		"\x1b[31m", "Refreshes in 64h 47m")...)
	doc = append(doc, "", "")
	for _, l := range wrapWords(usageNoteText, usageWrapWidth(width)) {
		doc = append(doc, "  \x1b[38;2;238;232;213m│\x1b[38;2;101;123;131m"+l+"\x1b[39m")
	}
	return doc
}

// usageDocSummary renders a live retrieveUserQuotaSummary response in the
// 1.4.3 layout: each group carries both its weekly and 5-hour buckets,
// with two blank rows before the next group and before the note.
func usageDocSummary(s *auth.QuotaSummary, width int) []string {
	var doc []string
	for _, g := range s.Groups {
		doc = append(doc, "\x1b[1m"+strings.ToUpper(g.Name)+"\x1b[0m")
		if g.Description != "" {
			doc = append(doc, "  "+cDimGrayOpen+g.Description+cDimGrayClose)
		}
		for _, b := range g.Buckets {
			doc = append(doc, "", "  \x1b[1m"+b.DisplayName+"\x1b[0m")
			doc = append(doc, usageBar(b.RemainingFraction*100, 50))
			color := "\x1b[32m"
			if b.RemainingFraction <= 0 {
				color = "\x1b[31m"
			}
			if !b.ResetTime.IsZero() {
				doc = append(doc, "    "+color+"Refreshes in "+quotaCountdown(b.ResetTime)+"\x1b[39m")
			}
		}
		doc = append(doc, "", "")
	}
	note := s.Description
	if note == "" {
		note = usageNoteText
	}
	for _, l := range wrapWords(note, usageWrapWidth(width)) {
		doc = append(doc, "  \x1b[38;2;238;232;213m│\x1b[38;2;101;123;131m"+l+"\x1b[39m")
	}
	return doc
}

// usagePage is the number of document lines REF shows at once. REF lays the
// panel out as title + 3 chrome rows + window + (counter) + blank + legend
// and starts the panel ten rows above the bottom of the screen, which works
// out to height-13: 11 rows at h=24, 15 at h=28, 20 at h=33 (the last height
// that still shows a counter). Recovered from the h=24/28/40/44/50 captures.
func usagePage(height int) int {
	page := height - 13
	if page < 1 {
		page = 1
	}
	return page
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
		"\x1b[32m", "Refreshes in "+quotaCountdown(m.panel.quota.refreshG))
	doc = append(doc, "", "")
	doc = append(doc, usageGroup("CLAUDE AND GPT MODELS", "Claude Opus, Claude Sonnet, GPT-OSS", 0, 50,
		"\x1b[31m", "Refreshes in "+quotaCountdown(m.panel.quota.refreshO))...)
	doc = append(doc, "", "")
	for _, l := range wrapWords(usageNoteText, usageWrapWidth(m.width)) {
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
	return []string{
		"  " + usageSpinnerFrame(m.quotaFrame) + "  " +
			cDimGrayOpen + "Loading quota summary..." + cDimGrayClose,
	}
}

// usageSpinnerFrame picks the nth frame from sp.Dot, the same 8-frame
// Braille set the streaming spinner uses (sp.Dot: ⣾⣽⣻⢿⡿⣟⣯⣷). It is
// pure — no cap on n — so callers increment freely. Returning the glyph
// bare (two trailing spaces follow it in the row builder) keeps the row
// builder in one place for the height-0 test path that never sets
// quotaFrame.
func usageSpinnerFrame(n int) string {
	frames := []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	if n < 0 {
		n = 0
	}
	return frames[n%len(frames)]
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
func usageGeminiGroupRows() []string {
	return []string{
		"\x1b[1m" + "GEMINI MODELS" + "\x1b[0m",
		cDimGrayOpen + "  Models within this group: Gemini Flash, Gemini Pro" + cDimGrayClose,
		"",
		"\x1b[1mWeekly Limit Remaining\x1b[0m",
		"    " + "\x1b[32m" + strings.Repeat("█", 50) + "\x1b[39m" +
			cDimGrayOpen + " 100.00%" + cDimGrayClose,
		"\x1b[32mQuota available\x1b[39m",
	}
}

func usageClaudeGroupRows() []string {
	return []string{
		"\x1b[1m" + "CLAUDE AND GPT MODELS" + "\x1b[0m",
		cDimGrayOpen + "  Models within this group: Claude Opus, Claude Sonnet, GPT-OSS" + cDimGrayClose,
		"",
		"\x1b[1mWeekly Limit Remaining\x1b[0m",
		"    [\x1b[38;2;101;123;131m" + strings.Repeat("░", 50) + "\x1b[39m]" +
			cDimGrayOpen + " 0.00%" + cDimGrayClose,
		"\x1b[31mRefreshes in 74h 29m\x1b[39m",
		cDimGrayOpen + "  │Within each group, models share a weekly limit." + cDimGrayClose,
		cDimGrayOpen + "  │Quota refreshes weekly and is consumed as you use" + cDimGrayClose,
		cDimGrayOpen + "  │models in the group." + cDimGrayClose,
	}
}

func usageViewerLinesStatic() []string {
	email := auth.EffectiveUserEmail()
	if email == "" {
		email = "navigaid@gmail.com"
	}
	out := []string{
		"\x1b[1m" + panelGray238 + "└ Models & Quota" + "\x1b[0m",
		"",
		"  Account: " + cBlueOpen + email + cBlueClose,
		"",
	}
	out = append(out, usageGeminiGroupRows()...)
	out = append(out, "")
	out = append(out, usageClaudeGroupRows()...)
	return out
}

// changelogBody is the opening set of bullet points under `## 1.3.6`.
// Cycle 193 shipped this block verbatim from
// /tmp/agycmp/panels/ppref.changelog.txt; cycle 201 keeps it as the head
// of the scrollable document (the rest of the release notes is parsed
// from the embedded print-mode markdown below).
var changelogBody = []string{
	"",
	"  \x1b[1m\x1b[38;5;27m## 1.3.6\x1b[0m",
	"",
	"  \x1b[38;5;235m• Added Claude Opus 5.5 and Claude Sonnet 5.5 for Application Default Credentials (AGY_ADC_AUTH) sign-in. Claude models now appear in /model only if your Google Cloud project can use them.\x1b[39m",
	"  \x1b[38;5;235m• Improved Auto mode's review of commands that would leave the terminal sandbox: the review is faster and no longer times out without a decision, it allows the workspace's own format, lint, and fix commands instead of asking about them, and it blocks commands that would upload sensitive data.\x1b[39m",
	"  \x1b[38;5;235m• Improved the /diff Commit Tree view with colors: commit hashes and change IDs are highlighted, descriptions are dimmed, the Working Copy (Uncommitted Changes) row stands out, and @ is bold, so hashes, timestamps, and descriptions no longer run together.\x1b[39m",
	"  \x1b[38;5;235m• Fixed the agent hanging indefinitely when the model connection stalled: if no data arrives for 5 minutes the request is now canceled and retried automatically, and a stalled request no longer leaves an empty response step behind.\x1b[39m",
	"  \x1b[38;5;235m• Fixed questions asked by subagents and workflow agents being lost, which left the subagent waiting forever: the alert now reads <agent> has a question and shows it, and Ctrl+K or a in the subagent's view opens a question picker that sends your answer to that agent. Headless -p runs skip these questions so the subagent can continue.\x1b[39m",
	"  \x1b[38;5;235m• Fixed agent shell commands hanging on an invisible editor when a version control command wants one, such as git commit without -m or jj describe: the CLI now sets GIT_EDITOR, HGEDITOR, and JJ_EDITOR to false for agent commands, so they fail right away instead.\x1b[39m",
	"  \x1b[38;5;235m• Fixed skills, rules, workflows, and Markdown agents failing to load with mapping values are not allowed in this context when an unquoted description: contains a colon, such as description: Runs checks: formatters and linters.\x1b[39m",
	"  \x1b[38;5;235m• Fixed Mermaid diagrams that start with YAML frontmatter, a %%{init: ...}%% directive, or %% comments, or that use Mermaid v11 syntax such as id@{ shape: ..., label: ... } nodes, showing as raw source instead of rendering in agent responses and the artifact viewer.\x1b[39m",
	"  \x1b[38;5;235m• Fixed the @ file-mention dropdown staying open after a newline in a multi-line prompt, where it took over Up/Down and made Enter pick a file instead of inserting the newline.\x1b[39m",
	"  \x1b[38;5;235m• Fixed / search in the file and artifact viewer highlighting the wrong characters, or crashing, on lines with letters whose lowercase form has a different length such as İ; n and N now stop only on lines that show a highlight.\x1b[39m",
	"  \x1b[38;5;235m• Fixed long lines and file paths containing CJK or other multi-byte text turning into characters in /codesearch results; they are now cut cleanly with an ellipsis.\x1b[39m",
	"  \x1b[38;5;235m• Fixed agy plugin validate, the summary agy plugin install prints, and marketplace previews in the /plugin Discover tab leaving out subagents a plugin declares in its root agents.json.\x1b[39m",
}

// panelChangelogLines renders the /changelog viewer. Layout recovered
// from /tmp/agycmp/panels/ppref.changelog.raw.txt. Esc closes silently
// (the changelog viewer never writes an `Exited /changelog` row).
//
// Cycle 201: the viewer scrolls. changelogExcerptRows is the cycle-193
// first screen (title + tagline + the 1.3.6 block) and is what offset 0
// still renders; changelogRestRows appends the remaining sections so
// ↑/↓ have somewhere to go. The counter keeps REF's denominator.
//
// Cycle changelog-pagination: the document now comes from
// internal/cli/testdata/changelog_reference.txt when that file is
// reachable (see changelogViewerLines), so the panel has 900+ rows
// to scroll instead of the ~50 the cycle-201 hardcoded fallback had.
// The footer advertises the full pagination key set (↑/↓ line,
// pgup/pgdown page, ctrl+end bottom, ctrl+home top, esc close) the
// same way the /usage viewer does.
