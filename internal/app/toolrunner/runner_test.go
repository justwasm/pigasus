package toolrunner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
)

type mockToolRunner struct {
	ran bool
}

func (m *mockToolRunner) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	m.ran = true
	return "ok", nil
}

func TestPermissionRunner_PromptUserApproval(t *testing.T) {
	inner := &mockToolRunner{}
	pol := &permissions.Policy{
		Ask: []permissions.Rule{
			{Action: "write_file", Pattern: "test.go"},
		},
	}

	promptCalled := false
	runner := &PermissionRunner{
		Inner:  inner,
		Policy: pol,
		PromptUser: func(ctx context.Context, call backend.ToolCall) (bool, error) {
			promptCalled = true
			return true, nil
		},
	}

	call := backend.ToolCall{
		Name:      "write_to_file",
		Arguments: json.RawMessage(`{"path":"test.go","content":"package main"}`),
	}

	res, err := runner.Run(context.Background(), call)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !promptCalled {
		t.Errorf("expected PromptUser to be called")
	}
	if !inner.ran || res != "ok" {
		t.Errorf("expected inner runner to execute after approval")
	}
}
