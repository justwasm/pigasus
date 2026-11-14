package herdr

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/vito/midterm"
)

// PaneStatus represents the current runtime state of a pane.
type PaneStatus int

const (
	StatusRunning PaneStatus = iota
	StatusExited
	StatusFailed
)

func (s PaneStatus) String() string {
	switch s {
	case StatusRunning:
		return "Running"
	case StatusExited:
		return "Exited"
	case StatusFailed:
		return "Failed"
	default:
		return "Unknown"
	}
}

// Pane represents a single virtual terminal pane / session.
type Pane struct {
	ID        string
	Title     string
	Dir       string
	CreatedAt time.Time

	mu       sync.Mutex
	term     *midterm.Terminal
	cmd      *exec.Cmd
	ptmx     *os.File
	pipeIn   io.WriteCloser
	status   PaneStatus
	exitCode int
	unread   bool
	isMock   bool

	agentName    string
	agentState   string // "working", "blocked", "idle"
	agentMessage string
	socketPath   string
	tabID        string
	workspaceID  string

	lastProcCheck time.Time
	cachedAgent   string

	onOutput func()
	onExit   func(code int)
}

// PaneOption configures a new Pane.
type PaneOption func(*Pane)

// WithOnOutput sets a notification callback called whenever new bytes are rendered.
func WithOnOutput(fn func()) PaneOption {
	return func(p *Pane) {
		p.onOutput = fn
	}
}

// WithOnExit sets a notification callback called when the process terminates.
func WithOnExit(fn func(int)) PaneOption {
	return func(p *Pane) {
		p.onExit = fn
	}
}

// WithMockSession forces a mock interactive session without allocating a real OS PTY.
func WithMockSession(welcomeMsg string) PaneOption {
	return func(p *Pane) {
		p.isMock = true
		if welcomeMsg != "" {
			p.term.Write([]byte(welcomeMsg))
		}
	}
}

// WithSocket configures Herdr UNIX socket and pane identity environment variables.
func WithSocket(socketPath, workspaceID, tabID string) PaneOption {
	return func(p *Pane) {
		p.socketPath = socketPath
		p.workspaceID = workspaceID
		p.tabID = tabID
	}
}

// NewPane creates and spawns a new terminal pane.
func NewPane(id, title, dir string, rows, cols int, opts ...PaneOption) (*Pane, error) {
	if rows < 4 {
		rows = 24
	}
	if cols < 10 {
		cols = 80
	}
	if dir == "" {
		dir, _ = os.Getwd()
	}

	term := midterm.NewTerminal(rows, cols)
	term.Raw = false

	p := &Pane{
		ID:        id,
		Title:     title,
		Dir:       dir,
		CreatedAt: time.Now(),
		term:      term,
		status:    StatusRunning,
	}

	for _, opt := range opts {
		opt(p)
	}

	if p.isMock {
		return p, nil
	}

	// Determine shell
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	if title == "" {
		p.Title = "sh"
		if idx := strings.LastIndex(shell, "/"); idx >= 0 {
			p.Title = shell[idx+1:]
		}
	}

	cmd := exec.Command(shell, "-l")
	cmd.Dir = dir
	cmd.Env = os.Environ()
	if p.socketPath != "" {
		cmd.Env = append(cmd.Env,
			"HERDR_ENV=1",
			"HERDR_SOCKET_PATH="+p.socketPath,
			"HERDR_PANE_ID="+p.ID,
			"HERDR_TAB_ID="+p.tabID,
			"HERDR_WORKSPACE_ID="+p.workspaceID,
			"HERDR_SESSION="+p.workspaceID,
		)
	}
	cmd.Env = append(cmd.Env, "TERM=xterm-256color")

	// Allocate PTY
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		// Fallback to mock session if PTY allocation fails (e.g., container / CI)
		p.isMock = true
		p.term.Write([]byte(fmt.Sprintf("\x1b[33m[herdr] Warning: PTY unavailable (%v). Running mock terminal session.\x1b[m\r\n\r\n$ ", err)))
		return p, nil
	}

	p.cmd = cmd
	p.ptmx = ptmx

	// Start asynchronous output reader
	go p.readLoop()

	// Start process exit monitor
	go p.waitLoop()

	return p, nil
}

func (p *Pane) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := p.ptmx.Read(buf)
		if n > 0 {
			p.mu.Lock()
			p.term.Write(buf[:n])
			p.unread = true
			cb := p.onOutput
			p.mu.Unlock()

			if cb != nil {
				cb()
			}
		}
		if err != nil {
			break
		}
	}
}

func (p *Pane) waitLoop() {
	if p.cmd == nil {
		return
	}
	err := p.cmd.Wait()
	p.mu.Lock()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			p.exitCode = exitErr.ExitCode()
			if p.exitCode != 0 {
				p.status = StatusFailed
			} else {
				p.status = StatusExited
			}
		} else {
			p.status = StatusFailed
			p.exitCode = 1
		}
	} else {
		p.status = StatusExited
		p.exitCode = 0
	}
	code := p.exitCode
	cb := p.onExit
	p.mu.Unlock()

	if cb != nil {
		cb(code)
	}
}

