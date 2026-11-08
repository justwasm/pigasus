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
// This file assembles that record. The steps the conversation was
// adopted with are preserved as they were, and the turns that ran since
// are appended: the user input, the tool calls of the turn and the
// model's reply, in the order they happened.
//
// The store owns the assembly because the store owns the trajectory:
// the host holds the four facts below and the store decides what they
// mean.
package convstore

import (
	"strings"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// Trajectory carries the four facts a conversation's trajectory is
// assembled from.
//
// The host owns the facts — they are the live state of a running
// session — and the store owns the assembly.
type Trajectory struct {
	// Loaded is the trajectory the conversation was adopted with: the
	// steps the store already holds. They come first and keep their
	// indices.
	Loaded []Step
	// Calls are the tool calls the turns that ran since have made. A
	// turn's calls come before the model's reply of that turn.
	Calls []Step
	// Session is the live conversation's messages — the seeded ones and
	// the ones the turns that ran since added.
	Session []backend.Message
	// SeededLen is how many of Session's messages the seeding wrote, so
	// the turns that ran since are the messages after it.
	SeededLen int
}

// BuildSteps assembles the conversation's trajectory: the steps it was
// adopted with, plus the steps of the turns that ran since.
//
// The adopted steps come first and keep their indices, so the new steps
// continue the trajectory's numbering — the reference stores one row
// per step, indexed in order (【一手】 the reference's own conversation
// stores).
func BuildSteps(t Trajectory) []Step {
	steps := make([]Step, 0, len(t.Loaded)+len(t.Calls)+len(t.Session))
	steps = append(steps, t.Loaded...)
	idx := NextStepIndex(t.Loaded)
	lastCheckpointEnd := 0
	for _, s := range t.Loaded {
		if s.Type == StepCheckpoint && s.IncludedEnd > lastCheckpointEnd {
			lastCheckpointEnd = s.IncludedEnd
		}
	}
	calls := 0
	seededLen := t.SeededLen
	if seededLen < 0 {
		seededLen = 0
	}
	if seededLen > len(t.Session) {
		seededLen = len(t.Session)
	}
	for _, msg := range t.Session[seededLen:] {
		switch msg.Role {
		case "user":
			steps = append(steps, Step{
				Idx:    idx,
				Type:   StepUserInput,
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
			steps = append(steps, Step{
				Idx:    idx,
				Type:   StepSystemNotice,
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
			for ; calls < len(t.Calls); calls++ {
				call := t.Calls[calls]
				call.Idx = idx
				if call.Type == StepCheckpoint {
					call.CheckpointIndex = countCheckpoints(steps)
					call.IncludedStart = lastCheckpointEnd
					call.IncludedEnd = int(idx) + 1
					lastCheckpointEnd = call.IncludedEnd
				}
				steps = append(steps, call)
				idx++
			}
			steps = append(steps, Step{
				Idx:    idx,
				Type:   StepModelResponse,
				Status: 3,
				Text:   msg.Content,
			})
			idx++
		}
	}
	// The calls of a turn that has not replied yet.
	for ; calls < len(t.Calls); calls++ {
		call := t.Calls[calls]
		call.Idx = idx
		if call.Type == StepCheckpoint {
			call.CheckpointIndex = countCheckpoints(steps)
			call.IncludedStart = lastCheckpointEnd
			call.IncludedEnd = int(idx) + 1
			lastCheckpointEnd = call.IncludedEnd
		}
		steps = append(steps, call)
		idx++
	}
	if n := len(steps); n > 0 && steps[n-1].Type == StepModelResponse {
		steps[n-1].Text = backend.TrimTrailingBlankLines(steps[n-1].Text)
	}
	return steps
}

// NextStepIndex returns the index the next step of the trajectory
// takes: one past the last stored step.
func NextStepIndex(steps []Step) int64 {
	var next int64
	for _, s := range steps {
		if s.Idx >= next {
			next = s.Idx + 1
		}
	}
	return next
}

// countCheckpoints is the number of checkpoint steps already in the
// trajectory. It is the checkpoint's own index.
func countCheckpoints(steps []Step) int {
	n := 0
	for _, s := range steps {
		if s.Type == StepCheckpoint {
			n++
		}
	}
	return n
}
