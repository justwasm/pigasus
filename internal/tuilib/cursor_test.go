package tuilib_test

import (
	"testing"

	tas "charm.land/bubbles/v2/textarea"
	"github.com/justwasm/pigasus/internal/tuilib"
)

func TestCursorLine(t *testing.T) {
	val := "hello\nworld\nfoo"
	if got := tuilib.CursorLine(val, 0); got != 0 {
		t.Errorf("CursorLine(val, 0) = %d, want 0", got)
	}
	if got := tuilib.CursorLine(val, 6); got != 1 {
		t.Errorf("CursorLine(val, 6) = %d, want 1", got)
	}
	if got := tuilib.CursorLine(val, 12); got != 2 {
		t.Errorf("CursorLine(val, 12) = %d, want 2", got)
	}
}

func TestIsOnFirstAndLastLine(t *testing.T) {
	val := "line1\nline2\nline3"
	if !tuilib.IsOnFirstLine(val, 2) {
		t.Error("expected offset 2 to be on first line")
	}
	if tuilib.IsOnFirstLine(val, 8) {
		t.Error("expected offset 8 not to be on first line")
	}
	if !tuilib.IsOnLastLine(val, 14) {
		t.Error("expected offset 14 to be on last line")
	}
	if tuilib.IsOnLastLine(val, 2) {
		t.Error("expected offset 2 not to be on last line")
	}
}

func TestInputCursorByteOffset(t *testing.T) {
	m := tas.New()
	m.SetWidth(100)
	m.SetHeight(10)
	m.SetValue("hello\nworld")
	m.MoveToBegin()
	if off := tuilib.InputCursorByteOffset(m); off != 0 {
		t.Errorf("InputCursorByteOffset at start = %d, want 0", off)
	}
}
