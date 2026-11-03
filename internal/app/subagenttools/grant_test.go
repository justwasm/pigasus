// grant_test.go — the cycle-349 tests for the subagent authorization model.
//
// The reference's define_subagent carries three switches — EnableWriteTools,
// EnableMCPTools, EnableSubagentTools — and one sentence that fixes the base
// set: "Subagents always have tools to research the codebase and to
// communicate with other agents."
//
// These tests pin the clone's reading of that sentence: the research tools
// and the messaging tool are always granted, the three switches add the
// write tools, the MCP tools and the subagent tools, and the last one is
// what lets a subagent open subagents of its own.
package subagenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
)

// grantTestCatalogue is a stand-in for the parent's tool catalogue: one stub
// per name the grant vocabulary mentions, the four subagent tools the parent
// owns, and two tools no grant names (schedule, ask_question).
func grantTestCatalogue() []piagent.AgentTool {
	names := []string{
		"view_file", "grep_search", "list_dir", "search_web", "read_url_content",
		"write_to_file", "replace_file_content", "multi_replace_file_content",
		"notebook_edit", "sed_file", "run_command", "command_status",
		"call_mcp_tool", "list_resources", "read_resource",
		"send_message", "define_subagent", "invoke_subagent", "manage_subagents",
		"schedule", "ask_question",
	}
	out := make([]piagent.AgentTool, 0, len(names))
	for _, n := range names {
		out = append(out, &stubTool{name: n})
	}
	return out
}

// nameSet turns a tool list into a set of names.
func nameSet(list []piagent.AgentTool) map[string]int {
	out := make(map[string]int, len(list))
	for _, t := range list {
		if t != nil {
			out[t.Name()]++
		}
	}
	return out
}

// requireNames fails when a name is missing from the set.
func requireNames(t *testing.T, set map[string]int, names ...string) {
	t.Helper()
	for _, n := range names {
		if set[n] == 0 {
			t.Errorf("the tool set has no %q (it has %v)", n, set)
		}
	}
}

// forbidNames fails when a name is present in the set.
func forbidNames(t *testing.T, set map[string]int, names ...string) {
	t.Helper()
	for _, n := range names {
		if set[n] > 0 {
			t.Errorf("the tool set carries %q, which no grant names (it has %v)", n, set)
		}
	}
}

// TestDefineSubagentStoresTheGrants pins that the three switches reach the
// store, in both spellings: the reference's own declaration uses the
// capitalised names (that declaration is what goes on the wire), and the
// clone's own schema uses the snake_case ones.
func TestDefineSubagentStoresTheGrants(t *testing.T) {
	defs := NewStore()
	tool := &defineTool{defs: defs}

	if _, err := tool.Execute(context.Background(), "call-cap",
		json.RawMessage(`{"Name":"cap","SystemPrompt":"You are a test.","EnableWriteTools":true,"EnableMCPTools":true,"EnableSubagentTools":true}`), nil); err != nil {
		t.Fatalf("define_subagent (capitalised): %v", err)
	}
	def, ok := defs.Get("cap")
	if !ok {
		t.Fatal("the capitalised definition was not stored")
	}
	if def.Grant != (Grant{Write: true, MCP: true, Subagent: true}) {
		t.Errorf("the stored grant = %+v, want every switch on", def.Grant)
	}

	if _, err := tool.Execute(context.Background(), "call-snake",
		json.RawMessage(`{"name":"snake","system_prompt":"You are a test.","enable_write_tools":true}`), nil); err != nil {
		t.Fatalf("define_subagent (snake_case): %v", err)
	}
	def, ok = defs.Get("snake")
	if !ok {
		t.Fatal("the snake_case definition was not stored")
	}
	if def.Grant != (Grant{Write: true}) {
		t.Errorf("the stored grant = %+v, want the write switch only", def.Grant)
	}
}

// TestDefineSubagentSchemaAdvertisesTheGrants pins that the model can ask
// for the grants at all: without the three fields in the schema there is
// nothing to send.
func TestDefineSubagentSchemaAdvertisesTheGrants(t *testing.T) {
	tool := &defineTool{defs: NewStore()}
	props, ok := tool.Schema().Parameters["properties"].(pi.JsonObject)
	if !ok {
		t.Fatalf("schema properties are %T, want pi.JsonObject", tool.Schema().Parameters["properties"])
	}
	for _, name := range []string{"enable_write_tools", "enable_mcp_tools", "enable_subagent_tools"} {
		entry, ok := props[name].(pi.JsonObject)
		if !ok {
			t.Errorf("the schema has no %q property", name)
			continue
		}
		if entry["type"] != "boolean" {
			t.Errorf("%q is %v, want a boolean", name, entry["type"])
		}
	}
}

