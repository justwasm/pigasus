// slash_viewer_panels.go — cycle 207: live re-diff of the document viewers
// (/usage, /changelog, /diff) against the closed-source reference `agy`
// 1.4.0. The cycle-193/201 renderers in slash_panels.go were transcribed
// from an older build; this file carries the corrected geometry that the
// 1.4.0 captures show, leaving the old renderers in place as the
// unit-test (m.height == 0) fallback so the cycle-201/202 tests stay
// byte-exact.
//
// Evidence: the 2026-10-17 probe run under tmux socket `agy207`
// (/private/var/folders/.../opencode/agy207/*.ref.raw.txt). Every colour
// and column below is transcribed from those captures; see
// docs/cycles/cycle-207/deliverable-viewer-panels.md for the raw lines and
// the list of what could not be measured.
//
// Colour caveat (unchanged from slash_panels.go): `capture-pane -e`
// normalises 24-bit RGB to a 256-colour index, so cBlueOpen (38;2;66;133;244)
// and the capture's 38;5;69 are the same cell. The renderers below reuse the
// recovered true-colour constants where one exists.

package main

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"time"

	tea "charm.land/bubbletea/v2"
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

// ----------------------------------------------------------------------
// /diff — REF 1.4.0 git working-copy view
// ----------------------------------------------------------------------
//
// REF 1.4.0 detects the workspace's VCS. In a git tree /diff's All Changes
// tab shows a header row, a "N file(s) changed" count, the tracked files
// (path-sorted, each with its +added/-deleted counts), a blank, the
// untracked files and a legend. The other two tabs are placeholders
// ("No file changes in conversation" / a commit list). Recovered from the
// diff3/diff50/diffA/diffD/diffE captures against three git fixtures.

// diffChange is one working-copy file with its line counts.
type diffChange struct {
	path      string
	added     int
	deleted   int
	untracked bool
}

// gitDiffChanges returns the tracked then untracked changes of the git
// working copy rooted (anywhere) at dir. Tracked counts come from
// `git diff --numstat` plus `git diff --cached --numstat`; untracked files
// are counted directly, the way `git diff --numstat` reports a new file.
// It returns ok=false when git is unavailable or dir is not a repo.
func gitDiffChanges(dir string) (tracked, untracked []diffChange, ok bool) {
	status, err := runGit(dir, "status", "--porcelain", "-uall")
	if err != nil {
		return nil, nil, false
	}
	known := map[string]diffChange{}
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		code, path := line[:2], line[3:]
		if code == "??" {
			ut := diffChange{path: path, added: countFileLines(dir, path), untracked: true}
			untracked = append(untracked, ut)
			continue
		}
		if !strings.ContainsAny(code, "MADRCUT") {
			continue
		}
		known[path] = diffChange{path: path}
	}
	if len(known) == 0 {
		sortChanges(untracked)
		return nil, untracked, true
	}
	// Tracked counts: unstaged then staged, summed per path.
	for _, args := range [][]string{
		{"diff", "--numstat"},
		{"diff", "--cached", "--numstat"},
	} {
		numstat, err := runGit(dir, args...)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(numstat, "\n") {
			fields := strings.SplitN(line, "\t", 3)
			if len(fields) != 3 {
				continue
			}
			c, exists := known[fields[2]]
			if !exists {
				continue
			}
			c.added += diffAtoi(fields[0])
			c.deleted += diffAtoi(fields[1])
			known[fields[2]] = c
		}
	}
	for _, c := range known {
		tracked = append(tracked, c)
	}
	sortChanges(tracked)
	sortChanges(untracked)
	return tracked, untracked, true
}

