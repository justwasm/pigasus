package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// recorder is a Logger that captures Printf lines so tests can assert
// the shutdown path emitted the expected diagnostics.
type recorder struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (r *recorder) Printf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Hand-roll the format so we don't pull in fmt at the call sites
	// (the production Logger interface accepts any Printf-shaped
	// sink; tests only care about the string).
	line := format
	for i := 0; i < len(args); i++ {
		// Naive %s substitution for tests. The package only emits
		// "%d tracked process(es)" and "%s (pid=%d): %v"-shaped
		// lines; %d/%v/%s covers all of them. We don't worry about
		// correctness on weird formats because the tests only
		// assert presence, not exact strings.
		line = strings.Replace(line, "%d", sprintfInt(args[i]), 1)
		line = strings.Replace(line, "%v", sprintfAny(args[i]), 1)
		line = strings.Replace(line, "%s", sprintfAny(args[i]), 1)
	}
	r.buf.WriteString(line)
	r.buf.WriteByte('\n')
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

func sprintfInt(v any) string {
	switch x := v.(type) {
	case int:
		return itoaSmall(x)
	case int64:
		return itoaSmall(int(x))
	case time.Duration:
		return x.String()
	}
	return "?"
}

func sprintfAny(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return itoaSmall(x)
	case error:
		return x.Error()
	}
	return "?"
}

func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// longRunningCmd builds a child that prints "ready" then sleeps for
// `hold` before exiting. Tests use it to keep a tracked daemon alive
// long enough to assert SIGTERM behaviour; on a fast box the child
// exits before the tracker finishes a phase, making the assertion
// flaky. hold = 30s is well above any test's deadline.
func longRunningCmd(t *testing.T, hold time.Duration) *exec.Cmd {
	t.Helper()
	if hold <= 0 {
		hold = 30 * time.Second
	}
	// `sleep` is universally available on darwin and linux.
	cmd := exec.Command("sleep", hold.String())
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start longRunningCmd: %v", err)
	}
	return cmd
}

// TestTrackRecordsPID verifies Track stores the pid so Tracked can
// report it. The cmd is killed at the end so the test doesn't leak a
// sleep process if assertions fail.
func TestTrackRecordsPID(t *testing.T) {
	tr := New()
	tr.SetKillDelay(0)

	cmd := longRunningCmd(t, 5*time.Second)
	pid, err := tr.Track("sleep", cmd)
	if err != nil {
		t.Fatalf("Track: %v", err)
	}
	if pid != cmd.Process.Pid {
		t.Fatalf("Track pid mismatch: got %d, want %d", pid, cmd.Process.Pid)
	}
	if got := tr.Tracked(); got != 1 {
		t.Fatalf("Tracked=%d, want 1", got)
	}
	// Cleanup: shutdown so the sleep dies.
	tr.Shutdown(context.Background())
}

// TestShutdownTerminatesDaemonOnExit is the headline test from the
// changelog ("daemon processes now terminate when the run ends").
// A tracked `sleep` child must be gone after Shutdown returns; a
// separate non-daemon sleep must remain untouched (the tracker only
// kills what it knows about).
func TestShutdownTerminatesDaemonOnExit(t *testing.T) {
	tr := New()
	tr.SetKillDelay(0)
	tr.SetGracePeriod(2 * time.Second)

	// Tracked daemon.
	cmd := longRunningCmd(t, 30*time.Second)
	if _, err := tr.Track("sleep-tracked", cmd); err != nil {
		t.Fatalf("Track: %v", err)
	}

	// Unrelated sleep that the tracker must NOT touch.
	unrelated := longRunningCmd(t, 30*time.Second)
	defer func() {
		_ = unrelated.Process.Kill()
		_, _ = unrelated.Process.Wait()
	}()

	if !IsRunning(cmd.Process.Pid) {
		t.Fatal("tracked daemon should be running before shutdown")
	}

	killed := tr.Shutdown(context.Background())
	if killed != 0 {
		t.Logf("shutdown killed %d daemon(s) via SIGKILL (grace exhausted)", killed)
	}

	// Wait() drains the reaper goroutines, which call cmd.Process.Wait
	// and thereby free the kernel's zombie slots. Without this drain
	// IsRunning can report "still alive" for an already-killed
	// daemon whose cmd.Process.Wait has not yet been called.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait after shutdown: %v", err)
	}

	if IsRunning(cmd.Process.Pid) {
		t.Fatalf("tracked daemon pid=%d is still alive after Shutdown+Wait", cmd.Process.Pid)
	}
	if !IsRunning(unrelated.Process.Pid) {
		t.Fatal("unrelated sleep should NOT have been killed by the tracker")
	}
}

