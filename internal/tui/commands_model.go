package tui

// commands_model.go — the slash-command table sub-model.
//
// CommandsModel owns the one dispatcher the session builds and reuses for
// every slash command. Building it per command re-registered every handler
// (and let two tables answer the same name), so the table is created once
// on first use and kept here.

import "github.com/justwasm/pigasus/internal/tui/commands"

// CommandsModel owns the slash-command dispatcher.
type CommandsModel struct {
	dispatcher *commands.Dispatcher
}

// Dispatcher returns the existing table, or nil before the first command.
func (c *CommandsModel) Dispatcher() *commands.Dispatcher {
	if c == nil {
		return nil
	}
	return c.dispatcher
}

// SetDispatcher records the table built for this session.
func (c *CommandsModel) SetDispatcher(d *commands.Dispatcher) {
	if c != nil {
		c.dispatcher = d
	}
}
