// Package codesearchpanel holds the /codesearch panel: the streaming
// code-search result list.
//
// # What it is
//
// AGY 1.4.0 added /codesearch (cycle 228): the reference spawns
// `rg --json`, shows a "Searching…" header that ticks into "Found N
// matches in K files" as the matches stream in, lets the user hit Esc
// to kill the child mid-search, and falls back to a local walk when the
// rg binary cannot be executed (typically an endpoint security agent
// blocking it).
//
// The frame is the reference's own: the title echoes the verbatim
// argument tail, the status line counts the matches and the files, the
// rows are laid out as a file header plus a rule plus the matching line
// with its two context lines, and the footer is the keyboard legend.
//
//	Code Search: needle
//	Found 5 matches in 4 files.  [Enter on row to open]
//	a.txt
//	────────────
//	  2 │ line2 needle here
//
// # Where it lives
//
// The panel is here: the argument tail, the query, the hits, the file
// set, the spinner, the cursor and the scroll window. The search and
// the runner it fills live in internal/features/tools/codesearch — the
// panel drives them, it does not reimplement them.
//
// The host keeps only the seams: the search root, the terminal's size,
// whether the progressive path is on, the runner factory, the way the
// panel reads a file, the way it opens one in the editor and the way
// out of the panel. The panel is rebuilt every time it opens, so a
// stale query, a stale hit set or a stale cursor never survives a
// close.
//
// # The file viewer
//
// Enter opens the highlighted result in the panel's own file viewer,
// scrolled to the matching line, and Ctrl+G opens it in the external
// editor (official-docs/cli/commands/codesearch.md:36-37).
//
// The viewer is the reference's interactive viewer: the cursor moves over
// the lines, c leaves a comment on the cursor's line, d drops it, /
// searches the file, and the comments that were not sent yet are
// collected on the "Unsent Comments" screen when the user leaves
// (official-docs/cli/commands/codesearch.md:74-100).
//
// The comment model, the gutter marker, the wire format and the screen
// live in internal/ui/views/linecomments: the reference has one comment
// model and one screen, so the facts are shared and every viewer that
// carries line comments answers through them.
package codesearchpanel

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/codesearch"
	"github.com/justwasm/pigasus/internal/ui/views/linecomments"
)

// TickInterval is the cadence at which the panel drains the runner's
// matches channel. 80 ms is fast enough that the user sees the count
// tick up smoothly while typing a query, slow enough that a
// 1200-match search doesn't flood the message queue.
const TickInterval = 80 * time.Millisecond

// ResultCap is the reference's hard ceiling on reported matches: 1200
// matching lines render as `Found 1000 results` and `[… of 1000 items]`.
const ResultCap = codesearch.MaxHits

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves
// as if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Root returns the directory the search runs in.
	Root func() string
	// Size returns the terminal's width and height in cells.
	//
	// The width lays the rows out (the rule under a file header is as
	// wide as the panel) and the height sizes the list window.
	Size func() (width, height int)
	// Streaming reports whether the progressive path is on.
	//
	// The host turns it off when there is no TUI to tick on: the
	// synchronous path fills the panel in one shot instead.
	Streaming func() bool
	// NewRunner builds the runner for a search root.
	//
	// The default is codesearch.NewRunner. A test replaces it to drive
	// a mock ripgrep.
	NewRunner func(root string) *codesearch.Runner
	// ReadFile reads the file the panel wants to view.
	//
	// The panel never touches the disk: the host owns the filesystem
	// and answers with the file's lines. The second value is the
	// message to show when the file cannot be viewed — it is empty
	// when the read worked.
	//
	// The path is relative to the search root.
	ReadFile func(path string) (lines []string, message string, ok bool)
	// OpenEditor opens a file in the external editor, at a line.
	//
	// The host owns the process: the panel decides that the file is
	// opened and hands over the path and the line, and the host starts
	// the editor.
	OpenEditor func(path string, line int) tea.Cmd
	// SendComments hands the comments to the agent as the next
	// message.
	//
	// The panel decides that the comments are sent and hands the text
	// over; the host closes the panel and starts the turn. The text is
	// the reference's own wire format — the literal "<file>" tag, the
	// file, the line and the comment, one comment per line (【strings】
	// "<file>%s:%d:%s\n").
	SendComments func(text string) tea.Cmd
	// Close leaves the panel.
	Close func() tea.Cmd
}