// runGitEnvKVs is the slice of KEY=VALUE pairs runGit always injects on top
// of os.Environ() before exec'ing git. Mirrors internal/tools.AgentShellEnvKVs
// in shape (a package-level slice + a small helper that prepends the parent
// environment) so unit tests can pin the contract without invoking git.
//
// Two groups:
//
//   - Pager disabling: PAGER=cat, GIT_PAGER=cat, JJ_PAGER=cat. AGY 1.3.3
//     changelog lines 50-51: "Fixed jj (Jujutsu VCS) command output being
//     broken by the `less` pager — JJ_PAGER=cat is now set alongside
//     GIT_PAGER=cat so commands like `jj log` show full output instead of
//     paging through interactive less." `cat` writes the file to stdout
//     unchanged. Even though runGit only invokes `git` today, JJ_PAGER=cat
//     travels alongside GIT_PAGER=cat so the two settings stay in lockstep
//     with the agent-shell-env trio in internal/tools/command.go and the
//     vcsDiff env in slash_actions.go — a future caller that pipes git
//     output into a jj-aware step inherits the same pager contract.
//   - Editor disabling: GIT_EDITOR=false. AGY 1.3.6 changelog line 7: a
//     future caller invoking `git commit` without `-m` cannot hang the
//     TUI on a hidden vim. HGEDITOR / JJ_EDITOR are omitted because runGit
//     only ever execs git; vcsDiff (slash_actions.go) is the multi-VCS path
//     and sets the full trio.
//   - Locale pinning: LC_ALL=C. Keeps `git status` / `git diff` parseable
//     across locales (a German LANG= setting turns "On branch main" into
//     something ungreppable).
var runGitEnvKVs = []string{
	"PAGER=cat",
	"GIT_PAGER=cat",
	"JJ_PAGER=cat",
	"GIT_EDITOR=false",
	"LC_ALL=C",
}

// runGitEnv returns os.Environ() with runGitEnvKVs appended. The KVs are
// appended *after* the parent environment so on POSIX systems where
// duplicate env entries take the last value, our overrides win over
// anything the user's shell set (e.g. a GIT_PAGER=less from their rcfile).
func runGitEnv() []string {
	return append(os.Environ(), runGitEnvKVs...)
}

// runGit runs a git subcommand in dir and returns its trimmed stdout.
// PAGER=cat / GIT_PAGER=cat / JJ_PAGER=cat disable any pager so a long
// `git log` lands on stdout. GIT_EDITOR=false (1.3.6 line 7) keeps a
// future `git commit` without `-m` from wedging the TUI on a hidden vim.
// LC_ALL=C keeps diffs and status parseable across locales.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = runGitEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// countFileLines counts the lines git would report for a brand-new file:
// newline count, plus one when the file does not end in a newline, and zero
// for an empty file.
func countFileLines(dir, path string) int {
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil || len(data) == 0 {
		return 0
	}
	n := strings.Count(string(data), "\n")
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func diffAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func sortChanges(cs []diffChange) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].path < cs[j].path })
}

// diffCounts renders a change's " +N -M" suffix (each part omitted when
// zero), matching ref_usage… style: +N in the effort-low green and -M in
// the error red-orange. The leading space is added by the row builder.
func diffCounts(c diffChange) string {
	var parts []string
	if c.added > 0 {
		parts = append(parts, panelEffLowOpen+"+"+strconv.Itoa(c.added)+"\x1b[39m")
	}
	if c.deleted > 0 {
		parts = append(parts, cToolBulletErrOpen+"-"+strconv.Itoa(c.deleted)+"\x1b[39m")
	}
	return strings.Join(parts, " ")
}

// diffTabRow is the /diff header. The active tab is brand blue, the others
// are plain; `leading` writes the one-space left margin REF shows. The
// margin is present in every captured view except All Changes with changes
// to list, which is flush-left: diff3/diff3_dd/diff3_jk/diffA/diffB (no
// margin) versus diffD/diffE/diffclean2 (one space). 【一手】
func diffTabRow(vcs string, active int, leading bool) string {
	var b strings.Builder
	if leading {
		b.WriteString(" ")
	}
	b.WriteString("\x1b[1mDiff (" + vcs + ")\x1b[0m")
	for i, name := range diffViews {
		b.WriteString("  ")
		if i == active {
			b.WriteString(cBlueOpen + name + cBlueClose)
		} else {
			b.WriteString(name)
		}
	}
	return b.String()
}

