// panel_hydrate_test.go — the cold-step path's other half.
//
// A cold row's Enter queues a store read and answers with
// contextHydrateMsg. Before this test the message had no handler: the
// read ran, the answer was dropped, and the row kept saying "body not in
// memory" forever. The key was claimed and nothing visible happened,
// which is the worst of the three possible outcomes.
package tui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// TestHydrateAnswerFoldsIntoTheRow — the answer the store gives is what
// the Trajectory row shows afterwards.
func TestHydrateAnswerFoldsIntoTheRow(t *testing.T) {
	m := mkPanel(panelregistry.Debug, "/debug", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.viewport.height = 40
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "read it"},
		{Role: "assistant", Content: "done"},
	})
	m.session.sup().RecordTrajectoryCalls([]convstore.Step{
		{Idx: 1, Type: convstore.StepUserInput, Text: "read it"},
		{Idx: 2, Type: convstore.StepToolCall, Status: 3, ToolName: "read_file",
			ToolPrefix: "Read", ToolSummary: "main.go", ToolArgs: `{"path":"main.go"}`},
		{Idx: 3, Type: convstore.StepModelResponse, Text: "done"},
	})

	// The row is cold before the answer: the body is not in memory.
	before := m.contextTrajectoryState()
	var cold bool
	for _, turn := range before.TurnsList {
		for _, step := range turn.Steps {
			if step.Cold {
				cold = true
			}
		}
	}
	if !cold {
		t.Fatal("the fixture's tool step is not cold, so this test proves nothing")
	}

	// The answer lands with a body the store kept.
	_, _ = m.Update(contextHydrateMsg{
		index: 2,
		step: convstore.Step{
			Idx: 2, Type: convstore.StepToolCall, Status: 3, ToolName: "read_file",
			ToolPrefix: "Read", ToolSummary: "main.go",
			ToolArgs: `{"path":"main.go"}`, ToolResult: "package main\n",
		},
	})

	after := m.contextTrajectoryState()
	var hydrated *struct {
		cold   bool
		detail []string
	}
	for _, turn := range after.TurnsList {
		for i := range turn.Steps {
			step := &turn.Steps[i]
			if step.Index != 2 {
				continue
			}
			hydrated = &struct {
				cold   bool
				detail []string
			}{cold: step.Cold, detail: step.Detail}
		}
	}
	if hydrated == nil {
		t.Fatal("step 2 vanished from the trajectory")
	}
	if hydrated.cold {
		t.Error("the row still says its body is not in memory after the read answered")
	}
	if len(hydrated.detail) == 0 {
		t.Error("the row carries no body after the read answered")
	}
	if got := strings.Join(hydrated.detail, "\n"); !strings.Contains(got, "package main") {
		t.Errorf("the row's body is not what the store returned: %q", got)
	}
}

// TestHydrateErrorLeavesTheRowAlone — a failed read does not make the row
// claim a body it does not have.
func TestHydrateErrorLeavesTheRowAlone(t *testing.T) {
	m := mkPanel(panelregistry.Debug, "/debug", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.viewport.height = 40
	m.setSessionMessages([]backend.Message{{Role: "user", Content: "read it"}})
	m.session.sup().RecordTrajectoryCalls([]convstore.Step{
		{Idx: 1, Type: convstore.StepUserInput, Text: "read it"},
		{Idx: 2, Type: convstore.StepToolCall, Status: 3, ToolName: "read_file"},
	})

	_, _ = m.Update(contextHydrateMsg{index: 2, err: errTestHydrate})

	for _, step := range m.contextTrajectoryState().Steps {
		if step.Index != 2 {
			continue
		}
		if !step.Cold {
			t.Error("a failed read made the row claim it has a body")
		}
	}
}

type hydrateErr struct{}

func (hydrateErr) Error() string { return "store unavailable" }

var errTestHydrate error = hydrateErr{}
