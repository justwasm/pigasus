// inspect.go — the /debug surface's view state, keys and layout.
//
// # What this is
//
// One full-viewport surface with five views over the same question —
// "what is this session doing":
//
//	Logs        the session log file (the reference's /logs)
//	Wire        the request the next turn puts on the wire
//	Trajectory  every step of the conversation, folded by turn
//	Requests    the HTTP exchanges this session made
//	Context     how big the context is, and how full the window is
//
// # Why a panel and not three panels
//
// Three panels would mean three registry kinds, three route-table rows,
// three picker entries and three half-height layouts for one question.
// The views are the panel's own vocabulary, so they live in the panel and
// the tree keeps exactly one /debug.
//
// # One arithmetic
//
// The window is computed once, by layout(), and every consumer reads that
// one answer: the render clips with it, the keys clamp against it, and a
// click is mapped through the same row indices. The /context panel's
// scroll bug — a render that reserved one budget and a key handler that
// clamped against another — is the failure this shape exists to prevent,
// so there is no second place to compute a bound.
package debugpanel

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/wirelog"
	"github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// Seams are the host's answers the panel cannot give itself.
//
// Every one of them is optional: a panel built without a seam renders the
// view's empty frame, which keeps the package usable from a test with no
// host.
type Seams struct {
	// Logs returns the session log's facts.
	Logs func() State

	// Wire returns the Wire view's facts.
	Wire func() WireState

	// Trajectory returns the Trajectory view's facts.
	Trajectory func() TrajectoryState

	// Requests returns the Requests view's facts: the HTTP exchanges this
	// session made, as the recorder holds them.
	Requests func() RequestState

	// Context returns the Context view's facts: the measured size of the
	// context and its share of the model's window.
	Context func() ContextState

	// Hydrate loads a cold step's body, keyed by the step's index. The
	// host answers with a command so the read happens off the update
	// goroutine.
	Hydrate func(index int64) tea.Cmd

	// Read returns a row's full body, keyed by the row's stable ID.
	//
	// It is the reader's seam, and it is separate from Hydrate because
	// the two answer different questions: Hydrate fills in a body the
	// panel already has a row for, while Read is asked when a user wants
	// to read a body the list deliberately clipped. The command answers
	// with a ReaderMsg so the panel never blocks on a database read.
	Read func(id string) tea.Cmd

	// Copy writes text to the clipboard.
	Copy func(text string) tea.Cmd

	// Close leaves the panel.
	Close func() tea.Cmd

	// Width and Height are the terminal's size.
	Width  func() int
	Height func() int

	// Follow and SetFollow read and write the log view's auto-tail.
	Follow    func() bool
	SetFollow func(bool)
}

// Panel is the /debug surface.
type Panel struct {
	seams Seams
	view  View

	// scroll is the Logs view's window offset; the two foldable views keep
	// their window in their own list, next to the cursor that drives it.
	scroll int

	summary    foldList
	wire       foldList
	trajectory foldList
	requests   foldList
	context    foldList

	// wireState and trajState are the facts the foldable views were last
	// built from, so a heading and its list are one frame of the same
	// conversation rather than two reads.
	wireState WireState
	trajState TrajectoryState
	reqState  RequestState
	ctxState  ContextState

	// sortByBytes orders the Wire view by size instead of request order.
	sortByBytes bool

	// hydrated records the steps whose cold bodies have been asked for, so
	// a second Enter on the same row does not queue a second read.
	hydrated map[int64]bool

	// barRow is the y offset of the bar within the frame, and barSpans
	// are the label ranges, so a click on the bar can become a view.
	//
	// wireHeadingRows and trajHeadingRows are the heading heights the last
	// draw used. They are recorded rather than recomputed so a click maps
	// to the frame on screen: the heading comes from the host's facts, and
	// asking for them again could describe a different frame.
	barRow             int
	barSpans           []tuilib.TabSpan
	summaryHeadingRows int
	wireHeadingRows    int
	trajHeadingRows    int
	reqHeadingRows     int
	ctxHeadingRows     int

	// reader is the row whose full body is open, or nil when the list is
	// what is on screen. readPending names the row the panel asked the
	// host for, so a second Enter does not queue a second read and a
	// stale answer is recognised rather than opened.
	reader            *reader
	readPending       string
	readerRows        int
	readerHeadingRows int

	// frame is the terminal size the current repaint is laid out for.
	// It is zero between frames, so the seams are still asked then.
	frame frameSize

	// frames caches the host's facts for the length of one repaint.
	//
	// The Wire view's state is not cheap: it re-serialises every tool
	// declaration and token-counts every message (measured at ~1.2 ms for
	// 60 messages and 80 tool steps). The host asks for the frame twice —
	// once to draw it, once through the registry's RowCount — so the seam
	// ran twice per wheel notch on top of everything else. A wheel should
	// cost a slice, not a request assembly.
	//
	// The cache is keyed by generation, which every input that can change
	// the rows bumps: a key, a wheel, a view switch, a fold, a width
	// change. A background turn that adds a step changes what the seam
	// would answer, so the tick that notices it bumps the generation too.
	frames panelFrames
}

// panelFrames is the per-repaint memo of the host's facts.
type panelFrames struct {
	// gen increments whenever the rows could have changed.
	gen uint64
	// wireGen and trajGen are the gens the cached rows were built at.
	//
	// There is no reqGen: the Requests facts are re-read every frame (see
	// syncRequests) because the recorder can add a record without going
	// through the update loop that bumps the generation.
	wireGen uint64
	trajGen uint64
	// wire and traj are the cached states the rows were built from, kept
	// for the headings; the rows themselves are cached too, because
	// building them is not free (WireRows measured at ~770 µs for 400
	// messages — the sort and the per-row column arithmetic).
	wire        WireState
	traj        TrajectoryState
	req         RequestState
	ctx         ContextState
	wireRows    []FoldRow
	trajRows    []FoldRow
	reqRows     []FoldRow
	ctxRows     []FoldRow
	summaryRows []FoldRow
	// wireKey and trajKey are the inputs the cached rows were built with
	// that the generation does not cover: a sort toggle and a width
	// change alter the rows without altering the facts.
	wireKey    wireRowsKey
	trajKey    int
	wireRowGen uint64
	trajRowGen uint64
	wireSet    bool
	trajSet    bool
	// reqKey is what the cached Requests rows were built from, so an
	// unchanged ring at an unchanged width is a slice rather than a
	// rebuild.
	reqKey reqRowsKey
	// ctxKey is the same for the Context rows.
	ctxKey ctxRowsKey
}

