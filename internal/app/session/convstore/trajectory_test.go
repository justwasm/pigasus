package convstore

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// TestBuildStepsKeepsTheAdoptedSteps pins the shape of the trajectory:
// the steps the conversation was adopted with come first and keep their
// indices, and the turns that ran since are appended to them.
//
// The clone writes to the same store the reference uses, so a
// trajectory that dropped the adopted steps would replace the
// reference's record with a thinner one.
func TestBuildStepsKeepsTheAdoptedSteps(t *testing.T) {
	steps := BuildSteps(Trajectory{
		Loaded: []Step{
			{Idx: 0, Type: StepUserInput, Status: 3, Text: "define a viewer subagent"},
			{Idx: 1, Type: StepToolCall, Status: 3, ToolName: "define_subagent"},
			{Idx: 2, Type: StepModelResponse, Status: 3, Text: "Defined."},
		},
		Calls: []Step{
			{Type: StepToolCall, Status: 3, ToolName: "run_command"},
		},
		Session: []backend.Message{
			{Role: "user", Content: "seeded"},
			{Role: "assistant", Content: "seeded answer"},
			{Role: "user", Content: "now run ls"},
			{Role: "assistant", Content: "Listed."},
		},
		SeededLen: 2,
	})
	wantTypes := []int64{
		StepUserInput,
		StepToolCall,
		StepModelResponse,
		StepUserInput,
		StepToolCall,
		StepModelResponse,
	}
	if len(steps) != len(wantTypes) {
		t.Fatalf("trajectory = %d steps, want %d: %+v", len(steps), len(wantTypes), steps)
	}
	for i, s := range steps {
		if s.Type != wantTypes[i] {
			t.Fatalf("step %d type = %d, want %d", i, s.Type, wantTypes[i])
		}
		// The indices continue the trajectory's numbering.
		if s.Idx != int64(i) {
			t.Fatalf("step %d index = %d, want %d", i, s.Idx, i)
		}
	}
	if steps[3].Text != "now run ls" {
		t.Fatalf("the new user turn = %+v", steps[3])
	}
	if steps[4].ToolName != "run_command" {
		t.Fatalf("the new tool call = %+v", steps[4])
	}
}

// TestBuildStepsPutsTheCallsBeforeTheReply pins the order of a turn:
// the model calls its tools, then answers.
//
// The reference's stored trajectory holds the call before the reply
// (【一手】 the reference's own conversation stores), and the
// rehydration pass walks the trajectory in that order.
func TestBuildStepsPutsTheCallsBeforeTheReply(t *testing.T) {
	steps := BuildSteps(Trajectory{
		Calls: []Step{
			{Type: StepToolCall, Status: 3, ToolName: "first"},
			{Type: StepToolCall, Status: 4, ToolName: "second"},
		},
		Session: []backend.Message{
			{Role: "user", Content: "go"},
			{Role: "assistant", Content: "done"},
		},
	})
	if len(steps) != 4 {
		t.Fatalf("trajectory = %d steps, want 4: %+v", len(steps), steps)
	}
	if steps[1].ToolName != "first" || steps[2].ToolName != "second" {
		t.Fatalf("the calls are out of order: %+v", steps)
	}
	if steps[3].Type != StepModelResponse {
		t.Fatalf("the reply is not last: %+v", steps)
	}
}

// TestBuildStepsSkipsTheBlankMessages pins that a message with no
// content adds no step: an empty system notice is not a notice, and an
// empty reply is not an answer.
func TestBuildStepsSkipsTheBlankMessages(t *testing.T) {
	steps := BuildSteps(Trajectory{
		Session: []backend.Message{
			{Role: "user", Content: "go"},
			{Role: "system", Content: "   "},
			{Role: "assistant", Content: "   "},
			{Role: "assistant", Content: "done"},
		},
	})
	if len(steps) != 2 {
		t.Fatalf("trajectory = %d steps, want 2: %+v", len(steps), steps)
	}
	if steps[0].Type != StepUserInput || steps[1].Type != StepModelResponse {
		t.Fatalf("the steps are wrong: %+v", steps)
	}
}

