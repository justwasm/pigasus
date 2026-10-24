package atconversation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/atconversation"
)

func TestParseMention_HappyPath(t *testing.T) {
	tests := []struct {
		raw   string
		query string
		ok    bool
	}{
		{atconversation.MentionPrefix, "", true},
		{"@conversation:hello", "hello", true},
		{"@conversation:hello world", "hello world", true},
		{"@conversation:  hello  ", "hello", true},
		{"@conversation:\tfoo\t", "foo", true},
		{"@conversation:auth flow", "auth flow", true},
	}
	for _, tc := range tests {
		gotQ, gotOK := atconversation.ParseMention(tc.raw)
		if gotOK != tc.ok || gotQ != tc.query {
			t.Errorf("ParseMention(%q) = (%q,%v), want (%q,%v)",
				tc.raw, gotQ, gotOK, tc.query, tc.ok)
		}
	}
}

func TestParseMention_RejectsNonMatch(t *testing.T) {
	tests := []struct {
		raw string
		why string
	}{
		{"", "empty buffer"},
		{"hello world", "no leading @"},
		{"@hello", "different @-mention"},
		{"@conversation", "missing colon"},
		{"@conversation ", "missing colon (trailing space)"},
		{"@Conversation:hello", "wrong case"},
		{"inside @conversation:hi text", "not at start"},
	}
	for _, tc := range tests {
		if _, ok := atconversation.ParseMention(tc.raw); ok {
			t.Errorf("ParseMention(%q) matched; want rejection (%s)", tc.raw, tc.why)
		}
	}
}

func TestSearchMatches_HappyPathRemote(t *testing.T) {
	searcher := func(ctx context.Context, query string) ([]atconversation.Match, error) {
		return []atconversation.Match{{ID: "1", Label: "First"}}, nil
	}
	matches, source := atconversation.SearchMatches(searcher, nil, "query")
	if source != atconversation.SourceRemote {
		t.Errorf("source = %q, want %q", source, atconversation.SourceRemote)
	}
	if len(matches) != 1 || matches[0].ID != "1" {
		t.Errorf("matches = %+v, want ID '1'", matches)
	}
}

func TestSearchMatches_TimeoutFallsBackToLocal(t *testing.T) {
	searcher := func(ctx context.Context, query string) ([]atconversation.Match, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	local := func(query string) []atconversation.Match {
		return []atconversation.Match{{ID: "loc-1", Label: "Local 1"}}
	}
	matches, source := atconversation.SearchMatches(searcher, local, "test")
	if source != atconversation.SourceLocalFallback {
		t.Errorf("source = %q, want %q", source, atconversation.SourceLocalFallback)
	}
	if len(matches) != 1 || matches[0].ID != "loc-1" {
		t.Errorf("matches = %+v, want ID 'loc-1'", matches)
	}
}

func TestSearchMatches_RemoteErrorFallsBackToLocal(t *testing.T) {
	searcher := func(ctx context.Context, query string) ([]atconversation.Match, error) {
		return nil, errors.New("network failure")
	}
	local := func(query string) []atconversation.Match {
		return []atconversation.Match{{ID: "loc-2", Label: "Local 2"}}
	}
	matches, source := atconversation.SearchMatches(searcher, local, "test")
	if source != atconversation.SourceLocalFallback {
		t.Errorf("source = %q, want %q", source, atconversation.SourceLocalFallback)
	}
	if len(matches) != 1 || matches[0].ID != "loc-2" {
		t.Errorf("matches = %+v, want ID 'loc-2'", matches)
	}
}

func TestSearchMatches_EmptyBoth(t *testing.T) {
	searcher := func(ctx context.Context, query string) ([]atconversation.Match, error) {
		return nil, nil
	}
	local := func(query string) []atconversation.Match {
		return nil
	}
	matches, source := atconversation.SearchMatches(searcher, local, "test")
	if source != atconversation.SourceEmpty {
		t.Errorf("source = %q, want %q", source, atconversation.SourceEmpty)
	}
	if len(matches) != 0 {
		t.Errorf("expected empty matches, got %+v", matches)
	}
}

func TestMentionInsert(t *testing.T) {
	got := atconversation.MentionInsert("conv-xyz")
	want := `@conversation:"conv-xyz"`
	if got != want {
		t.Errorf("MentionInsert = %q, want %q", got, want)
	}
}

func TestTimeoutConstantIsTwoSeconds(t *testing.T) {
	if atconversation.Timeout != 2*time.Second {
		t.Errorf("Timeout = %v, want 2s", atconversation.Timeout)
	}
}
