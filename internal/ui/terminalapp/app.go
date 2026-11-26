// Package terminalapp implements the standalone `pigasus terminal` subcommand,
// extracting the interactive multi-tab virtual terminal panel into a tmux-like
// terminal multiplexer where Esc passes directly through to the subprocess.
package terminalapp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/infra/terminalmgr"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/views/terminalpanel"
	"github.com/justwasm/pigasus/rcterminal"
)

// Options configures a standalone `pigasus terminal` session.
type Options struct {
	Cwd          string
	Command      string
	Title        string
	SocketPath   string
	RemainOnExit bool
	MCPMode      bool
}

// SessionInfo describes a created terminal session.
type SessionInfo struct {
	TerminalID string
	Title      string
	Cwd        string
	PID        int
}

// StreamEvent carries output, metadata, or exit events from a terminal backend.
type StreamEvent struct {
	Output   []byte
	ExitCode *int
	Offset   int64
	Title    string
	Cwd      string
}

// Backend abstracts PTY lifecycle and I/O for standalone terminal sessions.
type Backend interface {
	Create(cwd string) (SessionInfo, error)
	Write(id string, data []byte) error
	Resize(id string, rows, cols int) error
	Stream(ctx context.Context, id string, offset int64) (<-chan StreamEvent, error)
	Close(id string) error
}

// LocalBackend implements Backend using rcterminal.Registry.
type LocalBackend struct {
	reg      *rcterminal.Registry
	extraEnv []string
}

// NewLocalBackend creates a Backend backed by local pseudo-terminals.
func NewLocalBackend(workspace string) *LocalBackend {
	reg := rcterminal.NewRegistry()
	if workspace != "" {
		reg.SetWorkspace(workspace)
	}
	return &LocalBackend{reg: reg}
}

// SetExtraEnv configures additional KEY=VALUE environment variables passed to spawned shells.
func (b *LocalBackend) SetExtraEnv(env []string) {
	if b != nil {
		b.extraEnv = append([]string(nil), env...)
	}
}

// Create spawns a local login shell in cwd.
func (b *LocalBackend) Create(cwd string) (SessionInfo, error) {
	req := rcterminal.CreateRequest{
		Workspace: cwd,
		Env:       b.extraEnv,
	}
	if req.Workspace == "" {
		req.Workspace = b.reg.Workspace()
	}
	info, err := b.reg.Create(req)
	if err != nil {
		return SessionInfo{}, err
	}
	return SessionInfo{
		TerminalID: info.TerminalID,
		Title:      info.Title,
		Cwd:        info.Cwd,
		PID:        info.PID,
	}, nil
}

// Write sends raw input bytes to the PTY.
func (b *LocalBackend) Write(id string, data []byte) error {
	return b.reg.Write(id, data)
}

// Resize updates the PTY window dimensions.
func (b *LocalBackend) Resize(id string, rows, cols int) error {
	return b.reg.Resize(id, cols, rows)
}

