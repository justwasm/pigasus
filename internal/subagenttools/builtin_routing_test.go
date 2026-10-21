// builtin_routing_test.go — cycle image-generator-subagent tests for
// the agent_name plumbing in invoke_subagent.
//
// Three properties the cycle establishes:
//
//  1. TestInvokeSubagentBuiltinRouting_ImageGenerator — invoking
//     invoke_subagent with `agent_name: "image-generator"` and a
//     prompt routes to the built-in handler, skips the PiG
//     session entirely, and returns the structured Result envelope
//     the model reads back.
//
//  2. TestInvokeSubagentBuiltinRouting_UnknownName — a typo in
//     agent_name surfaces as IsError=true with the typed
//     ErrUnknownName message, not as a Go error from the parent.
//
//  3. TestInvokeSubagentBuiltinRouting_SchemaMentionsBuiltins —
//     the invoke_subagent schema description advertises the
//     built-in catalogue so the model can pick a name without
//     trial-and-error.
//
// Tests reset the package-level registry to a fresh
// builtinsubagent.Default() instance so a future cycle that mutates
// it (SetBuiltinSubagentRegistry) does not leak into this suite.
package subagenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/justwasm/pigasus/internal/builtinsubagent"
)

// resetBuiltinRegistry ensures the singleton is the package-default
// before each test runs. Without this, a test that calls
// SetBuiltinSubagentRegistry could leak state into subsequent runs
// in the same process.
func resetBuiltinRegistry(t *testing.T) {
	t.Helper()
	SetBuiltinSubagentRegistry(builtinsubagent.Default())
}

// TestInvokeSubagentBuiltinRouting_ImageGenerator confirms that
// invoke_subagent with agent_name="image-generator" reaches the
// built-in handler and produces a structured result envelope the
// model can read. The envelope is the path the model's tool
// result block takes into the parent transcript.
//
// TMPDIR is redirected via t.Setenv so the produced PNG lives in
// the test's TempDir and is cleaned up by t.TempDir's auto-remove.
func TestInvokeSubagentBuiltinRouting_ImageGenerator(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	resetBuiltinRegistry(t)

	tool := &invokeTool{deps: Deps{
		Services: subagentTestServices(t),
		Model:    subagentTestModel(),
		CWD:      t.TempDir(),
	}}

	args, _ := json.Marshal(invokeArgs{
		AgentName: "image-generator",
		Prompt:    "a calm orange sunset over still water",
	})
	res, err := tool.Execute(context.Background(), "call-img", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned IsError=true on built-in success: %+v", res)
	}
	if len(res.Content) != 1 {
		t.Fatalf("Execute returned %d content blocks, want 1", len(res.Content))
	}
	body, ok := res.Content[0].(pi.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want pi.TextContent", res.Content[0])
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(body.Text), &env); err != nil {
		t.Fatalf("envelope unmarshal: %v (raw=%q)", err, body.Text)
	}
	if ok, _ := env["ok"].(bool); !ok {
		t.Fatalf("envelope.ok = %v, want true", env["ok"])
	}
	if name, _ := env["agent_name"].(string); name != "image-generator" {
		t.Errorf("envelope.agent_name = %q, want %q", name, "image-generator")
	}
	if path, _ := env["path"].(string); path == "" {
		t.Errorf("envelope.path is empty")
	}
	if mime, _ := env["mime_type"].(string); mime != "image/png" {
		t.Errorf("envelope.mime_type = %q, want image/png", mime)
	}
}

// TestInvokeSubagentBuiltinRouting_UnknownName pins the typed error
// path: a mistyped agent_name produces IsError=true with the
// ErrUnknownName message, NOT a Go error. The model sees the JSON
// envelope and can recover by trying a different name.
func TestInvokeSubagentBuiltinRouting_UnknownName(t *testing.T) {
	resetBuiltinRegistry(t)
	tool := &invokeTool{deps: Deps{
		Services: subagentTestServices(t),
		Model:    subagentTestModel(),
		CWD:      t.TempDir(),
	}}

	args, _ := json.Marshal(invokeArgs{
		AgentName: "ghost-subagent",
		Prompt:    "x",
	})
	res, err := tool.Execute(context.Background(), "call-ghost", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.IsError {
		t.Fatalf("Execute returned IsError=false on unknown agent_name, want IsError=true")
	}
	if len(res.Content) != 1 {
		t.Fatalf("Execute returned %d content blocks, want 1", len(res.Content))
	}
	body, ok := res.Content[0].(pi.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want pi.TextContent", res.Content[0])
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(body.Text), &env); err != nil {
		t.Fatalf("envelope unmarshal: %v (raw=%q)", err, body.Text)
	}
	if env["ok"] != false {
		t.Errorf("envelope.ok = %v, want false", env["ok"])
	}
	if name, _ := env["agent_name"].(string); name != "ghost-subagent" {
		t.Errorf("envelope.agent_name = %q, want ghost-subagent", name)
	}
	if msg, _ := env["error"].(string); !strings.Contains(msg, "ghost-subagent") {
		t.Errorf("envelope.error = %q, want substring %q", msg, "ghost-subagent")
	}
}

// TestInvokeSubagentBuiltinRouting_SchemaMentionsBuiltins confirms
// the model-facing description surfaces the catalogue so a model
// can pick a built-in name without guessing. The exact text is
// allowed to grow as more built-ins are added; the load-bearing
// claims are:
//
//   - the description mentions "image-generator" by name;
//   - the description mentions the agent_name parameter as the way
//     to invoke a built-in.
//
// A future cycle that renames the parameter is welcome to update
// the description; the test would need to follow.
func TestInvokeSubagentBuiltinRouting_SchemaMentionsBuiltins(t *testing.T) {
	resetBuiltinRegistry(t)
	tool := &invokeTool{}
	schema := tool.Schema()
	if !strings.Contains(schema.Description, "image-generator") {
		t.Errorf("schema.Description does not mention image-generator:\n%s", schema.Description)
	}
	if !strings.Contains(schema.Description, "Built-in subagents") {
		t.Errorf("schema.Description does not include the catalogue block:\n%s", schema.Description)
	}
	props, ok := schema.Parameters["properties"].(pi.JsonObject)
	if !ok {
		t.Fatalf("schema.Properties is %T, want pi.JsonObject", schema.Parameters["properties"])
	}
	agentName, ok := props["agent_name"].(pi.JsonObject)
	if !ok {
		t.Fatalf("schema.Properties missing agent_name entry: %+v", props)
	}
	if desc, _ := agentName["description"].(string); !strings.Contains(desc, "image-generator") {
		t.Errorf("agent_name.description does not reference image-generator:\n%s", desc)
	}
}

// piTextContent is a local alias for the PiG TextContent type the
// invokeTool responses use. PiG's package alias (`pi`) is imported
// at the top of this file; the alias here is documentation-only.
type piTextContent = pi.TextContent
