package slashactions

import (
	"testing"
)

func TestPromptGenerators(t *testing.T) {
	if PlanPrompt() == "" {
		t.Error("PlanPrompt should not be empty")
	}
	if GoalPrompt() == "" {
		t.Error("GoalPrompt should not be empty")
	}
	if LearnPrompt() == "" {
		t.Error("LearnPrompt should not be empty")
	}
	if TeamworkPrompt() == "" {
		t.Error("TeamworkPrompt should not be empty")
	}
	if GrillMePrompt() == "" {
		t.Error("GrillMePrompt should not be empty")
	}
	if SchedulePrompt() == "" {
		t.Error("SchedulePrompt should not be empty")
	}
}
