package tuitools

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

func TestToolStepTitle_FileRelativePaths(t *testing.T) {
	ws := "/mock/workspace/openagy"
	tuilib.SetWorkspaceRoot(ws)
	defer tuilib.SetWorkspaceRoot("")

	// Read file within workspace
	readArgs := json.RawMessage(`{"Path":"/mock/workspace/openagy/internal/tui/bridge.go"}`)
	if got := ToolStepTitle("read_file", readArgs); got != "Read(internal/tui/bridge.go)" {
		t.Errorf("read_file title = %q, want 'Read(internal/tui/bridge.go)'", got)
	}

	// Create file within workspace
	createArgs := json.RawMessage(`{"Path":"/mock/workspace/openagy/pkg/newfile.go"}`)
	if got := ToolStepTitle("write_file", createArgs); got != "Create(pkg/newfile.go)" {
		t.Errorf("write_file title = %q, want 'Create(pkg/newfile.go)'", got)
	}

	// Edit file within workspace
	editArgs := json.RawMessage(`{"Path":"/mock/workspace/openagy/docs/guide.md"}`)
	if got := ToolStepTitle("edit_file", editArgs); got != "Edit(docs/guide.md)" {
		t.Errorf("edit_file title = %q, want 'Edit(docs/guide.md)'", got)
	}

	// List dir within workspace
	listArgs := json.RawMessage(`{"path":"/mock/workspace/openagy/internal/ui"}`)
	if got := ToolStepTitle("list_dir", listArgs); got != "Listed internal/ui" {
		t.Errorf("list_dir title = %q, want 'Listed internal/ui'", got)
	}

	// Explored / find in workspace
	findArgs := json.RawMessage(`{"SearchPath":"/mock/workspace/openagy/internal"}`)
	if got := ToolStepTitle("find_by_name", findArgs); got != "Explored internal" {
		t.Errorf("find_by_name title = %q, want 'Explored internal'", got)
	}
}

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
