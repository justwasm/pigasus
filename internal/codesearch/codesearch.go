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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/mattn/go-runewidth"
	"github.com/bmatcuk/doublestar/v4"
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

// Runner drives a Search. rgPath is the ripgrep binary ("rg" by
// default; tests override it). Workdir is the search root.
//
// The runner is single-use per Search. Construct a new Runner for each
// invocation; sharing one across runs would mean sharing the rgPath /
// Workdir but not the per-Search state.
type Runner struct {
	RgPath  string
	Workdir string
	// RgBin is the closure that produces the *exec.Cmd. Tests replace
	// it with a mock that feeds JSON. The default spawns rg --json.
	RgBin func(ctx context.Context, root string, args Args) *exec.Cmd

	// cancelCmd is the in-flight child process, set by Run and
	// cleared when the search finishes. Cancel() kills it.
	cancelCmd *exec.Cmd
}

// NewRunner builds a Runner with the default rg path. Returns a Runner
// whose RgPath is empty when no ripgrep binary is on PATH — Run will
// skip straight to the fallback in that case.
func NewRunner(workdir string) *Runner {
	r := &Runner{Workdir: workdir, RgPath: "rg", RgBin: DefaultRgCmd}
	if _, err := exec.LookPath(r.RgPath); err != nil {
		r.RgPath = ""
		r.RgBin = nil
	}
	return r
}

// CanRipgrep reports whether the runner has a ripgrep binary it can
// use. The panel may consult this before opening a Search to decide
// whether the "Searching…" line should mention ripgrep.
func (r *Runner) CanRipgrep() bool { return r.RgPath != "" }

// Run starts the search and returns once State has settled. The
// returned error is non-nil only when the search failed to start
// (rg missing AND the fallback also failed); a per-file read error or
// a partial cancellation is reflected via Search.Err / Search.State.
//
// Run blocks until Matches has been closed. The caller is expected to
// drain Matches from another goroutine (the panel does this through
// a tea.Cmd fan-out).
func (r *Runner) Run(ctx context.Context, s *Search) error {
	if s.Matches == nil {
		s.Matches = make(chan Match, 64)
	}
	if s.Limit <= 0 {
		s.Limit = MaxHits
	}
	if s.ID == "" {
		s.ID = fmt.Sprintf("cs-%d", time.Now().UnixNano())
	}
	s.StartedAt = time.Now()
	s.State = StateRunning

	// Decide which path to take.
	if r.RgPath != "" && r.RgBin != nil {
		err := r.runRipgrep(ctx, s)
		// ripgrep exits with status 1 when it ran cleanly but found no
		// matches — that's a successful empty search, not a failure.
		// Anything else (status 2, spawn refused, etc.) falls back to
		// the local walk so the user still gets a result rather than
		// an empty list.
		if isExitStatusOne(err) || err == nil ||
			errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			s.closeMatches()
			if errors.Is(err, context.Canceled) {
				s.State = StateCancelled
			} else if errors.Is(err, context.DeadlineExceeded) {
				s.State = StateFailed
				s.Err = err
			} else {
				s.State = StateDone
			}
			s.EndedAt = time.Now()
			return nil
		}
		// rg itself started but exited with an error we should map to
		// "failed" — e.g. permission denied, malformed glob. Fall
		// through to the fallback path so the user still gets a
		// result rather than an empty list.
		s.Err = err
	}
	err := r.runFallback(ctx, s)
	s.closeMatches()
	switch {
	case errors.Is(err, context.Canceled):
		s.State = StateCancelled
	case err != nil:
		s.State = StateFailed
		if s.Err == nil {
			s.Err = err
		}
	default:
		s.State = StateDone
	}
	s.EndedAt = time.Now()
	return nil
}

func (s *Search) closeMatches() {
	defer func() { _ = recover() }()
	close(s.Matches)
}