// Stream streams output, metadata, and exit events from the PTY starting at offset.
func (b *LocalBackend) Stream(ctx context.Context, id string, offset int64) (<-chan StreamEvent, error) {
	frames, err := b.reg.StreamChannelFrom(ctx, id, offset)
	if err != nil {
		return nil, err
	}
	out := make(chan StreamEvent, 64)
	go func() {
		defer close(out)
		for f := range frames {
			var ev StreamEvent
			switch f.Kind {
			case rcterminal.FrameOutput:
				ev = StreamEvent{Output: f.Output, Offset: f.Offset}
			case rcterminal.FrameTitle:
				ev = StreamEvent{Title: f.Title}
			case rcterminal.FrameCwd:
				ev = StreamEvent{Cwd: f.Cwd}
			case rcterminal.FrameExitCode:
				code := f.ExitCode
				ev = StreamEvent{ExitCode: &code}
			default:
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

// Close terminates the PTY session.
func (b *LocalBackend) Close(id string) error {
	return b.reg.Close(id, true)
}

// ParseArgs parses CLI arguments for `pigasus terminal`.
func ParseArgs(args []string) (Options, bool, error) {
	var opts Options
	var positional []string
	afterDash := false

	if len(args) > 0 && args[0] == "mcp" {
		opts.MCPMode = true
		args = args[1:]
	}

	for i := 0; i < len(args); i++ {
		a := args[i]
		if afterDash {
			positional = append(positional, a)
			continue
		}
		switch {
		case a == "--":
			afterDash = true
		case a == "-h" || a == "--help":
			return opts, true, nil
		case a == "--remain-on-exit":
			opts.RemainOnExit = true
		case a == "--socket":
			if i+1 >= len(args) {
				return opts, false, fmt.Errorf("flag %s requires a socket path", a)
			}
			i++
			opts.SocketPath = args[i]
		case strings.HasPrefix(a, "--socket="):
			opts.SocketPath = strings.TrimPrefix(a, "--socket=")
		case a == "-C" || a == "--cwd" || a == "--dir":
			if i+1 >= len(args) {
				return opts, false, fmt.Errorf("flag %s requires a directory argument", a)
			}
			i++
			opts.Cwd = args[i]
		case strings.HasPrefix(a, "--cwd="):
			opts.Cwd = strings.TrimPrefix(a, "--cwd=")
		case strings.HasPrefix(a, "--dir="):
			opts.Cwd = strings.TrimPrefix(a, "--dir=")
		case strings.HasPrefix(a, "-C="):
			opts.Cwd = strings.TrimPrefix(a, "-C=")
		case a == "-c" || a == "--command":
			if i+1 >= len(args) {
				return opts, false, fmt.Errorf("flag %s requires a command argument", a)
			}
			i++
			opts.Command = args[i]
		case strings.HasPrefix(a, "--command="):
			opts.Command = strings.TrimPrefix(a, "--command=")
		case strings.HasPrefix(a, "-c="):
			opts.Command = strings.TrimPrefix(a, "-c=")
		case a == "-t" || a == "--title":
			if i+1 >= len(args) {
				return opts, false, fmt.Errorf("flag %s requires a title argument", a)
			}
			i++
			opts.Title = args[i]
		case strings.HasPrefix(a, "--title="):
			opts.Title = strings.TrimPrefix(a, "--title=")
		case strings.HasPrefix(a, "-t="):
			opts.Title = strings.TrimPrefix(a, "-t=")
		case strings.HasPrefix(a, "-"):
			return opts, false, fmt.Errorf("unknown flag: %s", a)
		default:
			positional = append(positional, a)
		}
	}

	if opts.Command == "" && len(positional) > 0 {
		opts.Command = strings.Join(positional, " ")
	}
	return opts, false, nil
}

// Run launches the standalone interactive terminal multiplexer (or MCP stdio server).
func Run(w io.Writer, args []string) error {
	opts, showHelp, err := ParseArgs(args)
	if err != nil {
		return err
	}
	if showHelp {
		if w == nil {
			w = os.Stdout
		}
		_, _ = io.WriteString(w, SubcommandSpec().HelpFn("pigasus"))
		return nil
	}

	if opts.MCPMode {
		sockPath := strings.TrimSpace(opts.SocketPath)
		if sockPath == "" {
			sockPath = terminalmgr.DiscoverSocket()
		}
		if sockPath == "" {
			return errors.New("no running pigasus terminal socket found (pass --socket or run inside pigasus terminal)")
		}
		client := terminalmgr.NewIPCClient(sockPath)
		pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := client.Ping(pingCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("connect to pigasus terminal socket %s: %w", sockPath, err)
		}
		if w == nil {
			w = os.Stdout
		}
		return terminalmgr.ServeMCP(context.Background(), os.Stdin, w, client, terminalmgr.SelfSessionIDFromEnv())
	}

	cwd := strings.TrimSpace(opts.Cwd)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	opts.Cwd = cwd

	sockPath := strings.TrimSpace(opts.SocketPath)
	if sockPath == "" {
		sockPath = terminalmgr.DefaultSocketPath(os.Getpid())
	}

	backend := NewLocalBackend(cwd)
	backend.SetExtraEnv([]string{terminalmgr.EnvSocket + "=" + sockPath})
	m := NewModel(backend, opts)
	if _, err := m.CreateInitialSession(); err != nil {
		return err
	}
	defer m.CloseAll()

	if ipcSrv, err := terminalmgr.StartIPCServer(sockPath, m.ControlBackend()); err == nil {
		defer ipcSrv.Close()
	}

	prog := tea.NewProgram(m, tea.WithColorProfile(termcolor.Resolve(os.Environ())))
	m.SetProgram(prog)

	_, err = prog.Run()
	return err
}

type termSession struct {
	id          string
	name        string
	title       string
	customTitle bool
	cwd         string
	pid         int
	panel       *terminalpanel.Panel
	cancel      context.CancelFunc
	streaming   bool
	exited      bool
	exitCode    int
}

type termOutputMsg struct {
	TerminalID string
	Data       []byte
}

type termMetaMsg struct {
	TerminalID string
	Title      string
	Cwd        string
}

type termExitMsg struct {
	TerminalID string
	Code       int
	Err        error
}

type termStreamEndedMsg struct {
	TerminalID string
}

type termControlMsg struct {
	fn   func() tea.Cmd
	done chan struct{}
}

// Model is the Bubbletea model for the standalone `pigasus terminal` multiplexer.
type Model struct {
	backend      Backend
	cwd          string
	pendingCmd   string
	initialTitle string
	remainOnExit bool

	width    int
	height   int
	sized    bool
	quitting bool

	sessions map[string]*termSession
	order    []string
	activeID string

	sel tuiviewport.TextSelection

	mu      sync.Mutex
	ctrlMu  sync.Mutex
	prog    *tea.Program
	sendMsg func(tea.Msg)
}

// NewModel constructs a standalone terminal multiplexer model.
func NewModel(backend Backend, opts Options) *Model {
	cwd := strings.TrimSpace(opts.Cwd)
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	return &Model{
		backend:      backend,
		cwd:          cwd,
		pendingCmd:   strings.TrimSpace(opts.Command),
		initialTitle: strings.TrimSpace(opts.Title),
		remainOnExit: opts.RemainOnExit,
		width:        80,
		height:       24,
		sessions:     make(map[string]*termSession),
	}
}

// SetProgram attaches the running Bubbletea program for async stream delivery.
func (m *Model) SetProgram(prog *tea.Program) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prog = prog
}

func (m *Model) emit(msg tea.Msg) {
	m.mu.Lock()
	sendFn := m.sendMsg
	prog := m.prog
	m.mu.Unlock()
	if sendFn != nil {
		sendFn(msg)
		return
	}
	if prog != nil {
		prog.Send(msg)
	}
}

// CreateInitialSession creates the first terminal session before entering the TUI loop.
func (m *Model) CreateInitialSession() (SessionInfo, error) {
	info, err := m.createSession(m.initialTitle)
	if err != nil {
		return SessionInfo{}, err
	}
	return info, nil
}

// Init starts streaming output for any pre-created session (or creates one if none exists).
func (m *Model) Init() tea.Cmd {
	if m.activeID == "" && len(m.order) == 0 {
		info, err := m.createSession(m.initialTitle)
		if err != nil {
			return tea.Quit
		}
		return m.startStream(info.TerminalID)
	}
	if m.activeID != "" {
		return m.startStream(m.activeID)
	}
	return nil
}

func (m *Model) innerDimensions() (rows, cols int) {
	h := m.height
	if h < 6 {
		h = 24
	}
	w := m.width
	if w < 20 {
		w = 80
	}
	// Borderless Omni Bar occupies 1 top row; the remaining rows and full width
	// belong to the terminal viewport.
	rows = h - 1
	if rows < 4 {
		rows = 4
	}
	cols = w
	if cols < 10 {
		cols = 10
	}
	return rows, cols
}

func (m *Model) resizeAllPanels() {
	rows, cols := m.innerDimensions()
	for _, id := range m.order {
		if sess := m.sessions[id]; sess != nil && sess.panel != nil {
			sess.panel.Resize(rows, cols)
		}
	}
}

func (m *Model) activeSession() *termSession {
	if m.activeID == "" {
		return nil
	}
	return m.sessions[m.activeID]
}

func (m *Model) activePanel() *terminalpanel.Panel {
	if sess := m.activeSession(); sess != nil {
		return sess.panel
	}
	return nil
}

func (m *Model) tabs() []terminalpanel.TabInfo {
	if len(m.order) == 0 {
		return nil
	}
	out := make([]terminalpanel.TabInfo, 0, len(m.order))
	for _, id := range m.order {
		sess := m.sessions[id]
		if sess == nil {
			continue
		}
		title := sess.title
		if title == "" {
			title = id
		}
		out = append(out, terminalpanel.TabInfo{
			ID:     id,
			Title:  title,
			Active: id == m.activeID,
			Exited: sess.exited,
		})
	}
	return out
}

func (m *Model) newPanelForSession(sessionID string) *terminalpanel.Panel {
	rows, cols := m.innerDimensions()
	return terminalpanel.New(rows, cols, terminalpanel.Seams{
		PassThroughEsc: true,
		Standalone:     true,
		OmniBar:        true,
		WriteInput: func(data []byte) error {
			if m.backend == nil || sessionID == "" {
				return nil
			}
			return m.backend.Write(sessionID, data)
		},
		Resize: func(r, c int) error {
			if m.backend == nil || sessionID == "" {
				return nil
			}
			return m.backend.Resize(sessionID, r, c)
		},
		Close: func() tea.Cmd {
			return m.closeSession(sessionID)
		},
		Quit: func() tea.Cmd {
			m.quitting = true
			m.CloseAll()
			return tea.Quit
		},
		Title: func() string {
			if sess := m.sessions[sessionID]; sess != nil && sess.title != "" {
				return sess.title
			}
			return "Terminal"
		},
		Cwd: func() string {
			if sess := m.sessions[sessionID]; sess != nil && sess.cwd != "" {
				return sess.cwd
			}
			return m.cwd
		},
		CopyText: clipboard.Write,
		Tabs:     m.tabs,
		NewTab: func() tea.Cmd {
			info, err := m.createSession("")
			if err != nil {
				return nil
			}
			return m.startStream(info.TerminalID)
		},
		SwitchTab: func(id string) tea.Cmd {
			return m.switchSession(id)
		},
		CloseTab: func(id string) tea.Cmd {
			return m.closeSession(id)
		},
		Rename: func(id, title string) {
			if id == "" {
				id = m.activeID
			}
			if sess := m.sessions[id]; sess != nil {
				sess.title = title
				sess.customTitle = true
				if sess.panel != nil {
					sess.panel.SetTitle(title)
				}
			}
		},
		Height: func() int { return m.height },
		Width:  func() int { return m.width },
	})
}

func (m *Model) createSession(customTitle string) (SessionInfo, error) {
	return m.createSessionWithOptions("", customTitle, "", true)
}

func (m *Model) createSessionWithOptions(name, customTitle, customCwd string, focus bool) (SessionInfo, error) {
	if m.backend == nil {
		return SessionInfo{}, errors.New("terminal backend is not initialized")
	}
	if name != "" {
		for _, existing := range m.sessions {
			if existing != nil && existing.name == name {
				return SessionInfo{}, fmt.Errorf("terminal session named %q already exists", name)
			}
		}
	}
	cwd := strings.TrimSpace(customCwd)
	if cwd == "" {
		cwd = m.cwd
		if active := m.activeSession(); active != nil && active.cwd != "" {
			cwd = active.cwd
		}
	}
	info, err := m.backend.Create(cwd)
	if err != nil {
		return SessionInfo{}, err
	}
	title := info.Title
	hasCustom := false
	if customTitle != "" {
		title = customTitle
		hasCustom = true
	} else if name != "" {
		title = name
		hasCustom = true
	}
	sessCwd := info.Cwd
	if sessCwd == "" {
		sessCwd = cwd
	}
	panel := m.newPanelForSession(info.TerminalID)
	if title != "" {
		panel.SetTitle(title)
	}
	if sessCwd != "" {
		panel.SetCwd(sessCwd)
	}
	sess := &termSession{
		id:          info.TerminalID,
		name:        name,
		title:       title,
		customTitle: hasCustom,
		cwd:         sessCwd,
		pid:         info.PID,
		panel:       panel,
	}
	m.sessions[info.TerminalID] = sess
	m.order = append(m.order, info.TerminalID)
	if focus || m.activeID == "" {
		m.activeID = info.TerminalID
		m.sel.ResetSelection()
	}
	m.resizeAllPanels()
	return info, nil
}

func (m *Model) switchSession(id string) tea.Cmd {
	if id == "" || m.sessions[id] == nil {
		return nil
	}
	m.activeID = id
	m.sel.ResetSelection()
	m.resizeAllPanels()
	return m.startStream(id)
}

func (m *Model) cycleSession(delta int) tea.Cmd {
	n := len(m.order)
	if n <= 1 {
		return nil
	}
	idx := 0
	for i, id := range m.order {
		if id == m.activeID {
			idx = i
			break
		}
	}
	next := (idx + delta) % n
	if next < 0 {
		next += n
	}
	return m.switchSession(m.order[next])
}

func (m *Model) closeSession(id string) tea.Cmd {
	if id == "" {
		return nil
	}
	if sess := m.sessions[id]; sess != nil {
		if sess.cancel != nil {
			sess.cancel()
			sess.cancel = nil
		}
		sess.streaming = false
	}
	if m.backend != nil {
		_ = m.backend.Close(id)
	}
	delete(m.sessions, id)
	filtered := m.order[:0]
	for _, existing := range m.order {
		if existing != id {
			filtered = append(filtered, existing)
		}
	}
	m.order = filtered
	m.sel.ResetSelection()

	if len(m.order) == 0 {
		m.activeID = ""
		m.quitting = true
		return tea.Quit
	}
	if m.activeID == id || m.sessions[m.activeID] == nil {
		m.activeID = m.order[0]
	}
	m.resizeAllPanels()
	return m.startStream(m.activeID)
}

// CloseAll cancels all active streams and closes all PTY sessions.
func (m *Model) CloseAll() {
	if m == nil {
		return
	}
	for id, sess := range m.sessions {
		if sess != nil && sess.cancel != nil {
			sess.cancel()
			sess.cancel = nil
			sess.streaming = false
		}
		if m.backend != nil {
			_ = m.backend.Close(id)
		}
	}
	m.sessions = make(map[string]*termSession)
	m.order = nil
	m.activeID = ""
}

func (m *Model) startStream(id string) tea.Cmd {
	if id == "" || m.backend == nil {
		return nil
	}
	sess := m.sessions[id]
	if sess == nil || sess.streaming || sess.exited {
		return nil
	}
	var resumeOffset int64
	if sess.panel != nil {
		resumeOffset = sess.panel.ResumeOffset()
	}
	ctx, cancel := context.WithCancel(context.Background())
	sess.cancel = cancel
	sess.streaming = true

	return func() tea.Msg {
		events, err := m.backend.Stream(ctx, id, resumeOffset)
		if err != nil {
			cancel()
			m.emit(termStreamEndedMsg{TerminalID: id})
			return nil
		}
		go func() {
			defer cancel()
			for ev := range events {
				if ev.Title != "" || ev.Cwd != "" {
					m.emit(termMetaMsg{
						TerminalID: id,
						Title:      ev.Title,
						Cwd:        ev.Cwd,
					})
				}
				if len(ev.Output) > 0 {
					m.emit(termOutputMsg{
						TerminalID: id,
						Data:       ev.Output,
					})
				}
				if ev.ExitCode != nil {
					m.emit(termExitMsg{
						TerminalID: id,
						Code:       *ev.ExitCode,
					})
					return
				}
			}
			m.emit(termStreamEndedMsg{TerminalID: id})
		}()
		return nil
	}
}

// Update handles Bubbletea messages for the standalone terminal multiplexer.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.sized = true
		m.resizeAllPanels()
		if m.pendingCmd != "" && m.activeID != "" && m.backend != nil {
			cmdStr := m.pendingCmd
			m.pendingCmd = ""
			_ = m.backend.Write(m.activeID, []byte(cmdStr+"\r"))
		}
		return m, nil

	case tea.KeyMsg:
		key := msg.Key()
		if translated, ok := keybind.TranslateMacOptionToAlt(key); ok {
			key = translated
		}
		return m.handleKey(key)

	case tea.PasteMsg:
		if p := m.activePanel(); p != nil {
			p.HandlePaste(msg.Content)
		}
		return m, nil

	case tea.MouseClickMsg:
		return m.handleMouseClick(msg.Mouse())

	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg.Mouse())

	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg.Mouse())

	case tea.MouseMsg:
		if p := m.activePanel(); p != nil {
			p.HandleWheel(msg.Mouse())
		}
		return m, nil

	case termMetaMsg:
		if sess := m.sessions[msg.TerminalID]; sess != nil {
			if msg.Title != "" && !sess.customTitle {
				sess.title = msg.Title
				if sess.panel != nil {
					sess.panel.SetTitle(msg.Title)
				}
			}
			if msg.Cwd != "" {
				sess.cwd = msg.Cwd
				if sess.panel != nil {
					sess.panel.SetCwd(msg.Cwd)
				}
			}
		}
		return m, nil

	case termOutputMsg:
		if sess := m.sessions[msg.TerminalID]; sess != nil && sess.panel != nil {
			sess.panel.WriteOutput(msg.Data)
		}
		return m, nil

	case termExitMsg:
		if sess := m.sessions[msg.TerminalID]; sess != nil {
			sess.streaming = false
			sess.cancel = nil
			sess.exited = true
			sess.exitCode = msg.Code
			if sess.panel != nil {
				sess.panel.SetExitStatus(msg.Code, msg.Err)
			}
			if !m.remainOnExit {
				return m, m.closeSession(msg.TerminalID)
			}
		}
		return m, nil

	case termStreamEndedMsg:
		if sess := m.sessions[msg.TerminalID]; sess != nil {
			sess.streaming = false
			sess.cancel = nil
		}
		return m, nil

	case termControlMsg:
		var cmd tea.Cmd
		if msg.fn != nil {
			cmd = msg.fn()
		}
		if msg.done != nil {
			close(msg.done)
		}
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleKey(key tea.Key) (tea.Model, tea.Cmd) {
	p := m.activePanel()
	if p != nil && p.InCopyMode() {
		_, cmd := p.HandleKey(key)
		return m, cmd
	}

	if cmd, handled := m.handleShortcuts(key); handled {
		return m, cmd
	}

	if p != nil {
		_, cmd := p.HandleKey(key)
		return m, cmd
	}
	return m, nil
}

func (m *Model) handleShortcuts(key tea.Key) (tea.Cmd, bool) {
	if !key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl) {
		return nil, false
	}
	closeMenu := func() {
		if p := m.activePanel(); p != nil {
			p.SetMenuOpen(false)
		}
	}
	switch {
	case key.Code == 'n' || key.Code == 'N':
		closeMenu()
		info, err := m.createSession("")
		if err != nil {
			return nil, true
		}
		return m.startStream(info.TerminalID), true
	case key.Code == 'w' || key.Code == 'W':
		closeMenu()
		if m.activeID != "" {
			return m.closeSession(m.activeID), true
		}
		m.quitting = true
		return tea.Quit, true
	case key.Code == 'q' || key.Code == 'Q':
		closeMenu()
		m.quitting = true
		m.CloseAll()
		return tea.Quit, true
	case (key.Code == tea.KeyLeft || key.Code == '[') && !key.Mod.Contains(tea.ModShift):
		closeMenu()
		return m.cycleSession(-1), true
	case (key.Code == tea.KeyRight || key.Code == ']') && !key.Mod.Contains(tea.ModShift):
		closeMenu()
		return m.cycleSession(1), true
	case key.Code >= '1' && key.Code <= '9':
		idx := int(key.Code - '1')
		if idx >= 0 && idx < len(m.order) {
			closeMenu()
			return m.switchSession(m.order[idx]), true
		}
	}
	return nil, false
}

