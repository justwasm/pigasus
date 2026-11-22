// Package terminalpanel implements an interactive virtual terminal panel
// powered by vito/midterm. It provides full screen emulation, scrollback
// navigation, key encoding (arrows, Ctrl+A..Z, Alt, paste), and modal isolation.
package terminalpanel

import (
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/vito/midterm"
)

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

	// Height returns the total available viewport height.
	Height func() int

	// Width returns the total available viewport width.
	Width func() int
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
	maxScrollbackLines int
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
	term.OnScrollback(func(line midterm.Line) {
		p.scrollbackMu.Lock()
		defer p.scrollbackMu.Unlock()
		p.scrollback = append(p.scrollback, line.Display())
		if len(p.scrollback) > p.maxScrollbackLines {
			p.scrollback = p.scrollback[len(p.scrollback)-p.maxScrollbackLines:]
		}
	})

	p.term = term
	return p
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
	defer p.mu.Unlock()

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
	if p.seams.Resize != nil {
		_ = p.seams.Resize(rows, cols)
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