// ctxRowsKey is everything that changes the Context rows.
type ctxRowsKey struct {
	used         int
	window       int
	requestBytes int64
	messages     int
	width        int
}

// reqRowsKey is everything that changes the Requests rows.
//
// The newest Seq rather than the count, for the reason the field's comment
// gives: once the ring is full — 500 exchanges into a long session — every
// arrival evicts one and the length never moves again.
type reqRowsKey struct {
	newest int64
	width  int
}

// wireRowsKey is everything besides the seam's own facts that changes the
// Wire rows.
type wireRowsKey struct {
	width       int
	sortByBytes bool
}

// invalidate marks the cached facts stale.
//
// Every mutator calls this. A missed call is a stale frame rather than a
// crash, so the rule is "bump it where the rows can change", not "bump it
// everywhere".
func (p *Panel) invalidate() {
	if p != nil {
		p.frames.gen++
	}
}

// Invalidate tells the panel the host's facts have moved.
//
// The panel caches the Wire and Trajectory states for the length of one
// repaint (see panelFrames). That is safe for everything the panel does
// itself — a wheel, a fold, a view switch — because those bump the
// generation on the way in. But a turn running in the background adds
// steps the panel cannot see, so the host's live refresh calls this to
// say "what I would answer has changed".
//
// Without it the panel would draw the previous repaint's steps forever:
// the tick repaints the frame, the frame comes out of the memo, and the
// step list never grows.
func (p *Panel) Invalidate() {
	p.invalidate()
}

// New returns a new /debug surface on the Summary view.
//
// Summary is the front door because it is the index of the other five: model,
// endpoint, occupancy, exchange health, compactions and the last error, one
// screen. That is the screen someone opening a diagnostic surface wants
// first — the alternative is a user paging through five views to find out
// which one has the thing that broke.
//
// It is also why the command is /debug and /logs is an alias that lands here
// too. An alias that opened a different view would not be an alias: the name
// typed would silently change what is on screen, which is a difference no
// user can see in the command they wrote.
func New(seams Seams) *Panel {
	return &Panel{seams: seams, sortByBytes: true, barRow: -1}
}

// View reports the visible view.
func (p *Panel) View() View {
	if p == nil {
		return ViewSummary
	}
	return p.view
}

// SetView moves the panel to a view.
//
// Moving views closes the reader: a body was opened from one view's list,
// and carrying it across would leave the bar naming a view whose rows are
// not what is below it.
func (p *Panel) SetView(v View) {
	if p == nil {
		return
	}
	if int(v) < 0 || int(v) >= ViewCount() {
		return
	}
	p.reader = nil
	p.view = v
	// A different view means a different list over different facts, so
	// the memo is not reused across the switch.
	p.invalidate()
}

// RowCount returns the selectable rows of the visible view.
//
// The registry asks this for the router's cursor bound; the panel owns its
// own cursor, so this is a courtesy to the router.
func (p *Panel) RowCount() int {
	if p == nil {
		return 0
	}
	switch p.view {
	case ViewSummary:
		p.syncSummary()
		return p.summary.rowCount()
	case ViewWire:
		p.syncWire()
		return p.wire.rowCount()
	case ViewTrajectory:
		p.syncTrajectory()
		return p.trajectory.rowCount()
	case ViewRequests:
		p.syncRequests()
		return p.requests.rowCount()
	case ViewContext:
		p.syncContext()
		return p.context.rowCount()
	default:
		st := p.logsState()
		return len(Document(st))
	}
}

// layout is the one arithmetic: how many rows the body may occupy, and
// therefore how far it may scroll.
type layout struct {
	// visible is how many body rows fit between the heading and the
	// pinned footer.
	visible int
}

// frameChrome is the rows the frame spends on everything but the body:
// the view bar, the blank under it, the blank under the heading, the
// counter and the legend. (The heading itself varies, so it is counted
// per draw rather than here.)
const frameChrome = 1 + 1 + 1 + 1 + 1

// layoutFor computes the frame's regions for the terminal height.
//
// The frame is exactly `height` rows tall, always: the caller clips the
// frame to the terminal, so a frame that drew one row too many would have
// its first row — the view bar — clipped off the top. Every row is
// accounted for here rather than by the renderers, so the size and the
// layout cannot disagree.
func (p *Panel) layoutFor(height int, headingRows int) layout {
	if height <= 0 {
		height = 24
	}
	visible := height - frameChrome - headingRows
	if visible < 1 {
		visible = 1
	}
	return layout{visible: visible}
}

// height is the terminal height, or a default the package can lay out.
//
// While a frame is being drawn this is the size snapshotted at the top of
// Lines. The host's Height seam is not a constant: it subtracts the
// status area's rows, and computing those runs the status row's render,
// which reads the endpoints config from disk. The frame asks for the
// height a dozen times (every layout, every bodyStart, the wheel's
// bound), so asking the host each time was a dozen file reads per
// repaint — the largest single cost in a scroll, and invisible in the
// panel's own code.
func (p *Panel) height() int {
	if p != nil && p.frame.height > 0 {
		return p.frame.height
	}
	if p != nil && p.seams.Height != nil {
		return p.seams.Height()
	}
	return 0
}

// width is the terminal width the views clip to.
//
// It is snapshotted for the same reason as height: the views clip every
// row to it, and the host's seam is not free.
func (p *Panel) width() int {
	if p != nil && p.frame.width > 0 {
		return p.frame.width
	}
	if p != nil && p.seams.Width != nil {
		return p.seams.Width()
	}
	return 0
}

// frameSize is the terminal size one repaint was laid out for.
//
// It is zero outside Lines, so a key handler that asks for the height
// between two frames still gets the host's answer.
type frameSize struct {
	width  int
	height int
}

// seamWidth and seamHeight ask the host directly, bypassing the frame
// snapshot. They exist so height() and width() can fill the snapshot
// without recursing into themselves.
func (p *Panel) seamWidth() int {
	if p != nil && p.seams.Width != nil {
		return p.seams.Width()
	}
	return 0
}