func (m *Model) handleMouseClick(ev tea.Mouse) (tea.Model, tea.Cmd) {
	p := m.activePanel()
	if p != nil {
		if handled, cmd := p.HandleMouse(ev, false, false); handled {
			m.sel.ResetSelection()
			return m, cmd
		}
	}
	if ev.Button != tea.MouseLeft {
		return m, nil
	}
	m.sel.SelDragging = true
	m.sel.SelStartRow = ev.Y
	m.sel.SelStartCol = ev.X
	m.sel.SelEndRow = ev.Y
	m.sel.SelEndCol = ev.X
	return m, nil
}

func (m *Model) handleMouseMotion(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if !m.sel.SelDragging {
		if p := m.activePanel(); p != nil {
			if handled, cmd := p.HandleMouse(ev, false, true); handled {
				return m, cmd
			}
		}
		return m, nil
	}
	row := ev.Y
	if row < 0 {
		row = 0
	}
	if m.height > 0 && row >= m.height {
		row = m.height - 1
	}
	m.sel.SelEndRow = row
	m.sel.SelEndCol = ev.X
	return m, nil
}

func (m *Model) handleMouseRelease(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if !m.sel.SelDragging {
		if p := m.activePanel(); p != nil {
			if handled, cmd := p.HandleMouse(ev, true, false); handled {
				return m, cmd
			}
		}
		return m, nil
	}
	m.sel.SelDragging = false
	if ev.Button != tea.MouseLeft {
		m.sel.ResetSelection()
		return m, nil
	}
	row := ev.Y
	if row < 0 {
		row = 0
	}
	if m.height > 0 && row >= m.height {
		row = m.height - 1
	}
	m.sel.SelEndRow = row
	m.sel.SelEndCol = ev.X

	if sr, sc, er, ec, ok := tuiviewport.SelectionRange(
		m.sel.SelStartRow, m.sel.SelStartCol,
		m.sel.SelEndRow, m.sel.SelEndCol,
		false,
	); ok {
		if p := m.activePanel(); p != nil {
			text := terminalpanel.ExtractSelectionTextWithOptions(p.Lines(), sr, sc, er, ec, false, true)
			if text != "" {
				_ = clipboard.Write(text)
			}
		}
	}
	m.sel.ResetSelection()
	return m, nil
}

