// picker_host.go — the host's side of the slash-command popup.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/slashpicker: it owns the
// filtered rows, the highlight, the scroll window, the on-screen
// rectangle, the rows it writes and the keys it handles.
//
// The host keeps the facts the panel cannot know — whether the
// alternate screen is up, the terminal's width, which screen row is
// the composer and what the composer holds — and hands them to the
// panel as seams.
//
// The host's job is to route: it re-derives the popup's state from
// the composer after every keystroke, it asks the panel for the rows
// when it paints a frame, and it asks the panel before it routes a
// key the panel owns.

package cliapp

import (
	"strings"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
)

// newSlashPicker builds the slash-command popup with the host's
// seams.
func (m *runMode) newSlashPicker() *slashpicker.Panel {
	return slashpicker.New(slashpicker.Seams{
		AltScreen:      func() bool { return m.altScreen },
		Width:          func() int { return m.width },
		IsInputY:       m.isInputY,
		Buffer:         func() string { return m.input.Value() },
		SetBuffer:      func(s string) { m.input.SetValue(s) },
		CustomCommands: m.engineSlashCommands,
		ScopeContext:   m.slashScopeContext,
	})
}

// slashScopeContext returns the active scope and capability context for slash command filtering.
func (m *runMode) slashScopeContext() slashcommands.ScopeContext {
	eng := m.turnEngine()
	name, caps := agentturn.CapabilitiesOf(eng)
	return slashcommands.ScopeContext{
		EngineName:   name,
		Capabilities: caps,
	}
}

// engineSlashCommands returns dynamic slash commands from the active agentturn
// engine if it implements agentturn.CommandLister (e.g. ACP, Claude, Codex).
func (m *runMode) engineSlashCommands() []slashcommands.Command {
	eng := m.turnEngine()
	if eng == nil {
		return nil
	}
	lister, ok := eng.(agentturn.CommandLister)
	if !ok {
		return nil
	}
	cmds := lister.AvailableCommands()
	if len(cmds) == 0 {
		return nil
	}
	out := make([]slashcommands.Command, 0, len(cmds))
	for _, c := range cmds {
		name := c.Name
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		desc := c.Description
		if c.InputHint != "" {
			desc += " " + c.InputHint
		}
		out = append(out, slashcommands.Command{
			Name:        name,
			Description: desc,
			Source:      slashcommands.SourceAgent,
			Badge:       "agent",
		})
	}
	return out
}
