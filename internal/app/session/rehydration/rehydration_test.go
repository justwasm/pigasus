package rehydration

import (
	"encoding/json"
	"testing"
)

// TestRehydrateRoutesByToolName pins the dispatch: a tool call is routed
// to the rehydrator registered under the name the model called, and the
// delta keeps the step it came from.
//
// 【反编译】 the reference's dispatch is by tool name too
// (`rehydrateToolCall`, LS 0x100f3f6d0): the table is a chain of
// name comparisons, one per tool.
func TestRehydrateRoutesByToolName(t *testing.T) {
	d := New(
		Tool{
			Name: "alpha",
			Rehydrate: func(args json.RawMessage) (map[string]string, bool) {
				return map[string]string{"seen": string(args)}, true
			},
			Apply: func(meta map[string]string) bool { return true },
		},
		Tool{
			Name: "beta",
			Rehydrate: func(json.RawMessage) (map[string]string, bool) {
				return nil, false
			},
		},
	)
	deltas := d.Rehydrate([]Step{
		{Idx: 3, Type: StepToolCall, ToolName: "alpha", ToolArgs: `{"x":1}`, Status: StatusCompleted},
		{Idx: 4, Type: StepToolCall, ToolName: "beta", ToolArgs: `{}`, Status: StatusCompleted},
		{Idx: 5, Type: StepToolCall, ToolName: "gamma", ToolArgs: `{}`, Status: StatusCompleted},
	})
	if len(deltas) != 1 {
		t.Fatalf("deltas = %d, want 1 (beta reports false, gamma is unknown)", len(deltas))
	}
	if deltas[0].Idx != 3 || deltas[0].ToolName != "alpha" {
		t.Fatalf("delta = %+v, want the step 3 alpha call", deltas[0])
	}
	if got := deltas[0].Metadata.Get("seen"); got != `{"x":1}` {
		t.Fatalf("metadata[seen] = %q, want the raw arguments", got)
	}
}

// TestRehydrateSkipsFailedSteps pins the status filter: a tool call that
// failed or was cancelled changed nothing, so its delta is not applied.
//
// 【一手】 every step in the reference's conversations carries status 3;
// the clone's own writer writes 3 for a finished step too.
func TestRehydrateSkipsFailedSteps(t *testing.T) {
	d := New(Tool{
		Name: "alpha",
		Rehydrate: func(json.RawMessage) (map[string]string, bool) {
			return map[string]string{"k": "v"}, true
		},
	})
	for _, status := range []int64{StatusError, StatusFailed, StatusCancelled} {
		deltas := d.Rehydrate([]Step{{Type: StepToolCall, ToolName: "alpha", Status: status}})
		if len(deltas) != 0 {
			t.Fatalf("status %d: deltas = %d, want 0", status, len(deltas))
		}
	}
	deltas := d.Rehydrate([]Step{{Type: StepToolCall, ToolName: "alpha", Status: StatusCompleted}})
	if len(deltas) != 1 {
		t.Fatalf("status %d: deltas = %d, want 1", StatusCompleted, len(deltas))
	}
}

// TestSucceededStatusMapping pins the status table: a step that ran to
// completion succeeded, and a step that errored, failed or was
// cancelled did not.
//
// Cycle 148 captured this against the v1.3.6 reference using the
// resumed conversation 4638a7b0-e5f4-4961-80c2-d61499f06050.db (390
// rows with status=4, 4 with status=6, 21 with status=7). The host
// reads the same table to render the "(Failed)" suffix of a loaded
// tool step.
func TestSucceededStatusMapping(t *testing.T) {
	cases := []struct {
		status int64
		want   bool
	}{
		{StatusCompleted, true},  // success
		{StatusError, false},     // failed (errored)
		{5, true},                // completed-skipped / alternate success
		{StatusFailed, false},    // rare
		{StatusCancelled, false}, // rare
		{0, true},                // unknown
	}
	for _, c := range cases {
		if got := Succeeded(c.status); got != c.want {
			t.Errorf("Succeeded(status=%d) = %v, want %v", c.status, got, c.want)
		}
	}
}

