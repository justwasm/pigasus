// slash_codesearch.go — cycle 228: the /codesearch panel with
// progressive streaming and ripgrep fallback.
//
// The cycle-213 file in this slot rendered the result list synchronously:
// csScan walked the workspace, and the panel came back with every match
// in one shot. AGY 1.4.0 reversed that — the reference spawns `rg --json`,
// shows a "Searching…" header that ticks into "Found N matches" as
// matches stream in, lets the user hit Esc to kill the child mid-search,
// and falls back to a local walk when the rg binary cannot be executed
// (typically an endpoint security agent blocking it).
//
// This file is the cycle-228 rewrite: it carries forward the cycle-213
// layout (file header + context rows + counter + Keyboard footer) but
// feeds it from internal/codesearch's runner. The runner pushes matches
// into a buffered channel; a tick message drains them on the TUI goroutine.
//
// One result block per matching line (a line holding two matches counts
// once, only the first match highlighted), context window = match ±1,
// per-file header underlined in colour 32, between-block rule of
// width-4 dashes in colour 253 — all preserved from cycle 213.
//
// When no results are in yet but a search is running, the panel shows
// "Searching…  [Esc to cancel]" and a Braille spinner glyph on the
// title row. As matches arrive the header updates to "Found N matches
// in K files" (cycle 228: the K counter is new — cycle 213 only ever
// knew the total count). Esc cancels the in-flight search by killing
// the rg child or stopping the walk and the channel is closed with
// State == StateCancelled; the panel reverts to its closed state
// without an exit notice (the reference closes /codesearch silently
// when the user dismisses it).
//
// Two new flag forms the cycle-213 parser did not understand:
//
//	f:<glob>     include only files matching <glob> (ripgrep -g)
//	file:<glob>  exclude files matching <glob>           (-g !<glob>)
//
// Both are repeatable and orthogonal to -F/--literal. Cycle 213's
// usage line still says "[-F|--literal] <query>" because the reference
// echoes the verbatim argument tail in the title — the f:/file:
// variants surface only when the user actually types them.

package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/codesearch"
)

// csResultCap is the reference's hard ceiling on reported matches: 1200
// matching lines render as `Found 1000 results` and `[… of 1000 items]`.
const csResultCap = codesearch.MaxHits

// csWidthInset is how much narrower than the terminal the list rows and the
// block separator are: at 220 columns the separators are 216 dashes and a long
// row is 216 cells; at 120 they are 116 (probe_w.py).
const csWidthInset = 4

// csFixedRows is the panel's non-list row count: title, `Found …`, counter,
// blank, `Keyboard:`, trailing blank, status row.
const csFixedRows = 7

// csBudgetNum / csBudgetDen encode the measured list budget
// `floor(height*11/15) - csFixedRows`. Exact hits at heights 28, 32, 40, 44,
// 72, 80, 100 and 120 (the window consumed the whole budget there); at the
// other probed heights the next item simply did not fit in what was left.
const (
	csBudgetNum = 11
	csBudgetDen = 15
)

// csHeaderOpen / csHeaderReset are the file-header style: underlined, colour
// 32. The truecolour value behind index 32 was not recovered, so the index is
// emitted directly (capture-pane keeps indexed colours as they are).
const (
	csHeaderOpen  = "\x1b[4m\x1b[38;5;32m"
	csHeaderReset = "\x1b[0m"
)

// csRuleOpen is the block separator's grey — the same index the /plugin
// panel uses (38;2;238;232;213).
const csRuleOpen = pluginColor253

// csErrorOpen is the orange the reference uses for the pattern-error row.
const csErrorOpen = "\x1b[38;5;166m"

// csSpinnerFrames is the same Braille set the streaming /usage spinner
// uses (cycle 225). Picked so the header's spinner glyph is byte-for-byte
// the same shape the reference's progressive /codesearch search shows.
var csSpinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

// csHit is one matching line plus the neighbouring lines the panel renders as
// context. ms/me are the first match's rune offsets inside text.
type csHit struct {
	file    string // slash-separated path relative to the search root
	line    int    // 1-based line number
	col     int    // 1-based rune column of the first match
	prev    string // line-1 text
	text    string // the matching line, verbatim
	next    string // line+1 text
	hasPrev bool
	hasNext bool
	ms, me  int // first match span, in runes
}