// TestGrantedToolSetFollowsTheGrant pins the grant vocabulary: the research
// tools and the messaging tool are always there, and each switch adds its
// own group.
func TestGrantedToolSetFollowsTheGrant(t *testing.T) {
	deps := Deps{Catalogue: grantTestCatalogue()}
	defs := NewStore()

	base := nameSet(grantedToolSet(deps, defs, Grant{}))
	requireNames(t, base, "view_file", "grep_search", "list_dir", "search_web", "read_url_content", "send_message")
	forbidNames(t, base, "write_to_file", "run_command", "call_mcp_tool", "define_subagent", "invoke_subagent")

	write := nameSet(grantedToolSet(deps, defs, Grant{Write: true}))
	requireNames(t, write, "write_to_file", "replace_file_content", "multi_replace_file_content", "notebook_edit", "sed_file", "run_command", "command_status")
	forbidNames(t, write, "call_mcp_tool", "define_subagent")

	mcp := nameSet(grantedToolSet(deps, defs, Grant{MCP: true}))
	requireNames(t, mcp, "call_mcp_tool", "list_resources", "read_resource")
	forbidNames(t, mcp, "write_to_file", "define_subagent")

	sub := nameSet(grantedToolSet(deps, defs, Grant{Subagent: true}))
	requireNames(t, sub, "define_subagent", "invoke_subagent")
	forbidNames(t, sub, "write_to_file", "call_mcp_tool")
}

// TestGrantWithoutACatalogueKeepsTheMessagingTool pins that a parent that
// hands over no catalogue still gets the reference's base set: the subagent
// can talk to its peers.
func TestGrantWithoutACatalogueKeepsTheMessagingTool(t *testing.T) {
	set := nameSet(grantedToolSet(Deps{}, NewStore(), Grant{}))
	requireNames(t, set, "send_message")
	if len(set) != 1 {
		t.Errorf("the tool set = %v, want only the messaging tool", set)
	}
}

// TestSelfTypeInheritsTheParentCatalogue pins the reference's `self` type:
// it inherits the parent's tool set, and the four subagent tools are this
// package's own implementations rather than the catalogue's placeholders.
func TestSelfTypeInheritsTheParentCatalogue(t *testing.T) {
	deps := Deps{Catalogue: grantTestCatalogue()}
	tool := &invokeTool{deps: deps, defs: NewStore()}

	inv, handled, _, err := tool.resolve(context.Background(), invokeArgs{Prompt: "do it", TypeName: "self"})
	if handled {
		t.Fatal("the self type was not delegated")
	}
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	set := nameSet(inv.Tools)
	requireNames(t, set, "view_file", "run_command", "call_mcp_tool", "schedule", "ask_question")
	requireNames(t, set, "send_message", "define_subagent", "invoke_subagent", "manage_subagents")
	for _, n := range []string{"send_message", "define_subagent", "invoke_subagent", "manage_subagents"} {
		if set[n] != 1 {
			t.Errorf("%q appears %d times in the self tool set, want once", n, set[n])
		}
	}
	// The parent's placeholders are replaced by this package's own tools.
	for _, tl := range inv.Tools {
		switch tl.Name() {
		case "define_subagent":
			if _, ok := tl.(*defineTool); !ok {
				t.Errorf("define_subagent is %T, want the package's own tool", tl)
			}
		case "invoke_subagent":
			if _, ok := tl.(*invokeTool); !ok {
				t.Errorf("invoke_subagent is %T, want the package's own tool", tl)
			}
		}
	}
}

