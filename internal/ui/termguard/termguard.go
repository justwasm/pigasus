package termguard

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"sync"
)

// ResetEscapeSequence contains ANSI sequences to restore cursor visibility,
// disable all mouse tracking modes, exit alternate screen without restoring
// saved cursor position, and reset text styling.
// Note: We use \x1b[?1047l instead of \x1b[?1049l because \x1b[?1049l restores
// the saved cursor position (DECRC), which would overwrite post-exit output
// such as the summary exit banner.
const ResetEscapeSequence = "\x1b[0m\x1b[?25h\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l\x1b[?2004l\x1b[?1047l"

// Guard tracks the terminal state and provides panic-safe restoration.
type Guard struct {
	once  sync.Once
	state *termState
	out   io.Writer
}

// Capture records the current terminal modes for stdin and stdout.
func Capture() *Guard {
	return &Guard{
		state: captureState(),
		out:   os.Stderr,
	}
}

// SetWriter overrides the output destination for escape sequences (e.g. for testing).
func (g *Guard) SetWriter(w io.Writer) {
	if g != nil {
		g.out = w
	}
}

// Restore brings the terminal back to its pre-TUI state and emits reset sequences.
// Restore is idempotent and safe for concurrent calls.
func (g *Guard) Restore() {
	if g == nil {
		return
	}
	g.once.Do(func() {
		out := g.out
		if out == nil {
			out = os.Stderr
		}
		// 1. Emit escape sequences to clean up terminal emulator modes
		_, _ = out.Write([]byte(ResetEscapeSequence))
		// 2. Restore low-level terminal driver modes (raw mode, echo, etc.)
		restoreState(g.state)
	})
}

// Run executes fn under the protection of Guard. If fn panics, Run restores
// the terminal modes immediately, outputs the panic and stack trace to stderr,
// and returns the panic as an error.
func Run(fn func() error) (err error) {
	g := Capture()
	defer g.Restore()

	defer func() {
		if r := recover(); r != nil {
			g.Restore()
			stack := debug.Stack()
			fmt.Fprintf(os.Stderr, "\n\x1b[31;1mPanic recovered in TUI: %v\x1b[0m\n\n%s\n", r, string(stack))
			err = fmt.Errorf("tui panic: %v", r)
		}
	}()

	return fn()
}
