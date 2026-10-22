package tuilib

import (
	"testing"
)

func TestTextUtilities(t *testing.T) {
	if got := ViewRows("hello\nworld"); got != 2 {
		t.Errorf("ViewRows = %d, want 2", got)
	}
	if got := ViewRows("hello\nworld\n"); got != 2 {
		t.Errorf("ViewRows = %d, want 2", got)
	}
	if got := ClampInt(10, 0, 5); got != 5 {
		t.Errorf("ClampInt = %d, want 5", got)
	}
	if got := ClampInt(-1, 0, 5); got != 0 {
		t.Errorf("ClampInt = %d, want 0", got)
	}
	wrapped := WrapRows("abc def", 3)
	if len(wrapped) != 3 {
		t.Errorf("WrapRows length = %d, want 3", len(wrapped))
	}
}
