// browser_subagent_test.go — the cycle-311 tests for the reference's
// browser_subagent tool.
//
// The reference's browser agent is a real agent: the tool hands it a task
// and the agent drives the browser. The clone's browser agent is a
// subagent whose tool set is the browser-control surface, so the tests pin
// three things: the prompt the agent runs under, the tool set it is given,
// and the delegation itself.
package browsersubagent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"

	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/tools"
)

// fauxModel mirrors subagenttools' own test model: the inline Provider
// field is the seam the runtime uses, so TestFauxProvider reaches Stream
// without an auth handshake.
func fauxModel() *pi.Model {
	return &pi.Model{
		Type:         pi.ModelTypeChat,
		ID:           "faux-1",
		DisplayName:  "faux-1",
		Provider:     &pi.TestFauxProvider{},
		ProviderMeta: pi.ProviderMetadata{ProviderID: "test-faux", API: "test-faux"},
		Input:        []string{"text"},
	}
}

func testServices(t *testing.T) *coding.AgentSessionServices {
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

// TestPromptLeadIsTheReferences pins the one sentence of the browser agent's
// prompt that is recoverable from the reference's binary: its opening line
// ("You are a browser agent.", the literal at 0x1035588eb).
func TestPromptLeadIsTheReferences(t *testing.T) {
	if PromptLead != "You are a browser agent." {
		t.Fatalf("PromptLead = %q, want the reference's sentence", PromptLead)
	}
	prompt := Prompt(nil, SurfaceManaged)
	if !strings.HasPrefix(prompt, PromptLead) {
		t.Errorf("the prompt does not open with the reference's sentence: %q", prompt)
	}
}

// TestPromptListsTheTools pins that the prompt tells the agent what it can
// call — the browser inventory the reference's browser agent carries.
func TestPromptListsTheTools(t *testing.T) {
	prompt := Prompt([]string{"browser_click_element — click a thing"}, SurfaceManaged)
	if !strings.Contains(prompt, "browser_click_element") {
		t.Errorf("the prompt does not mention the tools: %q", prompt)
	}
	if !strings.Contains(prompt, "Tools:") {
		t.Errorf("the prompt has no tools section: %q", prompt)
	}
}

// TestBrowserToolsAreTheCatalogueBrowserTools pins the tool set: the browser
// tools out of the catalogue, and nothing else.
func TestBrowserToolsAreTheCatalogueBrowserTools(t *testing.T) {
	reg := tools.Catalog(t.TempDir())
	got := BrowserTools(reg)
	if len(got) != len(BrowserToolNames) {
		t.Fatalf("BrowserTools returned %d tools, want %d", len(got), len(BrowserToolNames))
	}
	for _, tool := range got {
		name := tool.Name()
		if !strings.HasPrefix(name, "browser_") &&
			!strings.HasPrefix(name, "capture_browser_") &&
			!strings.HasPrefix(name, "click_browser_") &&
			!strings.HasPrefix(name, "execute_browser_") &&
			!strings.HasPrefix(name, "list_browser_") &&
			!strings.HasPrefix(name, "open_browser_") &&
			!strings.HasPrefix(name, "read_browser_") {
			t.Errorf("%s is not a browser tool", name)
		}
	}
	if _, ok := reg.Get("browser_subagent"); !ok {
		t.Fatal("the catalogue does not advertise browser_subagent")
	}
	for _, tool := range got {
		if tool.Name() == "browser_subagent" {
			t.Error("the browser agent was given the browser_subagent tool itself")
		}
	}
}

// TestBrowserToolsSkipsAMissingRegistry pins that a restricted registry
// yields fewer tools rather than a panic.
func TestBrowserToolsSkipsAMissingRegistry(t *testing.T) {
	if got := BrowserTools(nil); got != nil {
		t.Errorf("BrowserTools(nil) = %v, want nil", got)
	}
	reg := tools.NewRegistry()
	if got := BrowserTools(reg); len(got) != 0 {
		t.Errorf("BrowserTools(empty) = %v, want none", got)
	}
}

// TestRunBrowserTaskRequiresATask pins that an empty task is refused before
// a session is created.
func TestRunBrowserTaskRequiresATask(t *testing.T) {
	runner := New(subagenttools.Deps{Services: testServices(t), Model: fauxModel()}, nil)
	if _, err := runner.RunBrowserTask(context.Background(), "  "); err == nil {
		t.Fatal("RunBrowserTask(\"\") = nil error, want one")
	}
}

// TestRunBrowserTaskDelegatesToTheBrowserAgent runs the whole path: the
// browser agent gets the task and its answer comes back as the tool's
// result.
//
// The faux provider answers "What is 20+22?" with 42, so the assertion does
// not depend on model nondeterminism.
func TestRunBrowserTaskDelegatesToTheBrowserAgent(t *testing.T) {
	deps := subagenttools.Deps{
		Services: testServices(t),
		Model:    fauxModel(),
		CWD:      t.TempDir(),
	}
	runner := New(deps, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	out, err := runner.RunBrowserTask(ctx, "What is 20+22?")
	if err != nil {
		t.Fatalf("RunBrowserTask: %v", err)
	}
	if !strings.Contains(out, "42") {
		t.Errorf("RunBrowserTask returned %q, want it to contain %q", out, "42")
	}
}
