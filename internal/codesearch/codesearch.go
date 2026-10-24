// Package codesearch is the runtime behind /codesearch: a single
// workspace regex / literal search with progressive streaming and a
// ripgrep fallback.
//
// AGY 1.4.0 added /codesearch (cycle 228): it spawns `rg --json` to stream
// matches as they appear, lets the user cancel mid-search with Esc, and
// falls back to a local walk when the rg binary cannot be executed
// (typically an endpoint security agent blocking it). This package
// implements the same contract against any ripgrep-compatible binary the
// caller names, with a filepath.Walk + regexp fallback when rg is
// missing or fails to launch.
//
// All four behaviours the spec calls out:
//
//   - regex by default, -F / --literal for fixed-string matches;
//   - f:<glob> / file:<glob> repeatable include/exclude globs (the
//     ripgrep path forwards these as -g / --iglob; the fallback walks
//     the tree and applies the same globs);
//   - progressive streaming: every match reaches Search.Matches as a
//     separate value, then the channel is closed when the run ends;
//   - cancellation: ctx cancel kills the child (or stops the walk).
//
// One match is one *line*. The reference collapses multi-match lines to
// a single result and only highlights the first match — see
// cmd/wagyu/slash_codesearch.go for the equivalent csHit shaping.
//
// The runner is concurrency-safe: a single Search owns one child process
// (or one goroutine) and Cancel can be called from any goroutine. The
// panel hooks live on Search.State, which the panel polls on every
// Update — no extra plumbing is required.
package codesearch

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
)

// State is the lifecycle of a Search.
const (
	StateRunning   = "running"
	StateDone      = "done"
	StateCancelled = "cancelled"
	StateFailed    = "failed"
)

// MaxHits is the hard cap the reference applies. Above it the runner
// keeps counting overflow but stops streaming further matches so the
// panel never grows without bound.
const MaxHits = 1000

// SkipDirs are directories /codesearch never descends into. The
// ripgrep path forwards these as --glob '!NAME'; the fallback path
// drops them at the WalkDir level. .git/.hg/.jj hold VCS bookkeeping;
// node_modules / .venv / vendor are dependency noise.
var SkipDirs = map[string]bool{
	".git":         true,
	".hg":          true,
	".jj":          true,
	"node_modules": true,
	".venv":        true,
	"vendor":       true,
}

// Match is one matching line. File is slash-separated relative to the
// search root, Line/Col are 1-based, Text is the verbatim line (may
// contain CJK or other multi-byte text). Display is the same string
// truncated to a target cell width with an ellipsis; the panel picks
// one or the other based on terminal width.
type Match struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Text    string `json:"text"`
	Display string `json:"display,omitempty"`
}

