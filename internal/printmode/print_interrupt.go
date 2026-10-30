// print_interrupt.go — the Ctrl+C that ends a headless turn without
// killing the process.
//
// 【一手】 agy 1.4.11 added a `finished_reason` field to the
// `--output-format json` / `stream-json` result: it reports
// "interrupted" "when the run was canceled such as with `Ctrl+C`", so
// scripts can tell a cut-off answer from a complete one.
//
// For the field to exist at all, the reference has to **catch** the
// signal: an uncaught SIGINT kills the process, and a killed process
// writes no envelope. So the print turn installs a handler, cancels the
// turn's context, lets the turn unwind, and writes the envelope with the
// reason.
//
// A second Ctrl+C is fatal. The first one restores the default handler
// (`signal.Reset`), so a user who wants the process gone can always have
// it — the graceful path must not be a trap.
package printmode

import (
	"context"
	"os"
	"os/signal"
	"sync/atomic"
)

// printInterruptGuard cancels the turn's context on the first Ctrl+C and
// remembers that the interrupt is what stopped the turn.
type printInterruptGuard struct {
	ch          chan os.Signal
	interrupted atomic.Bool
	stop        func()
}

// newPrintInterruptGuard starts listening for SIGINT.
func newPrintInterruptGuard() *printInterruptGuard {
	g := &printInterruptGuard{ch: make(chan os.Signal, 1)}
	signal.Notify(g.ch, os.Interrupt)
	g.stop = func() { signal.Stop(g.ch) }
	return g
}

// cancel cancels the turn's context on the first Ctrl+C.
//
// The handler is stopped and the default disposition restored *before*
// the cancel, so a second Ctrl+C terminates the process the way it did
// before this guard existed.
func (g *printInterruptGuard) cancel(cancel context.CancelFunc) {
	go func() {
		<-g.ch
		g.interrupted.Store(true)
		g.stop()
		signal.Reset(os.Interrupt)
		cancel()
	}()
}

// Interrupted reports whether a Ctrl+C stopped the turn.
func (g *printInterruptGuard) Interrupted() bool { return g.interrupted.Load() }

// Close stops listening for SIGINT. It is safe to call more than once.
func (g *printInterruptGuard) Close() {
	if g.stop != nil {
		g.stop()
	}
}

// printInterruptedExitCode is the exit status of a headless run that a
// Ctrl+C ended: 128 + SIGINT, the conventional shell status.
//
// 【推测】 The 1.4.11 note pins the *field*, not the status: it says
// the field exists "so scripts can tell a cut-off answer from a complete
// one". The reference's own exit code for an interrupted run is not
// recoverable from the notes, and 130 is the shell's own convention for
// "terminated by SIGINT".
const printInterruptedExitCode = 130

// printInterruptedMessage is the stderr line an interrupted headless run
// prints before exiting.
func printInterruptedMessage(runErr error) string {
	if runErr != nil {
		if msg := runErr.Error(); msg != "" {
			return "interrupted: " + msg
		}
	}
	return "interrupted"
}
