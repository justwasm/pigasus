// agents_host.go — the host's side of the /agents panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/agentspanel: it owns the agent
// list, the running-agent section, the cursor, the keys and the
// prepared switch. The host supplies the facts the panel cannot
// answer itself — the session's active agent, the running subagents,
// whether the conversation has turns — and the two ways out of the
// panel.
//
// # The catalogue
//
// The markdown agents are internal/features/agents's fact: it reads the
// workspace's and the user's markdown trees. The host discovers them
// when the panel opens and hands the panel the values it found — the
// catalogue's own agent type, not a copy of it.

package cliapp

import (
	"os"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/ui/views/agentspanel"
)

// newAgentsPanel builds the /agents panel with the host's seams.
//
// The markdown agents are discovered here: the panel is rebuilt every
// time it opens, so the catalogue is read at that moment and the panel
// never shows a stale list.
func (m *runMode) newAgentsPanel() *agentspanel.Panel {
	catalogue := agents.DiscoverWith(agents.DiscoverOptions{Workspace: m.panelAgentsWorkspace()})
	return agentspanel.New(agentspanel.Seams{
		// The agent the session runs right now. The panel shows it as
		// the active one until a switch is prepared.
		ActiveAgent: func() string {
			return m.agentName
		},
		// The subagents that are running right now. The panel renders
		// their names and their status lines.
		Subagents: func() []*subagent.Subagent {
			return m.runningSubagents()
		},
		// The prepared-switch message says the switch will "fork the
		// current conversation" when there is one and "create a new
		// conversation" when there is not.
		HasSession: func() bool {
			return len(m.session) > 0
		},
		// The panel's own echo lines: the reference answers the Enter
		// on an agent row with a "> /agents" block in the transcript.
		Respond: func(msg string) {
			m.slashRespond("/agents", msg)
		},
		// Esc with a prepared switch applies it: the agent is
		// installed and the conversation is forked at that point. The
		// panel is closed without the exit notice, the way the
		// reference does it.
		Switch: func(name, body string) {
			m.applyAgentSwitch(name, body)
			m.panel = slashPanel{}
		},
		// Esc without a prepared switch just leaves the panel.
		Close: m.closePanel,
		// The kill: the manager owns the policy — only a running
		// subagent may be killed — so the host hands the panel's
		// subagent to it and answers whether the kill happened.
		Kill: func(sa *subagent.Subagent) bool {
			return m.killAgentsSubagent(sa)
		},
		// The authorization the subagent waits on, if any.
		PendingAuthorization: func(subagentID string) (lifecycle.PendingApproval, bool) {
			return m.pendingSubagentAuthorization(subagentID)
		},
		// The two answers to a pending authorization.
		Approve: func(approvalID string) bool {
			return m.answerSubagentAuthorization(approvalID, true)
		},
		Deny: func(approvalID string) bool {
			return m.answerSubagentAuthorization(approvalID, false)
		},
	}, catalogue)
}

// agentsPanel returns the open /agents panel.
//
// The panel is built when the /agents panel opens — the host's
// slashPanel struct is reset whenever a panel closes — so the accessor
// is here so that a caller which finds the panel missing still gets one
// that answers.
func (m *runMode) agentsPanel() *agentspanel.Panel {
	if m.panel.ag == nil {
		m.panel.ag = m.newAgentsPanel()
	}
	return m.panel.ag
}

// runningSubagents returns the subagents the /agents panel lists.
//
// The manager is the session's own when it has one, and the package's
// shared one otherwise — the same fallback the /tasks panel uses.
func (m *runMode) runningSubagents() []*subagent.Subagent {
	mgr := m.taskSubagentManager
	if mgr == nil {
		return nil
	}
	return mgr.List()
}

// killAgentsSubagent kills the subagent the /agents panel highlights.
//
// The manager owns the policy: only a *running* subagent may be
// killed, and a killed subagent keeps its row with its final status
// (the reference's own status list has "killed: Terminated manually
// by you or the parent process",
// official-docs/cli/commands/agents.md:88).
func (m *runMode) killAgentsSubagent(sa *subagent.Subagent) bool {
	mgr := m.taskSubagentManager
	if mgr == nil || sa == nil {
		return false
	}
	return mgr.KillRunning(sa.ID)
}

// pendingSubagentAuthorization is the protected operation the named
// subagent waits on, if any.
func (m *runMode) pendingSubagentAuthorization(subagentID string) (lifecycle.PendingApproval, bool) {
	mgr := m.taskSubagentManager
	if mgr == nil {
		return lifecycle.PendingApproval{}, false
	}
	return mgr.PendingAuthorization(subagentID)
}

// answerSubagentAuthorization answers a pending authorization of the
// session's subagents.
//
// approve allows the protected operation, refuse denies it.
func (m *runMode) answerSubagentAuthorization(approvalID string, approve bool) bool {
	mgr := m.taskSubagentManager
	if mgr == nil {
		return false
	}
	return mgr.AnswerAuthorization(approvalID, approve)
}

// panelAgentsWorkspace is the workspace the /agents panel discovers
// the markdown agents in: the session's own, or the process's.
func (m *runMode) panelAgentsWorkspace() string {
	workspace := m.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	return workspace
}
