package clipboard_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/clipboard"
)

// TestWriteReadRoundTrip pins the wiring of the clipboard's system
// half: the clone's Write and Read go through atotto/clipboard, the
// library the reference binary vendors, which on darwin shells out to
// pbcopy / pbpaste — the same two commands the hand-rolled version
// used.
//
// The test writes to the real clipboard and puts back the text that
// was there. A pasteboard that held only an image cannot be restored
// through pbpaste / pbcopy (these tools see its text representation
// only), and the package's OSC 52 copy test already replaces it.
//
// The terminal is taken out of the picture first: with a terminal that
// speaks OSC 52 the write would go out as an escape sequence and never
// reach the system clipboard at all.
func TestWriteReadRoundTrip(t *testing.T) {
	const marker = "pigasus-clipboard-round-trip"

	clearEnv(t, "SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY",
		"MOSH_CONNECTION", "TERM_PROGRAM", "LC_TERMINAL", "TERM")

	before := clipboard.Read()
	if err := clipboard.Write(marker); err != nil {
		t.Skipf("no clipboard utility here: %v", err)
	}
	t.Cleanup(func() { _ = clipboard.Write(before) })

	if got := clipboard.Read(); got != marker {
		t.Fatalf("clipboard.Read() = %q, want %q", got, marker)
	}
}
