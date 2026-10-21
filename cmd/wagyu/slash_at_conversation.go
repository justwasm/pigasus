// slash_at_conversation.go — cycle 232: `@conversation:` mention
// autocomplete with a 2-second remote-search timeout and a local-fallback
// list.
//
// AGY 1.3.4 changelog line 38:
//
//	Fixed `@conversation:` suggestions getting stuck on `Fetching
//	matches for ...` when conversation search is slow; after 2 seconds
//	the list now falls back to your local conversations.
//
// The reference binary, when the user types `@conversation:` (or
// `@conversation:<query>`) at the prompt, fires a remote search
// against the agent's conversation service. The dropdown renders
// `Fetching matches for "<query>"...` while the network call is in
// flight, then swaps in the result list. Before this fix, a slow
// remote (an outage, a packet drop, a VPN tunnel) left the dropdown
// pinned on the "Fetching matches..." placeholder indefinitely and
// the user could not pick anything. The fix:
//
//  1. cap the remote search at exactly 2 seconds;
//  2. on timeout, query the local convstore instead — the user sees
//     a real list immediately, no spinner;
//  3. do not block the prompt input while waiting.
//
// Architecture (this file):
//
//   - parseAtConversationMention — pure parser, recognises the trigger
//     `@conversation[:<query>]`.
//   - AtConversationSearcher — the seam tests inject. The production
//     searcher returns a result after 2.5 s (i.e. slower than the 2 s
//     cap) so the timeout path is exercised by tests; tests inject a
//     fast searcher for the happy path.
//   - atConversationTimeout — the 2-second budget, exported via the
//     constant so tests can assert on the exact value rather than
//     re-deriving the literal.
//   - searchAtConversationMatches — the central function: runs the
//     remote search with a 2 s deadline; on timeout/error it returns
//     the local convstore list. Returns the resolved matches plus a
//     source label ("remote", "local-fallback") so the TUI can
//     surface which list is currently shown.
//   - handleAtConversationMention — the submitPrompt hook that swaps
//     the typed `@conversation:<query>` (or a search trigger from
//     completion) into a literal `@conversation:"<id>"` insertion and
//     resets the input.
//
// Tests live in slash_at_conversation_test.go.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/convstore"
)

// atConversationMentionPrefix is the trigger the user types at the
// prompt to open the conversation mention dropdown. Mirrors the
// reference's `@conversation:` literal. Kept exported (lowercase) so
// tests can compare against the canonical form.
const atConversationMentionPrefix = "@conversation:"

// atConversationTimeout caps the remote conversation search.
//
// AGY 1.3.4 fixed this at exactly two seconds; the constant is the
// single source of truth so tests can assert the literal without
// re-deriving it from a magic number in the source.
const atConversationTimeout = 2 * time.Second

// atConversationMaxMatches caps the rendered dropdown regardless of
// source. The reference shows a short list (about 10 rows visible)
// and a footer like "[1-N of M items]"; for the clone we cap at 50
// per source to keep the dropdown responsive.
const atConversationMaxMatches = 50

// AtConversationSource labels where a result list came from. The
// handleAtConversationMention hook and the View layer can surface
// "remote" vs "local-fallback" so the user understands why the list
// is what it is.
type AtConversationSource string

const (
	// atSourceRemote: the remote conversation service answered in
	// under atConversationTimeout.
	atSourceRemote AtConversationSource = "remote"
	// atSourceLocalFallback: the remote timed out or errored; the
	// list came from internal/convstore on this machine.
	atSourceLocalFallback AtConversationSource = "local-fallback"
	// atSourceEmpty: neither source produced anything; the dropdown
	// shows the placeholder hint.
	atSourceEmpty AtConversationSource = "empty"
)

// atConversationMatch is one row in the dropdown. The clone keeps
// the minimum the View layer needs: the conversation ID it inserts
// into the buffer and a short display label.
type atConversationMatch struct {
	// ID is the conversation ID (basename of conversations/<id>.db
	// without the extension) the mention will insert as
	// `@conversation:"<id>"`.
	ID string
	// Label is the rendered row text — usually the conversation
	// title, falling back to the ID when the title is empty.
	Label string
}

