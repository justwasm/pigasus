package mentionpicker

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func Test_MentionPickerPureMessagePassing(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "alpha.go"), []byte("package main"), 0o600)

	buf := "@al"
	p := New(Seams{
		Buffer:    func() string { return buf },
		SetBuffer: func(s string) { buf = s },
		Cwd:       func() string { return dir },
		Width:     func() int { return 80 },
	})
	p.Update() // activates candidates

	// 1. Tab completes and emits MsgCompleted
	claimed, cmd := p.HandleKeyMsg(tea.Key{Code: tea.KeyTab})
	if !claimed || cmd == nil {
		t.Fatalf("expected Tab to be claimed with command")
	}
	msg := cmd()
	comp, ok := msg.(MsgCompleted)
	if !ok || comp.RelPath != "alpha.go" {
		t.Fatalf("expected MsgCompleted with alpha.go, got %v", msg)
	}

	// 2. Esc resets and emits MsgDismissed
	p.Update()
	claimed, cmd = p.HandleKeyMsg(tea.Key{Code: tea.KeyEsc})
	if !claimed || cmd == nil {
		t.Fatalf("expected Esc to be claimed with command")
	}
	msg = cmd()
	if _, ok := msg.(MsgDismissed); !ok {
		t.Fatalf("expected MsgDismissed, got %T", msg)
	}
}
