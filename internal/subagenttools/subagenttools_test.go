// subagenttools_test.go — the six cycle-228 tests for the AGY 1.4.0
// subagent toolset: invoke_subagent (real), send_message and
// manage_subagents (stubs).
//
// The first three probes (declaration + scripted run + error path)
// give the AGY-shape contract test coverage today; the last three
// freeze the stub surface (description + IsError envelope) so a
// later cycle can swap them to real implementations without changing
// the wire schema the model already learned.
//
// End-to-end Invoke uses ai.TestFauxProvider via the inline
// `pi.Model.Provider` field; PiG's `coding.Runtime` does not route
// through `BuildModel` when a fully-resolved `*ai.Model` is handed
// to `SessionStartOptions.Model`, so the test-faux provider reaches
// `Stream` without an auth handshake. This is the same trick
// `internal/pigrt/runtime_test.go:24` uses; the only difference is
// that we exercise subagenttools.Invoke from here.
package subagenttools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
)

// ─── shared helpers ───────────────────────────────────────────────────────

// subagentTestModel mirrors `pigrt.fauxModel()` but lives in this
// package's _test scope so the declaration is local. The inline
// `Provider` field is the seam the runtime uses — see the file header
// for why TestFauxProvider is the right fixture here.
func subagentTestModel() *pi.Model {
	return &pi.Model{
		Type:         pi.ModelTypeChat,
		ID:           "faux-1",
		DisplayName:  "faux-1",
		Provider:     &pi.TestFauxProvider{},
		ProviderMeta: pi.ProviderMetadata{ProviderID: "test-faux", API: "test-faux"},
		Input:        []string{"text"},
	}
}

// subagentTestServices builds a coding.AgentSessionServices container the Invoke
// path can use directly. The container is closed by the caller's
// defer and owns no background goroutines outside its auth store.
func subagentTestServices(t *testing.T) *coding.AgentSessionServices {
	t.Helper()
	dir := t.TempDir()
	svcs, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      dir,
		AgentDir: filepath.Join(dir, "agent"),
	})
	if err != nil {
		t.Fatalf("coding.CreateAgentSessionServices: %v", err)
	}
	t.Cleanup(func() { svcs.Close() })
	return svcs
}

// ─── TestInvokeSubagentToolDeclaration ─────────────────────────────────────

// TestInvokeSubagentToolDeclaration verifies the AGY-shape wire
// contract: Tools() returns exactly the three cycle-228 tools in
// stable order, with the documented names, schemas and the
// "cycle 228 stub" marker visible in every stub description.
func TestInvokeSubagentToolDeclaration(t *testing.T) {
	deps := Deps{
		Services: subagentTestServices(t),
		Model:    subagentTestModel(),
	}
	got := Tools(deps)
	if len(got) != 4 {
		t.Fatalf("Tools() returned %d tools, want 4", len(got))
	}
	names := make([]string, 0, len(got))
	for _, tool := range got {
		names = append(names, tool.Name())
	}
	want := []string{"invoke_subagent", "define_subagent", "send_message", "manage_subagents"}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("Tools()[%d].Name() = %q, want %q (full: %v)", i, names[i], n, names)
		}
	}

	// invoke_subagent schema: prompt is required; model, system_prompt,
	// worktree are optional.
	invokeSchema := got[0].Schema()
	if invokeSchema.Name != "invoke_subagent" {
		t.Fatalf("invoke schema.Name = %q, want invoke_subagent", invokeSchema.Name)
	}
	if !hasRequiredProperty(invokeSchema.Parameters, "prompt") {
		t.Fatalf("invoke_subagent parameters missing required=%q: %+v", "prompt", invokeSchema.Parameters)
	}
	if hasRequiredProperty(invokeSchema.Parameters, "model") {
		t.Fatalf("invoke_subagent.parameters should NOT mark model as required: %+v", invokeSchema.Parameters)
	}

	// send_message: unknown recipients are reported in the reference's
	// shape. The description still carries the stub marker until delivery is
	// wired.
	sendSchema := got[2].Schema()
	if sendSchema.Name != "send_message" {
		t.Fatalf("send schema.Name = %q, want send_message", sendSchema.Name)
	}
	if !strings.Contains(sendSchema.Description, "not yet implemented (cycle 228 stub)") {
		t.Fatalf("send_message description missing stub marker: %q", sendSchema.Description)
	}
	if !hasRequiredProperty(sendSchema.Parameters, "subagent_id") || !hasRequiredProperty(sendSchema.Parameters, "message") {
		t.Fatalf("send_message parameters missing required subagent_id/message: %+v", sendSchema.Parameters)
	}

	// manage_subagents: stub. action is required AND enum'd to
	// list/kill/status.
	mgrSchema := got[3].Schema()
	if mgrSchema.Name != "manage_subagents" {
		t.Fatalf("manage schema.Name = %q, want manage_subagents", mgrSchema.Name)
	}
	if !strings.Contains(mgrSchema.Description, "not yet implemented (cycle 228 stub)") {
		t.Fatalf("manage_subagents description missing stub marker: %q", mgrSchema.Description)
	}
	if !hasRequiredProperty(mgrSchema.Parameters, "action") {
		t.Fatalf("manage_subagents parameters missing required action: %+v", mgrSchema.Parameters)
	}
	if !propertyHasEnum(mgrSchema.Parameters, "action", []string{"list", "kill", "status"}) {
		t.Fatalf("manage_subagents.parameters.action missing list/kill/status enum: %+v", mgrSchema.Parameters)
	}
}

