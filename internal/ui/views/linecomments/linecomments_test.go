// linecomments_test.go — the shared line-comment package.
//
// The package owns the reference's comment facts: the model, the gutter
// marker, the wire format the comments travel in and the "Unsent
// Comments" screen. The tests pin the bytes, because the bytes are a
// contract with the agent: the comments are delivered as the next
// message.

package linecomments

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestFormatIsTheReferenceWireFormat: the comment travels as
// "<file><file>:<line>:<comment>" and one newline per comment.
//
// 【strings】 the 1.4.11 binary carries the literal "<file>%s:%d:%s\n"
// once in the whole string table, immediately before the "Unsent
// Comments" header.
func TestFormatIsTheReferenceWireFormat(t *testing.T) {
	c := Comment{Line: 12, Body: "look here"}
	got := Format("a/b.txt", c)
	want := "<file>a/b.txt:12:look here\n"
	if got != want {
		t.Errorf("Format = %q, want %q", got, want)
	}
}

// TestFormatAllJoinsTheComments: the comments are one wire line each,
// in line order.
func TestFormatAllJoinsTheComments(t *testing.T) {
	cs := []Comment{
		{Line: 3, Body: "second"},
		{Line: 1, Body: "first"},
	}
	got := FormatAll("a.txt", cs)
	want := "<file>a.txt:3:second\n<file>a.txt:1:first\n"
	if got != want {
		t.Errorf("FormatAll = %q, want %q", got, want)
	}
}

// TestStoreKeepsOneCommentPerLine: a second comment on the same line
// edits the first.
func TestStoreKeepsOneCommentPerLine(t *testing.T) {
	s := NewStore()
	s.Add(4, "first")
	s.Add(4, "second")
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s.Len())
	}
	c, ok := s.At(4)
	if !ok {
		t.Fatal("the comment must be there")
	}
	if c.Body != "second" {
		t.Errorf("the body = %q, want %q", c.Body, "second")
	}
	if !s.Delete(4) {
		t.Error("Delete must report the comment it dropped")
	}
	if s.Len() != 0 {
		t.Errorf("Len = %d, want 0", s.Len())
	}
}

// TestStoreAllIsInLineOrder: the screen shows the comments in the
// order of their lines.
func TestStoreAllIsInLineOrder(t *testing.T) {
	s := NewStore()
	s.Add(9, "third")
	s.Add(2, "first")
	s.Add(5, "second")
	all := s.All()
	if len(all) != 3 {
		t.Fatalf("All returned %d comments, want 3", len(all))
	}
	for i, want := range []int{2, 5, 9} {
		if all[i].Line != want {
			t.Errorf("comment %d is on line %d, want %d", i, all[i].Line, want)
		}
	}
}

// TestDecideTakesTheCapitalLetters: Y sends, N discards, Esc cancels —
// and the small letters answer nothing.
func TestDecideTakesTheCapitalLetters(t *testing.T) {
	cases := []struct {
		name string
		key  tea.Key
		want Decision
	}{
		{"capital y", tea.Key{Code: 'Y', Text: "Y"}, DecisionSend},
		{"capital y, kitty", tea.Key{Code: 'y', Text: "Y", Mod: tea.ModShift}, DecisionSend},
		{"small y", tea.Key{Code: 'y', Text: "y"}, DecisionNone},
		{"capital n", tea.Key{Code: 'N', Text: "N"}, DecisionDiscard},
		{"capital n, kitty", tea.Key{Code: 'n', Text: "N", Mod: tea.ModShift}, DecisionDiscard},
		{"small n", tea.Key{Code: 'n', Text: "n"}, DecisionNone},
		{"escape", tea.Key{Code: tea.KeyEsc}, DecisionCancel},
		{"other", tea.Key{Code: 'x', Text: "x"}, DecisionNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Decide(tc.key); got != tc.want {
				t.Errorf("Decide = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestScreenRowsCarryTheHeaderAndTheComments: the screen's header is
// the reference's own and the rows are the wire lines.
func TestScreenRowsCarryTheHeaderAndTheComments(t *testing.T) {
	rows := ScreenRows("a.txt", []Comment{{Line: 2, Body: "look here"}})
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, Header) {
		t.Errorf("the screen must carry the %q header; got %q", Header, joined)
	}
	if !strings.Contains(joined, "<file>a.txt:2:look here") {
		t.Errorf("the screen must show the wire line; got %q", joined)
	}
	if !strings.Contains(joined, "send + close") || !strings.Contains(joined, "discard + close") {
		t.Errorf("the screen must name the two answers; got %q", joined)
	}
}