// Search is a single codesearch invocation. The runner fills in
// State, Err, StartedAt, EndedAt as it runs; Matches is the streaming
// channel and is closed exactly once when the run finishes.
//
// ID is a short tag the panel can use to log the run (defaults to the
// start time formatted as "cs-<unix>").
type Search struct {
	ID        string     `json:"id"`
	Query     string     `json:"query"`
	Literal   bool       `json:"literal"`
	Include   []string   `json:"include,omitempty"`
	Exclude   []string   `json:"exclude,omitempty"`
	Root      string     `json:"root"`
	Limit     int        `json:"limit"`
	Matches   chan Match `json:"-"`
	State     string     `json:"state"`
	Err       error      `json:"-"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   time.Time  `json:"ended_at"`

	// FilesHit counts the unique files that produced at least one
	// streamed match. The panel's "Found N matches in K files" line
	// uses it.
	FilesHit int `json:"files_hit"`

	// mu guards State. The runner goroutine writes it while the panel
	// — on the UI goroutine — renders the status line and reads it to
	// decide whether Esc cancels a live search, so the field is never
	// read directly: CurrentState and setState are the only accessors.
	mu sync.Mutex
}

// CurrentState returns the search's state. It takes the lock the
// runner holds while it moves the search between states, so the panel
// never reads a half-written value.
func (s *Search) CurrentState() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.State
}

// setState records a new state under the lock.
func (s *Search) setState(state string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.State = state
}

func (s *Search) closeMatches() {
	defer func() { _ = recover() }()
	close(s.Matches)
}

// countSent is the number of matches already pushed into s.Matches.
// It walks the channel up to its current buffered count (the channel
// is being filled by the same goroutine that calls send, so reads
// here are safe — the panel only reads in Update).
func countSent(s *Search) int {
	if s.Matches == nil {
		return 0
	}
	return len(s.Matches)
}

// send pushes m into s.Matches unless ctx is cancelled. Returns the
// ctx error so the caller can stop iterating.
func send(ctx context.Context, s *Search, m Match) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.Matches <- m:
		return nil
	}
}

// Args is the parsed form of the slash command's argument tail. It is
// distinct from Search because ParseArgs does not require a root path
// — the runner fills that in. Keeping the two types separate lets the
// slash command echo the "needs a query" message before any runner is
// created.
type Args struct {
	Query   string
	Literal bool
	Include []string
	Exclude []string
}

// ErrEmptyQuery is returned by ParseArgs when no non-flag tokens remain
// in the argument tail.
var ErrEmptyQuery = errors.New("codesearch: empty query")

// ParseArgs splits the raw argument tail into the structured Args. It
// recognises:
//
//   - -F / --literal as a fixed-string match (ripgrep's flag);
//   - -- as the end-of-flags marker, so positional `-F` becomes the
//     pattern after `--`;
//   - f:<glob> as an include glob (repeatable);
//   - file:<glob> as an exclude glob (repeatable).
//
// Globs are extracted from anywhere in the token stream — both before
// and after the pattern — because rg's `-g` order is independent. The
// remaining tokens (after the flags and globs are removed) become the
// pattern verbatim.
//
// Unrecognised flags are passed through as part of the pattern (rg
// behaves the same way: an unknown flag becomes literal text once the
// runner's arg list ends).
func ParseArgs(raw string) (Args, error) {
	a := Args{}
	var pattern []string
	tokens := strings.Fields(raw)
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if tok == "--" {
			// Everything after -- is positional, including -F.
			pattern = append(pattern, tokens[i+1:]...)
			break
		}
		if strings.HasPrefix(tok, "f:") {
			a.Include = append(a.Include, strings.TrimPrefix(tok, "f:"))
			continue
		}
		if strings.HasPrefix(tok, "file:") {
			a.Exclude = append(a.Exclude, strings.TrimPrefix(tok, "file:"))
			continue
		}
		if strings.HasPrefix(tok, "-") {
			switch tok {
			case "-F", "--literal":
				a.Literal = true
				continue
			default:
				// Unknown flag — pass through verbatim, like rg.
				pattern = append(pattern, tokens[i:]...)
				break
			}
			break
		}
		pattern = append(pattern, tok)
	}
	a.Query = strings.TrimSpace(strings.Join(pattern, " "))
	if a.Query == "" {
		return a, ErrEmptyQuery
	}
	return a, nil
}

// Truncate returns s clipped to at most width *cells* (rune-aware via
// runewidth), with a trailing '…' on the clipped side when the input
// is too wide. Returns s unchanged when it already fits.
//
// This is the helper the panel calls to populate Match.Display — see
// cycle 217's `textcut` reference. CJK characters and emoji width > 1,
// so the rune count alone (utf8.RuneCountInString) undercounts them;
// runewidth.StringWidth is the byte-faithful measure.
func Truncate(s string, width int) string {
	if width <= 0 || runewidth.StringWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(s)
	var b strings.Builder
	used := 0
	target := width - 1 // reserve one cell for the ellipsis
	for _, r := range runes {
		rw := runewidth.RuneWidth(r)
		if used+rw > target {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	b.WriteString("…")
	return b.String()
}

// utf8RuneCount is utf8.RuneCountInString as a local helper so the
// ripgrep path doesn't need a separate import.
func utf8RuneCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}