// Panel is the /codesearch panel.
//
// The panel owns the whole surface: the argument tail, the query, the
// hits, the file set, the spinner, the cursor and the scroll window.
// Nothing of it is left in the host but the seams.
type Panel struct {
	seams Seams

	// args is the raw argument tail, echoed verbatim in the title.
	args string
	// query is the pattern actually searched for.
	query string
	// literal says the pattern is a fixed string (-F / --literal).
	literal bool
	// include and exclude are the f:<glob> / file:<glob> / path:<glob>
	// filters and their "-" exclusions.
	include []string
	exclude []string
	// err is non-empty when the pattern did not compile.
	err string
	// hits are the matches already drained into the panel.
	hits []codesearch.Hit
	// files is the set of files a drained match came from.
	files map[string]struct{}

	// search is the codesearch.Search the runner is filling; nil when
	// no search is running.
	search *codesearch.Search
	// runner is the runner the panel called Run() on; nil when no
	// runner (the synchronous path).
	runner *codesearch.Runner
	// cancel stops the search: it kills the child and ends the
	// fallback walk.
	cancel context.CancelFunc
	// spinner is the Braille frame index for the loading header.
	spinner int

	// cursor is the selected hit's index.
	cursor int
	// window is the index of the first visible hit.
	window int

	// viewer is the open file: the file's lines, the matching line, the
	// cursor, the scroll window, the comments and the inline inputs. It
	// is nil when no file is open — the panel shows the result list
	// then.
	viewer *fileViewer

	// comments are the line comments the user left, one store per file.
	//
	// The store is per file because a comment is anchored to a line of
	// a file, and the two files' comments never mix. The store outlives
	// the viewer: leaving a file and opening it again shows the
	// comments that are still there.
	comments map[string]*linecomments.Store

	// unsent is the "Unsent Comments" screen: the comments that wait
	// for the user's answer. It is nil when the screen is not up.
	unsent *unsentScreen
}

// unsentScreen is the reference's "Unsent Comments" screen: the file the
// comments belong to and the comments themselves.
//
// The screen is the panel's: the panel shows it, and the panel answers
// the three keys. The header, the answers and the rows live in
// internal/ui/views/linecomments — the reference has one such screen.
type unsentScreen struct {
	// file is the file the comments belong to.
	file string
	// comments are the comments that wait for an answer.
	comments []linecomments.Comment
}

// New builds the /codesearch panel.
//
// The panel opens on the usage form: the query is empty, so the
// reference's "Usage: /codesearch [-F|--literal] <query>" line is what
// the panel shows.
func New(seams Seams) *Panel {
	return &Panel{seams: seams, files: map[string]struct{}{}}
}

// ---- the panel's state, read from outside ------------------------------

// Query is the pattern the panel searched for.
func (p *Panel) Query() string {
	if p == nil {
		return ""
	}
	return p.query
}

// Args is the raw argument tail the panel echoes in its title.
func (p *Panel) Args() string {
	if p == nil {
		return ""
	}
	return p.args
}

// Hits are the matches the panel has drained so far.
func (p *Panel) Hits() []codesearch.Hit {
	if p == nil {
		return nil
	}
	return p.hits
}

// Err is the pattern error the panel shows, or the empty string.
func (p *Panel) Err() string {
	if p == nil {
		return ""
	}
	return p.err
}

// Cursor is the selected hit's index.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// Search is the search the runner is filling, or nil when no search is
// running.
func (p *Panel) Search() *codesearch.Search {
	if p == nil {
		return nil
	}
	return p.search
}

// Runner is the runner the panel started, or nil when the panel ran the
// synchronous path.
func (p *Panel) Runner() *codesearch.Runner {
	if p == nil {
		return nil
	}
	return p.runner
}

// ---- the seams ---------------------------------------------------------

// root is the directory the search runs in.
func (p *Panel) root() string {
	if p == nil || p.seams.Root == nil {
		return ""
	}
	return p.seams.Root()
}

// size is the terminal's width and height in cells.
func (p *Panel) size() (int, int) {
	if p == nil || p.seams.Size == nil {
		return 0, 0
	}
	return p.seams.Size()
}

// streaming reports whether the progressive path is on.
func (p *Panel) streaming() bool {
	if p == nil || p.seams.Streaming == nil {
		return false
	}
	return p.seams.Streaming()
}

// newRunner builds the runner for a search root.
func (p *Panel) newRunner(root string) *codesearch.Runner {
	if p != nil && p.seams.NewRunner != nil {
		return p.seams.NewRunner(root)
	}
	return codesearch.NewRunner(root)
}

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}

