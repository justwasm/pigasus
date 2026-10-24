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

package cliapp

import (
	"context"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/codesearch"
)

// csResultCap is the reference's hard ceiling on reported matches: 1200
// matching lines render as `Found 1000 results` and `[… of 1000 items]`.
const csResultCap = codesearch.MaxHits

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
// and extracts any f:/file: glob flags.
func csParseArgs(args string) (query string, literal bool, include, exclude []string, err error) {
	return codesearch.ParseArgsFlat(args)
}

// csMatcher compiles the query with the reference's semantics: a fixed string
// under -F/--literal, a regex otherwise, case-insensitive while the pattern is
// all lowercase. The runner uses its own smart-case for the rg path; this
// helper stays for the local-fallback scan (used by the unit tests and by
// the `csPanelLines` / csHit-from-string helper when the search is
// reconstructed for cycle-213 tests).
//
// The rule itself now lives in internal/codesearch (see CompileMatcher).
func csMatcher(query string, literal bool) (*regexp.Regexp, error) {
	return codesearch.CompileMatcher(query, literal)
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
//
// The walk itself now lives in internal/codesearch (see Scan).
func csScan(root string, re *regexp.Regexp) []csHit {
	return codesearch.Scan(root, re, csResultCap)
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
	// `/model <name>` is a direct setter: the reference changes the model
	// and echoes "Model set to <label>" instead of opening the Switch
	// Model panel. Only a bare `/model` opens the panel.
	// 【一手】 agy 1.4.6, live.
	if cmd == "model" && strings.TrimSpace(args) != "" {
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
	if m.height <= 0 || !m.streamingSearch {
		m.csStartSync(query, literal)
		return
	}

	m.csStartStreaming(query, literal, include, exclude)
}

func (m *runMode) csStartSync(query string, literal bool) {
	if re, err := csMatcher(query, literal); err == nil {
		hits := csScan(m.cwd, re)
		m.panel.cs.hits = hits
		m.panel.cs.files = map[string]struct{}{}
		for _, h := range hits {
			m.panel.cs.files[h.File] = struct{}{}
		}
	}
}

func (m *runMode) csStartStreaming(query string, literal bool, include, exclude []string) {
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
		if m.panel.cs.search != nil && m.panel.cs.search.CurrentState() == codesearch.StateRunning {
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
		for i < len(hits) && hits[i].File == hits[start].File {
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
	for i > 0 && hits[i-1].File == hits[i].File {
		i--
	}
	return i
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
			h := codesearch.HitFromMatch(match)
			// Re-derive ms/me so csRow highlights the first match.
			if h.Col > 0 {
				col := h.Col - 1
				if col > len(h.Text) {
					col = len(h.Text)
				}
				h.MS = utf8.RuneCountInString(h.Text[:col])
			}
			if h.ME == 0 || h.ME > utf8.RuneCountInString(h.Text) {
				h.ME = utf8.RuneCountInString(h.Text)
			}
			if _, dup := cs.files[h.File]; !dup {
				cs.files[h.File] = struct{}{}
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
