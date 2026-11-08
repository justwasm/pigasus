package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

func TestSlashGoalLifecycleAndContinuation(t *testing.T) {
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", t.TempDir(), true)

	// 1. Bare /goal initializes goal and sends prompt
	handled, cmd := m.slashGoal("")
	if !handled || cmd == nil {
		t.Errorf("bare /goal should return handled=true, cmd != nil")
	}
	if m.session.activeGoal == nil {
		t.Errorf("bare /goal should set activeGoal")
	}

	// 2. /goal with instruction initializes active goal
	handled, cmd = m.slashGoal("implement distributed consensus")
	if !handled || cmd == nil {
		t.Errorf("/goal with instruction should return cmd != nil")
	}
	if m.session.activeGoal == nil {
		t.Fatalf("expected m.session.activeGoal to be non-nil")
	}
	if m.session.activeGoal.GoalText != "implement distributed consensus" {
		t.Errorf("activeGoal.GoalText = %q, want 'implement distributed consensus'", m.session.activeGoal.GoalText)
	}
	if m.session.activeGoal.ContinuationCount != 0 {
		t.Errorf("activeGoal.ContinuationCount = %d, want 0", m.session.activeGoal.ContinuationCount)
	}

	// 3. First turn completion without completion token -> continues goal
	msg := toolsteps.TurnDoneMsg{
		Reply: "I have started working on Raft leader election...",
	}
	_, nextCmd := m.updateTurnDoneMsg(msg)
	if nextCmd == nil {
		t.Errorf("expected nextCmd to trigger continuation")
	}
	if m.session.activeGoal == nil {
		t.Fatalf("goal should remain active")
	}
	if m.session.activeGoal.ContinuationCount != 1 {
		t.Errorf("activeGoal.ContinuationCount = %d, want 1", m.session.activeGoal.ContinuationCount)
	}

	// 4. Second turn completion with <!-- GOAL_COMPLETE --> -> finishes goal
	msgDone := toolsteps.TurnDoneMsg{
		Reply: "Consensus module is fully tested and verified.\n<!-- GOAL_COMPLETE -->",
	}
	m.updateTurnDoneMsg(msgDone)
	if m.session.activeGoal != nil {
		t.Errorf("activeGoal should be cleared after completion, got %+v", m.session.activeGoal)
	}

	// 5. Start new goal and cancel with <!-- GOAL_CANCELLED -->
	m.slashGoal("another task")
	if m.session.activeGoal == nil {
		t.Fatalf("expected activeGoal to be set")
	}
	msgCancel := toolsteps.TurnDoneMsg{
		Reply: "Cannot continue due to requirement conflicts.\n<!-- GOAL_CANCELLED -->",
	}
	m.updateTurnDoneMsg(msgCancel)
	if m.session.activeGoal != nil {
		t.Errorf("activeGoal should be cleared after cancellation, got %+v", m.session.activeGoal)
	}
}
