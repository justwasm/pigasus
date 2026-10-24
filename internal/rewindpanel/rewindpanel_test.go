package rewindpanel

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
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
	if steps[0].Snippet != "second" {
		t.Errorf("steps[0].Snippet = %q, want %q", steps[0].Snippet, "second")
	}
	if steps[1].UserIdx != 1 || steps[1].EndIdx != 3 {
		t.Errorf("steps[1] = %+v, want UserIdx 1 EndIdx 3", steps[1])
	}
	if steps[1].Snippet != "first" {
		t.Errorf("steps[1].Snippet = %q, want %q", steps[1].Snippet, "first")
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

// TestFirstRow pins the snippet rule: first non-empty line, trimmed,
// capped at max runes with an ellipsis.
func TestFirstRow(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"empty", "", ""},
		{"blank-first-line", "\n\nreal", "real"},
		{"trims", "   padded   ", "padd…"},
		{"first-line-only", "one\ntwo", "one"},
		{"under-cap", "short", "short"},
		{"at-cap", "12345", "12345"},
		{"over-cap", "123456", "1234…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FirstRow(tc.in, 5); got != tc.want {
				t.Errorf("FirstRow(%q, 5) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestStepHeight pins the visible-row budget: the cycle-189 default of 17
// when the height is unknown, and height-6 once the terminal reports one.
func TestStepHeight(t *testing.T) {
	for _, tc := range []struct {
		h    int
		want int
	}{
		{0, 17},
		{-5, 17},
		{23, 17},
		{24, 18},
		{6, 1},
		{3, 1},
	} {
		if got := StepHeight(tc.h); got != tc.want {
			t.Errorf("StepHeight(%d) = %d, want %d", tc.h, got, tc.want)
		}
	}
}

// TestRenderStepRows pins the row shape: the cursor row is blue and
// prefixed with "> ", the newest turn carries a "(current)" annotation.
func TestRenderStepRows(t *testing.T) {
	steps := []Step{{Snippet: "newest"}, {Snippet: "older"}}
	rows := RenderStepRows(steps, 1, "<B>", "</B>", "<G>", "</G>")
	if len(rows) != 2 {
		t.Fatalf("RenderStepRows = %d rows, want 2", len(rows))
	}
	if !strings.Contains(rows[0], "<G>  (current)</G>") {
		t.Errorf("row 0 = %q, want the (current) annotation", rows[0])
	}
	if !strings.HasPrefix(rows[1], "<B>> ") {
		t.Errorf("row 1 = %q, want the blue cursor prefix", rows[1])
	}
	if !strings.Contains(rows[1], "Turn 2") {
		t.Errorf("row 1 = %q, want the Turn 2 label", rows[1])
	}
}

// TestStepCounter pins the footer counter's shape.
func TestStepCounter(t *testing.T) {
	got := StepCounter(1, 5, 20, "<D>", "</D>")
	want := "<D>  (1–5 of 20 turns)</D>"
	if got != want {
		t.Errorf("StepCounter = %q, want %q", got, want)
	}
}

func TestRewindSession(t *testing.T) {
	if got := RewindSession(nil); got != 0 {
		t.Errorf("RewindSession(nil) = %d, want 0", got)
	}
	msgs := []backend.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	}
	if got := RewindSession(msgs); got != 2 {
		t.Errorf("RewindSession = %d, want 2", got)
	}
	userOnly := []backend.Message{
		{Role: "user", Content: "u1"},
	}
	if got := RewindSession(userOnly); got != 1 {
		t.Errorf("RewindSession(userOnly) = %d, want 1", got)
	}
}

