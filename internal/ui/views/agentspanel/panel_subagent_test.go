// panel_subagent_test.go — the /agents panel's subagent keys.
//
// Cycle 383 (A-12, A-13): the reference's /agents panel advertises the
// kill ("k Kill Active Subagent" — the legend the live frame of cycle
// 205 captured) and answers a pending authorization inline ("press A to
// approve or D to deny", official-docs/cli/commands/agents.md:103).
// The clone advertised the kill and implemented neither.
//
// The tests build the panel directly — there is no runMode behind them
// — so a seam is a closure over a test value, and the manager behind it
// is the real one: the kill's policy belongs to the owner of the
// subagent, so the refusal for a finished subagent is the manager's
// answer, not the panel's.

package agentspanel

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
)

// ---------------------------------------------------------------- helpers

// startRunningSubagent registers a subagent that is running right now.
func startRunningSubagent(t *testing.T, mgr *subagent.Manager) *subagent.Subagent {
	t.Helper()
	sa := subagent.NewSubagent("sh", "-c", "sleep 30")
	if err := sa.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	mgr.Add(sa)
	return sa
}

// startDoneSubagent registers a subagent that has already finished.
func startDoneSubagent(t *testing.T, mgr *subagent.Manager) *subagent.Subagent {
	t.Helper()
	sa := subagent.NewSubagent("true")
	if err := sa.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := sa.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	mgr.Add(sa)
	return sa
}

// panelWithManager builds the panel over the manager's subagents, with
// the kill and the two answers wired to the manager itself.
func panelWithManager(mgr *subagent.Manager) (*Panel, *[]string) {
	var named []string
	p := New(Seams{
		Subagents: func() []*subagent.Subagent { return mgr.List() },
		Kill: func(sa *subagent.Subagent) bool {
			named = append(named, sa.ID)
			return mgr.KillRunning(sa.ID)
		},
		PendingAuthorization: func(id string) (lifecycle.PendingApproval, bool) {
			return mgr.PendingAuthorization(id)
		},
		Approve: func(id string) bool { return mgr.AnswerAuthorization(id, true) },
		Deny:    func(id string) bool { return mgr.AnswerAuthorization(id, false) },
	}, nil)
	return p, &named
}

// cursorOnSubagent puts the cursor on the row of the named subagent.
func cursorOnSubagent(t *testing.T, p *Panel, id string) {
	t.Helper()
	for i, sa := range p.subagents() {
		if sa != nil && sa.ID == id {
			p.SetCursor(p.agentRowCount() + i)
			return
		}
	}
	t.Fatalf("the subagent %q is not listed", id)
}

// ---------------------------------------------------------------- tests

// TestAgentsPanelKillKillsTheHighlightedSubagent — the kill names the
// subagent under the cursor, and the legend advertises the very key the
// handler acts on.
func TestAgentsPanelKillKillsTheHighlightedSubagent(t *testing.T) {
	mgr := subagent.NewManager()
	running := startRunningSubagent(t, mgr)
	p, named := panelWithManager(mgr)

	cursorOnSubagent(t, p, running.ID)
	if handled, _ := p.HandleKey(tea.Key{Code: 'k'}); !handled {
		t.Fatal("the 'k' key reported unhandled")
	}
	if len(*named) != 1 || (*named)[0] != running.ID {
		t.Fatalf("the kill named %v, want [%s]", *named, running.ID)
	}
	if got := running.StatusLine(); got != subagent.StatusKilled {
		t.Errorf("the status after the kill = %q, want %q", got, subagent.StatusKilled)
	}

	// The legend and the handler agree: the legend's key is the key
	// the panel acts on.
	legend := plainLines(p.Lines())
	last := legend[len(legend)-1]
	if !strings.Contains(last, "k Kill Active Subagent") {
		t.Errorf("the legend = %q, want the reference's kill hint", last)
	}
}

