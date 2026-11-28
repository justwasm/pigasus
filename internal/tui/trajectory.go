package tui

import (
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

// trajectory.go — the conversation's durable record.
//
// The reference stores every step of a conversation in one trajectory
// table — the user inputs, the model responses, the tool calls — and
// derives the agent's live state from it when the conversation is
// loaded: the subagents it defined, the schedules it created, the
// subagents it killed (【反编译】 the reference's `cortex/rehydration`
// package and the cascade manager's `processLoadedTrajectory`, CLI
// 0x101f26440; see out/ide-language-server/README.md §10.9).
//
// The clone used to write only the model messages, so a resumed
// conversation lost every tool call it had made — and, because the
// clone writes to the same store the reference uses, it also replaced
// the reference's stored trajectory with that thinner record.
//
// The store owns the trajectory — `convstore.BuildSteps` assembles it,
// because the store is what the steps are written to. This file keeps
// the host's half: the tool calls a turn made, and the reset when a
// conversation ends.

// trajectoryFacts collects the four facts the store needs to assemble
// the conversation's trajectory.
//
// The host owns the facts — they are the live state of the running
// session — and the store owns the assembly (convstore.BuildSteps).
func (m *RootModel) trajectoryFacts() convstore.Trajectory {
	if sup := m.session.sup(); sup != nil {
		return sup.TrajectoryFacts()
	}
	return convstore.Trajectory{Session: m.SessionMessages()}
}

// trajectoryCounts reports the adopted step count, the calls made since and
// the number of transcripts the adopted conversation contributed.
func (m *RootModel) trajectoryCounts() (loaded, calls, seededLen int) {
	if sup := m.session.sup(); sup != nil {
		return sup.TrajectoryCounts()
	}
	return 0, 0, 0
}

// flushPendingCheckpoints inserts the checkpoints a turn recorded into the
// trajectory's call list. The insertion point is the number of calls that
// preceded the checkpoint, so the persisted order matches the turn.
func (m *RootModel) flushPendingCheckpoints() {
	m.session.flushCheckpoints()
}

// recordTrajectoryCalls adds a turn's tool calls to the trajectory.
//
// The steps come from the turn's own tool steps, so the raw arguments
// the model produced are stored verbatim — the rehydration pass reads
// them back to rebuild the state the calls produced (see
// internal/app/session/rehydration).
func (m *RootModel) recordTrajectoryCalls(steps []toolsteps.Step) {
	var convSteps []convstore.Step
	for _, s := range steps {
		if s.Kind != toolsteps.KindTool || s.Name == "" {
			continue
		}
		status := int64(3)
		if s.Err {
			status = 4
		}
		convSteps = append(convSteps, convstore.Step{
			Type:     convstore.StepToolCall,
			Status:   status,
			ToolName: s.Name,
			ToolArgs: s.Args,
			// The two halves of the collapsed line, kept apart the way
			// the reference stores them: the verb in ToolPrefix and the
			// noun phrase in ToolSummary. Joining them is the reader's
			// job, and it can only do that once.
			//
			// Step.Summary is the *display* line ("Ran (go build ./...)"),
			// verb already included. Storing it verbatim put the verb in
			// the title field, and every reader that re-attached the
			// prefix printed "Ran Ran (go build ./...)".
			ToolPrefix:  convstore.RenderPrefix(s.Name),
			ToolSummary: convstore.TitleFromSummary(s.Summary, s.Name, s.Args),
			ToolResult:  s.Result,
		})
	}
	if sup := m.session.sup(); sup != nil {
		sup.RecordTrajectoryCalls(convSteps)
	}
}

// resetTrajectory drops the trajectory of the conversation that ended.
func (m *RootModel) resetTrajectory() {
	if sup := m.session.sup(); sup != nil {
		sup.Clear()
	}
	// The /context panel's hydrated bodies belong to the conversation
	// that just ended: a new conversation numbers its steps from zero
	// again, so a map keyed by index alone would answer a new step with
	// the old conversation's body.
	m.overlays.panel.ctxHydrated = nil
}
