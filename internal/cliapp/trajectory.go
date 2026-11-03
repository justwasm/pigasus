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
package cliapp

import (
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

// trajectoryFacts collects the four facts the store needs to assemble
// the conversation's trajectory.
//
// The host owns the facts — they are the live state of the running
// session — and the store owns the assembly (convstore.BuildSteps).
func (m *runMode) trajectoryFacts() convstore.Trajectory {
	return convstore.Trajectory{
		Loaded:    m.trajLoaded,
		Calls:     m.trajCalls,
		Session:   m.session,
		SeededLen: m.trajSeededLen,
	}
}

// recordTrajectoryCalls adds a turn's tool calls to the trajectory.
//
// The steps come from the turn's own tool steps, so the raw arguments
// the model produced are stored verbatim — the rehydration pass reads
// them back to rebuild the state the calls produced (see
// internal/app/session/rehydration).
func (m *runMode) recordTrajectoryCalls(steps []toolsteps.Step) {
	for _, s := range steps {
		if s.Kind != toolsteps.KindTool || s.Name == "" {
			continue
		}
		status := int64(3)
		if s.Err {
			status = 4
		}
		m.trajCalls = append(m.trajCalls, convstore.Step{
			Type:       convstore.StepToolCall,
			Status:     status,
			ToolName:   s.Name,
			ToolArgs:   s.Args,
			ToolResult: s.Result,
		})
	}
}

// resetTrajectory drops the trajectory of the conversation that ended.
func (m *runMode) resetTrajectory() {
	m.trajLoaded = nil
	m.trajCalls = nil
	m.trajSeededLen = 0
}
