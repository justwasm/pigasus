package hooks

import "testing"

// TestEventsAreTheSixDocumented pins the event vocabulary.
//
// The documentation names six events: the two tool events, the three
// invocation events and the session event. The names are the keys of a
// hook definition, so they cannot drift.
func TestEventsAreTheSixDocumented(t *testing.T) {
	want := []Event{
		EventSessionStart,
		EventPreToolUse,
		EventPostToolUse,
		EventPreInvocation,
		EventPostInvocation,
		EventStop,
	}
	got := Events()
	if len(got) != len(want) {
		t.Fatalf("Events() = %d events, want %d", len(got), len(want))
	}
	for i, ev := range want {
		if got[i] != ev {
			t.Errorf("Events()[%d] = %q, want %q", i, got[i], ev)
		}
	}
	names := EventNames()
	if len(names) != 6 {
		t.Fatalf("EventNames() = %d names, want 6", len(names))
	}
	if names[0] != "SessionStart" || names[5] != "Stop" {
		t.Errorf("EventNames() = %v", names)
	}
}

// TestParseEvent pins the parser: the six names are read, anything else
// is not.
func TestParseEvent(t *testing.T) {
	for _, ev := range Events() {
		got, ok := ParseEvent(string(ev))
		if !ok || got != ev {
			t.Errorf("ParseEvent(%q) = %q, %v; want %q, true", ev, got, ok, ev)
		}
	}
	for _, name := range []string{"", "pretooluse", "PreTool", "Session Start"} {
		if _, ok := ParseEvent(name); ok {
			t.Errorf("ParseEvent(%q) accepted an event the documentation does not name", name)
		}
	}
}

// TestUsesMatcher pins which events match on a tool's name.
//
// The two tool events match; the four lifecycle events ignore their
// matcher.
func TestUsesMatcher(t *testing.T) {
	tool := map[Event]bool{
		EventPreToolUse:  true,
		EventPostToolUse: true,
	}
	for _, ev := range Events() {
		if got := ev.UsesMatcher(); got != tool[ev] {
			t.Errorf("%s.UsesMatcher() = %v, want %v", ev, got, tool[ev])
		}
	}
}
