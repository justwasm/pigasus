// Package terminalpanel implements an interactive virtual terminal panel
// powered by vito/midterm. It provides full screen emulation, scrollback
// navigation, copy mode, key encoding (arrows, Ctrl+A..Z, Alt, paste), and modal isolation.
package terminalpanel

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/vito/midterm"
)

// TabInfo describes one terminal session tab in the panel header.
type TabInfo struct {
	ID     string
	Title  string
	Active bool
	Exited bool
}

// Seams connects the terminal panel to the backend PTY or session host.
type Seams struct {
	// WriteInput writes raw input bytes to the running PTY process.
	WriteInput func([]byte) error

	// Resize notifies the PTY process of a window dimension change.
	Resize func(rows, cols int) error

	// Close terminates the terminal process and closes the panel.
	Close func() tea.Cmd

	// Detach exits the modal panel without killing the underlying PTY session.
	Detach func() tea.Cmd

	// Quit terminates all terminal sessions and exits the standalone terminal app.
	Quit func() tea.Cmd

	// PassThroughEsc forwards Esc directly to the PTY when not in menu, copy, or scrollback mode.
	PassThroughEsc bool

	// Standalone indicates the panel owns the full terminal viewport (no outer status line).
	Standalone bool

	// OmniBar renders a borderless full-bleed terminal with a single top omni bar
	// combining tabs, status, and menu trigger (maximizing usable screen area).
	OmniBar bool

	// Title returns the title or ID of the active terminal session.
	Title func() string

	// Cwd returns the current working directory of the active terminal session.
	Cwd func() string

	// CopyText writes yanked text to the clipboard.
	CopyText func(string) error

	// Tabs returns the open terminal sessions for the multi-session tab bar.
	Tabs func() []TabInfo

	// NewTab creates and switches to a new terminal session.
	NewTab func() tea.Cmd

	// SwitchTab switches to the terminal session with the given ID.
	SwitchTab func(id string) tea.Cmd

	// CloseTab closes the terminal session with the given ID.
	CloseTab func(id string) tea.Cmd

	// Rename updates the title of the specified (or active) terminal session.
	Rename func(id, title string)

	// Height returns the total available viewport height.
	Height func() int

	// Width returns the total available viewport width.
	Width func() int
}

type copyVisualKind int

const (
	copyVisualNone copyVisualKind = iota
	copyVisualChar
	copyVisualLine
)

type hitActionKind int

const (
	hitActionNone hitActionKind = iota
	hitActionSwitchTab
	hitActionCloseTab
	hitActionNewTab
	hitActionOpenMenu
	hitActionMenuItem
)

type hitRect struct {
	row    int
	startX int
	endX   int
	kind   hitActionKind
	id     string
	index  int
}

type menuBoxRect struct {
	valid  bool
	top    int
	bottom int
	left   int
	right  int
}

// Panel represents an interactive virtual terminal view.
type Panel struct {
	seams Seams

	mu   sync.Mutex
	term *midterm.Terminal
	rows int
	cols int

	scrollbackMu sync.Mutex
	scrollback   []string

	scrollOffset       int
	isScrollbackMode   bool
	resumeOffset       int64
	exitCode           *int
	exitErr            error
	status             string
	title              string
	cwd                string
	maxScrollbackLines int

	copyMode      bool
	copyRow       int
	copyCol       int
	copyVisual    copyVisualKind
	copyAnchorRow int
	copyAnchorCol int

	// Terminal DEC private modes forwarded by midterm
	mouseClicks      bool
	mouseCellMotion  bool
	mouseAllMotion   bool
	mouseSGR         bool
	cursorKeysApp    bool
	bracketedPaste   bool
	modifyOtherKeys  int
	mousePressedBtn  tea.MouseButton
	pendingResponses [][]byte

	// Interactive terminal menu & mouse hit-testing state
	menuOpen      bool
	menuCursor    int
	menuRenaming  bool
	menuRenameBuf string
	menuBox       menuBoxRect
	hits          []hitRect
}

