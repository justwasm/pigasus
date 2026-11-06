package agentspanel

import (
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
)

// MsgClose signals that the /agents panel should close.
type MsgClose struct{}

// MsgSwitchAgent requests switching to the prepared agent (and forking conversation).
type MsgSwitchAgent struct {
	Name string
	Body string
}

// MsgKillSubagent requests terminating a subagent.
type MsgKillSubagent struct {
	Subagent *subagent.Subagent
}

// MsgAuthorizeSubagent requests approving or denying a subagent authorization request.
type MsgAuthorizeSubagent struct {
	ApprovalID string
	Approved   bool
}
