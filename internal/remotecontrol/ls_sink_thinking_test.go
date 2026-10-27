package remotecontrol

import (
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
)

// TestTheThinkingSurvivesAReload checks that a turn's thinking reaches
// the store and comes back as the step the Web UI renders.
//
// 【一手】 the reference's own stored model-response blob carries the
// thinking and the time it took — its planner-response message reads
// back with the answer, the reasoning and a "6.773079s" duration — and
// the Web UI's collapsible "Thought for Ns" panel is rendered from the
// last two. A stored step without them shows the answer alone once the
// page is reloaded and the history comes from the store.
func TestTheThinkingSurvivesAReload(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	traj := newLSTrajectory("conv-thinking")
	sink := newLSTrajectorySink(traj, nil)

	sink.OnThought(nil, "The user asks why the sky is blue.")
	sink.OnTextDelta(nil, "Because of Rayleigh scattering.")
	sink.Finish(MessageResult{Reply: "Because of Rayleigh scattering."}, 3*time.Second)

	// The live step carries the thinking and the duration: the panel
	// opens while the turn runs.
	live := traj.Snapshot().Steps
	if len(live) != 1 {
		t.Fatalf("live steps = %d, want 1", len(live))
	}
	planner, ok := live[0]["plannerResponse"].(map[string]any)
	if !ok {
		t.Fatalf("live step has no plannerResponse: %v", live[0])
	}
	if planner["thinking"] != "The user asks why the sky is blue." {
		t.Errorf("live thinking = %v", planner["thinking"])
	}
	if planner["thinkingDuration"] != "3s" {
		t.Errorf("live thinkingDuration = %v, want %q", planner["thinkingDuration"], "3s")
	}

	// The stored step carries them too.
	stored := sink.StoredSteps()
	if len(stored) != 1 {
		t.Fatalf("stored steps = %d, want 1", len(stored))
	}
	if stored[0].Thinking != "The user asks why the sky is blue." {
		t.Errorf("stored thinking = %q", stored[0].Thinking)
	}
	if stored[0].ThinkingDuration != 3*time.Second {
		t.Errorf("stored thinking duration = %v, want %v", stored[0].ThinkingDuration, 3*time.Second)
	}

	// And the store gives them back the way the reload path reads them.
	if err := convstore.AppendConversationSteps("conv-thinking", "/workspace/openagy", stored); err != nil {
		t.Fatalf("AppendConversationSteps: %v", err)
	}
	reloaded := lsStoredSteps("conv-thinking")
	if len(reloaded) != 1 {
		t.Fatalf("reloaded steps = %d, want 1", len(reloaded))
	}
	planner, ok = reloaded[0]["plannerResponse"].(map[string]any)
	if !ok {
		t.Fatalf("reloaded step has no plannerResponse: %v", reloaded[0])
	}
	if planner["response"] != "Because of Rayleigh scattering." {
		t.Errorf("reloaded response = %v", planner["response"])
	}
	if planner["thinking"] != "The user asks why the sky is blue." {
		t.Errorf("reloaded thinking = %v", planner["thinking"])
	}
	if planner["thinkingDuration"] != "3s" {
		t.Errorf("reloaded thinkingDuration = %v, want %q", planner["thinkingDuration"], "3s")
	}
}
