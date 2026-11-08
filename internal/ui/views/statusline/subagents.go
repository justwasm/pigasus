// subagents.go — the payload's `subagents` member.
//
// The payload's entry carries a subagent's name, its role and its status
// (official-docs/cli/statusline.md:71). The conversation's registry
// carries the same children in its own list entries: the type, the role,
// the conversation id and the state — the reference's `manage_subagents`
// entry.
//
// The mapping is the payload's: the wire vocabulary is a contract with
// the scripts that read the document, so it belongs with the document and
// not with the host that fills it in.
package statusline

import "github.com/justwasm/pigasus/internal/app/subagenttools"

// Subagents renders the conversation's subagent list entries in the
// payload's shape.
//
// The name is the subagent's type — the name the child was launched as —
// and the role is the role the call asked for, falling back to the type's
// name when the call named none (the reference's own rule for its list
// entry).
//
// The status is the payload's own two-value vocabulary: "running" while
// the child's turn is in flight and "completed" otherwise. The registry
// knows more states (a child that is being killed, one whose turn
// failed), and the payload's contract has no room for them.
func Subagents(states []subagenttools.StateEntry) []SubagentInfo {
	if len(states) == 0 {
		return nil
	}
	out := make([]SubagentInfo, 0, len(states))
	for _, s := range states {
		role := s.Role
		if role == "" {
			role = s.Type
		}
		status := "completed"
		if s.State == subagenttools.StateRunning {
			status = "running"
		}
		out = append(out, SubagentInfo{
			Name:   s.Type,
			Role:   role,
			Status: status,
			Model:  s.Model,
		})
	}
	return out
}