// TestAgentsPanelKillAcceptsBothCases — the reference's own legend
// writes the kill in lower case, its documentation in upper case, so
// both spellings are accepted.
func TestAgentsPanelKillAcceptsBothCases(t *testing.T) {
	cases := []struct {
		name string
		key  tea.Key
	}{
		{"lower case", tea.Key{Code: 'k', Text: "k"}},
		{"upper case (shift)", tea.Key{Code: 'k', Mod: tea.ModShift, Text: "K"}},
		{"upper case (code)", tea.Key{Code: 'K', Text: "K"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := subagent.NewManager()
			running := startRunningSubagent(t, mgr)
			p, _ := panelWithManager(mgr)
			cursorOnSubagent(t, p, running.ID)
			p.HandleKey(tc.key)
			if got := running.StatusLine(); got != subagent.StatusKilled {
				t.Errorf("the status after the kill = %q, want %q", got, subagent.StatusKilled)
			}
		})
	}
}

// TestAgentsPanelKillIgnoresCtrlK — a Ctrl+K is a different key.
func TestAgentsPanelKillIgnoresCtrlK(t *testing.T) {
	mgr := subagent.NewManager()
	running := startRunningSubagent(t, mgr)
	p, named := panelWithManager(mgr)
	cursorOnSubagent(t, p, running.ID)
	p.HandleKey(tea.Key{Code: 'k', Mod: tea.ModCtrl, Text: "k"})
	if len(*named) != 0 {
		t.Errorf("Ctrl+K named %v, want nothing", *named)
	}
	if got := running.StatusLine(); got != subagent.StatusRunning {
		t.Errorf("the status = %q, want %q", got, subagent.StatusRunning)
	}
}

// TestAgentsPanelKillLeavesAFinishedSubagentAlone — the reference's key
// targets the *active* subagent only, and the policy belongs to the
// owner of the subagent: the manager refuses a kill for a subagent that
// is not running.
func TestAgentsPanelKillLeavesAFinishedSubagentAlone(t *testing.T) {
	mgr := subagent.NewManager()
	done := startDoneSubagent(t, mgr)
	p, _ := panelWithManager(mgr)

	cursorOnSubagent(t, p, done.ID)
	p.HandleKey(tea.Key{Code: 'k'})
	if got := done.StatusLine(); got != subagent.StatusDone {
		t.Errorf("the status after the kill = %q, want %q", got, subagent.StatusDone)
	}
}

// TestAgentsPanelKillOnAnAgentRowDoesNothing — the cursor is on an
// agent row, so no subagent is named.
func TestAgentsPanelKillOnAnAgentRowDoesNothing(t *testing.T) {
	mgr := subagent.NewManager()
	running := startRunningSubagent(t, mgr)
	p, named := panelWithManager(mgr)

	p.HandleKey(tea.Key{Code: 'k'})
	if len(*named) != 0 {
		t.Errorf("the kill named %v, want nothing", *named)
	}
	if got := running.StatusLine(); got != subagent.StatusRunning {
		t.Errorf("the status = %q, want %q", got, subagent.StatusRunning)
	}
}

// TestAgentsPanelApproveAnswersThePendingAuthorization — the "A" key
// answers the authorization the highlighted subagent waits on.
func TestAgentsPanelApproveAnswersThePendingAuthorization(t *testing.T) {
	mgr := subagent.NewManager()
	sa := startRunningSubagent(t, mgr)
	appr := mgr.RequestAuthorization(sa.ID, lifecycle.ApprovalToolConfirm, "write_file", "")
	if appr.ID == "" {
		t.Fatal("the queued authorization has no id")
	}
	p, _ := panelWithManager(mgr)

	cursorOnSubagent(t, p, sa.ID)
	if handled, _ := p.HandleKey(tea.Key{Code: 'a'}); !handled {
		t.Fatal("the 'a' key reported unhandled")
	}
	if _, ok := mgr.PendingAuthorization(sa.ID); ok {
		t.Error("the authorization is still pending after 'a'")
	}
}

