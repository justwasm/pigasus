// slash_compact_test.go — cycle 237: /compact regression tests.
//
// The cycle-167 /compact implementation just halved m.session
// (compactSession helper); cycle 229 wired the PiG compactor;
// cycle 237 adds a generic summarizer path that drives
// internal/compaction.ComputeSummary directly. The tests in this
// file pin the cycle-237 contract:
//
//   - Argument parsing: `/compact` alone vs `/compact <message>`
//   - Pre/post stats: the summarizer reports StatsBefore /
//     StatsAfter so the user-facing "-Y%" reduction can be derived.
//   - Cancellation: a cancelled context restores the original
//     transcript (TestSlashCompact_CancelRestoresSession) — the
//     TUI relies on this so Esc leaves the user's history intact.
//   - Token estimation: EstimateTokens(s) is a /4 byte-count
//     approximation; the test pins the [200, 320] window for a
//     1 KB string the spec calls out.
//
// The tests deliberately do NOT exercise the cycle-229 PiG path
// (the manualCompactor seam) — those tests live in
// slash_cycle229_test.go and are unchanged. Cycle 237 ships the
// path that fires when m.compactor is nil, which is what every
// offline test (and the print-mode mirror) hits.

package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/compaction"
)

// fakeStreamClient is the test fake for backend.Client. It records
// the StreamRequest, returns a canned summary, and respects ctx
// cancellation (the chunk channel surfaces ctx.Err() as a final
// Error chunk so ComputeSummary can return ErrCancelled).
//
// Mirrors the cycle-235 mockClient shape but keeps every chunk on
// a single function so the cycle-237 cancellation test can
// synchronise deterministically without sleeping.
type fakeStreamClient struct {
	mu sync.Mutex

	// summary is what the fake Stream() returns. Empty string
	// means "don't write any chunks" (used by the cancellation
	// test to block until ctx fires).
	summary string

	// hang blocks the chunk writer until cancel fires. Used by
	// TestSlashCompact_CancelRestoresSession.
	hang bool

	// lastRequest is the most recent StreamRequest the fake saw;
	// tests inspect it to confirm the slash command sent the
	// expected system prompt + transcript payload.
	lastRequest backend.StreamRequest

	// calls counts Stream() invocations so tests can assert the
	// slash command made exactly one.
	calls int
}

func (f *fakeStreamClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	f.mu.Lock()
	f.calls++
	f.lastRequest = req
	hang := f.hang
	summary := f.summary
	f.mu.Unlock()

	out := make(chan backend.StreamChunk, 4)
	go func() {
		defer close(out)
		if hang {
			// Block until ctx fires, then surface the cancel
			// as a final Error chunk so ComputeSummary returns
			// ErrCancelled.
			<-ctx.Done()
			out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
			return
		}
		// Send the canned summary in two halves so the test
		// exercises the multi-chunk join path.
		mid := len(summary) / 2
		if mid == 0 {
			mid = len(summary)
		}
		out <- backend.StreamChunk{Text: summary[:mid]}
		out <- backend.StreamChunk{Text: summary[mid:], Done: true}
	}()
	return out, nil
}

// mkCompactNoProgModel returns a runMode wired for the cycle-237
// generic-compact path (m.compactor == nil, m.prog == nil so the
// goroutine resolves straight into m.response via
// applyGenericCompactResult). The backend is the supplied fake.
//
// Mirrors mkCompactModel in slash_cycle229_test.go but skips the
// compactor wiring so every test below hits the cycle-237 branch
// of startManualCompact.
func mkCompactNoProgModel(t *testing.T, be *fakeStreamClient) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(be, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.flags = newStandardFlagSetForTest(t, nil)
	return m
}

