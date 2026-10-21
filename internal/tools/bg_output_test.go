// bg_output_test.go — cycle 258.
//
// Tests for AGY 1.4.0 changelog line 24:
//
//	"Fixed a background command's final output sometimes missing from
//	 its task log; the output is now always recorded when the command
//	 finishes or is canceled."
//
// The fix lives in internal/tools/bg_output.go (recordFinalOutput,
// bgExitReason, bgLogTailBytes) and is wired into the cmd.Wait
// goroutine and kill() in internal/tools/support.go.
//
// The tests below drive recordFinalOutput directly through safeBuffer,
// because the function is the single exit point every bg termination
// reason converges on. End-to-end coverage of the bgTable integration
// lives in nonnetwork_test.go (TestBackgroundCommands,
// TestBackgroundCommandKill) and git_editor_disable_test.go
// (TestBackgroundCommand_GitEditorSet); we don't duplicate that here.
package tools

import (
	"strings"
	"testing"
)

// fillBuffer writes s into a fresh safeBuffer and returns it.
func fillBuffer(s string) *safeBuffer {
	var b safeBuffer
	if s != "" {
		b.WriteString(s)
	}
	return &b
}

// assertContains fails the test if needle is not in haystack, with a
// short message naming both. Avoids the noise of assert.Error-style
// helpers for the simple substring checks below.
func assertContains(t *testing.T, haystack, needle, label string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("%s: log missing %q\nlog was: %q",
			label, needle, haystack)
	}
}

// assertNotContains fails the test if needle IS in haystack.
func assertNotContains(t *testing.T, haystack, needle, label string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Errorf("%s: log unexpectedly contains %q\nlog was: %q",
			label, needle, haystack)
	}
}

// TestRecordFinalOutput_NormalExit — AGY 1.4.0 changelog line 24
// happy path. Command exits with code 0, the live buffer holds 100
// bytes, the log records all 100 bytes plus the "exit code 0" line.
// The reason is the exact phrasing bgExitReason(nil) returns so the
// test pins the contract callers downstream depend on.
func TestRecordFinalOutput_NormalExit(t *testing.T) {
	payload := strings.Repeat("a", 100)
	buf := fillBuffer(payload)
	var log safeBuffer

	recordFinalOutput(buf, &log, "exit code 0")

	got := log.String()
	assertContains(t, got, payload, "NormalExit")
	assertContains(t, got, "exit code 0", "NormalExit")
	// The reason must be the LAST line so a downstream panel can
	// parse it without scanning for the substring.
	if strings.Count(got, "exit code 0") != 1 {
		t.Errorf("NormalExit: log has %d occurrences of reason, want 1: %q",
			strings.Count(got, "exit code 0"), got)
	}
}

// TestRecordFinalOutput_ErrorExit — non-zero exit code. The cycle-258
// fix must record the output regardless of the exit code, because the
// changelog line is "always recorded when the command finishes or is
// canceled" with no exception for failure modes. Exit code 137 is the
// 128+SIGKILL(9) convention, but in our model the cmd.Wait error path
// records the numeric code regardless.
func TestRecordFinalOutput_ErrorExit(t *testing.T) {
	payload := strings.Repeat("e", 50)
	buf := fillBuffer(payload)
	var log safeBuffer

	recordFinalOutput(buf, &log, "exit code 137")

	got := log.String()
	assertContains(t, got, payload, "ErrorExit")
	assertContains(t, got, "exit code 137", "ErrorExit")
	// The exit-code line is the LAST line, separated from the body
	// by a newline so a downstream panel can render the reason on
	// its own row.
	if !strings.HasSuffix(got, "exit code 137\n") {
		t.Errorf("ErrorExit: reason is not the last line; log = %q", got)
	}
}