func (p *Panel) seamHeight() int {
	if p != nil && p.seams.Height != nil {
		return p.seams.Height()
	}
	return 0
}

// logsState reads the log view's facts.
func (p *Panel) logsState() State {
	if p != nil && p.seams.Logs != nil {
		return p.seams.Logs()
	}
	return State{Width: p.width(), Height: p.height()}
}

// following reports the log view's auto-tail.
func (p *Panel) following() bool {
	if p != nil && p.seams.Follow != nil {
		return p.seams.Follow()
	}
	return true
}

// setFollowing writes the log view's auto-tail.
func (p *Panel) setFollowing(v bool) {
	if p != nil && p.seams.SetFollow != nil {
		p.seams.SetFollow(v)
	}
}

// maxLogScroll is how far the log document may scroll with the window the
// render uses.
func (p *Panel) maxLogScroll() int {
	st := p.logsState()
	st.Width = p.width()
	st.Height = p.height()
	st.Follow = p.following()
	// The bound is measured with the same heading the render counts. A
	// bound computed from a zero-row heading would let the keys scroll
	// past the window the frame actually draws.
	vis := p.layoutFor(p.height(), len(HeaderLines(st))).visible
	return maxOffset(len(Spec(st).Lines), vis)
}

// maxOffset bounds an offset so the window never scrolls past the end.
func maxOffset(total, visible int) int {
	if visible < 1 {
		visible = 1
	}
	if n := total - visible; n > 0 {
		return n
	}
	return 0
}

// Lines renders the frame.
//
// Every row is clipped to the terminal's width here, once, for all three
// views. The host does not clip horizontally — it lays the frame out and
// lets the terminal wrap — so a row one column too wide would wrap onto
// the next line and push the whole frame down by one, which is the same
// class of damage as a frame one row too tall. Clipping at the one place
// that produces rows means a view cannot forget it.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	// Snapshot the terminal size for the whole repaint. Every layout
	// call, every bodyStart and the wheel's bound read it, and the
	// host's height seam is not free (it runs the status row, which
	// reads the endpoints config). Asking once per frame rather than
	// once per call is the difference between one file read and a
	// dozen. See height().
	p.frame = frameSize{width: p.seamWidth(), height: p.seamHeight()}
	defer func() { p.frame = frameSize{} }()

	var out []string
	switch {
	case p.reader != nil:
		// The reader replaces the list rather than sitting over it: it is
		// the same frame, one level deeper, and the bar above it is still
		// the surface's.
		out = p.readerFrame()
	default:
		switch p.view {
		case ViewSummary:
			out = p.summaryFrame()
		case ViewLogs:
			out = p.logsFrame()
		case ViewWire:
			out = p.wireFrame()
		case ViewTrajectory:
			out = p.trajectoryFrame()
		case ViewRequests:
			out = p.requestsFrame()
		case ViewContext:
			out = p.contextFrame()
		default:
			out = p.summaryFrame()
		}
	}
	return clipRows(out, p.width())
}

// clipMemo remembers rows already measured against a width.
//
// ansi.StringWidth and ansi.Truncate walk a row a grapheme cluster at a
// time, which made clipping the most expensive part of a scrolling
// frame. A scroll re-shows the same rows, so the answers repeat: the
// memo turns the second look at a row into a map hit.
//
// It is bounded because the key includes the row's own text; a comment
// to an unbounded map would leak on a long session. Past the bound the
// map is dropped wholesale rather than evicted one key at a time — a
// wrong answer is impossible (the key is the input), and a reset costs
// one frame's worth of measurements.
const clipMemoMax = 4096

type clipKey struct {
	row   string
	width int
}

var clipMemo = struct {
	m map[clipKey]string
}{m: make(map[clipKey]string, 256)}

func clipMemoLookup(row string, width int) (string, bool) {
	v, ok := clipMemo.m[clipKey{row: row, width: width}]
	return v, ok
}

func clipMemoStore(row string, width int, out string) {
	if len(clipMemo.m) >= clipMemoMax {
		clipMemo.m = make(map[clipKey]string, 256)
	}
	clipMemo.m[clipKey{row: row, width: width}] = out
}

// clipRows cuts every row to width columns, leaving the rows alone when
// the width is unknown.
//
// ansi.Truncate is used rather than a rune count so a styled row is cut
// at a cell boundary and its escape sequences are closed: a row cut
// mid-sequence would leave the colour on for everything below it.
func clipRows(rows []string, width int) []string {
	if width <= 0 {
		return rows
	}
	for i, r := range rows {
		if clipped, ok := clipMemoLookup(r, width); ok {
			rows[i] = clipped
			continue
		}
		out := r
		if ansi.StringWidth(r) > width {
			out = ansi.Truncate(r, width, "…")
		}
		clipMemoStore(r, width, out)
		rows[i] = out
	}
	return rows
}

// barLines renders the bar and its blank separator, recording the hit
// regions for the click handler.
func (p *Panel) barLines() []string {
	line, spans := TabBarWidth(p.view, p.width())
	p.barSpans = spans
	p.barRow = 0
	return []string{line, ""}
}

// footerLines renders the counter and the legend, pinned below the body.
func (p *Panel) footerLines(counter, legend string) []string {
	return []string{counter, legendRow(legend, p.width())}
}

// legendRow renders the key hint, dropping whole entries that do not fit.
//
// Entries are separated by "  ·  " and ordered most- to least-important,
// so a narrow terminal loses the tail of the hint rather than a fragment
// of its last word: "esc c…" names no key, and a hint that names no key is
// worse than the shorter hint that still names four real ones.
func legendRow(text string, width int) string {
	if width > 0 {
		const sep = "  ·  "
		parts := strings.Split(text, sep)
		row := "  " + parts[0]
		for _, p := range parts[1:] {
			next := row + sep + p
			if ansi.StringWidth(next) > width {
				break
			}
			row = next
		}
		text = strings.TrimPrefix(row, "  ")
	}
	return "  " + palette.DimGrayOpen + text + palette.DimGrayClose
}

