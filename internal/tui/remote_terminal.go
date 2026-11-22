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
		if len(terminals) == 0 {
			info, err := svc.CreateTerminal(ctx, m.session.cwd)
			if err != nil {
				m.slashRespond("/terminal", "error: "+err.Error())
				return true, nil
			}
			m.remote.terminalID = info.TerminalID
			m.slashRespond("/terminal", "Terminal "+info.TerminalID+" created in "+info.Cwd+".")
			m.openTerminalOverlay()
			return true, m.startTerminalStream(info.TerminalID)
		}
		if trimmed == "" {
			if m.remote.terminalID == "" {
				m.remote.terminalID = terminals[0].TerminalID
			}
			m.openTerminalOverlay()
			return true, m.startTerminalStream(m.remote.terminalID)
		}
		lines := []string{"Active terminals:"}
		for _, info := range terminals {
			line := "  " + info.TerminalID
			if info.Title != "" {
				line += "  " + info.Title
			}
			if info.Cwd != "" {
				line += "  " + info.Cwd
			}
			lines = append(lines, line)
		}
		m.slashContent(strings.Join(lines, "\n"))
		return true, nil
	case strings.EqualFold(verb, "open") || strings.EqualFold(verb, "attach"):
		terminals, err := svc.ListTerminals(ctx)
		if err != nil {
			m.slashRespond("/terminal", "error: "+err.Error())
			return true, nil
		}
		id := slashparse.Trim(rest)
		if id != "" {
			m.remote.terminalID = id
		} else if m.remote.terminalID == "" && len(terminals) > 0 {
			m.remote.terminalID = terminals[0].TerminalID
		}
		m.openTerminalOverlay()
		return true, m.startTerminalStream(m.remote.terminalID)
	case strings.EqualFold(verb, "new"):
		info, err := svc.CreateTerminal(ctx, m.session.cwd)
		if err != nil {
			m.slashRespond("/terminal", "error: "+err.Error())
			return true, nil
		}
		m.remote.terminalID = info.TerminalID
		m.slashRespond("/terminal", "Terminal "+info.TerminalID+" created in "+info.Cwd+".")
		m.openTerminalOverlay()
		return true, m.startTerminalStream(info.TerminalID)
	case strings.EqualFold(verb, "close"):
		id := slashparse.Trim(rest)
		if id == "" {
			id = m.remote.terminalID
		}
		if id == "" {
			m.slashRespond("/terminal", "Usage: /terminal close <terminal-id>")
			return true, nil
		}
		if err := svc.CloseTerminal(ctx, id); err != nil {
			m.slashRespond("/terminal", "error: "+err.Error())
			return true, nil
		}
		if m.remote.terminalID == id {
			m.remote.terminalID = ""
		}
		m.slashRespond("/terminal", "Terminal "+id+" closed.")
		return true, nil
	case strings.EqualFold(verb, "run"):
		command := slashparse.Trim(rest)
		if command == "" {
			m.slashRespond("/terminal", "Usage: /terminal run <command>")
			return true, nil
		}
		return true, m.startRemoteTerminal(command)
	default:
		m.slashRespond("/terminal", "Usage: /terminal [list|new|close <id>|run <command>]")
		return true, nil
	}
}

// openTerminalOverlay displays the interactive virtual terminal panel modal.
func (m *RootModel) openTerminalOverlay() {
	m.overlays.panel = slashPanel{kind: panelregistry.Terminal, cmd: "/terminal"}
	m.initOpenedSlashPanel(panelregistry.Terminal)
}

// startTerminalStream streams output from the specified terminal into the active terminal panel.
func (m *RootModel) startTerminalStream(terminalID string) tea.Cmd {
	if terminalID == "" {
		return nil
	}
	svc := m.terminalService()
	if svc == nil {
		return nil
	}
	prog := m.turn.prog
	return func() tea.Msg {
		ctx := context.Background()
		events, err := svc.StreamTerminal(ctx, terminalID, 0)
		if err != nil {
			return nil
		}
		go func() {
			for event := range events {
				if len(event.Output) > 0 && prog != nil {
					prog.Send(terminalChunkMsg{Text: string(event.Output)})
				}
				if event.ExitCode != nil {
					if prog != nil {
						prog.Send(terminalExitMsg{Code: int(*event.ExitCode)})
					}
					return
				}
			}
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
			return terminalExitMsg{Err: err}
		}
		// The shell exits after the command, so the output stream ends.
		if err := svc.SendTerminalInput(ctx, info.TerminalID, []byte(command+"\nexit\n")); err != nil {
			return terminalExitMsg{Err: err}
		}
		events, err := svc.StreamTerminal(ctx, info.TerminalID, 0)
		if err != nil {
			return terminalExitMsg{Err: err}
		}
		go func() {
			for event := range events {
				if len(event.Output) > 0 && prog != nil {
					prog.Send(terminalChunkMsg{Text: string(event.Output)})
				}
				if event.ExitCode != nil {
					if prog != nil {
						prog.Send(terminalExitMsg{Code: int(*event.ExitCode)})
					}
					return
				}
			}
			if prog != nil {
				prog.Send(terminalExitMsg{})
			}
		}()
		return terminalStartedMsg{Info: remoteTerminalInfo{TerminalID: info.TerminalID, Cwd: info.Cwd}}
	}
}

// terminalStartedMsg announces a spawned terminal.
type terminalStartedMsg struct {
	Info remoteTerminalInfo
}

// terminalChunkMsg carries output bytes.
type terminalChunkMsg struct {
	Text string
}

// terminalExitMsg ends the stream.
type terminalExitMsg struct {
	Code int
	Err  error
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
