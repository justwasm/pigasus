package slashcommands

import (
	"testing"
)

func TestFilterWithDynamic_Empty(t *testing.T) {
	cmds := FilterWithDynamic("/model", nil)
	if len(cmds) != 1 || cmds[0].Name != "/model" {
		t.Fatalf("expected 1 command /model, got: %+v", cmds)
	}
}

func TestFilterWithDynamic_MergeAndCollision(t *testing.T) {
	dynamic := []Command{
		{Name: "review", Description: "Review code changes"},
		{Name: "compact", Description: "Agent-specific compact"},
	}

	// Unfiltered: contains builtin commands + dynamic commands
	all := FilterWithDynamic("", dynamic)

	foundReview := false
	foundAgentCompact := false
	foundBuiltinCompact := false

	for _, c := range all {
		if c.Name == "/review" {
			foundReview = true
			if c.Source != SourceAgent || c.Badge != "agent" {
				t.Errorf("expected /review to have SourceAgent and badge 'agent', got %+v", c)
			}
		}
		if c.Name == "/agent compact" {
			foundAgentCompact = true
			if c.Source != SourceAgent || c.Badge != "agent" {
				t.Errorf("expected /agent compact to have SourceAgent, got %+v", c)
			}
		}
		if c.Name == "/compact" {
			foundBuiltinCompact = true
			if c.Source != SourceBuiltin {
				t.Errorf("expected /compact to have SourceBuiltin, got %+v", c)
			}
		}
	}

	if !foundReview {
		t.Errorf("expected /review to be found in dynamic merge")
	}
	if !foundAgentCompact {
		t.Errorf("expected /agent compact to be namespaced due to collision with builtin /compact")
	}
	if !foundBuiltinCompact {
		t.Errorf("expected builtin /compact to be preserved")
	}
}
