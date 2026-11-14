package tuitools

import (
	"encoding/json"
	"testing"
)

// TestSubagentCallTitle pins the delegation's collapsed row, measured with
// tuialign 29 against agy 1.5.8:
//
//	● Agent(research: Weather Researcher)(查询当前上海的天气情况)
func TestSubagentCallTitle(t *testing.T) {
	args := json.RawMessage(`{"prompt":"查询当前上海的天气情况","type_name":"research","role":"Weather Researcher"}`)
	got := ToolStepTitle("invoke_subagent", args)
	want := "Agent(research: Weather Researcher)(查询当前上海的天气情况)"
	if got != want {
		t.Fatalf("invoke_subagent title = %q, want %q", got, want)
	}

	// A role-less delegation omits the colon.
	got = ToolStepTitle("invoke_subagent", json.RawMessage(`{"prompt":"look around","type_name":"research"}`))
	if got != "Agent(research)(look around)" {
		t.Errorf("role-less title = %q", got)
	}

	// The camel-case spelling the tool declaration accepts works too.
	got = ToolStepTitle("invoke_subagent", json.RawMessage(`{"Prompt":"look around","TypeName":"self","Role":"Tester"}`))
	if got != "Agent(self: Tester)(look around)" {
		t.Errorf("camel-case title = %q", got)
	}
}
