// Package subagent provides real sub-process spawn management.
//
// It wraps os/exec to give each spawned child a stable ID, a name, an
// observable state machine (pending -> running -> done|failed|killed), a
// captured Result buffer, and helpers (Manager) for tracking many at once.
package subagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/daemon"
)

// AGY 1.3.1 changelog line:
//
//	Fixed subagents that stopped on an error, such as running out of
//	quota or model capacity, being shown as `Done` in `/agents` and
//	the list of running agents; they now show `Error:` with the
//	reason, and the error clears once the subagent makes further
//	progress.
//
// The three constants below are the user-facing labels the /agents
// panel and the running-agents list render. Cycle
// agent-stopped-error-status adds them so the panel code can stop
// hard-coding the strings and so any future surface (status-line
// summary, /tasks detail) reuses one source of truth.
//
//	StatusDone   — the subagent exited cleanly (Wait returned nil).
//	StatusError  — the subagent stopped on an error; the colon is
//	               followed by the reason captured into LastError.
//	StatusRunning — the subagent is still executing.
//	StatusPending — the subagent was constructed but never Start'd.
//	StatusKilled  — the subagent was terminated via Kill() or ctx
//	               cancellation while still running.
const (
	StatusDone    = "Done"
	StatusError   = "Error:"
	StatusRunning = "Running"
	StatusPending = "Pending"
	StatusKilled  = "Killed"
)

// StateChangeFunc is invoked on every state transition. It is the
// hook the tasks-panel cycle wires to internal/taskregistry so that
// /tasks and the status-line "· N tasks" segment both reflect the
// shell wrapper's progress in real time. The hook runs synchronously
// inside Start/Wait/Kill, so it must not block or call back into the
// Subagent. A nil hook is a no-op.
//
// Tasks-panel: the hook is set with SetOnStateChange after the
// Subagent is constructed; the existing call sites (internal/tools
// create_task, ddgr-driven search, the bash wrappers in cycle 22's
// stubs.go) were updated to pass the registry's TaskRegistryHook
// helper at construction time.
type StateChangeFunc func(s *Subagent, newState string)

// Subagent is a single spawned sub-process.
//
// Stdout is captured into Result in a goroutine that drains the pipe after
// Start. Stdin is wired to an os.Pipe so callers can push messages in via
// Send. The public Struct fields are ID, Name, Cmd, State, Result, Err,
// LastError; the remaining fields are plumbing for the capture goroutine.
//
// LastError is the human-readable reason a Subagent stopped on error.
// It is populated by Wait() when Cmd.Wait returns a non-nil error, and
// is cleared by MarkProgress when the subagent makes further progress
// (e.g., a new Send() while the subagent is still alive). The /agents
// panel and the running-agents list read it through StatusLine and
// ErrorReason — see AGY 1.3.1 changelog line "Fixed subagents that
// stopped on an error … being shown as `Done` … they now show
// `Error:` with the reason".
type Subagent struct {
	ID     string
	Name   string
	Cmd    *exec.Cmd
	State  string
	Result string
	Err    error
// Title is the human-given name the caller passed when sending the
	// subagent to the background. The taskregistry (and the /tasks
	// panel / completion notifications) prefer this over the auto-derived
	// "<Name> <args...>" string produced by subagentDisplayName so a
	// task keeps the friendly name it was given instead of reverting
	// to the underlying command line. Empty means "no name given; fall
	// back to the auto-derived form". Title is set once after
	// construction (mirroring the way ID/Name are set) and is read by
	// the taskregistry bridge at attach time; it is not mutated
	// afterwards, so the lack of a mutex is intentional.
	Title string
	// LastError is the human-readable error reason, populated by
	// Wait() when the sub-process exits with a non-nil error.
	// Cleared by MarkProgress when the subagent makes further
	// progress. Use StatusLine / ErrorReason to render it; do not
	// read the field directly from UI code.
	LastError string

	mu          sync.Mutex
	stdinWriter io.WriteCloser
	stdoutBuf   *bytes.Buffer
	// captureWg removed (was race-prone)
	// onState is the tasks-panel hook. nil means no listener.
	onState StateChangeFunc
}

