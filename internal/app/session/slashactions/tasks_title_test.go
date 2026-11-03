package slashactions

import (
	"testing"
)

func TestTasksCountAndFeedback(t *testing.T) {
	states := []string{"running", "pending", "done", "failed", "killed", "unknown"}
	running, done := TasksCount(states)
	if running != 2 {
		t.Errorf("running = %d, want 2", running)
	}
	if done != 3 {
		t.Errorf("done = %d, want 3", done)
	}
	if msg := TasksFeedback(running, done); msg != "2 running, 3 done" {
		t.Errorf("TasksFeedback = %q", msg)
	}
	if msg := TasksFeedback(0, 0); msg != "No background tasks." {
		t.Errorf("TasksFeedback(0, 0) = %q", msg)
	}
}

func TestTitleVerbParsingAndResolve(t *testing.T) {
	cases := []struct {
		arg      string
		wantVerb TitleVerbKind
	}{
		{"", TitleVerbToggle},
		{"   ", TitleVerbToggle},
		{"on", TitleVerbOn},
		{"ON", TitleVerbOn},
		{"enable", TitleVerbOn},
		{"off", TitleVerbOff},
		{"OFF", TitleVerbOff},
		{"disable", TitleVerbOff},
		{"bogus", TitleVerbInvalid},
	}

	for _, c := range cases {
		got := ParseTitleVerb(c.arg)
		if got != c.wantVerb {
			t.Errorf("ParseTitleVerb(%q) = %v, want %v", c.arg, got, c.wantVerb)
		}
	}

	if !ResolveTitleToggle(TitleVerbOn, false) {
		t.Errorf("ResolveTitleToggle(On, false) want true")
	}
	if ResolveTitleToggle(TitleVerbOff, true) {
		t.Errorf("ResolveTitleToggle(Off, true) want false")
	}
	if !ResolveTitleToggle(TitleVerbToggle, false) {
		t.Errorf("ResolveTitleToggle(Toggle, false) want true")
	}
	if ResolveTitleToggle(TitleVerbToggle, true) {
		t.Errorf("ResolveTitleToggle(Toggle, true) want false")
	}
	if !ResolveTitleToggle(TitleVerbInvalid, true) {
		t.Errorf("ResolveTitleToggle(Invalid, true) want true (unchanged)")
	}
}
