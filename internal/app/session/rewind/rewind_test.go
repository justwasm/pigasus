// rewind_test.go — the /rewind policy's own tests.
//
// The policy moved out of the ui package (which truncated a copy of the
// session slice) and out of the host (which truncated it inline), so
// the tests that pinned it moved with it.

package rewind

import (
	"testing"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// TestSteps pins the turn split: one step per user message, newest
// first, each carrying the index AFTER the messages it owns.
func TestSteps(t *testing.T) {
	msgs := []backend.Message{
		{Role: "assistant", Content: "orphan"},
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "reply one"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "reply two"},
	}
	steps := Steps(msgs)
	if len(steps) != 2 {
		t.Fatalf("Steps = %d steps, want 2", len(steps))
	}
	// Newest first.
	if steps[0].UserIdx != 3 || steps[0].EndIdx != 5 {
		t.Errorf("steps[0] = %+v, want UserIdx 3 EndIdx 5", steps[0])
	}
	if steps[0].Content != "second" {
		t.Errorf("steps[0].Content = %q, want %q", steps[0].Content, "second")
	}
	if steps[1].UserIdx != 1 || steps[1].EndIdx != 3 {
		t.Errorf("steps[1] = %+v, want UserIdx 1 EndIdx 3", steps[1])
	}
	if steps[1].Content != "first" {
		t.Errorf("steps[1].Content = %q, want %q", steps[1].Content, "first")
	}
}

// TestStepsEmpty pins the empty-session result.
func TestStepsEmpty(t *testing.T) {
	if got := Steps(nil); got != nil {
		t.Errorf("Steps(nil) = %v, want nil", got)
	}
}

// TestStepsUserOnly pins the "no assistant has replied yet" case: the
// step's end index is the user message's own index.
func TestStepsUserOnly(t *testing.T) {
	steps := Steps([]backend.Message{{Role: "user", Content: "hello"}})
	if len(steps) != 1 {
		t.Fatalf("Steps = %d steps, want 1", len(steps))
	}
	if steps[0].UserIdx != 0 || steps[0].EndIdx != 1 {
		t.Errorf("steps[0] = %+v, want UserIdx 0 EndIdx 1", steps[0])
	}
}

// TestToStepTruncates pins the picker's truncation: a rewind to the
// oldest turn of a three-turn conversation keeps the first turn and
// drops the two that follow it.
func TestToStepTruncates(t *testing.T) {
	msgs := []backend.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u3"},
		{Role: "assistant", Content: "a3"},
	}
	r := ToStep(msgs, 2)
	if r.Removed != 4 {
		t.Errorf("Removed = %d, want 4", r.Removed)
	}
	if r.Step != 3 {
		t.Errorf("Step = %d, want 3", r.Step)
	}
	if len(r.Session) != 2 {
		t.Fatalf("session = %d messages, want 2", len(r.Session))
	}
	if r.Session[0].Content != "u1" || r.Session[1].Content != "a1" {
		t.Errorf("kept messages = %+v, want the first turn", r.Session)
	}
}

// TestToStepCurrentTurn pins the current turn's no-op: the newest step
// owns the whole tail, so a rewind to it drops nothing.
func TestToStepCurrentTurn(t *testing.T) {
	msgs := []backend.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
	}
	r := ToStep(msgs, 0)
	if r.Removed != 0 {
		t.Errorf("Removed = %d, want 0", r.Removed)
	}
	if len(r.Session) != len(msgs) {
		t.Errorf("session = %d messages, want %d", len(r.Session), len(msgs))
	}
}

// TestToStepClampsIndex pins the clamp: an index outside the step list
// is the current turn, and an empty conversation has no step at all.
func TestToStepClampsIndex(t *testing.T) {
	msgs := []backend.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	}
	for _, idx := range []int{-1, 7} {
		r := ToStep(msgs, idx)
		if r.Removed != 0 {
			t.Errorf("ToStep(index %d).Removed = %d, want 0", idx, r.Removed)
		}
	}
	empty := ToStep(nil, 0)
	if empty.Removed != 0 || len(empty.Session) != 0 {
		t.Errorf("ToStep(nil, 0) = %+v, want an empty result", empty)
	}
}

// TestPopTail pins the bare command's tail pop: the trailing
// assistant/user pair goes, and the result carries the truncated
// session — so the caller's session really shrinks.
func TestPopTail(t *testing.T) {
	if got := PopTail(nil); got.Removed != 0 {
		t.Errorf("PopTail(nil).Removed = %d, want 0", got.Removed)
	}
	msgs := []backend.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	}
	r := PopTail(msgs)
	if r.Removed != 2 {
		t.Errorf("PopTail.Removed = %d, want 2", r.Removed)
	}
	if len(r.Session) != 0 {
		t.Errorf("PopTail session = %d messages, want 0", len(r.Session))
	}
	userOnly := []backend.Message{
		{Role: "user", Content: "u1"},
	}
	r = PopTail(userOnly)
	if r.Removed != 1 {
		t.Errorf("PopTail(userOnly).Removed = %d, want 1", r.Removed)
	}
	if len(r.Session) != 0 {
		t.Errorf("PopTail(userOnly) session = %d messages, want 0", len(r.Session))
	}
}

// TestPopTailKeepsTheCaller — the policy hands the truncated session
// back, so the caller's own slice can be truncated by it. The copy the
// ui package carried could not do this: it popped a local slice and
// never wrote it back.
func TestPopTailKeepsTheCaller(t *testing.T) {
	msgs := []backend.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
	}
	r := PopTail(msgs)
	msgs = r.Session
	if len(msgs) != 2 {
		t.Errorf("session = %d messages, want 2", len(msgs))
	}
	if msgs[1].Content != "a1" {
		t.Errorf("kept tail = %+v, want the first turn", msgs)
	}
}

// TestStepEcho pins the picker's echo text, byte for byte.
func TestStepEcho(t *testing.T) {
	got := StepEcho(Result{Step: 3, Removed: 4})
	want := "Rewound to step 3 (4 message(s) removed)"
	if got != want {
		t.Errorf("StepEcho = %q, want %q", got, want)
	}
	current := StepEcho(Result{Step: 1})
	wantCurrent := "Rewound to step 1 (current, no messages removed)"
	if current != wantCurrent {
		t.Errorf("StepEcho(current) = %q, want %q", current, wantCurrent)
	}
}

// TestTailEcho pins the bare command's echo text, byte for byte.
func TestTailEcho(t *testing.T) {
	got := TailEcho(Result{Removed: 2})
	want := "Rewound 2 message(s) from the session tail"
	if got != want {
		t.Errorf("TailEcho = %q, want %q", got, want)
	}
	none := TailEcho(Result{})
	wantNone := "Nothing to rewind."
	if none != wantNone {
		t.Errorf("TailEcho(empty) = %q, want %q", none, wantNone)
	}
}
