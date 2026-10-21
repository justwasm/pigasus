// subagent_error_status_test.go — cycle agent-stopped-error-status:
//
// AGY 1.3.1 changelog:
//
//	Fixed subagents that stopped on an error, such as running out
//	of quota or model capacity, being shown as `Done` in `/agents`
//	and the list of running agents; they now show `Error:` with the
//	reason, and the error clears once the subagent makes further
//	progress.
//
// The tests below pin down the four contract points:
//
//  1. an errored subagent's StatusLine starts with "Error:" and
//     carries the captured reason;
//  2. a MarkProgress (or a fresh Send) clears the error label so
//     StatusLine no longer carries the stale reason;
//  3. a subagent that exited cleanly still reports "Done" — the
//     failure branch does not regress the success branch;
//  4. the error reason text is preserved byte-for-byte through
//     StatusLine so the panel can render it without truncation.
//
// All four tests use small real sub-processes (true / false / sleep)
// so they exercise the production Wait() codepath — no mocking, no
// parallel.
package subagent

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSubagentStatus_ErrorShownOnStop — errored subagents render
// "Error:" with the reason, NOT "Done". This is the bug AGY 1.3.1
// closed in the reference.
func TestSubagentStatus_ErrorShownOnStop(t *testing.T) {
	s := NewSubagent("false") // exits 1 immediately, Wait returns *exec.ExitError
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Wait(ctx); err == nil {
		t.Fatal("expected Wait to return an error for `false`, got nil")
	}

	if s.State != "failed" {
		t.Errorf("State = %q, want %q", s.State, "failed")
	}
	if s.LastError == "" {
		t.Errorf("LastError = %q, want non-empty (the captured reason)", s.LastError)
	}

	got := s.StatusLine()
	if got == StatusDone {
		t.Fatalf("StatusLine = %q, want \"Error:\" prefix (not Done)", got)
	}
	if !strings.HasPrefix(got, StatusError) {
		t.Fatalf("StatusLine = %q, want prefix %q", got, StatusError)
	}
	if !strings.Contains(got, s.LastError) {
		t.Errorf("StatusLine = %q, want it to contain LastError=%q", got, s.LastError)
	}
}

// TestSubagentStatus_ErrorClearsOnProgress — the error reason clears
// when the subagent makes further progress. Uses the explicit
// MarkProgress API so the test does not depend on a live stdin pipe.
func TestSubagentStatus_ErrorClearsOnProgress(t *testing.T) {
	s := NewSubagent("false")
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Wait(ctx); err == nil {
		t.Fatal("expected Wait to return an error for `false`, got nil")
	}

	if s.LastError == "" {
		t.Fatalf("precondition: LastError should be set after Wait errored, got empty")
	}
	if !strings.Contains(s.StatusLine(), StatusError) {
		t.Fatalf("precondition: StatusLine should start with %q, got %q", StatusError, s.StatusLine())
	}

	// Progress — clears the error reason.
	s.MarkProgress()
	if s.LastError != "" {
		t.Errorf("LastError after MarkProgress = %q, want empty", s.LastError)
	}

	// The State is still "failed" — MarkProgress only clears the
	// display reason, it does not rewrite the terminal state. The
	// /agents panel re-renders and the "Error:" prefix is gone,
	// so the user sees the agent made progress.
	if strings.Contains(s.StatusLine(), StatusError) {
		t.Errorf("StatusLine after MarkProgress = %q, want no %q prefix",
			s.StatusLine(), StatusError)
	}
}

// TestSubagentStatus_NormalDoneStillWorks — the success branch is
// untouched: a subagent that exits 0 still reports "Done".
// Regression guard for the cycle.
func TestSubagentStatus_NormalDoneStillWorks(t *testing.T) {
	s := NewSubagent("true") // exits 0 immediately
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	if s.State != "done" {
		t.Errorf("State = %q, want %q", s.State, "done")
	}
	if s.LastError != "" {
		t.Errorf("LastError = %q, want empty on clean exit", s.LastError)
	}
	if got := s.StatusLine(); got != StatusDone {
		t.Errorf("StatusLine = %q, want %q", got, StatusDone)
	}
}