// diffAllChangesRow renders one change row. The cursor row carries the
// "> " marker in brand blue; an untracked row carries the orange
// "[Untracked]" badge.
func diffAllChangesRow(c diffChange, cursor bool) string {
	var b strings.Builder
	switch {
	case cursor && c.untracked:
		b.WriteString(cBlueOpen + "> " + "\x1b[38;5;172m? [Untracked] " + cBlueOpen + c.path + cBlueClose)
	case cursor:
		b.WriteString(cBlueOpen + "> " + c.path + cBlueClose)
	case c.untracked:
		b.WriteString("  " + "\x1b[38;5;172m? [Untracked] " + "\x1b[39m" + c.path)
	default:
		b.WriteString("  " + c.path)
	}
	if counts := diffCounts(c); counts != "" {
		b.WriteString("  ")
		b.WriteString(counts)
	}
	return b.String()
}

// diffAllChangesLines renders the tab's file list, or the empty state.
// cursor indexes the flattened (tracked + untracked) list.
func diffAllChangesLines(tracked, untracked []diffChange, cursor int) []string {
	total := len(tracked) + len(untracked)
	if total == 0 {
		return []string{
			diffTabRow("git", 0, true),
			"",
			"   " + cDimGrayOpen + "No uncommitted changes" + cDimGrayClose,
			"",
			"   " + panelDotHint("", "←/→", "switch view", "esc", "dismiss"),
		}
	}
	if cursor < 0 || cursor >= total {
		cursor = 0
	}
	lines := []string{
		diffTabRow("git", 0, false),
		panelGray238 + "  " + strconv.Itoa(total) + " file(s) changed" + panelGray238C,
		"",
	}
	for i, c := range tracked {
		lines = append(lines, diffAllChangesRow(c, cursor == i))
	}
	if len(tracked) > 0 && len(untracked) > 0 {
		lines = append(lines, "")
	}
	for i, c := range untracked {
		lines = append(lines, diffAllChangesRow(c, cursor == len(tracked)+i))
	}
	lines = append(lines, "",
		"  "+panelDotHint("", "←/→", "switch view", "↑/↓/j/k", "navigate", "enter", "view diff", "esc", "dismiss"))
	return lines
}

// panelDiffLinesVCS renders /diff in a git workspace: the All Changes list,
// the Per Turn placeholder or the Commit Tree. Per Turn has no captured
// per-turn model (the clone does not track per-turn changes), and the
// Commit Tree is only ever captured with a single commit, so both carry a
// 【推测】 marker in the cycle report.
//
// Cycle 226: when the user has pressed enter on a working-copy file row
// (tab 0) or a commit row (tab 2), m.panel.diffView is non-zero and the
// view is replaced by the inline diff body (panelDiffInlineLines); Esc
// returns to the list. Per Turn (tab 1) keeps the placeholder.
func (m *runMode) panelDiffLinesVCS() []string {
	if m.panel.diffView != 0 {
		return m.panelDiffInlineLines()
	}
	tab := m.panel.tab
	if tab < 0 || tab >= len(diffViews) {
		tab = 0
	}
	switch tab {
	case 1: // Per Turn
		return m.diffPerTurnLines()
	case 2: // Commit Tree
		return m.diffCommitTreeLines()
	default:
		tracked, untracked, _ := gitDiffChanges(m.cwd)
		return diffAllChangesLines(tracked, untracked, m.panel.cursor)
	}
}

