package conversation_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/prompt/mentions/conversation"
)

func TestParseMention_HappyPath(t *testing.T) {
	tests := []struct {
		raw   string
		query string
		ok    bool
	}{
		{conversation.MentionPrefix, "", true},
		{"@conversation:hello", "hello", true},
		{"@conversation:hello world", "hello world", true},
		{"@conversation:  hello  ", "hello", true},
		{"@conversation:\tfoo\t", "foo", true},
		{"@conversation:auth flow", "auth flow", true},
	}
	for _, tc := range tests {
		gotQ, gotOK := conversation.ParseMention(tc.raw)
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
		if _, ok := conversation.ParseMention(tc.raw); ok {
			t.Errorf("ParseMention(%q) matched; want rejection (%s)", tc.raw, tc.why)
		}
	}
}

func TestSearchMatches_HappyPathRemote(t *testing.T) {
	searcher := func(ctx context.Context, query string) ([]conversation.Match, error) {
		return []conversation.Match{{ID: "1", Label: "First"}}, nil
	}
	matches, source := conversation.SearchMatches(searcher, nil, "query")
	if source != conversation.SourceRemote {
		t.Errorf("source = %q, want %q", source, conversation.SourceRemote)
	}
	if len(matches) != 1 || matches[0].ID != "1" {
		t.Errorf("matches = %+v, want ID '1'", matches)
	}
}

func TestSearchMatches_TimeoutFallsBackToLocal(t *testing.T) {
	searcher := func(ctx context.Context, query string) ([]conversation.Match, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	local := func(query string) []conversation.Match {
		return []conversation.Match{{ID: "loc-1", Label: "Local 1"}}
	}
	matches, source := conversation.SearchMatches(searcher, local, "test")
	if source != conversation.SourceLocalFallback {
		t.Errorf("source = %q, want %q", source, conversation.SourceLocalFallback)
	}
	if len(matches) != 1 || matches[0].ID != "loc-1" {
		t.Errorf("matches = %+v, want ID 'loc-1'", matches)
	}
}

func TestSearchMatches_RemoteErrorFallsBackToLocal(t *testing.T) {
	searcher := func(ctx context.Context, query string) ([]conversation.Match, error) {
		return nil, errors.New("network failure")
	}
	local := func(query string) []conversation.Match {
		return []conversation.Match{{ID: "loc-2", Label: "Local 2"}}
	}
	matches, source := conversation.SearchMatches(searcher, local, "test")
	if source != conversation.SourceLocalFallback {
		t.Errorf("source = %q, want %q", source, conversation.SourceLocalFallback)
	}
	if len(matches) != 1 || matches[0].ID != "loc-2" {
		t.Errorf("matches = %+v, want ID 'loc-2'", matches)
	}
}

func TestSearchMatches_EmptyBoth(t *testing.T) {
	searcher := func(ctx context.Context, query string) ([]conversation.Match, error) {
		return nil, nil
	}
	local := func(query string) []conversation.Match {
		return nil
	}
	matches, source := conversation.SearchMatches(searcher, local, "test")
	if source != conversation.SourceEmpty {
		t.Errorf("source = %q, want %q", source, conversation.SourceEmpty)
	}
	if len(matches) != 0 {
		t.Errorf("expected empty matches, got %+v", matches)
	}
}

func TestMentionInsert(t *testing.T) {
	got := conversation.MentionInsert("conv-xyz")
	want := `@conversation:"conv-xyz"`
	if got != want {
		t.Errorf("MentionInsert = %q, want %q", got, want)
	}
}

func TestTimeoutConstantIsTwoSeconds(t *testing.T) {
	if conversation.Timeout != 2*time.Second {
		t.Errorf("Timeout = %v, want 2s", conversation.Timeout)
	}
}
