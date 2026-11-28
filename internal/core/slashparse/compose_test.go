package slashparse

// Guard for the composed-command rule.
//
// Every case below is a live agy 1.5.18 observation: the input is what was
// handed to `agy -p`, the text is what the capture proxy saw inside
// <USER_REQUEST>, and names are the [Slash Command] expansions the wire
// carried. The table is the evidence, so a future change to the scanner that
// "cleans up" the trailing-append quirk fails here with the capture quoted
// next to it.

import (
	"reflect"
	"testing"
)

// composedExpandableForTest is the set the captures exercised. It is the four
// commands the reference expands; /plan, /boost and /teamwork never expand.
func composedExpandableForTest(name string) bool {
	switch name {
	case "learn", "goal", "grill-me", "schedule":
		return true
	}
	return false
}

func TestExpandComposed_CapturedBehaviour(t *testing.T) {
	cases := []struct {
		in    string
		text  string
		names []string
	}{
		// Leading run: every expandable command in the leading run expands,
		// in order, and the text is left as the run plus the remaining tail.
		{"/goal /learn do the thing", "/goal /learn do the thing", []string{"goal", "learn"}},
		{"/goal /learn x", "/goal /learn x", []string{"goal", "learn"}},
		{"/goal /learn", "/goal /learn", []string{"goal", "learn"}},
		{"/goal /learn /plan x", "/goal /learn /plan x", []string{"goal", "learn"}},
		{"/goal /schedule x", "/goal /schedule x", []string{"goal", "schedule"}},
		{"/goal /goal x", "/goal /goal x", []string{"goal", "goal"}},
		{"/schedule /goal /learn z", "/schedule /goal /learn z", []string{"schedule", "goal", "learn"}},
		{"/learn /goal /schedule a b", "/learn /goal /schedule a b", []string{"learn", "goal", "schedule"}},
		{"/goal /learn /plan x", "/goal /learn /plan x", []string{"goal", "learn"}},

		// A non-expandable command ends the leading run and is not expanded.
		{"/goal /plan x", "/goal /plan x", []string{"goal"}},
		{"/goal /boost x", "/goal /boost x", []string{"goal"}},
		{"/goal /teamwork x", "/goal /teamwork x", []string{"goal"}},
		{"/goal /plan", "/goal /plan", []string{"goal"}},
		{"/goal x /plan", "/goal x /plan", []string{"goal"}},
		{"/goal", "/goal", []string{"goal"}},
		{"/learn", "/learn", []string{"learn"}},

		// The leading run is re-serialised with single spaces; the tail after
		// it keeps its own whitespace.
		{"/goal  x  /learn", "/goal x  /learn/learn", []string{"goal", "learn"}},
		{"/goal\tx", "/goal x", []string{"goal"}},
		{"/learn /goal\tx", "/learn /goal x", []string{"learn", "goal"}},

		// Trailing append: the last expandable command AFTER the leading run is
		// appended to the text with no separator, and expanded.
		{"x /goal y", "x /goal y/goal", []string{"goal"}},
		{"x  /goal  y", "x  /goal  y/goal", []string{"goal"}},
		{"do this /goal now", "do this /goal now/goal", []string{"goal"}},
		{"prefix /learn suffix", "prefix /learn suffix/learn", []string{"learn"}},
		{"a /goal b /learn c", "a /goal b /learn c/learn", []string{"learn"}},
		{"a /learn b /goal c", "a /learn b /goal c/goal", []string{"goal"}},
		{"a /goal b /learn c /schedule d", "a /goal b /learn c /schedule d/schedule", []string{"schedule"}},
		{"/goal a /learn b /schedule c", "/goal a /learn b /schedule c/schedule", []string{"goal", "schedule"}},
		{"/goal x /learn y", "/goal x /learn y/learn", []string{"goal", "learn"}},
		{"/goal x /goal y", "/goal x /goal y/goal", []string{"goal", "goal"}},
		{"/goal /boost /learn x", "/goal /boost /learn x/learn", []string{"goal", "learn"}},
		{"/plan x /goal", "/plan x /goal/goal", []string{"goal"}},
		{"/plan /goal x", "/plan /goal x/goal", []string{"goal"}},
		{"/plan /learn", "/plan /learn/learn", []string{"learn"}},
		{"/boost /learn y", "/boost /learn y/learn", []string{"learn"}},
		{"/boost /goal", "/boost /goal/goal", []string{"goal"}},
		{"/boost /goal ship the release", "/boost /goal ship the release/goal", []string{"goal"}},
		{"/teamwork /goal w", "/teamwork /goal w/goal", []string{"goal"}},
		{"/plan /goal /learn x", "/plan /goal /learn x/learn", []string{"learn"}},
		{"/boost /goal /grill-me y", "/boost /goal /grill-me y/grill-me", []string{"grill-me"}},
		{"x /schedule y", "x /schedule y/schedule", []string{"schedule"}},
		{"x /grill-me y", "x /grill-me y/grill-me", []string{"grill-me"}},

		// In the tail, the LAST expandable command wins even when later tokens
		// are commands or nothing:
		{"x /goal y /notacommand", "x /goal y /notacommand/goal", []string{"goal"}},
		{"x /goal y/z", "x /goal y/z/goal", []string{"goal"}},
		{"x /goal y /plan", "x /goal y /plan/goal", []string{"goal"}},
		{"x /goal y /plan z /learn", "x /goal y /plan z /learn/learn", []string{"learn"}},
		{"x /goal y /learn z /plan", "x /goal y /learn z /plan/learn", []string{"learn"}},
		{"hello /goal world /plan more", "hello /goal world /plan more/goal", []string{"goal"}},

		// Nothing expands.
		{"hello world", "hello world", nil},
		{"x /plan y", "x /plan y", nil},
		{"read /etc/hosts", "read /etc/hosts", nil},
		{"x/goal y", "x/goal y", nil},
		{"see http://x/goal here", "see http://x/goal here", nil},
		{"/goal, then", "/goal, then", nil},
		{"x /GOAL y", "x /GOAL y", nil},
	}

	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			text, names := ExpandComposed(tc.in, composedExpandableForTest)
			if text != tc.text {
				t.Errorf("text = %q, want %q", text, tc.text)
			}
			if !reflect.DeepEqual(names, tc.names) {
				t.Errorf("names = %v, want %v", names, tc.names)
			}
		})
	}
}