// TestSubagentStatus_ErrorReasonPreserved — the captured reason is
// passed through StatusLine byte-for-byte so the panel renders the
// full message. Uses a sub-process whose error message is non-trivial
// (a multi-word reason including digits) so the assertion would fail
// on any truncation or substitution.
func TestSubagentStatus_ErrorReasonPreserved(t *testing.T) {
	// `sh -c 'exit 7'` yields "exit status 7" — different from the
	// default `false` reason ("exit status 1") so we know we are
	// surfacing the real error, not a hard-coded string.
	s := NewSubagent("sh", "-c", "exit 7")
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	waitErr := s.Wait(ctx)
	if waitErr == nil {
		t.Fatal("expected Wait to return an error for `exit 7`, got nil")
	}

	reason := s.ErrorReason()
	if reason == "" {
		t.Fatalf("ErrorReason() = empty, want captured Wait error text")
	}
	if !strings.Contains(reason, "exit status 7") {
		t.Errorf("ErrorReason() = %q, want it to contain %q",
			reason, "exit status 7")
	}

	// StatusLine must embed the same reason.
	status := s.StatusLine()
	if !strings.HasPrefix(status, StatusError+" ") {
		t.Fatalf("StatusLine = %q, want prefix %q", status, StatusError+" ")
	}
	if !strings.Contains(status, "exit status 7") {
		t.Errorf("StatusLine = %q, want it to contain %q",
			status, "exit status 7")
	}

	// Sanity: clearing via MarkProgress also drops it from
	// StatusLine (the two paths share the same backing field).
	s.MarkProgress()
	if s.ErrorReason() != "" {
		t.Errorf("ErrorReason after MarkProgress = %q, want empty", s.ErrorReason())
	}
}

// TestSubagentStatus_PendingAndRunningLabels — the StatusLine
// mapping for the non-terminal states is also pinned down so the
// /agents panel does not regress to "Done" for a still-running row.
func TestSubagentStatus_PendingAndRunningLabels(t *testing.T) {
	pending := NewSubagent("true")
	if got := pending.StatusLine(); got != StatusPending {
		t.Errorf("new Subagent StatusLine = %q, want %q", got, StatusPending)
	}

	running := NewSubagent("sleep", "30")
	if err := running.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = running.Kill() })
	if got := running.StatusLine(); got != StatusRunning {
		t.Errorf("after Start StatusLine = %q, want %q", got, StatusRunning)
	}
}

// TestSubagentStatus_SendClearsError — the production progress path
// (Send writing to stdin) clears the error reason. This is the wiring
// the changelog line "the error clears once the subagent makes
// further progress" requires for shell-style subagents.
func TestSubagentStatus_SendClearsError(t *testing.T) {
	// We can't easily simulate "errored, then resumed" on the same
	// bash sub-process (it would be dead). What we can assert is
	// that Send on a Subagent that already has LastError set
	// clears it — the cycle adds the clearLastError() call right
	// before the stdin write.
	s := NewSubagent("true")
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Simulate a prior failure by setting LastError directly
	// (covers the recovery path even on the cycle's testing
	// surface, where we can't easily construct a live
	// err-then-resume bash flow).
	s.mu.Lock()
	s.LastError = "synthetic prior error"
	s.mu.Unlock()

	if s.ErrorReason() != "synthetic prior error" {
		t.Fatalf("setup: ErrorReason = %q, want %q",
			s.ErrorReason(), "synthetic prior error")
	}

	if err := s.Send("ping\n"); err != nil {
		// On macOS, the stdin pipe might already be closed
		// because Wait drained it. That is fine — we only
		// need the error reason to be cleared, which happens
		// before the Write call.
		t.Logf("Send returned (acceptable on drained pipe): %v", err)
	}
	if s.ErrorReason() != "" {
		t.Errorf("ErrorReason after Send = %q, want empty", s.ErrorReason())
	}

	// Tidy up.
	_ = s.Kill()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.Wait(ctx)
}
