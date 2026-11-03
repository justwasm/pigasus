package subagenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
)

// TestRehydrateDefineSubagent pins the delta of a define_subagent call.
//
// 【一手】 the reference stores the call as
// {"Name","Description","SystemPrompt","EnableWriteTools",…} — the
// capitalised native-payload names. The model-facing spelling is the
// lower-case one, and both must produce the same delta.
func TestRehydrateDefineSubagent(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{
			// The reference's stored form.
			name: "stored",
			args: `{"Name":"viewer","Description":"Viewport work","SystemPrompt":"You are a Go developer.","EnableWriteTools":true,"EnableMCPTools":false}`,
		},
		{
			// The model-facing form.
			name: "model",
			args: `{"name":"viewer","description":"Viewport work","system_prompt":"You are a Go developer.","enable_write_tools":true,"enable_mcp_tools":false}`,
		},
	}
	for _, tc := range cases {
		meta, ok := RehydrateDefineSubagent(json.RawMessage(tc.args))
		if !ok {
			t.Fatalf("%s: RehydrateDefineSubagent reported false", tc.name)
		}
		if meta["name"] != "viewer" || meta["Name"] != "viewer" {
			t.Fatalf("%s: name = %q / %q, want viewer", tc.name, meta["name"], meta["Name"])
		}
		if meta["system_prompt"] != "You are a Go developer." ||
			meta["SystemPrompt"] != "You are a Go developer." {
			t.Fatalf("%s: system prompt = %q / %q", tc.name, meta["system_prompt"], meta["SystemPrompt"])
		}
		if meta["description"] != "Viewport work" || meta["Description"] != "Viewport work" {
			t.Fatalf("%s: description = %q / %q", tc.name, meta["description"], meta["Description"])
		}
		if meta["EnableWriteTools"] != "true" || meta["enable_write_tools"] != "true" {
			t.Fatalf("%s: write grant = %q / %q, want true", tc.name,
				meta["EnableWriteTools"], meta["enable_write_tools"])
		}
		if meta["EnableMCPTools"] != "false" {
			t.Fatalf("%s: MCP grant = %q, want false", tc.name, meta["EnableMCPTools"])
		}
		if _, ok := meta["EnableSubagentTools"]; ok {
			t.Fatalf("%s: an unset grant must not appear in the delta: %v", tc.name, meta)
		}
	}
}

// TestRehydrateDefineSubagentRejects pins the arguments that do not
// describe a definition.
func TestRehydrateDefineSubagentRejects(t *testing.T) {
	for _, args := range []string{``, `{`, `{"Name":"only-a-name"}`, `{"SystemPrompt":"only-a-prompt"}`} {
		if _, ok := RehydrateDefineSubagent(json.RawMessage(args)); ok {
			t.Fatalf("RehydrateDefineSubagent(%q) reported ok, want false", args)
		}
	}
}

// TestRestoreDefinition pins the restore: a rehydrated definition comes
// back as a usable definition, the same one the live handler stores.
func TestRestoreDefinition(t *testing.T) {
	store := NewStore()
	meta, ok := RehydrateDefineSubagent(json.RawMessage(
		`{"Name":"viewer","SystemPrompt":"You are a Go developer."}`))
	if !ok {
		t.Fatalf("RehydrateDefineSubagent reported false")
	}
	if !store.RestoreDefinition(meta) {
		t.Fatalf("RestoreDefinition reported false")
	}
	def, ok := store.Get("viewer")
	if !ok {
		t.Fatalf("the definition was not restored: %v", store.Names())
	}
	if def.SystemPrompt != "You are a Go developer." || def.Prompt != "You are a Go developer." {
		t.Fatalf("restored definition = %+v, want the system prompt as prompt and system prompt", def)
	}
	// A delta without a name or a prompt restores nothing.
	if store.RestoreDefinition(map[string]string{}) {
		t.Fatalf("RestoreDefinition(empty) reported true, want false")
	}
}