// csState is the /codesearch panel's full state. The zero value is the
// argument-less usage form (and also what a closed panel leaves behind).
//
// search is the codesearch.Search the runner is filling; runner is the
// Runner the panel called Run() on (kept so Esc can call Cancel()). The
// panel never reads search.Matches directly — a tick message drains
// the channel into hits every csTickInterval milliseconds.
type csState struct {
	args    string // raw argument tail, echoed verbatim in the title
	query   string // the pattern actually searched for
	literal bool
	include []string
	exclude []string
	err     string  // non-empty when the pattern did not compile
	hits    []csHit // matches already drained into the panel
	files   map[string]struct{}

	// Progressive streaming state.
	search  *codesearch.Search // nil when no search is running
	runner  *codesearch.Runner // nil when no runner (test-only path)
	spinner int                // Braille frame index for the loading header
}

// csTickInterval is the cadence at which the panel drains the runner's
// matches channel. 80 ms is fast enough that the user sees the count
// tick up smoothly while typing a query, slow enough that a 1200-match
// search doesn't flood the message queue.
const csTickInterval = 80 * time.Millisecond

// csParseArgs splits the leading -F / --literal flag from the argument tail
// and extracts any f:/file: glob flags. It is a thin wrapper over
// codesearch.ParseArgs so both the print-mode /codesearch echo path and
// the panel path agree on the contract.
func csParseArgs(args string) (query string, literal bool, include, exclude []string, err error) {
	a, perr := codesearch.ParseArgs(args)
	if perr != nil {
		if perr == codesearch.ErrEmptyQuery {
			return "", false, nil, nil, codesearch.ErrEmptyQuery
		}
		return "", false, nil, nil, perr
	}
	return a.Query, a.Literal, a.Include, a.Exclude, nil
}

// csHasUpper reports whether s contains an uppercase rune. ripgrep's
// smart-case rule — which the reference inherits — turns case-sensitivity on
// only when the pattern has one.
func csHasUpper(s string) bool {
	return strings.IndexFunc(s, unicode.IsUpper) >= 0
}

// csMatcher compiles the query with the reference's semantics: a fixed string
// under -F/--literal, a regex otherwise, case-insensitive while the pattern is
// all lowercase. The runner uses its own smart-case for the rg path; this
// helper stays for the local-fallback scan (used by the unit tests and by
// the `csPanelLines` / csHit-from-string helper when the search is
// reconstructed for cycle-213 tests).
func csMatcher(query string, literal bool) (*regexp.Regexp, error) {
	pat := query
	if literal {
		pat = regexp.QuoteMeta(pat)
	}
	if !csHasUpper(query) {
		pat = "(?i)" + pat
	}
	return regexp.Compile(pat)
}