// TestSelfTypeInheritsTheParentPrompt pins that `self` runs on the parent's
// own system prompt — the reference's "full configuration including tools,
// system prompt, and model" — and that the role prefix does not apply.
func TestSelfTypeInheritsTheParentPrompt(t *testing.T) {
	deps := Deps{
		Catalogue:    grantTestCatalogue(),
		SystemPrompt: func() string { return "the parent's prompt" },
	}
	tool := &invokeTool{deps: deps, defs: NewStore()}

	inv, handled, _, err := tool.resolve(context.Background(), invokeArgs{Prompt: "do it", TypeName: "self", Role: "reviewer"})
	if handled || err != nil {
		t.Fatalf("resolve: handled=%v err=%v", handled, err)
	}
	if inv.SystemPrompt != "the parent's prompt" {
		t.Errorf("the self prompt = %q, want the parent's own prompt", inv.SystemPrompt)
	}
}

// TestResearchTypeRunsOnTheReadOnlySet pins the reference's `research` type:
// it is reachable, and it is read-only.
func TestResearchTypeRunsOnTheReadOnlySet(t *testing.T) {
	deps := Deps{Catalogue: grantTestCatalogue()}
	tool := &invokeTool{deps: deps, defs: NewStore()}

	inv, handled, _, err := tool.resolve(context.Background(), invokeArgs{Prompt: "look around", TypeName: "research"})
	if handled {
		t.Fatal("the research type was refused")
	}
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	set := nameSet(inv.Tools)
	requireNames(t, set, "view_file", "grep_search", "search_web", "read_url_content", "send_message")
	forbidNames(t, set, "write_to_file", "sed_file", "run_command", "call_mcp_tool", "define_subagent", "invoke_subagent")
	if !strings.Contains(inv.SystemPrompt, "research subagent") {
		t.Errorf("the research prompt = %q, want the research instruction", inv.SystemPrompt)
	}
}

// TestSubagentGrantCarriesDefineAndInvoke pins the nesting: a subagent with
// the subagent grant gets define_subagent and invoke_subagent, and the
// invoke tool it carries reaches the grandchild types — with the grandchild's
// own grant.
func TestSubagentGrantCarriesDefineAndInvoke(t *testing.T) {
	defs := NewStore()
	defs.Put("foreman", Invocation{
		Prompt:       "You are a foreman.",
		SystemPrompt: "You are a foreman.",
		Grant:        Grant{Subagent: true},
	})
	deps := Deps{Catalogue: grantTestCatalogue()}
	tool := &invokeTool{deps: deps, defs: defs}

	inv, handled, _, err := tool.resolve(context.Background(), invokeArgs{Prompt: "go", TypeName: "foreman"})
	if handled || err != nil {
		t.Fatalf("resolve: handled=%v err=%v", handled, err)
	}
	set := nameSet(inv.Tools)
	requireNames(t, set, "define_subagent", "invoke_subagent")

	var nested *invokeTool
	for _, tl := range inv.Tools {
		if it, ok := tl.(*invokeTool); ok {
			nested = it
		}
	}
	if nested == nil {
		t.Fatal("the granted subagent carries no invoke tool")
	}

	// The grandchild is a definition in the same conversation store, and it
	// runs under its own grant.
	defs.Put("runner", Invocation{
		Prompt:       "You are a runner.",
		SystemPrompt: "You are a runner.",
		Grant:        Grant{Write: true},
	})
	ginv, handled, _, err := nested.resolve(context.Background(), invokeArgs{Prompt: "deeper", TypeName: "runner"})
	if handled {
		t.Fatal("the grandchild type was refused")
	}
	if err != nil {
		t.Fatalf("resolve (nested): %v", err)
	}
	if !ginv.Grant.Write {
		t.Errorf("the grandchild grant = %+v, want the write switch", ginv.Grant)
	}
	requireNames(t, nameSet(ginv.Tools), "write_to_file")
	forbidNames(t, nameSet(ginv.Tools), "define_subagent")
}

// TestDefinedTypeRunsUnderItsOwnGrant pins that a type the conversation
// defined runs under the grant its definition recorded.
func TestDefinedTypeRunsUnderItsOwnGrant(t *testing.T) {
	defs := NewStore()
	defs.Put("scribe", Invocation{
		Prompt:       "You are a scribe.",
		SystemPrompt: "You are a scribe.",
		Grant:        Grant{Write: true, MCP: true},
	})
	deps := Deps{Catalogue: grantTestCatalogue()}
	tool := &invokeTool{deps: deps, defs: defs}

	inv, handled, _, err := tool.resolve(context.Background(), invokeArgs{Prompt: "write it", TypeName: "scribe"})
	if handled || err != nil {
		t.Fatalf("resolve: handled=%v err=%v", handled, err)
	}
	set := nameSet(inv.Tools)
	requireNames(t, set, "view_file", "write_to_file", "call_mcp_tool", "send_message")
	forbidNames(t, set, "define_subagent", "invoke_subagent")
	if !strings.Contains(inv.SystemPrompt, "You are a scribe.") {
		t.Errorf("the prompt = %q, want the definition's own prompt", inv.SystemPrompt)
	}
}

