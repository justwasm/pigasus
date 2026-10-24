package subagenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	pi "github.com/MichaelKinsy/PiG/ai"
)

// TestManageSubagentsList pins the list action's contract. The clone has no
// subagent registry — delegations are synchronous, so no subagent is ever live
// when the parent runs a tool — and the reference formats the marshalled state
// slice after the count (【反编译】 .list @0x101d5fff0). The empty slice
// marshals to "[]".
func TestManageSubagentsList(t *testing.T) {
	tool := &manageSubagentsTool{}

	for _, action := range []string{"list", "List", "status"} {
		args, _ := json.Marshal(manageSubagentsArgs{Action: action})
		res, err := tool.Execute(context.Background(), "call-mgr-list", args, nil)
		if err != nil {
			t.Fatalf("%s: Execute returned err: %v", action, err)
		}
		if res.IsError {
			t.Fatalf("%s: Execute returned IsError=true, want a normal result", action)
		}
		body := res.Content[0].(pi.TextContent)
		want := "You have 0 active subagent(s):\n[]"
		if !strings.HasSuffix(body.Text, want) {
			t.Fatalf("%s: result = %q, want suffix %q", action, body.Text, want)
		}
		if strings.Contains(body.Text, "not yet implemented") {
			t.Fatalf("%s: stub marker leaked into the model's context: %q", action, body.Text)
		}
	}

	// The reference's declaration takes the capitalised spelling too.
	args, _ := json.Marshal(manageSubagentsArgs{ActionCap: "list"})
	res, err := tool.Execute(context.Background(), "call-mgr-list-cap", args, nil)
	if err != nil {
		t.Fatalf("Action=list: Execute returned err: %v", err)
	}
	if body := res.Content[0].(pi.TextContent); !strings.HasSuffix(body.Text, "You have 0 active subagent(s):\n[]") {
		t.Fatalf("Action=list: result = %q", body.Text)
	}
}

// TestManageSubagentsKill pins the two measured kill texts.
//
// kill_all with nothing registered returns the 【一手】 1.4.4 sentence. kill
// naming a conversation id returns the 【反编译】 per-id sentence, which points
// the model at the list action — the clone never has a live id to kill, so
// that is the only reachable kill outcome.
func TestManageSubagentsKill(t *testing.T) {
	tool := &manageSubagentsTool{}

	// kill_all / kill with no ids → the empty-registry text.
	for _, action := range []string{"kill_all", "kill"} {
		args, _ := json.Marshal(manageSubagentsArgs{Action: action})
		res, err := tool.Execute(context.Background(), "call-mgr-kill", args, nil)
		if err != nil {
			t.Fatalf("%s: Execute returned err: %v", action, err)
		}
		if res.IsError {
			t.Fatalf("%s: Execute returned IsError=true, want a normal result", action)
		}
		body := res.Content[0].(pi.TextContent)
		if !strings.HasSuffix(body.Text, "No active subagents to kill.") {
			t.Fatalf("%s: result = %q", action, body.Text)
		}
	}

	// kill naming an id → the per-id sentence, as a normal result.
	args, _ := json.Marshal(manageSubagentsArgs{ActionCap: "kill", ConversationIdsCap: []string{"abc-123", "def-456"}})
	res, err := tool.Execute(context.Background(), "call-mgr-kill-id", args, nil)
	if err != nil {
		t.Fatalf("kill with ids: Execute returned err: %v", err)
	}
	if res.IsError {
		t.Fatalf("kill with ids: Execute returned IsError=true, want a normal result")
	}
	want := `conversation "abc-123" is not an active subagent of this conversation, so nothing was killed; ` +
		`run the "list" action to see the active subagents and their ids`
	if body := res.Content[0].(pi.TextContent); !strings.HasSuffix(body.Text, want) {
		t.Fatalf("kill with ids: result = %q, want suffix %q", body.Text, want)
	}

	// An action outside the reference's enum is a caller-side error.
	if _, err := tool.Execute(context.Background(), "call-mgr-bad", json.RawMessage(`{"Action":"nope"}`), nil); err == nil {
		t.Fatal("unknown action returned nil error")
	}
}

// TestSubagentTools_AgySchemaCompatibility tests that tools accept
// the reference Google agy parameters (TypeName, Role, Workspace, Recipient, ConversationIds).
func TestSubagentTools_AgySchemaCompatibility(t *testing.T) {
	// 1. send_message with Recipient and Message
	msgTool := &sendMessageTool{}
	msgJSON := []byte(`{"Recipient": "subagent-12345", "Message": "hello"}`)
	res, err := msgTool.Execute(context.Background(), "call-1", msgJSON, nil)
	if err != nil {
		t.Fatalf("sendMessageTool.Execute: %v", err)
	}
	if got := res.Content[0].(pi.TextContent).Text; !strings.HasSuffix(got, `recipient "subagent-12345" not found`) {
		t.Fatalf("send_message = %q", got)
	}

	// 2. manage_subagents with Action and ConversationIds
	mgrTool := &manageSubagentsTool{}
	mgrJSON := []byte(`{"Action": "kill", "ConversationIds": ["subagent-12345"]}`)
	res, err = mgrTool.Execute(context.Background(), "call-2", mgrJSON, nil)
	if err != nil {
		t.Fatalf("manageSubagentsTool.Execute: %v", err)
	}
	if got := res.Content[0].(pi.TextContent).Text; !strings.HasSuffix(got,
		`conversation "subagent-12345" is not an active subagent of this conversation, so nothing was killed; `+
			`run the "list" action to see the active subagents and their ids`) {
		t.Fatalf("manage_subagents = %q", got)
	}

	// 3. invoke_subagent with TypeName, Role, Workspace
	invTool := &invokeTool{deps: Deps{
		Services: subagentTestServices(t),
		Model:    subagentTestModel(),
		CWD:      t.TempDir(),
	}}
	invJSON := []byte(`{"TypeName": "image-generator", "Role": "Artist", "Prompt": "cat drawing", "Workspace": "inherit"}`)
	res, err = invTool.Execute(context.Background(), "call-3", invJSON, nil)
	if err != nil {
		t.Fatalf("invokeTool.Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("invokeTool.Execute with image-generator failed: %+v", res)
	}
}
