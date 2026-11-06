// agents_host_test.go — the host's side of the /agents panel.
//
// The panel itself is tested in internal/ui/views/agentspanel: this file
// covers what only the host owns — the session's subagent manager, the
// seams that carry the kill and the two answers to it, and the fact
// that the panel's keys reach the panel at all.

package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
)

// seedAgentsRunMode returns a runMode with a fresh subagent manager.
func seedAgentsRunMode() *runMode {
	m := &runMode{}
	m.engine.taskSubagentManager = subagent.NewManager()
	return m
}

// TestAgentsPanelKillReachesTheSessionManager — the "k" key the panel
// owns reaches the manager that owns the subagent, and the subagent
// dies.
func TestAgentsPanelKillReachesTheSessionManager(t *testing.T) {
	m := seedAgentsRunMode()
	sa := subagent.NewSubagent("sh", "-c", "sleep 30")
	if err := sa.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.engine.taskSubagentManager.Add(sa)

	m.openSlashPanel("agents")
	p := m.agentsPanel()
	// The subagent row is the last row: the default agent is the first.
	p.SetCursor(p.RowCount() - 1)

	handled, _ := m.handlePanelKey(tea.Key{Code: 'k'})
	if !handled {
		t.Fatal("the 'k' key reported unhandled")
	}
	if got := sa.StatusLine(); got != subagent.StatusKilled {
		t.Errorf("the status after the kill = %q, want %q", got, subagent.StatusKilled)
	}
}

// TestAgentsPanelApprovalReachesTheSessionManager — the "a" key the
// panel owns answers the authorization the session's manager holds.
func TestAgentsPanelApprovalReachesTheSessionManager(t *testing.T) {
	m := seedAgentsRunMode()
	sa := subagent.NewSubagent("sh", "-c", "sleep 30")
	if err := sa.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.engine.taskSubagentManager.Add(sa)
	appr := m.engine.taskSubagentManager.RequestAuthorization(sa.ID, lifecycle.ApprovalToolConfirm, "write_file", "")
	if appr.ID == "" {
		t.Fatal("the queued authorization has no id")
	}

	m.openSlashPanel("agents")
	p := m.agentsPanel()
	p.SetCursor(p.RowCount() - 1)

	handled, _ := m.handlePanelKey(tea.Key{Code: 'a'})
	if !handled {
		t.Fatal("the 'a' key reported unhandled")
	}
	if _, ok := m.engine.taskSubagentManager.PendingAuthorization(sa.ID); ok {
		t.Error("the authorization is still pending after 'a'")
	}
}

// TestAgentsPanelDenialReachesTheSessionManager — the "d" key refuses.
func TestAgentsPanelDenialReachesTheSessionManager(t *testing.T) {
	m := seedAgentsRunMode()
	sa := subagent.NewSubagent("sh", "-c", "sleep 30")
	if err := sa.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	m.engine.taskSubagentManager.Add(sa)
	m.engine.taskSubagentManager.RequestAuthorization(sa.ID, lifecycle.ApprovalToolConfirm, "write_file", "")

	m.openSlashPanel("agents")
	p := m.agentsPanel()
	p.SetCursor(p.RowCount() - 1)

	handled, _ := m.handlePanelKey(tea.Key{Code: 'd'})
	if !handled {
		t.Fatal("the 'd' key reported unhandled")
	}
	if _, ok := m.engine.taskSubagentManager.PendingAuthorization(sa.ID); ok {
		t.Error("the authorization is still pending after 'd'")
	}
}