// hasRequiredProperty returns true when params["required"] lists name.
// params is the JSON-object PiG builds the schema from.
func hasRequiredProperty(params pi.JsonObject, name string) bool {
	req, ok := params["required"]
	if !ok {
		return false
	}
	values, ok := req.([]string)
	if !ok {
		return false
	}
	for _, v := range values {
		if v == name {
			return true
		}
	}
	return false
}

// propertyHasEnum walks params["properties"][name]["enum"] and
// returns true iff the JSON array matches want elementwise.
func propertyHasEnum(params pi.JsonObject, name string, want []string) bool {
	props, ok := params["properties"].(pi.JsonObject)
	if !ok {
		return false
	}
	entry, ok := props[name].(pi.JsonObject)
	if !ok {
		return false
	}
	raw, ok := entry["enum"]
	if !ok {
		return false
	}
	// PiG's JsonObject lives on encoding/json — the enum may be
	// `[]any` (the generic form) or `[]string`. Accept both.
	switch v := raw.(type) {
	case []string:
		return stringSliceEqual(v, want)
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			s, ok := x.(string)
			if !ok {
				return false
			}
			out = append(out, s)
		}
		return stringSliceEqual(out, want)
	default:
		return false
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ─── TestInvokeSubagentStubProvider ────────────────────────────────────────

// TestInvokeSubagentStubProvider runs Invoke end-to-end through
// TestFauxProvider; the prompt is crafted to deterministically
// trigger parity-basic's "42" response so the test does not depend
// on model nondeterminism. The assertion checks the resolved text
// matches, the SubagentID is populated, and no error is reported.
func TestInvokeSubagentStubProvider(t *testing.T) {
	deps := Deps{
		Services: subagentTestServices(t),
		Model:    subagentTestModel(),
		CWD:      t.TempDir(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := Invoke(ctx, Invocation{
		Prompt: "What is 20+22?",
	}, deps)
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !strings.Contains(res.Text, "42") {
		t.Fatalf("Invoke returned text %q, want substring %q", res.Text, "42")
	}
	if res.SubagentID == "" {
		t.Fatalf("Invoke returned empty SubagentID")
	}
	if !strings.HasPrefix(res.SubagentID, "subagent-") {
		t.Fatalf("SubagentID %q does not start with %q", res.SubagentID, "subagent-")
	}
	if res.Duration <= 0 {
		t.Fatalf("Duration = %v, want > 0", res.Duration)
	}
}

// TestInvokeSubagentToolExecuteEnvelope exercises the parent-facing
// Execute path (not the package-internal Invoke) to assert the
// success envelope AGY's model will see: ok=true, subagent_id set,
// text carrying the subagent's reply. This is the on-the-wire shape
// the parent's prompt reads.
func TestInvokeSubagentToolExecuteEnvelope(t *testing.T) {
	tool := &invokeTool{deps: Deps{
		Services: subagentTestServices(t),
		Model:    subagentTestModel(),
		CWD:      t.TempDir(),
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	args, _ := json.Marshal(invokeArgs{Prompt: "What is 20+22?"})
	res, err := tool.Execute(ctx, "call-1", args, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned IsError=true on success envelope: %+v", res)
	}
	if len(res.Content) != 1 {
		t.Fatalf("Execute returned %d content blocks, want 1", len(res.Content))
	}
	body, ok := res.Content[0].(pi.TextContent)
	if !ok {
		t.Fatalf("Execute content[0] = %T, want pi.TextContent", res.Content[0])
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(body.Text), &envelope); err != nil {
		t.Fatalf("envelope unmarshal: %v (raw=%q)", err, body.Text)
	}
	if ok, _ := envelope["ok"].(bool); !ok {
		t.Fatalf("envelope.ok = %v, want true", envelope["ok"])
	}
	if _, has := envelope["subagent_id"]; !has {
		t.Fatalf("envelope missing subagent_id: %+v", envelope)
	}
	text, _ := envelope["text"].(string)
	if !strings.Contains(text, "42") {
		t.Fatalf("envelope.text = %q, want substring %q", text, "42")
	}
}

// ─── TestInvokeSubagentErrorPath ───────────────────────────────────────────

// TestInvokeSubagentErrorPath covers the deterministic pre-flight
// errors Invoke rejects without ever spinning up a session:
//
//   - missing Deps.Services
//   - missing/blank Invocation.Prompt
//   - missing Deps.Model
//
// All three paths must surface as Go errors, not as in-stream
// failures, because each is the caller's contract violation (the
// parent tool's Execute method turns them into IsError envelopes).
func TestInvokeSubagentErrorPath(t *testing.T) {
	ctx := context.Background()

	// nil Deps.Services
	if _, err := Invoke(ctx, Invocation{Prompt: "anything"}, Deps{}); err == nil {
		t.Fatal("Invoke with nil Services returned nil error, want error")
	}

	// blank Prompt
	deps := Deps{Services: subagentTestServices(t), Model: subagentTestModel()}
	if _, err := Invoke(ctx, Invocation{Prompt: "   "}, deps); err == nil {
		t.Fatal("Invoke with blank Prompt returned nil error, want error")
	}

	// nil Deps.Model
	noModelDeps := Deps{Services: subagentTestServices(t), Model: nil}
	if _, err := Invoke(ctx, Invocation{Prompt: "anything"}, noModelDeps); err == nil {
		t.Fatal("Invoke with nil Deps.Model returned nil error, want error")
	}

	// Model + Invocation.Model set: invModel copies Deps.Model and
	// overrides ID. A typed-nil model is rejected identically to nil.
	var nilModel *pi.Model
	if _, err := Invoke(ctx, Invocation{Prompt: "anything", Model: "x"}, Deps{Services: subagentTestServices(t), Model: nilModel}); err == nil {
		t.Fatal("Invoke with typed-nil Deps.Model returned nil error, want error")
	}
}

// TestInvokeSubagentToolArgErrors exercises the parent-facing
// Execute path's argument validation: a blank Prompt and a
// malformed JSON payload both surface as raw Go errors so the
// parent's loop can classify them as pre-flight failures. Runtime
// failures (failed Invoke) get the structured envelope with
// IsError=true; only validation errors return raw errs.
func TestInvokeSubagentToolArgErrors(t *testing.T) {
	tool := &invokeTool{deps: Deps{
		Services: subagentTestServices(t),
		Model:    subagentTestModel(),
	}}

	// blank prompt: pre-flight validation error
	args, _ := json.Marshal(invokeArgs{Prompt: "  "})
	_, err := tool.Execute(context.Background(), "call-blank", args, nil)
	if err == nil {
		t.Fatal("Execute(blank) returned nil error, want pre-flight validation error")
	}
	if !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("Execute(blank) err = %q, want substring %q", err.Error(), "prompt is required")
	}

	// malformed JSON: pre-flight parse error
	_, err = tool.Execute(context.Background(), "call-junk", json.RawMessage("not json"), nil)
	if err == nil {
		t.Fatal("Execute(bad-json) returned nil error, want JSON parse failure")
	}
	if !strings.Contains(err.Error(), "parse args") {
		t.Fatalf("Execute(bad-json) err = %q, want substring %q", err.Error(), "parse args")
	}
}

// ─── TestSendMessageStub ───────────────────────────────────────────────────

// TestSendMessageUnknownRecipient pins the measured 1.4.4 result: a
// conversation id that is not live (this clone delegates synchronously, so
// none ever is) is reported as a normal, stamped result — not IsError.
func TestSendMessageUnknownRecipient(t *testing.T) {
	tool := &sendMessageTool{}

	args, _ := json.Marshal(sendMessageArgs{
		SubagentID: "subagent-1700000000000-1",
		Message:    "ping",
	})
	res, err := tool.Execute(context.Background(), "call-sm", args, nil)
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute returned IsError=true, want a normal result")
	}
	if len(res.Content) != 1 {
		t.Fatalf("Execute returned %d content blocks, want 1", len(res.Content))
	}
	body, ok := res.Content[0].(pi.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T, want pi.TextContent", res.Content[0])
	}
	if !strings.HasPrefix(body.Text, "Created At: ") || !strings.Contains(body.Text, "\nCompleted At: ") {
		t.Fatalf("result has no timestamp header: %q", body.Text)
	}
	want := `Encountered error in tool execution: recipient "subagent-1700000000000-1" not found`
	if !strings.HasSuffix(body.Text, want) {
		t.Fatalf("result = %q, want suffix %q", body.Text, want)
	}

	// Missing required field: should return a Go error (caller-side).
	res, err = tool.Execute(context.Background(), "call-sm-empty", json.RawMessage("{}"), nil)
	if err == nil {
		t.Fatal("Execute with empty args returned nil error, want missing-field error")
	}
	if res.IsError {
		t.Fatalf("Execute with empty args returned IsError=true envelope, want raw error")
	}
}

// ─── TestManageSubagentsStubList ───────────────────────────────────────────

// TestManageSubagentsStubList locks the list action's contract:
// action=list is accepted, returns ok=false with the documented
// stub error and IsError=true. The model already sees this today;
// cycle 229 will swap the body for the real listing.
func TestManageSubagentsStubList(t *testing.T) {
	tool := &manageSubagentsTool{}

	args, _ := json.Marshal(manageSubagentsArgs{Action: "list"})
	res, err := tool.Execute(context.Background(), "call-mgr-list", args, nil)
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if !res.IsError {
		t.Fatalf("Execute returned IsError=false, want IsError=true")
	}
	assertStubEnvelope(t, res)
}

// ─── TestManageSubagentsStubKill ───────────────────────────────────────────

// TestManageSubagentsKillEmpty pins the measured 1.4.4 text for kill/kill_all
// when nothing is running (always, here: delegations are synchronous).
func TestManageSubagentsKillEmpty(t *testing.T) {
	tool := &manageSubagentsTool{}

	for _, action := range []string{"kill", "kill_all"} {
		args, _ := json.Marshal(manageSubagentsArgs{Action: action, SubagentID: "subagent-1"})
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
}

// assertStubEnvelope is a small helper the two manage_subagents
// tests share: it confirms the body is JSON with ok=false and the
// documented "not yet implemented (cycle 228 stub)" marker.
func assertStubEnvelope(t *testing.T, res piagent.AgentToolResult) {
	t.Helper()
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
		t.Fatalf("envelope.ok = %v, want false", env["ok"])
	}
	if msg, _ := env["error"].(string); !strings.Contains(msg, "not yet implemented (cycle 228 stub)") {
		t.Fatalf("envelope.error = %q, want substring %q", msg, "not yet implemented (cycle 228 stub)")
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
	if got := res.Content[0].(pi.TextContent).Text; !strings.HasSuffix(got, "No active subagents to kill.") {
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

// TestDefineSubagentResultText pins the captured 1.4.4 text and the fact that
// a definition is stored under its name for invoke_subagent to find.
func TestDefineSubagentResultText(t *testing.T) {
	defs := newDefinitionStore()
	tool := &defineTool{defs: defs}
	res, err := tool.Execute(context.Background(), "call-def",
		json.RawMessage(`{"Name":"cycletest","Description":"Test subagent","SystemPrompt":"You are a test."}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("define_subagent returned IsError=true")
	}
	got := res.Content[0].(pi.TextContent).Text
	if !strings.HasSuffix(got, `Subagent "cycletest" defined successfully. It can now be invoked via invoke_subagent.`) {
		t.Fatalf("define_subagent = %q", got)
	}
	if def, ok := defs.get("cycletest"); !ok || def.SystemPrompt != "You are a test." {
		t.Fatalf("definition not stored: %+v ok=%v", def, ok)
	}
}

// TestInvokeSubagentUnknownType pins the captured 1.4.4 rejection. The old
// behaviour spawned a fresh session with whatever prompt was present, which
// silently ignored TypeName.
func TestInvokeSubagentUnknownType(t *testing.T) {
	tool := &invokeTool{deps: Deps{
		Services: subagentTestServices(t),
		Model:    subagentTestModel(),
		CWD:      t.TempDir(),
	}, defs: newDefinitionStore()}
	res, err := tool.Execute(context.Background(), "call-inv",
		json.RawMessage(`{"TypeName":"general","Role":"Tester","Prompt":"Say OK."}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("unknown type returned IsError=true, want a normal result")
	}
	got := res.Content[0].(pi.TextContent).Text
	if !strings.HasSuffix(got, `Encountered error in tool execution: subagent "general" not found or not allowed to be invoked`) {
		t.Fatalf("invoke_subagent unknown type = %q", got)
	}
}
