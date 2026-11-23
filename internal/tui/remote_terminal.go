// remote_terminal.go — terminal management and execution path.
//
// Supports /terminal in local sessions (via rcterminal), remote agent sessions
// (via RemoteAgentEngine), and client/server sessions.
package tui

import (
	"context"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/views/terminalpanel"
)

func (m *RootModel) terminalService() TerminalService {
	if m.session.remoteAgent {
		if engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine); ok {
			return NewRemoteAgentTerminalService(engine)
		}
	}
	if m.remote.termService == nil {
		ws := m.session.cwd
		if ws == "" {
			ws, _ = os.Getwd()
		}
		m.remote.termService = NewLocalTerminalService(ws)
	}
	return m.remote.termService
}

func (m *RootModel) ensureTerminalSession(id, title, cwd string) *terminalSessionState {
	if id == "" {
		return nil
	}
	if m.remote.terminals == nil {
		m.remote.terminals = make(map[string]*terminalSessionState)
	}
	if sess, ok := m.remote.terminals[id]; ok {
		if title != "" && sess.title == "" {
			sess.title = title
			if sess.panel != nil {
				sess.panel.SetTitle(title)
			}
		}
		if cwd != "" && sess.cwd == "" {
			sess.cwd = cwd
			if sess.panel != nil {
				sess.panel.SetCwd(cwd)
			}
		}
		return sess
	}
	panel := m.newTerminalPanelForSession(id)
	if title != "" {
		panel.SetTitle(title)
	}
	if cwd != "" {
		panel.SetCwd(cwd)
	}
	sess := &terminalSessionState{
		id:    id,
		title: title,
		cwd:   cwd,
		panel: panel,
	}
	m.remote.terminals[id] = sess
	m.remote.terminalOrder = append(m.remote.terminalOrder, id)
	return sess
}

func (m *RootModel) activeTerminalSession() *terminalSessionState {
	if m.remote.terminalID == "" || m.remote.terminals == nil {
		return nil
	}
	return m.remote.terminals[m.remote.terminalID]
}

func (m *RootModel) terminalInnerDimensions() (rows, cols int) {
	h := m.viewport.height
	if h < 10 {
		h = 24
	}
	w := m.viewport.width
	if w < 20 {
		w = 80
	}
	rows = h - 4
	if len(m.remote.terminalOrder) > 1 && rows > 4 {
		rows--
	}
	if rows < 4 {
		rows = 4
	}
	cols = w - 4
	if cols < 10 {
		cols = 10
	}
	return rows, cols
}

func (m *RootModel) resizeAllTerminalPanels() {
	rows, cols := m.terminalInnerDimensions()
	for _, id := range m.remote.terminalOrder {
		if sess := m.remote.terminals[id]; sess != nil && sess.panel != nil {
			sess.panel.Resize(rows, cols)
		}
	}
	if m.overlays.panel.term != nil {
		active := m.activeTerminalSession()
		if active == nil || active.panel != m.overlays.panel.term {
			m.overlays.panel.term.Resize(rows, cols)
		}
	}
}

func (m *RootModel) switchTerminalSession(id string) tea.Cmd {
	if id == "" {
		return nil
	}
	sess := m.remote.terminals[id]
	if sess == nil {
		return nil
	}
	m.remote.terminalID = id
	m.overlays.panel.term = sess.panel
	rows, cols := m.terminalInnerDimensions()
	if sess.panel != nil {
		sess.panel.Resize(rows, cols)
	}
	m.openTerminalOverlay()
	return m.startTerminalStream(id)
}

func (m *RootModel) cycleTerminalSession(delta int) tea.Cmd {
	n := len(m.remote.terminalOrder)
	if n <= 1 {
		return nil
	}
	idx := 0
	for i, id := range m.remote.terminalOrder {
		if id == m.remote.terminalID {
			idx = i
			break
		}
	}
	next := (idx + delta) % n
	if next < 0 {
		next += n
	}
	return m.switchTerminalSession(m.remote.terminalOrder[next])
}

func (m *RootModel) createAndOpenTerminalSession() (TerminalInfo, tea.Cmd, error) {
	svc := m.terminalService()
	if svc == nil {
		return TerminalInfo{}, nil, os.ErrInvalid
	}
	ctx, cancel := remoteTerminalContext()
	defer cancel()
	info, err := svc.CreateTerminal(ctx, m.session.cwd)
	if err != nil {
		return TerminalInfo{}, nil, err
	}
	m.ensureTerminalSession(info.TerminalID, info.Title, info.Cwd)
	m.remote.terminalID = info.TerminalID
	m.resizeAllTerminalPanels()
	m.openTerminalOverlay()
	return info, m.startTerminalStream(info.TerminalID), nil
}