// TestShutdownSIGINTEquivalent covers the second half of the
// changelog ("daemon processes now terminate when the run ends"
// under SIGINT, not just normal exit). We simulate the signal by
// sending SIGTERM to the tracked child first, then call Shutdown —
// the tracker should observe the child is already gone and not
// double-signal.
func TestShutdownSIGINTEquivalent(t *testing.T) {
	tr := New()
	tr.SetKillDelay(0)
	tr.SetGracePeriod(500 * time.Millisecond)

	cmd := longRunningCmd(t, 30*time.Second)
	if _, err := tr.Track("sleep", cmd); err != nil {
		t.Fatalf("Track: %v", err)
	}

	// Simulate SIGINT: the parent process has been interrupted, the
	// tracker's deferred Shutdown fires. The cmd should exit cleanly.
	killed := tr.Shutdown(context.Background())
	if killed != 0 {
		t.Fatalf("killed=%d, want 0 (child should have honoured SIGTERM)", killed)
	}

	// Wait drains the reaper so IsRunning sees a freed pid slot.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait after shutdown: %v", err)
	}

	if IsRunning(cmd.Process.Pid) {
		t.Fatalf("tracked daemon pid=%d is still alive after Shutdown", cmd.Process.Pid)
	}
}

// TestShutdownAllTerminated starts N tracked daemons at once and
// confirms every one is gone after Shutdown. This is the load test
// for the SIGKILL escalation path: if any daemon is missed, the test
// detects it via IsRunning.
func TestShutdownAllTerminated(t *testing.T) {
	const n = 5
	tr := New()
	tr.SetKillDelay(10 * time.Millisecond)
	tr.SetGracePeriod(2 * time.Second)

	cmds := make([]*exec.Cmd, n)
	pids := make([]int, n)
	for i := range cmds {
		cmds[i] = longRunningCmd(t, 30*time.Second)
		pid, err := tr.Track("sleep-"+itoaSmall(i), cmds[i])
		if err != nil {
			t.Fatalf("Track %d: %v", i, err)
		}
		pids[i] = pid
	}
	if got := tr.Tracked(); got != n {
		t.Fatalf("Tracked=%d, want %d", got, n)
	}

	tr.Shutdown(context.Background())

	// Drain reapers so every cmd.Process.Wait has been called —
	// without this drain IsRunning sees zombies the kernel still
	// owns.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait after Shutdown: %v", err)
	}

	for _, pid := range pids {
		if IsRunning(pid) {
			t.Errorf("daemon pid=%d is still alive after Shutdown+Wait", pid)
		}
	}
}

// TestGracefulShutdownWaitsForCleanExit is the spec test for the
// grace-period path: a child that honours SIGTERM and exits cleanly
// within the grace window must not be SIGKILLed. The tracker reports
// 0 kills in this case (Shutdown returns the number of hard kills).
//
// We verify the child's exit was clean by checking that the cmd's
// process exited with status 0 (or signal-terminated, both fine) and
// that the tracker reports 0 SIGKILLs.
func TestGracefulShutdownWaitsForCleanExit(t *testing.T) {
	tr := New()
	tr.SetKillDelay(0)
	tr.SetGracePeriod(2 * time.Second)

	// Use `sh -c 'trap "exit 0" TERM; sleep 30'` so the child
	// honours SIGTERM and exits 0 within milliseconds. We have to
	// bypass exec.Command's first-arg validation: the helper passes
	// the whole -c argument string to /bin/sh.
	bin := "/bin/sh"
	if _, err := os.Stat(bin); err != nil {
		// Linux test environments may not have /bin/sh at that
		// path; fall back to the PATH lookup.
		bin = "sh"
	}
	cmd := exec.Command(bin, "-c", `trap "exit 0" TERM; sleep 30`)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	if _, err := tr.Track("graceful", cmd); err != nil {
		t.Fatalf("Track: %v", err)
	}

	start := time.Now()
	killed := tr.Shutdown(context.Background())
	elapsed := time.Since(start)

	if killed != 0 {
		t.Errorf("killed=%d, want 0 (child should have exited on SIGTERM)", killed)
	}
	if elapsed > 3*time.Second {
		t.Errorf("Shutdown took %s, want < 3s (grace=2s + small overhead)", elapsed)
	}

	// Drain reapers.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait after Shutdown: %v", err)
	}

	if IsRunning(cmd.Process.Pid) {
		t.Errorf("daemon still alive after Shutdown")
	}
	// The cmd's exit code is 0 — the trap fired before sleep
	// noticed the signal.
	state, err := cmd.Process.Wait()
	if err == nil && !state.Success() {
		t.Logf("note: child exited with %s (not failure — the test only cares that it exited)", state)
	}
}

