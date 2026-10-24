package taskregistry

import (
	"fmt"
	"testing"
)

func TestPanelRows(t *testing.T) {
	rows := NewPanelRows()
	if snap := rows.Snapshot(); len(snap) != 0 {
		t.Fatalf("expected empty snapshot, got %d", len(snap))
	}
	if f := rows.Filtered(); f != 0 {
		t.Fatalf("expected 0 filtered, got %d", f)
	}

	reg := New()
	for i := 0; i < 25; i++ {
		reg.Register(&Task{ID: fmt.Sprintf("task-%d", i)})
	}

	rows.Rebuild(reg)
	snap := rows.Snapshot()
	if len(snap) != TasksPanelLimit {
		t.Errorf("len(snap) = %d, want %d", len(snap), TasksPanelLimit)
	}
	if rows.Filtered() != 5 {
		t.Errorf("filtered = %d, want 5", rows.Filtered())
	}

	rows.Rebuild(nil)
	if snap := rows.Snapshot(); len(snap) != 0 {
		t.Errorf("len(snap) after nil = %d, want 0", len(snap))
	}
}
