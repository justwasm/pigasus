// runner.go — Search runner driving ripgrep child process or fallback.
package codesearch

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
)

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
	//
	// mu guards cancelCmd: the runner goroutine attaches and detaches
	// the child while the UI goroutine cancels it (Esc), so the pointer
	// is never read or written outside the lock.
	mu       sync.Mutex
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

// attachCmd / detachCmd wire cmd.Process.Kill into Runner.Cancel.
// The runner itself doesn't keep a long-lived *exec.Cmd — Run owns it
// — but a single Cancel hook is enough to kill the in-flight child.
func (r *Runner) attachCmd(cmd *exec.Cmd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelCmd = cmd
}

func (r *Runner) detachCmd(cmd *exec.Cmd) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelCmd == cmd {
		r.cancelCmd = nil
	}
}

// childPID returns the pid of the attached child process, or 0 when no
// child is attached. It takes the lock the attach/detach path holds.
func (r *Runner) childPID() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelCmd == nil || r.cancelCmd.Process == nil {
		return 0
	}
	return r.cancelCmd.Process.Pid
}

// Cancel kills the in-flight child process if set.
func (r *Runner) Cancel() error {
	r.mu.Lock()
	cmd := r.cancelCmd
	r.cancelCmd = nil
	r.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func initSearch(s *Search) {
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
	s.setState(StateRunning)
}

func finishSearch(s *Search, err error) {
	s.closeMatches()
	switch {
	case errors.Is(err, context.Canceled):
		s.setState(StateCancelled)
	case errors.Is(err, context.DeadlineExceeded):
		s.setState(StateFailed)
		s.Err = err
	case err != nil:
		s.setState(StateFailed)
		if s.Err == nil {
			s.Err = err
		}
	default:
		s.setState(StateDone)
	}
	s.EndedAt = time.Now()
}

func (r *Runner) handleRipgrep(ctx context.Context, s *Search) bool {
	if r.RgPath == "" || r.RgBin == nil {
		return false
	}
	err := r.runRipgrep(ctx, s)
	if isExitStatusOne(err) {
		err = nil
	}
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		finishSearch(s, err)
		return true
	}
	s.Err = err
	return false
}

// Run starts the search and returns once State has settled. The
// returned error is non-nil only when the search failed to start
// (rg missing AND the fallback also failed); a per-file read error or
// a partial cancellation is reflected via Search.Err / Search.State.
//
// Run blocks until Matches has been closed. The caller is expected to
// drain Matches from another goroutine (the panel does this through
// a tea.Cmd fan-out).
func (r *Runner) Run(ctx context.Context, s *Search) error {
	initSearch(s)
	if r.handleRipgrep(ctx, s) {
		return nil
	}
	err := r.runFallback(ctx, s)
	finishSearch(s, err)
	return nil
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