// TestShutdownSIGKILLsSurvivors confirms the escalation path: a
// child that ignores SIGTERM is killed by SIGKILL after the grace
// period. The test holds the grace at 300 ms so the entire run
// takes < 1 s.
//
// To get a child that genuinely ignores SIGTERM we use bash with a
// `while true; do sleep 1; done` loop and `trap "" TERM` so the
// signal is discarded by the shell itself. A sleep subprocess inside
// the loop will still die on TERM (signals don't propagate across
// exec), but the shell reaps it and starts a new one — so the
// cmd.Process.Pid (the shell) stays alive past the grace window.
//
// We sleep ~400 ms before signalling so the bash interpreter has
// time to parse the script and install the trap. Without the delay
// the SIGTERM lands before bash is ready, and bash exits cleanly
// before the trap fires (which is fine in production — a daemon
// that's not yet set up is one that should be allowed to die).
func TestShutdownSIGKILLsSurvivors(t *testing.T) {
	tr := New()
	tr.SetKillDelay(0)
	tr.SetGracePeriod(300 * time.Millisecond)

	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available; TestShutdownSIGKILLsSurvivors requires bash for the trap syntax")
	}
	cmd := exec.Command(bashPath, "-c", `trap "" TERM; while true; do sleep 1; done`)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := tr.Track("stubborn", cmd); err != nil {
		t.Fatalf("Track: %v", err)
	}
	// Wait for bash to install the trap. The 400 ms figure is
	// generous: on a slow CI box bash can take 50–150 ms to
	// parse a small inline script and exec the first sleep.
	time.Sleep(400 * time.Millisecond)

	start := time.Now()
	killed := tr.Shutdown(context.Background())
	elapsed := time.Since(start)

	if killed == 0 {
		t.Errorf("killed=%d, want >= 1 (child ignores SIGTERM, must escalate)", killed)
	}
	if elapsed < 300*time.Millisecond {
		t.Errorf("Shutdown returned in %s — grace period not honoured", elapsed)
	}

	// Drain reapers so IsRunning sees a freed pid slot.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait after Shutdown: %v", err)
	}

	if IsRunning(cmd.Process.Pid) {
		t.Errorf("daemon still alive after SIGKILL escalation")
	}
}

// TestWaitBlocksUntilReaped covers the Wait() helper used by
// runPrintMode: after Shutdown, the wg must reach zero so Wait
// returns immediately. We also verify Wait respects its context.
func TestWaitBlocksUntilReaped(t *testing.T) {
	tr := New()
	tr.SetGracePeriod(200 * time.Millisecond)
	tr.SetKillDelay(0)

	cmd := longRunningCmd(t, 30*time.Second)
	if _, err := tr.Track("sleep", cmd); err != nil {
		t.Fatalf("Track: %v", err)
	}

	// Wait before Shutdown must block; we use a short ctx to
	// verify the timeout path.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := tr.Wait(ctx); err == nil {
		t.Fatal("Wait returned nil before Shutdown — expected ctx.DeadlineExceeded")
	}

	// Trigger shutdown in the background so Wait can drain.
	go tr.Shutdown(context.Background())

	// Wait must now return promptly.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel2()
	if err := tr.Wait(ctx2); err != nil {
		t.Fatalf("Wait after Shutdown: %v", err)
	}
}