// TestBuildStepsTrimsTheLastReply pins that only the last model
// response is trimmed of its trailing blank lines.
//
// The stored blob is what the reference wrote; the trailing blank
// lines are the clone's own stream artefact, and they are trimmed off
// the last reply only — a reply in the middle of the trajectory is
// stored as it was.
func TestBuildStepsTrimsTheLastReply(t *testing.T) {
	steps := BuildSteps(Trajectory{
		Session: []backend.Message{
			{Role: "assistant", Content: "first\n\n"},
			{Role: "user", Content: "go"},
			{Role: "assistant", Content: "last\n\n\n"},
		},
	})
	if len(steps) != 3 {
		t.Fatalf("trajectory = %d steps, want 3: %+v", len(steps), steps)
	}
	if steps[0].Text != "first\n\n" {
		t.Fatalf("the first reply was trimmed: %q", steps[0].Text)
	}
	if steps[2].Text != "last" {
		t.Fatalf("the last reply = %q, want %q", steps[2].Text, "last")
	}
}

// TestNextStepIndex pins the numbering: the next step takes the index
// one past the last stored one.
func TestNextStepIndex(t *testing.T) {
	if got := NextStepIndex(nil); got != 0 {
		t.Fatalf("NextStepIndex(nil) = %d, want 0", got)
	}
	steps := []Step{
		{Idx: 0},
		{Idx: 1},
		{Idx: 2},
	}
	if got := NextStepIndex(steps); got != 3 {
		t.Fatalf("NextStepIndex = %d, want 3", got)
	}
	// The steps are not necessarily in order, and the highest index is
	// the one that counts.
	steps = []Step{{Idx: 7}, {Idx: 2}}
	if got := NextStepIndex(steps); got != 8 {
		t.Fatalf("NextStepIndex = %d, want 8", got)
	}
}

// TestBuildStepsHandlesSeededLenOutOfBounds pins that BuildSteps does not panic
// when SeededLen is larger than the session length (e.g. after a compaction or rewind)
// or negative.
func TestBuildStepsHandlesSeededLenOutOfBounds(t *testing.T) {
	// SeededLen > len(Session)
	steps := BuildSteps(Trajectory{
		Loaded: []Step{
			{Idx: 0, Type: StepUserInput, Status: 3, Text: "seeded user"},
		},
		Calls: []Step{
			{Type: StepToolCall, Status: 3, ToolName: "tool1"},
		},
		Session: []backend.Message{
			{Role: "user", Content: "# Resuming from a compaction"},
		},
		SeededLen: 363,
	})
	if len(steps) != 2 {
		t.Fatalf("trajectory = %d steps, want 2: %+v", len(steps), steps)
	}
	if steps[0].Text != "seeded user" {
		t.Fatalf("step 0 text = %q, want 'seeded user'", steps[0].Text)
	}
	if steps[1].ToolName != "tool1" {
		t.Fatalf("step 1 tool = %q, want 'tool1'", steps[1].ToolName)
	}

	// SeededLen < 0
	stepsNeg := BuildSteps(Trajectory{
		Loaded: []Step{
			{Idx: 0, Type: StepUserInput, Status: 3, Text: "loaded"},
		},
		Session: []backend.Message{
			{Role: "user", Content: "fresh"},
		},
		SeededLen: -1,
	})
	if len(stepsNeg) != 2 {
		t.Fatalf("trajectory = %d steps, want 2: %+v", len(stepsNeg), stepsNeg)
	}
}

// TestBuildStepsInsertsCheckpoints pins the durable checkpoint placement and
// the exclusive boundary ChatTurns reads.
func TestBuildStepsInsertsCheckpoints(t *testing.T) {
	steps := BuildSteps(Trajectory{
		Calls: []Step{
			{Type: StepToolCall, Status: 3, ToolName: "first"},
			{Type: StepCheckpoint, Status: 3, Summary: "<summary>s</summary>", UserRequests: []string{"go"}},
			{Type: StepToolCall, Status: 3, ToolName: "second"},
		},
		Session: []backend.Message{
			{Role: "user", Content: "go"},
			{Role: "assistant", Content: "done"},
		},
	})
	if len(steps) != 5 {
		t.Fatalf("steps = %d, want 5: %+v", len(steps), steps)
	}
	var cp *Step
	for i := range steps {
		if steps[i].Type == StepCheckpoint {
			cp = &steps[i]
		}
	}
	if cp == nil {
		t.Fatalf("no checkpoint in %+v", steps)
	}
	if cp.IncludedStart != 0 || cp.IncludedEnd != int(cp.Idx)+1 {
		t.Errorf("checkpoint range = %d..%d, idx %d", cp.IncludedStart, cp.IncludedEnd, cp.Idx)
	}
	if cp.CheckpointIndex != 0 {
		t.Errorf("checkpoint index = %d, want 0", cp.CheckpointIndex)
	}
}
