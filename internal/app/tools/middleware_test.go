package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type mockTool struct {
	name   string
	desc   string
	schema json.RawMessage
	runFn  func(ctx context.Context, args json.RawMessage) (any, error)
}

func (m *mockTool) Name() string            { return m.name }
func (m *mockTool) Schema() json.RawMessage { return m.schema }
func (m *mockTool) Description() string     { return m.desc }
func (m *mockTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	if m.runFn != nil {
		return m.runFn(ctx, args)
	}
	return "ok", nil
}

func TestMiddleware_ChainAndOrder(t *testing.T) {
	var callOrder []string

	mw1 := func(next Tool) Tool {
		return &middlewareTool{
			inner: next,
			run: func(ctx context.Context, args json.RawMessage) (any, error) {
				callOrder = append(callOrder, "mw1_before")
				res, err := next.Run(ctx, args)
				callOrder = append(callOrder, "mw1_after")
				return res, err
			},
		}
	}

	mw2 := func(next Tool) Tool {
		return &middlewareTool{
			inner: next,
			run: func(ctx context.Context, args json.RawMessage) (any, error) {
				callOrder = append(callOrder, "mw2_before")
				res, err := next.Run(ctx, args)
				callOrder = append(callOrder, "mw2_after")
				return res, err
			},
		}
	}

	tool := &mockTool{
		name: "test_tool",
		runFn: func(ctx context.Context, args json.RawMessage) (any, error) {
			callOrder = append(callOrder, "tool_run")
			return "done", nil
		},
	}

	wrapped := Wrap(tool, mw1, mw2)
	res, err := wrapped.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "done" {
		t.Fatalf("unexpected result: %v", res)
	}

	wantOrder := []string{"mw1_before", "mw2_before", "tool_run", "mw2_after", "mw1_after"}
	if len(callOrder) != len(wantOrder) {
		t.Fatalf("order length mismatch: got %v, want %v", callOrder, wantOrder)
	}
	for i := range wantOrder {
		if callOrder[i] != wantOrder[i] {
			t.Errorf("callOrder[%d] = %q, want %q", i, callOrder[i], wantOrder[i])
		}
	}
}

func TestMiddleware_UnwrapTool(t *testing.T) {
	base := &mockTool{name: "base_tool", desc: "base description"}
	wrapped1 := Wrap(base, ValidationMiddleware())
	wrapped2 := Wrap(wrapped1, RecoveryMiddleware())

	if UnwrapTool(wrapped2) != base {
		t.Errorf("UnwrapTool did not recover base tool")
	}
	if Describe(wrapped2) != "base description" {
		t.Errorf("Describe(wrapped2) = %q, want %q", Describe(wrapped2), "base description")
	}
}

