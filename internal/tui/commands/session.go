package commands

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/slashparse"
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
	// ApplyModelArgument applies the "/model <name>" form; the host owns
	// the catalogue and the active-engine refresh.
	ApplyModelArgument(arg string) tea.Cmd
	// ShowModelList renders the bare "/model" listing.
	ShowModelList()
	// OpenResumePicker opens the conversation picker for "/resume".
	OpenResumePicker() error
}

// RegisterSessionCommands attaches the session-level commands — clear,
// exit, help, copy, feedback, compact, model and resume — to the dispatcher.
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

	// "/model <name>" is a direct setter — the reference changes the model
	// and echoes "Model set to <label>". Only a bare "/model" opens the
	// listing. 【一手】 agy 1.4.6, live.
	d.Register("model", func(args string) (bool, tea.Cmd) {
		if trimmed := slashparse.Trim(args); trimmed != "" {
			return true, h.ApplyModelArgument(trimmed)
		}
		h.ShowModelList()
		return true, nil
	})

	// "/resume" opens the conversation picker; the picker owns the list.
	d.Register("resume", func(args string) (bool, tea.Cmd) {
		if err := h.OpenResumePicker(); err != nil {
			h.SlashRespond("/resume", "Failed to list conversations: "+err.Error())
		}
		return true, nil
	})
}