func (m *RootModel) killTerminalSession(id string) (tea.Cmd, error) {
	if id == "" {
		return nil, nil
	}
	if sess := m.remote.terminals[id]; sess != nil {
		if sess.cancel != nil {
			sess.cancel()
			sess.cancel = nil
		}
		sess.streaming = false
	}
	if svc := m.terminalService(); svc != nil {
		ctx, cancel := remoteTerminalContext()
		err := svc.CloseTerminal(ctx, id)
		cancel()
		if err != nil {
			return nil, err
		}
	}
	delete(m.remote.terminals, id)
	filtered := m.remote.terminalOrder[:0]
	for _, existing := range m.remote.terminalOrder {
		if existing != id {
			filtered = append(filtered, existing)
		}
	}
	m.remote.terminalOrder = filtered

	if m.remote.terminalID == id {
		if len(m.remote.terminalOrder) > 0 {
			nextID := m.remote.terminalOrder[0]
			m.remote.terminalID = nextID
			if nextSess := m.remote.terminals[nextID]; nextSess != nil {
				m.overlays.panel.term = nextSess.panel
			}
			if m.overlays.panel.kind == panelregistry.Terminal {
				m.resizeAllTerminalPanels()
				return m.startTerminalStream(nextID), nil
			}
			return nil, nil
		}
		m.remote.terminalID = ""
		m.overlays.panel.term = nil
		if m.overlays.panel.kind == panelregistry.Terminal {
			return m.closePanel(), nil
		}
	}
	return nil, nil
}

// CloseAllTerminals cancels all active terminal streams and closes all tracked
// terminal sessions so no orphan PTY processes remain on TUI exit.
func (m *RootModel) CloseAllTerminals() {
	if m == nil {
		return
	}
	svc := m.terminalService()
	for id, sess := range m.remote.terminals {
		if sess != nil && sess.cancel != nil {
			sess.cancel()
			sess.cancel = nil
			sess.streaming = false
		}
		if svc != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = svc.CloseTerminal(ctx, id)
			cancel()
		}
	}
	for id := range m.remote.runTerminals {
		if svc != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_ = svc.CloseTerminal(ctx, id)
			cancel()
		}
	}
	m.remote.terminals = nil
	m.remote.terminalOrder = nil
	m.remote.runTerminals = nil
	m.remote.terminalID = ""
	m.overlays.panel.term = nil
}

