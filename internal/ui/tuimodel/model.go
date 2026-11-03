package tuimodel

import (
	tea "charm.land/bubbletea/v2"
)

// Model defines the 10-method contract for TUI models, matching
// agy 1.4.12's model layer (google3/third_party/jetski/cli/model).
//
// In the reference binary, 49 model types implement Update (and View/Init),
// while inheriting the other 7 methods from baseModel by embedding:
//   - Init() tea.Cmd
//   - Update(tea.Msg) (tea.Model, tea.Cmd)
//   - View() tea.View
//   - Store() any
//   - Layout() Layout
//   - UIPhase() UIPhase
//   - Keybindings() any
//   - ActionFromMsg(tea.Msg) tea.Cmd
//   - HasArtifacts() bool
//   - SetLayout(Layout)
type Model interface {
	tea.Model

	// The 7 inherited methods from Base:
	Store() any
	Layout() Layout
	UIPhase() UIPhase
	Keybindings() any
	ActionFromMsg(tea.Msg) tea.Cmd
	HasArtifacts() bool
	SetLayout(Layout)
}