func TestValidationMiddleware(t *testing.T) {
	executed := false
	tool := &mockTool{
		name: "json_checker",
		runFn: func(ctx context.Context, args json.RawMessage) (any, error) {
			executed = true
			return "processed", nil
		},
	}

	valMW := ValidationMiddleware()
	wrapped := Wrap(tool, valMW)

	// Case 1: valid JSON passes
	res, err := wrapped.Run(context.Background(), json.RawMessage(`{"key": "value"}`))
	if err != nil || res != "processed" || !executed {
		t.Fatalf("valid JSON failed: res=%v, err=%v, executed=%v", res, err, executed)
	}

	// Case 2: empty JSON passes
	executed = false
	res, err = wrapped.Run(context.Background(), nil)
	if err != nil || res != "processed" || !executed {
		t.Fatalf("empty JSON failed: res=%v, err=%v, executed=%v", res, err, executed)
	}

	// Case 3: malformed JSON rejected upfront
	executed = false
	res, err = wrapped.Run(context.Background(), json.RawMessage(`{invalid_json`))
	if err == nil {
		t.Fatal("expected error on malformed JSON, got nil")
	}
	if executed {
		t.Fatal("tool should not have been executed on invalid JSON")
	}
	if !strings.Contains(err.Error(), "invalid JSON arguments") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestRecoveryMiddleware(t *testing.T) {
	var panickedTool string
	var panicVal any

	recMW := RecoveryMiddleware(func(tool string, p any) {
		panickedTool = tool
		panicVal = p
	})

	tool := &mockTool{
		name: "panicky_tool",
		runFn: func(ctx context.Context, args json.RawMessage) (any, error) {
			panic("something broke violently")
		},
	}

	wrapped := Wrap(tool, recMW)
	res, err := wrapped.Run(context.Background(), json.RawMessage(`{}`))
	if res != nil {
		t.Fatalf("expected nil result on panic, got %v", res)
	}
	if err == nil {
		t.Fatal("expected error on panic, got nil")
	}
	if !strings.Contains(err.Error(), "panicked: something broke violently") {
		t.Fatalf("unexpected error format: %v", err)
	}
	if panickedTool != "panicky_tool" || panicVal != "something broke violently" {
		t.Fatalf("onPanic callback mismatch: tool=%s, val=%v", panickedTool, panicVal)
	}
}

func TestLoggingMiddleware(t *testing.T) {
	var loggedRecord *AuditRecord
	logger := AuditLogFn(func(rec AuditRecord) {
		loggedRecord = &rec
	})

	tool := &mockTool{
		name: "audit_tool",
		runFn: func(ctx context.Context, args json.RawMessage) (any, error) {
			time.Sleep(10 * time.Millisecond)
			return "audit_result", nil
		},
	}

	logMW := LoggingMiddleware(logger)
	wrapped := Wrap(tool, logMW)

	_, err := wrapped.Run(context.Background(), json.RawMessage(`{"action":"test"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if loggedRecord == nil {
		t.Fatal("expected audit record to be logged")
	}
	if loggedRecord.Tool != "audit_tool" {
		t.Errorf("logged tool = %s, want audit_tool", loggedRecord.Tool)
	}
	if string(loggedRecord.Arguments) != `{"action":"test"}` {
		t.Errorf("logged args = %s", string(loggedRecord.Arguments))
	}
	if loggedRecord.Duration < 5*time.Millisecond {
		t.Errorf("duration too short: %v", loggedRecord.Duration)
	}
	if !loggedRecord.Success {
		t.Errorf("expected success to be true")
	}
}

func TestTimeoutMiddleware(t *testing.T) {
	slowTool := &mockTool{
		name: "slow_tool",
		runFn: func(ctx context.Context, args json.RawMessage) (any, error) {
			select {
			case <-time.After(100 * time.Millisecond):
				return "finished", nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	timeoutMW := TimeoutMiddleware(20 * time.Millisecond)
	wrapped := Wrap(slowTool, timeoutMW)

	_, err := wrapped.Run(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected deadline exceeded error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got: %v", err)
	}
}

func TestRegistry_UseAndDispatch(t *testing.T) {
	reg := NewRegistry()

	tool1 := &mockTool{name: "tool_1", desc: "first tool"}
	reg.Register(tool1)

	var logged []string
	logger := AuditLogFn(func(rec AuditRecord) {
		logged = append(logged, rec.Tool)
	})

	// Add middlewares to registry (should re-wrap tool1 and apply to future tool2)
	reg.Use(ValidationMiddleware(), RecoveryMiddleware(), LoggingMiddleware(logger))

	tool2 := &mockTool{name: "tool_2", desc: "second tool"}
	reg.Register(tool2)

	// Dispatch tool 1
	out1, err := reg.Dispatch(context.Background(), "tool_1", json.RawMessage(`{"test":1}`))
	if err != nil || out1 != "ok" {
		t.Fatalf("dispatch tool_1 failed: out=%v, err=%v", out1, err)
	}

	// Dispatch tool 2 with invalid JSON
	_, err = reg.Dispatch(context.Background(), "tool_2", json.RawMessage(`{bad_json`))
	if err == nil {
		t.Fatal("expected invalid JSON error on tool_2")
	}

	// Check audit records
	if len(logged) != 1 || logged[0] != "tool_1" {
		t.Fatalf("unexpected audit log: %v", logged)
	}

	// Descriptions should still be accessible
	t1, ok := reg.Get("tool_1")
	if !ok || Describe(t1) != "first tool" {
		t.Fatalf("description lost: ok=%v, desc=%q", ok, Describe(t1))
	}
}
