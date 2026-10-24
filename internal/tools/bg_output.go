// bg_output.go — cycle 258. AGY 1.4.0 changelog line 24:
//
//	"Fixed a background command's final output sometimes missing from its
//	 task log; the output is now always recorded when the command finishes
//	 or is canceled."
//
// Before this cycle, a background command's output buffer was the only
// place the captured stdout/stderr lived. When the cmd.Wait goroutine
// observed the exit it set t.done = true and t.err, but did not snapshot
// the buffer. A caller (the /tasks panel, command_status, manage_task)
// that raced the goroutine could observe t.done = true with an empty
// buffer because the I/O relay goroutines had not yet finished flushing
// their last bytes — and there was no place to record the final reason
// (exit code, signal, user cancel) anyway. The fix is recordFinalOutput:
//
//   - recordFinalOutput(buf, log, reason) copies whatever bytes are in the
//     live buffer (buf) into the persistent task log (log), then appends
//     a one-line reason for the exit. The sync.Once on log guarantees
//     idempotency, so even if the cmd.Wait goroutine and kill() race to
//     observe termination the first wins and the second is a no-op.
//
//   - The function is invoked via defer in the cmd.Wait goroutine, so a
//     panic during shutdown or an unexpected goroutine termination still
//     captures the tail of the buffer before the task is forgotten.
//
//   - The function is invoked from kill() too, so a SIGTERM/SIGKILL
//     from manage_task records the tail at the moment the signal is
//     delivered rather than waiting for the cmd.Wait goroutine.
//
// The tail-truncation policy (bgLogTailBytes) keeps memory bounded: a
// command that prints megabytes does not pin 1 MB+ in the log, only the
// last 4 KB plus a "[truncated N bytes]" marker.
//
// No third-party deps. stdlib only.
package tools

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// bgLogTailBytes is the upper bound on the bytes preserved in a task log
// at exit time. Commands that print megabytes (e.g. `find /`, a runaway
// log stream) would otherwise pin unbounded memory in the agent's
// per-task log until the bgTable entry is evicted. The cycle-258 fix
// mirrors the cycle-198 truncate policy used elsewhere for tool outputs:
// keep the tail (the most recent bytes are what an operator inspecting
// the log after the fact wants to see) plus a marker noting how much
// was dropped.
//
// 4 KiB is the cycle-198 truncate default for prose output, so the
// two policies line up. Tests pin this value at 4096 bytes.
const bgLogTailBytes = 4096

// recordFinalOutput copies any pending bytes from the live output buffer
// (buf) into the persistent task log (log) and appends a one-line reason
// for the exit. Idempotent: log.appendOnce guarantees only the first call
// wins; subsequent invocations are no-ops. This matters because both the
// cmd.Wait goroutine and the kill() path could observe termination in a
// pathological race — the first records, the second is silent.
//
// The function is nil-safe: nil receivers return without writing so the
// defer in start() can call it unconditionally before fields are wired
// up. (bgTask.start constructs t.log as a zero-value SafeBuffer, so in
// practice nil only happens for a future caller that passes literals.)
//
// Every bg exit path goes through this function:
//
//   - Normal completion (cmd.Wait returns nil): reason = "exit code 0".
//   - Non-zero exit (cmd.Wait returns *exec.ExitError): reason = "exit
//     code N".
//   - Killed by signal (kill() sends SIGKILL): reason = "killed by user".
//   - Canceled by user (kill() / context cancel): reason = "canceled".
//
// Invoked via defer from the cmd.Wait goroutine so a panic or an
// unexpected goroutine termination still captures the tail before the
// task is forgotten. See start() in support.go for the call site.
func recordFinalOutput(buf, log *SafeBuffer, reason string) {
	if buf == nil || log == nil {
		return
	}
	log.appendOnce.Do(func() {
		pending := buf.String()
		if len(pending) > bgLogTailBytes {
			dropped := len(pending) - bgLogTailBytes
			// Keep the tail; prepend a marker naming the
			// dropped prefix so the operator sees both that
			// truncation happened and how many bytes were
			// discarded. String concat allocates once.
			pending = "[truncated " + strconv.Itoa(dropped) +
				" bytes]\n" + pending[len(pending)-bgLogTailBytes:]
		}
		if pending != "" {
			log.WriteString(pending)
			if !strings.HasSuffix(pending, "\n") {
				log.WriteByte('\n')
			}
		}
		if reason != "" {
			log.WriteString(reason)
			if !strings.HasSuffix(reason, "\n") {
				log.WriteByte('\n')
			}
		}
	})
}

// bgExitReason turns a cmd.Wait error into a one-line reason string for
// the task log. The mapping:
//
//   - nil                  -> "exit code 0"
//   - *exec.ExitError      -> "exit code N"   (N may be -1 for signal
//     kills; the os/exec
//     contract is documented
//     to return -1 when the
//     process exited because
//     of a signal rather than
//     a normal return.)
//   - context.Canceled     -> "canceled"
//   - context.DeadlineExceeded -> "timed out"
//   - other                -> "exited with error: <msg>"
//
// The "canceled" / "timed out" mapping matters because cmd.CommandContext
// surfaces context cancellation through the same channel as a normal
// process exit — without this mapping the log would record a confusing
// "exit code -1" or "exited with error: signal: killed" for an
// intentional cancellation.
func bgExitReason(waitErr error) string {
	if waitErr == nil {
		return "exit code 0"
	}
	if errors.Is(waitErr, context.Canceled) {
		return "canceled"
	}
	if errors.Is(waitErr, context.DeadlineExceeded) {
		return "timed out"
	}
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		return fmt.Sprintf("exit code %d", exitErr.ExitCode())
	}
	return "exited with error: " + waitErr.Error()
}