// slashTerminal handles /terminal across local and remote sessions.
func (m *RootModel) slashTerminal(args string) (bool, tea.Cmd) {
	svc := m.terminalService()
	if svc == nil {
		m.slashRespond("/terminal", "Terminal is not available.")
		return true, nil
	}
	trimmed := slashparse.Trim(args)
	verb, rest := slashparse.SplitVerb(trimmed)
	ctx, cancel := remoteTerminalContext()
	defer cancel()

	switch {
	case trimmed == "" || strings.EqualFold(verb, "list"):
		terminals, err := svc.ListTerminals(ctx)
		if err != nil {
			m.slashRespond("/terminal", "error: "+err.Error())
			return true, nil
		}
		if len(terminals) == 0 && len(m.remote.terminalOrder) == 0 {
			info, cmd, err := m.createAndOpenTerminalSession()
			if err != nil {
				m.slashRespond("/terminal", "error: "+err.Error())
				return true, nil
			}
			m.slashRespond("/terminal", "Terminal "+info.TerminalID+" created in "+info.Cwd+".")
			return true, cmd
		}
		for _, info := range terminals {
			m.ensureTerminalSession(info.TerminalID, info.Title, info.Cwd)
		}
		if trimmed == "" {
			if m.remote.terminalID == "" || m.remote.terminals[m.remote.terminalID] == nil {
				if len(m.remote.terminalOrder) > 0 {
					m.remote.terminalID = m.remote.terminalOrder[0]
				} else if len(terminals) > 0 {
					m.remote.terminalID = terminals[0].TerminalID
				}
			}
			return true, m.switchTerminalSession(m.remote.terminalID)
		}
		lines := []string{"Active terminals:"}
		for _, info := range terminals {
			prefix := "  "
			if info.TerminalID == m.remote.terminalID {
				prefix = "* "
			}
			line := prefix + info.TerminalID
			title := info.Title
			if sess := m.remote.terminals[info.TerminalID]; sess != nil && sess.title != "" {
				title = sess.title
			}
			if title != "" {
				line += "  " + title
			}
			if info.Cwd != "" {
				line += "  " + info.Cwd
			}
			lines = append(lines, line)
		}
		m.slashContent(strings.Join(lines, "\n"))
		return true, nil
	case strings.EqualFold(verb, "open") || strings.EqualFold(verb, "attach") || strings.EqualFold(verb, "switch"):
		terminals, err := svc.ListTerminals(ctx)
		if err != nil {
			m.slashRespond("/terminal", "error: "+err.Error())
			return true, nil
		}
		for _, info := range terminals {
			m.ensureTerminalSession(info.TerminalID, info.Title, info.Cwd)
		}
		id := slashparse.Trim(rest)
		if id == "" {
			if strings.EqualFold(verb, "switch") {
				m.slashRespond("/terminal", "Usage: /terminal switch <terminal-id>")
				return true, nil
			}
			if m.remote.terminalID != "" && m.remote.terminals[m.remote.terminalID] != nil {
				id = m.remote.terminalID
			} else if len(m.remote.terminalOrder) > 0 {
				id = m.remote.terminalOrder[0]
			}
		}
		if id == "" || m.remote.terminals[id] == nil {
			m.slashRespond("/terminal", "Terminal "+id+" not found.")
			return true, nil
		}
		return true, m.switchTerminalSession(id)
	case strings.EqualFold(verb, "new"):
		info, cmd, err := m.createAndOpenTerminalSession()
		if err != nil {
			m.slashRespond("/terminal", "error: "+err.Error())
			return true, nil
		}
		m.slashRespond("/terminal", "Terminal "+info.TerminalID+" created in "+info.Cwd+".")
		return true, cmd
	case strings.EqualFold(verb, "rename"):
		title := slashparse.Trim(rest)
		if title == "" {
			m.slashRespond("/terminal", "Usage: /terminal rename <title>")
			return true, nil
		}
		sess := m.activeTerminalSession()
		if sess == nil {
			m.slashRespond("/terminal", "No active terminal session.")
			return true, nil
		}
		sess.title = title
		if sess.panel != nil {
			sess.panel.SetTitle(title)
		}
		m.slashRespond("/terminal", "Terminal "+sess.id+" renamed to "+title+".")
		return true, nil
	case strings.EqualFold(verb, "close") || strings.EqualFold(verb, "kill"):
		id := slashparse.Trim(rest)
		if id == "" {
			id = m.remote.terminalID
		}
		if id == "" {
			m.slashRespond("/terminal", "Usage: /terminal close <terminal-id>")
			return true, nil
		}
		cmd, err := m.killTerminalSession(id)
		if err != nil {
			m.slashRespond("/terminal", "error: "+err.Error())
			return true, nil
		}
		m.slashRespond("/terminal", "Terminal "+id+" closed.")
		return true, cmd
	case strings.EqualFold(verb, "run"):
		command := slashparse.Trim(rest)
		if command == "" {
			m.slashRespond("/terminal", "Usage: /terminal run <command>")
			return true, nil
		}
		return true, m.startRemoteTerminal(command)
	default:
		m.slashRespond("/terminal", "Usage: /terminal [list|new|attach <id>|switch <id>|rename <title>|kill <id>|run <command>]")
		return true, nil
	}
}

// openTerminalOverlay displays the interactive virtual terminal panel modal.
func (m *RootModel) openTerminalOverlay() {
	if sess := m.activeTerminalSession(); sess != nil && sess.panel != nil {
		m.overlays.panel = slashPanel{kind: panelregistry.Terminal, cmd: "/terminal", term: sess.panel}
	} else {
		m.overlays.panel = slashPanel{kind: panelregistry.Terminal, cmd: "/terminal"}
	}
	m.initOpenedSlashPanel(panelregistry.Terminal)
}