// TestExpandComposed_NoExpandableNamesAreUntouched pins that a prompt with no
// expandable command comes back byte-identical — including its whitespace.
func TestExpandComposed_NoExpandableNamesAreUntouched(t *testing.T) {
	for _, in := range []string{"hello world", "a  b\tc", "x /plan y", "  padded  "} {
		text, names := ExpandComposed(in, composedExpandableForTest)
		if len(names) != 0 {
			t.Errorf("%q: names = %v, want none", in, names)
		}
		if text != in {
			t.Errorf("%q: text = %q, want it unchanged", in, text)
		}
	}
}

// TestExpandComposed_NonExpandableBreaksTheRun pins the predicate's role: a
// command the caller does not expand is treated as ordinary text, so it ends
// the leading run rather than being skipped over.
func TestExpandComposed_NonExpandableBreaksTheRun(t *testing.T) {
	only := func(name string) bool { return name == "goal" }
	text, names := ExpandComposed("/learn /goal x", only)
	if !reflect.DeepEqual(names, []string{"goal"}) {
		t.Fatalf("names = %v, want [goal]", names)
	}
	// /learn is not expandable here, so the leading run is empty and the last
	// expandable token in the text is /goal, appended to the end.
	if want := "/learn /goal x/goal"; text != want {
		t.Errorf("text = %q, want %q", text, want)
	}
}
