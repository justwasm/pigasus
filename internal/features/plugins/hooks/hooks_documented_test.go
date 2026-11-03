package hooks_test

// hooks_documented_test.go — the documented events on the bus.
//
// The bus carries the six documented events (SessionStart, PreToolUse,
// PostToolUse, PreInvocation, PostInvocation and Stop) next to its own
// in-process ones, and FireDocumented hands an event to the in-process
// handlers first and to the documented handlers second.

import (
	"context"
	"testing"

	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
)

// TestTheDocumentedEventNames pins the bus's own spelling of the six
// documented events. The names belong to the core tier, so the two
// cannot drift apart.
func TestTheDocumentedEventNames(t *testing.T) {
	cases := map[hooks.Event]string{
		hooks.SessionStart:   "SessionStart",
		hooks.PreToolUse:     "PreToolUse",
		hooks.PostToolUse:    "PostToolUse",
		hooks.PreInvocation:  "PreInvocation",
		hooks.PostInvocation: "PostInvocation",
		hooks.Stop:           "Stop",
	}
	for ev, want := range cases {
		if string(ev) != want {
			t.Errorf("the bus spells %q, want %q", ev, want)
		}
	}
}

// TestFireDocumentedRunsTheDocumentedHandlers pins the dispatch: the
// documented handlers run through the runtime, and their answer is what
// the caller gets.
func TestFireDocumentedRunsTheDocumentedHandlers(t *testing.T) {
	m := hooks.NewManager()
	rt := &corehooks.Runtime{
		Registry: &corehooks.Registry{Entries: []corehooks.Entry{{
			Hook:    "gate",
			Event:   corehooks.EventPreToolUse,
			Matcher: "run_command",
			Handler: corehooks.Handler{Type: "command", Command: `echo '{"decision":"deny","reason":"no"}'`},
		}}},
		Shell: func(command string) []string { return []string{"/bin/sh", "-c", command} },
	}
	out, err := m.FireDocumented(context.Background(), rt, corehooks.EventPreToolUse,
		corehooks.Input{ToolCall: &corehooks.ToolCall{Name: "run_command"}})
	if err != nil {
		t.Fatalf("FireDocumented: %v", err)
	}
	if out.Decision != corehooks.DecisionDeny {
		t.Errorf("decision = %q, want deny", out.Decision)
	}
	if out.Reason != "no" {
		t.Errorf("reason = %q, want no", out.Reason)
	}
}

// TestFireDocumentedRunsTheInProcessHandlersFirst pins the order: an
// in-process handler that blocks stops the call before the documented
// handlers run.
func TestFireDocumentedRunsTheInProcessHandlersFirst(t *testing.T) {
	m := hooks.NewManager()
	m.Register(hooks.PreToolUse, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		return hooks.DecisionBlock, "in-process", nil
	})
	rt := &corehooks.Runtime{
		Registry: &corehooks.Registry{Entries: []corehooks.Entry{{
			Hook:    "gate",
			Event:   corehooks.EventPreToolUse,
			Handler: corehooks.Handler{Type: "command", Command: `echo '{"decision":"allow"}'`},
		}}},
		Shell: func(command string) []string { return []string{"/bin/sh", "-c", command} },
	}
	out, err := m.FireDocumented(context.Background(), rt, corehooks.EventPreToolUse, corehooks.Input{})
	if err != nil {
		t.Fatalf("FireDocumented: %v", err)
	}
	if out.Decision != corehooks.DecisionDeny {
		t.Errorf("decision = %q, want deny", out.Decision)
	}
	if out.Reason != "in-process" {
		t.Errorf("reason = %q, want in-process", out.Reason)
	}
}

// TestFireDocumentedIgnoresAnUnknownEvent pins the dispatch's answer to
// an event the documentation does not name: nothing happens.
func TestFireDocumentedIgnoresAnUnknownEvent(t *testing.T) {
	m := hooks.NewManager()
	out, err := m.FireDocumented(context.Background(), nil, corehooks.Event("Nope"), corehooks.Input{})
	if err != nil {
		t.Fatalf("FireDocumented: %v", err)
	}
	if out.Ran != 0 || out.Decision != "" {
		t.Errorf("outcome = %+v, want nothing", out)
	}
}
