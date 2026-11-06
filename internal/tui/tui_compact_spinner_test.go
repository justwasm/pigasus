package tui

import (
	"testing"
	"time"
)

// TestCompactSpinnerTickAdvancesTheFrame pins the compaction spinner's
// tick end to end: the command's message has to be the message the
// host's update switch handles.
//
// The two were different types for two years. Commit 64e71d67 moved the
// tick commands into a shared viewer package and aliased the /usage
// panel's message type, but left the compaction spinner's message
// behind as a second, distinct type in the host — so the command
// produced a message the switch never matched. Nothing else in the
// tree noticed: the command returned, the host dropped the message, and
// the status row sat on its first Braille glyph for the whole
// compaction.
func TestCompactSpinnerTickAdvancesTheFrame(t *testing.T) {
	m := &runMode{}
	m.store.CompactStart = time.Now()
	m.store.CompactEnd = time.Time{}

	cmd := compactSpinnerTick()
	if cmd == nil {
		t.Fatal("compactSpinnerTick() = nil")
	}
	msg := cmd()
	if _, ok := msg.(compactSpinnerTickMsg); !ok {
		t.Fatalf("compactSpinnerTick() produced %T, want compactSpinnerTickMsg", msg)
	}

	before := m.store.CompactSpinnerFrame
	m.Update(msg)
	if m.store.CompactSpinnerFrame != before+1 {
		t.Errorf("compactSpinnerFrame = %d, want %d", m.store.CompactSpinnerFrame, before+1)
	}
}
