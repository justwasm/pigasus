package celpolicy

import (
	"sync"
	"testing"

	"github.com/google/cel-go/cel"
)

func TestEvalBasic(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	tests := []struct {
		expr string
		vars map[string]any
		want bool
	}{
		{"true", nil, true},
		{"false", nil, false},
		{"1 + 1 == 2", nil, true},
		{"2 * 3 == 5", nil, false},
		{`tool == "read_file"`, map[string]any{"tool": "read_file"}, true},
		{`tool == "run_command"`, map[string]any{"tool": "read_file"}, false},
	}

	for _, tc := range tests {
		got, err := engine.Eval(tc.expr, tc.vars)
		if err != nil {
			t.Errorf("Eval(%q) unexpected err: %v", tc.expr, err)
		} else if got != tc.want {
			t.Errorf("Eval(%q) = %v, want %v", tc.expr, got, tc.want)
		}
	}
}

func TestPathMatching(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	rule := `path.startsWith("/workspace/") && !path.contains("/..") && path.endsWith(".go")`

	allowed, err := engine.Eval(rule, map[string]any{"path": "/workspace/internal/celpolicy/eval.go"})
	if err != nil || !allowed {
		t.Fatalf("expected path to be allowed: %v", err)
	}

	denied, err := engine.Eval(rule, map[string]any{"path": "/workspace/../etc/passwd"})
	if err != nil || denied {
		t.Fatalf("expected path with parent traversal to be denied: %v", err)
	}

	deniedNonGo, err := engine.Eval(rule, map[string]any{"path": "/workspace/image.png"})
	if err != nil || deniedNonGo {
		t.Fatalf("expected non-go path to be denied: %v", err)
	}
}

func TestPathRegexMatching(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	rule := `path.matches("^/tmp/task-[0-9]+\\.log$")`
	got, err := engine.Eval(rule, map[string]any{"path": "/tmp/task-123.log"})
	if err != nil || !got {
		t.Fatalf("expected regex match, got %v, err: %v", got, err)
	}

	got2, err := engine.Eval(rule, map[string]any{"path": "/etc/passwd"})
	if err != nil || got2 {
		t.Fatalf("expected regex non-match, got %v, err: %v", got2, err)
	}
}

func TestToolPermissionsAllow(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	policy := `tool in ["read_file", "list_dir"] || (tool == "run_command" && (command.startsWith("go ") || command.startsWith("git ")))`

	allowRead, err := engine.Eval(policy, map[string]any{"tool": "read_file"})
	if err != nil || !allowRead {
		t.Errorf("read_file should be allowed: %v", err)
	}

	allowGoTest, err := engine.Eval(policy, map[string]any{
		"tool":    "run_command",
		"command": "go test -v ./...",
	})
	if err != nil || !allowGoTest {
		t.Errorf("go test command should be allowed: %v", err)
	}
}

func TestToolPermissionsDeny(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	policy := `tool in ["read_file", "list_dir"] || (tool == "run_command" && (command.startsWith("go ") || command.startsWith("git ")))`

	denyRm, err := engine.Eval(policy, map[string]any{
		"tool":    "run_command",
		"command": "rm -rf /",
	})
	if err != nil || denyRm {
		t.Errorf("rm -rf should be denied: got %v, err %v", denyRm, err)
	}

	denyUnknown, err := engine.Eval(policy, map[string]any{"tool": "delete_all"})
	if err != nil || denyUnknown {
		t.Errorf("unknown tool should be denied: got %v, err %v", denyUnknown, err)
	}
}

func TestEnvVariables(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	rule := `"CI" in env && env["CI"] == "true" && env["USER"] == "developer"`
	vars := map[string]any{
		"env": map[string]string{
			"CI":   "true",
			"USER": "developer",
		},
	}
	got, err := engine.Eval(rule, vars)
	if err != nil || !got {
		t.Fatalf("expected env rule match, got %v, err %v", got, err)
	}

	varsBad := map[string]any{
		"env": map[string]string{
			"CI":   "false",
			"USER": "root",
		},
	}
	gotBad, err := engine.Eval(rule, varsBad)
	if err != nil || gotBad {
		t.Fatalf("expected env rule non-match, got %v, err %v", gotBad, err)
	}
}

func TestCustomDeclarations(t *testing.T) {
	engine, err := NewEngine(
		cel.Variable("user", cel.StringType),
		cel.Variable("sandbox_enabled", cel.BoolType),
	)
	if err != nil {
		t.Fatalf("failed to create engine with custom decls: %v", err)
	}

	rule := `sandbox_enabled && user == "admin"`
	got, err := engine.Eval(rule, map[string]any{
		"sandbox_enabled": true,
		"user":            "admin",
	})
	if err != nil || !got {
		t.Fatalf("expected custom decl match, got %v, err %v", got, err)
	}
}

func TestInvalidSyntax(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	invalidExpressions := []string{
		"tool ==",
		"command.startsWith(",
		"&& true",
		"",
	}

	for _, expr := range invalidExpressions {
		_, err := engine.Eval(expr, nil)
		if err == nil {
			t.Errorf("expected syntax error for %q, got nil", expr)
		}
	}
}

func TestNonBooleanOutput(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	nonBoolExpressions := []string{
		"1 + 2",
		"tool",
		"env",
		"['a', 'b']",
	}

	for _, expr := range nonBoolExpressions {
		_, err := engine.Eval(expr, nil)
		if err == nil {
			t.Errorf("expected non-boolean error for %q, got nil", expr)
		}
	}
}

func TestUndeclaredVariable(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	_, err = engine.Eval("unknown_variable == 123", nil)
	if err == nil {
		t.Fatal("expected error for undeclared variable, got nil")
	}
}

func TestEvalRuleHelper(t *testing.T) {
	got, err := EvalRule(`tool == "run_command" && command == "ls"`, map[string]any{
		"tool":    "run_command",
		"command": "ls",
	})
	if err != nil || !got {
		t.Fatalf("EvalRule failed: got %v, err %v", got, err)
	}

	got2, err := EvalRule(`path.startsWith("/home/")`, map[string]any{
		"path": "/etc/shadow",
	})
	if err != nil || got2 {
		t.Fatalf("EvalRule expected false, got %v, err %v", got2, err)
	}
}

func TestConcurrentEval(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(iterations)

	for i := 0; i < iterations; i++ {
		go func(idx int) {
			defer wg.Done()
			rule := `tool == "run_command" && command.contains("build")`
			v := map[string]any{"tool": "run_command", "command": "go build"}
			res, err := engine.Eval(rule, v)
			if err != nil || !res {
				t.Errorf("goroutine %d failed: res=%v, err=%v", idx, res, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestEngineEnvGetter(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	if engine.Env() == nil {
		t.Fatal("expected non-nil cel.Env from engine.Env()")
	}
}
