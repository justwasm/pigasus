// subagenttools_test.go — the cycle-228 tests for the AGY subagent
// toolset: invoke_subagent, send_message, define_subagent and
// manage_subagents.
package subagenttools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
)

// ─── shared helpers ───────────────────────────────────────────────────────

func subagentTestModel() string {
	return "test-model"
}

func subagentTestServices(t *testing.T) Deps {
	t.Helper()
	dir := t.TempDir()
	mock := actor.NewMockProvider()
	return Deps{
		Provider: mock,
		Model:    subagentTestModel(),
		CWD:      dir,
		AgentDir: filepath.Join(dir, "agent"),
	}
}

// ─── TestInvokeSubagentToolDeclaration ─────────────────────────────────────

func TestInvokeSubagentToolDeclaration(t *testing.T) {
	deps := subagentTestServices(t)
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

	assertInvokeSchema(t, got[0].Definition())
	assertSendSchema(t, got[2].Definition())
	assertManageSchema(t, got[3].Definition())
}

func assertInvokeSchema(t *testing.T, s actor.ToolDefinition) {
	t.Helper()
	if s.Name != "invoke_subagent" {
		t.Fatalf("invoke schema.Name = %q, want invoke_subagent", s.Name)
	}
	if !hasRequiredProperty(s.Parameters, "prompt") {
		t.Fatalf("invoke_subagent parameters missing required=%q: %+v", "prompt", s.Parameters)
	}
	if hasRequiredProperty(s.Parameters, "model") {
		t.Fatalf("invoke_subagent.parameters should NOT mark model as required: %+v", s.Parameters)
	}
}

func assertSendSchema(t *testing.T, s actor.ToolDefinition) {
	t.Helper()
	if s.Name != "send_message" {
		t.Fatalf("send schema.Name = %q, want send_message", s.Name)
	}
	if strings.Contains(s.Description, "not yet implemented") {
		t.Fatalf("send_message description still carries a stub marker: %q", s.Description)
	}
	if !hasRequiredProperty(s.Parameters, "Recipient") || !hasRequiredProperty(s.Parameters, "Message") {
		t.Fatalf("send_message parameters missing required Recipient/Message: %+v", s.Parameters)
	}
}

func assertManageSchema(t *testing.T, s actor.ToolDefinition) {
	t.Helper()
	if s.Name != "manage_subagents" {
		t.Fatalf("manage schema.Name = %q, want manage_subagents", s.Name)
	}
	if strings.Contains(s.Description, "not yet implemented") {
		t.Fatalf("manage_subagents description still carries a stub marker: %q", s.Description)
	}
	if !hasRequiredProperty(s.Parameters, "Action") {
		t.Fatalf("manage_subagents parameters missing required Action: %+v", s.Parameters)
	}
	if !propertyHasEnum(s.Parameters, "Action", []string{"list", "kill", "kill_all"}) {
		t.Fatalf("manage_subagents.parameters.Action missing list/kill/kill_all enum: %+v", s.Parameters)
	}
	if propertyHasEnum(s.Parameters, "Action", []string{"list", "list_peers", "kill", "kill_all"}) {
		t.Fatalf("manage_subagents.parameters.Action carries the harness-side four-value enum: %+v", s.Parameters)
	}
}

func hasRequiredProperty(params map[string]any, name string) bool {
	if params == nil {
		return false
	}
	req, ok := params["required"]
	if !ok {
		return false
	}
	switch v := req.(type) {
	case []string:
		for _, s := range v {
			if s == name {
				return true
			}
		}
	case []any:
		for _, s := range v {
			if str, ok := s.(string); ok && str == name {
				return true
			}
		}
	}
	return false
}

