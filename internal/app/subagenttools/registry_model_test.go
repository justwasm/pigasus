package subagenttools

import "testing"

// TestStatesCarriesModelAndEffort pins the AGY 1.5.3 list entry: the
// registry's states carry the model (and effort) the child runs at, so
// the status-line JSON and the subagent rows can show them.
func TestStatesCarriesModelAndEffort(t *testing.T) {
	reg := NewRegistry("parent-1")
	child := NewChild("child-1", "research", "deep dive", t.TempDir(), func() {})
	child.Model = "Gemini 3.8 Flash (High)"
	child.Effort = "high"
	reg.Add(child)

	states := reg.States()
	if len(states) != 1 {
		t.Fatalf("states = %d, want 1", len(states))
	}
	if states[0].Model != "Gemini 3.8 Flash (High)" {
		t.Errorf("model = %q", states[0].Model)
	}
	if states[0].Effort != "high" {
		t.Errorf("effort = %q", states[0].Effort)
	}
}
