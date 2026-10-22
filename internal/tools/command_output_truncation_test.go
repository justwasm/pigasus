package tools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The run_command result text, replayed from live 1.4.4 request bodies
// (一手, docs/cycles/command-output-truncation). These are the exact strings
// the reference sent the model, stamps aside.
var capturedCommandTimes = [2]time.Time{
	time.Date(2026, 10, 22, 11, 14, 2, 0, time.FixedZone("", 8*3600)),
	time.Date(2026, 10, 22, 11, 14, 3, 0, time.FixedZone("", 8*3600)),
}

func TestCommandResultPlain(t *testing.T) {
	r := &RunCommand{Home: t.TempDir()}
	got := r.renderResult(capturedCommandTimes[0], capturedCommandTimes[1], 0, "a\nb\n")
	want := "Created At: 2026-10-22T11:14:02+08:00\n" +
		"Completed At: 2026-10-22T11:14:03+08:00\n" +
		"Command exited with code 0.\n\n" +
		"Command output:\n" +
		"a\nb\n"
	if got != want {
		t.Errorf("plain result:\n got %q\nwant %q", got, want)
	}
}

func TestCommandResultEmptyOutput(t *testing.T) {
	r := &RunCommand{Home: t.TempDir()}
	got := r.renderResult(capturedCommandTimes[0], capturedCommandTimes[1], 0, "")
	want := "Created At: 2026-10-22T11:14:02+08:00\n" +
		"Completed At: 2026-10-22T11:14:03+08:00\n" +
		"Command exited with code 0.\n\n" +
		"No output from the command."
	if got != want {
		t.Errorf("empty-output result:\n got %q\nwant %q", got, want)
	}
}

// A 10195-byte output (10240-byte body) is echoed verbatim; one more byte
// switches the result to the note + preview form. Both sides of the boundary
// were captured.
func TestCommandResultTruncationThreshold(t *testing.T) {
	r := &RunCommand{Home: t.TempDir(), ConversationID: "threshold"}
	short := r.renderResult(capturedCommandTimes[0], capturedCommandTimes[1], 0, strings.Repeat("K", 10195))
	if !strings.HasSuffix(short, "Command output:\n"+strings.Repeat("K", 10195)) {
		t.Fatalf("10195 bytes must pass through verbatim, got %d bytes", len(short))
	}
	if strings.Contains(short, "truncated") {
		t.Fatalf("10195 bytes must not truncate")
	}
	long := r.renderResult(capturedCommandTimes[0], capturedCommandTimes[1], 0, strings.Repeat("K", 10196))
	if !strings.Contains(long, "Here is a preview with the middle of the output truncated:") {
		t.Fatalf("10196 bytes must truncate, got %q", long[:200])
	}
	// 10241-byte body (10196 + the 45-byte header), 9894 kept.
	if !strings.Contains(long, "<truncated 347 of 10241 bytes from the middle of the output>") {
		t.Errorf("wrong marker in %q", long)
	}
}

// The truncated form: note + hint + body whose middle is replaced, with the
// full output spilled under brain/<conv>/.system_generated/steps/<n>/.
func TestCommandResultTruncationSpillsOutput(t *testing.T) {
	home := t.TempDir()
	r := &RunCommand{Home: home, ConversationID: "conv-1"}
	output := strings.Repeat("A", 20000)
	got := r.renderResult(capturedCommandTimes[0], capturedCommandTimes[1], 0, output)

	path := filepath.Join(home, "brain", "conv-1", ".system_generated", "steps", "1", "output.txt")
	spilled, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("full output was not spilled: %v", err)
	}
	if string(spilled) != output {
		t.Errorf("spilled %d bytes, want the %d-byte original", len(spilled), len(output))
	}

	wantNote := "The tool's output was truncated due to being too long. " +
		"You can find the full output in a file at file://" + path + ".\n\n" +
		"Here is a preview with the middle of the output truncated:\n" +
		"Command exited with code 0.\n\n" +
		"Command output:\n"
	if !strings.Contains(got, wantNote) {
		t.Fatalf("missing the captured note/hint:\n got %q", got)
	}
	if !strings.Contains(got, "<truncated 10151 of 20045 bytes from the middle of the output>") {
		t.Errorf("wrong marker in %q", got)
	}
	// 4902 bytes of head, 4947 of tail — the captured split for newline-free
	// output.
	head := strings.Repeat("A", commandPreviewHead)
	tail := strings.Repeat("A", commandPreviewTail)
	if !strings.Contains(got, "Command output:\n"+head+"\n<truncated") {
		t.Errorf("head is not %d bytes", commandPreviewHead)
	}
	if !strings.HasSuffix(got, "<truncated 10151 of 20045 bytes from the middle of the output>\n"+tail) {
		t.Errorf("tail is not %d bytes", commandPreviewTail)
	}
}

// The preview is the one place the reference emits CRLF: its newlines come
// from a render of the spilled file, not from a rewrite of the command's
// output (the plain path keeps LF).
func TestCommandResultPreviewUsesCRLF(t *testing.T) {
	r := &RunCommand{Home: t.TempDir(), ConversationID: "crlf"}
	output := strings.Repeat("x\n", 8000)
	got := r.renderResult(capturedCommandTimes[0], capturedCommandTimes[1], 0, output)
	body := got[strings.Index(got, "Command output:\n")+len("Command output:\n"):]
	if !strings.HasPrefix(body, strings.Repeat("x\r\n", 100)) {
		t.Errorf("preview should be CRLF-terminated, got %q", body[:80])
	}
}

// A spill failure must not claim a file exists: the preview is still
// returned, but without the note.
func TestCommandResultTruncationWithoutSpillHasNoNote(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &RunCommand{Home: blocked, ConversationID: "nope"}
	got := r.renderResult(capturedCommandTimes[0], capturedCommandTimes[1], 0, strings.Repeat("A", 20000))
	if strings.Contains(got, "file://") {
		t.Errorf("note must not point at an unwritable path: %q", got)
	}
	if !strings.Contains(got, "<truncated") {
		t.Errorf("preview must survive a failed spill: %q", got[:200])
	}
}