// logsFrame renders the log view: the bar, the file's own header, the
// window, and the footer.
func (p *Panel) logsFrame() []string {
	st := p.logsState()
	st.Width = p.width()
	st.Height = p.height()
	st.Follow = p.following()
	spec := Spec(st)
	// The log view's heading is its own two pinned rows: the badge and
	// the file. They are counted here so the frame is exactly as tall as
	// the terminal gives it — the caller clips from the top, so an
	// over-tall frame loses the view bar.
	header := HeaderLines(st)
	lay := p.layoutFor(p.height(), len(header))

	p.scroll = clampOffset(p.scroll, maxOffset(len(spec.Lines), lay.visible))
	// Auto-tail is drawn as the tail, not as a remembered offset: the
	// document can grow between two frames, and an offset pinned at open
	// time would leave the view looking at the old end while the badge
	// said it was following. Following means the last page, always.
	if st.Follow {
		p.scroll = maxOffset(len(spec.Lines), lay.visible)
	}
	rows, start, end, _ := window(spec.Lines, p.scroll, lay.visible)

	out := p.barLines()
	out = append(out, header...)
	out = append(out, "")
	out = append(out, rows...)
	// Pad below the body so the footer stays at the bottom of the frame
	// rather than riding up under a short log.
	for i := len(rows); i < lay.visible; i++ {
		out = append(out, "")
	}

	counter := counterRow(start, end, len(spec.Lines))
	if p.following() {
		counter += palette.DimGrayOpen + "  ·  auto-tail ON" + palette.DimGrayClose
	} else {
		counter += palette.DimGrayOpen + "  ·  auto-tail PAUSED (f to resume)" + palette.DimGrayClose
	}
	out = append(out, p.footerLines(counter, logLegend())...)
	return out
}

// wireFrame renders the Wire view.
func (p *Panel) wireFrame() []string {
	p.syncWire()
	heading := WireHeaderLines(p.wireState)
	p.wireHeadingRows = len(heading)
	lay := p.layoutFor(p.height(), len(heading))

	out := p.barLines()
	out = append(out, heading...)
	out = append(out, "")
	body := p.wire.windowLines(lay.visible)
	out = append(out, body...)
	out = append(out, padRows(lay.visible-len(body))...)
	start, end, total := p.wire.position(lay.visible)
	counter := counterRow(start, end, total) + cursorSuffix(p.wire.cursor, p.wire.rowCount())
	out = append(out, p.footerLines(counter, WireLegend())...)
	return out
}

// padRows returns n blank rows, so a short body leaves the footer at the
// bottom of the frame rather than riding up under it.
func padRows(n int) []string {
	if n <= 0 {
		return nil
	}
	out := make([]string, n)
	return out
}

// trajectoryFrame renders the Trajectory view.
func (p *Panel) trajectoryFrame() []string {
	p.syncTrajectory()
	heading := TrajectoryHeaderLines(p.trajState)
	p.trajHeadingRows = len(heading)
	lay := p.layoutFor(p.height(), len(heading))

	out := p.barLines()
	out = append(out, heading...)
	out = append(out, "")
	body := p.trajectory.windowLines(lay.visible)
	out = append(out, body...)
	out = append(out, padRows(lay.visible-len(body))...)
	start, end, total := p.trajectory.position(lay.visible)
	counter := counterRow(start, end, total) + cursorSuffix(p.trajectory.cursor, p.trajectory.rowCount())
	out = append(out, p.footerLines(counter, TrajLegend())...)
	return out
}

// syncRequests reads the ring and rebuilds the Requests rows when they moved.
func (p *Panel) syncRequests() {
	// The facts are asked for every frame, unlike Wire's and Trajectory's.
	//
	// Those two describe the conversation, and every change to it goes
	// through the host's update loop, which bumps the generation. The
	// recorder does not: a request can complete in a transport goroutine
	// that nobody told the panel about, so a generation-keyed memo would
	// draw a table that stopped at whatever was in the ring the last time
	// something else moved. The cost is a copy of a bounded slice (500
	// records), which is microseconds; the Wire view's 1.2 ms was token
	// counting, not copying.
	var rs RequestState
	if p.seams.Requests != nil {
		rs = p.seams.Requests()
	}
	p.frames.req = rs
	rs.Width = p.width()
	p.reqState = rs

	// The key is the newest Seq together with the width: an arrival bumps
	// the first, a resize the second, and neither alone is enough. An
	// eviction cannot happen without an arrival, so it needs no term of
	// its own — and the count is deliberately not part of the key, because
	// a full ring's count stops changing exactly when the table is busiest.
	var newest int64
	if n := len(rs.Records); n > 0 {
		newest = rs.Records[n-1].Seq
	}
	key := reqRowsKey{newest: newest, width: rs.Width}
	if p.frames.reqKey != key || len(p.frames.reqRows) == 0 {
		p.frames.reqRows = RequestRows(rs)
		p.frames.reqKey = key
	}
	p.requests.reset(p.frames.reqRows)
}

// requestsFrame renders the Requests view.
func (p *Panel) requestsFrame() []string {
	p.syncRequests()
	heading := RequestHeaderLines(p.reqState)
	p.reqHeadingRows = len(heading)
	lay := p.layoutFor(p.height(), len(heading))

	out := p.barLines()
	out = append(out, heading...)
	out = append(out, "")
	body := p.requests.windowLines(lay.visible)
	out = append(out, body...)
	out = append(out, padRows(lay.visible-len(body))...)
	start, end, total := p.requests.position(lay.visible)
	counter := counterRow(start, end, total) + cursorSuffix(p.requests.cursor, p.requests.rowCount())
	out = append(out, p.footerLines(counter, RequestLegend())...)
	return out
}

// syncContext rebuilds the Context rows.
//
// The facts are re-read every frame, like the Requests view's, and for a
// stronger reason: two of this view's numbers come from the recorder (the
// request bytes) and one from the token store (the last turn's usage), and
// neither goes through the host's update loop. A generation-keyed memo
// would leave the occupancy at whatever it was when something else last
// moved — which is precisely the number the user opened this view to watch.
//
// The cost is the seam itself, which is a handful of atomic loads and one
// slice copy; it is not the Wire view's token counting.
func (p *Panel) syncContext() {
	var cs ContextState
	if p.seams.Context != nil {
		cs = p.seams.Context()
	}
	cs.Width = p.width()
	p.ctxState = cs
	p.frames.ctx = cs

	key := ctxRowsKey{used: cs.LastPromptTokens, window: cs.WindowTokens,
		requestBytes: ctxRequestBytes(cs), messages: cs.Messages, width: cs.Width}
	if p.frames.ctxKey != key || len(p.frames.ctxRows) == 0 {
		p.frames.ctxRows = ContextRows(cs)
		p.frames.ctxKey = key
	}
	p.context.reset(p.frames.ctxRows)
}