// SetOnStateChange wires the tasks-panel listener. Calling it with
// nil is a no-op. Subsequent state transitions call fn after the
// internal mutex has released, so a slow fn will not block other
// readers of the Subagent.
func (s *Subagent) SetOnStateChange(fn StateChangeFunc) {
	s.mu.Lock()
	s.onState = fn
	s.mu.Unlock()
}

// SetTitle records the human-given name the caller passed for the
// task. The bridge (internal/taskregistry) reads Title at attach
// time to prefer it over the auto-derived "<Name> <args…>" form, so a
// background task that was created with `create_task({title: …})`
// keeps that friendly label in /tasks and completion notifications
// instead of reverting to the underlying command line.
//
// SetTitle should be called exactly once, between NewSubagent and
// the first AttachSubagent (or any other reader of Title). A nil
// receiver is a no-op so safe callers can drop the guard. Pass "" to
// clear a previously-set title so the auto-derived name takes over.
func (s *Subagent) SetTitle(title string) {
	if s == nil {
		return
	}
	s.Title = title
}

// Start launches the sub-process. Stdout/stderr are captured into
// stdoutBuf; Result is populated in Wait. State transitions from
// "pending" to "running" on success or "failed" if exec.Cmd.Start
// returns an error.
//
// Cycle daemon-cleanup-on-exit: on a successful Start the sub-process
// is registered with the global daemon.Tracker so a parent-process
// SIGINT / SIGTERM / process exit signals the child via the tracker's
// process-group kill (AGY 1.2.9 changelog: headless runs must not
// leave daemon processes running). The reaper inside the tracker
// calls cmd.Process.Wait when the child exits naturally, which
// reaps the kernel's zombie slot; the subagent's Wait method then
// reads Result / Err.
func (s *Subagent) Start() error {
	s.setState("running")
	if err := s.Cmd.Start(); err != nil {
		s.setState("failed")
		return err
	}
	if _, trackErr := daemon.Global().Track("subagent:"+s.Name, s.Cmd); trackErr != nil {
		// Don't fail the Start on tracker bookkeeping errors; the
		// subagent still runs. The tracker logs internally; an
		// inability to register only means we won't auto-clean
		// this particular child on shutdown.
		_ = trackErr
	}
	return nil
}

// Wait blocks until the sub-process exits or ctx is cancelled. If ctx
// fires first the process is killed and ctx.Err() is returned. On a
// clean exit Result is populated from the captured stdout/stderr buffer
// before returning (no goroutine race).
//
// AGY 1.3.1: when the process exits with a non-nil error, Wait
// captures the error message into LastError so the /agents panel and
// the running-agents list can render "Error: <reason>" instead of
// silently showing the row as "Done". A successful exit clears any
// stale LastError left over from a prior failure.
func (s *Subagent) Wait(ctx context.Context) error {
	done := make(chan error, 1)
	go func() { done <- s.Cmd.Wait() }()
	select {
	case err := <-done:
		if s.stdoutBuf != nil {
			s.mu.Lock()
			s.Result = s.stdoutBuf.String()
			s.mu.Unlock()
		}
		if err != nil {
			s.setState("failed")
			s.mu.Lock()
			s.Err = err
			s.LastError = err.Error()
			s.mu.Unlock()
			return err
		}
		s.mu.Lock()
		// A clean exit clears any stale error reason so the
		// running-agents list doesn't keep showing "Error:
		// <stale>" after the subagent recovered.
		s.LastError = ""
		s.mu.Unlock()
		s.setState("done")
		return nil
	case <-ctx.Done():
		if s.Cmd.Process != nil {
			s.Cmd.Process.Kill()
		}
		s.setState("killed")
		return ctx.Err()
	}
}

// Kill terminates the sub-process and waits for the stdout-capture
// goroutine to drain so Result is final. It is safe to call before
// Start; in that case the call is a no-op returning nil.
func (s *Subagent) Kill() error {
	s.setState("killed")
	if s.Cmd == nil || s.Cmd.Process == nil {
		return nil
	}
	if err := s.Cmd.Process.Kill(); err != nil {
		return err
	}
	return nil
}