// TestRehydrateSkipsNonToolCallSteps pins the type filter.
func TestRehydrateSkipsNonToolCallSteps(t *testing.T) {
	d := New(Tool{
		Name: "alpha",
		Rehydrate: func(json.RawMessage) (map[string]string, bool) {
			return map[string]string{"k": "v"}, true
		},
	})
	deltas := d.Rehydrate([]Step{{Type: 15, ToolName: "alpha", Status: StatusCompleted}})
	if len(deltas) != 0 {
		t.Fatalf("deltas = %d, want 0 for a model-response step", len(deltas))
	}
	// A step with no type but a tool name is treated as a tool call:
	// the clone's own writer may not set the type.
	deltas = d.Rehydrate([]Step{{ToolName: "alpha", Status: StatusCompleted}})
	if len(deltas) != 1 {
		t.Fatalf("deltas = %d, want 1 for an untyped tool call", len(deltas))
	}
}

// TestApplyCountsChangedDeltas pins the applier contract.
func TestApplyCountsChangedDeltas(t *testing.T) {
	d := New(Tool{
		Name:      "alpha",
		Rehydrate: func(json.RawMessage) (map[string]string, bool) { return map[string]string{"k": "v"}, true },
		Apply:     func(map[string]string) bool { return true },
	}, Tool{
		Name:      "beta",
		Rehydrate: func(json.RawMessage) (map[string]string, bool) { return map[string]string{"k": "v"}, true },
		Apply:     func(map[string]string) bool { return false },
	})
	deltas := d.Rehydrate([]Step{
		{Type: StepToolCall, ToolName: "alpha", Status: StatusCompleted},
		{Type: StepToolCall, ToolName: "beta", Status: StatusCompleted},
	})
	if got := d.Apply(deltas); got != 1 {
		t.Fatalf("Apply = %d, want 1", got)
	}
}

// TestEnabledKillSwitch pins the kill switch.
//
// 【反编译】 the reference gates the same pass behind the boolean flag
// `cascade-disable-rehydration` (CLI `shouldDisableRehydration`,
// 0x101f29860). The clone has no flag service, so the switch is an
// environment variable.
func TestEnabledKillSwitch(t *testing.T) {
	t.Setenv(DisableEnvVar, "")
	if !Enabled() {
		t.Fatalf("Enabled() = false with the switch unset, want true")
	}
	for _, v := range []string{"0", "false", "off", "no", "FALSE"} {
		t.Setenv(DisableEnvVar, v)
		if Enabled() {
			t.Fatalf("Enabled() = true with %s=%q, want false", DisableEnvVar, v)
		}
	}
	t.Setenv(DisableEnvVar, "1")
	if !Enabled() {
		t.Fatalf("Enabled() = false with %s=1, want true", DisableEnvVar)
	}
}

// TestTrajectoryIndexHelpers pins the trajectory view's index helpers.
func TestTrajectoryIndexHelpers(t *testing.T) {
	tr := NewTrajectory([]Step{
		{Idx: 0, Type: 15, Status: StatusCompleted},
		{Idx: 1, Type: StepToolCall, ToolName: "run_command", Status: StatusCompleted},
		{Idx: 2, Type: StepToolCall, ToolName: "read_file", Status: StatusError},
		{Idx: 3, Type: StepToolCall, ToolName: "edit_file", Status: StatusCompleted},
	})
	if tr.NumSteps() != 4 {
		t.Fatalf("NumSteps() = %d, want 4", tr.NumSteps())
	}
	if idx := tr.StepIndicesByType(StepToolCall); len(idx) != 3 || idx[0] != 1 || idx[2] != 3 {
		t.Fatalf("StepIndicesByType = %v, want [1 2 3]", idx)
	}
	if idx := tr.StepIndicesExcludingStatuses(StatusError); len(idx) != 3 || idx[0] != 0 || idx[2] != 3 {
		t.Fatalf("StepIndicesExcludingStatuses = %v, want [0 1 3]", idx)
	}
	calls := tr.ToolCallSteps()
	if len(calls) != 3 || calls[0].ToolName != "run_command" {
		t.Fatalf("ToolCallSteps = %+v, want the three tool calls", calls)
	}
	if _, ok := tr.Step(9); ok {
		t.Fatalf("Step(9) reported ok, want not found")
	}
	if got := tr.StepsSlice(1, 3); len(got) != 2 || got[0].ToolName != "run_command" {
		t.Fatalf("StepsSlice(1,3) = %+v, want two steps", got)
	}
}
