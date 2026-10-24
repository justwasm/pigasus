package printmode

// print_pig_subagent_question_test.go — cycle subagent-question-picker.
//
// The print `-p` path wires a headless subagentquestions Manager
// into the catalog so an ask_question call auto-skips with the
// SkipAnswer literal. This file pins:
//
//   - runPigPrintTurn (or its catalog wiring) registers AskQuestion
//     with a headless Manager
//   - a Dispatch through that catalog returns SkipAnswer
//   - the legacy no-Manager path still returns the cycle-214
//     noUserText reminder (regression guard)
//
// We exercise the catalog directly because driving a full PiG turn
// from a unit test is heavier than the wiring change warrants; the
// print path's runPigPrintTurn wraps this catalog and adds MCP /
// skills / observer plumbing around it.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/subagentquestions"
	"github.com/justwasm/pigasus/internal/tools"
)

// TestPrintPathAskQuestionHeadlessAutoSkips drives the tool through
// a fresh headless Manager and confirms it returns SkipAnswer
// without anyone calling Answer — the AGY changelog line "Headless
// -p runs skip these questions so the subagent can continue."
func TestPrintPathAskQuestionHeadlessAutoSkips(t *testing.T) {
	reg := tools.Catalog(t.TempDir())
	reg.Register(&tools.AskQuestion{
		SubAgentQuestions: subagentquestions.NewManager(true),
	})

	out, err := reg.Dispatch(
		context.Background(),
		"ask_question",
		json.RawMessage(`{"questions":[{"question":"Pick a tool","options":["go","node"]}]}`),
	)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if out != subagentquestions.SkipAnswer {
		t.Errorf("headless ask_question = %q, want %q", out,
			subagentquestions.SkipAnswer)
	}
}

// TestPrintPathAskQuestionKeepsLegacyNoManager pins the
// backward-compat behaviour: when the catalog does NOT have a
// subagentquestions Manager wired, ask_question keeps the
// cycle-214 noUserText reminder (the changelog line's "subagent was
// left waiting forever" bug is exactly what the headless path now
// replaces).
func TestPrintPathAskQuestionKeepsLegacyNoManager(t *testing.T) {
	reg := tools.Catalog(t.TempDir())
	// No AskQuestion re-register — the catalog's default
	// AskQuestion is the legacy noManager path.

	out, err := reg.Dispatch(
		context.Background(),
		"ask_question",
		json.RawMessage(`{"questions":[{"question":"Pick a tool"}]}`),
	)
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if !strings.Contains(out, "cannot be asked") {
		t.Errorf("legacy ask_question = %q, want noUserText", out)
	}
}
