// parentcontext_test.go — what a subagent must not lose when its own
// context is compacted (agy 1.4.10).
package subagenttools

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/agentmessage"
)

// TestParentContextKeepsTheTaskAndTheID pins the three things the 1.4.10
// note names: the task the parent gave, the parent's id, and the parent's
// messages.
func TestParentContextKeepsTheTaskAndTheID(t *testing.T) {
	at := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	pctx := &ParentContext{
		ID:     "73ab3617-5a99-4e26-b1f1-c88e712efe39",
		Task:   "Write the release notes",
		TaskAt: at,
	}
	pctx.Record("the first draft is in the file", at.Add(time.Minute))

	summary := pctx.Summary()
	for _, want := range []string{
		"73ab3617-5a99-4e26-b1f1-c88e712efe39",
		"Write the release notes",
		"the first draft is in the file",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary is missing %q:\n%s", want, summary)
		}
	}
	// The task travels in the reference's own envelope: the child
	// recognises the message it was given.
	if !strings.Contains(summary, agentmessage.Preamble) {
		t.Fatalf("the task is not in the reference's envelope:\n%s", summary)
	}
	if !strings.Contains(summary, agentmessage.LineFormat[:9]) {
		t.Fatalf("the parent's later message is not in the reference's line format:\n%s", summary)
	}
}

// TestParentContextKeepsTenMessages pins the count the 1.4.10 note names.
func TestParentContextKeepsTenMessages(t *testing.T) {
	pctx := &ParentContext{ID: "parent"}
	for i := 0; i < 14; i++ {
		pctx.Record("message", time.Unix(int64(i), 0))
	}
	if len(pctx.Messages) != MaxParentMessages {
		t.Fatalf("kept %d messages, want %d", len(pctx.Messages), MaxParentMessages)
	}
	// The oldest ones are the ones dropped.
	summary := pctx.Summary()
	if got := strings.Count(summary, "content=message"); got != MaxParentMessages {
		t.Fatalf("summary carries %d messages, want %d:\n%s", got, MaxParentMessages, summary)
	}
}

// TestParentContextRecordIgnoresEmpty pins that an empty message does not
// consume one of the ten slots.
func TestParentContextRecordIgnoresEmpty(t *testing.T) {
	pctx := &ParentContext{ID: "parent"}
	pctx.Record("   ", time.Now())
	pctx.Record("", time.Now())
	if len(pctx.Messages) != 0 {
		t.Fatalf("kept %d empty messages, want 0", len(pctx.Messages))
	}
}

// TestParentContextOfUsesTheRegistry pins the parent's own id: the
// registry knows it, and the dependency's own id is the fallback.
func TestParentContextOfUsesTheRegistry(t *testing.T) {
	deps := Deps{ConversationID: "fallback"}
	pctx := parentContextOf(deps, Invocation{Prompt: "do the thing"})
	if pctx.ID != "fallback" {
		t.Fatalf("id = %q, want %q", pctx.ID, "fallback")
	}
	if pctx.Task != "do the thing" {
		t.Fatalf("task = %q, want %q", pctx.Task, "do the thing")
	}

	reg := NewRegistry("registry-parent")
	deps.Registry = reg
	pctx = parentContextOf(deps, Invocation{Prompt: "do the thing"})
	if pctx.ID != "registry-parent" {
		t.Fatalf("id = %q, want %q", pctx.ID, "registry-parent")
	}
}