// ctxRequestBytes is the recorded request size the Context rows were built
// from, or 0 when nothing was recorded.
func ctxRequestBytes(cs ContextState) int64 {
	if cs.Wire == nil {
		return 0
	}
	return cs.Wire.RequestBytes
}

// contextFrame renders the Context view.
func (p *Panel) contextFrame() []string {
	p.syncContext()
	heading := ContextHeaderLines(p.ctxState)
	p.ctxHeadingRows = len(heading)
	lay := p.layoutFor(p.height(), len(heading))

	out := p.barLines()
	out = append(out, heading...)
	out = append(out, "")
	body := p.context.windowLines(lay.visible)
	out = append(out, body...)
	out = append(out, padRows(lay.visible-len(body))...)
	start, end, total := p.context.position(lay.visible)
	counter := counterRow(start, end, total) + cursorSuffix(p.context.cursor, p.context.rowCount())
	out = append(out, p.footerLines(counter, ContextLegend())...)
	return out
}

// syncSummary rebuilds the Summary rows from context and requests facts.
func (p *Panel) syncSummary() {
	p.syncContext()
	p.syncRequests()
	p.frames.summaryRows = SummaryRows(p.ctxState, p.reqState, p.width())
	p.summary.reset(p.frames.summaryRows)
}

// summaryFrame renders the Summary view.
func (p *Panel) summaryFrame() []string {
	p.syncSummary()
	heading := SummaryHeaderLines(p.ctxState, p.reqState)
	p.summaryHeadingRows = len(heading)
	lay := p.layoutFor(p.height(), len(heading))

	out := p.barLines()
	out = append(out, heading...)
	out = append(out, "")
	body := p.summary.windowLines(lay.visible)
	out = append(out, body...)
	out = append(out, padRows(lay.visible-len(body))...)
	start, end, total := p.summary.position(lay.visible)
	counter := counterRow(start, end, total) + cursorSuffix(p.summary.cursor, p.summary.rowCount())
	out = append(out, p.footerLines(counter, SummaryLegend())...)
	return out
}

// syncWire rebuilds the Wire rows and keeps the default fold state.
//
// The host's facts are fetched once per repaint rather than once per
// caller: drawing the frame and asking for the row count are two reads of
// the same state, and the seam is the expensive half (it re-serialises
// every tool declaration). See panelFrames.
func (p *Panel) syncWire() {
	if !p.frames.wireSet || p.frames.wireGen != p.frames.gen {
		var ws WireState
		if p.seams.Wire != nil {
			ws = p.seams.Wire()
		}
		p.frames.wire = ws
		p.frames.wireGen = p.frames.gen
		p.frames.wireSet = true
	}
	ws := p.frames.wire
	ws.Width = p.width()
	ws.SortByBytes = p.sortByBytes
	p.wireState = ws

	// The rows depend on the facts (covered by gen) and on the two
	// display choices below (covered by the key). Rebuilding them for an
	// unchanged pair is what made one wheel notch cost a sort.
	key := wireRowsKey{width: ws.Width, sortByBytes: ws.SortByBytes}
	if p.frames.wireKey != key || p.frames.wireRowGen != p.frames.gen ||
		len(p.frames.wireRows) == 0 {
		p.frames.wireRows = WireRows(ws)
		p.frames.wireKey = key
		p.frames.wireRowGen = p.frames.gen
	}
	p.wire.reset(p.frames.wireRows)
	if !p.wire.synced {
		p.wire.openAll()
		p.wire.synced = true
	}
}

// syncTrajectory rebuilds the Trajectory rows.
//
// Turns open by default so their steps are visible while each step's body
// stays folded; a flat step list stays folded so the outline is what shows.
func (p *Panel) syncTrajectory() {
	if !p.frames.trajSet || p.frames.trajGen != p.frames.gen {
		var ts TrajectoryState
		if p.seams.Trajectory != nil {
			ts = p.seams.Trajectory()
		}
		p.frames.traj = ts
		p.frames.trajGen = p.frames.gen
		p.frames.trajSet = true
	}
	ts := p.frames.traj
	ts.Width = p.width()
	p.trajState = ts

	// The rows must be rebuilt when the facts change, not only when the
	// width does: a background turn adding a step changes the steps
	// without changing the width, and keying on width alone left the list
	// at its old length forever.
	if p.frames.trajKey != ts.Width || p.frames.trajRowGen != p.frames.gen ||
		len(p.frames.trajRows) == 0 {
		p.frames.trajRows = TrajectoryRows(ts)
		p.frames.trajKey = ts.Width
		p.frames.trajRowGen = p.frames.gen
	}
	p.trajectory.reset(p.frames.trajRows)
	if !p.trajectory.synced {
		if len(ts.TurnsList) > 0 {
			for _, r := range p.trajectory.rows {
				p.trajectory.open[r.ID] = true
			}
		}
		p.trajectory.synced = true
	}
}

// counterRow is the position line: the visible range and the total.
func counterRow(start, end, total int) string {
	if total == 0 {
		return "  " + palette.DimGrayOpen + "(no rows)" + palette.DimGrayClose
	}
	return "  " + palette.DimGrayOpen +
		plural(start, end, total) + palette.DimGrayClose
}

// plural renders "(a–b of K lines)".
func plural(start, end, total int) string {
	return "(" + strconv.Itoa(start) + "–" + strconv.Itoa(end) + " of " + strconv.Itoa(total) + " lines)"
}

// cursorSuffix names the cursor's row, for the two cursor views.
func cursorSuffix(cursor, rows int) string {
	if rows == 0 {
		return ""
	}
	return "   " + palette.DimGrayOpen + "L" + strconv.Itoa(cursor+1) + "/" + strconv.Itoa(rows) + palette.DimGrayClose
}

// logLegend is the Logs view's key hint, ordered like TrajLegend's so a
// narrow terminal keeps the key that leaves the surface.
func logLegend() string {
	return "↑/↓ scroll  ·  esc close  ·  f follow  ·  g/G top/end  ·  tab next view"
}

