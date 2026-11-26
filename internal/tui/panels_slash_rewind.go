package tui

import (
	"strings"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/rewind"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/views/rewindpanel"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
)

// SlashPicker
func (m *RootModel) AltScreenActive() bool { return m.viewport.altScreen }
func (m *RootModel) SlashScopeContext() slashcommands.ScopeContext {
	return m.slashScopeContext()
}
func (m *RootModel) slashScopeContext() slashcommands.ScopeContext {
	eng := m.turnEngine()
	name, caps := agentturn.CapabilitiesOf(eng)
	return slashcommands.ScopeContext{
		EngineName:   name,
		Capabilities: caps,
	}
}
func (m *RootModel) EngineSlashCommands() []slashcommands.Command {
	return m.engineSlashCommands()
}
func (m *RootModel) engineSlashCommands() []slashcommands.Command {
	var out []slashcommands.Command
	// In AGY 1.5.2: In --remote-agent sessions, skills from connected remote/cloud plugins
	// appear in the `/` menu, message-beginning completions, and `/skills`.
	if remotes := skills.RemoteSkills(); len(remotes) > 0 {
		for _, s := range remotes {
			name := "/" + s.Name
			out = append(out, slashcommands.Command{
				Name:        name,
				Description: s.Description,
				Source:      slashcommands.SourceSkill,
				Badge:       "remote",
			})
		}
	}

	eng := m.turnEngine()
	if eng == nil {
		return out
	}
	lister, ok := eng.(agentturn.CommandLister)
	if !ok {
		return out
	}
	cmds := lister.AvailableCommands()
	if len(cmds) == 0 {
		return out
	}
	for _, c := range cmds {
		name := c.Name
		if !strings.HasPrefix(name, "/") {
			name = "/" + name
		}
		desc := c.Description
		if c.InputHint != "" {
			desc += " " + c.InputHint
		}
		engName, _ := agentturn.CapabilitiesOf(eng)
		badge := "agent"
		if engName == "acp" || strings.Contains(engName, "acp") {
			badge = "acp"
		}
		out = append(out, slashcommands.Command{
			Name:        name,
			Description: desc,
			Source:      slashcommands.SourceAgent,
			Badge:       badge,
		})
	}
	return out
}
func (m *RootModel) newSlashPicker() *slashpicker.Panel {
	p := panels.NewSlashPicker(m)
	p.SetTerminalSource(m.terminalSlashCommands)
	return p
}

// Rewind
func (m *RootModel) RewindSteps() []rewind.Step {
	if sup := m.session.sup(); sup != nil {
		return sup.RewindSteps()
	}
	return nil
}
func (m *RootModel) RewindPickStep(index int) int {
	sup := m.session.sup()
	if sup == nil {
		return 0
	}
	r := sup.RewindToStep(index)
	if r.Removed <= 0 {
		return 0
	}
	m.slashRespond("/rewind", rewind.StepEcho(r))
	return r.Removed
}
func (m *RootModel) rewindPanel() *rewindpanel.Panel {
	return m.rewind.Panel(m)
}
