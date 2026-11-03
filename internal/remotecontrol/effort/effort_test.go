// Package effort — effort_test.go: the closed set the dashboard
// may send.
//
// The vocabulary is the reference's own: low | medium | high |
// xhigh | max. Anything else is rejected by the surfaces that
// consume it (the cascade's per-turn effort and the project's
// default effort), and both answer -32602.
package effort

import (
	"testing"
)

// TestIsValid pins the effort vocabulary: every value in the
// closed set is valid, and the near-misses are not.
//
// The near-misses are the ones a dashboard actually sends by
// accident: the empty string, a wrong case, a dash instead of
// an underscore, a prefix of a value.
func TestIsValid(t *testing.T) {
	for _, want := range Values {
		if !IsValid(want) {
			t.Errorf("IsValid(%q) = false, want true", want)
		}
	}
	for _, bad := range []string{"", "bogus", "LOW", "x-high", "maxx", " ultra "} {
		if IsValid(bad) {
			t.Errorf("IsValid(%q) = true, want false", bad)
		}
	}
}

// TestValues pins the closed set itself, in order. The order is
// the dashboard's slider order, and a dashboard that renders the
// list verbatim depends on it.
func TestValues(t *testing.T) {
	want := []string{"low", "medium", "high", "xhigh", "max"}
	if len(Values) != len(want) {
		t.Fatalf("Values length = %d, want %d", len(Values), len(want))
	}
	for i, w := range want {
		if Values[i] != w {
			t.Errorf("Values[%d] = %q, want %q", i, Values[i], w)
		}
	}
}
