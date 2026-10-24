// rehydrate.go — the print mode's rehydration of a continued
// conversation.
//
// The reference keeps one durable record of a conversation — the
// trajectory — and derives the agent's world from it when the
// conversation is loaded: the subagents it defined, the schedules it
// created, the tasks it managed (【反编译】 the reference's
// `cortex/rehydration` package and the cascade manager's
// `processLoadedTrajectory`, CLI 0x101e4e8a0; see
// out/ide-language-server/README.md §10.9).
//
// The print mode replays a continued conversation's earlier turns
// ahead of the prompt; the state the tools hold does not come back
// with them. This file fills it from the stored steps, so a continued
// conversation continues where it left off.
package printmode

import (
	"github.com/justwasm/pigasus/internal/rehydration"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

// rehydratePrintConversation rebuilds the live state of a continued
// conversation and returns its subagent definition store.
//
// The store is returned even when there is nothing to rehydrate: it is
// the conversation's store, and an empty one is what a fresh
// conversation has.
func rehydratePrintConversation(sel ConversationSelection, conversationID string) *subagenttools.Store {
	defs := subagenttools.NewStore()
	if sel.Store == nil {
		return defs
	}
	convID := sel.Store.ID
	if convID == "" {
		convID = conversationID
	}
	state := &rehydration.State{
		ConversationID: convID,
		Definitions:    defs,
		Schedules:      tools.ScheduleTableFor(convID),
		Tasks:          tools.SharedTaskRegistry(),
	}
	rehydration.Restore(sel.Store.RehydrationSteps(), state)
	return defs
}
