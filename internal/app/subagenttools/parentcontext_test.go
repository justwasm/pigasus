// parentcontext_test.go — what a subagent must not lose when its own
// context is compacted (agy 1.4.10).
package subagenttools

import (
	"strings"
	"testing"
	"time"

	agentmessage "github.com/justwasm/pigasus/internal/core/msg"
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

	lines, _ := pctx.Lines()
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"73ab3617-5a99-4e26-b1f1-c88e712efe39",
		"Write the release notes",
		"the first draft is in the file",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("parent block is missing %q:\n%s", want, joined)
		}
	}
	// Every line is in the reference's own one-line form, which is what
	// the "# Parent Agent Messages" section carries.
	for _, line := range lines {
		if !strings.HasPrefix(line, agentmessage.LineFormat[:9]) {
			t.Fatalf("line is not in the reference's format: %q", line)
		}
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
	lines, _ := pctx.Lines()
	if got := strings.Count(strings.Join(lines, "\n"), "content=message"); got != MaxParentMessages {
		t.Fatalf("parent block carries %d messages, want %d", got, MaxParentMessages)
	}
}

// TestParentContextTaskSurvivesTheCap pins that the delegation itself is
// never one of the messages the cap drops.
//
// The task is the *oldest* thing the parent said, so a cap applied to the
// combined list would drop it first — which is precisely the defect agy
// 1.4.10 fixed ("subagents losing the task their parent agent gave them").
// The cap must therefore bound the messages only, and Record must be its
// only enforcer.
func TestParentContextTaskSurvivesTheCap(t *testing.T) {
	pctx := &ParentContext{ID: "parent", Task: "the task the parent gave", TaskAt: time.Unix(0, 0)}
	for i := 0; i < MaxParentMessages+2; i++ {
		pctx.Record("later message", time.Unix(int64(i+1), 0))
	}
	lines, mostRecent := pctx.Lines()
	if !mostRecent {
		t.Error("mostRecent = false, want true: the cap hid older messages")
	}
	if len(lines) != MaxParentMessages+1 {
		t.Fatalf("got %d lines, want the task plus %d messages", len(lines), MaxParentMessages)
	}
	if !strings.Contains(lines[0], "content=the task the parent gave") {
		t.Fatalf("the task is not the first line: %q", lines[0])
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

// TestSystemWakeupDoesNotCrowdOutParentContext verifies that system wakeup
// notifications are attributed to "system" and do not consume ParentContext
// slots that preserve coordinator instructions across compaction.
func TestSystemWakeupDoesNotCrowdOutParentContext(t *testing.T) {
	at := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	pctx := &ParentContext{
		ID:     "coordinator-1",
		Task:   "Coordinate the swarm",
		TaskAt: at,
	}
	pctx.Record("coordinator instruction 1", at.Add(time.Second))

	for i := 0; i < 15; i++ {
		env := renderDeliveredMessage(pctx, "coordinator-1", Message{
			From:    "system",
			Content: "board wakeup",
			At:      at.Add(time.Duration(i+2) * time.Second),
		})
		if !strings.Contains(env, "sender=system ") {
			t.Fatalf("expected sender=system in envelope, got:\n%s", env)
		}
	}

	if len(pctx.Messages) != 1 || pctx.Messages[0].Content != "coordinator instruction 1" {
		t.Fatalf("system wakeups crowded out ParentContext: %+v", pctx.Messages)
	}
}