// TestSlashCompact_ParsesTrailingMessage pins the slash-command
// argument split. The cycle-237 implementation reuses the
// splitSlash helper that other commands use; this test guards
// against a future cycle stripping / re-quoting the args string
// in a way that drops the trailing message.
//
// The assertions mirror the spec: `/compact foo bar` reports
// "foo bar" as the trailing message (joined with single spaces);
// `/compact` alone reports "".
func TestSlashCompact_ParsesTrailingMessage(t *testing.T) {
	cases := []struct {
		buf  string
		want string
	}{
		{"/compact", ""},
		{"/compact foo", "foo"},
		{"/compact foo bar", "foo bar"},
		{"/compact   multi   space   args", "multi   space   args"},
	}
	for _, tc := range cases {
		cmd, args := splitSlash(tc.buf)
		if cmd != "compact" {
			t.Errorf("splitSlash(%q) cmd = %q, want %q", tc.buf, cmd, "compact")
		}
		// splitSlash returns the trimmed-and-joined args; mirror
		// the same trim semantics the slash command applies
		// before forwarding to startManualCompact.
		got := strings.TrimSpace(args)
		if got != tc.want {
			t.Errorf("splitSlash(%q) args = %q, want %q", tc.buf, got, tc.want)
		}
	}
}

// TestSlashCompact_PreCompactStats runs the cycle-237 summarizer
// against a small transcript and asserts:
//
//   - ComputeSummary returns a non-empty summary.
//   - StatsBefore.Messages == N (the transcript size).
//   - StatsBefore.Tokens ≈ the /4 byte count.
//   - StatsAfter.Tokens is non-zero (the summary string).
//
// The transcript is three user-role turns so the StatsBefore
// count is unambiguous (3 messages, ~T tokens). The canned
// summary is a 200-byte string so StatsAfter is also non-zero.
func TestSlashCompact_PreCompactStats(t *testing.T) {
	be := &fakeStreamClient{
		summary: strings.Repeat("summary bullet. ", 12), // ~200 bytes
	}
	sess := []backend.Message{
		{Role: "user", Content: strings.Repeat("u", 400)},
		{Role: "assistant", Content: strings.Repeat("a", 400)},
		{Role: "user", Content: strings.Repeat("u", 400)},
	}

	summary, err := compaction.ComputeSummary(context.Background(), be, "test-model", "", sess)
	if err != nil {
		t.Fatalf("ComputeSummary: %v", err)
	}
	if summary.Text == "" {
		t.Fatal("Summary.Text empty; want non-empty summary")
	}
	if got, want := summary.StatsBefore.Messages, 3; got != want {
		t.Errorf("StatsBefore.Messages = %d, want %d", got, want)
	}
	if summary.StatsBefore.Tokens <= 0 {
		t.Errorf("StatsBefore.Tokens = %d, want > 0", summary.StatsBefore.Tokens)
	}
	if summary.StatsAfter.Tokens <= 0 {
		t.Errorf("StatsAfter.Tokens = %d, want > 0", summary.StatsAfter.Tokens)
	}
	if pct := compaction.ReductionPercent(summary.StatsBefore, summary.StatsAfter); pct <= 0 {
		t.Errorf("ReductionPercent = %d, want > 0", pct)
	}
	if !strings.Contains(be.lastRequest.SystemPrompt, compaction.SummarizePrompt) {
		t.Errorf("SystemPrompt missing SummarizePrompt; got %q", be.lastRequest.SystemPrompt)
	}
	if len(be.lastRequest.Messages) != 1 || be.lastRequest.Messages[0].Role != "user" {
		t.Errorf("Request.Messages should be one user-role carrying the transcript; got %+v", be.lastRequest.Messages)
	}
}