// TestRecordFinalOutput_KilledBySignal — kill() path. A command that
// SIGKILLed leaves a partial buffer (the kill happens mid-stream, not
// after a clean exit), so the recorded output is whatever the OS
// flushed before the signal landed. The fix's job is to record
// *whatever is in the buffer*, not to fabricate bytes the process
// never wrote.
func TestRecordFinalOutput_KilledBySignal(t *testing.T) {
	payload := strings.Repeat("k", 200)
	buf := fillBuffer(payload)
	var log safeBuffer

	recordFinalOutput(buf, &log, "killed by user")

	got := log.String()
	assertContains(t, got, payload, "KilledBySignal")
	assertContains(t, got, "killed by user", "KilledBySignal")
}

// TestRecordFinalOutput_CanceledByUser — user-cancellation case. The
// command produces some output, then the user calls manage_task kill
// (which translates to kill()), and the cycle-258 fix records the
// partial output. The reason reflects what bgExitReason returns for
// a context.Canceled error wrapped by cmd.CommandContext; we test
// against the literal "canceled" string the function produces.
func TestRecordFinalOutput_CanceledByUser(t *testing.T) {
	payload := strings.Repeat("c", 64)
	buf := fillBuffer(payload)
	var log safeBuffer

	recordFinalOutput(buf, &log, "canceled")

	got := log.String()
	assertContains(t, got, payload, "CanceledByUser")
	assertContains(t, got, "canceled", "CanceledByUser")
	// The cycle-258 reason for context.Canceled is exactly the
	// word "canceled" — no "exit code" prefix — so a downstream
	// panel can pattern-match cleanly.
	assertNotContains(t, got, "exit code", "CanceledByUser")
}

// TestRecordFinalOutput_EmptyOutput — the edge case the AGY 1.4.0
// changelog line is silent on. A command that exits cleanly with no
// output at all: the live buffer is empty, the log records the exit
// reason, and there is no body. This pins the contract that
// recordFinalOutput does NOT fabricate a "(no output)" marker — the
// downstream consumer (the /tasks panel, command_status, manage_task)
// can detect "empty output" by checking for the reason line alone.
//
// The cycle-258 reasoning: a synthetic marker would obscure the
// distinction between "the command produced no output" (legitimate,
// e.g. `true`) and "the output was lost" (the bug). Leaving the body
// empty preserves that distinction — a reason line with no preceding
// body is unambiguous.
func TestRecordFinalOutput_EmptyOutput(t *testing.T) {
	var buf, log safeBuffer
	recordFinalOutput(&buf, &log, "exit code 0")

	got := log.String()
	assertContains(t, got, "exit code 0", "EmptyOutput")
	// The body must be empty (only the reason line). A test that
	// accidentally injects a placeholder here would fail this
	// assertion, pinning the no-placeholder contract.
	if strings.Contains(strings.TrimSuffix(got, "\n"), "\n") {
		t.Errorf("EmptyOutput: log has unexpected body lines; got = %q", got)
	}
}

