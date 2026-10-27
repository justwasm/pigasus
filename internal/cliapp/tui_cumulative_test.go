// tui_cumulative_test.go — cycle 312: the implicit trajectory reaches the
// model.
//
// The reference records a `!` command in its own per-workspace store and
// never in the conversation; the cumulative-prompt handler is what turns that
// trajectory into chat messages for the next prompt. These tests pin the
// clone's side of that: the command reaches the model, in the reference's
// own shape, and it stays out of the conversation.
package cliapp

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/implicit"
)

// recordImplicitCommand writes one `!` command into the reference's
// implicit trajectory store.
func recordImplicitCommand(t *testing.T, cwd, cmd, output string) {
	t.Helper()
	step := implicit.Step{
		Status: implicit.StepStatusDone,
		RunCommand: implicit.RunCommand{
			CommandLine:         cmd,
			ProposedCommandLine: cmd,
			CWD:                 cwd,
			CombinedOutput:      output,
			ExitCode:            0,
			HasExitCode:         true,
			Blocking:            true,
			UsedIDETerminal:     true,
		},
		CreatedAt:   time.Unix(1792644998, 0).UTC(),
		CompletedAt: time.Unix(1792644998, 196151000).UTC(),
	}
	store := implicit.Open(convstore.StoreDir())
	if err := store.Record(implicit.WorkspaceURI(cwd), step); err != nil {
		t.Fatalf("record: %v", err)
	}
}

// TestTheImplicitCommandReachesTheModel pins the delivery: a `!` command
// the user ran outside the conversation reaches the model with the next
// turn, in the reference's own shape.
func TestTheImplicitCommandReachesTheModel(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	m := &runMode{cwd: "/private/var/folders/x/T/opencode"}
	recordImplicitCommand(t, m.cwd, "ls -la", "total 12\n")

	msgs := m.prepareStreamTurnMessages()
	if len(msgs) != 2 {
		t.Fatalf("the turn carries %d messages, want 2", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("the first message's role is %q, want \"user\"", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "The USER performed the following action:\nCommand: ls -la") {
		t.Errorf("the first message does not name the action:\n%s", msgs[0].Content)
	}
	if !strings.Contains(msgs[1].Content, "The command exited with code 0.\nOutput:\ntotal 12") {
		t.Errorf("the second message does not carry the output:\n%s", msgs[1].Content)
	}
	for i, msg := range msgs {
		if !strings.HasPrefix(msg.Content, "Step Id: 0\n") {
			t.Errorf("message %d does not carry the step id:\n%s", i, msg.Content)
		}
		if strings.Contains(msg.Content, "<USER_REQUEST>") {
			t.Errorf("message %d carries the user envelope:\n%s", i, msg.Content)
		}
	}
}

// TestTheImplicitCommandStaysOutOfTheConversation pins the store: the
// reference records a `!` command in the implicit trajectory only, and the
// conversation store holds no record of it.
func TestTheImplicitCommandStaysOutOfTheConversation(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	m := &runMode{cwd: "/private/var/folders/x/T/opencode"}
	recordImplicitCommand(t, m.cwd, "ls -la", "total 12\n")

	m.prepareStreamTurnMessages()
	if len(m.session) != 0 {
		t.Errorf("the session holds %d messages, want 0", len(m.session))
	}
	if steps := m.trajectorySteps(); len(steps) != 0 {
		t.Errorf("the trajectory holds %d steps, want 0", len(steps))
	}
}

// TestTheImplicitCommandIsCarriedOnce: the reference converts only the steps
// no prompt has carried yet — its handler caches what it already converted.
func TestTheImplicitCommandIsCarriedOnce(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())

	m := &runMode{cwd: "/private/var/folders/x/T/opencode"}
	recordImplicitCommand(t, m.cwd, "ls -la", "total 12\n")

	if msgs := m.prepareStreamTurnMessages(); len(msgs) != 2 {
		t.Fatalf("the first turn carries %d messages, want 2", len(msgs))
	}
	if msgs := m.prepareStreamTurnMessages(); len(msgs) != 2 {
		t.Fatalf("the second turn carries %d messages, want 2", len(msgs))
	}

	// A second command reaches the model with the next turn, and the first
	// one is still carried — the reference's cumulative prompt keeps the
	// persistent context.
	recordImplicitCommand(t, m.cwd, "pwd", "/tmp\n")
	msgs := m.prepareStreamTurnMessages()
	if len(msgs) != 4 {
		t.Fatalf("the third turn carries %d messages, want 4", len(msgs))
	}
	if !strings.Contains(msgs[2].Content, "The USER performed the following action:\nCommand: pwd") {
		t.Errorf("the third message does not name the new action:\n%s", msgs[2].Content)
	}
	if !strings.HasPrefix(msgs[2].Content, "Step Id: 1\n") {
		t.Errorf("the third message does not carry the second step's id:\n%s", msgs[2].Content)
	}
}