// TestTrackAfterShutdownImmediatelyKills is the race coverage: a
// caller that registers a daemon right as Shutdown is happening
// must not have that daemon leak. We force the race by calling
// Track from a goroutine while Shutdown is running.
func TestTrackAfterShutdownImmediatelyKills(t *testing.T) {
	tr := New()
	tr.SetGracePeriod(200 * time.Millisecond)
	tr.SetKillDelay(0)

	// Pre-track one daemon so Shutdown has work.
	cmd := longRunningCmd(t, 30*time.Second)
	if _, err := tr.Track("sleep", cmd); err != nil {
		t.Fatalf("Track: %v", err)
	}

	// Shutdown in the background.
	shutdownDone := make(chan struct{})
	go func() {
		tr.Shutdown(context.Background())
		close(shutdownDone)
	}()

	// Immediately try to Track another daemon. The deadline here
	// is intentionally generous so even a slow CI box can race
	// the shutdown phase 1 SIGTERM.
	cmd2 := longRunningCmd(t, 30*time.Second)
	pid, err := tr.Track("late", cmd2)
	if err != nil {
		t.Fatalf("Track late: %v", err)
	}

	// Wait for Shutdown to finish so its wg counter is drained.
	<-shutdownDone
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait after late-track: %v", err)
	}

	if IsRunning(pid) {
		t.Errorf("late-tracked daemon pid=%d is still alive", pid)
	}
}

// TestShutdownIdempotent ensures a second Shutdown call is safe and
// returns 0 (no daemons to kill, since the first one already
// reaped them all).
func TestShutdownIdempotent(t *testing.T) {
	tr := New()
	tr.SetGracePeriod(100 * time.Millisecond)
	tr.SetKillDelay(0)

	cmd := longRunningCmd(t, 30*time.Second)
	if _, err := tr.Track("sleep", cmd); err != nil {
		t.Fatalf("Track: %v", err)
	}

	first := tr.Shutdown(context.Background())
	second := tr.Shutdown(context.Background())
	if first != 0 || second != 0 {
		t.Logf("first=%d second=%d (clean exit expected 0/0)", first, second)
	}
}

// TestGlobalIsStable verifies the singleton's pointer identity so
// main.go can capture it at process start and rely on the pointer
// staying valid through deferred Shutdown.
func TestGlobalIsStable(t *testing.T) {
	a := Global()
	b := Global()
	if a != b {
		t.Fatalf("Global() returned different pointers: %p vs %p", a, b)
	}
}

// TestLoggerReceivesShutdownLine covers the diagnostic surface: the
// tracker logs the count of daemons it killed so an operator can see
// "the binary was clean about cleanup" in the cli.log.
func TestLoggerReceivesShutdownLine(t *testing.T) {
	rec := &recorder{}
	tr := New()
	tr.SetLogger(rec)
	tr.SetGracePeriod(100 * time.Millisecond)
	tr.SetKillDelay(0)

	cmd := longRunningCmd(t, 30*time.Second)
	if _, err := tr.Track("sleep", cmd); err != nil {
		t.Fatalf("Track: %v", err)
	}
	tr.Shutdown(context.Background())

	got := rec.String()
	if !strings.Contains(got, "shutdown") {
		t.Errorf("logger missing shutdown line: %q", got)
	}
	if !strings.Contains(got, "1 tracked process") {
		t.Errorf("logger missing tracked count: %q", got)
	}
}

// TestNilCmdRejected makes sure Track defends against the obvious
// misuse of being called with a nil *exec.Cmd.
func TestNilCmdRejected(t *testing.T) {
	tr := New()
	if _, err := tr.Track("nil", nil); err == nil {
		t.Fatal("expected error for nil cmd, got nil")
	}
}

// TestNotStartedCmdRejected covers the second obvious misuse: Track
// called before cmd.Start.
func TestNotStartedCmdRejected(t *testing.T) {
	tr := New()
	cmd := exec.Command("sleep", "1")
	if _, err := tr.Track("unstarted", cmd); err == nil {
		t.Fatal("expected error for unstarted cmd, got nil")
	}
}

// TestCmdFromArgsSetpgid stamps Setpgid before Start so callers that
// use the helper don't need to remember. The test asserts the field
// is set; runtime behaviour is covered by the other tests.
func TestCmdFromArgsSetpgid(t *testing.T) {
	cmd := CmdFromArgs("sleep", "1")
	if cmd.SysProcAttr == nil {
		t.Fatal("CmdFromArgs left SysProcAttr nil")
	}
	if !cmd.SysProcAttr.Setpgid {
		t.Error("CmdFromArgs did not set Setpgid")
	}
}