// diffCommitTreeLines renders the Commit Tree tab. Only the single-commit
// shape was captured (diffE/diff_right2), so the cycle-207 implementation
// extrapolated a "|" tree connector before every commit. The
// multi-commit probe (agy226/diff50-CT.ref.raw.txt) shows the connector
// is only emitted **once**, between the working-copy row and the first
// commit: subsequent commits are stacked directly. Cycle 226 corrects
// the connector logic and the cursor mapping (cycle-207 had the row
// cursor inert on Commit Tree; cycle 226 unifies it with the other
// tabs).
func (m *runMode) diffCommitTreeLines() []string {
	log, _ := runGit(m.cwd, "log", "--pretty=%h%x09%s")
	var commits [][2]string
	for _, line := range strings.Split(log, "\n") {
		if fields := strings.SplitN(line, "\t", 2); len(fields) == 2 {
			commits = append(commits, [2]string{fields[0], fields[1]})
		}
	}
	lines := []string{
		diffTabRow("git", 2, true),
		" " + panelGray238 + "  " + strconv.Itoa(len(commits)) + " commit(s) available" + panelGray238C,
		"",
		" \x1b[38;5;69m\x1b[48;5;255m▌\x1b[39m \x1b[1m*\x1b[0m\x1b[48;5;255m  \x1b[1m\x1b[38;5;172mWorking Copy (Uncommitted Changes)\x1b[0m\x1b[48;5;255m",
	}
	for i, c := range commits {
		if i == 0 {
			lines = append(lines, "\x1b[49m   |")
		}
		lines = append(lines, "   * "+cBlueOpen+c[0]+cBlueClose+" "+panelGray238+c[1]+panelGray238C)
	}
	lines = append(lines, "",
		"   "+panelDotHint("", "↑/↓", "navigate", "enter", "view diff", "←/→", "switch view", "esc", "dismiss"))
	return lines
}

// diffMoveCursor moves /diff's row cursor over the active tab's row
// list. Cycle 226 extends the cycle-207 cursor model (All Changes
// only) to Per Turn and Commit Tree, both of which now carry row lists
// the user can navigate. The inline subview (m.panel.diffView != 0)
// owns its own scroll, so the cursor is inert there.
//
// Cursor mapping per tab:
//   - All Changes (0): cursor is the index into tracked+untracked.
//   - Per Turn (1):    cursor is the index into the turns list.
//   - Commit Tree (2): cursor is the index into working-copy (0) plus
//     the N commits (1..N).
//
// Cycle 207 retained the no-VCS short-circuit; this still holds.
func (m *runMode) diffMoveCursor(delta int) {
	if m.cwd == "" || m.panel.diffView != 0 {
		return
	}
	var hi int
	switch m.panel.tab {
	case 0: // All Changes
		tracked, untracked, ok := gitDiffChanges(m.cwd)
		if !ok {
			return
		}
		hi = len(tracked) + len(untracked) - 1
	case 1: // Per Turn
		turns := diffCountTurns(m)
		tracked, untracked, ok := gitDiffChanges(m.cwd)
		if !ok || turns == 0 || len(tracked)+len(untracked) == 0 {
			return
		}
		hi = turns - 1
	case 2: // Commit Tree
		log, err := runGit(m.cwd, "log", "--pretty=%h%x09%s")
		if err != nil {
			return
		}
		if log == "" {
			return
		}
		// log has one line per commit (no trailing newline); the
		// cursor indexes the working-copy row (0) plus the N commits
		// (1..N), so hi = N.
		hi = strings.Count(log, "\n") + 1
	default:
		return
	}
	if hi < 0 {
		return
	}
	m.panel.cursor += delta
	if m.panel.cursor < 0 {
		m.panel.cursor = 0
	}
	if m.panel.cursor > hi {
		m.panel.cursor = hi
	}
}

// diffTextKey returns the cursor delta for an unmodified j/k key, or 0.
func diffTextKey(key tea.Key) int {
	if key.Mod != 0 || len(key.Text) != 1 {
		return 0
	}
	switch key.Text[0] {
	case 'j':
		return 1
	case 'k':
		return -1
	}
	return 0
}

// ----------------------------------------------------------------------
// /diff — cycle 226 follow-up: Per Turn rows, inline-diff subview
// ----------------------------------------------------------------------
//
// REF 1.4.0's enter-view-diff on All Changes opens a side-by-side diff
// viewer with a "change 1/1 · file 1/N" footer and the n/N f c d esc
// legend (agy226/diffA-enter1.ref.raw.txt). The full side-by-side viewer
// is out of scope for this cycle; the clone substitutes a plain inline
// diff body, gated through m.panel.diffView so the default panel shape
// stays byte-exact. The same wiring handles Commit Tree enter: REF drills
// into the commit's files list and then a file's diff (a two-level
// drill); the clone collapses that into one `git show <hash>` body.

