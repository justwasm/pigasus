// remote_terminal.go — the smallest remote-terminal output path.
//
// The terminal RPCs are the IDE/web surface, not a reference CLI command.
// For `pigasus --remote-agent` they are exposed as a host-only /terminal
// command so the create → input → stream → close cycle is usable from the
// TUI, and the output lands in the transcript.
package tui

import (
	"context"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
)

// slashTerminal handles /terminal in a remote session.
func (m *RootModel) slashTerminal(args string) (bool, tea.Cmd) {
	engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine)
	if !ok || !m.session.remoteAgent {
		m.slashRespond("/terminal", "Remote terminals are only available in a --remote-agent session.")
		return true, nil
	}
	trimmed := slashparse.Trim(args)
	verb, rest := slashparse.SplitVerb(trimmed)
	ctx, cancel := remoteTerminalContext()
	defer cancel()

	switch {
	case trimmed == "" || strings.EqualFold(verb, "list"):
		terminals, err := engine.Terminals(ctx)
		if err != nil {
			m.slashRespond("/terminal", "error: "+err.Error())
			return true, nil
		}
		if len(terminals) == 0 {
			info, err := engine.CreateTerminal(ctx)
			if err != nil {
				m.slashRespond("/terminal", "error: "+err.Error())
				return true, nil
			}
			m.remote.terminalID = info.TerminalID
			m.slashRespond("/terminal", "Terminal "+info.TerminalID+" created in "+info.Cwd+".")
			return true, nil
		}
		lines := []string{"Remote terminals:"}
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
	case strings.EqualFold(verb, "new"):
		info, err := engine.CreateTerminal(ctx)
		if err != nil {
			m.slashRespond("/terminal", "error: "+err.Error())
			return true, nil
		}
		m.remote.terminalID = info.TerminalID
		m.slashRespond("/terminal", "Terminal "+info.TerminalID+" created in "+info.Cwd+".")
		return true, nil
	case strings.EqualFold(verb, "close"):
		id := slashparse.Trim(rest)
		if id == "" {
			id = m.remote.terminalID
		}
		if id == "" {
			m.slashRespond("/terminal", "Usage: /terminal close <terminal-id>")
			return true, nil
		}
		if err := engine.CloseTerminal(ctx, id); err != nil {
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

// startRemoteTerminal creates a terminal, runs the command, and streams its
// output to the transcript until it exits.
func (m *RootModel) startRemoteTerminal(command string) tea.Cmd {
	engine, ok := m.turn.engine.(*agentturn.RemoteAgentEngine)
	if !ok {
		return nil
	}
	prog := m.turn.prog
	return func() tea.Msg {
		ctx := context.Background()
		info, err := engine.CreateTerminal(ctx)
		if err != nil {
			return terminalExitMsg{Err: err}
		}
		// The shell exits after the command, so the output stream ends.
		if err := engine.SendTerminalInput(ctx, info.TerminalID, []byte(command+"\nexit\n")); err != nil {
			return terminalExitMsg{Err: err}
		}
		events, err := engine.StreamTerminal(ctx, info.TerminalID, 0)
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
