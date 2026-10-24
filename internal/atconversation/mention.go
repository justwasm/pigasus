// Package atconversation implements @conversation: mention autocomplete
// with a 2-second remote-search timeout and a local-fallback list.
//
// AGY 1.3.4 changelog line 38:
//
//	Fixed `@conversation:` suggestions getting stuck on `Fetching
//	matches for ...` when conversation search is slow; after 2 seconds
//	the list now falls back to your local conversations.
package atconversation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
)

// MentionPrefix is the trigger the user types at the prompt.
const MentionPrefix = "@conversation:"

// Timeout caps the remote conversation search (exactly 2 seconds per AGY 1.3.4).
const Timeout = 2 * time.Second

// MaxMatches caps the rendered dropdown regardless of source.
const MaxMatches = 50

// Source labels where a result list came from.
type Source string

const (
	SourceRemote        Source = "remote"
	SourceLocalFallback Source = "local-fallback"
	SourceEmpty         Source = "empty"
)

// Match is one row in the dropdown.
type Match struct {
	ID    string
	Label string
}

// LocalFn is the contract for listing local conversations matching query.
type LocalFn func(query string) []Match

// SearchFn is the contract for searching remote conversations with a context deadline.
type SearchFn func(ctx context.Context, query string) ([]Match, error)

// DefaultSearcher is the default remote searcher (simulates timeout).
var DefaultSearcher SearchFn = func(ctx context.Context, query string) ([]Match, error) {
	select {
	case <-time.After(3 * time.Second):
		return nil, errors.New("at-conversation: remote searcher not implemented")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// ParseMention recognises `@conversation:` and `@conversation:<query>`
// at the start of a prompt buffer.
func ParseMention(buf string) (string, bool) {
	if !strings.HasPrefix(buf, MentionPrefix) {
		return "", false
	}
	query := strings.TrimSpace(buf[len(MentionPrefix):])
	return query, true
}

// SearchMatches runs the remote search with a 2-second deadline and,
// on timeout or error, returns the local convstore list filtered by the query.
func SearchMatches(searcher SearchFn, local LocalFn, query string) ([]Match, Source) {
	if searcher == nil {
		searcher = DefaultSearcher
	}
	if local == nil {
		local = ListLocalMatches
	}

	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()

	remote, err := searcher(ctx, query)
	if err == nil && len(remote) > 0 {
		if len(remote) > MaxMatches {
			remote = remote[:MaxMatches]
		}
		return remote, SourceRemote
	}

	rows := local(query)
	if len(rows) > 0 {
		return rows, SourceLocalFallback
	}
	return nil, SourceEmpty
}

// ListLocalMatches reads internal/convstore on this machine and filters by query.
func ListLocalMatches(query string) []Match {
	all, err := convstore.ListConversations()
	if err != nil || len(all) == 0 {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]Match, 0, len(all))
	for _, c := range all {
		if q != "" {
			title := strings.ToLower(c.Title)
			id := strings.ToLower(c.ID)
			if !strings.Contains(title, q) && !strings.Contains(id, q) {
				continue
			}
		}
		label := c.Title
		if label == "" {
			label = c.ID
		}
		out = append(out, Match{ID: c.ID, Label: label})
		if len(out) >= MaxMatches {
			break
		}
	}
	return out
}

// MentionInsert returns the literal buffer the TUI should swap into the input
// when a user picks a conversation from the dropdown.
func MentionInsert(id string) string {
	return fmt.Sprintf(`@conversation:"%s"`, id)
}