// openEditor opens a file in the external editor, at a line.
//
// The panel decides that the file is opened and hands the path and the
// line over; the host starts the editor.
func (p *Panel) openEditor(path string, line int) tea.Cmd {
	if p == nil || p.seams.OpenEditor == nil {
		return nil
	}
	return p.seams.OpenEditor(path, line)
}

// storeFor returns the comment store of a file, creating it the first
// time the file is opened.
//
// The store is the panel's: it owns the comments of every file the user
// annotated, so a comment survives leaving the file and coming back to
// it.
func (p *Panel) storeFor(path string) *linecomments.Store {
	if p == nil {
		return linecomments.NewStore()
	}
	if p.comments == nil {
		p.comments = map[string]*linecomments.Store{}
	}
	s, ok := p.comments[path]
	if !ok {
		s = linecomments.NewStore()
		p.comments[path] = s
	}
	return s
}

// ---- the comment flow ---------------------------------------------------

// leaveViewer leaves the open file.
//
// The comments the user left on the file and did not send yet are
// collected: the "Unsent Comments" screen asks what to do with them
// (official-docs/cli/commands/codesearch.md:96-100). Without a pending
// comment the file closes and the result list is back.
func (p *Panel) leaveViewer() (handled bool, cmd tea.Cmd, ok bool) {
	v := p.viewer
	if v == nil {
		return false, nil, false
	}
	if v.comments.Len() == 0 {
		p.viewer = nil
		return true, nil, true
	}
	p.unsent = &unsentScreen{file: v.path, comments: v.comments.All()}
	return true, nil, true
}

// unsentKey owns the keys of the "Unsent Comments" screen.
//
// The screen is modal: it claims every key under it, the way the
// reference's own overlay does. The three answers are the capital
// letters Y and N and Esc — a lower-case y is not a yes
// (official-docs/cli/commands/diff.md:130-136).
func (p *Panel) unsentKey(key tea.Key) (handled bool, cmd tea.Cmd, ok bool) {
	switch linecomments.Decide(key) {
	case linecomments.DecisionSend:
		return true, p.sendComments(), true
	case linecomments.DecisionDiscard:
		// N exits without sending: the comments are dropped and the
		// panel is left.
		p.discardComments()
		return true, p.close(), true
	case linecomments.DecisionCancel:
		// Esc cancels and keeps editing: the screen goes down and the
		// viewer is back.
		p.unsent = nil
		return true, nil, true
	}
	return true, nil, true
}

// sendComments hands the pending comments to the host.
//
// The panel decides that the comments are sent and hands the text over
// in the reference's wire format; the host closes the panel and starts
// the turn (invariant 163 — the host owns what happens next).
func (p *Panel) sendComments() tea.Cmd {
	s := p.unsent
	p.unsent = nil
	if s == nil {
		return nil
	}
	if st := p.storeFor(s.file); st != nil {
		st.Reset()
	}
	if p.seams.SendComments == nil {
		return nil
	}
	return p.seams.SendComments(linecomments.FormatAll(s.file, s.comments))
}

// discardComments drops the pending comments.
//
// The comments are dropped on their own file: the other files' comments
// are not affected.
func (p *Panel) discardComments() {
	s := p.unsent
	p.unsent = nil
	if s == nil {
		return
	}
	if st := p.storeFor(s.file); st != nil {
		st.Reset()
	}
}

// ---- the search --------------------------------------------------------

// Start parses the argument tail and starts the search.
//
// The argument tail is the reference's: `-F` / `--literal` are flags,
// `f:<glob>` (with the aliases `file:<glob>` and `path:<glob>`) are the
// repeatable include globs, a leading "-" on a filter makes it an
// exclusion, and the rest is the pattern verbatim.
//
// An empty pattern is the usage form, not an error: the panel shows
// the reference's usage line. A pattern that does not compile is an
// error: the panel shows it in place of the results.
//
// The synchronous path fills the panel in one shot; it is the path the
// host takes when there is no TUI to tick on. The streaming path starts
// the runner and lets the tick drain the matches as they arrive.
func (p *Panel) Start(args string) {
	if p == nil {
		return
	}
	p.args = strings.TrimSpace(args)
	p.hits = nil
	p.files = map[string]struct{}{}
	p.cursor, p.window = 0, 0
	p.spinner = 0
	p.search, p.runner, p.cancel = nil, nil, nil
	p.viewer = nil
	p.unsent = nil

	query, literal, include, exclude, perr := codesearch.ParseArgsFlat(p.args)
	p.query, p.literal, p.include, p.exclude = query, literal, include, exclude
	if perr != nil {
		// The empty query is the usage form, not a failure.
		p.err = ""
		return
	}
	p.err = ""
	// Compile the pattern up front so the panel can show the
	// "Error: …" row before the runner even starts (the rg path would
	// surface a different error, but the panel shape is the same).
	if _, err := codesearch.CompileMatcher(query, literal); err != nil {
		p.err = err.Error()
		return
	}

	_, height := p.size()
	if height <= 0 || !p.streaming() {
		p.startSync()
		return
	}
	p.startStreaming()
}

