// Package history holds the prompt history: the prompts the composer
// remembers and the navigation over them.
//
// The list is host-free. The host (internal/cliapp) owns the composer,
// the persistence to history.jsonl and the composer's own overlays; it
// hands the list the plain values and takes back the entry to recall.
//
// The blank-prompt rule lives here too. It was introduced for the
// history — a whitespace-only prompt used to slip past the "empty
// prompt" guard and land in the history, the transcript and the session
// at once — and the submit path asks the same question, so the answer
// has one owner.
package history

import "strings"

// Cap is the maximum number of prompts the list remembers.
//
// The oldest entries fall off the top once the list grows past it.
const Cap = 100

// List is the prompt history: the remembered prompts, the navigation's
// index, and the draft the navigation stashed on its way in.
//
// The zero value is an empty list with the navigation idle.
type List struct {
	entries []string
	index   int
	draft   string
}

// IsBlank reports whether s is empty or contains only whitespace runes.
//
// A blank prompt is not a prompt: the history drops it, and the submit
// path refuses it.
//
// Cycle trim-empty-history-messages: a single space (or any run of
// Unicode whitespace) used to slip past the prompt == "" guard and
// produced empty entries in three places at once. AGY 1.2.7's fix is
// to reject blank inputs the same way it rejects "".
func IsBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}

// Push appends the prompt to the list and returns the stored form.
//
// The prompt is trimmed first: a blank prompt is not stored at all, and
// a prompt that repeats the previous entry is not stored twice. The
// list keeps the newest Cap entries.
//
// Cycle trim-empty-history-messages: a prompt of one or more spaces is
// treated as empty and skipped. The history list, transcript, and
// history.jsonl would otherwise all receive empty entries — wasted
// screen rows and confusing Up/Down landings.
//
// Surrounding whitespace is trimmed before the dedupe check and the
// append so a follow-up "  hello  " after "hello" collapses to the
// same entry instead of producing a near-duplicate. The trimmed form
// is what reaches history.jsonl and the Up/Down buffer.
func (l *List) Push(prompt string) (string, bool) {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return "", false
	}
	if len(l.entries) > 0 && l.entries[len(l.entries)-1] == trimmed {
		return "", false
	}
	l.entries = append(l.entries, trimmed)
	if len(l.entries) > Cap {
		l.entries = l.entries[len(l.entries)-Cap:]
	}
	return trimmed, true
}

// Load seeds the list from the persistent store.
//
// An already-populated list is preserved: the store is a seed, not an
// overwrite. (Cycle 286: agy shares prompt history across all sessions
// globally, so a fresh CLI session recalls the most recent prompt from
// earlier sessions — but a list that already holds entries keeps them.)
func (l *List) Load(entries []string) {
	if len(l.entries) > 0 {
		return
	}
	l.entries = append(l.entries, entries...)
}

// Up recalls the entry one slot back.
//
// current is the composer's own value. It is stashed as the draft when
// the navigation starts, so that Down can hand it back. The second
// result is false when there is nothing to recall: the list is empty,
// or the navigation already stands on the oldest entry.
func (l *List) Up(current string) (string, bool) {
	if len(l.entries) == 0 {
		return "", false
	}
	if l.index == 0 {
		l.draft = current
	}
	if l.index >= len(l.entries) {
		return "", false
	}
	l.index++
	return l.entries[len(l.entries)-l.index], true
}

// Down recalls the entry one slot forward.
//
// The draft comes back when the navigation leaves the history. The
// second result is false when the navigation is not running.
func (l *List) Down() (string, bool) {
	if l.index == 0 {
		return "", false
	}
	l.index--
	if l.index == 0 {
		return l.draft, true
	}
	return l.entries[len(l.entries)-l.index], true
}

// Reset leaves the navigation and drops the stashed draft.
//
// The draft is only read when the navigation leaves the history, and the
// next Up overwrites it — so dropping it here is the same behaviour as
// dropping it only when the navigation was running.
func (l *List) Reset() {
	l.index = 0
	l.draft = ""
}

// Entries returns the remembered prompts, oldest first.
func (l *List) Entries() []string {
	return l.entries
}

// Index returns the navigation's index: 0 when the navigation is idle,
// and the number of slots back from the newest entry otherwise.
func (l *List) Index() int {
	return l.index
}

// Len returns the number of remembered prompts.
func (l *List) Len() int {
	return len(l.entries)
}
