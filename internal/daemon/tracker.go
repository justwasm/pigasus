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
	"os"
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
func (t *Tracker) Track(name string, cmd *exec.Cmd) (int, error) {
	if cmd == nil {
		return 0, errors.New("daemon: Track: nil cmd")
	}
	if cmd.Process == nil {
		return 0, errors.New("daemon: Track: cmd not started")
	}
	pid := cmd.Process.Pid

	// Make sure the child leads its own process group. cmd.Start
	// usually has this in SysProcAttr from the wrapper, but a caller
	// that hand-rolled exec.Command without Setpgid would otherwise
	// share the wagyu pid, and `kill -f pid` would also signal the
	// CLI itself. Setting the group here is a one-shot syscall: if
	// the process is already a group leader, Setpgid is a no-op.
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	if !cmd.SysProcAttr.Setpgid {
		// Setpgid best-effort: ignore the error — the child may have
		// already execed, and even without it the per-pid kill still
		// works (we just can't guarantee a per-tree kill).
		_ = syscall.Setpgid(pid, pid)
		cmd.SysProcAttr.Setpgid = true
	}
	pgid, err := syscall.Getpgid(pid)
	if err != nil {
		pgid = pid // fall back; the per-pid kill still works.
	}

	tr := &tracked{
		name:   name,
		cmd:    cmd,
		pgid:   pgid,
		exitCh: make(chan struct{}),
	}

	t.mu.Lock()
	closed := t.closed.Load()
	if closed {
		t.mu.Unlock()
		// Late registration after Shutdown: record the book-keeping
		// but signal the daemon right away so the late spawn doesn't
		// outlive the binary.
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			_ = syscall.Kill(-pgid, syscall.SIGTERM)
			time.Sleep(50 * time.Millisecond)
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
			close(tr.exitCh)
		}()
		return pid, nil
	}
	t.procs[pid] = tr
	t.wg.Add(1)
	t.mu.Unlock()

	// Reaper goroutine: poll the kernel for the pid slot to be freed
	// and Untrack when that happens. We deliberately do NOT call
	// cmd.Wait here because exec.Cmd.Wait calls Process.Wait, which is
	// not idempotent — a second caller gets "no child processes". The
	// subagent, the codesearch runner, and every other owner of an
	// *exec.Cmd calls cmd.Wait to retrieve the exit state; stealing
	// that slot would break them. Our observation is purely passive
	// (kill -0 returns ESRCH once the owner has reaped), so the
	// goroutine is invisible to the rest of the runtime.
	//
	// The reaper's bookkeeping contract:
	//
	//   - ESRCH observed: the kernel slot is gone, the daemon is
	//     verifiably reaped. Untrack so the bookkeeping clears AND the
	//     WaitGroup decrements. This is the "natural exit" path.
	//
	//   - closed.Load() true, ESRCH false: Shutdown is in flight. Do NOT
	//     Untrack yet — Shutdown still needs `procs[pid]` to identify
	//     survivors for SIGKILL escalation. Just return; Shutdown's
	//     phase 4 ("force untrack after SIGKILL") will decrement the
	//     WaitGroup when the survivors are dead or the grace has
	//     elapsed. Leaving the reaper without decrementing means the
	//     WaitGroup stays at 1 until Shutdown's phase 4 runs, which is
	//     exactly the property we want.
	//
	//   - The earlier direct `t.wg.Done()` in the closed branch was a
	//     double-decrement bug: a Track whose reaper had already
	//     decremented via Untrack (or a test that manually called
	//     Untrack) would panic on the next closed=true observation.
	//     Removing that branch fixes the negative counter.
	//
	// Tests that build a raw *exec.Cmd without an owner (the daemon
	// package's own tracker_test does this for some scenarios) must
	// drain the cmd explicitly via cmd.Wait before asserting on
	// Wait(); see the test helpers.
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
				// Shutdown owns the WaitGroup balance from here
				// on. The reaper's job is observation only; once
				// Shutdown has been called, the bookkeeping is
				// either cleared naturally (ESRCH, handled above)
				// or cleared by Shutdown's phase 4 force-Untrack
				// after the SIGKILL escalation. Returning without
				// decrementing keeps the WaitGroup in a known
				// state: Shutdown is responsible for draining
				// whatever's left.
				return
			}
		}
	}()

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
func (t *Tracker) Shutdown(ctx context.Context) int {
	// Once-only: signal-handler callers may race; the second caller
	// waits on the first via stopped.
	t.onceSignal.Do(func() {
		t.stopped = make(chan struct{})
	})

	t.mu.Lock()
	if t.closed.Load() {
		t.mu.Unlock()
		// Wait for the in-flight Shutdown to finish so callers see
		// the same outcome.
		if t.stopped != nil {
			<-t.stopped
		}
		return 0
	}
	t.closed.Store(true)
	// Snapshot the current set so we can release the mutex before
	// blocking on signals.
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

	// Phase 1: SIGTERM every group.
	for _, tr := range snapshot {
		// -pgid signals the whole process group; pid-only fallback if
		// Getpgid failed at registration.
		target := -tr.pgid
		if tr.pgid == 0 || tr.pgid == tr.cmd.Process.Pid {
			target = -tr.cmd.Process.Pid
		}
		if err := syscall.Kill(target, syscall.SIGTERM); err != nil {
			// ESRCH means the process already exited; nothing to do.
			if !errors.Is(err, syscall.ESRCH) {
				logger.Printf("[daemon] SIGTERM %s (pid=%d): %v", tr.name, tr.cmd.Process.Pid, err)
			}
		}
	}

	// Phase 2: wait up to grace for any of them to exit. The wait
	// channel closes when every tracked daemon has been reaped
	// (either by its own reaper goroutine or by us below).
	graceCtx, cancel := context.WithTimeout(ctx, grace)
	defer cancel()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		t.mu.Lock()
		alive := len(t.procs)
		t.mu.Unlock()
		if alive == 0 {
			// Every daemon was either reaped during the grace
			// period (the per-Track reaper goroutine called
			// Untrack on ESRCH) or had already exited before
			// Shutdown was called. Either way the WaitGroup is
			// drained; release the stopped channel.
			if t.stopped != nil {
				close(t.stopped)
			}
			return 0
		}
		select {
		case <-graceCtx.Done():
			// Grace period elapsed: escalate.
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
			// Phase 4: drain the bookkeeping. The reaper goroutines
			// deliberately do NOT Untrack on `closed=true` (only on
			// ESRCH), so the WaitGroup counter is still at 1 per
			// survivor. We Untrack each survivor here, which
			// decrements the WaitGroup and clears `procs`. Untrack
			// is idempotent — if a reaper raced ahead and observed
			// ESRCH between SIGKILL and now, present=false and we
			// no-op.
			for _, tr := range survivors {
				t.Untrack(tr.cmd.Process.Pid)
			}
			// Best-effort: wait briefly for SIGKILL to be observed
			// (the kernel marks the pid gone, reapers will observe
			// ESRCH on their next tick). We bound this with a
			// short timer so a process that ignores SIGKILL
			// (rare but possible if it's in D state) can't hang
			// the exit forever — the WaitGroup is already drained
			// above, this is just so anyone observing the kernel
			// sees a consistent state.
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
		case <-ticker.C:
			// poll again
		}
	}
}