// HandleKey processes the panel's keys.
//
// The chrome keys (tab, shift+tab, 1/2/3) work in every view; the rest go
// to the visible one.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	// The reader is a level inside the panel, so it gets the key first:
	// the scroll keys it uses must scroll the body rather than move the
	// list's cursor, and esc must back out one level rather than close
	// the surface under it.
	if p.reader != nil {
		return p.readerKey(key)
	}
	if p.chromeKey(key) {
		return true, nil
	}
	switch p.view {
	case ViewSummary:
		return p.foldKey(&p.summary, key, ViewSummary)
	case ViewWire:
		return p.foldKey(&p.wire, key, ViewWire)
	case ViewTrajectory:
		return p.foldKey(&p.trajectory, key, ViewTrajectory)
	case ViewRequests:
		return p.foldKey(&p.requests, key, ViewRequests)
	case ViewContext:
		return p.foldKey(&p.context, key, ViewContext)
	default:
		return p.logKey(key)
	}
}

// chromeKey answers the keys that mean the same thing in every view.
func (p *Panel) chromeKey(key tea.Key) bool {
	if key.Code == tea.KeyTab || key.String() == "tab" {
		if key.Mod.Contains(tea.ModShift) {
			p.SetView(p.view.Prev())
		} else {
			p.SetView(p.view.Next())
		}
		return true
	}
	if key.Code == tea.KeyLeft || key.Code == tea.KeyRight {
		// The bar says "←/→ or tab to cycle", so the arrows must work: a
		// hint that names a key the panel ignores is worse than no hint.
		if key.Code == tea.KeyLeft {
			p.SetView(p.view.Prev())
		} else {
			p.SetView(p.view.Next())
		}
		return true
	}
	if key.Mod == 0 {
		switch key.Text {
		case "0", "1":
			p.SetView(ViewSummary)
			return true
		case "2":
			p.SetView(ViewLogs)
			return true
		case "3":
			p.SetView(ViewWire)
			return true
		case "4":
			p.SetView(ViewTrajectory)
			return true
		case "5":
			p.SetView(ViewRequests)
			return true
		case "6":
			p.SetView(ViewContext)
			return true
		}
	}
	return false
}

// logKey is the Logs view's keys: a document's scroll, plus the tail.
func (p *Panel) logKey(key tea.Key) (bool, tea.Cmd) {
	if key.Mod == 0 {
		switch key.Text {
		case "f", "F":
			p.setFollowing(!p.following())
			if p.following() {
				p.scroll = p.maxLogScroll()
			}
			return true, nil
		}
	}
	max := p.maxLogScroll()
	switch tuilib.ClassifyViewerNavKey(key) {
	case tuilib.NavUp:
		p.scroll = clampOffset(p.scroll-1, max)
		// Moving up is the user taking the view off the tail, even when
		// the document is too short to have moved: the intent is what the
		// badge reports, not whether the offset changed.
		p.setFollowing(false)
		return true, nil
	case tuilib.NavDown:
		p.scroll = clampOffset(p.scroll+1, max)
		p.setFollowing(p.scroll >= max)
		return true, nil
	case tuilib.NavPageUp:
		p.scroll = clampOffset(p.scroll-p.pageRows(), max)
		p.setFollowing(false)
		return true, nil
	case tuilib.NavPageDown:
		p.scroll = clampOffset(p.scroll+p.pageRows(), max)
		p.setFollowing(p.scroll >= max)
		return true, nil
	case tuilib.NavHalfPageUp:
		p.scroll = clampOffset(p.scroll-p.pageRows()/2, max)
		p.setFollowing(false)
		return true, nil
	case tuilib.NavHalfPageDown:
		p.scroll = clampOffset(p.scroll+p.pageRows()/2, max)
		p.setFollowing(p.scroll >= max)
		return true, nil
	case tuilib.NavTop:
		p.scroll = 0
		p.setFollowing(false)
		return true, nil
	case tuilib.NavBottom:
		p.scroll = max
		p.setFollowing(true)
		return true, nil
	case tuilib.NavCancel:
		// NavCancel is esc *and* q: tuilib classifies both as the cancel
		// key because every viewer in the tree closes on either. Claiming
		// the key and then doing nothing with it is the one answer that is
		// wrong — the keystroke vanishes and the panel looks stuck.
		return true, p.close()
	}
	return false, nil
}

// close asks the host to leave the panel, if it gave a seam for it.
func (p *Panel) close() tea.Cmd {
	if p != nil && p.seams.Close != nil {
		return p.seams.Close()
	}
	return nil
}

// pageRows is a page key's step: the window's own height, one row short
// so the line under the fold stays visible.
func (p *Panel) pageRows() int {
	if n := p.layoutFor(p.height(), 0).visible - 1; n > 0 {
		return n
	}
	return 1
}

// bodyRows is how many body lines the view has room for, given the
// heading it just drew.
//
// The wheel needs this to know how far the window may scroll. It reads
// the heading row count the last frame recorded rather than recomputing
// the heading, so a wheel that arrives between two draws scrolls against
// the geometry that is actually on screen.
func (p *Panel) bodyRows(headingRows int) int {
	return p.layoutFor(p.height(), headingRows).visible
}

// foldKey is the foldable views' keys: a cursor over rows.
func (p *Panel) foldKey(l *foldList, key tea.Key, v View) (bool, tea.Cmd) {
	if key.Mod == 0 {
		switch key.Text {
		case "v", "V":
			// The explicit "read this row" key. Enter already reads a row
			// that has nothing to fold, but a foldable row would eat it:
			// the fold is on screen, and the body behind it is what v is
			// for.
			return p.readCursor(l)
		case "o", "O":
			l.expandAll()
			return true, nil
		case "p", "P":
			l.collapseAll()
			l.toCursor(false)
			return true, nil
		case "s", "S":
			if v == ViewWire {
				// A new order means new rows: bump the generation so the
				// memo does not hand back the previous sort.
				p.sortByBytes = !p.sortByBytes
				p.invalidate()
				return true, nil
			}
		case "y", "Y":
			if p.seams.Copy != nil {
				if text := l.cursorClipboardText(); text != "" {
					return true, p.seams.Copy(text)
				}
			}
			return true, nil
		case "c":
			if v == ViewRequests && p.seams.Copy != nil {
				if r, ok := p.selectedRequestRecord(); ok {
					return true, p.seams.Copy(CurlCommand(r))
				}
			}
		case "C":
			if v == ViewRequests && p.seams.Copy != nil {
				if r, ok := p.selectedRequestRecord(); ok {
					return true, p.seams.Copy(MarkdownExchange(r))
				}
			}
		}
	}
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		l.moveCursor(-1)
		return true, nil
	case tuilib.NavDown:
		l.moveCursor(1)
		return true, nil
	case tuilib.NavPageUp:
		l.moveCursor(-p.pageRows())
		return true, nil
	case tuilib.NavPageDown:
		l.moveCursor(p.pageRows())
		return true, nil
	case tuilib.NavTop:
		l.toCursor(false)
		return true, nil
	case tuilib.NavBottom:
		l.toCursor(true)
		return true, nil
	case tuilib.NavSelect:
		return true, p.activate(l, v)
	case tuilib.NavCancel:
		return true, p.close()
	}
	return false, nil
}

