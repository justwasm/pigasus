package rehydration

import (
	"encoding/json"

	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/taskregistry"
	"github.com/justwasm/pigasus/internal/tools"
)

// State is the live state a rehydration pass reads and writes.
//
// The reference's rehydration has no such struct: it rebuilds a step
// and lets the cascade manager apply the delta to the agent state it
// owns (【反编译】 CLI `processLoadedTrajectory` 0x101e4e8a0 →
// `updateStateForLoadedTrajectory` 0x101e4fc00). The clone collects
// the live stores in one place so the pass has a single entry point.
type State struct {
	// ConversationID is the conversation being rehydrated. The
	// schedule table's task ids are scoped to it.
	ConversationID string
	// Definitions is the conversation's subagent definition store.
	//
	// A definition is conversation-scoped in the reference ("Define a
	// subagent for the duration of this conversation"), so this is
	// where a resumed conversation gets its definitions back.
	Definitions *subagenttools.Store
	// Schedules is the conversation's schedule table.
	Schedules *tools.ScheduleTable
	// Tasks is the task registry the /tasks panel reads.
	Tasks *taskregistry.Registry
}

// NewDispatcher returns the dispatcher with the reference's five
// state-carrying tools wired to the given state.
//
// A tool whose store is missing is still dispatched — the delta is
// produced, it just has nowhere to go. That keeps the pass's shape
// independent of which stores the caller wired.
func NewDispatcher(state *State) *Dispatcher {
	d := New()
	if state == nil {
		return d
	}
	d.Register(Tool{
		Name:      "define_subagent",
		Rehydrate: subagenttools.RehydrateDefineSubagent,
		Apply:     applyDefinition(state),
	})
	d.Register(Tool{
		Name:      "manage_subagents",
		Rehydrate: subagenttools.RehydrateManageSubagents,
		Apply:     applyManageSubagents(state),
	})
	d.Register(Tool{
		Name:      "send_message",
		Rehydrate: subagenttools.RehydrateSendMessage,
		Apply:     applySendMessage(state),
	})
	d.Register(Tool{
		Name:      "manage_task",
		Rehydrate: tools.RehydrateManageTask,
		Apply:     applyManageTask(state),
	})
	d.Register(Tool{
		Name:      "schedule",
		Rehydrate: tools.RehydrateSchedule,
		Apply:     applySchedule(state),
	})
	return d
}

// applyDefinition returns the applier for a define_subagent delta, or
// nil when the state has no definition store.
func applyDefinition(state *State) Applier {
	if state.Definitions == nil {
		return nil
	}
	return state.Definitions.RestoreDefinition
}

// applyManageSubagents returns the applier for a manage_subagents
// delta.
func applyManageSubagents(state *State) Applier {
	if state.Definitions == nil {
		return nil
	}
	return state.Definitions.RestoreManageSubagents
}

// applySendMessage returns the applier for a send_message delta.
//
// The clone's delegations are synchronous, so there is never a live
// recipient to deliver a pending message to; the applier has nothing
// to restore. It is wired anyway so the pass reports the same shape as
// the reference's.
func applySendMessage(state *State) Applier {
	if state.Definitions == nil {
		return nil
	}
	return state.Definitions.RestoreSendMessage
}

// applyManageTask returns the applier for a manage_task delta.
func applyManageTask(state *State) Applier {
	if state.Tasks == nil {
		return nil
	}
	return func(meta map[string]string) bool {
		return tools.RestoreManageTask(state.Tasks, meta)
	}
}

// applySchedule returns the applier for a schedule delta.
func applySchedule(state *State) Applier {
	table := state.Schedules
	if table == nil {
		return nil
	}
	return func(meta map[string]string) bool {
		return tools.RestoreSchedule(table, meta)
	}
}

// Restore runs the whole pass over a trajectory: it rebuilds the
// deltas and writes them into the live state.
//
// It returns the deltas it produced and the number of them that
// changed something. The pass is skipped entirely when the rehydration
// is disabled (see Enabled).
func Restore(steps []Step, state *State) ([]Delta, int) {
	if !Enabled() {
		return nil, 0
	}
	// The trajectory replaces the conversation's state: a second resume
	// of the same conversation must not double the schedules it
	// restores.
	if state != nil && state.Schedules != nil {
		state.Schedules.Reset()
	}
	d := NewDispatcher(state)
	deltas := d.Rehydrate(steps)
	return deltas, d.Apply(deltas)
}

// compile-time assertion: the rehydrators the tool packages export
// have the shape the dispatcher expects.
var (
	_ Rehydrator = subagenttools.RehydrateDefineSubagent
	_ Rehydrator = subagenttools.RehydrateManageSubagents
	_ Rehydrator = subagenttools.RehydrateSendMessage
	_ Rehydrator = tools.RehydrateManageTask
	_ Rehydrator = tools.RehydrateSchedule
	_            = json.RawMessage{}
)
