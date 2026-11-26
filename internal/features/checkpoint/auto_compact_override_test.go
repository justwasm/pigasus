package checkpoint

import (
	"testing"

	"github.com/justwasm/pigasus/actor"
)

func TestAutoCompactOverridePolicy(t *testing.T) {
	t.Setenv("AGY_CLI_AUTO_COMPACT_OVERRIDE", "800K")
	p := New(Config{Model: "gemini-3.8-flash-high"})
	if got := p.Profile().TokenThreshold; got != 800_000 {
		t.Fatalf("Profile().TokenThreshold = %d, want 800000", got)
	}
	if got := p.Limit().ContextWindow; got != 1_048_576 {
		t.Fatalf("Limit().ContextWindow = %d, want 1048576", got)
	}

	probe := actor.CheckpointProbe{
		LastStep: &actor.Step{
			ToolCalls: []actor.ToolCall{{ID: "1", Name: "read_file"}},
		},
	}
	// Below 800K (even above the default 256K checkpoint limit): must NOT trigger.
	if p.hard(500_000) {
		t.Fatalf("hard(500000) = true, want false under 800K override")
	}
	if p.should(probe, 500_000, false) {
		t.Fatalf("should(500000) = true, want false under 800K override")
	}
	// At 800K: soft threshold triggers.
	if !p.should(probe, 800_000, false) {
		t.Fatalf("should(800000) = false, want true under 800K override")
	}

	// Percentage override on Claude (250k published window -> 80% = 200k).
	t.Setenv("AGY_CLI_AUTO_COMPACT_OVERRIDE", "80%")
	pClaude := New(Config{Model: "claude-sonnet-4-6"})
	if got := pClaude.Profile().TokenThreshold; got != 200_000 {
		t.Fatalf("Claude Profile().TokenThreshold = %d, want 200000", got)
	}
	if pClaude.should(probe, 199_999, false) {
		t.Fatalf("Claude should(199999) = true, want false")
	}
	if !pClaude.should(probe, 200_000, false) {
		t.Fatalf("Claude should(200000) = false, want true")
	}
}
