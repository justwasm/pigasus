package activeitemspanel

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

func Test_ActiveItemsPanelPureMessagePassing(t *testing.T) {
	reg := taskregistry.New()
	reg.Register(&taskregistry.Task{
		ID:        "task-123",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now(),
	})

	p := New(Seams{
		Registry: func() *taskregistry.Registry { return reg },
	})
	p.SetFocus(true)

	// 1. Enter emits MsgOpenDetail
	claimed, cmd := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !claimed || cmd == nil {
		t.Fatalf("expected Enter to be claimed with command")
	}
	msg := cmd()
	detail, ok := msg.(MsgOpenDetail)
	if !ok || detail.ID != "task-123" {
		t.Fatalf("expected MsgOpenDetail with ID task-123, got %v", msg)
	}

	// 2. Esc emits MsgDefocused
	p.SetFocus(true)
	claimed, cmd = p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !claimed || cmd == nil {
		t.Fatalf("expected Esc to be claimed with command")
	}
	msg = cmd()
	if _, ok := msg.(MsgDefocused); !ok {
		t.Fatalf("expected MsgDefocused, got %T", msg)
	}
}