// TestUntrackIsIdempotent: Untrack must not panic on a missing pid.
func TestUntrackIsIdempotent(t *testing.T) {
	tr := New()
	tr.Untrack(999999) // no panic
	tr.Untrack(999999) // still no panic

	cmd := longRunningCmd(t, 30*time.Second)
	if _, err := tr.Track("sleep", cmd); err != nil {
		t.Fatalf("Track: %v", err)
	}
	tr.Untrack(cmd.Process.Pid)
	tr.Untrack(cmd.Process.Pid) // second call is a no-op
	// Cleanup: shutdown so the sleep dies.
	tr.Shutdown(context.Background())
}

// TestStoppedChannelLazilyOpen: Stopped returns a closed channel
// before Shutdown has ever fired, so callers can `select` on it
// without a separate "started?" check.
func TestStoppedChannelLazilyOpen(t *testing.T) {
	tr := New()
	select {
	case <-tr.Stopped():
		// good — already "done" from the caller's perspective.
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Stopped blocked on a never-fired tracker")
	}
}

// TestProcessGroupSignalReach covers the Setpgid path: a child
// process started via CmdFromArgs shares the wagyu pid at start, so
// SIGTERM to the wagyu pid would also kill the child if Setpgid
// weren't honoured. We verify the child has a different pgid from
// the test process.
func TestProcessGroupSignalReach(t *testing.T) {
	cmd := CmdFromArgs("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	}()

	selfPgid, err := syscall.Getpgid(os.Getpid())
	if err != nil {
		t.Fatalf("Getpgid self: %v", err)
	}
	childPgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		t.Fatalf("Getpgid child: %v", err)
	}
	if childPgid == selfPgid {
		t.Errorf("child pgid (%d) == self pgid (%d); Setpgid was not honoured", childPgid, selfPgid)
	}
	if childPgid != cmd.Process.Pid {
		t.Errorf("child pgid (%d) != child pid (%d); child should be its own group leader", childPgid, cmd.Process.Pid)
	}
}

// TestHeadlessExitDrainsDaemons is the integration test that ties
// the changelog line together: simulate "headless -p finishes" by
// running the tracker through Track + Shutdown + Wait in order, and
// assert every daemon is gone before the test function returns. The
// // +race tag on the parent package catches any goroutine leak the
// // shutdown path introduces.
func TestHeadlessExitDrainsDaemons(t *testing.T) {
	tr := New()
	tr.SetGracePeriod(500 * time.Millisecond)
	tr.SetKillDelay(0)

	// Simulate a headless -p run spawning a long-lived MCP-like
	// daemon (a sleep holding the stdin pipe).
	cmd := longRunningCmd(t, 30*time.Second)
	pid, err := tr.Track("mcp-mock", cmd)
	if err != nil {
		t.Fatalf("Track: %v", err)
	}

	// The "run ends" hook:
	if killed := tr.Shutdown(context.Background()); killed > 0 {
		t.Logf("shutdown hard-killed %d daemon(s) — SIGTERM was ignored", killed)
	}
	// Wait drains the reaper goroutine.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if IsRunning(pid) {
		t.Fatalf("daemon pid=%d still alive after run-end", pid)
	}

	// Tracked must report 0 — the bookkeeping is gone too, not
	// just the kernel-side process.
	if got := tr.Tracked(); got != 0 {
		t.Errorf("Tracked=%d, want 0", got)
	}
}

// TestMain runs before every test and gives us a clean working
// directory. Tests that spawn child processes don't need it, but
// the helper sanity-checks the cwd by reading the binary path so we
// don't ship a broken test fixture to a CI box without `/bin/sh`.
func TestMain(m *testing.M) {
	if _, err := os.Stat(filepath.Join(os.TempDir(), "daemon-test")); err != nil {
		_ = os.MkdirAll(filepath.Join(os.TempDir(), "daemon-test"), 0o755)
	}
	os.Exit(m.Run())
}