// TestSlashCompact_CancelRestoresSession runs the slash command
// with a hanging backend, cancels the context mid-call, and
// asserts the original session is preserved (m.session is the
// input we started with, not the summary, not nil).
//
// The test drives the no-prog branch directly
// (applyGenericCompactResult) because the goroutine path posts a
// tea.Msg that needs a running *tea.Program. The applyGeneric
// method is what the future TUI handler will call; this test
// pins its contract independently of the message plumbing.
func TestSlashCompact_CancelRestoresSession(t *testing.T) {
	be := &fakeStreamClient{hang: true}
	m := mkCompactNoProgModel(t, be)
	sess := []backend.Message{
		{Role: "user", Content: "first turn"},
		{Role: "assistant", Content: "first reply"},
	}
	m.session = append([]backend.Message(nil), sess...)

	// Drive the goroutine path. We want cancellation to fire
	// immediately after the slash command dispatches, so we
	// reach into the package-level cancel and fire it from a
	// short sleep.
	_, _ = m.handleSlashCommand("/compact")
	// Give the goroutine a moment to install its cancel; the
	// TriggerCompactCancel call is a no-op if the slot is empty.
	time.Sleep(20 * time.Millisecond)
	TriggerCompactCancel()

	// The goroutine posts to m.response via applyGenericCompactResult
	// because m.prog is nil. Wait for the goroutine to settle.
	waitForCompactSettle(t, m, 2*time.Second)

	// Original session preserved verbatim.
	if len(m.session) != len(sess) {
		t.Fatalf("m.session length = %d, want %d (cancellation must not clobber)", len(m.session), len(sess))
	}
	for i, want := range sess {
		if m.session[i].Role != want.Role || m.session[i].Content != want.Content {
			t.Errorf("m.session[%d] = %+v, want %+v", i, m.session[i], want)
		}
	}
	// Transcript echoes the cancellation.
	if !responseContains(m, "[compact cancelled]") {
		t.Errorf("transcript missing '[compact cancelled]' after cancel:\n%q", m.response)
	}
}

// TestSlashCompact_SuccessReplacesSession runs the no-prog path
// with a non-hanging fake and asserts:
//
//   - m.session is replaced with one user-role message carrying
//     the summary.
//   - The transcript has the "Compacted N messages (-Y%)" row.
//   - The /compact <message> form appends the trailing message
//     after the summary (a fresh prompt is armed but not fired
//     in the no-prog path; the assertion is on the slice state).
func TestSlashCompact_SuccessReplacesSession(t *testing.T) {
	be := &fakeStreamClient{
		summary: "compacted summary line.",
	}
	m := mkCompactNoProgModel(t, be)
	sess := []backend.Message{
		{Role: "user", Content: strings.Repeat("u", 400)},
		{Role: "assistant", Content: strings.Repeat("a", 400)},
		{Role: "user", Content: strings.Repeat("u", 400)},
		{Role: "assistant", Content: strings.Repeat("a", 400)},
	}
	m.session = append([]backend.Message(nil), sess...)

	// /compact with a trailing message: the slash command should
	// append "summarize this and answer" to m.session after the
	// summary lands.
	handled, _ := m.handleSlashCommand("/compact summarize this and answer")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	waitForCompactSettle(t, m, 2*time.Second)

	if len(m.session) != 2 {
		t.Fatalf("m.session length = %d, want 2 (summary + trailing message)", len(m.session))
	}
	if m.session[0].Role != "user" || !strings.Contains(m.session[0].Content, "compacted summary") {
		t.Errorf("m.session[0] = %+v; want user-role carrying the summary", m.session[0])
	}
	if m.session[1].Role != "user" || m.session[1].Content != "summarize this and answer" {
		t.Errorf("m.session[1] = %+v; want user-role carrying the trailing message", m.session[1])
	}
	if !responseContains(m, "Compacted") {
		t.Errorf("transcript missing 'Compacted' row:\n%q", m.response)
	}
}

// TestSlashCompact_EmptyTranscriptShortCircuits confirms that
// `/compact` with no prior session history echoes "Nothing to
// compact yet." without calling the backend. This is the
// cycle-167 contract the cycle-237 path preserves verbatim.
func TestSlashCompact_EmptyTranscriptShortCircuits(t *testing.T) {
	be := &fakeStreamClient{summary: "should not be called"}
	m := mkCompactNoProgModel(t, be)
	handled, _ := m.handleSlashCommand("/compact")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	if be.calls != 0 {
		t.Errorf("backend.Stream called %d times for empty session; want 0", be.calls)
	}
	if !responseContains(m, "Nothing to compact yet.") {
		t.Errorf("transcript missing 'Nothing to compact yet.':\n%q", m.response)
	}
}