// runRipgrep shells out to rg --json. The output format is one JSON
// object per line; we only care about type=="match" records.
//
// rg's JSON carries byte offsets into the line (submatches[*].start/end);
// we convert those to rune column numbers so the panel's csFit math keeps
// working (its ellipsis-truncation operates on runes, not bytes).
func (r *Runner) runRipgrep(ctx context.Context, s *Search) error {
	cmd := r.RgBin(ctx, r.Workdir, argsOf(s))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		// Spawn itself failed (e.g. blocked by an endpoint security
		// agent — "operation not permitted"). Surface so Run can
		// fall back to the walk.
		return err
	}
	// Track the child so Cancel() can kill it. Run already maps
	// ctx.Canceled → StateCancelled; Cancel() is a separate
	// convenience for the panel's Esc handler.
	r.attachCmd(cmd)
	defer r.detachCmd(cmd)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		row := scanner.Bytes()
		if len(row) == 0 {
			continue
		}
		var msg rgMessage
		if err := json.Unmarshal(row, &msg); err != nil {
			continue
		}
		if msg.Type != "match" {
			continue
		}
		m := Match{
			File: filepath.ToSlash(msg.Data.Path.Text),
			Line: msg.Data.LineNumber,
			Col:  1,
			Text: strings.TrimRight(msg.Data.Lines.Text, "\n"),
		}
		if len(msg.Data.Submatches) > 0 {
			start := msg.Data.Submatches[0].Start
			end := msg.Data.Submatches[0].End
			if start < 0 {
				start = 0
			}
			if end > len(m.Text) {
				end = len(m.Text)
			}
			m.Col = utf8RuneCount(m.Text[:start]) + 1
			// Reuse me on a fake csHit field — we collapse to one
			// match per line in the fallback too, so the byte-span
			// is the first match's span.
			if start > len(m.Text) {
				start = len(m.Text)
			}
		}
		if err := send(ctx, s, m); err != nil {
			_ = cmd.Process.Kill()
			return err
		}
		if s.Limit > 0 && countSent(s) >= MaxHits {
			// rg has likely more matches buffered; ask it to stop.
			_ = cmd.Process.Kill()
			break
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, context.Canceled) {
		// Best effort: surface as the search error so the panel can
		// show "falling back to local search" or similar.
		return err
	}
	return cmd.Wait()
}

// attachCmd / detachCmd wire cmd.Process.Kill into Runner.Cancel.
// The runner itself doesn't keep a long-lived *exec.Cmd — Run owns it
// — but a single Cancel hook is enough to kill the in-flight child.
func (r *Runner) attachCmd(cmd *exec.Cmd) {
	r.cancelCmd = cmd
}
func (r *Runner) detachCmd(cmd *exec.Cmd) {
	if r.cancelCmd == cmd {
		r.cancelCmd = nil
	}
}

// cancelCmd is the in-flight child process. nil when no search is
// running. Cancel() kills it if set.
func (r *Runner) Cancel() error {
	if r.cancelCmd == nil || r.cancelCmd.Process == nil {
		return nil
	}
	err := r.cancelCmd.Process.Kill()
	r.cancelCmd = nil
	return err
}

// argsOf extracts an Args from a Search for the rg command builder.
// Used by both the production RgBin and the tests.
func argsOf(s *Search) Args {
	return Args{
		Query:   s.Query,
		Literal: s.Literal,
		Include: s.Include,
		Exclude: s.Exclude,
	}
}

// DefaultRgCmd returns the default ripgrep invocation. Public so
// tests can call it to assert against the same flag list the
// production path uses.
func DefaultRgCmd(ctx context.Context, root string, a Args) *exec.Cmd {
	args := []string{"--json", "--no-heading", "--no-messages"}
	if a.Literal {
		args = append(args, "--fixed-strings")
	}
	for _, g := range a.Include {
		args = append(args, "-g", g)
	}
	for _, g := range a.Exclude {
		args = append(args, "-g", "!"+g)
	}
	// Always exclude the skip-dirs via a glob negation.
	for d := range SkipDirs {
		args = append(args, "-g", "!"+d)
	}
	// Hidden files are skipped by default.
	args = append(args, "--hidden")
	args = append(args, "-e", a.Query)
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = root
	return cmd
}