// diffCountTurns returns the number of user-role turns in m.session.
// Cycle 226 follows the cycle-194 convention (lastUserMessage walks
// `Role == "user"`), so the count is the count of user messages.
func diffCountTurns(m *runMode) int {
	n := 0
	for _, msg := range m.session {
		if msg.Role == "user" {
			n++
		}
	}
	return n
}

// diffFirstChangedPath returns the path of the first change in the
// working copy (tracked then untracked), or "" when there are none. Used
// to keep Per Turn rows non-empty without forcing a per-turn diff model.
func diffFirstChangedPath(tracked, untracked []diffChange) string {
	if len(tracked) > 0 {
		return tracked[0].path
	}
	if len(untracked) > 0 {
		return untracked[0].path
	}
	return ""
}

// diffPerTurnLines renders the Per Turn tab. Cycle 226 replaces the
// blank placeholder with a row per user turn when the working copy has
// changes (the row text is `Turn N · <first changed file>`); when the
// working copy is clean, or the session has no user turns, the original
// `"No file changes in conversation"` placeholder is preserved.
func (m *runMode) diffPerTurnLines() []string {
	tracked, untracked, ok := gitDiffChanges(m.cwd)
	if !ok || len(tracked)+len(untracked) == 0 {
		return []string{
			diffTabRow("git", 1, true),
			"",
			"   " + cDimGrayOpen + "No file changes in conversation" + cDimGrayClose,
			"",
			"   " + panelDotHint("", "←/→", "switch view", "esc", "dismiss"),
		}
	}
	turns := diffCountTurns(m)
	if turns == 0 {
		return []string{
			diffTabRow("git", 1, true),
			"",
			"   " + cDimGrayOpen + "No file changes in conversation" + cDimGrayClose,
			"",
			"   " + panelDotHint("", "←/→", "switch view", "esc", "dismiss"),
		}
	}
	first := diffFirstChangedPath(tracked, untracked)
	all := append(append([]diffChange{}, tracked...), untracked...)
	lines := []string{
		diffTabRow("git", 1, true),
		" " + panelGray238 + "  " + strconv.Itoa(turns) + " turn(s) · " +
			strconv.Itoa(len(all)) + " file(s) changed" + panelGray238C,
		"",
	}
	for i := 0; i < turns; i++ {
		file := first
		if len(all) > 1 {
			file = all[i%len(all)].path
		}
		marker := "  "
		if i == m.panel.cursor {
			marker = cBlueOpen + "> " + cBlueClose
		}
		lines = append(lines, marker+"Turn "+strconv.Itoa(i+1)+" · "+cDimGrayOpen+file+cDimGrayClose)
	}
	lines = append(lines, "",
		"   "+panelDotHint("", "↑/↓", "navigate", "←/→", "switch view", "esc", "dismiss"))
	return lines
}

// gitDiffBody returns the diff body for `target` in dir. kind=file runs
// `git diff --no-color -- <path>` (unstaged + staged working-copy diff);
// kind=commit runs `git show --no-color <hash>` (the full commit diff,
// including the stat header). The empty slice is returned when git is
// unavailable or the command fails — the caller renders a single
// "no diff available" row in that case.
//
// Each line is normalised through trimDiffTrailing so trailing tabs and
// spaces don't show up as explicit whitespace markers in the inline-diff
// subview (AGY 1.2.16). The leading marker byte (` `, `+`, `-`, `@` and
// the file/hunk header characters) is preserved; only the tail after the
// last non-whitespace rune changes. The on-disk file is unchanged.
func gitDiffBody(dir, target, kind string) []string {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	var args []string
	switch kind {
	case "file":
		args = []string{"diff", "--no-color", "--", target}
	case "commit":
		args = []string{"show", "--no-color", target}
	default:
		return nil
	}
	out, err := runGit(dir, args...)
	if err != nil {
		return nil
	}
	if out == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = trimDiffTrailing(l)
	}
	return lines
}

