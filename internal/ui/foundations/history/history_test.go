package history

import (
	"fmt"
	"testing"
)

func TestIsBlank(t *testing.T) {
	blanks := []string{"", " ", "\n", "\t", "  \t \n ", " ", "　", " ", "                    "}
	for _, b := range blanks {
		if !IsBlank(b) {
			t.Errorf("IsBlank(%q) = false, want true", b)
		}
	}
	for _, s := range []string{"a", " a ", "0", "a b"} {
		if IsBlank(s) {
			t.Errorf("IsBlank(%q) = true, want false", s)
		}
	}
}

// A blank prompt is not stored; a trimmed one is; a repeat of the
// previous entry is not stored twice.
func TestPush(t *testing.T) {
	var l List
	if _, ok := l.Push("   "); ok {
		t.Errorf("Push(blank) stored an entry")
	}
	if l.Len() != 0 {
		t.Fatalf("Len() = %d after a blank push, want 0", l.Len())
	}

	got, ok := l.Push("  hello  ")
	if !ok || got != "hello" {
		t.Fatalf("Push(%q) = (%q, %v), want (%q, true)", "  hello  ", got, ok, "hello")
	}
	if _, ok := l.Push("hello"); ok {
		t.Errorf("Push of the previous entry stored a second copy")
	}
	if l.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", l.Len())
	}

	if _, ok := l.Push("  multi word prompt "); !ok {
		t.Errorf("Push of a new entry was refused")
	}
	if l.Entries()[1] != "multi word prompt" {
		t.Errorf("Entries()[1] = %q, want %q", l.Entries()[1], "multi word prompt")
	}
}

// The oldest entries fall off the top once the list grows past the cap.
func TestPushCap(t *testing.T) {
	var l List
	for i := 0; i < Cap+5; i++ {
		l.Push(fmt.Sprintf("prompt-%d", i))
	}
	if l.Len() != Cap {
		t.Fatalf("Len() = %d, want %d", l.Len(), Cap)
	}
	if l.Entries()[0] != "prompt-5" {
		t.Errorf("Entries()[0] = %q, want %q", l.Entries()[0], "prompt-5")
	}
	if l.Entries()[Cap-1] != fmt.Sprintf("prompt-%d", Cap+4) {
		t.Errorf("Entries()[%d] = %q, want %q", Cap-1, l.Entries()[Cap-1], fmt.Sprintf("prompt-%d", Cap+4))
	}
}

// The store seeds an empty list; a populated list keeps its entries.
func TestLoad(t *testing.T) {
	var l List
	l.Load([]string{"first", "second"})
	if l.Len() != 2 || l.Entries()[0] != "first" {
		t.Fatalf("Load did not seed the list: %q", l.Entries())
	}
	l.Load([]string{"third"})
	if l.Len() != 2 || l.Entries()[1] != "second" {
		t.Fatalf("Load overwrote a populated list: %q", l.Entries())
	}
}

// The navigation walks back over the entries and hands the draft back
// when it leaves the history.
func TestNavigation(t *testing.T) {
	var l List
	l.Push("alpha")
	l.Push("beta")
	l.Push("gamma")

	if got, ok := l.Up("DRAFT"); !ok || got != "gamma" {
		t.Fatalf("Up = (%q, %v), want (%q, true)", got, ok, "gamma")
	}
	if l.Index() != 1 {
		t.Fatalf("Index() = %d, want 1", l.Index())
	}
	if got, ok := l.Up(""); !ok || got != "beta" {
		t.Fatalf("Up = (%q, %v), want (%q, true)", got, ok, "beta")
	}
	if got, ok := l.Up(""); !ok || got != "alpha" {
		t.Fatalf("Up = (%q, %v), want (%q, true)", got, ok, "alpha")
	}
	// One more Up is a no-op — there is no fourth entry.
	if got, ok := l.Up(""); ok || got != "" {
		t.Fatalf("Up past the end = (%q, %v), want (\"\", false)", got, ok)
	}
	if l.Index() != 3 {
		t.Fatalf("Index() = %d after an Up past the end, want 3", l.Index())
	}

	if got, ok := l.Down(); !ok || got != "beta" {
		t.Fatalf("Down = (%q, %v), want (%q, true)", got, ok, "beta")
	}
	if got, ok := l.Down(); !ok || got != "gamma" {
		t.Fatalf("Down = (%q, %v), want (%q, true)", got, ok, "gamma")
	}
	if got, ok := l.Down(); !ok || got != "DRAFT" {
		t.Fatalf("Down = (%q, %v), want (%q, true)", got, ok, "DRAFT")
	}
	if l.Index() != 0 {
		t.Fatalf("Index() = %d after leaving the history, want 0", l.Index())
	}
	// Down past the start is a no-op.
	if got, ok := l.Down(); ok || got != "" {
		t.Fatalf("Down past the start = (%q, %v), want (\"\", false)", got, ok)
	}
}

// An empty list has nothing to recall.
func TestNavigationOnAnEmptyList(t *testing.T) {
	var l List
	if got, ok := l.Up("DRAFT"); ok || got != "" {
		t.Fatalf("Up on an empty list = (%q, %v), want (\"\", false)", got, ok)
	}
	if got, ok := l.Down(); ok || got != "" {
		t.Fatalf("Down on an empty list = (%q, %v), want (\"\", false)", got, ok)
	}
}

// The reset leaves the navigation and drops the draft.
func TestReset(t *testing.T) {
	var l List
	l.Push("alpha")
	l.Up("DRAFT")
	l.Reset()
	if l.Index() != 0 {
		t.Fatalf("Index() = %d after a reset, want 0", l.Index())
	}
	if got, ok := l.Down(); ok || got != "" {
		t.Fatalf("Down after a reset = (%q, %v), want (\"\", false)", got, ok)
	}
	if got, ok := l.Up(""); !ok || got != "alpha" {
		t.Fatalf("Up after a reset = (%q, %v), want (%q, true)", got, ok, "alpha")
	}
}
