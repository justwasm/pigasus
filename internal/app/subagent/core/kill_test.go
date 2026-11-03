// kill_test.go — the manager's kill.
//
// The reference's /agents panel kills the *active* subagent: its
// keybinding table says "Instantly cancel (`CancelSubagent`) the
// highlighted running subagent" (official-docs/cli/commands/agents.md:115)
// and its "Common mistakes" table says the key "Only targets active
// (`running`) subagent processes" (the same file).
//
// The policy lives with the state it mutates — the manager owns the
// tracked subagents and their states — so these tests pin it here.

package core

import (
	"context"
	"testing"

	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
)

// TestManagerKillRunning — only a running subagent may be killed.
func TestManagerKillRunning(t *testing.T) {
	mgr := NewManager()

	running := NewSubagent("sh", "-c", "sleep 30")
	if err := running.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mgr.Add(running)

	done := NewSubagent("true")
	if err := done.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := done.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	mgr.Add(done)

	if !mgr.KillRunning(running.ID) {
		t.Fatal("the running subagent was not killed")
	}
	if got := running.StatusLine(); got != StatusKilled {
		t.Errorf("the status after the kill = %q, want %q", got, StatusKilled)
	}
	// The killed subagent keeps its row: the panel shows its final
	// status.
	if _, ok := mgr.Get(running.ID); !ok {
		t.Error("the killed subagent left the manager's list")
	}

	// A finished subagent is left alone.
	if mgr.KillRunning(done.ID) {
		t.Error("a finished subagent was killed")
	}
	if got := done.StatusLine(); got != StatusDone {
		t.Errorf("the status = %q, want %q", got, StatusDone)
	}

	// A killed subagent is not killed twice.
	if mgr.KillRunning(running.ID) {
		t.Error("the killed subagent was killed again")
	}

	// An unknown id and an empty id name no subagent.
	if mgr.KillRunning("no-such-id") {
		t.Error("an unknown id was killed")
	}
	if mgr.KillRunning("") {
		t.Error("an empty id was killed")
	}
}

// TestManagerKillRunningDropsThePendingApprovals — a killed subagent
// will never answer its authorizations, so they go with it (the same
// rule the AGY 1.3.3 changelog states for a stopped subagent).
func TestManagerKillRunningDropsThePendingApprovals(t *testing.T) {
	mgr := NewManager()
	sa := NewSubagent("sh", "-c", "sleep 30")
	if err := sa.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mgr.Add(sa)

	mgr.RequestAuthorization(sa.ID, lifecycle.ApprovalToolConfirm, "write_file", "")
	if _, ok := mgr.PendingAuthorization(sa.ID); !ok {
		t.Fatal("the authorization was not queued")
	}

	if !mgr.KillRunning(sa.ID) {
		t.Fatal("the running subagent was not killed")
	}
	if _, ok := mgr.PendingAuthorization(sa.ID); ok {
		t.Error("the killed subagent kept its pending authorization")
	}
}
