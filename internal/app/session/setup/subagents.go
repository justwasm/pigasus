// setup — subagents.go: the markdown agents a session's prompt carries.
//
// The list belongs to the prompt, not to the mode that builds it: the
// interactive session, the headless one and the server's all name the
// same subagents.

package setup

import (
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/features/agents"
)

// CustomSubagents lists the markdown agents added to the default agent's
// <subagents> section.
//
// The list belongs to the prompt, not to the mode that builds it: the
// interactive session, the headless one and the server's all name the
// same subagents, so the discovery lives in one place.
func CustomSubagents() []agyprompt.Subagent {
	found := agents.Discover()
	out := make([]agyprompt.Subagent, 0, len(found))
	for _, a := range found {
		out = append(out, agyprompt.Subagent{
			Name:        a.Name,
			Description: a.Description,
		})
	}
	return out
}
