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
// This file keeps the trajectory whole. The steps the conversation was
// adopted with are preserved as they were, and the turns that ran
// since are appended: the user input, the tool calls of the turn and
// the model's reply, in the order they happened.
package cliapp

import (
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
)

// trajectorySteps returns the conversation's trajectory: the steps it
// was adopted with, plus the steps of the turns that ran since.
//
// The adopted steps come first and keep their indices, so the new
// steps continue the trajectory's numbering — the reference stores one
// row per step, indexed in order (【一手】 the reference's own
// conversation stores).
func (m *runMode) trajectorySteps() []convstore.Step {
	steps := make([]convstore.Step, 0, len(m.trajLoaded)+len(m.trajCalls)+len(m.session))
	steps = append(steps, m.trajLoaded...)
	idx := nextTrajectoryIndex(m.trajLoaded)
	calls := 0
	for _, msg := range m.session[m.trajSeededLen:] {
		switch msg.Role {
		case "user":
			steps = append(steps, convstore.Step{
				Idx:    idx,
				Type:   convstore.StepUserInput,
				Status: 3,
				Text:   msg.Content,
			})
			idx++
		case "system":
			// A background task's notification is the reference's own
			// system message: CORTEX_STEP_TYPE_SYSTEM_MESSAGE = 101
			// (the recovered .proto,
			// out/local-macos/protos/cortex_1.4.6.proto), with the
			// step status DONE = 3.
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			steps = append(steps, convstore.Step{
				Idx:    idx,
				Type:   convstore.StepSystemNotice,
				Status: 3,
				Text:   msg.Content,
			})
			idx++
		case "assistant":
			if strings.TrimSpace(msg.Content) == "" {
				continue
			}
			// The tool calls of the turn come before the reply: the
			// model calls its tools, then answers.
			for ; calls < len(m.trajCalls); calls++ {
				call := m.trajCalls[calls]
				call.Idx = idx
				steps = append(steps, call)
				idx++
			}
			steps = append(steps, convstore.Step{
				Idx:    idx,
				Type:   convstore.StepModelResponse,
				Status: 3,
				Text:   msg.Content,
			})
			idx++
		}
	}
	// The calls of a turn that has not replied yet.
	for ; calls < len(m.trajCalls); calls++ {
		call := m.trajCalls[calls]
		call.Idx = idx
		steps = append(steps, call)
		idx++
	}
	if n := len(steps); n > 0 && steps[n-1].Type == convstore.StepModelResponse {
		steps[n-1].Text = backend.TrimTrailingBlankLines(steps[n-1].Text)
	}
	return steps
}

// recordTrajectoryCalls adds a turn's tool calls to the trajectory.
//
// The steps come from the turn's own tool steps, so the raw arguments
// the model produced are stored verbatim — the rehydration pass reads
// them back to rebuild the state the calls produced (see
// internal/rehydration).
func (m *runMode) recordTrajectoryCalls(steps []toolStep) {
	for _, s := range steps {
		if s.kind != stepKindTool || s.name == "" {
			continue
		}
		status := int64(3)
		if s.err {
			status = 4
		}
		m.trajCalls = append(m.trajCalls, convstore.Step{
			Type:       convstore.StepToolCall,
			Status:     status,
			ToolName:   s.name,
			ToolArgs:   s.args,
			ToolResult: s.result,
		})
	}
}

// resetTrajectory drops the trajectory of the conversation that ended.
func (m *runMode) resetTrajectory() {
	m.trajLoaded = nil
	m.trajCalls = nil
	m.trajSeededLen = 0
}

// nextTrajectoryIndex returns the index the next step of the trajectory
// takes: one past the last stored step.
func nextTrajectoryIndex(steps []convstore.Step) int64 {
	var next int64
	for _, s := range steps {
		if s.Idx >= next {
			next = s.Idx + 1
		}
	}
	return next
}
