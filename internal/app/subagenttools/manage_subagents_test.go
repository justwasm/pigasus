package subagenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestManageSubagentsList pins the list action's contract.
func TestManageSubagentsList(t *testing.T) {
	tool := &manageSubagentsTool{}

	for _, action := range []string{"list", "List", "status"} {
		args, _ := json.Marshal(manageSubagentsArgs{Action: action})
		res, err := tool.Execute(context.Background(), string(args))
		if err != nil {
			t.Fatalf("%s: Execute returned err: %v", action, err)
		}
		want := "You have 0 active subagent(s):\n[]"
		if !strings.HasSuffix(res.Output, want) {
			t.Fatalf("%s: result = %q, want suffix %q", action, res.Output, want)
		}
		if strings.Contains(res.Output, "not yet implemented") {
			t.Fatalf("%s: stub marker leaked into the model's context: %q", action, res.Output)
		}
	}

	// The reference's declaration takes the capitalised spelling too.
	args, _ := json.Marshal(manageSubagentsArgs{ActionCap: "list"})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Action=list: Execute returned err: %v", err)
	}
	if !strings.HasSuffix(res.Output, "You have 0 active subagent(s):\n[]") {
		t.Fatalf("Action=list: result = %q", res.Output)
	}
}

// TestManageSubagentsKill pins the two measured kill texts.
func TestManageSubagentsKill(t *testing.T) {
	tool := &manageSubagentsTool{}

	// kill_all with nothing registered returns the 1.4.4 sentence.
	for _, action := range []string{"kill_all"} {
		args, _ := json.Marshal(manageSubagentsArgs{Action: action})
		res, err := tool.Execute(context.Background(), string(args))
		if err != nil {
			t.Fatalf("%s: Execute returned err: %v", action, err)
		}
		if !strings.HasSuffix(res.Output, "No active subagents to kill.") {
			t.Fatalf("%s: result = %q", action, res.Output)
		}
	}

	// kill with no ids → the reference's own sentence.
	args, _ := json.Marshal(manageSubagentsArgs{Action: "kill"})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("kill without ids: Execute returned err: %v", err)
	}
	wantNoIDs := `the "kill" action requires ConversationIds; run "list" to get the ids of the active subagents, ` +
		`or use "kill_all" to kill all of them`
	if !strings.HasSuffix(res.Output, wantNoIDs) {
		t.Fatalf("kill without ids: result = %q, want suffix %q", res.Output, wantNoIDs)
	}

	// kill naming an id → the per-id sentence, as a normal result.
	args, _ = json.Marshal(manageSubagentsArgs{ActionCap: "kill", ConversationIdsCap: []string{"abc-123", "def-456"}})
	res, err = tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("kill with ids: Execute returned err: %v", err)
	}
	want := `conversation "abc-123" is not an active subagent of this conversation, so nothing was killed; ` +
		`run the "list" action to see the active subagents and their ids`
	if !strings.HasSuffix(res.Output, want) {
		t.Fatalf("kill with ids: result = %q, want suffix %q", res.Output, want)
	}

	// An action outside the reference's enum is a caller-side error.
	if _, err := tool.Execute(context.Background(), `{"Action":"nope"}`); err == nil {
		t.Fatal("unknown action returned nil error")
	}
}

// TestSubagentTools_AgySchemaCompatibility tests that tools accept
// the reference Google agy parameters (TypeName, Role, Workspace, Recipient, ConversationIds).
func TestSubagentTools_AgySchemaCompatibility(t *testing.T) {
	// 1. send_message with Recipient and Message
	msgTool := &sendMessageTool{}
	msgJSON := `{"Recipient": "subagent-12345", "Message": "hello"}`
	res, err := msgTool.Execute(context.Background(), msgJSON)
	if err != nil {
		t.Fatalf("sendMessageTool.Execute: %v", err)
	}
	if !strings.HasSuffix(res.Output, `recipient "subagent-12345" not found`) {
		t.Fatalf("send_message = %q", res.Output)
	}

	// 2. manage_subagents with Action and ConversationIds
	mgrTool := &manageSubagentsTool{}
	mgrJSON := `{"Action": "kill", "ConversationIds": ["subagent-12345"]}`
	res, err = mgrTool.Execute(context.Background(), mgrJSON)
	if err != nil {
		t.Fatalf("manageSubagentsTool.Execute: %v", err)
	}
	if !strings.HasSuffix(res.Output,
		`conversation "subagent-12345" is not an active subagent of this conversation, so nothing was killed; `+
			`run the "list" action to see the active subagents and their ids`) {
		t.Fatalf("manage_subagents = %q", res.Output)
	}

	// 3. invoke_subagent with TypeName, Role, Workspace
	invTool := &invokeTool{deps: Deps{
		CWD: t.TempDir(),
	}}
	invJSON := `{"TypeName": "image-generator", "Role": "Artist", "Prompt": "cat drawing", "Workspace": "inherit"}`
	res, err = invTool.Execute(context.Background(), invJSON)
	if err != nil {
		t.Fatalf("invokeTool.Execute: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("invokeTool.Execute with image-generator failed: %+v", res)
	}
}