// New creates a new interactive terminal panel.
func New(rows, cols int, seams Seams) *Panel {
	if rows < 4 {
		rows = 24
	}
	if cols < 10 {
		cols = 80
	}
	p := &Panel{
		seams:              seams,
		rows:               rows,
		cols:               cols,
		status:             "Running",
		maxScrollbackLines: 2000,
	}

	term := midterm.NewTerminal(rows, cols)
	term.Raw = false
	term.CursorVisible = true
	term.ForwardRequests = modeTrackerWriter{panel: p}
	term.ForwardResponses = responseBufferWriter{panel: p}
	term.OnScrollback(func(line midterm.Line) {
		styled := renderStyledLine(line)
		p.scrollbackMu.Lock()
		defer p.scrollbackMu.Unlock()
		p.scrollback = append(p.scrollback, styled)
		if len(p.scrollback) > p.maxScrollbackLines {
			p.scrollback = p.scrollback[len(p.scrollback)-p.maxScrollbackLines:]
		}
	})

	p.term = term
	return p
}

type modeTrackerWriter struct {
	panel *Panel
}

func (w modeTrackerWriter) Write(data []byte) (int, error) {
	if w.panel != nil {
		w.panel.applyForwardedModeLocked(data)
	}
	return len(data), nil
}

type responseBufferWriter struct {
	panel *Panel
}

func (w responseBufferWriter) Write(data []byte) (int, error) {
	if w.panel != nil && len(data) > 0 {
		cp := make([]byte, len(data))
		copy(cp, data)
		w.panel.pendingResponses = append(w.panel.pendingResponses, cp)
	}
	return len(data), nil
}

// applyForwardedModeLocked parses CSI mode sequences emitted by midterm.ForwardRequests
// (e.g. "\x1b[1000h", "\x1b[?1000l") while p.mu is held.
func (p *Panel) applyForwardedModeLocked(data []byte) {
	// midterm emits "\x1b[%dh" on SetMode and "\x1b[?%dl" on UnsetMode.
	for len(data) > 0 {
		idx := bytes.Index(data, []byte("\x1b["))
		if idx < 0 {
			return
		}
		data = data[idx+2:]
		hasQuestion := false
		if len(data) > 0 && data[0] == '?' {
			hasQuestion = true
			data = data[1:]
		}
		end := 0
		for end < len(data) && data[end] >= '0' && data[end] <= '9' {
			end++
		}
		if end == 0 || end >= len(data) {
			return
		}
		modeNum, err := strconv.Atoi(string(data[:end]))
		op := data[end]
		data = data[end+1:]
		if err != nil {
			continue
		}
		if !hasQuestion && op == 'm' {
			p.modifyOtherKeys = modeNum
			continue
		}
		if op != 'h' && op != 'l' {
			continue
		}
		enabled := op == 'h'
		switch modeNum {
		case 1:
			p.cursorKeysApp = enabled
		case 1000:
			p.mouseClicks = enabled
		case 1002:
			p.mouseCellMotion = enabled
		case 1003:
			p.mouseAllMotion = enabled
		case 1006:
			p.mouseSGR = enabled
		case 2004:
			p.bracketedPaste = enabled
		}
	}
}