// activate is Enter on the cursor's row: load a cold body, read a row's
// full text, or fold a row.
//
// The three are one key because they are one question — "show me what
// this row has" — and which answer applies is the row's own shape, not
// the user's choice:
//
//	cold    the body is on disk, so Enter loads it
//	readable the row carries an ID the host can answer for, so Enter reads it
//	foldable the body is here, so Enter opens it in place
//
// A row that is both readable and foldable folds first: the folded body is
// on screen already, and reading it is the second Enter.
func (p *Panel) activate(l *foldList, v View) tea.Cmd {
	rows := l.flattened()
	if l.cursor < 0 || l.cursor >= len(rows) {
		return nil
	}
	row := rows[l.cursor]
	if v == ViewSummary && strings.HasPrefix(row.ID, "jump/") {
		switch row.ID {
		case "jump/requests":
			p.SetView(ViewRequests)
		case "jump/context":
			p.SetView(ViewContext)
		case "jump/logs":
			p.SetView(ViewLogs)
		case "jump/wire":
			p.SetView(ViewWire)
		case "jump/trajectory":
			p.SetView(ViewTrajectory)
		}
		return nil
	}
	if row.Cold && v == ViewTrajectory && p.seams.Hydrate != nil {
		var idx int64
		if n, _ := fmt.Sscanf(row.ID, "traj/%d", &idx); n == 1 {
			if p.hydrated == nil {
				p.hydrated = make(map[int64]bool)
			}
			if p.hydrated[idx] {
				return nil
			}
			p.hydrated[idx] = true
			return p.seams.Hydrate(idx)
		}
		return nil
	}
	if row.Foldable() {
		l.toggle()
		return nil
	}
	return p.openReader(row)
}

// openReader asks the host for the row's full body.
//
// The row's own detail is the fallback a hostless panel reads, so the
// reader is useful in a test and in a panel built without a session.
func (p *Panel) openReader(row FoldRow) tea.Cmd {
	return p.read(row.ID, row.Detail)
}

// readCursor opens the cursor's row in the reader, whatever its fold
// state: this is the explicit "show me the whole thing" key, as opposed
// to Enter's "show me what this row has".
func (p *Panel) readCursor(l *foldList) (bool, tea.Cmd) {
	rows := l.flattened()
	if l.cursor < 0 || l.cursor >= len(rows) {
		return true, nil
	}
	return true, p.openReader(rows[l.cursor])
}

// Click handles a *press* at a frame-relative cell.
//
// A press on the bar switches view. A press on a body row puts the cursor
// there and nothing else: the fold or the open waits for the release, so
// the press can also be the anchor of a drag. It used to fold on the
// press, which meant a press on a body row was consumed by the panel and
// the host never saw the drag begin — the text under the pointer could be
// selected only by starting the drag on some other row. A press anywhere
// else is left to the host, whose drag-selection owns the rest of the
// surface.
//
// Act answers what the release does: the row the press landed on, or
// ok=false when the press was not on a body row.
func (p *Panel) Click(row, col int) (bool, tea.Cmd) {
	if p == nil || row < 0 {
		return false, nil
	}
	// A click only means something against the frame the user is looking
	// at, and that frame is the one the last draw recorded. Redrawing here
	// would rebuild the rows the click is about to index into, so a click
	// would land on whatever row happened to be at that offset afterwards.
	if len(p.barSpans) == 0 {
		return false, nil
	}
	if row == p.barRow {
		for _, span := range p.barSpans {
			if col >= span.Start && col < span.End {
				// A click on the bar while reading a body means both
				// things at once: leave the body and go to the view the
				// label names. A click that only did the first would look
				// dead, because the bar it landed on was painted.
				p.reader = nil
				p.SetView(View(span.Index))
				return true, nil
			}
		}
		return false, nil
	}
	if p.reader != nil {
		// The reader's body is text, not a tree: a click in it belongs to
		// the host's drag selection, which is how a user copies a line
		// without the copy key.
		return false, nil
	}
	// The body starts after the bar, its separator, the heading block and
	// the heading's separator. The heading's height is the one the last
	// draw used, which is why it is recorded rather than recomputed: the
	// heading is the host's facts, and re-asking for them here would
	// describe a frame that is not on screen.
	switch p.view {
	case ViewSummary:
		p.clickPlace(&p.summary, row, p.bodyStart(p.summaryHeadingRows))
		return true, nil
	case ViewWire:
		p.clickPlace(&p.wire, row, p.bodyStart(p.wireHeadingRows))
		return true, nil
	case ViewTrajectory:
		p.clickPlace(&p.trajectory, row, p.bodyStart(p.trajHeadingRows))
		return true, nil
	case ViewRequests:
		p.clickPlace(&p.requests, row, p.bodyStart(p.reqHeadingRows))
		return true, nil
	case ViewContext:
		p.clickPlace(&p.context, row, p.bodyStart(p.ctxHeadingRows))
		return true, nil
	}
	return false, nil
}

// SelectionGutter is how many leading cells of each reader row are the
// gutter rather than the body, or 0 when no reader is open.
//
// The host asks this when it copies a drag out of the reader: the line
// number is the reader's furniture, so a selection that began in the
// gutter must not paste it.
func (p *Panel) SelectionGutter() int {
	if p == nil || p.reader == nil {
		return 0
	}
	bodyTotal := 0
	for _, l := range p.reader.lines(p.width()) {
		if readerLineIsBody(l) {
			bodyTotal++
		}
	}
	return viewer.SelectionIndent(bodyTotal)
}

