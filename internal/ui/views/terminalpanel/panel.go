// Package terminalpanel implements an interactive virtual terminal panel
// powered by vito/midterm. It provides full screen emulation, scrollback
// navigation, copy mode, key encoding (arrows, Ctrl+A..Z, Alt, paste), and modal isolation.
package terminalpanel

import (
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
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

	// Title returns the title or ID of the active terminal session.
	Title func() string

	// Cwd returns the current working directory of the active terminal session.
	Cwd func() string

	// CopyText writes yanked text to the clipboard.
	CopyText func(string) error

	// Tabs returns the open terminal sessions for the multi-session tab bar.
	Tabs func() []TabInfo

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
	defer p.mu.Unlock()

	if p.term != nil && len(data) > 0 {
		_, _ = p.term.Write(data)
		p.resumeOffset += int64(len(data))
	}
}

// HandlePaste encodes pasted text with bracketed paste delimiters and writes it to the PTY.
func (p *Panel) HandlePaste(text string) {
	if p == nil || text == "" {
		return
	}
	p.mu.Lock()
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