// renderStyledLine converts a midterm.Line into an ANSI SGR-preserving string
// without leaking attributes past the end of the line.
func renderStyledLine(line midterm.Line) string {
	end := len(line.Content)
	for end > 0 {
		r := line.Content[end-1]
		var f midterm.Format
		if end-1 < len(line.Format) {
			f = line.Format[end-1]
		}
		if r == ' ' && f == (midterm.Format{}) {
			end--
			continue
		}
		break
	}
	if end == 0 {
		return ""
	}

	var b strings.Builder
	var lastFormat midterm.Format
	for col := 0; col < end; col++ {
		var f midterm.Format
		if col < len(line.Format) {
			f = line.Format[col]
		}
		if f != lastFormat {
			if formatLeaks(lastFormat, f) {
				b.WriteString("\x1b[0m")
			}
			b.WriteString(f.Render())
			lastFormat = f
		}
		b.WriteRune(line.Content[col])
	}
	if lastFormat != (midterm.Format{}) && !lastFormat.IsReset() {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

func formatLeaks(prev, f midterm.Format) bool {
	if f.IsReset() || f == (midterm.Format{}) {
		return false
	}
	const attrs = midterm.BoldBit | midterm.FaintBit | midterm.ItalicBit | midterm.UnderlineBit | midterm.BlinkBit | midterm.ReverseBit | midterm.ConcealBit
	if (prev.Properties&attrs)&^f.Properties != 0 {
		return true
	}
	if prev.Fg != nil && f.Fg == nil {
		return true
	}
	if prev.Bg != nil && f.Bg == nil {
		return true
	}
	return false
}

// WriteOutput receives raw byte chunks from the terminal output stream and
// passes them to the virtual terminal emulator.
func (p *Panel) WriteOutput(data []byte) {
	p.mu.Lock()
	if p.term != nil && len(data) > 0 {
		_, _ = p.term.Write(data)
		p.resumeOffset += int64(len(data))
	}
	var responses [][]byte
	if len(p.pendingResponses) > 0 {
		responses = p.pendingResponses
		p.pendingResponses = nil
	}
	writeFn := p.seams.WriteInput
	p.mu.Unlock()

	if writeFn != nil {
		for _, resp := range responses {
			_ = writeFn(resp)
		}
	}
}

// HandlePaste encodes pasted text with bracketed paste delimiters and writes it to the PTY.
func (p *Panel) HandlePaste(text string) {
	if p == nil || text == "" {
		return
	}
	p.mu.Lock()
	if p.menuOpen && p.menuRenaming {
		clean := strings.ReplaceAll(strings.ReplaceAll(text, "\r", ""), "\n", " ")
		p.menuRenameBuf += clean
		p.mu.Unlock()
		return
	}
	if p.copyMode {
		p.copyMode = false
		p.copyVisual = copyVisualNone
	}
	if p.isScrollbackMode {
		p.isScrollbackMode = false
		p.scrollOffset = 0
	}
	writeFn := p.seams.WriteInput
	p.mu.Unlock()

	if writeFn != nil {
		_ = writeFn(EncodePaste(text))
	}
}

// SetTitle updates the terminal session title.
func (p *Panel) SetTitle(title string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.title = title
}

// Title returns the terminal session title.
func (p *Panel) Title() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.title != "" {
		return p.title
	}
	if p.seams.Title != nil {
		return p.seams.Title()
	}
	return ""
}

// SetCwd updates the terminal session working directory.
func (p *Panel) SetCwd(cwd string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cwd = cwd
}

// Cwd returns the terminal session working directory.
func (p *Panel) Cwd() string {
	if p == nil {
		return ""
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cwd != "" {
		return p.cwd
	}
	if p.seams.Cwd != nil {
		return p.seams.Cwd()
	}
	return ""
}

// InCopyMode reports whether keyboard copy mode is currently active.
func (p *Panel) InCopyMode() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.copyMode
}

// ResumeOffset returns the acknowledged stream byte offset for resumption.
func (p *Panel) ResumeOffset() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resumeOffset
}

// SetResumeOffset sets the acknowledged stream byte offset.
func (p *Panel) SetResumeOffset(offset int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.resumeOffset = offset
}

// SetExitStatus marks the terminal process as completed.
func (p *Panel) SetExitStatus(code int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.exitCode = &code
	p.exitErr = err
	if err != nil {
		p.status = fmt.Sprintf("Error: %v", err)
	} else if code == 0 {
		p.status = "Exited (0)"
	} else {
		p.status = fmt.Sprintf("Exited (%d)", code)
	}
}