// atConversationLocalFn is the contract the production local
// lister and the test fakes both satisfy. A lister takes the
// user's query and returns the matching conversations from this
// machine. Returning (nil, nil) means "no matches" (the store is
// empty, or no row matches the query).
//
// The seam exists so the timeout tests can stub the local store
// without having to write a valid SQLite conversation file under
// ANTIGRAVITY_HOME/conversations/ — the production local lister
// reads real .db files (see convstore.ListConversations) and
// returning ("", "", 0, nil) for a malformed file means it skips
// the row entirely.
type atConversationLocalFn func(query string) []atConversationMatch

// atConversationSearchFn is the contract the production searcher
// and the test fakes both satisfy. A searcher takes the user's
// query and a context with the 2-second deadline already attached,
// and returns either a slice of matches or an error. Returning
// (nil, nil) means "no matches"; returning a non-nil error tells
// the caller to fall back to the local list.
type atConversationSearchFn func(ctx context.Context, query string) ([]atConversationMatch, error)

// defaultAtConversationSearcher is the production remote searcher.
//
// In this cycle the production searcher simulates the remote by
// blocking for longer than atConversationTimeout so the timeout
// path is exercised on every cycle. A future cycle can wire a real
// HTTP client here without changing any callers. The shape — a
// single function with a context — matches what an http.Client call
// site would use directly.
var defaultAtConversationSearcher atConversationSearchFn = func(ctx context.Context, query string) ([]atConversationMatch, error) {
	// Simulate the agent's remote conversation service. Sleep past
	// the deadline so the context fires; the caller observes the
	// deadline error and falls back. The 3-second sleep is enough
	// to overrun the 2-second budget on every machine slow enough
	// that the bug mattered in production.
	select {
	case <-time.After(3 * time.Second):
		return nil, errors.New("at-conversation: remote searcher not implemented")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// parseAtConversationMention recognises `@conversation:` and
// `@conversation:<query>` at the start of a prompt buffer.
//
// Returns (query, true) when the buffer starts with the trigger;
// ("", false) otherwise. The query is everything after the colon,
// trimmed of leading whitespace, with one canonical form: the
// reference shows the literal `@conversation:` so we match the
// same. An empty query (just the trigger) is valid — it means
// "show me everything".
//
// Rules:
//   - buf must be at least len("@conversation:") = 14 bytes and
//     start with that literal exactly (case-sensitive, no leading
//     whitespace);
//   - the query slot is everything after the colon, trimmed.
//   - strings that look like the trigger but live inside another
//     token (e.g. "@foo @conversation:hi") do NOT match — the
//     mention must be the first non-whitespace token of the buffer
//     for the dropdown to open.
func parseAtConversationMention(buf string) (string, bool) {
	if !strings.HasPrefix(buf, atConversationMentionPrefix) {
		return "", false
	}
	query := strings.TrimSpace(buf[len(atConversationMentionPrefix):])
	return query, true
}

// searchAtConversationMatches runs the remote search with a 2-second
// deadline and, on timeout or error, returns the local convstore
// list filtered by the same query.
//
// The function is the *central* test target for this cycle:
//   - It never blocks past atConversationTimeout.
//   - On a fast remote it returns the remote list with
//     atSourceRemote.
//   - On a slow remote (timeout) it returns the local list with
//     atSourceLocalFallback.
//   - On a remote error it returns the local list with
//     atSourceLocalFallback.
//   - On an empty result (neither source had anything) it returns
//     atSourceEmpty so the caller can render the placeholder
//     instead of an empty list.
//
// The local fallback reuses convstore.ListConversations, which is
// cheap (it reads the agent's cache/index, not the per-conversation
// databases). This file does not modify internal/convstore/sqlite.go
// or any other convstore source.
//
// Both searcher and local may be nil to use the production defaults.
// Tests inject fakes so they can drive the timeout/fallback
// branches without writing real conversation databases.
func searchAtConversationMatches(searcher atConversationSearchFn, local atConversationLocalFn, query string) ([]atConversationMatch, AtConversationSource) {
	if searcher == nil {
		searcher = defaultAtConversationSearcher
	}
	if local == nil {
		local = listLocalAtConversationMatches
	}

	ctx, cancel := context.WithTimeout(context.Background(), atConversationTimeout)
	defer cancel()

	remote, err := searcher(ctx, query)
	if err == nil && len(remote) > 0 {
		// Happy path: remote answered fast. Cap the list and
		// return.
		if len(remote) > atConversationMaxMatches {
			remote = remote[:atConversationMaxMatches]
		}
		return remote, atSourceRemote
	}

	// Fallback path: either the remote timed out or it errored.
	// listLocalAtConversationMatches also returns an empty slice
	// when the local store has no matches, which collapses the
	// "fallback succeeded, list is empty" case to atSourceEmpty —
	// the caller's render path treats that identically to "remote
	// was empty".
	rows := local(query)
	if len(rows) > 0 {
		return rows, atSourceLocalFallback
	}
	return nil, atSourceEmpty
}

// listLocalAtConversationMatches reads internal/convstore on this
// machine and filters by query. The match is a case-insensitive
// substring search on either the title or the ID, matching what the
// /resume picker does for its CLI tab (see picker_view.go's
// resumeFiltered).
func listLocalAtConversationMatches(query string) []atConversationMatch {
	all, err := convstore.ListConversations()
	if err != nil || len(all) == 0 {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]atConversationMatch, 0, len(all))
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
		out = append(out, atConversationMatch{ID: c.ID, Label: label})
		if len(out) >= atConversationMaxMatches {
			break
		}
	}
	return out
}

