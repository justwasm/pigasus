package subagenttools

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	builtinsubagent "github.com/justwasm/pigasus/internal/app/subagent/builtin"
)

// resetBuiltinRegistry ensures the singleton is the package-default
// before each test runs.
func resetBuiltinRegistry(t *testing.T) {
	t.Helper()
	SetBuiltinSubagentRegistry(builtinsubagent.Default())
}

func TestInvokeSubagentBuiltinRouting_ImageGenerator(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	resetBuiltinRegistry(t)

	tool := &invokeTool{deps: Deps{
		CWD: t.TempDir(),
	}}

	args, _ := json.Marshal(invokeArgs{
		AgentName: "image-generator",
		Prompt:    "a calm orange sunset over still water",
	})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(res.Output), &env); err != nil {
		t.Fatalf("envelope unmarshal: %v (raw=%q)", err, res.Output)
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

func TestInvokeSubagentBuiltinRouting_UnknownName(t *testing.T) {
	resetBuiltinRegistry(t)
	tool := &invokeTool{deps: Deps{
		CWD: t.TempDir(),
	}}

	args, _ := json.Marshal(invokeArgs{
		AgentName: "ghost-subagent",
		Prompt:    "x",
	})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(res.Output), &env); err != nil {
		t.Fatalf("envelope unmarshal: %v (raw=%q)", err, res.Output)
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

func TestInvokeSubagentBuiltinRouting_SchemaMentionsBuiltins(t *testing.T) {
	resetBuiltinRegistry(t)
	tool := &invokeTool{}
	def := tool.Definition()
	if !strings.Contains(def.Description, "image-generator") {
		t.Errorf("def.Description does not mention image-generator:\n%s", def.Description)
	}
	if !strings.Contains(def.Description, "Built-in subagents") {
		t.Errorf("def.Description does not include the catalogue block:\n%s", def.Description)
	}
	props, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("def.Parameters properties is %T, want map[string]any", def.Parameters["properties"])
	}
	agentName, ok := props["agent_name"].(map[string]any)
	if !ok {
		t.Fatalf("def.Parameters missing agent_name entry: %+v", props)
	}
	if desc, _ := agentName["description"].(string); !strings.Contains(desc, "image-generator") {
		t.Errorf("agent_name.description does not reference image-generator:\n%s", desc)
	}
}

func TestInvokeSubagentBuiltinRouting_TypeNameReachesTheBuiltin(t *testing.T) {
	resetBuiltinRegistry(t)

	reg := builtinsubagent.NewRegistry()
	var gotPrompt string
	if err := reg.Register(builtinsubagent.Spec{
		Name:        builtinsubagent.KindBrowser,
		Description: "the browser agent",
		Handler: func(_ context.Context, prompt string) (builtinsubagent.Result, error) {
			gotPrompt = prompt
			return builtinsubagent.Result{Text: "the page said Example Domain"}, nil
		},
	}); err != nil {
		t.Fatalf("register the fake browser agent: %v", err)
	}
	SetBuiltinSubagentRegistry(reg)
	t.Cleanup(func() { resetBuiltinRegistry(t) })

	tool := &invokeTool{deps: Deps{
		CWD: t.TempDir(),
	}}

	args, _ := json.Marshal(invokeArgs{
		TypeName: string(builtinsubagent.KindBrowser),
		Prompt:   "open https://example.com and report the title",
	})
	res, err := tool.Execute(context.Background(), string(args))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if gotPrompt != "open https://example.com and report the title" {
		t.Errorf("the handler saw %q", gotPrompt)
	}
	var env map[string]any
	if err := json.Unmarshal([]byte(res.Output), &env); err != nil {
		t.Fatalf("envelope unmarshal: %v (raw=%q)", err, res.Output)
	}
	if ok, _ := env["ok"].(bool); !ok {
		t.Fatalf("envelope.ok = %v, want true", env["ok"])
	}
	if name, _ := env["agent_name"].(string); name != string(builtinsubagent.KindBrowser) {
		t.Errorf("envelope.agent_name = %q, want %q", name, builtinsubagent.KindBrowser)
	}
	if text, _ := env["text"].(string); !strings.Contains(text, "Example Domain") {
		t.Errorf("envelope.text = %q, want the agent's answer", text)
	}
}

func TestSetBuiltinSubagentRegistrySurvivesTheFirstRead(t *testing.T) {
	builtinRegistryMu.Lock()
	oldOnce, oldReg := builtinRegistryOnce, builtinRegistry
	builtinRegistryOnce, builtinRegistry = sync.Once{}, nil
	builtinRegistryMu.Unlock()
	t.Cleanup(func() {
		builtinRegistryMu.Lock()
		builtinRegistryOnce, builtinRegistry = oldOnce, oldReg
		builtinRegistryMu.Unlock()
	})

	reg := builtinsubagent.Default()
	if err := reg.Register(builtinsubagent.Spec{
		Name:        builtinsubagent.KindBrowser,
		Description: "the browser agent",
		Handler: func(_ context.Context, _ string) (builtinsubagent.Result, error) {
			return builtinsubagent.Result{Text: "ok"}, nil
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	SetBuiltinSubagentRegistry(reg)

	if catalog := BuiltinSubagentCatalog(); !strings.Contains(catalog, string(builtinsubagent.KindBrowser)) {
		t.Errorf("the installed registry did not survive the first read; catalogue:\n%s", catalog)
	}
}
