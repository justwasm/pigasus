package commands

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/hostactions"
)

// SessionHost represents host capabilities for session management slash commands.
type SessionHost interface {
	HostActionSeams() hostactions.Seams
	SlashRespond(cmd, msg string, extra ...string)
	StartManualCompact(instructions string)
	TurnEngine() any
	IsStreaming() bool
	SetInputNotice(msg string)
}

// RegisterSessionCommands attaches clear, exit, help, copy, feedback and compact handlers to the dispatcher.
func RegisterSessionCommands(d *Dispatcher, h SessionHost) {
	d.Register("clear", func(args string) (bool, tea.Cmd) {
		hostactions.HandleClear(h.HostActionSeams(), args)
		return true, nil
	})

	d.Register("exit", func(args string) (bool, tea.Cmd) {
		hostactions.HandleExit(h.HostActionSeams(), args)
		return true, tea.Quit
	})

	d.Register("help", func(args string) (bool, tea.Cmd) {
		hostactions.HandleHelp(h.HostActionSeams(), args)
		return true, nil
	})

	d.Register("copy", func(args string) (bool, tea.Cmd) {
		hostactions.HandleCopy(h.HostActionSeams(), args)
		return true, nil
	})

	d.Register("feedback", func(args string) (bool, tea.Cmd) {
		hostactions.HandleFeedback(h.HostActionSeams(), args)
		return true, nil
	})

	d.Register("compact", func(args string) (bool, tea.Cmd) {
		if h.IsStreaming() {
			h.SetInputNotice("Nothing to compact yet.")
			return true, nil
		}
		instructions := strings.TrimSpace(args)
		h.StartManualCompact(instructions)
		return true, nil
	})
}
