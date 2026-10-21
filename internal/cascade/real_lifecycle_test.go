package cascade

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
)

// TestState_SubagentAwaitsApproval verifies the recovered
// TriggerSubagentApprovalFast path. The 1.3.5 binary calls
// `TriggerSubagentApprovalFast` whenever a subagent needs tool
// confirmation; the cascader transitions to CascaderAwaitApproval and
// the approval sits in the queue until the user calls
// ApproveSubagent / RejectSubagent.
func TestState_SubagentAwaitsApproval(t *testing.T) {
	s := NewState()
	if got := s.Current(); got != CascaderInit {
		t.Fatalf("expected CascaderInit, got %s", got)
	}

	id, appr := s.SubagentAwaitsApproval("sub-1", ApprovalToolConfirm, "run_command", "")
	if id == "" {
		t.Fatal("expected non-empty approval id")
	}
	if appr.SubagentID != "sub-1" {
		t.Fatalf("expected sub-1, got %s", appr.SubagentID)
	}
	if appr.Kind != ApprovalToolConfirm {
		t.Fatalf("expected ApprovalToolConfirm, got %s", appr.Kind)
	}
	if got := s.Current(); got != CascaderAwaitApproval {
		t.Fatalf("expected CascaderAwaitApproval, got %s", got)
	}

	pending := s.PendingApprovals()
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].ID != id {
		t.Fatalf("expected id %s, got %s", id, pending[0].ID)
	}

	// Approve it.
	resolved, err := s.ApproveSubagent(id)
	if err != nil {
		t.Fatalf("ApproveSubagent: %v", err)
	}
	if resolved.ID != id {
		t.Fatalf("expected %s, got %s", id, resolved.ID)
	}
	if got := s.Current(); got != CascaderStream {
		t.Fatalf("expected CascaderStream after approve, got %s", got)
	}
	if got := len(s.PendingApprovals()); got != 0 {
		t.Fatalf("expected 0 pending after approve, got %d", got)
	}
}

// TestState_RejectSubagent verifies the dual of ApproveSubagent: the
// cascader transitions to CascaderError and the approval is removed
// from the queue.
func TestState_RejectSubagent(t *testing.T) {
	s := NewState()
	id, _ := s.SubagentAwaitsApproval("sub-1", ApprovalUserInput, "", "what is the time?")
	if id == "" {
		t.Fatal("expected non-empty approval id")
	}
	if got := s.Current(); got != CascaderAwaitApproval {
		t.Fatalf("expected CascaderAwaitApproval, got %s", got)
	}
	if _, err := s.RejectSubagent(id); err != nil {
		t.Fatalf("RejectSubagent: %v", err)
	}
	if got := s.Current(); got != CascaderError {
		t.Fatalf("expected CascaderError after reject, got %s", got)
	}
	if got := len(s.PendingApprovals()); got != 0 {
		t.Fatalf("expected 0 pending after reject, got %d", got)
	}
}

// TestState_ApproveUnknownID ensures we surface an error when the
// approval id is not in the queue (recovered 1.3.5 code path:
// `(*Store).RespondToSubagentToolConfirmation` returns the same
// "no such approval" error).
func TestState_ApproveUnknownID(t *testing.T) {
	s := NewState()
	if _, err := s.ApproveSubagent("nonexistent"); err == nil {
		t.Fatal("expected error for unknown approval id")
	}
	if _, err := s.RejectSubagent("nonexistent"); err == nil {
		t.Fatal("expected error for unknown approval id")
	}
}

// TestRealLifecycle_HappyPath verifies the recovered `cascader.Run`
// lifecycle. We wire a Mock client + a fresh Lifecycle, then drive
// CascaderRun and assert the cascader reaches CascaderDone.
func TestRealLifecycle_HappyPath(t *testing.T) {
	mock := backend.NewMock()
	lc, err := backend.NewLifecycle(backend.LifecycleConfig{
		Client:       mock,
		Sink:         backend.NewTextSink(nil),
		DefaultModel: "claude-4.5-sonnet",
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	rl := NewRealLifecycle(lc)
	defer rl.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rl.CascaderRun(ctx, "claude-4.5-sonnet", "hello"); err != nil {
		t.Fatalf("CascaderRun: %v", err)
	}
	if got := rl.State.Current(); got != CascaderDone {
		t.Fatalf("expected CascaderDone, got %s", got)
	}
	// Verify the chain renders all expected states.
	hist := rl.State.History()
	if len(hist) < 4 {
		t.Fatalf("expected >=4 history entries, got %d", len(hist))
	}
	// Build the chain string and assert it includes "ready" and
	// "done" at minimum.
	chain := renderChain(hist)
	for _, want := range []string{"init", "ready", "execute", "save", "done"} {
		if !strings.Contains(chain, want) {
			t.Fatalf("chain %q missing %s", chain, want)
		}
	}
}

// TestRealLifecycle_CloseIsIdempotent asserts that the recovered
// `(*CascadeRunState).closeSubagentForwarder` path is idempotent: a
// second Close() is a no-op (the decompiled thunk at 0x76caa60 has
// the same property).
func TestRealLifecycle_CloseIsIdempotent(t *testing.T) {
	mock := backend.NewMock()
	lc, err := backend.NewLifecycle(backend.LifecycleConfig{
		Client:       mock,
		Sink:         backend.NewTextSink(nil),
		DefaultModel: "claude-4.5-sonnet",
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	rl := NewRealLifecycle(lc)
	if err := rl.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := rl.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// renderChain is a small helper that flattens a Transition log to a
// single arrow-separated string. Used by the test above.
func renderChain(hist []Transition) string {
	seen := make(map[CascaderState]bool)
	parts := []string{}
	for _, t := range hist {
		if !seen[t.To] {
			seen[t.To] = true
			parts = append(parts, string(t.To))
		}
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " -> "
		}
		out += p
	}
	return out
}