// ExitStatus returns whether the process exited, its code, and any error.
func (p *Panel) ExitStatus() (exited bool, code int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.exitCode != nil {
		return true, *p.exitCode, p.exitErr
	}
	return false, 0, nil
}

// Status returns the current status string.
func (p *Panel) Status() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status
}

// ScrollOffset returns the current scrollback offset (0 = bottom / live view).
func (p *Panel) ScrollOffset() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scrollOffset
}

// ScrollbackLines returns a copy of accumulated scrollback history.
func (p *Panel) ScrollbackLines() []string {
	return p.copyScrollback()
}

func (p *Panel) scrollbackCount() int {
	p.scrollbackMu.Lock()
	defer p.scrollbackMu.Unlock()
	return len(p.scrollback)
}

func (p *Panel) copyScrollback() []string {
	p.scrollbackMu.Lock()
	defer p.scrollbackMu.Unlock()
	out := make([]string, len(p.scrollback))
	copy(out, p.scrollback)
	return out
}

// Resize updates the terminal dimensions and notifies the backend PTY.
func (p *Panel) Resize(rows, cols int) {
	p.mu.Lock()
	if rows < 4 {
		rows = 4
	}
	if cols < 10 {
		cols = 10
	}
	p.rows = rows
	p.cols = cols
	if p.term != nil {
		p.term.Resize(rows, cols)
	}
	resizeFn := p.seams.Resize
	p.mu.Unlock()

	if resizeFn != nil {
		_ = resizeFn(rows, cols)
	}
}

// RowCount reports the number of rendered screen lines (height).
func (p *Panel) RowCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term != nil {
		return p.term.Height
	}
	return p.rows
}

// Cursor returns the virtual terminal cursor coordinates (0-indexed).
func (p *Panel) Cursor() (x, y int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term != nil {
		return p.term.Cursor.X, p.term.Cursor.Y
	}
	return 0, 0
}

// CursorKeysApp reports whether DECCKM application cursor keys mode is active.
func (p *Panel) CursorKeysApp() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.cursorKeysApp
}

// ScreenLines returns formatted text for each active row in the terminal.
func (p *Panel) ScreenLines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.term == nil {
		return nil
	}

	rows := p.term.Height
	lines := make([]string, 0, rows)
	for y := 0; y < rows; y++ {
		var b strings.Builder
		_ = p.term.RenderLine(&b, y)
		lines = append(lines, b.String())
	}
	return lines
}

// ScreenLinesPlain returns plain-text (ANSI-stripped, right-trimmed) lines for each active row.
func (p *Panel) ScreenLinesPlain() []string {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.term == nil {
		return nil
	}
	rows := p.term.Height
	lines := make([]string, rows)
	lastNonEmpty := -1
	for y := 0; y < rows; y++ {
		var b strings.Builder
		_ = p.term.RenderLine(&b, y)
		line := strings.TrimRight(ansi.Strip(b.String()), " ")
		lines[y] = line
		if line != "" {
			lastNonEmpty = y
		}
	}
	if lastNonEmpty < 0 {
		return nil
	}
	return lines[:lastNonEmpty+1]
}

// Snapshot captures the live screen, optional scrollback, cursor, and metadata atomically.
type Snapshot struct {
	Rows            int
	Cols            int
	CursorX         int
	CursorY         int
	Status          string
	Title           string
	Cwd             string
	Exited          bool
	ExitCode        int
	ScrollbackLines int
	Screen          []string
	Scrollback      []string
}