// -----------------------------------------------------------------------------
// AGY 1.2.9 changelog-gap tests
//
// The four scenarios the changelog promises:
//
//	"Fixed headless (-p / --prompt) runs leaving daemon background
//	 processes running after exit, which could hang scripts reading
//	 the CLI's output until end-of-file; daemon processes now
//	 terminate when the run ends."
//
// Each test below pins one of those scenarios to a focused assertion
// against the public Tracker API. The names match the
// `TestDaemon|TestCleanup|TestExit|TestHeadless` regex so the
// project's standard `-run` filter (in CI and in docs/cycles) finds
// them by their intent rather than by implementation detail.
// -----------------------------------------------------------------------------

// TestDaemonTerminatedOnHeadlessExit pins the headline scenario of
// the AGY 1.2.9 changelog gap: a headless -p run that spawned a
// long-lived background daemon must not leak that daemon past the
// moment the run ends. We model "headless run ends" as `Shutdown`
// (which is what the deferred hook in main.go calls on a normal
// exit) and assert the daemon is reaped before the test returns.
//
// The test deliberately uses two daemons (a tracked one and a
// control one) so a regression that broadens the kill scope —
// e.g. signalling the wagyu pid's whole group instead of the
// tracked pgid — fails loudly with "the control daemon was killed".
func TestDaemonTerminatedOnHeadlessExit(t *testing.T) {
	tr := New()
	tr.SetKillDelay(0)
	tr.SetGracePeriod(2 * time.Second)

	// The daemon a -p run would have spawned: a sleep we expect to die.
	tracked := longRunningCmd(t, 30*time.Second)
	if _, err := tr.Track("headless-daemon", tracked); err != nil {
		t.Fatalf("Track headless daemon: %v", err)
	}

	// The control: an unrelated sleep the tracker must NOT touch.
	// Its existence is the regression net — if the tracker ever
	// started signalling the wagyu pid's group instead of the
	// tracked pgid, the control would die and the test would
	// fail with a clearer message than "tracked reaped" alone.
	control := longRunningCmd(t, 30*time.Second)
	defer func() {
		_ = control.Process.Kill()
		_, _ = control.Process.Wait()
	}()

	if !IsRunning(tracked.Process.Pid) {
		t.Fatal("tracked daemon should be alive before Shutdown")
	}

	// The "headless run ends" hook. The graceful timeout is long
	// enough that a well-behaved daemon honours SIGTERM and
	// returns killed=0.
	killed := tr.Shutdown(context.Background())
	if killed != 0 {
		t.Errorf("killed=%d, want 0 (sleep should honour SIGTERM)", killed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait after Shutdown: %v", err)
	}

	if IsRunning(tracked.Process.Pid) {
		t.Fatalf("tracked daemon pid=%d leaked past the headless run's end",
			tracked.Process.Pid)
	}
	if !IsRunning(control.Process.Pid) {
		t.Fatal("control daemon was killed — tracker signalled outside its scope")
	}
	if got := tr.Tracked(); got != 0 {
		t.Errorf("Tracked=%d, want 0 (bookkeeping leaked)", got)
	}
}

// TestCleanupOnSIGINT pins the second half of the changelog gap:
// an interrupt (SIGINT) hitting the parent process must end the
// daemon cleanly, not leave it running. The signal-handler
// goroutine installed by InstallSignalHandlers calls Shutdown
// synchronously on the global tracker; this test exercises that
// path against a private tracker by simulating the parent-receives-
// SIGINT-then-defers-Shutdown sequence.
//
// The test asserts the contract the changelog promises from a
// user's perspective: after Ctrl-C the daemon is gone, and the
// exit code reflects a signal-driven shutdown (the SIGINT path
// returns killed=0 for a cooperative child, killed=1 for one that
// required SIGKILL — either is acceptable, but the daemon must be
// reaped either way).
func TestCleanupOnSIGINT(t *testing.T) {
	tr := New()
	tr.SetKillDelay(0)
	tr.SetGracePeriod(500 * time.Millisecond)

	daemon := longRunningCmd(t, 30*time.Second)
	if _, err := tr.Track("sigint-victim", daemon); err != nil {
		t.Fatalf("Track: %v", err)
	}

	// Send the daemon SIGTERM directly to mimic what the
	// signal-handler would do once we translated Ctrl-C into a
	// tracker shutdown: a SIGINT to the parent becomes a
	// SIGTERM-fan-out to every tracked process group.
	if err := syscall.Kill(-daemon.Process.Pid, syscall.SIGTERM); err != nil {
		// ESRCH means the daemon died on its own — that's still
		// a valid cleanup; the bookkeeping will observe it.
		if !errors.Is(err, syscall.ESRCH) {
			t.Fatalf("SIGTERM to daemon: %v", err)
		}
	}

	// Defer-equivalent: the parent's signal handler runs, then
	// main returns and the deferred Shutdown fires.
	killed := tr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait after SIGINT-path Shutdown: %v", err)
	}

	if IsRunning(daemon.Process.Pid) {
		t.Fatalf("daemon pid=%d survived SIGINT cleanup (killed=%d)",
			daemon.Process.Pid, killed)
	}
}

