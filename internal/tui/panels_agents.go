package tui

import (
	"os"

	tea "charm.land/bubbletea/v2"
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/core/lifecycle"
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/views/accountspanel"
	"github.com/justwasm/pigasus/internal/ui/views/agentspanel"
)

// Accounts
func (m *RootModel) StartAccountLogin() tea.Cmd             { return m.startAccountLogin() }
func (m *RootModel) FetchPlanTierCmd() tea.Cmd              { return m.fetchPlanTierCmd() }
func (m *RootModel) FetchQuotaCmd(email string) tea.Cmd     { return fetchAccountQuotaCmd(email) }
func (m *RootModel) newAccountsPanel() *accountspanel.Panel { return panels.NewAccountsPanel(m) }
func (m *RootModel) accountsPanel() *accountspanel.Panel {
	if m.overlays.panel.acct == nil {
		m.overlays.panel.acct = m.newAccountsPanel()
	}
	return m.overlays.panel.acct
}

// Agents
func (m *RootModel) ActiveAgentName() string                { return m.session.agentName }
func (m *RootModel) RunningSubagents() []*subagent.Subagent { return m.runningSubagents() }
func (m *RootModel) HasSessionTurns() bool                  { return m.session.messageCount() > 0 }
func (m *RootModel) ApplyAgentSwitch(name, body string) {
	m.applyAgentSwitch(name, body)
	m.overlays.panel = slashPanel{}
}
func (m *RootModel) KillAgentsSubagent(sa *subagent.Subagent) bool { return m.killAgentsSubagent(sa) }
func (m *RootModel) PendingSubagentAuthorization(id string) (lifecycle.PendingApproval, bool) {
	return m.pendingSubagentAuthorization(id)
}
func (m *RootModel) AnswerSubagentAuthorization(id string, approved bool) bool {
	return m.answerSubagentAuthorization(id, approved)
}
func (m *RootModel) AgentsWorkspace() string { return m.panelAgentsWorkspace() }
func (m *RootModel) newAgentsPanel() *agentspanel.Panel {
	return panels.NewAgentsPanel(m)
}
func (m *RootModel) agentsPanel() *agentspanel.Panel {
	if m.overlays.panel.ag == nil {
		m.overlays.panel.ag = m.newAgentsPanel()
	}
	return m.overlays.panel.ag
}
func (m *RootModel) LookupTask(id string) *taskregistry.Task {
	if m.tasks.reg != nil {
		if t := m.tasks.reg.Get(id); t != nil {
			return t
		}
	}
	if m.tasks.ctrl != nil && m.tasks.ctrl.Registry() != nil {
		return m.tasks.ctrl.Registry().Get(id)
	}
	return nil
}
func (m *RootModel) runningSubagents() []*subagent.Subagent {
	var out []*subagent.Subagent
	seen := make(map[string]bool)
	if m.tasks.subagentManager != nil {
		for _, sa := range m.tasks.subagentManager.List() {
			if sa != nil {
				seen[sa.ID] = true
				out = append(out, sa)
			}
		}
	}
	reg := m.tasks.reg
	if reg == nil && m.tasks.ctrl != nil {
		reg = m.tasks.ctrl.Registry()
	}
	if reg != nil {
		for _, t := range reg.List() {
			if t != nil && t.Kind == taskregistry.KindSubagent && !seen[t.ID] {
				seen[t.ID] = true
				sa := &subagent.Subagent{
					ID:     t.ID,
					Name:   t.Name,
					Title:  t.Name,
					State:  string(t.State),
					Model:  t.Model,
					Effort: t.Effort,
					Result: t.Output,
					Err:    t.Err,
				}
				out = append(out, sa)
			}
		}
	}
	return out
}
func (m *RootModel) killAgentsSubagent(sa *subagent.Subagent) bool {
	mgr := m.tasks.subagentManager
	if mgr == nil || sa == nil {
		return false
	}
	return mgr.KillRunning(sa.ID)
}
func (m *RootModel) pendingSubagentAuthorization(subagentID string) (lifecycle.PendingApproval, bool) {
	mgr := m.tasks.subagentManager
	if mgr == nil {
		return lifecycle.PendingApproval{}, false
	}
	return mgr.PendingAuthorization(subagentID)
}
func (m *RootModel) answerSubagentAuthorization(approvalID string, approve bool) bool {
	mgr := m.tasks.subagentManager
	if mgr == nil {
		return false
	}
	return mgr.AnswerAuthorization(approvalID, approve)
}
func (m *RootModel) panelAgentsWorkspace() string {
	workspace := m.session.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	return workspace
}