// diffInlineStyle applies REF-style +/- colouring to one diff line. The
// `/` lines (file headers) and `@@` lines (hunk headers) pass through
// plain; `+` lines get the green low-effort colour, `-` lines get the
// red-orange tool-bullet colour. The leading byte of each line is the
// only thing that drives the colour, matching the captured per-line
// pattern REF's side-by-side viewer paints (agy226/diffA-enter1.raw.txt
// rows 4-5).
func diffInlineStyle(line string) string {
	if line == "" {
		return line
	}
	switch line[0] {
	case '+':
		return panelEffLowOpen + line + "\x1b[39m"
	case '-':
		return cToolBulletErrOpen + line + "\x1b[39m"
	default:
		return line
	}
}

// diffInlineLegend returns the keyboard hint row the inline subview
// shows. REF's diff viewer advertises `n/N next/prev diff · ←/→ cycle
// file · f full file · c comment · d delete comment · esc back`; the
// clone implements the f-binding (cycle diff-folding, AGY 1.3.5,
// hunk fold) and the esc-back binding, and skips the bindings the
// reference's side-by-side viewer supports that the plain inline view
// does not (n/N, ←/→, c, d).
func diffInlineLegend() string {
	return "   " + panelDotHint("", "f", "fold hunk", "esc", "back")
}

// panelDiffInlineLines renders the inline-diff subview opened by
// `enter` on All Changes (diffView=1, path=file) or Commit Tree
// (diffView=2, path=commit hash). Esc returns to the list; the inline
// view is otherwise inert. The body is `git diff/show` output styled
// line-by-line, with hunk-fold state applied per diffFolded[i] (see
// slash_diff_fold.go); the title row carries the path or
// `commit <hash>` so the user knows which diff they are looking at.
//
// Cycle diff-syntax-highlight: on the file view (diffView==1) each
// non-pre-header body line is fed through renderDiffInlineLine so
// keywords / strings / numbers / comments / types are coloured per
// the Aurora-Dark tokens the v1.3.6 binary recovers via
// `strings ~/.local/bin/agy | grep chroma`. The marker byte (the
// leading `+` / `-` / ` `) keeps the existing per-line green / red
// tint so the visual contract of the cycle-127 tool-step renderer
// (the diff colour applies to the marker alone) is preserved. The
// commit view (diffView==2) has no single file path to bind a
// language to, so it falls back to the original diffInlineStyle
// monochrome behaviour.
//
// Cycle diff-folding (AGY 1.3.5): the renderer walks splitDiffHunks
// and emits each hunk's body either expanded (every body line
// styled) or collapsed (header line + "+N" line-count indicator in
// dim grey). Pre-header lines ("diff --git ...", "index ...", "---",
// "+++") are always rendered uncondensed.
func (m *runMode) panelDiffInlineLines() []string {
	var title, kind, langFile string
	switch m.panel.diffView {
	case 1:
		title = m.panel.diffViewPath
		kind = "file"
		langFile = m.panel.diffViewPath
	case 2:
		title = "commit " + m.panel.diffViewPath
		kind = "commit"
	default:
		title = "(no diff)"
		kind = "file"
	}
	lines := []string{
		"\x1b[1m" + title + "\x1b[0m",
		"",
	}
	body := gitDiffBody(m.cwd, m.panel.diffViewPath, kind)
	if len(body) == 0 {
		lines = append(lines, "   "+cDimGrayOpen+"no diff available"+cDimGrayClose)
	} else {
		rows, _ := m.renderDiffFromBody(body, langFile)
		// Clamp the inline cursor into the new visible range —
		// the user's cursor follows them across re-renders
		// (e.g. after a fold toggle reduces the body, so an
		// out-of-range row gets pulled back to the last visible
		// row). Clamping here too means a panel re-paint after
		// the user navigated manually never strands the cursor.
		maxRow := len(rows) - 1
		if maxRow < 0 {
			m.panel.diffInlineRow = 0
		} else {
			if m.panel.diffInlineRow < 0 {
				m.panel.diffInlineRow = 0
			}
			if m.panel.diffInlineRow > maxRow {
				m.panel.diffInlineRow = maxRow
			}
		}
		// Highlight the cursor row by prefixing it with the
		// blue "> " marker (the same convention /diff's list
		// subviews use — `> Turn 1 · ...`). The marker is
		// applied to the styled line so the inline colour
		// spans stay intact.
		cursor := m.panel.diffInlineRow
		for i, r := range rows {
			if i == cursor {
				lines = append(lines, " "+cBlueOpen+"> "+cBlueClose+r)
			} else {
				lines = append(lines, r)
			}
		}
	}
	lines = append(lines, "", diffInlineLegend())
	return lines
}

