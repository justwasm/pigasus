package agentspanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
	"github.com/justwasm/pigasus/internal/features/agents"
)

func Test_AgentsPanelPureMessagePassing(t *testing.T) {
	sa := &subagent.Subagent{
		ID:    "sub-1",
		State: "running",
	}

	p := New(Seams{
		Subagents: func() []*subagent.Subagent { return []*subagent.Subagent{sa} },
		PendingAuthorization: func(id string) (lifecycle.PendingApproval, bool) {
			return lifecycle.PendingApproval{ID: "appr-1"}, true
		},
	}, []agents.Agent{{Name: "custom", Body: "prompt body"}})

	// 1. Esc on un-prepared emits MsgClose
	claimed, cmd := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !claimed || cmd == nil {
		t.Fatalf("expected Esc to be claimed with command")
	}
	msg := cmd()
	if _, ok := msg.(MsgClose); !ok {
		t.Fatalf("expected MsgClose, got %T", msg)
	}

	// 2. Cursor on agent, Enter prepares, Esc emits MsgSwitchAgent
	p.cursor = 1 // on "custom" agent
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	claimed, cmd = p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !claimed || cmd == nil {
		t.Fatalf("expected Esc to be claimed with command")
	}
	msg = cmd()
	sw, ok := msg.(MsgSwitchAgent)
	if !ok || sw.Name != "custom" {
		t.Fatalf("expected MsgSwitchAgent with custom, got %v", msg)
	}

	// 3. Cursor on subagent row, 'k' emits MsgKillSubagent
	p.cursor = p.agentRowCount() // on subagent
	claimed, cmd = p.HandleKey(tea.Key{Code: 'k', Text: "k"})
	if !claimed || cmd == nil {
		t.Fatalf("expected 'k' to be claimed with command")
	}
	msg = cmd()
	killMsg, ok := msg.(MsgKillSubagent)
	if !ok || killMsg.Subagent == nil || killMsg.Subagent.ID != "sub-1" {
		t.Fatalf("expected MsgKillSubagent for sub-1, got %v", msg)
	}

	// 4. Cursor on subagent row, 'a' emits MsgAuthorizeSubagent approved=true
	claimed, cmd = p.HandleKey(tea.Key{Code: 'a', Text: "a"})
	if !claimed || cmd == nil {
		t.Fatalf("expected 'a' to be claimed with command")
	}
	msg = cmd()
	authMsg, ok := msg.(MsgAuthorizeSubagent)
	if !ok || authMsg.ApprovalID != "appr-1" || !authMsg.Approved {
		t.Fatalf("expected MsgAuthorizeSubagent approved, got %v", msg)
	}
}