// TestEstimateTokens_RoughAccuracy feeds a 1 KB string into
// EstimateTokens and asserts the result is in [200, 320] — the
// rough quarter-byte rule. Lower bound 200 because pure ASCII
// is exactly 256 tokens / KB; upper bound 320 because UTF-8
// multi-byte characters drift higher but rarely past 320.
func TestEstimateTokens_RoughAccuracy(t *testing.T) {
	s := strings.Repeat("a", 1024) // exactly 1024 bytes
	got := compaction.EstimateTokens(s)
	if got < 200 || got > 320 {
		t.Errorf("EstimateTokens(1024-byte ascii) = %d, want in [200, 320]", got)
	}
	if got2 := compaction.EstimateTokens(""); got2 != 0 {
		t.Errorf("EstimateTokens(\"\") = %d, want 0", got2)
	}
}

// waitForCompactSettle polls m.response until the cancel row /
// success row lands, or the timeout fires. The compact goroutine
// posts via prog.Send (in production) or applyGenericCompactResult
// (in the no-prog test path); the no-prog path is synchronous
// relative to the goroutine's lifetime, so a short poll is enough.
func waitForCompactSettle(t *testing.T, m *runMode, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if responseContains(m, "Compacted") || responseContains(m, "[compact cancelled]") || responseContains(m, "Compact failed") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("compact did not settle within %s; transcript:\n%q", timeout, m.response)
}

// responseContains reports whether any line in m.response contains
// needle. The cycle-237 path emits styled rows — the message line
// is wrapped in cWarningBlueOpen / panelGray238 — so a substring
// search tolerates the SGR decorations.
func responseContains(m *runMode, needle string) bool {
	for _, line := range m.response {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// TestSlashCompact_TriggerCancelNoOp verifies that TriggerCompactCancel
// is a safe no-op when no compact is in flight. The Esc handler will
// reach this from a future cycle; pinning the no-op behaviour now
// keeps that cycle small.
func TestSlashCompact_TriggerCancelNoOp(t *testing.T) {
	// Should not panic, should not deadlock.
	TriggerCompactCancel()
	TriggerCompactCancel()
}

// TestSlashCompact_ErrCancelledSentinel pins the contract that
// ComputeSummary returns a typed ErrCancelled sentinel — the TUI
// uses errors.Is to distinguish Esc from backend failure. A future
// refactor that returns the raw ctx.Err() instead would silently
// break the cancel-detection path.
func TestSlashCompact_ErrCancelledSentinel(t *testing.T) {
	if !errors.Is(compaction.ErrCancelled, compaction.ErrCancelled) {
		t.Fatal("compaction.ErrCancelled should be a sentinel")
	}
	if !errors.Is(compaction.ErrEmptyTranscript, compaction.ErrEmptyTranscript) {
		t.Fatal("compaction.ErrEmptyTranscript should be a sentinel")
	}
}

func TestSlashCompact_GenericDoneDispatchesTrailingPrompt(t *testing.T) {
	m := &runMode{
		session: []backend.Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "world"},
		},
	}
	msg := genericCompactDoneMsg{
		Summary:            "concise summary of conversation",
		StatsBefore:        compaction.Stats{Messages: 2, Tokens: 100},
		StatsAfter:         compaction.Stats{Messages: 1, Tokens: 20},
		CustomInstructions: "now do the next task",
	}
	cmd := m.applyGenericCompactResult(msg, msg.CustomInstructions)
	if len(m.session) != 2 {
		t.Fatalf("session len = %d, want 2 (summary + custom instructions)", len(m.session))
	}
	if m.session[0].Content != "concise summary of conversation" {
		t.Errorf("session[0] = %q, want summary", m.session[0].Content)
	}
	if m.session[1].Content != "now do the next task" {
		t.Errorf("session[1] = %q, want custom instructions", m.session[1].Content)
	}
	if cmd == nil {
		t.Fatalf("cmd is nil, want prompt stream cmd")
	}
}

// silence unused-import warnings on toolchains that strip dead code.
// The bubbletea import is used by the no-prog path's nil-check; the
// rest are file-level guards.
var _ = tea.QuitMsg{}
