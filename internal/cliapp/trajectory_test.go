package cliapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
)

// writeTrajectoryFixture writes a conversation whose trajectory holds a
// define_subagent call, and returns the path of the store.
//
// The fixture is written with the clone's own writer, so the test
// exercises the same round trip a real conversation takes: the tool
// call goes into the trajectory, comes back out of it, and the
// rehydration pass reads the arguments.
func writeTrajectoryFixture(t *testing.T, steps []convstore.Step) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "trajectory.db")
	if err := convstore.WriteConversation(path, "traj-fixture", steps); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

// defineSubagentStep is the step a define_subagent call leaves behind.
//
// 【一手】 the reference stores the call's arguments under the
// capitalised native-payload names, so the stored arguments are
// {"Name":…,"SystemPrompt":…}.
func defineSubagentStep(idx int64) convstore.Step {
	return convstore.Step{
		Idx:    idx,
		Type:   convstore.StepToolCall,
		Status: 3,
		ToolName: "define_subagent",
		ToolArgs: `{"Name":"viewer","Description":"Viewport work",` +
			`"SystemPrompt":"You are a Go developer."}`,
		ToolResult: `Subagent "viewer" defined successfully.`,
	}
}

// TestTrajectoryCarriesToolCalls pins that the conversation's
// trajectory holds the tool calls, and that a call survives the round
// trip through the store with its arguments intact.
func TestTrajectoryCarriesToolCalls(t *testing.T) {
	path := writeTrajectoryFixture(t, []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "define a viewer subagent"},
		defineSubagentStep(1),
		{Idx: 2, Type: convstore.StepModelResponse, Status: 3, Text: "Defined."},
	})
	c, err := convstore.LoadConversation(path)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	var calls []convstore.Step
	for _, s := range c.Steps {
		if s.Type == convstore.StepToolCall {
			calls = append(calls, s)
		}
	}
	if len(calls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(calls))
	}
	if calls[0].ToolName != "define_subagent" {
		t.Fatalf("tool name = %q, want define_subagent", calls[0].ToolName)
	}
	if !strings.Contains(calls[0].ToolArgs, `"Name":"viewer"`) {
		t.Fatalf("tool args = %q, want the stored arguments verbatim", calls[0].ToolArgs)
	}
}

// TestResumeRehydratesTheDefinitions pins the whole path: a resumed
// conversation gets its subagent definitions back from the trajectory.
//
// The reference derives the agent's live state from the trajectory
// when the conversation is loaded (【反编译】 the reference's
// `cortex/rehydration` package; see out/ide-language-server/README.md
// §10.9). The clone's definitions live in a conversation-scoped store,
// so a resume that did not rehydrate would leave the conversation
// unable to call the subagent it defined.
func TestResumeRehydratesTheDefinitions(t *testing.T) {
	path := writeTrajectoryFixture(t, []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "define a viewer subagent"},
		defineSubagentStep(1),
		{Idx: 2, Type: convstore.StepModelResponse, Status: 3, Text: "Defined."},
	})
	c, err := convstore.LoadConversation(path)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.seedConversation(c)
	def, ok := m.subagentDefs.Get("viewer")
	if !ok {
		t.Fatalf("the definition was not rehydrated: %v", m.subagentDefs.Names())
	}
	if def.SystemPrompt != "You are a Go developer." {
		t.Fatalf("rehydrated definition = %+v", def)
	}
}

// TestTrajectoryKeepsTheAdoptedSteps pins that a resumed conversation
// keeps the steps it was adopted with — the reference's own tool calls
// included — and appends the new turns to them.
//
// The clone writes to the same store the reference uses, so a resume
// that dropped the adopted steps would replace the reference's
// trajectory with a thinner one.
func TestTrajectoryKeepsTheAdoptedSteps(t *testing.T) {
	path := writeTrajectoryFixture(t, []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "define a viewer subagent"},
		defineSubagentStep(1),
		{Idx: 2, Type: convstore.StepModelResponse, Status: 3, Text: "Defined."},
	})
	c, err := convstore.LoadConversation(path)
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	m := NewRunMode(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.seedConversation(c)

	// A new turn: the user asks something, the model calls a tool and
	// answers.
	m.session = append(m.session, backend.Message{Role: "user", Content: "now run ls"})
	m.recordTrajectoryCalls([]toolStep{{
		kind:   stepKindTool,
		name:   "run_command",
		args:   `{"CommandLine":"ls"}`,
		result: "a b c",
	}})
	m.session = append(m.session, backend.Message{Role: "assistant", Content: "Listed."})

	steps := m.trajectorySteps()
	if len(steps) != 6 {
		t.Fatalf("trajectory = %d steps, want 6: %+v", len(steps), steps)
	}
	wantTypes := []int64{
		convstore.StepUserInput,
		convstore.StepToolCall,
		convstore.StepModelResponse,
		convstore.StepUserInput,
		convstore.StepToolCall,
		convstore.StepModelResponse,
	}
	// The adopted steps keep their place, the new ones follow.
	for i, s := range steps {
		if s.Type != wantTypes[i] {
			t.Fatalf("step %d type = %d, want %d", i, s.Type, wantTypes[i])
		}
	}
	if steps[1].ToolName != "define_subagent" {
		t.Fatalf("the adopted tool call was lost: %+v", steps[1])
	}
	if steps[3].Type != convstore.StepUserInput || steps[3].Text != "now run ls" {
		t.Fatalf("the new user turn = %+v", steps[3])
	}
	if steps[4].Type != convstore.StepToolCall || steps[4].ToolName != "run_command" {
		t.Fatalf("the new tool call = %+v", steps[4])
	}
	if steps[4].ToolArgs != `{"CommandLine":"ls"}` {
		t.Fatalf("the new tool call's arguments = %q", steps[4].ToolArgs)
	}
	// The indices continue the trajectory's numbering.
	for i, s := range steps {
		if s.Idx != int64(i) {
			t.Fatalf("step %d index = %d, want %d", i, s.Idx, i)
		}
	}
}

// TestTrajectorySaveRoundTrip pins that the saved trajectory reads back
// with its tool calls and their arguments — the rehydration pass reads
// the arguments, so they have to survive the store.
func TestTrajectorySaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	path := filepath.Join(dir, "roundtrip.db")
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "define a viewer subagent"},
		defineSubagentStep(1),
		{Idx: 2, Type: convstore.StepModelResponse, Status: 3, Text: "Defined."},
	}
	if err := convstore.WriteConversation(path, "traj-fixture", steps); err != nil {
		t.Fatalf("write: %v", err)
	}
	c, err := convstore.LoadConversation(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(c.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(c.Steps))
	}
	if c.Steps[1].ToolName != "define_subagent" {
		t.Fatalf("tool name = %q", c.Steps[1].ToolName)
	}
	if c.Steps[1].ToolArgs != steps[1].ToolArgs {
		t.Fatalf("tool args = %q, want %q", c.Steps[1].ToolArgs, steps[1].ToolArgs)
	}
	if c.Steps[1].ToolResult != steps[1].ToolResult {
		t.Fatalf("tool result = %q, want %q", c.Steps[1].ToolResult, steps[1].ToolResult)
	}
	if c.Steps[0].Text != "define a viewer subagent" {
		t.Fatalf("user text = %q", c.Steps[0].Text)
	}
	if c.Steps[2].Text != "Defined." {
		t.Fatalf("model text = %q", c.Steps[2].Text)
	}
	// The store is a real file the picker can find.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}
}