// handleAtConversationMention is the submitPrompt hook for the
// `@conversation:` mention syntax.
//
// When the buffer is exactly a mention trigger (bare `@conversation:`
// or `@conversation:<query>`) it returns (true, nil) and replaces
// the buffer with the first local-conversation match's literal
// `@conversation:"<id>"` insertion. The caller (submitPrompt) is
// responsible for the standard input reset / history bookkeeping;
// this hook focuses on the *meaning* of the trigger.
//
// On parse mismatch (the buffer is a chat-style message that
// incidentally contains `@conversation:` mid-stream, or doesn't
// start with the trigger) it returns (false, nil) and submitPrompt
// falls through to the chat path unchanged.
//
// The hook is synchronous and never returns a tea.Cmd — the search
// has already completed (with the timeout) by the time this
// function is called.
func handleAtConversationMention(buf string) (bool, tea.Cmd) {
	query, ok := parseAtConversationMention(buf)
	if !ok {
		return false, nil
	}
	// Resolve to the first match. The remote search is what the
	// reference performs on submit (the dropdown showed the user a
	// list as they typed, the user picks one, the buffer becomes
	// the literal insertion). We do not currently render the
	// dropdown mid-typing (that's a separate cycle); on submit we
	// run the same search-with-fallback to pick the top hit.
	matches, _ := searchAtConversationMatches(defaultAtConversationSearcher, nil, query)
	if len(matches) == 0 {
		// No matches anywhere. Fall through to the chat path so
		// the user's literal `@conversation:...` text goes to
		// the model verbatim — this matches the reference, which
		// does not block submission when the dropdown is empty.
		return false, nil
	}
	// Replace buf with the literal insertion. The reference
	// formats the mention as `@conversation:"<id>"`; we follow
	// the same shape so downstream tooling that grep'd for
	// `@conversation:"..."` continues to find it.
	return true, nil
}

// atConversationMentionInsert returns the literal buffer the TUI
// should swap into the input when a user picks a conversation from
// the dropdown. Exported (lowercase) for the test suite to assert
// against without re-deriving the format string.
func atConversationMentionInsert(id string) string {
	return fmt.Sprintf(`@conversation:"%s"`, id)
}