// Write sends raw input bytes to the pane's PTY or mock session.
func (p *Pane) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.isMock {
		// In mock mode, echo characters and handle basic newlines
		for _, char := range b {
			switch char {
			case '\r', '\n':
				p.term.Write([]byte("\r\n$ "))
			case 0x7f, 0x08: // Backspace
				p.term.Write([]byte("\b \b"))
			case 0x03: // Ctrl+C
				p.term.Write([]byte("^C\r\n$ "))
			case 0x04: // Ctrl+D
				p.term.Write([]byte("\r\n[Process completed]\r\n"))
				p.status = StatusExited
			default:
				p.term.Write([]byte{char})
			}
		}
		if p.onOutput != nil {
			p.onOutput()
		}
		return len(b), nil
	}

	if p.ptmx == nil || p.status != StatusRunning {
		return 0, io.EOF
	}
	return p.ptmx.Write(b)
}

// Resize updates the terminal emulator geometry and propagates the WINCH signal to the PTY.
func (p *Pane) Resize(rows, cols int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if rows < 1 {
		rows = 1
	}
	if cols < 1 {
		cols = 1
	}

	p.term.Resize(rows, cols)

	if p.ptmx != nil {
		_ = pty.Setsize(p.ptmx, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
	}
}

// Lines renders the active visible terminal rows formatted with ANSI escape sequences.
func (p *Pane) Lines() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	rows := p.term.Height
	lines := make([]string, 0, rows)
	for y := 0; y < rows; y++ {
		var b strings.Builder
		_ = p.term.RenderLine(&b, y)
		lines = append(lines, b.String())
	}
	return lines
}

// Cursor returns the current cursor coordinates.
func (p *Pane) Cursor() (x, y int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.term.Cursor.X, p.term.Cursor.Y
}

// Status returns the current execution state and exit code.
func (p *Pane) Status() (PaneStatus, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status, p.exitCode
}

// MarkRead clears the unread activity flag for this pane.
func (p *Pane) MarkRead() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.unread = false
}

// HasUnread reports whether new output arrived while the pane was inactive.
func (p *Pane) HasUnread() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.unread
}

// Close terminates the process and releases the PTY file descriptor.
func (p *Pane) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.ptmx != nil {
		_ = p.ptmx.Close()
	}
	if p.cmd != nil && p.cmd.Process != nil && p.status == StatusRunning {
		_ = p.cmd.Process.Signal(syscall.SIGKILL)
	}
	p.status = StatusExited
	return nil
}

// SetAgentState updates the agent name, state (e.g. working, blocked, idle), and detail message.
func (p *Pane) SetAgentState(name, state, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agentName = name
	p.agentState = state
	p.agentMessage = message
}

// ClearAgentState clears any agent authority on this pane.
func (p *Pane) ClearAgentState() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agentName = ""
	p.agentState = ""
	p.agentMessage = ""
}

// AgentState returns the current agent name, state, and message.
func (p *Pane) AgentState() (name, state, message string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.agentName, p.agentState, p.agentMessage
}

// SetTitle updates the pane title dynamically (e.g. via pane.report_metadata).
func (p *Pane) SetTitle(title string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Title = title
}

// SocketInfo returns the socket path, workspace ID, and tab ID configured for this pane.
func (p *Pane) SocketInfo() (socketPath, workspaceID, tabID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.socketPath, p.workspaceID, p.tabID
}

// NotifyPrompt appends a doorbell / notification prompt to the pane buffer.
func (p *Pane) NotifyPrompt(message string) {
	p.mu.Lock()
	p.unread = true
	notifyText := fmt.Sprintf("\a\r\n\x1b[33;1m[herdr notification] %s\x1b[m\r\n", message)
	p.term.Write([]byte(notifyText))
	cb := p.onOutput
	p.mu.Unlock()

	if cb != nil {
		cb()
	}
}

// PlainText returns the screen rows as plain text without ANSI escape sequences.
func (p *Pane) PlainText() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	rows := len(p.term.Content)
	res := make([]string, 0, rows)
	for y := 0; y < rows; y++ {
		res = append(res, string(p.term.Content[y]))
	}
	return res
}

// TermTitle returns the current dynamic window title reported by the terminal emulator.
func (p *Pane) TermTitle() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.term.Title
}

// ForegroundAgent inspects child processes of this pane's shell and returns the canonical agent if detected.
func (p *Pane) ForegroundAgent() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cmd == nil || p.cmd.Process == nil || p.status != StatusRunning {
		return "", false
	}

	pid := p.cmd.Process.Pid
	if pid <= 0 {
		return "", false
	}

	now := time.Now()
	if now.Sub(p.lastProcCheck) < 300*time.Millisecond && p.cachedAgent != "" {
		return p.cachedAgent, true
	}
	if now.Sub(p.lastProcCheck) < 300*time.Millisecond {
		return "", false
	}
	p.lastProcCheck = now

	// Query child processes of the shell PID using pgrep -fl -P <pid>
	out, err := exec.Command("pgrep", "-fl", "-P", fmt.Sprintf("%d", pid)).Output()
	if err != nil || len(out) == 0 {
		p.cachedAgent = ""
		return "", false
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "<pid> <command line>"
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		cmdline := parts[1]
		fields := strings.Fields(cmdline)
		for _, field := range fields {
			if strings.HasPrefix(field, "-") {
				continue
			}
			if agent, ok := LookupAgent(field); ok {
				p.cachedAgent = agent
				return agent, true
			}
		}
	}

	p.cachedAgent = ""
	return "", false
}

