// pasteboard_test.go — the pasteboard's own sentence.
//
// 【文档】 official-docs/cli/troubleshooting.md:126 —
//
//	Error: local pasteboard is empty or unreachable over SSH connection
package clipboard

import (
	"errors"
	"testing"
)

// TestPasteboardUnavailableErrorNamesTheSSHSentence pins the
// reference's own sentence for a paste over SSH.
//
// The bytes are the whole point: the composer shows the notice as it
// is, so the "Error: " belongs to the sentence itself.
func TestPasteboardUnavailableErrorNamesTheSSHSentence(t *testing.T) {
	for _, key := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "10.0.0.1 5000 10.0.0.2 22")
			got := pasteboardUnavailableError()
			const want = "Error: local pasteboard is empty or unreachable over SSH connection"
			if got.Error() != want {
				t.Errorf("error = %q, want %q", got.Error(), want)
			}
			if !errors.Is(got, ErrPasteboardUnreachable) {
				t.Errorf("error %v does not carry ErrPasteboardUnreachable", got)
			}
		})
	}
}

// TestPasteboardUnavailableErrorKeepsTheLocalSentence pins the other
// half: a local pasteboard with nothing in it is the reference's
// 1.4.9 sentence, not the SSH one.
func TestPasteboardUnavailableErrorKeepsTheLocalSentence(t *testing.T) {
	for _, key := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
		t.Setenv(key, "")
	}
	got := pasteboardUnavailableError()
	if got != ErrNoMedia {
		t.Errorf("error = %v, want ErrNoMedia", got)
	}
}
