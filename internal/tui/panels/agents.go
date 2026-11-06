package panels

import (
	tea "charm.land/bubbletea/v2"
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/ui/views/agentspanel"
)

// AgentsHost defines the host operations needed by the /agents panel.
type AgentsHost interface {
	ActiveAgentName() string
	RunningSubagents() []*subagent.Subagent
	HasSessionTurns() bool
	SlashRespond(cmd, msg string, extra ...string)
	ApplyAgentSwitch(name, body string)
	ClosePanel() tea.Cmd
	KillAgentsSubagent(sa *subagent.Subagent) bool
	PendingSubagentAuthorization(subagentID string) (lifecycle.PendingApproval, bool)
	AnswerSubagentAuthorization(approvalID string, approved bool) bool
	AgentsWorkspace() string
	TerminalSize() (width int, height int)
}

// NewAgentsPanel builds the /agents panel with the given host seams.
func NewAgentsPanel(h AgentsHost) *agentspanel.Panel {
	catalogue := agents.DiscoverWith(agents.DiscoverOptions{Workspace: h.AgentsWorkspace()})
	return agentspanel.New(agentspanel.Seams{
		ActiveAgent: h.ActiveAgentName,
		Subagents:   h.RunningSubagents,
		HasSession:  h.HasSessionTurns,
		Respond: func(msg string) {
			h.SlashRespond("/agents", msg)
		},
		Switch: func(name, body string) {
			h.ApplyAgentSwitch(name, body)
		},
		Close:                h.ClosePanel,
		Kill:                 h.KillAgentsSubagent,
		PendingAuthorization: h.PendingSubagentAuthorization,
		Approve: func(approvalID string) bool {
			return h.AnswerSubagentAuthorization(approvalID, true)
		},
		Deny: func(approvalID string) bool {
			return h.AnswerSubagentAuthorization(approvalID, false)
		},
		Size: h.TerminalSize,
	}, catalogue)
}