// TestAgentsPanelDenyAnswersThePendingAuthorization — the "D" key
// refuses it.
func TestAgentsPanelDenyAnswersThePendingAuthorization(t *testing.T) {
	mgr := subagent.NewManager()
	sa := startRunningSubagent(t, mgr)
	appr := mgr.RequestAuthorization(sa.ID, lifecycle.ApprovalToolConfirm, "write_file", "")
	p, _ := panelWithManager(mgr)

	cursorOnSubagent(t, p, sa.ID)
	if handled, _ := p.HandleKey(tea.Key{Code: 'd'}); !handled {
		t.Fatal("the 'd' key reported unhandled")
	}
	if _, ok := mgr.PendingAuthorization(sa.ID); ok {
		t.Error("the authorization is still pending after 'd'")
	}
	if appr.ID == "" {
		t.Error("the queued authorization has no id")
	}
}

// TestAgentsPanelAnswerWithoutAPendingAuthorizationDoesNothing — a
// subagent that waits for nothing has no answer to give.
func TestAgentsPanelAnswerWithoutAPendingAuthorizationDoesNothing(t *testing.T) {
	mgr := subagent.NewManager()
	sa := startRunningSubagent(t, mgr)
	p, _ := panelWithManager(mgr)

	cursorOnSubagent(t, p, sa.ID)
	p.HandleKey(tea.Key{Code: 'a'})
	p.HandleKey(tea.Key{Code: 'd'})
	if _, ok := mgr.PendingAuthorization(sa.ID); ok {
		t.Error("an authorization appeared out of nowhere")
	}
	if got := sa.StatusLine(); got != subagent.StatusRunning {
		t.Errorf("the status = %q, want %q", got, subagent.StatusRunning)
	}
}

// TestAgentsPanelRendersTheInlineApproval — the authorization shows up
// on the subagent's own row, with the reference's two option labels.
func TestAgentsPanelRendersTheInlineApproval(t *testing.T) {
	mgr := subagent.NewManager()
	sa := startRunningSubagent(t, mgr)
	mgr.RequestAuthorization(sa.ID, lifecycle.ApprovalToolConfirm, "write_file", "")
	p, _ := panelWithManager(mgr)

	rows := plainLines(p.Lines())
	want := "awaiting approval: write_file  [A] Yes, allow tool call  [D] No, deny tool call"
	if !containsRow(rows, want) {
		t.Fatalf("the inline approval is missing:\n got: %v\nwant a row with %q", rows, want)
	}
}

// TestAgentsPanelCursorReachesTheSubagentRows — the reference's cursor
// moves between the headers, the subagents and the available agents, so
// the subagent row under the cursor is marked.
func TestAgentsPanelCursorReachesTheSubagentRows(t *testing.T) {
	mgr := subagent.NewManager()
	sa := startRunningSubagent(t, mgr)
	p, _ := panelWithManager(mgr)

	if got := p.RowCount(); got != 2 {
		t.Fatalf("the row count = %d, want 2 (the default agent and the subagent)", got)
	}
	cursorOnSubagent(t, p, sa.ID)
	rows := plainLines(p.Lines())
	if !containsRow(rows, "> "+sa.Name+"  "+sa.StatusLine()) {
		t.Fatalf("the cursor is not on the subagent's row:\n got: %v", rows)
	}
}

// TestAgentsPanelEnterOnASubagentRowPreparesNothing — the reference's
// Enter on a subagent row opens the Subagent Detail View, which the
// clone does not have, so it prepares no switch.
func TestAgentsPanelEnterOnASubagentRowPreparesNothing(t *testing.T) {
	mgr := subagent.NewManager()
	sa := startRunningSubagent(t, mgr)
	var echoed []string
	p := New(Seams{
		Subagents: func() []*subagent.Subagent { return mgr.List() },
		Respond:   func(msg string) { echoed = append(echoed, msg) },
	}, nil)

	cursorOnSubagent(t, p, sa.ID)
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Prepared() {
		t.Error("the Enter on a subagent row prepared a switch")
	}
	if len(echoed) != 0 {
		t.Errorf("the Enter on a subagent row wrote %q", echoed)
	}
}
