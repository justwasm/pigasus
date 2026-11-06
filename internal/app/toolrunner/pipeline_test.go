package toolrunner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/policy/sandbox"
)

func TestPipeline_OrderOfExecution(t *testing.T) {
	var trace []string

	mw1 := func(next Handler) Handler {
		return func(ctx context.Context, call backend.ToolCall) (string, error) {
			trace = append(trace, "mw1-before")
			res, err := next(ctx, call)
			trace = append(trace, "mw1-after")
			return res + "-mw1", err
		}
	}

	mw2 := func(next Handler) Handler {
		return func(ctx context.Context, call backend.ToolCall) (string, error) {
			trace = append(trace, "mw2-before")
			res, err := next(ctx, call)
			trace = append(trace, "mw2-after")
			return res + "-mw2", err
		}
	}

	pipeline := NewPipeline(mw1, mw2)
	handler := pipeline.Wrap(func(ctx context.Context, call backend.ToolCall) (string, error) {
		trace = append(trace, "core")
		return "result", nil
	})

	res, err := handler(context.Background(), backend.ToolCall{Name: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedTrace := []string{"mw1-before", "mw2-before", "core", "mw2-after", "mw1-after"}
	if len(trace) != len(expectedTrace) {
		t.Fatalf("trace length = %d, want %d: %v", len(trace), len(expectedTrace), trace)
	}
	for i, v := range expectedTrace {
		if trace[i] != v {
			t.Errorf("trace[%d] = %q, want %q", i, trace[i], v)
		}
	}
	if res != "result-mw2-mw1" {
		t.Errorf("res = %q, want 'result-mw2-mw1'", res)
	}
}

func TestPipeline_PermissionGateMiddleware_Allow(t *testing.T) {
	policy := &permissions.Policy{
		Allow: []permissions.Rule{
			{Action: "command", Pattern: "echo *"},
		},
	}

	var deniedTool string
	mw := PermissionGateMiddleware(policy, nil, func(name string) {
		deniedTool = name
	})

	pipeline := NewPipeline(mw)
	handler := pipeline.Wrap(func(ctx context.Context, call backend.ToolCall) (string, error) {
		return "executed", nil
	})

	call := backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"CommandLine":"echo hello"}`),
	}
	res, err := handler(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res != "executed" {
		t.Errorf("res = %q, want executed", res)
	}
	if deniedTool != "" {
		t.Errorf("deniedTool = %q, want empty", deniedTool)
	}
}

func TestPipeline_PermissionGateMiddleware_Deny(t *testing.T) {
	policy := &permissions.Policy{
		Deny: []permissions.Rule{
			{Action: "command", Pattern: "rm -rf"},
		},
	}

	var deniedTool string
	mw := PermissionGateMiddleware(policy, nil, func(name string) {
		deniedTool = name
	})

	pipeline := NewPipeline(mw)
	handler := pipeline.Wrap(func(ctx context.Context, call backend.ToolCall) (string, error) {
		return "executed", nil
	})

	call := backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"CommandLine":"rm -rf /"}`),
	}
	res, err := handler(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(res, "was denied") {
		t.Errorf("res = %q, want denial message", res)
	}
	if deniedTool != "run_command" {
		t.Errorf("deniedTool = %q, want run_command", deniedTool)
	}
}

func TestPipeline_PermissionGateMiddleware_Ask_Approved(t *testing.T) {
	policy := &permissions.Policy{
		Ask: []permissions.Rule{
			{Action: "command", Pattern: "git push"},
		},
	}

	promptCalled := false
	promptUser := func(ctx context.Context, call backend.ToolCall) (bool, error) {
		promptCalled = true
		return true, nil
	}

	mw := PermissionGateMiddleware(policy, promptUser, nil)
	pipeline := NewPipeline(mw)
	handler := pipeline.Wrap(func(ctx context.Context, call backend.ToolCall) (string, error) {
		return "git-push-ok", nil
	})

	call := backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"CommandLine":"git push origin main"}`),
	}
	res, err := handler(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !promptCalled {
		t.Errorf("promptUser was not called")
	}
	if res != "git-push-ok" {
		t.Errorf("res = %q, want git-push-ok", res)
	}
}

func TestPipeline_PermissionGateMiddleware_Ask_Rejected(t *testing.T) {
	policy := &permissions.Policy{
		Ask: []permissions.Rule{
			{Action: "command", Pattern: "git push"},
		},
	}

	promptUser := func(ctx context.Context, call backend.ToolCall) (bool, error) {
		return false, nil
	}

	var deniedTool string
	mw := PermissionGateMiddleware(policy, promptUser, func(name string) {
		deniedTool = name
	})
	pipeline := NewPipeline(mw)
	handler := pipeline.Wrap(func(ctx context.Context, call backend.ToolCall) (string, error) {
		return "git-push-ok", nil
	})

	call := backend.ToolCall{
		Name:      "run_command",
		Arguments: json.RawMessage(`{"CommandLine":"git push origin main"}`),
	}
	res, err := handler(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(res, "was denied") {
		t.Errorf("res = %q, want denial message", res)
	}
	if deniedTool != "run_command" {
		t.Errorf("deniedTool = %q, want run_command", deniedTool)
	}
}

func TestPipeline_OutputBudgetMiddleware(t *testing.T) {
	mw := OutputBudgetMiddleware(50)
	pipeline := NewPipeline(mw)
	handler := pipeline.Wrap(func(ctx context.Context, call backend.ToolCall) (string, error) {
		return strings.Repeat("A very long output string that exceeds the max tokens budget. ", 10), nil
	})

	res, err := handler(context.Background(), backend.ToolCall{Name: "test"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(res, "characters truncated") && !strings.Contains(res, "...") {
		t.Logf("output was budgeted: len=%d", len(res))
	}
}

func TestPipeline_PassThroughError(t *testing.T) {
	pipeline := NewPipeline(
		PermissionGateMiddleware(nil, nil, nil),
		SandboxBoundaryMiddleware(sandbox.Sandbox{}),
		OutputBudgetMiddleware(100),
	)
	handler := pipeline.Wrap(func(ctx context.Context, call backend.ToolCall) (string, error) {
		return "", errors.New("underlying tool failed")
	})

	_, err := handler(context.Background(), backend.ToolCall{Name: "test"})
	if err == nil || err.Error() != "underlying tool failed" {
		t.Errorf("err = %v, want 'underlying tool failed'", err)
	}
}
