// autocont_test.go — the recovery vocabulary.
//
// Cycle 346: the iteration-count cases came from the two engines that
// used to carry their own copy of the parser — internal/agent
// (turnsFromErr) and internal/pigrt (iterationCountFromErr) — and the
// byte-exactness assertions on the two recovery copies came with them.
package autocont

import (
	"errors"
	"fmt"
	"testing"
)

// TestIterationCount covers the message shapes the two turn engines see.
func TestIterationCount(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"empty", errors.New(""), 0},
		{"agent message", fmt.Errorf("agent: exceeded tool-iteration budget (%d)", 12), 12},
		{"wrapped turns suffix", fmt.Errorf("%w (%d turns)", errors.New("max turns"), 8), 8},
		{"PiG detailed", errors.New("agent: exceeded turn limit: 21 turns completed"), 21},
		{"pigrt zero", fmt.Errorf("%w (%d turns)", errors.New("max turns"), 0), 0},
		// The count is the largest digit run, not every digit
		// concatenated: this message used to read as 83.
		{"two numbers", errors.New("budget (8) after 3 tries"), 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IterationCount(tc.err); got != tc.want {
				t.Errorf("IterationCount(%q) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestRecoveryCopiesAreByteExact pins the two strings against the
// reference's own bytes — they are the reference's prose, not the
// clone's, so a typo here is a parity break.
func TestRecoveryCopiesAreByteExact(t *testing.T) {
	if got, want := StreamInterrupted, "The stream was interrupted. Please continue the task you were working on."; got != want {
		t.Errorf("StreamInterrupted = %q, want %q", got, want)
	}
	if got, want := IterationCapFormat, "**Status**: Cancelled after %d iterations"; got != want {
		t.Errorf("IterationCapFormat = %q, want %q", got, want)
	}
	if MaxDepth != 3 {
		t.Errorf("MaxDepth = %d, want 3", MaxDepth)
	}
	if MaxNetworkDepth != 8 {
		t.Errorf("MaxNetworkDepth = %d, want 8", MaxNetworkDepth)
	}
}