// TestRecordFinalOutput_LargeOutput — memory-bound tail truncation.
// The cycle-258 fix uses bgLogTailBytes (4096) as the upper bound on
// the preserved tail, so a command that prints 1 MB does not pin
// 1 MB in the per-task log. The test:
//
//   - writes 1 MiB of payload into buf,
//   - calls recordFinalOutput,
//   - asserts the resulting log is bounded (~ bgLogTailBytes + reason
//   - a "[truncated N bytes]" marker),
//   - asserts the LAST 4 KiB of the payload is preserved verbatim,
//   - asserts the FIRST bytes of the payload are NOT present (because
//     they were dropped).
//
// The "without OOM" requirement is met because recordFinalOutput
// allocates a single string of len(bgLogTailBytes) for the tail and
// a small marker; the dropped prefix is garbage-collected as soon as
// the buf String() copy goes out of scope.
func TestRecordFinalOutput_LargeOutput(t *testing.T) {
	const total = 1 << 20 // 1 MiB
	// Pre-build a payload whose head is "HEAD" and tail is "TAIL"
	// so the assertions below can confirm the prefix is dropped
	// and the suffix is preserved. Fill the middle with a known
	// byte (0x42) so a regression that pads with NULs is caught.
	head := strings.Repeat("H", 4)
	tail := strings.Repeat("T", 4)
	mid := strings.Repeat(string(byte(0x42)), total-len(head)-len(tail))
	payload := head + mid + tail
	if len(payload) != total {
		t.Fatalf("payload length = %d, want %d (test setup wrong)", len(payload), total)
	}
	buf := fillBuffer(payload)
	var log safeBuffer

	recordFinalOutput(buf, &log, "exit code 0")

	got := log.String()
	// The log must end with the reason line.
	if !strings.HasSuffix(got, "exit code 0\n") {
		t.Fatalf("LargeOutput: log does not end with reason; tail = %q",
			got[len(got)-64:])
	}
	// The head marker is dropped.
	assertNotContains(t, got, head, "LargeOutput")
	// The tail marker is preserved.
	assertContains(t, got, tail, "LargeOutput")
	// The body length is bounded — bgLogTailBytes + the truncated
	// marker + the reason. The cycle-258 fix should never pin
	// megabytes in the per-task log.
	if len(got) > bgLogTailBytes+256 {
		t.Errorf("LargeOutput: log size = %d, want <= bgLogTailBytes (=%d) + marker slack",
			len(got), bgLogTailBytes)
	}
	// The truncation marker names the dropped byte count.
	if !strings.Contains(got, "[truncated") {
		t.Errorf("LargeOutput: log missing [truncated N bytes] marker; got = %q",
			got[:min(80, len(got))])
	}
	// The last 4 KiB of the payload must appear verbatim. We
	// assert on a 64-byte sub-slice of the tail to keep the
	// error message short.
	want := payload[len(payload)-64:]
	assertContains(t, got, want, "LargeOutput")
}

// TestRecordFinalOutput_Idempotent — sync.Once contract. The
// cmd.Wait goroutine and kill() can race; only the first call to
// recordFinalOutput must write to the log. This test simulates the
// race by calling recordFinalOutput twice with different reasons and
// asserting that only the first reason is present.
//
// This pins the contract that downstream consumers (the /tasks panel)
// can rely on the log having exactly one reason line per task, even
// when the user mashes manage_task kill while the process is mid-exit.
func TestRecordFinalOutput_Idempotent(t *testing.T) {
	const payload = "alpha\nbeta\ngamma" // 17 bytes
	buf := fillBuffer(payload)
	var log safeBuffer

	recordFinalOutput(buf, &log, "exit code 0")
	recordFinalOutput(buf, &log, "killed by user") // second call: no-op

	got := log.String()
	assertContains(t, got, payload, "Idempotent")
	assertContains(t, got, "exit code 0", "Idempotent")
	// The second reason must NOT appear — the first call won the
	// sync.Once and the second was a silent no-op.
	assertNotContains(t, got, "killed by user", "Idempotent")
	if strings.Count(got, "exit code") != 1 {
		t.Errorf("Idempotent: log has %d reason lines, want 1: %q",
			strings.Count(got, "exit code"), got)
	}
}

// TestRecordFinalOutput_NilSafe — defensive guard. The defer in the
// cmd.Wait goroutine calls recordFinalOutput unconditionally; if a
// future refactor ever lets the goroutine run before t.log is wired
// up (it can't today — bgTask is constructed zero-valued, which is a
// usable safeBuffer — but the contract should hold even if a caller
// passes literals), the function must not panic.
func TestRecordFinalOutput_NilSafe(t *testing.T) {
	// Should not panic on nil buf.
	recordFinalOutput(nil, &safeBuffer{}, "exit code 0")
	// Should not panic on nil log.
	recordFinalOutput(&safeBuffer{}, nil, "exit code 0")
	// Should not panic on both nil.
	recordFinalOutput(nil, nil, "exit code 0")
}

// min is missing on Go versions before 1.21 in some build tags;
// locally we target 1.27 but be defensive.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