func propertyHasEnum(params map[string]any, name string, want []string) bool {
	if params == nil {
		return false
	}
	props, ok := params["properties"].(map[string]any)
	if !ok {
		return false
	}
	entry, ok := props[name].(map[string]any)
	if !ok {
		return false
	}
	raw, ok := entry["enum"]
	if !ok {
		return false
	}
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

func TestInvokeSubagentStubProvider(t *testing.T) {
	deps := subagentTestServices(t)
	mock := deps.Provider.(*actor.MockProvider)
	mock.EnqueueResponse("42")

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

func TestInvokeSubagentToolExecuteAcknowledgement(t *testing.T) {
	registry := NewRegistry("parent-conversation")
	defer registry.Close()
	deps := subagentTestServices(t)
	deps.Registry = registry
	tool := &invokeTool{deps: deps}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	args, _ := json.Marshal(invokeArgs{Prompt: "What is 20+22?"})
	res, err := tool.Execute(ctx, string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("Execute returned Error on success envelope: %+v", res)
	}
	text := res.Output
	for _, want := range []string{
		"Created At: ",
		"Completed At: ",
		"Created the following subagent:\n",
		"  \"conversationId\":  \"",
		"  \"logAbsoluteUri\":  \"file://",
		"  \"workspaceUris\":  [\n",
		"The subagent will send you a message when it has completed its task or requires guidance. There is no need to poll for its response.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("acknowledgement %q does not contain %q", text, want)
		}
	}
	children := registry.List()
	if len(children) != 1 {
		t.Fatalf("registry holds %d children, want 1", len(children))
	}
	if !strings.Contains(text, children[0].ConversationID) {
		t.Fatalf("acknowledgement %q does not name the child %q", text, children[0].ConversationID)
	}
	registry.Close()
	time.Sleep(100 * time.Millisecond)
}

// ─── TestInvokeSubagentErrorPath ───────────────────────────────────────────

func TestInvokeSubagentErrorPath(t *testing.T) {
	ctx := context.Background()

	// nil Deps.Provider
	if _, err := Invoke(ctx, Invocation{Prompt: "anything"}, Deps{Model: "x"}); err == nil {
		t.Fatal("Invoke with nil Provider returned nil error, want error")
	}

	// blank Prompt
	deps := subagentTestServices(t)
	if _, err := Invoke(ctx, Invocation{Prompt: "   "}, deps); err == nil {
		t.Fatal("Invoke with blank Prompt returned nil error, want error")
	}

	// empty Deps.Model
	noModelDeps := subagentTestServices(t)
	noModelDeps.Model = ""
	if _, err := Invoke(ctx, Invocation{Prompt: "anything"}, noModelDeps); err == nil {
		t.Fatal("Invoke with empty Deps.Model returned nil error, want error")
	}
}

func TestInvokeSubagentToolArgErrors(t *testing.T) {
	tool := &invokeTool{deps: subagentTestServices(t)}

	// blank prompt: pre-flight validation error
	args, _ := json.Marshal(invokeArgs{Prompt: "  "})
	_, err := tool.Execute(context.Background(), string(args))
	if err == nil {
		t.Fatal("Execute(blank) returned nil error, want pre-flight validation error")
	}
	if !strings.Contains(err.Error(), "prompt is required") {
		t.Fatalf("Execute(blank) err = %q, want substring %q", err.Error(), "prompt is required")
	}

	// malformed JSON: pre-flight parse error
	_, err = tool.Execute(context.Background(), "not json")
	if err == nil {
		t.Fatal("Execute(bad-json) returned nil error, want JSON parse failure")
	}
	if !strings.Contains(err.Error(), "parse args") {
		t.Fatalf("Execute(bad-json) err = %q, want substring %q", err.Error(), "parse args")
	}
}

// ─── TestSendMessageStub ───────────────────────────────────────────────────

func TestSendMessageUnknownRecipient(t *testing.T) {
	tool := &sendMessageTool{}

	args, _ := json.Marshal(sendMessageArgs{
		SubagentID: "subagent-1700000000000-1",
		Message:    "ping",
	})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute returned err: %v", err)
	}
	if res.Error != "" {
		t.Fatalf("Execute returned Error, want a normal result: %v", res.Error)
	}
	body := res.Output
	if !strings.HasPrefix(body, "Created At: ") || !strings.Contains(body, "\nCompleted At: ") {
		t.Fatalf("result has no timestamp header: %q", body)
	}
	want := `Encountered error in tool execution: recipient "subagent-1700000000000-1" not found`
	if !strings.HasSuffix(body, want) {
		t.Fatalf("result = %q, want suffix %q", body, want)
	}

	// Missing required field: should return a Go error (caller-side).
	_, err = tool.Execute(context.Background(), "{}")
	if err == nil {
		t.Fatal("Execute with empty args returned nil error, want missing-field error")
	}
}

func TestDefineSubagentResultText(t *testing.T) {
	defs := NewStore()
	tool := &defineTool{defs: defs}
	res, err := tool.Execute(context.Background(), `{"Name":"cycletest","Description":"Test subagent","SystemPrompt":"You are a test."}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatalf("define_subagent returned Error: %v", res.Error)
	}
	got := res.Output
	if !strings.HasSuffix(got, `Subagent "cycletest" defined successfully. It can now be invoked via invoke_subagent.`) {
		t.Fatalf("define_subagent = %q", got)
	}
	if def, ok := defs.Get("cycletest"); !ok || def.SystemPrompt != "You are a test." {
		t.Fatalf("definition not stored: %+v ok=%v", def, ok)
	}
}

func TestInvokeSubagentUnknownType(t *testing.T) {
	tool := &invokeTool{deps: subagentTestServices(t), defs: NewStore()}
	res, err := tool.Execute(context.Background(), `{"TypeName":"general","Role":"Tester","Prompt":"Say OK."}`)
	if err != nil {
		t.Fatal(err)
	}
	if res.Error != "" {
		t.Fatalf("unknown type returned Error, want a normal result: %v", res.Error)
	}
	got := res.Output
	if !strings.HasSuffix(got, `Encountered error in tool execution: subagent "general" not found or not allowed to be invoked`) {
		t.Fatalf("invoke_subagent unknown type = %q", got)
	}
}