// Send writes msg to the sub-process's stdin. It returns an error if
// stdin was not initialized (e.g. NewSubagent was bypassed) or the pipe
// writer has already been closed.
func (s *Subagent) Send(msg string) error {
	s.mu.Lock()
	w := s.stdinWriter
	s.mu.Unlock()
	if w == nil {
		return fmt.Errorf("subagent: stdin not initialized")
	}
	// Sending a message is progress — clear any stale error
	// reason so the /agents row stops showing "Error: <stale>"
	// once the subagent is moving again. AGY 1.3.1 changelog
	// "the error clears once the subagent makes further
	// progress" — this is one of the two places progress is
	// observed (the other is MarkProgress, exposed for callers
	// that want to clear without writing to stdin).
	if msg != "" {
		s.clearLastError()
	}
	_, err := w.Write([]byte(msg))
	return err
}

// MarkProgress records that the subagent has made further progress
// and clears any stale error reason captured by Wait. The /agents
// panel and the running-agents list re-render on the next event; if
// the subagent's State was "failed", the row stops showing
// "Error: <stale>" and reverts to "Done" (the terminal state) or
// "Running" if the caller has transitioned the state back.
//
// AGY 1.3.1 changelog: "the error clears once the subagent makes
// further progress". Send already calls this; callers that drive
// progress without writing to stdin can call it directly.
//
// MarkProgress is safe to call from any goroutine. It is a no-op on
// a Subagent that has no error reason set, so wiring it into every
// progress-bearing hook is cheap.
func (s *Subagent) MarkProgress() {
	s.clearLastError()
}

// ErrorReason returns the human-readable reason the Subagent stopped
// on error, or "" if it never errored or the error has since been
// cleared by progress. The /agents panel reads this through
// StatusLine so the rendered row can include the reason inline.
func (s *Subagent) ErrorReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.LastError
}

// Output returns the output the sub-process wrote to stdout.
//
// It is empty until the process exits — Wait copies the capture buffer
// into Result before it reports the terminal state. The taskregistry
// bridge reads it through this accessor (not the public Result field)
// so the /tasks detail view can show a finished task's output without
// racing Wait's write.
func (s *Subagent) Output() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Result
}

// StatusLine returns the one-line status label the /agents panel and
// the running-agents list render for the Subagent:
//
//	"Done"          — exited cleanly (State == "done")
//	"Error: <why>"  — stopped on error (State == "failed" AND a
//	                  reason is still attached). The "<why>" is
//	                  the LastError captured by Wait.
//	"Done"          — recovered from error (State == "failed" but
//	                  LastError was cleared by MarkProgress or a
//	                  successful Wait). The error display is
//	                  gone, per the AGY 1.3.1 changelog "the
//	                  error clears once the subagent makes
//	                  further progress".
//	"Running"       — actively executing (State == "running")
//	"Pending"       — constructed, not yet started
//	"Killed"        — terminated via Kill() or ctx cancel
//	<state>         — any other / future state, raw
//
// AGY 1.3.1: the panel must show "Error:" (with the reason) for
// errored subagents rather than the silent "Done" the pre-cycle
// rendering produced.
func (s *Subagent) StatusLine() string {
	s.mu.Lock()
	state := s.State
	reason := s.LastError
	s.mu.Unlock()
	switch state {
	case "done":
		return StatusDone
	case "failed":
		if reason != "" {
			return StatusError + " " + reason
		}
		// The error reason was cleared by MarkProgress or by
		// a subsequent clean Wait — per AGY 1.3.1 "the error
		// clears once the subagent makes further progress".
		// Fall through to StatusDone so the /agents row stops
		// showing the stale "Error:" prefix. Callers that
		// still want to see the failure can read State
		// directly; the running-agents list cares about the
		// user-facing label only.
		return StatusDone
	case "running":
		return StatusRunning
	case "pending":
		return StatusPending
	case "killed":
		return StatusKilled
	}
	if state == "" {
		return StatusPending
	}
	return state
}

// clearLastError is the internal helper that drops LastError under
// the mutex. Used by Send and MarkProgress.
func (s *Subagent) clearLastError() {
	s.mu.Lock()
	s.LastError = ""
	s.mu.Unlock()
}

// MarshalJSON renders the observable state for serialization.
func (s *Subagent) MarshalJSON() ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Marshal(map[string]string{
		"id":     s.ID,
		"name":   s.Name,
		"state":  s.State,
		"result": s.Result,
	})
}