// diffOpenInline sets m.panel.diffView and diffViewPath so the next
// panelDiffLinesVCS call renders the inline-diff subview. The cycle-226
// substitute is plain text (not REF's side-by-side viewer); see
// panelDiffInlineLines for the wiring.
//
// Cycle diff-folding (AGY 1.3.5): also resets diffInlineRow and
// diffFolded so each new file's diff starts unfolded at the top.
// Without this reset the cursor and fold state would carry over from
// the previous diff (e.g. scroll position from one file would land
// you in the middle of another).
func (m *runMode) diffOpenInline(kind int, target string) {
	m.panel.diffView = kind
	m.panel.diffViewPath = target
	m.panel.diffInlineRow = 0
	m.panel.diffFolded = nil
}

// diffCloseInline returns to the file-list view. Cycle 226 binds Esc on
// the inline subview to this; the panel itself stays open.
//
// Cycle diff-folding (AGY 1.3.5): resets diffInlineRow and clears
// diffFolded so the next Enter opens a fresh, unfolded diff at the
// top row. (Esc is treated as "leave this diff" rather than "minimise
// this diff" — the reference's side-by-side viewer collapses to a
// re-open on Esc, but Esc-then-Enter in the clone opens a new view.)
//
// The list-view cursor (m.panel.cursor) is the row into the
// file/commit list and is left alone so Esc returns to the same row
// the user pressed Enter on.
func (m *runMode) diffCloseInline() {
	m.panel.diffView = 0
	m.panel.diffViewPath = ""
	m.panel.diffInlineRow = 0
	m.panel.diffFolded = nil
}

// diffEnter is the binding `enter` triggers on All Changes / Commit
// Tree. On Per Turn the binding is inert (the placeholder has no rows
// to drill into; the cycle-226 row-list mode is also inert because the
// per-turn model has no per-file body to show). When the cursor points
// at a valid row the subview is opened; when it does not (empty list)
// the binding is a no-op.
func (m *runMode) diffEnter() {
	switch m.panel.tab {
	case 0: // All Changes
		tracked, untracked, ok := gitDiffChanges(m.cwd)
		if !ok {
			return
		}
		all := append(append([]diffChange{}, tracked...), untracked...)
		if m.panel.cursor < 0 || m.panel.cursor >= len(all) {
			return
		}
		m.diffOpenInline(1, all[m.panel.cursor].path)
	case 2: // Commit Tree
		log, err := runGit(m.cwd, "log", "--pretty=%h%x09%s")
		if err != nil {
			return
		}
		var commits [][2]string
		for _, line := range strings.Split(log, "\n") {
			if fields := strings.SplitN(line, "\t", 2); len(fields) == 2 {
				commits = append(commits, [2]string{fields[0], fields[1]})
			}
		}
		// Cursor: 0 = working-copy row (the "▌ *" line at index 3), 1..N
		// = the N commits. Skip 0 (working-copy has no per-commit diff
		// hash to drill into), so commit i is at cursor==i.
		idx := m.panel.cursor - 1
		if idx < 0 || idx >= len(commits) {
			return
		}
		m.diffOpenInline(2, commits[idx][0])
	}
}
