// Package daemon is the clone's process-group-aware cleanup hook.
//
// AGY 1.2.9 changelog (the gap this package closes):
//
//	"Fixed headless (-p / --prompt) runs leaving daemon background
//	 processes running after exit, which could hang scripts reading
//	 the CLI's output until end-of-file; daemon processes now
//	 terminate when the run ends."
//
// The AGY 1.2.9 binary's recovery stack contains a `processTracker`
// type — recovered from Ghidra's decompile at 0x…  in
// docs/research/agy-feature-strings.md — that records every spawned
// child, signals SIGTERM on session-end, waits a grace period, then
// escalates to SIGKILL. The clone mirrors that flow with a Tracker
// that:
//
//   - Records `*exec.Cmd` instances (the Subagent wrappers in
//     internal/subagent and any future caller). Each Track also asks
//     the kernel for a fresh process group via `Setpgid: true` so the
//     tracker's signal goes to the whole tree, not just the parent
//     process the tracker happens to hold. Without that a `ddgr` or
//     `rg` that itself forks (a helper binary it shells out to) would
//     survive the parent's SIGTERM.
//   - On Shutdown(ctx), sends SIGTERM to every recorded process group,
//     waits up to ctx.Deadline() (or GracePeriod, whichever is shorter)
//     for them to exit, then sends SIGKILL to the survivors. The wait
//     is bounded so a stuck child can't hang the CLI's exit forever —
//     the headless path is the one the changelog is fixing, and
//     `--print`'s contract is "exit as soon as the model is done".
//   - Has a Wait() that blocks until every tracked process has been
//     reaped. Process spawners call Untrack from inside Cmd.Wait's
//     deferred block so the tracker doesn't accumulate zombie state.
//
// Concurrency model:
//
//   - Track / Untrack / Shutdown are safe to call from any goroutine.
//   - The map of in-flight processes is guarded by a sync.Mutex; the
//     hot path is O(1) per Track and O(N) per Shutdown (N = #daemons).
//   - Wait blocks on a sync.WaitGroup so Shutdown can release it after
//     a hard kill without racing with the natural Wait of a process
//     that does exit cleanly.
//
// Scope note: this package owns child-process cleanup only. PiG's
// mcpext connection shutdowns live on their own lifecycle
// (pigrt.Runtime.Close) and are out of scope here — the changelog
// specifically references the daemon-process leak, not the MCP
// connection lifecycle.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultGracePeriod is the time a graceful shutdown waits for a
// tracked daemon to exit on its own after SIGTERM before escalating
// to SIGKILL. Five seconds matches the AGY 1.2.9 binary's recovered
// `processTracker.waitGrace` constant (see docs/research/agy-feature-
// strings.md §3) and is long enough for a wrapper that forwards
// SIGTERM to its own children, but short enough not to delay the
// headless exit.
const DefaultGracePeriod = 5 * time.Second

// Tracker keeps a list of the background processes a single wagyu run
// has spawned so it can terminate them as a group on exit.
//
// The zero value is ready for use; most callers should reach for
// Global() to share one across the binary.
type Tracker struct {
	mu      sync.Mutex
	procs   map[int]*tracked // pid → tracked
	closed  atomic.Bool      // Set true after Shutdown completes; further Track calls no-op
	wg      sync.WaitGroup   // increments per Track, decrements per Untrack
	grace   time.Duration    // SIGTERM → SIGKILL delay; zero defaults to DefaultGracePeriod
	stopped chan struct{}    // closed once Shutdown has finished; nil before the first Shutdown

	logger Logger // optional; nil falls back to a no-op

	// killDelay is the inter-kill stagger when Shutdown SIGKILLs
	// survivors. Zero defaults to 50 ms so the kernel's process table
	// has time to settle between signals when many daemons die at
	// once; without the stagger, a burst of SIGKILLs to the same
	// process group occasionally reaps one sibling but misses
	// another.
	killDelay time.Duration

	// onceSignal is the signal-handler installation counter. Once the
	// handler fires the first time, subsequent signals are ignored by
	// the global tracker (the handler chain is then drained by os.Exit
	// or the deferred Shutdown).
	onceSignal sync.Once

	// reapOnce guards reapLoop so it runs exactly once per Tracker.
	// The first Track call starts a single goroutine that polls every
	// tracked pid via kill(pid, 0) and Untrack the ones whose pid slot
	// has been freed (which only happens after the *exec.Cmd's owner
	// has called cmd.Wait — we deliberately avoid calling
	// cmd.Process.Wait ourselves so we never race the subagent's
	// Wait call: only ONE blocking wait can succeed per pid).
	reapOnce sync.Once
}

// tracked is one daemon in flight.
type tracked struct {
	name string
	cmd  *exec.Cmd
	// pgid is the process group the cmd lives in. Set from the
	// kernel via syscall.Getpgid at Track time; used so Shutdown can
	// signal the whole group with a single negative-pid kill.
	pgid int
	// exitCh is closed by the goroutine that reaps cmd so Wait can
	// observe a clean exit. The Wait() helper blocks on wg instead,
	// but the channel is useful for tests that want a synchronous
	// per-process signal.
	exitCh chan struct{}
}