// Act runs the action a completed click on the press's row means: it
// folds or unfolds a foldable row, or opens a leaf row's body.
//
// The host calls it on the release of a press that did not become a drag,
// so a drag across the body selects text and a plain click still does
// what a click on a tree row does everywhere else.
func (p *Panel) Act() (bool, tea.Cmd) {
	if p == nil || p.reader != nil {
		return false, nil
	}
	switch p.view {
	case ViewSummary:
		return true, p.activate(&p.summary, ViewSummary)
	case ViewWire:
		return true, p.activate(&p.wire, ViewWire)
	case ViewTrajectory:
		return true, p.activate(&p.trajectory, ViewTrajectory)
	case ViewRequests:
		return true, p.activate(&p.requests, ViewRequests)
	case ViewContext:
		return true, p.activate(&p.context, ViewContext)
	}
	return false, nil
}

func (p *Panel) selectedRequestRecord() (wirelog.Record, bool) {
	rows := p.requests.flattened()
	if p.requests.cursor < 0 || p.requests.cursor >= len(rows) {
		return wirelog.Record{}, false
	}
	return p.findRequestRecordByID(rows[p.requests.cursor].ID)
}

func (p *Panel) findRequestRecordByID(id string) (wirelog.Record, bool) {
	seq, ok := requestRowSeq(id)
	if !ok {
		return wirelog.Record{}, false
	}
	for _, r := range p.reqState.Records {
		if r.Seq == seq {
			return r, true
		}
	}
	return wirelog.Record{}, false
}

// bodyStart is the frame row the body begins at, for a heading of n rows.
func (p *Panel) bodyStart(headingRows int) int {
	return 1 + 1 + headingRows + 1
}

// clickPlace maps a frame row to a list line and puts the cursor on it.
//
// Three coordinate systems meet here and each step is a different one:
//
//	frame row      what the terminal reported, relative to the panel
//	window offset  which display line the body's first row is showing
//	display line   the line that row actually draws
//
// The frame row is *not* a display line: the body starts partway down
// the frame, and it shows the list from its window, not from its first
// line. Subtracting only the body start would index the unscrolled list,
// so a click after a scroll would act on whatever row happened to be at
// that offset at the top — a tree that folds the wrong branch.
//
// The display line is in turn not a selectable row: an open row prints
// detail lines under itself, so the two diverge after the first body.
// The mapping therefore ends at the line's own row index rather than
// using the offset as one.
func (p *Panel) clickPlace(l *foldList, row, start int) bool {
	idx := l.window + row - start
	if idx < 0 {
		return false
	}
	lines := l.lines()
	if idx >= len(lines) {
		return false
	}
	l.cursor = lines[idx].row
	return true
}

// Scroll moves the visible view by delta rows, for the mouse wheel.
func (p *Panel) Scroll(delta int) bool {
	if p == nil {
		return false
	}
	if p.reader != nil {
		// The wheel scrolls the body it is over, like any pager — the
		// window moves and the cursor is dragged along only when it would
		// leave the window. Keys are what move the cursor; see readerKey.
		visible := p.readerRows
		if visible < 1 {
			visible = p.layoutFor(p.height(), p.readerHeadingRows).visible
		}
		doc := p.reader.numberedDoc(p.width())
		max := maxOffset(len(doc), visible)
		old := p.reader.scroll
		p.reader.scroll = clampOffset(p.reader.scroll+delta, max)
		// Drag the cursor into the new window so a key press acts on
		// something the user can see.
		if line := p.reader.line; line < p.reader.scroll || line >= p.reader.scroll+visible {
			if delta < 0 {
				p.reader.line = p.reader.scroll
			} else {
				end := p.reader.scroll + visible - 1
				if end > len(doc)-1 {
					end = len(doc) - 1
				}
				if end >= 0 {
					p.reader.line = end
				}
			}
		}
		return p.reader.scroll != old
	}
	switch p.view {
	case ViewSummary:
		p.summary.scrollWindow(delta, p.bodyRows(p.summaryHeadingRows))
		return true
	case ViewWire:
		// The wheel scrolls the viewport, like the transcript and like
		// /artifact. The cursor is dragged along only when it would
		// otherwise leave the window; j/k is what moves the selection.
		//
		// This used to call moveCursor, which made the wheel move the
		// `>` highlight and left the viewport to follow it — a different
		// gesture from every other scrollable surface in the app, and
		// the reason the wheel felt sticky here.
		p.wire.scrollWindow(delta, p.bodyRows(p.wireHeadingRows))
		return true
	case ViewTrajectory:
		p.trajectory.scrollWindow(delta, p.bodyRows(p.trajHeadingRows))
		return true
	case ViewRequests:
		// Without this arm the wheel fell through to the Logs view's
		// offset and scrolled a document that was not on screen.
		p.requests.scrollWindow(delta, p.bodyRows(p.reqHeadingRows))
		return true
	case ViewContext:
		p.context.scrollWindow(delta, p.bodyRows(p.ctxHeadingRows))
		return true
	default:
		max := p.maxLogScroll()
		if delta < 0 {
			// A wheel-up is the user leaving the tail. Without this the
			// renderer would pin the view back to the end on the next
			// frame and the wheel would look dead.
			p.setFollowing(false)
		}
		p.scroll = clampOffset(p.scroll+delta, max)
		if delta > 0 && p.scroll >= max {
			p.setFollowing(true)
		}
		return true
	}
}

// clampOffset bounds v to [0, max].
func clampOffset(v, max int) int {
	if max < 0 {
		max = 0
	}
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}

// window returns the visible slice of lines and the 1-based range.
func window(lines []string, offset, visible int) (rows []string, start, end int, clamped int) {
	if visible < 1 {
		visible = 1
	}
	clamped = clampOffset(offset, maxOffset(len(lines), visible))
	hi := clamped + visible
	if hi > len(lines) {
		hi = len(lines)
	}
	rows = lines[clamped:hi]
	start = clamped + 1
	end = clamped + visible
	if end > len(lines) {
		end = len(lines)
	}
	if end < start {
		end = start
	}
	return rows, start, end, clamped
}