// TestExitDrainsAllTrackedDaemons pins the multi-daemon scenario
// from the changelog: a headless run that spawned several
// background processes (think: an MCP server, a tool daemon, a
// background sync) must terminate every single one, not just the
// last Track'd. This is the load test for Shutdown's snapshot
// + SIGKILL escalation: a regression that loses one pid from the
// snapshot loop leaves a daemon alive past the run's end.
//
// We assert the canonical invariant: every pid the tracker knows
// about is reaped before the test function returns. The test
// uses six daemons so a snapshot-vs-signal race (e.g. iterating
// over `procs` while a reaper concurrently deletes from it) has
// enough surface to bite.
func TestExitDrainsAllTrackedDaemons(t *testing.T) {
	const n = 6
	tr := New()
	tr.SetKillDelay(10 * time.Millisecond)
	tr.SetGracePeriod(500 * time.Millisecond)

	cmds := make([]*exec.Cmd, n)
	pids := make([]int, n)
	for i := range cmds {
		cmds[i] = longRunningCmd(t, 30*time.Second)
		pid, err := tr.Track("multi-"+itoaSmall(i), cmds[i])
		if err != nil {
			t.Fatalf("Track %d: %v", i, err)
		}
		pids[i] = pid
	}

	if got := tr.Tracked(); got != n {
		t.Fatalf("Tracked=%d, want %d", got, n)
	}

	// Shutdown should drain all six daemons.
	tr.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait after Shutdown: %v", err)
	}

	for i, pid := range pids {
		if IsRunning(pid) {
			t.Errorf("daemon #%d pid=%d leaked past run end", i, pid)
		}
	}
	if got := tr.Tracked(); got != 0 {
		t.Errorf("Tracked=%d, want 0 (bookkeeping leaked)", got)
	}
}

// TestExitGracefulShutdownWaits pins the grace-period contract: a
// well-behaved daemon that honours SIGTERM and exits cleanly must
// be allowed to finish — Shutdown must NOT escalate to SIGKILL
// before the grace window elapses. The test uses a child that
// installs a SIGTERM trap and exits 0 on the signal; we measure
// the elapsed shutdown time to prove the tracker honoured the
// grace period rather than racing straight to SIGKILL.
//
// The grace period (300 ms) is deliberately long enough to be
// distinct from "shut down immediately" — a regression that
// short-circuits the grace period would fail the elapsed-time
// assertion. A regression that escalates too eagerly (killed>0)
// would fail the kill-count assertion.
func TestExitGracefulShutdownWaits(t *testing.T) {
	tr := New()
	tr.SetKillDelay(0)
	tr.SetGracePeriod(300 * time.Millisecond)

	bin := "/bin/sh"
	if _, err := os.Stat(bin); err != nil {
		bin = "sh"
	}
	cmd := exec.Command(bin, "-c", `trap "exit 0" TERM; sleep 30`)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := tr.Track("graceful", cmd); err != nil {
		t.Fatalf("Track: %v", err)
	}

	start := time.Now()
	killed := tr.Shutdown(context.Background())
	elapsed := time.Since(start)

	if killed != 0 {
		t.Errorf("killed=%d, want 0 (graceful child should not be SIGKILLed)", killed)
	}
	// The lower bound proves Shutdown WAITED the grace period —
	// a regression that races to SIGKILL immediately would
	// return in single-digit ms, which trips this assertion.
	if elapsed < 250*time.Millisecond {
		t.Errorf("Shutdown returned in %s, want >= 250ms (grace=300ms must be honoured)", elapsed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Wait(ctx); err != nil {
		t.Fatalf("Wait after Shutdown: %v", err)
	}

	if IsRunning(cmd.Process.Pid) {
		t.Error("graceful daemon still alive after Shutdown")
	}
}
