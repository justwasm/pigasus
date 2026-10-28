package vim

import "testing"

// 【一手】 agy 1.4.9, the vendor's own release notes: "a half-typed
// operator such as `d` carrying over after you submit a prompt".
//
// The engine keeps a half-typed operator across the text replacement a
// submit causes. A carried operator may only *complete*: when the next
// key does not complete it, the operator is dropped and the key is read
// on its own — the stale "d" must not swallow the "i" that starts the
// next prompt.

func TestSetTextDropsThePendingOperator(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "d")
	if s.Pending != "d" {
		t.Fatalf("pending = %q, want d", s.Pending)
	}
	s.SetText("")
	if s.Pending != "" {
		t.Fatalf("pending after SetText = %q, want empty", s.Pending)
	}
}

func TestSetTextKeepingPendingKeepsTheOperator(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "d")
	s.SetTextKeepingPending("")
	if s.Pending != "d" {
		t.Fatalf("pending after SetTextKeepingPending = %q, want d", s.Pending)
	}
}

func TestCarriedOperatorStillRunsItsMotion(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "d")
	s.SetTextKeepingPending("hello")
	feed(t, s, "w")
	// "dw" deleted the word: the carried operator still worked.
	checkText(t, s, "")
}

func TestCarriedOperatorDoesNotSwallowTheNextKey(t *testing.T) {
	s := newWith(t, "abc")
	feed(t, s, "d")
	s.SetTextKeepingPending("")
	feed(t, s, "i")
	checkMode(t, s, Insert)
	if s.Pending != "" {
		t.Fatalf("pending = %q, want empty", s.Pending)
	}
}