// startTerminalStream streams output from the specified terminal into its session panel.
// Active streams are deduplicated per terminalID and resume from panel.ResumeOffset().
func (m *RootModel) startTerminalStream(terminalID string) tea.Cmd {
	if terminalID == "" {
		return nil
	}
	svc := m.terminalService()
	if svc == nil {
		return nil
	}
	prog := m.turn.prog
	if prog == nil {
		return nil
	}
	sess := m.remote.terminals[terminalID]
	var resumeOffset int64
	if sess != nil {
		if sess.streaming || sess.exited {
			return nil
		}
		if sess.panel != nil {
			resumeOffset = sess.panel.ResumeOffset()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	if sess != nil {
		sess.cancel = cancel
		sess.streaming = true
	}
	return func() tea.Msg {
		events, err := svc.StreamTerminal(ctx, terminalID, resumeOffset)
		if err != nil {
			cancel()
			prog.Send(terminalStreamEndedMsg{TerminalID: terminalID})
			return nil
		}
		go func() {
			defer cancel()
			for event := range events {
				if event.Title != "" || event.Cwd != "" {
					prog.Send(terminalMetaMsg{
						TerminalID: terminalID,
						Title:      event.Title,
						Cwd:        event.Cwd,
					})
				}
				if len(event.Output) > 0 {
					prog.Send(terminalChunkMsg{
						TerminalID: terminalID,
						Text:       string(event.Output),
					})
				}
				if event.ExitCode != nil {
					prog.Send(terminalExitMsg{
						TerminalID: terminalID,
						Code:       int(*event.ExitCode),
					})
					return
				}
			}
			prog.Send(terminalStreamEndedMsg{TerminalID: terminalID})
		}()
		return nil
	}
}

// startRemoteTerminal creates a terminal, runs the command, and streams its
// output to the transcript until it exits.
func (m *RootModel) startRemoteTerminal(command string) tea.Cmd {
	svc := m.terminalService()
	if svc == nil {
		return nil
	}
	prog := m.turn.prog
	cwd := m.session.cwd
	return func() tea.Msg {
		ctx := context.Background()
		info, err := svc.CreateTerminal(ctx, cwd)
		if err != nil {
			return terminalExitMsg{Err: err, RunOnly: true}
		}
		// The shell exits after the command, so the output stream ends.
		if err := svc.SendTerminalInput(ctx, info.TerminalID, []byte(command+"\nexit\n")); err != nil {
			return terminalExitMsg{TerminalID: info.TerminalID, Err: err, RunOnly: true}
		}
		events, err := svc.StreamTerminal(ctx, info.TerminalID, 0)
		if err != nil {
			return terminalExitMsg{TerminalID: info.TerminalID, Err: err, RunOnly: true}
		}
		go func() {
			for event := range events {
				if len(event.Output) > 0 && prog != nil {
					prog.Send(terminalChunkMsg{
						TerminalID: info.TerminalID,
						Text:       string(event.Output),
						RunOnly:    true,
					})
				}
				if event.ExitCode != nil {
					if prog != nil {
						prog.Send(terminalExitMsg{
							TerminalID: info.TerminalID,
							Code:       int(*event.ExitCode),
							RunOnly:    true,
						})
					}
					return
				}
			}
			if prog != nil {
				prog.Send(terminalExitMsg{TerminalID: info.TerminalID, RunOnly: true})
			}
		}()
		return terminalStartedMsg{
			Info:    remoteTerminalInfo{TerminalID: info.TerminalID, Cwd: info.Cwd},
			RunOnly: true,
		}
	}
}

// terminalStartedMsg announces a spawned terminal.
type terminalStartedMsg struct {
	Info    remoteTerminalInfo
	RunOnly bool
}

// terminalChunkMsg carries output bytes for a specific terminal session.
type terminalChunkMsg struct {
	TerminalID string
	Text       string
	RunOnly    bool
}

// terminalMetaMsg carries title/cwd updates for a specific terminal session.
type terminalMetaMsg struct {
	TerminalID string
	Title      string
	Cwd        string
}

// terminalExitMsg ends the stream for a specific terminal session.
type terminalExitMsg struct {
	TerminalID string
	Code       int
	Err        error
	RunOnly    bool
}

// terminalStreamEndedMsg clears the active streaming flag when a stream closes without an exit frame.
type terminalStreamEndedMsg struct {
	TerminalID string
}

// remoteTerminalInfo is the subset the transcript prints.
type remoteTerminalInfo struct {
	TerminalID string
	Cwd        string
}

func remoteTerminalContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// terminalOutputRows turns a chunk into transcript rows, stripping SGR and
// carriage returns and keeping internal blank lines.
func terminalOutputRows(text string) []string {
	text = termcolor.StripSGR(text)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

// itoaTerminal renders an exit code without importing strconv here.
func itoaTerminal(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func (m *RootModel) terminalTabs() []terminalpanel.TabInfo {
	if len(m.remote.terminalOrder) == 0 {
		return nil
	}
	tabs := make([]terminalpanel.TabInfo, 0, len(m.remote.terminalOrder))
	for _, id := range m.remote.terminalOrder {
		sess := m.remote.terminals[id]
		if sess == nil {
			continue
		}
		title := sess.title
		if title == "" {
			title = id
		}
		tabs = append(tabs, terminalpanel.TabInfo{
			ID:     id,
			Title:  title,
			Active: id == m.remote.terminalID,
			Exited: sess.exited,
		})
	}
	return tabs
}

func (m *RootModel) handleTerminalPanelShortcuts(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if !key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl) {
		return m, nil, false
	}
	switch {
	case key.Code == 'n' || key.Code == 'N':
		_, cmd, err := m.createAndOpenTerminalSession()
		if err != nil {
			m.slashRespond("/terminal", "error: "+err.Error())
			return m, nil, true
		}
		return m, cmd, true
	case key.Code == 'w' || key.Code == 'W':
		if m.remote.terminalID != "" {
			cmd, _ := m.killTerminalSession(m.remote.terminalID)
			return m, cmd, true
		}
		return m, m.closePanel(), true
	case key.Code == tea.KeyLeft || key.Code == '[':
		return m, m.cycleTerminalSession(-1), true
	case key.Code == tea.KeyRight || key.Code == ']':
		return m, m.cycleTerminalSession(1), true
	}
	return m, nil, false
}
