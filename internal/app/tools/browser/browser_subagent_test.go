package browser

import (
	"context"
	"encoding/json"
	"github.com/justwasm/pigasus/internal/app/tools"
	"strings"
	"testing"
)

// The browser_subagent tool: the reference's browser agent, behind a
// provider seam.
//
// 【一手】 the name is one of the sixty-three the 1.4.7 binary advertises.
// 【反编译】 the reference's step for the call is CortexStepBrowserSubagent,
// whose first field is `task` — the tool takes a task and answers with a
// result.

type fakeBrowserSubagentProvider struct {
	task   string
	result string
	err    error
	calls  int
}

func (f *fakeBrowserSubagentProvider) RunBrowserTask(_ context.Context, task string) (string, error) {
	f.calls++
	f.task = task
	return f.result, f.err
}

// TestBrowserSubagentSchema pins the declaration's shape: one string
// parameter, `task`, and it is required.
func TestBrowserSubagentSchema(t *testing.T) {
	tool := &BrowserSubagent{}
	if tool.Name() != "browser_subagent" {
		t.Fatalf("Name() = %q, want browser_subagent", tool.Name())
	}
	var schema struct {
		Type       string `json:"type"`
		Properties struct {
			Task struct {
				Type string `json:"type"`
			} `json:"task"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("schema unmarshal: %v (%s)", err, tool.Schema())
	}
	if schema.Type != "object" {
		t.Errorf("schema.type = %q, want object", schema.Type)
	}
	if schema.Properties.Task.Type != "string" {
		t.Errorf("task type = %q, want string", schema.Properties.Task.Type)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "task" {
		t.Errorf("required = %v, want [task]", schema.Required)
	}
}

// TestBrowserSubagentDescriptionIsTheReferences pins that the tool advertises
// the reference's own browser-agent text, not a paraphrase.
func TestBrowserSubagentDescriptionIsTheReferences(t *testing.T) {
	got := (&BrowserSubagent{}).Description()
	want := tools.ReferenceDescriptions()["browser_subagent"].Text
	if got != want {
		t.Errorf("description is not the reference's text\n got: %q\nwant: %q", got, want)
	}
	if !strings.Contains(got, "Browser agent that can perform actions in user's Google Chrome browser") {
		t.Errorf("description does not look like the reference's browser-agent text: %q", got)
	}
}

// TestBrowserSubagentRequiresTask pins that a call without a task fails
// loudly instead of launching a browser agent with nothing to do.
func TestBrowserSubagentRequiresTask(t *testing.T) {
	provider := &fakeBrowserSubagentProvider{result: "unused"}
	tool := &BrowserSubagent{Provider: provider}
	for _, args := range []string{`{}`, `{"task":""}`, `{"task":"   "}`} {
		if _, err := tool.Run(context.Background(), json.RawMessage(args)); err == nil {
			t.Errorf("Run(%s) = nil error, want one", args)
		}
	}
	if provider.calls != 0 {
		t.Errorf("the provider was called %d times, want 0", provider.calls)
	}
}

// TestBrowserSubagentDelegatesToTheProvider pins the delegation: the task
// reaches the provider verbatim and its answer is the tool's result.
func TestBrowserSubagentDelegatesToTheProvider(t *testing.T) {
	provider := &fakeBrowserSubagentProvider{result: "the page said hello"}
	tool := &BrowserSubagent{Provider: provider}
	out, err := tool.Run(context.Background(), json.RawMessage(`{"task":"open example.com"}`))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("the provider was called %d times, want 1", provider.calls)
	}
	if provider.task != "open example.com" {
		t.Errorf("the provider got task %q, want %q", provider.task, "open example.com")
	}
	if got, _ := out.(string); got != "the page said hello" {
		t.Errorf("result = %v, want %q", out, "the page said hello")
	}
}

// TestBrowserSubagentFallsBackToTheProcessWideProvider pins the seam: a tool
// without its own provider uses the installed one.
func TestBrowserSubagentFallsBackToTheProcessWideProvider(t *testing.T) {
	prev := SetBrowserSubagentProvider(nil)
	t.Cleanup(func() { SetBrowserSubagentProvider(prev) })

	provider := &fakeBrowserSubagentProvider{result: "done"}
	SetBrowserSubagentProvider(provider)
	if got := DefaultBrowserSubagentProvider(); got != provider {
		t.Fatalf("DefaultBrowserSubagentProvider() = %v, want the installed provider", got)
	}
	tool := &BrowserSubagent{}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"task":"look"}`)); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if provider.calls != 1 {
		t.Errorf("the provider was called %d times, want 1", provider.calls)
	}
}

// TestBrowserSubagentWithoutProviderFails pins that a session with no browser
// agent reports the gap rather than pretending the task ran.
func TestBrowserSubagentWithoutProviderFails(t *testing.T) {
	prev := SetBrowserSubagentProvider(nil)
	t.Cleanup(func() { SetBrowserSubagentProvider(prev) })

	tool := &BrowserSubagent{}
	if _, err := tool.Run(context.Background(), json.RawMessage(`{"task":"look"}`)); err == nil {
		t.Fatal("Run = nil error, want one")
	}
}