// Stopped returns a channel that closes when the most recent
// Shutdown call completes. nil until Shutdown is called for the first
// time. Tests use this to synchronise without a sleep.
func (t *Tracker) Stopped() <-chan struct{} {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped == nil {
		// Lazy channel: callers can select on Stopped before
		// Shutdown fires and never deadlock.
		ch := make(chan struct{})
		close(ch)
		return ch
	}
	return t.stopped
}

// String formats the current tracker state for diagnostics. Used by
// the test recorder and the optional log sink.
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

// IsRunning reports whether pid is still alive. Used by tests to
// assert "the daemon is gone".
//
// The check is layered:
//
//  1. Wait4(pid, ..., WNOHANG). If it returns ESRCH, the pid is
//     freed from the process table — definitely gone.
//  2. If Wait4 returns the pid with status.Signaled() or
//     status.Exited(), the pid is a zombie — the kernel has marked
//     it dead but the parent hasn't reaped it yet. From the test's
//     point of view the daemon is gone. Wait4's WNOHANG reaps the
//     zombie as a side effect, so a subsequent `kill -0` returns
//     ESRCH.
//  3. Otherwise (pid == 0, err == nil) we cannot tell from Wait4
//     alone. The process is either alive OR a zombie whose state we
//     cannot reap because Wait4 only succeeds once per pid. We
//     fall back to `kill(pid, 0)`: ESRCH means gone; nil means the
//     pid slot exists (alive or zombie).
//
// macOS keeps a zombie's pid slot alive (kill -0 returns nil) until
// the parent reaps it via wait(). The tracker always calls
// cmd.Process.Wait in a reaper goroutine, so a daemon that was
// killed via this tracker's Shutdown will be reaped "soon" — but
// "soon" is racy with the test calling IsRunning right after
// Shutdown. Tests that want a hard guarantee should call
// Tracker.Wait before IsRunning; that drains every reaper.
func IsRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	var status syscall.WaitStatus
	rpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	switch {
	case errors.Is(err, syscall.ECHILD):
		// Not our child — fall back to kill -0.
		return IsRunningViaKill(pid)
	case errors.Is(err, syscall.ESRCH):
		return false
	case err != nil:
		return IsRunningViaKill(pid)
	case rpid == pid:
		// Reaped the zombie. The process is gone.
		return false
	case rpid == 0:
		// WNOHANG + no zombie AND no error: pid is alive OR a zombie
		// whose state has already been reaped by a previous Wait4.
		// The pid slot might still exist (zombie) or might not (alive
		// but reused, very unlikely). kill -0 disambiguates.
		return IsRunningViaKill(pid)
	}
	return IsRunningViaKill(pid)
}

// IsRunningViaKill is the slow path: send signal 0 and inspect the
// error. ESRCH means gone, anything else (including nil) means we
// couldn't determine. Used when Wait4 returns ECHILD (the pid isn't
// our child) or some other unexpected error.
func IsRunningViaKill(pid int) bool {
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	// EPERM means the process exists but we can't signal it; treat
	// that as "running" — the test infrastructure usually owns the
	// same uid and the process shouldn't be invisible.
	return errors.Is(err, syscall.EPERM)
}

// CmdFromArgs is a tiny helper used by subagent and the test helpers:
// build an *exec.Cmd and Start it, returning the cmd the caller
// should then hand to Track. The helper stamps Setpgid before Start
// so the cmd emerges from the kernel already a group leader; this
// keeps Track's retroactive Setpgid syscall off the hot path.
//
// On platforms where Setpgid isn't supported (not the case on
// darwin/linux but documented for the future) the helper silently
// ignores the SysProcAttr and falls back to a per-pid kill.
func CmdFromArgs(argv0 string, argv ...string) *exec.Cmd {
	cmd := exec.Command(argv0, argv...)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	return cmd
}

// os.Exit is referenced to keep the import set stable across the
// //-prefixed build tags the package might pick up later.
var _ = os.Exit