// TestRolePrefixSitsOnTopOfTheDefinitionPrompt pins the role handling:
// the reference's declaration makes `Role` required, and the clone puts
// it on top of the prompt the type resolved to.
func TestRolePrefixSitsOnTopOfTheDefinitionPrompt(t *testing.T) {
	defs := NewStore()
	defs.Put("scribe", Invocation{
		Prompt:       "You are a scribe.",
		SystemPrompt: "You are a scribe.",
	})
	tool := &invokeTool{deps: Deps{Catalogue: grantTestCatalogue()}, defs: defs}

	inv, handled, _, err := tool.resolve(context.Background(), invokeArgs{Prompt: "write", TypeName: "scribe", Role: "Codebase Researcher"})
	if handled || err != nil {
		t.Fatalf("resolve: handled=%v err=%v", handled, err)
	}
	want := "You are a subagent acting as: Codebase Researcher.\n\nYou are a scribe."
	if inv.SystemPrompt != want {
		t.Errorf("the prompt = %q, want %q", inv.SystemPrompt, want)
	}
}

// TestBuiltinTypeIsNotShadowedByADefinition pins that the reference's own
// built-in types answer for themselves: a definition of the same name does
// not take them over.
func TestBuiltinTypeIsNotShadowedByADefinition(t *testing.T) {
	defs := NewStore()
	defs.Put("research", Invocation{Prompt: "mine", SystemPrompt: "mine", Grant: Grant{Write: true}})
	tool := &invokeTool{deps: Deps{Catalogue: grantTestCatalogue()}, defs: defs}

	inv, handled, _, err := tool.resolve(context.Background(), invokeArgs{Prompt: "look", TypeName: "research"})
	if handled || err != nil {
		t.Fatalf("resolve: handled=%v err=%v", handled, err)
	}
	if inv.SystemPrompt != researchSystemPrompt {
		t.Errorf("the prompt = %q, want the built-in research instruction", inv.SystemPrompt)
	}
	forbidNames(t, nameSet(inv.Tools), "write_to_file")
}

// TestUnknownTypeIsRefused pins the reference's own rejection: a type the
// conversation cannot reach answers with its sentence, not a delegation.
func TestUnknownTypeIsRefused(t *testing.T) {
	tool := &invokeTool{deps: Deps{Catalogue: grantTestCatalogue()}, defs: NewStore()}
	_, handled, res, err := tool.resolve(context.Background(), invokeArgs{Prompt: "go", TypeName: "ghost"})
	if !handled {
		t.Fatal("the unknown type was delegated")
	}
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	body, ok := res.Content[0].(pi.TextContent)
	if !ok {
		t.Fatalf("the result content is %T, want a text body", res.Content[0])
	}
	if !strings.Contains(body.Text, `subagent "ghost" not found or not allowed to be invoked`) {
		t.Errorf("the result = %q, want the reference's refusal", body.Text)
	}
}

// TestRestoreDefinitionRestoresTheGrants pins that a resumed conversation's
// subagents run under the authorization they were defined with.
func TestRestoreDefinitionRestoresTheGrants(t *testing.T) {
	defs := NewStore()
	ok := defs.RestoreDefinition(map[string]string{
		metaName:                "resumed",
		metaSystemPrompt:        "You are a resumed subagent.",
		metaEnableWriteTools:    "true",
		metaEnableMCPTools:      "false",
		metaEnableSubagentTools: "true",
	})
	if !ok {
		t.Fatal("the rehydrated definition was not restored")
	}
	def, found := defs.Get("resumed")
	if !found {
		t.Fatal("the restored definition is not in the store")
	}
	if def.Grant != (Grant{Write: true, Subagent: true}) {
		t.Errorf("the restored grant = %+v, want the write and subagent switches", def.Grant)
	}
}