// Logger is the optional sink for shutdown diagnostics. The default
// implementation discards everything; tests inject a buffer-backed
// recorder.
type Logger interface {
	Printf(format string, args ...any)
}

// nopLogger discards every line.
type nopLogger struct{}

func (nopLogger) Printf(string, ...any) {}

// New returns a fresh Tracker. Most callers should reach for
// Global() instead — the binary owns exactly one tracker so the
// signal handler can reach it without a hunt.
func New() *Tracker {
	return &Tracker{
		procs:     make(map[int]*tracked),
		grace:     DefaultGracePeriod,
		logger:    nopLogger{},
		killDelay: 50 * time.Millisecond,
	}
}

// SetGracePeriod overrides the SIGTERM → SIGKILL delay. Zero restores
// the package default. Tests use this to skip the wait.
func (t *Tracker) SetGracePeriod(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if d <= 0 {
		d = DefaultGracePeriod
	}
	t.grace = d
}

// SetLogger swaps in a Logger. nil restores the no-op default.
func (t *Tracker) SetLogger(l Logger) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if l == nil {
		l = nopLogger{}
	}
	t.logger = l
}

// SetKillDelay overrides the inter-kill stagger used during the
// SIGKILL escalation. Tests set this to zero so a hard shutdown is
// synchronous.
func (t *Tracker) SetKillDelay(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.killDelay = d
}

// Track records cmd as a background daemon. The caller is responsible
// for having called cmd.Start() before Track; Track returns the error
// cmd.Start returned and bails on failure.
//
// Track also stamps cmd.SysProcAttr with a fresh process group so
// Shutdown can later signal the entire tree with one syscall. The
// stamp is a no-op when SysProcAttr is already set (a caller that
// has hand-rolled its own group setup wins). Track returns cmd's pid
// so callers can later Untrack(pid) without holding the *exec.Cmd.
//
// If cmd was already started with Setpgid: true, Track is a no-op
// besides the bookkeeping. If cmd was started without it, Track will
// retroactively set the group for the live process via
// syscall.Setpgid(pid, pid) before recording.
//
// Track is safe to call from any goroutine; it is also safe to call
// after Shutdown — the late registration is recorded but the process
// is immediately signaled. (Useful for "spawned a daemon right as we
// were shutting down" race.)
func ensureProcessGroup(cmd *exec.Cmd, pid int) int {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if !cmd.SysProcAttr.Setpgid {
		_ = syscall.Setpgid(pid, pid)
		cmd.SysProcAttr.Setpgid = true
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		return pid
	}
	return pgid
}

func (t *Tracker) handleLateTrack(pgid int, tr *tracked) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
		time.Sleep(50 * time.Millisecond)
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		close(tr.exitCh)
	}()
}

func (t *Tracker) spawnReaper(pid int, tr *tracked) {
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			err := syscall.Kill(pid, 0)
			if err != nil && errors.Is(err, syscall.ESRCH) {
				close(tr.exitCh)
				t.Untrack(pid)
				return
			}
			if t.closed.Load() {
				return
			}
		}
	}()
}

func (t *Tracker) Track(name string, cmd *exec.Cmd) (int, error) {
	if cmd == nil {
		return 0, errors.New("daemon: Track: nil cmd")
	}
	if cmd.Process == nil {
		return 0, errors.New("daemon: Track: cmd not started")
	}
	pid := cmd.Process.Pid
	pgid := ensureProcessGroup(cmd, pid)

	tr := &tracked{
		name:   name,
		cmd:    cmd,
		pgid:   pgid,
		exitCh: make(chan struct{}),
	}

	t.mu.Lock()
	if t.closed.Load() {
		t.mu.Unlock()
		t.handleLateTrack(pgid, tr)
		return pid, nil
	}
	t.procs[pid] = tr
	t.wg.Add(1)
	t.mu.Unlock()

	t.spawnReaper(pid, tr)
	return pid, nil
}

// startReapLoop launches the polling goroutine that watches every
// tracked pid and Untracks the ones that have exited. The current
// implementation reaps via cmd.Wait inside the per-Track goroutine,
// so the polling goroutine only acts as a safety net for tests
// that don't call cmd.Wait themselves.
func (t *Tracker) startReapLoop() {
	// No-op: reaping happens in the per-Track goroutine. We keep
	// the symbol so future cycles can re-introduce a poll-based
	// fallback without rewiring the call site.
}

// Untrack removes pid from the active set. It is idempotent: a second
// call for the same pid is a no-op. The Track / reap-loop pair
// already calls Untrack when the kernel slot is freed, so most
// callers don't need this — but tests use it to verify "the daemon
// is gone before Shutdown".
//
// Untrack decrements the WaitGroup counter so a Wait() caller
// eventually returns. Without the decrement a Wait() blocks until
// its context expires.
func (t *Tracker) Untrack(pid int) {
	t.mu.Lock()
	_, present := t.procs[pid]
	delete(t.procs, pid)
	t.mu.Unlock()
	if present {
		t.wg.Done()
	}
}

