package subagentrouter

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestParsePrompt_HappyPath(t *testing.T) {
	tests := []struct {
		raw     string
		name    string
		message string
		ok      bool
	}{
		{"@explore-bot find the auth code", "explore-bot", "find the auth code", true},
		{"@explore-bot ", "explore-bot", "", true},
		{"@Explore-Bot hi", "Explore-Bot", "hi", true},
		{"@a x", "a", "x", true},
		{"@foo.bar.baz hello world", "foo.bar.baz", "hello world", true},
		{"@_under_score hi", "_under_score", "hi", true},
		{"@name with\ttabs", "name", "with\ttabs", true},
		{"@name line\nbreak", "name", "line\nbreak", true},
	}
	for _, tc := range tests {
		name, msg, ok := ParsePrompt(tc.raw)
		if ok != tc.ok || name != tc.name || msg != tc.message {
			t.Errorf("ParsePrompt(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.raw, name, msg, ok, tc.name, tc.message, tc.ok)
		}
	}
}

func TestParsePrompt_RejectsNonMatch(t *testing.T) {
	tests := []struct {
		raw string
		why string
	}{
		{"", "empty buffer"},
		{"@", "bare @, no name"},
		{"@explore-bot", "still typing — no whitespace yet"},
		{"hello world", "no leading @"},
		{"/help", "slash command"},
		{"@/", "name slot contains slash"},
		{"@file/foo.txt", "name slot contains slash (regression)"},
		{"@explore-bot@next", "name slot contains @"},
		{"@name!", "name slot contains punctuation"},
		{strings.Repeat("a", 65), "name over 64 chars"},
		{"@explo\x01re-bot hello", "name slot contains control"},
		{"@ hi", "empty name"},
	}
	for _, tc := range tests {
		name, msg, ok := ParsePrompt(tc.raw)
		if ok {
			t.Errorf("ParsePrompt(%q) unexpectedly matched (%s): name=%q, msg=%q",
				tc.raw, tc.why, name, msg)
		}
	}
}

func TestRoute(t *testing.T) {
	m := NewManager()
	s := &fakeSender{}
	m.Register("bot", s)

	entry, matched, err := Route(context.Background(), m, "@bot hello")
	if !matched || err != nil || entry == nil || entry.Name != "bot" {
		t.Fatalf("Route failed: matched=%v, err=%v, entry=%v", matched, err, entry)
	}
	if s.Calls() != 1 || s.LastMsg() != "hello" {
		t.Fatalf("Sender received %d calls, lastMsg=%q", s.Calls(), s.LastMsg())
	}

	_, matched, err = Route(context.Background(), m, "@unknown hello")
	if !matched || !errors.Is(err, ErrUnknownName) {
		t.Fatalf("expected ErrUnknownName, got %v", err)
	}

	_, matched, err = Route(context.Background(), m, "@bot ")
	if !matched || !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("expected ErrEmptyBody, got %v", err)
	}
}
