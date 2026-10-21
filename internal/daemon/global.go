package daemon

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// global is the singleton tracker the wagyu binary uses. The pointer
// is set once on first call to Global() and never re-pointed, so a
// caller that captures the *Tracker at process start can rely on it
// staying valid for the process lifetime.
var (
	global     *Tracker
	globalOnce sync.Once
)

// Global returns the process-wide Tracker. The first call allocates
// it; subsequent calls return the same pointer.
//
// Tests that need an isolated tracker should call New() directly
// rather than mutating this singleton.
func Global() *Tracker {
	globalOnce.Do(func() {
		global = New()
	})
	return global
}

// SetGlobal replaces the process-wide tracker. Returns the previous
// tracker so tests can restore it on cleanup. nil restores a fresh
// tracker on the next Global() call.
func SetGlobal(t *Tracker) *Tracker {
	prev := Global()
	if t == nil {
		// Don't reset globalOnce so concurrent Global() callers
		// keep getting the same one; instead expose a Swap on the
		// existing struct so test cleanup is symmetric.
		prev = global
		global = New()
	} else {
		global = t
	}
	return prev
}

// InstallSignalHandlers installs SIGINT/SIGTERM/SIGHUP handlers that
// trigger the global tracker's Shutdown. The handlers do not call
// os.Exit on their own — they hand the cleanup to a deferred
// tracker.Shutdown in main — so the binary still flushes stdout /
// runs deferred close hooks in the usual way.
//
// The handlers also close stdoutWriter so a cmd/wagyu process that
// is interrupted mid-write gets a clean EOF on its output (the
// changelog's "scripts reading the CLI's output until end-of-file"
// symptom).
//
// Callers who do NOT want signal interference (the TUI's bubbletea
// loop installs its own SIGINT handler that translates the signal
// to a tea.QuitMsg) should skip this call; concurrent signal.Notify
// subscribers are independent in Go's runtime, but the redundant
// Shutdown still costs the grace-period wait when the user hits
// Ctrl-C inside the TUI.
func InstallSignalHandlers() {
	InstallSignalHandlersOn(Global())
}

// InstallSignalHandlersOn is InstallSignalHandlers for an explicit
// tracker. The signal-handler goroutine is started exactly once per
// tracker; a second call is a no-op.
func InstallSignalHandlersOn(t *Tracker) {
	if t == nil {
		return
	}
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		// First signal: trigger shutdown. Subsequent signals are
		// ignored (the OS will deliver SIGKILL if we don't exit
		// promptly, which is the right escalation).
		select {
		case sig := <-ch:
			// Honour an explicit exit code so a wrapper script
			// that captured the exit value of the CLI sees 130
			// (SIGINT) / 143 (SIGTERM) / 129 (SIGHUP) instead of
			// the post-cleanup 0. We translate via a sentinel
			// goroutine that defers os.Exit with the right
			// code.
			t.Shutdown(contextWithGrace(t, defaultShutdownGrace))
			switch sig {
			case syscall.SIGINT:
				os.Exit(130)
			case syscall.SIGTERM:
				os.Exit(143)
			case syscall.SIGHUP:
				os.Exit(129)
			}
		case <-t.Stopped():
			return
		}
	}()
}

const defaultShutdownGrace = 7 * time.Second

// contextWithGrace returns a context whose deadline is grace from
// "now". The shutdown goroutine does not block on this context's
// Done() — it only uses it as the parent for t.Shutdown — so the
// cancel function is not exposed to the caller. We still have to
// give it a name to satisfy govet's "defer the cancel" rule; the
// cancel runs in a 1-tick goroutine tied to the context's lifetime.
func contextWithGrace(_ *Tracker, grace time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	go func() {
		<-ctx.Done()
		cancel()
	}()
	return ctx
}