// TestRehydrateManageSubagents pins the delta of a manage_subagents call.
//
// 【一手】 the reference stores it as {"Action":"kill","ConversationIds":[…]}.
// The killed ids are joined with a comma, and the killed list is
// JSON-marshalled.
func TestRehydrateManageSubagents(t *testing.T) {
	meta, ok := RehydrateManageSubagents(json.RawMessage(
		`{"Action":"kill","ConversationIds":["aaa","bbb"]}`))
	if !ok {
		t.Fatalf("RehydrateManageSubagents reported false")
	}
	if meta["action"] != "kill" {
		t.Fatalf("action = %q, want kill", meta["action"])
	}
	if meta["conversation_ids"] != "aaa,bbb" {
		t.Fatalf("conversation_ids = %q, want %q", meta["conversation_ids"], "aaa,bbb")
	}
	if meta["killed_conversation_ids"] != `["aaa","bbb"]` {
		t.Fatalf("killed_conversation_ids = %q", meta["killed_conversation_ids"])
	}
	// A list action kills nothing.
	meta, ok = RehydrateManageSubagents(json.RawMessage(`{"Action":"list"}`))
	if !ok {
		t.Fatalf("RehydrateManageSubagents(list) reported false")
	}
	if _, ok := meta["killed_conversation_ids"]; ok {
		t.Fatalf("a list action must not carry killed ids: %v", meta)
	}
}

// TestRestoreManageSubagents pins the restore of the killed set.
func TestRestoreManageSubagents(t *testing.T) {
	store := NewStore()
	meta, ok := RehydrateManageSubagents(json.RawMessage(
		`{"Action":"kill","ConversationIds":["aaa"]}`))
	if !ok {
		t.Fatalf("RehydrateManageSubagents reported false")
	}
	if !store.RestoreManageSubagents(meta) {
		t.Fatalf("RestoreManageSubagents reported false")
	}
	if !store.Killed("aaa") {
		t.Fatalf("the killed conversation id was not recorded")
	}
	if store.Killed("bbb") {
		t.Fatalf("an unrelated conversation id was recorded as killed")
	}
	if store.RestoreManageSubagents(map[string]string{}) {
		t.Fatalf("RestoreManageSubagents(empty) reported true, want false")
	}
}

// TestRehydrateSendMessage pins the delta of a send_message call.
//
// 【一手】 the reference stores it as {"Recipient":"…","Message":"…"}.
func TestRehydrateSendMessage(t *testing.T) {
	meta, ok := RehydrateSendMessage(json.RawMessage(
		`{"Recipient":"conv-1","Message":"track five"}`))
	if !ok {
		t.Fatalf("RehydrateSendMessage reported false")
	}
	if meta["recipient"] != "conv-1" {
		t.Fatalf("recipient = %q, want conv-1", meta["recipient"])
	}
	if meta["Message"] != "track five" {
		t.Fatalf("Message = %q, want %q", meta["Message"], "track five")
	}
	if _, ok := RehydrateSendMessage(json.RawMessage(`{"Message":"no recipient"}`)); ok {
		t.Fatalf("RehydrateSendMessage without a recipient reported ok, want false")
	}
}

// TestRestoreSendMessageHasNothingToRestore pins the honest no-op: the
// clone's delegations are synchronous, so there is no live recipient and
// no pending delivery to restore.
func TestRestoreSendMessageHasNothingToRestore(t *testing.T) {
	store := NewStore()
	if store.RestoreSendMessage(map[string]string{"recipient": "conv-1"}) {
		t.Fatalf("RestoreSendMessage reported true, want false")
	}
}

// TestInvokeSubagentFindsARestoredDefinition pins that a rehydrated
// definition is usable: the tool set looks it up by name, so a resumed
// conversation can call the subagent it defined.
//
// The reference derives the definition from the trajectory when the
// conversation is loaded (【反编译】 the reference's
// `cortex/rehydration` package); a definition that came back but was not
// wired into the tool set would be no better than one that did not come
// back at all.
func TestInvokeSubagentFindsARestoredDefinition(t *testing.T) {
	store := NewStore()
	meta, ok := RehydrateDefineSubagent(json.RawMessage(
		`{"Name":"viewer","SystemPrompt":"You are a Go developer."}`))
	if !ok {
		t.Fatalf("RehydrateDefineSubagent reported false")
	}
	if !store.RestoreDefinition(meta) {
		t.Fatalf("RestoreDefinition reported false")
	}
	tools := Tools(Deps{
		Services:    subagentTestServices(t),
		Model:       subagentTestModel(),
		CWD:         t.TempDir(),
		Definitions: store,
	})
	var invoke piagent.AgentTool
	for _, tool := range tools {
		if tool.Name() == "invoke_subagent" {
			invoke = tool
		}
	}
	if invoke == nil {
		t.Fatalf("the tool set has no invoke_subagent")
	}
	res, err := invoke.Execute(context.Background(), "call-inv",
		json.RawMessage(`{"TypeName":"viewer","Prompt":"Say OK."}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Content[0].(pi.TextContent).Text
	if strings.Contains(got, "not found or not allowed to be invoked") {
		t.Fatalf("the rehydrated definition was not found: %q", got)
	}
}