// startSync walks the tree and fills the panel in one shot.
//
// It is the path the host takes when there is no TUI to tick on: the
// panel's window is unknown, so nothing would ever drain the runner's
// channel.
func (p *Panel) startSync() {
	re, err := codesearch.CompileMatcher(p.query, p.literal)
	if err != nil {
		return
	}
	hits := codesearch.Scan(p.root(), re, ResultCap)
	p.hits = hits
	p.files = map[string]struct{}{}
	for _, h := range hits {
		p.files[h.File] = struct{}{}
	}
}

// startStreaming starts the runner in its own goroutine.
//
// The matches reach the panel through the search's channel; the tick
// drains them.
func (p *Panel) startStreaming() {
	root := p.root()
	runner := p.newRunner(root)
	search := &codesearch.Search{
		Query:   p.query,
		Literal: p.literal,
		Include: p.include,
		Exclude: p.exclude,
		Root:    root,
	}
	// The channel is made here and not by the runner: the panel's first
	// tick can arrive before the runner's goroutine has been scheduled,
	// and a nil channel would read as "the search is over" — the panel
	// would sit on "Searching…" forever.
	search.Matches = make(chan codesearch.Match, 64)
	// The context is made here and not in the goroutine, so the cancel
	// the panel hands to Esc is never written from the runner's
	// goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	p.search = search
	p.runner = runner
	p.cancel = cancel
	go func() {
		_ = runner.Run(ctx, search)
	}()
}

// running reports whether a search is still in flight.
func (p *Panel) running() bool {
	if p == nil || p.search == nil {
		return false
	}
	return p.search.CurrentState() == codesearch.StateRunning
}

// cancelSearch stops the in-flight search.
//
// The runner's Cancel kills the rg child; the context cancel ends the
// fallback walk, which has no child to kill.
func (p *Panel) cancelSearch() {
	if p == nil {
		return
	}
	if p.runner != nil {
		_ = p.runner.Cancel()
	}
	if p.cancel != nil {
		p.cancel()
	}
}

// Cancel stops the in-flight search, if any.
//
// The host calls it when the panel closes: a search that is still
// running would otherwise outlive the panel — the runner's goroutine
// and the ripgrep child it spawned.
func (p *Panel) Cancel() {
	if p == nil {
		return
	}
	p.cancelSearch()
}

// TickMsg is delivered to the TUI every TickInterval while a search is
// running. The handler drains all available matches from the runner's
// channel into the panel's hits and re-arms the next tick unless the
// search has settled.
type TickMsg struct{}

// Tick arms the next TickMsg.
//
// The tick is armed even when the search has already settled — the
// handler observes the closed channel and stops re-arming. This keeps a
// fast-completing search (e.g. an empty ripgrep result) from racing
// the very first tick and leaving the panel stuck on "Searching…".
func (p *Panel) Tick() tea.Cmd {
	if p == nil || p.search == nil {
		return nil
	}
	p.spinner++
	return tea.Tick(TickInterval, func(time.Time) tea.Msg { return TickMsg{} })
}

// Drain reads every buffered match off the runner's channel and appends
// them to the panel's hits.
//
// The channel is closed by the runner once the search has settled; the
// second return value reports whether the close was observed.
func (p *Panel) Drain() (drained, closed bool) {
	if p == nil || p.search == nil {
		return false, false
	}
	if p.search.Matches == nil {
		// The runner has not created the channel yet: the search is not
		// over, so the tick keeps going.
		return false, false
	}
	if p.files == nil {
		p.files = map[string]struct{}{}
	}
	for {
		select {
		case match, ok := <-p.search.Matches:
			if !ok {
				return drained, true
			}
			h := codesearch.HitFromMatch(match)
			// Re-derive ms/me so the row highlights the first match.
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
			if _, dup := p.files[h.File]; !dup {
				p.files[h.File] = struct{}{}
			}
			p.hits = append(p.hits, h)
			drained = true
		default:
			return drained, false
		}
	}
}