// CaptureSnapshot returns a consistent snapshot of the panel's virtual terminal.
func (p *Panel) CaptureSnapshot(scrollbackCount int, raw bool) Snapshot {
	if p == nil {
		return Snapshot{}
	}
	p.mu.Lock()
	var snap Snapshot
	snap.Rows = p.rows
	snap.Cols = p.cols
	snap.Status = p.status
	snap.Title = p.title
	if snap.Title == "" && p.seams.Title != nil {
		snap.Title = p.seams.Title()
	}
	snap.Cwd = p.cwd
	if snap.Cwd == "" && p.seams.Cwd != nil {
		snap.Cwd = p.seams.Cwd()
	}
	if p.exitCode != nil {
		snap.Exited = true
		snap.ExitCode = *p.exitCode
	}
	if p.term != nil {
		snap.Rows = p.term.Height
		snap.Cols = p.term.Width
		snap.CursorX = p.term.Cursor.X
		snap.CursorY = p.term.Cursor.Y
		lines := make([]string, snap.Rows)
		lastNonEmpty := -1
		for y := 0; y < snap.Rows; y++ {
			var b strings.Builder
			_ = p.term.RenderLine(&b, y)
			line := b.String()
			if !raw {
				line = ansi.Strip(line)
			}
			line = strings.TrimRight(line, " ")
			lines[y] = line
			if line != "" {
				lastNonEmpty = y
			}
		}
		if lastNonEmpty >= 0 {
			snap.Screen = lines[:lastNonEmpty+1]
		}
	}
	p.mu.Unlock()

	sb := p.copyScrollback()
	snap.ScrollbackLines = len(sb)
	if scrollbackCount > 0 && len(sb) > 0 {
		start := len(sb) - scrollbackCount
		if start < 0 {
			start = 0
		}
		out := make([]string, 0, len(sb)-start)
		for _, line := range sb[start:] {
			if !raw {
				line = ansi.Strip(line)
			}
			out = append(out, strings.TrimRight(line, " "))
		}
		snap.Scrollback = out
	}
	return snap
}

// Lines returns the full rendered frame as lines for panel display.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	rendered := p.Render()
	return strings.Split(rendered, "\n")
}

// ViewerSpec returns false as the terminal panel owns its own rendering and scrollback.
func (p *Panel) ViewerSpec() (tuiviewer.Spec, bool) {
	return tuiviewer.Spec{}, false
}

// ExtractSelectionText extracts selected text from rendered panel lines,
// stripping the outer box borders ("│ " and " │") and header/footer rows.
func ExtractSelectionText(lines []string, sr, sc, er, ec int, hasTabs bool) string {
	return ExtractSelectionTextWithOptions(lines, sr, sc, er, ec, hasTabs, false)
}

// ExtractSelectionTextWithOptions extracts selected text from rendered panel lines,
// supporting both boxed mode and borderless OmniBar mode.
func ExtractSelectionTextWithOptions(lines []string, sr, sc, er, ec int, hasTabs, omniBar bool) string {
	if sr < 0 || sr >= len(lines) || er < 0 || er >= len(lines) {
		return ""
	}
	firstBody := 1
	if !omniBar && hasTabs {
		firstBody = 2
	}
	lastBody := len(lines) - 2
	if omniBar {
		lastBody = len(lines) - 1
	}
	var out []string
	for r := sr; r <= er; r++ {
		if r < firstBody || r > lastBody {
			continue
		}
		plain := ansi.Strip(lines[r])
		rw := ansi.StringWidth(plain)
		innerStart := 2
		innerEnd := rw - 2
		if omniBar {
			innerStart = 0
			innerEnd = rw
		}
		if innerEnd < innerStart {
			continue
		}
		from := innerStart
		to := innerEnd
		if r == sr && sc > from {
			from = sc
		}
		if r == er && ec+1 < to {
			to = ec + 1
		}
		if from > innerEnd {
			from = innerEnd
		}
		if to < innerStart {
			to = innerStart
		}
		if from > to {
			from = to
		}
		seg := ansi.Cut(plain, from, to)
		out = append(out, strings.TrimRight(seg, " "))
	}
	return strings.Join(out, "\n")
}