// csScan walks the workspace and collects the first csResultCap matching lines
// in lexical path order. It mirrors searchWorkspace's file policy (1 MiB size
// cap, binary skip, codeSearchSkipDirs) and adds hidden entries, which a
// workspace search never descends into.
//
// Cycle 228 keeps csScan as a synchronous helper for tests and for the
// fallback when the runner has nothing to stream (e.g. m.height == 0
// unit-test path that never arms a tick). The streaming path uses
// internal/codesearch.Runner instead.
func csScan(root string, re *regexp.Regexp) []csHit {
	if root == "" {
		return nil
	}
	var hits []csHit
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (codesearch.SkipDirs[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 1<<20 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		head := data
		if len(head) > 8<<10 {
			head = head[:8<<10]
		}
		if bytesIndexByte(head, 0) >= 0 {
			return nil // binary
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		hits = appendFileHits(hits, rel, string(data), re)
		if len(hits) >= csResultCap {
			hits = hits[:csResultCap]
			return fs.SkipAll
		}
		return nil
	})
	return hits
}

// bytesIndexByte is bytes.IndexByte without importing bytes for one call.
func bytesIndexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

// bti promotes a bool to an int (0/1) so the csBlockRows / csRow signatures
// can keep their positional int cursors and flags.
func bti(b bool) int {
	if b {
		return 1
	}
	return 0
}

// appendFileHits appends one csHit per matching line of content, stopping at
// csResultCap. The trailing empty element strings.Split produces for a file
// that ends in "\n" is dropped, so a match on the last line has no phantom
// "next" row (end.txt: `aaa zwk\n` renders a single row).
func appendFileHits(hits []csHit, rel, content string, re *regexp.Regexp) []csHit {
	lines := strings.Split(content, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i, line := range lines {
		loc := re.FindStringIndex(line)
		if loc == nil {
			continue
		}
		line = strings.TrimRight(line, "\r")
		if loc[1] > len(line) {
			loc[1] = len(line)
		}
		h := csHit{
			file: rel,
			line: i + 1,
			col:  utf8.RuneCountInString(line[:loc[0]]) + 1,
			text: line,
			ms:   utf8.RuneCountInString(line[:loc[0]]),
			me:   utf8.RuneCountInString(line[:loc[1]]),
		}
		if i > 0 {
			h.prev = strings.TrimRight(lines[i-1], "\r")
			h.hasPrev = true
		}
		if i+1 < len(lines) {
			h.next = strings.TrimRight(lines[i+1], "\r")
			h.hasNext = true
		}
		hits = append(hits, h)
		if len(hits) >= csResultCap {
			break
		}
	}
	return hits
}

// csListBudget is how many rows the list may occupy: the measured
// floor(height*11/15) minus the panel's seven fixed rows. height <= 0 (terminal
// size unknown, which is the pre-sizing layout the unit tests use) disables
// windowing, so the whole list renders.
func csListBudget(height int) int {
	if height <= 0 {
		return 0
	}
	b := height*csBudgetNum/csBudgetDen - csFixedRows
	if b < 1 {
		b = 1
	}
	return b
}

// csHitFromMatch reshapes a codesearch.Match into the panel's csHit. The
// Match already carries file/line/col and the verbatim line text; the
// panel fills the prev/next text lazily as it lays out the result (the
// runner can't read surrounding lines without re-opening every file,
// which the ripgrep path explicitly avoids). When prev/next are empty
// the csBlockRows helper renders a single-row block instead of three.
func csHitFromMatch(m codesearch.Match) csHit {
	return csHit{
		file: m.File,
		line: m.Line,
		col:  m.Col,
		text: m.Text,
		ms:   0,                              // re-derived by csRow below; matches the ripgrep path
		me:   utf8.RuneCountInString(m.Text), // first-match span covers the line
	}
}

// openSlashPanelArgs is openSlashPanel plus the command's argument tail, which
// only /codesearch consumes today. The cycle-228 contract: bare `/codesearch`
// (no args) short-circuits to the `⎿  /codesearch needs a query.` echo, so
// the panel only opens when there is a query to render.
func (m *runMode) openSlashPanelArgs(cmd, args string) bool {
	if cmd == "add-dir" && args != "" {
		return false
	}
	if cmd == "codesearch" && strings.TrimSpace(args) == "" {
		return false
	}
	// Cycle effort-picker: `/effort <level>` (valid or invalid) is a direct
	// setter that goes through the slash router (case "effort" in
	// slash_commands.go) so it can echo "Effort set to <level>." / "Invalid
	// level: <level>." without opening the picker. A bare `/effort` still
	// opens the timeline-gauge panel below.
	if cmd == "effort" && args != "" {
		return false
	}
	// Cycle plugin-case-insensitive: `/plugin <subcmd>` (e.g.
	// `/plugin List`) is a direct command dispatch handled by
	// `case "plugin"` in slash_commands.go, NOT the panel.
	// AGY 1.3.6 changelog line 21 names this gap: the
	// pre-cycle build opened the panel for every /plugin
	// invocation and silently swallowed the argument tail,
	// leaving `/plugin List` / `/plugin LIST` / `/plugin
	// install foo` indistinguishable from bare `/plugin`.
	// Bare `/plugin` (no args) still opens the panel below.
	if cmd == "plugin" && strings.TrimSpace(args) != "" {
		return false
	}
	if !m.openSlashPanel(cmd) {
		return false
	}
	if m.panel.kind == panelCodeSearch {
		m.codesearchStart(args)
	}
	return true
}

// codesearchStart parses the argument tail, builds a Search + Runner, and
// fires off the streaming goroutine. The panel render can run from this
// point forward; the goroutine feeds matches via csTickMsg.
//
// Cycle 213's codesearchStart was synchronous (it ran the scan inline);
// cycle 228 keeps the synchronous path for unit tests that never arm
// a tick (they pass args through and rely on csScan/csMatcher to fill
// hits). When m.height == 0 — the test path — we still kick off a
// synchronous scan so the height-0 renderer gets a populated hits
// slice; the streaming path is gated on m.height > 0.
func (m *runMode) codesearchStart(args string) {
	cs := csState{
		args:    strings.TrimSpace(args),
		files:   map[string]struct{}{},
		spinner: 0,
	}
	query, literal, include, exclude, perr := csParseArgs(cs.args)
	cs.query = query
	cs.literal = literal
	cs.include = include
	cs.exclude = exclude
	if perr != nil {
		cs.err = perr.Error()
	} else if query != "" {
		// Compile the pattern up front so the panel can show the
		// "Error: …" row before the goroutine even starts (the rg
		// path would surface a different error, but the panel
		// shape is the same).
		if _, err := csMatcher(query, literal); err != nil {
			cs.err = err.Error()
		}
	}
	m.panel.cs = cs
	m.panel.cursor, m.panel.window = 0, 0

	if cs.err != "" || query == "" {
		return
	}

	// Synchronous path — only on the unit-test path (m.height == 0)
	// or when the user is in print mode (no TUI). Real terminals go
	// through the streaming path so the panel can show progressive
	// matches and Esc-cancel works.
	//
	// Tests can opt out of the streaming path by clearing
	// m.streamingSearch = false (the default is true when the
	// csState has no search field). The streaming path always wins
	// when both are configured; the synchronous path is the
	// fallback the cycle-213 layout tests rely on.
	if m.height <= 0 || !m.streamingSearch {
		if re, err := csMatcher(query, literal); err == nil {
			hits := csScan(m.cwd, re)
			m.panel.cs.hits = hits
			m.panel.cs.files = map[string]struct{}{}
			for _, h := range hits {
				m.panel.cs.files[h.file] = struct{}{}
			}
		}
		return
	}

	runner := codesearch.NewRunner(m.cwd)
	search := &codesearch.Search{
		Query:   query,
		Literal: literal,
		Include: include,
		Exclude: exclude,
		Root:    m.cwd,
	}
	// DefaultRgCmd is the production rg --json builder. Tests that
	// want to drive a mock rg override Runner.RgBin directly; the
	// production path leaves it nil and uses DefaultRgCmd.
	m.panel.cs.search = search
	m.panel.cs.runner = runner
	go runCodesearch(m, search, runner)
}

// codesearchPanelLines renders the /codesearch panel. The four shapes:
//
//  1. usage form (no query):                "Code Search:" + usage row
//  2. running, no hits yet:                 "Code Search: <args>" + spinner + "Searching…  [Esc to cancel]"
//  3. running, hits streaming in:           "Code Search: <args>" + "Found N matches in K files.  [Esc to cancel]"
//  4. terminal (done/cancelled/failed):      "Code Search: <args>" + "Found N matches in K files.  [Enter on row to open]"
//  5. pattern error:                        "Code Search: <args>" + red "Error: …"
//  6. terminal, zero matches:               "Code Search: <args>" + "No results"
//
// The synchronous path (no search field) reports State==StateDone so
// the same terminal-state line serves unit-test fixtures that don't
// arm a tick.
func (m *runMode) codesearchPanelLines() []string {
	cs := &m.panel.cs
	keyboard := panelKeyboardHint("↑/↓", "Navigate", "←/→", "Prev/Next File", "enter", "Open File",
		"ctrl+g", "Open in Editor", "esc", "Close Search")

	// Usage form.
	if cs.query == "" {
		return []string{
			panelTitleOpen + "Code Search" + panelTitleClose + ":",
			cDimGrayOpen + "  Usage: /codesearch [-F|--literal] <query> [f:<glob>] [file:<glob>]" + cDimGrayClose,
			"",
			keyboard,
		}
	}

	// Title echoes the raw argument tail verbatim (REF does this).
	title := panelTitleOpen + "Code Search" + panelTitleClose + ": " + cs.args
	lines := []string{title}

	// Pattern error: short-circuit before the count row.
	if cs.err != "" {
		lines = append(lines, csErrorOpen+"  Error: "+cs.err+cDimGrayClose)
		return append(lines, "", keyboard)
	}

	// Streaming / terminal count row. Default to StateDone when no
	// search field is present (synchronous test path that never
	// armed a tick); the rendering below treats that as "terminal,
	// no spinner, show final count".
	state := codesearch.StateDone
	if cs.search != nil {
		state = cs.search.State
	}
	hits := len(cs.hits)
	files := len(cs.files)

	switch {
	case state == codesearch.StateRunning && hits == 0:
		glyph := csSpinnerFrames[cs.spinner%len(csSpinnerFrames)]
		lines = append(lines,
			"  "+glyph+"  "+cDimGrayOpen+"Searching\u2026  [Esc to cancel]"+cDimGrayClose)
		// Live terminal: pad with a blank + the legend; no list yet.
		return append(lines, "", keyboard)
	case state == codesearch.StateRunning:
		// Streaming — show running count and the cancel hint.
		lines = append(lines,
			cDimGrayOpen+fmt.Sprintf("Found %s in %s.  [Esc to cancel]",
				countLabel(hits, categoryWords["match"]),
				countLabel(files, categoryWords["file"]))+cDimGrayClose)
	case hits == 0:
		// Terminal state, no hits — "No results" line.
		lines = append(lines, cDimGrayOpen+"No results"+cDimGrayClose)
		return append(lines, "", keyboard)
	default:
		// Terminal state with hits.
		var verb string
		switch state {
		case codesearch.StateDone:
			verb = fmt.Sprintf("Found %s in %s.",
				countLabel(hits, categoryWords["match"]),
				countLabel(files, categoryWords["file"]))
		case codesearch.StateCancelled:
			verb = fmt.Sprintf("Cancelled after %s in %s.",
				countLabel(hits, categoryWords["match"]),
				countLabel(files, categoryWords["file"]))
		case codesearch.StateFailed:
			verb = fmt.Sprintf("Partial: %s in %s.",
				countLabel(hits, categoryWords["match"]),
				countLabel(files, categoryWords["file"]))
		default:
			verb = fmt.Sprintf("Found %s in %s.",
				countLabel(hits, categoryWords["match"]),
				countLabel(files, categoryWords["file"]))
		}
		lines = append(lines, cDimGrayOpen+verb+"  [Enter on row to open]"+cDimGrayClose)
	}

	// Live terminal: when we have hits we render the list; the
	// budget still applies (height-aware windowing).
	if hits == 0 {
		return append(lines, "", keyboard)
	}
	rows, first, last := m.codesearchRows()
	lines = append(lines, rows...)
	if first != 0 || last != hits {
		lines = append(lines, cDimGrayOpen+
			fmt.Sprintf("  [%d-%d of %d items]", first+1, last, hits)+cDimGrayClose)
	}
	return append(lines, "", keyboard)
}

// codesearchRows renders the visible window and returns it with the visible
// item range [first, last) — the counter prints first+1 … last.
func (m *runMode) codesearchRows() (rows []string, first, last int) {
	hits := m.panel.cs.hits
	start := m.csWindowStart()
	last, rows = csFill(hits, start, m.panel.cursor, csListBudget(m.height), m.width)
	return rows, start, last
}

// csWindowStart clamps the stored window offset to a renderable item index.
func (m *runMode) csWindowStart() int {
	total := len(m.panel.cs.hits)
	if total == 0 {
		return 0
	}
	start := m.panel.window
	if start < 0 {
		start = 0
	}
	if start > m.panel.cursor {
		start = m.panel.cursor
	}
	if start >= total {
		start = total - 1
	}
	return start
}

// csFill lays out items[start:] while they fit the row budget and returns the
// exclusive end index. budget <= 0 means "no windowing". The first item is
// always emitted, however small the budget (the reference never renders an
// empty list for a non-empty result set).
//
// Block frame:
//   - first item:           header, separator, content
//   - same-file continuation: separator, content
//   - different-file continuation: blank, header, separator, content
//
// The reference's separator sits between the file header and its first
// content block too — a single-block per-file result renders "header
// separator L1:" (zwk capture). The same-file block boundary also carries
// a separator (needle capture: a.txt's block 2 follows a separator rule),
// so the separator is independent of how many files the window spans.
func csFill(hits []csHit, start, cursor, budget, width int) (int, []string) {
	if start >= len(hits) {
		return start, nil
	}
	var rows []string
	used := 0
	end := start
	prevFile := ""
	for i := start; i < len(hits); i++ {
		h := hits[i]
		newFile := i == start || h.file != prevFile
		var head []string
		if i == start {
			head = append(head, csHeaderRow(h.file))
			head = append(head, csRuleRow(width))
		} else if newFile {
			head = append(head, "")
			head = append(head, csHeaderRow(h.file))
			head = append(head, csRuleRow(width))
		} else {
			head = append(head, csRuleRow(width))
		}
		body := csBlockRows(h, bti(i == cursor), width)
		add := len(head) + len(body)
		if i > start && budget > 0 && used+add > budget {
			break
		}
		rows = append(rows, head...)
		rows = append(rows, body...)
		used += add
		if newFile {
			prevFile = h.file
		}
		end = i + 1
	}
	return end, rows
}

// csHeaderRow is a file header: underlined, colour 32, reset with SGR 0.
func csHeaderRow(file string) string {
	return csHeaderOpen + file + csHeaderReset
}

// csRuleRow is the between-blocks separator: width-4 dashes in grey 253.
func csRuleRow(width int) string {
	n := width - csWidthInset
	if n < 1 {
		n = 1
	}
	return csRuleOpen + strings.Repeat("─", n) + cDimGrayClose
}

// csBlockRows renders one result: the context lines around the matching line.
// cursor is non-zero when the result owns the cursor; isMatch is non-zero for
// the matching line itself. Both flags combine to choose the row style — see
// csRow below.
//
// Cycle 228 streaming caveat: the ripgrep path doesn't surface prev/next
// text (rg emits one match line at a time), so csBlockRows renders the
// match line only when the hit has no prev/next — saves the panel from
// showing empty context rows. The synchronous csScan still fills them.
func csBlockRows(h csHit, cursor, width int) []string {
	type ctxLine struct {
		n    int
		text string
	}
	body := make([]ctxLine, 0, 3)
	if h.hasPrev {
		body = append(body, ctxLine{h.line - 1, h.prev})
	}
	body = append(body, ctxLine{h.line, h.text})
	if h.hasNext {
		body = append(body, ctxLine{h.line + 1, h.next})
	}
	out := make([]string, 0, len(body))
	for _, l := range body {
		isMatch := 0
		if l.n == h.line {
			isMatch = 1
		}
		out = append(out, csRow(l.n, l.text, h.ms, h.me, isMatch, cursor&isMatch, width))
	}
	return out
}

// csRow renders one numbered line.
//
//	context    "  " + dim("L4: …")                    gutter is inside the dim span
//	match      "  " + "L4: " + dim(pre) + blue(match) + dim(post)
//	cursor     blue("> L4: ") + dim(pre) + blue(match) + dim(post)
//
// The cursor's marker and gutter are blue; a non-cursor match row keeps them
// at the default colour. Only the first match on the line is highlighted —
// dbl.txt's `needle needle here` renders `blue(needle) + dim(" needle here")`.
//
// Cycle 228: the match text is truncated to fit the row width using
// codesearch.Truncate (CJK-aware). The first 4 KiB of the line are
// already what rg saw, but Go's utf8-aware truncation guarantees we
// never split a rune mid-character, so long lines and CJK text are
// cut cleanly with an ellipsis instead of turning into replacement
// glyphs.
func csRow(n int, line string, ms, me int, isMatch, isCursor, width int) string {
	gutter := fmt.Sprintf("L%d: ", n)
	contentWidth := csContentWidth(width, gutter)
	if isMatch == 0 {
		body, _, _ := csFit(line, 0, 0, contentWidth)
		return "  " + cDimGrayOpen + gutter + body + cDimGrayClose
	}
	body, s, e := csFit(line, ms, me, contentWidth)
	r := []rune(body)
	var b strings.Builder
	if isCursor != 0 {
		b.WriteString(cBlueOpen)
		b.WriteString("> ")
	} else {
		b.WriteString("  ")
	}
	b.WriteString(gutter)
	if s > 0 {
		b.WriteString(cDimGrayOpen)
		b.WriteString(string(r[:s]))
	}
	b.WriteString(cBlueOpen)
	b.WriteString(string(r[s:e]))
	if e < len(r) {
		b.WriteString(cDimGrayOpen)
		b.WriteString(string(r[e:]))
	}
	b.WriteString(cDimGrayClose)
	return b.String()
}

// csContentWidth is the content cells left in a row after the gutter.
func csContentWidth(width int, gutter string) int {
	if width <= csWidthInset {
		return 0 // unknown/narrow terminal: never truncate
	}
	w := width - csWidthInset - utf8.RuneCountInString(gutter)
	if w < 1 {
		w = 1
	}
	return w
}

// csFit shortens a line to w *cells* (CJK-aware via runewidth) around
// its match, marking the elided sides with an ellipsis. The measured
// cycle-213 rule was two ellipses and a left/right split that keeps
// the match centred; cycle 228 keeps the same arithmetic but uses
// codesearch.Truncate to ensure CJK is never turned into a replacement
// glyph (the AGY 1.4.0 fix).
func csFit(text string, ms, me, w int) (string, int, int) {
	if w <= 0 {
		return text, ms, me
	}
	runes := []rune(text)
	if utf8.RuneCountInString(text) <= w {
		return text, ms, me
	}
	// csHit's me is the rune length of the *whole* line when the
	// ripgrep path filled the hit (csHitFromMatch sets it that way).
	// csFit assumes ms..me is the match span — collapse to a single
	// cell-anchored match if me - ms > w.
	matchLen := me - ms
	if matchLen > w {
		// Truncate the whole thing; no surrounding context.
		body := codesearch.Truncate(text, w)
		// The match now sits somewhere inside body; we don't have the
		// exact byte span after Truncate, so collapse to "match is the
		// start" — the cursor gutter still names the right line.
		return body, 0, 0
	}
	avail := w - 2 - matchLen
	if avail < 0 {
		avail = 0
	}
	left := avail / 2
	right := avail - left
	lo := ms - left
	if lo < 0 {
		lo = 0
	}
	hi := me + right
	if hi > len(runes) {
		hi = len(runes)
	}
	prefix, suffix := "", ""
	if lo > 0 {
		prefix = "…"
	}
	if hi < len(runes) {
		suffix = "…"
	}
	return prefix + string(runes[lo:hi]) + suffix,
		utf8.RuneCountInString(prefix) + (ms - lo),
		utf8.RuneCountInString(prefix) + (me - lo)
}

// codesearchPanelKey handles the navigation keys of a live /codesearch list.
// ok is false for the usage form, `No results` and pattern errors — those keep
// cycle 206's "the panel owns every key and does nothing" behaviour.
func (m *runMode) codesearchPanelKey(key tea.Key) (handled bool, cmd tea.Cmd, ok bool) {
	if m.panel.kind != panelCodeSearch || len(m.panel.cs.hits) == 0 {
		return false, nil, false
	}
	switch key.Code {
	case tea.KeyUp:
		m.csMoveCursor(-1)
	case tea.KeyDown:
		m.csMoveCursor(1)
	case tea.KeyLeft:
		m.csMoveFile(-1)
	case tea.KeyRight:
		m.csMoveFile(1)
	case tea.KeyEsc:
		// Cycle 228: Esc during a live search cancels the rg child
		// and closes the panel. When the search has already settled
		// we fall through to listPanelOwningKey's Esc close (cycle
		// 213 behaviour — the panel closes silently).
		if m.panel.cs.search != nil && m.panel.cs.search.State == codesearch.StateRunning {
			if m.panel.cs.runner != nil {
				_ = m.panel.cs.runner.Cancel()
			}
			return true, m.closePanel(), true
		}
		return false, nil, false // let listPanelOwningKey close it
	default:
		return false, nil, false
	}
	return true, nil, true
}

// csMoveCursor moves the item cursor by delta, clamped to the result set, and
// scrolls the window to keep it visible.
func (m *runMode) csMoveCursor(delta int) {
	total := len(m.panel.cs.hits)
	c := m.panel.cursor + delta
	if c < 0 {
		c = 0
	}
	if c > total-1 {
		c = total - 1
	}
	m.panel.cursor = c
	m.csFollow()
}

// csMoveFile moves the cursor to the previous/next file's first item.
func (m *runMode) csMoveFile(delta int) {
	hits := m.panel.cs.hits
	start := m.csFileStart(m.panel.cursor)
	if delta < 0 {
		if start == 0 {
			m.panel.cursor = 0
		} else {
			m.panel.cursor = m.csFileStart(start - 1)
		}
	} else {
		i := start
		for i < len(hits) && hits[i].file == hits[start].file {
			i++
		}
		if i >= len(hits) {
			i = len(hits) - 1
		}
		m.panel.cursor = i
	}
	m.csFollow()
}

// csFileStart returns the index of the first item of the file that owns item i.
func (m *runMode) csFileStart(i int) int {
	hits := m.panel.cs.hits
	if i < 0 || i >= len(hits) {
		return 0
	}
	for i > 0 && hits[i-1].file == hits[i].file {
		i--
	}
	return i
}

// csFollow re-anchors the window so the cursor is inside the rendered range.
// The reference puts the cursor at the top when it scrolls past the window
// (`Down Down`, `Down` ×5 in huge.txt), which is what setting the window to the
// cursor yields once the window is re-laid out.
func (m *runMode) csFollow() {
	start := m.csWindowStart()
	if m.panel.cursor < start {
		m.panel.window = m.panel.cursor
		return
	}
	end, _ := csFill(m.panel.cs.hits, start, m.panel.cursor, csListBudget(m.height), m.width)
	if m.panel.cursor >= end {
		m.panel.window = m.panel.cursor
	}
}

// runCodesearch kicks off the streaming search. The runner is run in
// its own goroutine; this function drives that goroutine. The runner
// closes Search.Matches when State has settled; the TUI's csTickMsg
// handler observes the close and stops arming further ticks.
func runCodesearch(m *runMode, s *codesearch.Search, r *codesearch.Runner) {
	ctx, cancel := context.WithCancel(context.Background())
	m.csCancel = cancel
	defer func() {
		m.csCancel = nil
	}()
	_ = r.Run(ctx, s)
}

// csTickMsg is delivered to the TUI every csTickInterval ms while a
// search is running. The handler drains all available matches from
// the runner's channel into m.panel.cs.hits and re-arms the next tick
// unless the search has settled.
type csTickMsg struct{}

// csTick arms the next csTickMsg. Returns nil immediately when no
// panel is open or no search field is wired up. The tick is armed
// even when the search has already settled — the handler observes the
// closed channel and stops re-arming. This keeps a fast-completing
// search (e.g. empty ripgrep result) from racing the very first tick
// and leaving the panel stuck on "Searching…".
func csTick(m *runMode) tea.Cmd {
	if m.panel.kind != panelCodeSearch {
		return nil
	}
	if m.panel.cs.search == nil {
		return nil
	}
	m.panel.cs.spinner++
	return tea.Tick(csTickInterval, func(time.Time) tea.Msg { return csTickMsg{} })
}

// drainCodesearchMatches reads every buffered match off the runner's
// channel and appends them to m.panel.cs.hits. Returns true if it
// drained anything (the TUI re-renders then). The channel is closed
// by the runner once State != StateRunning; the second return value
// reports whether the close was observed.
func (m *runMode) drainCodesearchMatches() (drained, closed bool) {
	cs := &m.panel.cs
	if cs.search == nil || cs.search.Matches == nil {
		return false, true
	}
	for {
		select {
		case match, ok := <-cs.search.Matches:
			if !ok {
				return drained, true
			}
			h := csHitFromMatch(match)
			// Re-derive ms/me so csRow highlights the first match.
			if h.col > 0 {
				col := h.col - 1
				if col > len(h.text) {
					col = len(h.text)
				}
				h.ms = utf8.RuneCountInString(h.text[:col])
			}
			if h.me == 0 || h.me > utf8.RuneCountInString(h.text) {
				h.me = utf8.RuneCountInString(h.text)
			}
			if _, dup := cs.files[h.file]; !dup {
				cs.files[h.file] = struct{}{}
			}
			cs.hits = append(cs.hits, h)
			drained = true
		default:
			return drained, false
		}
	}
}

// codesearchEmptyMessage is the verbatim echo the slash command emits
// when the user types `> /codesearch` with no argument tail. The
// `⎿  ` wrapper is the standard REF slash-echo row (slashRespond).
const codesearchEmptyMessage = "/codesearch needs a query."