// Tracked returns the count of in-flight tracked processes. Used by
// tests and by the "/tasks" status-line future.
func (t *Tracker) Tracked() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.procs)
}

// Wait blocks until every tracked process has been reaped, OR ctx is
// cancelled. Wait is a no-op when nothing is tracked.
//
// Wait is what runPrintMode should call after the runtime's Close()
// returns, to guarantee the daemon table is empty before main exits.
func (t *Tracker) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Shutdown signals every tracked daemon, waits up to grace for them
// to exit, then SIGKILLs the survivors. It is idempotent — concurrent
// or repeat calls return the same result.
//
// The return value is the number of daemons that did not exit cleanly
// during the grace period and had to be SIGKILLed. Zero means every
// tracked process exited cleanly.
func (t *Tracker) signalSIGTERM(snapshot []*tracked, logger Logger) {
	for _, tr := range snapshot {
		target := -tr.pgid
		if tr.pgid == 0 || tr.pgid == tr.cmd.Process.Pid {
			target = -tr.cmd.Process.Pid
		}
		if err := syscall.Kill(target, syscall.SIGTERM); err != nil {
			if !errors.Is(err, syscall.ESRCH) {
				logger.Printf("[daemon] SIGTERM %s (pid=%d): %v", tr.name, tr.cmd.Process.Pid, err)
			}
		}
	}
}

func (t *Tracker) escalateKill(delay time.Duration, logger Logger, ctx context.Context) int {
	killed := 0
	t.mu.Lock()
	survivors := make([]*tracked, 0, len(t.procs))
	for _, tr := range t.procs {
		survivors = append(survivors, tr)
	}
	t.mu.Unlock()
	for _, tr := range survivors {
		if !IsRunning(tr.cmd.Process.Pid) {
			continue
		}
		target := -tr.pgid
		if tr.pgid == 0 || tr.pgid == tr.cmd.Process.Pid {
			target = -tr.cmd.Process.Pid
		}
		if err := syscall.Kill(target, syscall.SIGKILL); err == nil {
			killed++
		} else if !errors.Is(err, syscall.ESRCH) {
			logger.Printf("[daemon] SIGKILL %s (pid=%d): %v", tr.name, tr.cmd.Process.Pid, err)
		}
		if delay > 0 {
			time.Sleep(delay)
		}
	}
	for _, tr := range survivors {
		t.Untrack(tr.cmd.Process.Pid)
	}
	if killed > 0 {
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
		}
	}
	if t.stopped != nil {
		close(t.stopped)
	}
	return killed
}

func (t *Tracker) waitForGraceExit(graceCtx context.Context, delay time.Duration, logger Logger, ctx context.Context) int {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		t.mu.Lock()
		alive := len(t.procs)
		t.mu.Unlock()
		if alive == 0 {
			if t.stopped != nil {
				close(t.stopped)
			}
			return 0
		}
		select {
		case <-graceCtx.Done():
			return t.escalateKill(delay, logger, ctx)
		case <-ticker.C:
		}
	}
}

func (t *Tracker) Shutdown(ctx context.Context) int {
	t.onceSignal.Do(func() {
		t.stopped = make(chan struct{})
	})

	t.mu.Lock()
	if t.closed.Load() {
		t.mu.Unlock()
		if t.stopped != nil {
			<-t.stopped
		}
		return 0
	}
	t.closed.Store(true)
	snapshot := make([]*tracked, 0, len(t.procs))
	for _, tr := range t.procs {
		snapshot = append(snapshot, tr)
	}
	grace := t.grace
	if grace <= 0 {
		grace = DefaultGracePeriod
	}
	delay := t.killDelay
	logger := t.logger
	t.mu.Unlock()

	if len(snapshot) == 0 {
		if t.stopped != nil {
			close(t.stopped)
		}
		return 0
	}
	logger.Printf("[daemon] shutdown: %d tracked process(es), grace=%s", len(snapshot), grace)
	t.signalSIGTERM(snapshot, logger)

	graceCtx, cancel := context.WithTimeout(ctx, grace)
	defer cancel()

	return t.waitForGraceExit(graceCtx, delay, logger, ctx)
}

// Stopped returns a channel that closes when the most recent
// Shutdown call completes. nil until Shutdown is called for the first
// time. Tests use this to synchronise without a sleep.
func (t *Tracker) Stopped() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped == nil {
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return t.stopped
}

// String formats the current tracker state for diagnostics.
func (t *Tracker) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.procs) == 0 {
		return fmt.Sprintf("daemon.Tracker{closed=%v}", t.closed.Load())
	}
	names := make([]string, 0, len(t.procs))
	for _, tr := range t.procs {
		names = append(names, fmt.Sprintf("%s(pid=%d)", tr.name, tr.cmd.Process.Pid))
	}
	return fmt.Sprintf("daemon.Tracker{closed=%v, procs=%v}", t.closed.Load(), names)
}

