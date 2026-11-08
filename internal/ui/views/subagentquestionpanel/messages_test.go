package subagentquestionpanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
)

func Test_SubAgentQuestionPanelPureMessagePassing(t *testing.T) {
	mgr := subagentquestions.NewManager(false)
	q := mgr.Enqueue(subagentquestions.ParentKey, "What is the secret?", nil)

	p := New(Seams{
		Manager: mgr,
		Respond: func(line string) {},
	})
	p.SetScope(subagentquestions.ParentKey)
	p.Toggle() // opens the picker

	// 1. Enter stages question and emits MsgQuestionStaged
	claimed, cmd := p.HandleKeyMsg(tea.Key{Code: tea.KeyEnter})
	if !claimed || cmd == nil {
		t.Fatalf("expected Enter to be claimed with command")
	}
	msg := cmd()
	staged, ok := msg.(MsgQuestionStaged)
	if !ok || staged.QuestionID != q.ID {
		t.Fatalf("expected MsgQuestionStaged with %s, got %v", q.ID, msg)
	}

	// 2. Esc closes and emits MsgDismissed
	p.Toggle() // reopen
	claimed, cmd = p.HandleKeyMsg(tea.Key{Code: tea.KeyEsc})
	if !claimed || cmd == nil {
		t.Fatalf("expected Esc to be claimed with command")
	}
	msg = cmd()
	if _, ok := msg.(MsgDismissed); !ok {
		t.Fatalf("expected MsgDismissed, got %T", msg)
	}
}