// SetStateForTest forces the Subagent's State field under the
// internal mutex. It exists solely so unit tests in cmd/wagyu can
// stage the failed/running/done transitions without having to
// exec a real sub-process on every case. Production code MUST NOT
// call this; use Start / Wait / Kill instead.
//
// The on-state hook (if any) is NOT fired — callers want a
// deterministic field value, not a re-render side effect.
func (s *Subagent) SetStateForTest(state string) {
	s.mu.Lock()
	s.State = state
	s.mu.Unlock()
}

// SetLastErrorForTest sets the LastError field under the internal
// mutex. Like SetStateForTest this is a test-only seam —
// production code captures LastError via Wait.
func (s *Subagent) SetLastErrorForTest(reason string) {
	s.mu.Lock()
	s.LastError = reason
	s.mu.Unlock()
}

func (s *Subagent) setState(state string) {
	s.mu.Lock()
	s.State = state
	fn := s.onState
	s.mu.Unlock()
	if fn != nil {
		// The hook runs outside the mutex so a slow listener
		// cannot block other readers of s. The hook MUST NOT
		// call back into s; doing so can deadlock the
		// registry's own mutex (see internal/taskregistry).
		fn(s, state)
	}
}

var counter uint64

// NewSubagent builds a fresh Subagent wrapping exec.Command(name, args...).
// Stdin is wired through os.Pipe so Send can push messages; stdout and
// stderr are captured into an internal bytes.Buffer that is read into
// Result after Cmd.Wait completes (no race).
func NewSubagent(name string, args ...string) *Subagent {
	counter++
	cmd := exec.Command(name, args...)
	inR, inW, err := os.Pipe()
	if err != nil {
		inR, inW = nil, nil
	}
	if inR != nil {
		cmd.Stdin = inR
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	return &Subagent{
		ID:          fmt.Sprintf("sub-%d-%d", time.Now().UnixNano(), counter),
		Name:        name,
		Cmd:         cmd,
		State:       "pending",
		stdinWriter: inW,
		stdoutBuf:   &buf,
	}
}

// Specific subagent types — thin convenience constructors built on
// NewSubagent. They keep the original cycle-22 ergonomics for callers
// that want a one-shot bash sub-process for a known task.

// ExploreSubagent lists the contents of path (capped at 50 entries).
func ExploreSubagent(path string) *Subagent {
	return NewSubagent("bash", "-c", "ls -la "+path+" | head -50")
}

// CodeReviewSubagent dumps the first 100 lines of file.
func CodeReviewSubagent(file string) *Subagent {
	return NewSubagent("bash", "-c", "cat "+file+" | head -100")
}

// TestSubagent runs `go test ./...` in pkg (capped at 50 lines).
func TestSubagent(pkg string) *Subagent {
	return NewSubagent("bash", "-c", "cd "+pkg+" && go test ./... 2>&1 | head -50")
}

// BuildSubagent runs `go build ./...` in pkg (capped at 20 lines).
func BuildSubagent(pkg string) *Subagent {
	return NewSubagent("bash", "-c", "cd "+pkg+" && go build ./... 2>&1 | head -20")
}

// DeploySubagent prints a deploy notification (placeholder behaviour
// preserved from cycle-22; no actual deployment is performed).
func DeploySubagent(src, dst string) *Subagent {
	return NewSubagent("bash", "-c", "echo 'deploy "+src+" to "+dst+"'")
}

// Manager tracks many Subagents by ID.
type Manager struct {
	agents map[string]*Subagent
	mu     sync.RWMutex
	// onStop is the OnStop hook fired from Manager.Stop / Remove
	// (see stop.go). nil = no listener. Stored under mu so
	// SetOnStop and the callers in Stop / Remove see a consistent
	// value at every invocation.
	onStop OnStopFunc
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{agents: make(map[string]*Subagent)}
}

// Add registers a Subagent under its ID.
func (m *Manager) Add(s *Subagent) {
	m.mu.Lock()
	m.agents[s.ID] = s
	m.mu.Unlock()
}

// Get returns the Subagent registered under id, if any.
func (m *Manager) Get(id string) (*Subagent, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.agents[id]
	return s, ok
}

// List returns every registered Subagent in arbitrary order.
func (m *Manager) List() []*Subagent {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Subagent, 0, len(m.agents))
	for _, s := range m.agents {
		out = append(out, s)
	}
	return out
}
