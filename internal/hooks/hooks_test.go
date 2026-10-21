package hooks_test

// hooks_test.go — cycle btw-runs-hooks: internal/hooks unit tests.
//
// The tests live in package hooks_test so the dispatch path is
// exercised exactly the way cmd/wagyu will call it. Higher-level
// integration tests (cmd/wagyu/slash_btw_test.go) verify /btw
// actually fires the right hooks; the package-level tests verify
// the dispatch itself.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/hooks"
)

// TestRegisterAndFire is the happy path. A single Allow handler
// runs once; the dispatch returns blocked=false and the payload is
// unchanged.
func TestRegisterAndFire(t *testing.T) {
	m := hooks.NewManager()
	called := 0
	m.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		called++
		if p.Event != hooks.PreSideQuestion {
			t.Errorf("Event = %q, want PreSideQuestion", p.Event)
		}
		if p.Question != "hello" {
			t.Errorf("Question = %q, want %q", p.Question, "hello")
		}
		return hooks.DecisionAllow, "", nil
	})

	blocked, final, reason := m.Fire(context.Background(), hooks.PreSideQuestion,
		hooks.SideQuestion(hooks.PreSideQuestion, "hello"))
	if blocked {
		t.Errorf("blocked=true; want false (reason=%q)", reason)
	}
	if called != 1 {
		t.Errorf("handler called %d times; want 1", called)
	}
	if final.Question != "hello" {
		t.Errorf("final.Question = %q, want %q", final.Question, "hello")
	}
	if m.Count(hooks.PreSideQuestion) != 1 {
		t.Errorf("Count = %d, want 1", m.Count(hooks.PreSideQuestion))
	}
}

// TestBlockStopsDispatch: a handler that returns DecisionBlock
// short-circuits the remaining handlers and reports the reason
// back to the caller.
func TestBlockStopsDispatch(t *testing.T) {
	m := hooks.NewManager()
	first := 0
	second := 0
	m.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		first++
		return hooks.DecisionBlock, "policy: denied", nil
	})
	m.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		second++
		return hooks.DecisionAllow, "", nil
	})

	blocked, final, reason := m.Fire(context.Background(), hooks.PreSideQuestion,
		hooks.SideQuestion(hooks.PreSideQuestion, "anything"))
	if !blocked {
		t.Errorf("blocked=false; want true")
	}
	if reason != "policy: denied" {
		t.Errorf("reason = %q, want %q", reason, "policy: denied")
	}
	if first != 1 {
		t.Errorf("first handler called %d times; want 1", first)
	}
	if second != 0 {
		t.Errorf("second handler called %d times; want 0 (blocked before)", second)
	}
	if !final.Blocked {
		t.Errorf("final.Blocked = false; want true")
	}
	if final.BlockReason != "policy: denied" {
		t.Errorf("final.BlockReason = %q, want %q", final.BlockReason, "policy: denied")
	}
}

// TestModifyReplacesQuestion: DecisionModify swaps the Question
// field with the handler's reason string. Subsequent handlers see
// the modified value.
func TestModifyReplacesQuestion(t *testing.T) {
	m := hooks.NewManager()
	seen := ""
	m.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		return hooks.DecisionModify, "rewritten question", nil
	})
	m.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		seen = p.Question
		return hooks.DecisionAllow, "", nil
	})

	_, final, _ := m.Fire(context.Background(), hooks.PreSideQuestion,
		hooks.SideQuestion(hooks.PreSideQuestion, "original"))
	if final.Question != "rewritten question" {
		t.Errorf("final.Question = %q, want %q", final.Question, "rewritten question")
	}
	if seen != "rewritten question" {
		t.Errorf("second handler saw %q, want %q", seen, "rewritten question")
	}
}

// TestPriorityOrder: handlers with lower priority values fire first;
// on ties, registration order wins. This is the deterministic
// contract a future cycle will rely on.
func TestPriorityOrder(t *testing.T) {
	m := hooks.NewManager()
	order := []string{}
	m.RegisterPriority(hooks.PreSideQuestion, 10, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		order = append(order, "mid")
		return hooks.DecisionAllow, "", nil
	})
	m.RegisterPriority(hooks.PreSideQuestion, 0, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		order = append(order, "first")
		return hooks.DecisionAllow, "", nil
	})
	m.RegisterPriority(hooks.PreSideQuestion, 100, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		order = append(order, "last")
		return hooks.DecisionAllow, "", nil
	})

	if _, _, _ = m.Fire(context.Background(), hooks.PreSideQuestion,
		hooks.SideQuestion(hooks.PreSideQuestion, "x")); false {
	}
	if got := strings.Join(order, ","); got != "first,mid,last" {
		t.Errorf("order = %q, want %q", got, "first,mid,last")
	}
}

// TestErrorShortCircuits: a handler returning a non-nil error
// blocks the dispatch with a synthesized reason.
func TestErrorShortCircuits(t *testing.T) {
	m := hooks.NewManager()
	m.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		return hooks.DecisionAllow, "", errors.New("boom")
	})
	blocked, _, reason := m.Fire(context.Background(), hooks.PreSideQuestion,
		hooks.SideQuestion(hooks.PreSideQuestion, "x"))
	if !blocked {
		t.Errorf("blocked=false; want true on error")
	}
	if !strings.Contains(reason, "boom") {
		t.Errorf("reason = %q, want it to contain %q", reason, "boom")
	}
}

// TestNoHandlers: firing an event with no handlers is a no-op that
// returns blocked=false.
func TestNoHandlers(t *testing.T) {
	m := hooks.NewManager()
	blocked, final, reason := m.Fire(context.Background(), hooks.PostSideQuestion,
		hooks.SideQuestion(hooks.PostSideQuestion, "x"))
	if blocked || reason != "" {
		t.Errorf("no-handler Fire returned blocked=%v reason=%q; want false, empty", blocked, reason)
	}
	if final.Question != "x" {
		t.Errorf("final.Question = %q, want %q", final.Question, "x")
	}
}

// TestCountAndRegistered: Count reports the per-event fire total;
// Registered reports every event with at least one handler.
func TestCountAndRegistered(t *testing.T) {
	m := hooks.NewManager()
	m.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		return hooks.DecisionAllow, "", nil
	})
	m.Register(hooks.PostSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		return hooks.DecisionAllow, "", nil
	})
	for i := 0; i < 3; i++ {
		m.Fire(context.Background(), hooks.PreSideQuestion,
			hooks.SideQuestion(hooks.PreSideQuestion, "x"))
	}
	m.Fire(context.Background(), hooks.PostSideQuestion,
		hooks.SideQuestion(hooks.PostSideQuestion, "x"))

	if c := m.Count(hooks.PreSideQuestion); c != 3 {
		t.Errorf("PreSideQuestion count = %d, want 3", c)
	}
	if c := m.Count(hooks.PostSideQuestion); c != 1 {
		t.Errorf("PostSideQuestion count = %d, want 1", c)
	}
	regs := m.Registered()
	if len(regs) != 2 {
		t.Errorf("Registered() = %v, want 2 entries", regs)
	}
}
