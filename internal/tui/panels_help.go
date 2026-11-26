package tui

import (
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"
	"github.com/justwasm/pigasus/internal/ui/views/helppanel"
	"github.com/justwasm/pigasus/internal/ui/views/swarmboard"
)

// Help
func (m *RootModel) ModelName() string      { return m.session.model }
func (m *RootModel) TierName() string       { return m.session.tier }
func (m *RootModel) UserEmail() string      { return m.session.email }
func (m *RootModel) ConversationID() string { return m.session.conversationID }
func (m *RootModel) AppendResponse(lines []string) {
	m.conversation.response = append(m.conversation.response, lines...)
}
func (m *RootModel) SelectCommand(cmd string) {
	m.prompt.input.SetValue(cmd)
	m.prompt.input.CursorEnd()
	m.prompt.picker.Reset()
	m.prompt.promptHistory.Reset()
	m.ResetInputSelection()
}
func (m *RootModel) newHelpPanel() *helppanel.Panel { return panels.NewHelpPanel(m) }

func (m *RootModel) helpPanel() *helppanel.Panel {
	if m.help.Panel == nil {
		m.help.Panel = m.newHelpPanel()
	}
	return m.help.Panel
}

func (m *RootModel) openHelp(tab int) {
	hp := m.helpPanel()
	hp.Open(tab)
	m.overlays.panel = slashPanel{kind: panelregistry.Help, cmd: "/help"}
}

// ActiveItems
func (m *RootModel) OpenTasksDetail(id string, isSubagent bool) { m.openTasksDetail(id, isSubagent) }
func (m *RootModel) newActiveItemsPanel() *activeitemspanel.Panel {
	return panels.NewActiveItemsPanel(m)
}
func (m *RootModel) hasActiveSwarm() bool {
	if m == nil || m.swarm.dismissed {
		return false
	}
	if m.swarm.item != nil {
		return true
	}
	if m.swarm.Board != nil && (m.swarm.Board.PostCount() > 0 || m.swarm.Board.Title() != "" || len(m.swarm.Board.Members()) > 0) {
		return true
	}
	return false
}
func (m *RootModel) SwarmItem() *activeitemspanel.SwarmItem {
	if m == nil || m.swarm.dismissed {
		return nil
	}
	if m.swarm.item != nil {
		return m.swarm.item
	}
	if m.swarm.Board != nil && (m.swarm.Board.PostCount() > 0 || m.swarm.Board.Title() != "") {
		latestText := ""
		if post, ok := m.swarm.Board.LatestPost(); ok {
			latestText = post.Body
		}
		return &activeitemspanel.SwarmItem{
			Title:     m.swarm.Board.Title(),
			Latest:    latestText,
			PostCount: m.swarm.Board.PostCount(),
			IsDone:    m.swarm.Board.IsDone(),
		}
	}
	return nil
}
func (m *RootModel) OpenSwarmBoard() {
	if m == nil {
		return
	}
	if m.swarm.Board == nil {
		m.swarm.Board = m.newSwarmBoard()
	}
	m.swarm.Board.Open()
}
func (m *RootModel) DismissSwarm() {
	if m == nil {
		return
	}
	m.swarm.dismissed = true
}
func (m *RootModel) newSwarmBoard() *swarmboard.Board {
	return swarmboard.NewBoard(func(convID string) {
		m.swarm.fromBoard = true
		m.openTasksDetail(convID, true)
	})
}