// View renders the active terminal panel full-screen in alt-screen mode.
func (m *Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	p := m.activePanel()
	var content string
	if p != nil {
		lines := p.Lines()
		if sr, sc, er, ec, ok := tuiviewport.SelectionRange(
			m.sel.SelStartRow, m.sel.SelStartCol,
			m.sel.SelEndRow, m.sel.SelEndCol,
			m.sel.SelDragging,
		); ok {
			lines = tuiviewport.SelectionPass(lines, sr, sc, er, ec, true)
		}
		content = strings.Join(lines, "\n")
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	if p != nil && p.MouseAllMotion() {
		v.MouseMode = tea.MouseModeAllMotion
	}
	return v
}

// ControlBackend returns a terminalmgr.Backend that exposes this multiplexer's
// live tabs and screens over IPC/MCP so pigasus or external tools can inspect
// and orchestrate sessions (including "self").
func (m *Model) ControlBackend() terminalmgr.Backend {
	return &modelControlBackend{m: m}
}

type modelControlBackend struct {
	m *Model
}

func (b *modelControlBackend) callOnLoop(ctx context.Context, fn func() tea.Cmd) error {
	if b == nil || b.m == nil {
		return errors.New("terminal model is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	b.m.mu.Lock()
	hasLoop := b.m.sendMsg != nil || b.m.prog != nil
	b.m.mu.Unlock()
	if !hasLoop {
		b.m.ctrlMu.Lock()
		defer b.m.ctrlMu.Unlock()
		_ = fn()
		return nil
	}
	done := make(chan struct{})
	b.m.emit(termControlMsg{fn: fn, done: done})
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *Model) resolveSessionLocked(idOrName string) (*termSession, error) {
	key := strings.TrimSpace(idOrName)
	if key == "" || key == "active" {
		if sess := m.activeSession(); sess != nil {
			return sess, nil
		}
		return nil, errors.New("no active terminal session")
	}
	if sess := m.sessions[key]; sess != nil {
		return sess, nil
	}
	for _, id := range m.order {
		if sess := m.sessions[id]; sess != nil {
			if sess.name == key || sess.title == key {
				return sess, nil
			}
		}
	}
	return nil, fmt.Errorf("terminal session %q not found", idOrName)
}

func (m *Model) sessionInfoLocked(sess *termSession) terminalmgr.SessionInfo {
	if sess == nil {
		return terminalmgr.SessionInfo{}
	}
	var snap terminalpanel.Snapshot
	if sess.panel != nil {
		snap = sess.panel.CaptureSnapshot(0, false)
	}
	return terminalmgr.SessionInfo{
		ID:              sess.id,
		Name:            sess.name,
		Title:           sess.title,
		Cwd:             sess.cwd,
		PID:             sess.pid,
		Rows:            snap.Rows,
		Cols:            snap.Cols,
		CursorX:         snap.CursorX,
		CursorY:         snap.CursorY,
		Active:          sess.id == m.activeID,
		Exited:          sess.exited,
		ExitCode:        sess.exitCode,
		Status:          snap.Status,
		ScrollbackLines: snap.ScrollbackLines,
	}
}

func (b *modelControlBackend) List() []terminalmgr.SessionInfo {
	var out []terminalmgr.SessionInfo
	_ = b.callOnLoop(context.Background(), func() tea.Cmd {
		out = make([]terminalmgr.SessionInfo, 0, len(b.m.order))
		for _, id := range b.m.order {
			if sess := b.m.sessions[id]; sess != nil {
				out = append(out, b.m.sessionInfoLocked(sess))
			}
		}
		return nil
	})
	return out
}

func (b *modelControlBackend) New(ctx context.Context, opts terminalmgr.NewOptions) (terminalmgr.CaptureResult, error) {
	var (
		createdID string
		createErr error
	)
	if err := b.callOnLoop(ctx, func() tea.Cmd {
		info, err := b.m.createSessionWithOptions(strings.TrimSpace(opts.Name), "", opts.Cwd, opts.Focus)
		if err != nil {
			createErr = err
			return nil
		}
		createdID = info.TerminalID
		return b.m.startStream(createdID)
	}); err != nil {
		return terminalmgr.CaptureResult{}, err
	}
	if createErr != nil {
		return terminalmgr.CaptureResult{}, createErr
	}
	if opts.Command != "" && b.m.backend != nil {
		if err := b.m.backend.Write(createdID, []byte(opts.Command+"\r")); err != nil {
			return terminalmgr.CaptureResult{}, err
		}
	}
	waitForControlOutput(ctx, opts.WaitMs)
	return b.Capture(ctx, terminalmgr.CaptureOptions{ID: createdID})
}

func (b *modelControlBackend) Capture(ctx context.Context, opts terminalmgr.CaptureOptions) (terminalmgr.CaptureResult, error) {
	var (
		res terminalmgr.CaptureResult
		err error
	)
	if callErr := b.callOnLoop(ctx, func() tea.Cmd {
		sess, rErr := b.m.resolveSessionLocked(opts.ID)
		if rErr != nil {
			err = rErr
			return nil
		}
		info := b.m.sessionInfoLocked(sess)
		var snap terminalpanel.Snapshot
		if sess.panel != nil {
			snap = sess.panel.CaptureSnapshot(opts.Scrollback, opts.Raw)
		}
		res = terminalmgr.CaptureResult{
			Session:    info,
			Screen:     snap.Screen,
			Scrollback: snap.Scrollback,
		}
		return nil
	}); callErr != nil {
		return terminalmgr.CaptureResult{}, callErr
	}
	return res, err
}

func (b *modelControlBackend) Send(ctx context.Context, opts terminalmgr.SendOptions) (terminalmgr.CaptureResult, error) {
	var (
		targetID      string
		cursorKeysApp bool
		err           error
	)
	if callErr := b.callOnLoop(ctx, func() tea.Cmd {
		sess, rErr := b.m.resolveSessionLocked(opts.ID)
		if rErr != nil {
			err = rErr
			return nil
		}
		if sess.exited {
			err = fmt.Errorf("terminal session %q has already exited (code %d)", sess.id, sess.exitCode)
			return nil
		}
		targetID = sess.id
		if sess.panel != nil {
			cursorKeysApp = sess.panel.CursorKeysApp()
		}
		return nil
	}); callErr != nil {
		return terminalmgr.CaptureResult{}, callErr
	}
	if err != nil {
		return terminalmgr.CaptureResult{}, err
	}

	var payload []byte
	if opts.Text != "" {
		if opts.Paste {
			payload = append(payload, terminalmgr.EncodePaste(opts.Text)...)
		} else {
			payload = append(payload, []byte(opts.Text)...)
		}
	}
	if len(opts.Keys) > 0 {
		encoded, encErr := terminalmgr.EncodeKeys(opts.Keys, cursorKeysApp)
		if encErr != nil {
			return terminalmgr.CaptureResult{}, encErr
		}
		payload = append(payload, encoded...)
	}
	if len(payload) > 0 && b.m.backend != nil {
		if wErr := b.m.backend.Write(targetID, payload); wErr != nil {
			return terminalmgr.CaptureResult{}, wErr
		}
	}
	waitForControlOutput(ctx, opts.WaitMs)
	return b.Capture(ctx, terminalmgr.CaptureOptions{ID: targetID})
}

func (b *modelControlBackend) Resize(idOrName string, rows, cols int) (terminalmgr.SessionInfo, error) {
	var (
		info terminalmgr.SessionInfo
		err  error
	)
	if callErr := b.callOnLoop(context.Background(), func() tea.Cmd {
		sess, rErr := b.m.resolveSessionLocked(idOrName)
		if rErr != nil {
			err = rErr
			return nil
		}
		if sess.panel != nil {
			sess.panel.Resize(rows, cols)
		}
		info = b.m.sessionInfoLocked(sess)
		return nil
	}); callErr != nil {
		return terminalmgr.SessionInfo{}, callErr
	}
	return info, err
}

func (b *modelControlBackend) Kill(idOrName string) (terminalmgr.SessionInfo, error) {
	var (
		info terminalmgr.SessionInfo
		err  error
	)
	if callErr := b.callOnLoop(context.Background(), func() tea.Cmd {
		sess, rErr := b.m.resolveSessionLocked(idOrName)
		if rErr != nil {
			err = rErr
			return nil
		}
		info = b.m.sessionInfoLocked(sess)
		return b.m.closeSession(sess.id)
	}); callErr != nil {
		return terminalmgr.SessionInfo{}, callErr
	}
	return info, err
}

func (b *modelControlBackend) Switch(idOrName string) (terminalmgr.SessionInfo, error) {
	var (
		info terminalmgr.SessionInfo
		err  error
	)
	if callErr := b.callOnLoop(context.Background(), func() tea.Cmd {
		sess, rErr := b.m.resolveSessionLocked(idOrName)
		if rErr != nil {
			err = rErr
			return nil
		}
		cmd := b.m.switchSession(sess.id)
		info = b.m.sessionInfoLocked(sess)
		return cmd
	}); callErr != nil {
		return terminalmgr.SessionInfo{}, callErr
	}
	return info, err
}

func (b *modelControlBackend) Rename(idOrName, title string) (terminalmgr.SessionInfo, error) {
	var (
		info terminalmgr.SessionInfo
		err  error
	)
	if callErr := b.callOnLoop(context.Background(), func() tea.Cmd {
		sess, rErr := b.m.resolveSessionLocked(idOrName)
		if rErr != nil {
			err = rErr
			return nil
		}
		sess.title = title
		sess.customTitle = true
		if sess.panel != nil {
			sess.panel.SetTitle(title)
		}
		info = b.m.sessionInfoLocked(sess)
		return nil
	}); callErr != nil {
		return terminalmgr.SessionInfo{}, callErr
	}
	return info, err
}

func waitForControlOutput(ctx context.Context, waitMsPtr *int) {
	waitMs := 150
	if waitMsPtr != nil {
		waitMs = *waitMsPtr
	}
	if waitMs <= 0 {
		return
	}
	if waitMs > 5000 {
		waitMs = 5000
	}
	timer := time.NewTimer(time.Duration(waitMs) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
