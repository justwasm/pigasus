package slashquote_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/slashquote"
)

func TestInsertText(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"hello\n", "hello"},
		{"hello\n\n", "hello"},
		{"multi\nline\n", "multi\nline"},
		{"plain", "plain"},
		{"", ""},
	}
	for _, c := range cases {
		if got := slashquote.InsertText(c.in); got != c.want {
			t.Errorf("InsertText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveQuote_HappyPath(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "first\n"},
		{Role: "assistant", Content: "reply 1"},
		{Role: "user", Content: "second\n"},
		{Role: "assistant", Content: "reply 2"},
	}

	// default (n=1)
	got, errStr := slashquote.ResolveQuote("", sess)
	if errStr != "" || got != "second" {
		t.Errorf("ResolveQuote(\"\") = (%q, %q), want (\"second\", \"\")", got, errStr)
	}

	// n=2
	got2, errStr2 := slashquote.ResolveQuote("2", sess)
	if errStr2 != "" || got2 != "first" {
		t.Errorf("ResolveQuote(\"2\") = (%q, %q), want (\"first\", \"\")", got2, errStr2)
	}
}

func TestResolveQuote_Errors(t *testing.T) {
	// Empty session
	_, err := slashquote.ResolveQuote("", nil)
	if err != "No previous turn to quote." {
		t.Errorf("expected no previous turn error, got %q", err)
	}

	// Invalid arg
	_, err = slashquote.ResolveQuote("abc", []backend.Message{{Role: "user", Content: "u"}})
	if err != "Usage: /quote [n]" {
		t.Errorf("expected usage error, got %q", err)
	}

	// Out of range
	_, err = slashquote.ResolveQuote("5", []backend.Message{{Role: "user", Content: "u"}})
	if err != "Quote index out of range (1..1)." {
		t.Errorf("expected out of range error, got %q", err)
	}
}