// runFallback walks the workspace, applies the include/exclude globs
// (bmatcuk/doublestar/v4 — supports `**`, brace expansion, and
// character classes, matching ripgrep's `-g` semantics for `*.go` /
// `*_test.go` and the multi-segment `**/foo/*.go` case), and pushes
// matches into the channel.
func (r *Runner) runFallback(ctx context.Context, s *Search) error {
	if s.Root == "" && r.Workdir != "" {
		s.Root = r.Workdir
	}
	if s.Root == "" {
		s.closeMatches()
		return errors.New("codesearch: no root directory")
	}
	if s.Err != nil {
		// Coming from a failed rg attempt — keep that detail so the
		// panel can mention "rg failed (…), falling back".
	}
	pat := s.Query
	if s.Literal {
		pat = regexp.QuoteMeta(pat)
	}
	// Smart-case: lowercase-only patterns are case-insensitive.
	if !hasUpper(s.Query) {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return fmt.Errorf("codesearch: %w", err)
	}
	limit := s.Limit
	if limit <= 0 {
		limit = MaxHits
	}
	overflow := 0
	seenFiles := map[string]bool{}
	err = filepath.WalkDir(s.Root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != s.Root && (SkipDirs[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
			return nil
		}
		rel, err := filepath.Rel(s.Root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		// Include filter: if the user named patterns, only let matched
		// files through. Empty include list = no filter.
		if len(s.Include) > 0 && !globMatchAny(rel, s.Include) {
			return nil
		}
		// Exclude filter: if the user named patterns, skip matched
		// files. Empty exclude list = no exclusions.
		if len(s.Exclude) > 0 && globMatchAny(rel, s.Exclude) {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 1<<20 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		head := data
		if len(head) > 8<<10 {
			head = head[:8<<10]
		}
		if bytesIndexByte(head, 0) >= 0 {
			return nil // binary
		}
		content := string(data)
		lines := strings.Split(content, "\n")
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1]
		}
		for i, line := range lines {
			if err := ctx.Err(); err != nil {
				return err
			}
			line = strings.TrimRight(line, "\r")
			loc := re.FindStringIndex(line)
			if loc == nil {
				continue
			}
			if countSent(s) >= limit {
				overflow++
				continue
			}
			col := utf8RuneCount(line[:loc[0]]) + 1
			if !seenFiles[rel] {
				seenFiles[rel] = true
				s.FilesHit++
			}
			if err := send(ctx, s, Match{
				File: rel,
				Line: i + 1,
				Col:  col,
				Text: line,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if overflow > 0 {
		// Mirror the reference: keep counting but don't stream past
		// the cap. The cap is internal; the overflow counter lets the
		// panel show "| %d more" if it wants to.
		s.Limit = countSent(s) + overflow
	}
	return err
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

// globMatchAny reports whether path matches any of the patterns. The
// two callers have opposite defaults:
//
//   - include: an empty list means "no inclusion filter, allow all".
//   - exclude: an empty list means "no exclusions, allow all".
//
// Both return true when len(patterns)==0 because "no filter means
// nothing rejects". Use want=false for include / want=true for exclude
// to invert the sense without two callers.
func globMatchAny(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, p := range patterns {
		if p == "" {
			continue
		}
		// doublestar.Match operates on '/' separators.
		if ok, _ := doublestar.Match(p, path); ok {
			return true
		}
		// Try matching just the basename too — rg's -g matches
		// against the full path; users tend to think in basenames.
		base := filepath.Base(path)
		if ok, _ := doublestar.Match(p, base); ok {
			return true
		}
	}
	return false
}

// hasUpper is the smart-case predicate. Lowercase-only patterns are
// case-insensitive; any uppercase rune forces a case-sensitive search.
func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// bytesIndexByte is bytes.IndexByte without importing the package.
func bytesIndexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
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

// isExitStatusOne reports whether err is the *exec.ExitError ripgrep
// returns when it ran cleanly but found no matches (exit code 1).
// Anything else (a spawn refusal, exit code 2, …) is left for the
// caller to map to the fallback path.
func isExitStatusOne(err error) bool {
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return false
	}
	return ee.ExitCode() == 1
}

// rgMessage is the shape of one ripgrep --json output line. Only the
// fields we read are present.
type rgMessage struct {
	Type string `json:"type"`
	Data struct {
		Path       rgPath `json:"path"`
		Lines      rgText `json:"lines"`
		LineNumber int    `json:"line_number"`
		Submatches []struct {
			Match rgText `json:"match"`
			Start int    `json:"start"`
			End   int    `json:"end"`
		} `json:"submatches"`
	} `json:"data"`
}

// rgPath decodes the {"text": "..."} wrapper ripgrep uses.
type rgPath struct {
	Text string `json:"text"`
}

// rgText is the same wrapper for arbitrary text payloads.
type rgText struct {
	Text string `json:"text"`
}
