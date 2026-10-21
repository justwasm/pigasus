// auto_continue_test.go — cycle auto-continue-interrupt unit tests for
// the runtime side. The hard integration is exercised by
// internal/pigrt/runtime_test.go (which already drives a real PiG
// session); here we pin the helpers in isolation so a regression in the
// classifier/parser lands as one explicit failure mode, not a sweep of
// flaky session races.
//
// Tests live in `package pigrt` (not `pigrt_test`) so they reach the
// internal classifyInterruption/iterationCountFromErr/appendRecoveryMessage
// helpers directly — no exported-for-test dance.
package pigrt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
)

// TestClassifyInterruptionMatrix pins the cycle's trigger matrix:
//
//	ctx-cancel | nil err              → ""
//	ErrMaxTurnsReached                  → Status banner
//	last.Assistant.StopReason Error/Aborted/Length → stream interrupted
//	last.Assistant.StopReason Stop/ToolUse         → ""
func TestClassifyInterruptionMatrix(t *testing.T) {
	cases := []struct {
		name string
		msgs []agent.AgentMessage
		err  error
		want string
	}{
		{"nil err no assistant",
			nil, nil, "",
		},
		{"ErrMaxTurnsReached yields status",
			nil,
			fmt.Errorf("%w (%d turns)", agent.ErrMaxTurnsReached, 9),
			fmt.Sprintf(AutoContIterationCapFormat, 9),
		},
		{"StopReasonError stream interrupted",
			[]agent.AgentMessage{
				{User: &agent.UserMessage{Role: "user", Content: ai.UserText("hi")}},
				{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonError}},
			},
			nil,
			AutoContStreamInterrupted,
		},
		{"StopReasonAborted stream interrupted",
			[]agent.AgentMessage{
				{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonAborted}},
			},
			nil,
			AutoContStreamInterrupted,
		},
		{"StopReasonLength stream interrupted",
			[]agent.AgentMessage{
				{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonLength}},
			},
			nil,
			AutoContStreamInterrupted,
		},
		{"StopReasonStop no recovery",
			[]agent.AgentMessage{
				{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonStop}},
			},
			nil,
			"",
		},
		{"StopReasonToolUse no recovery",
			[]agent.AgentMessage{
				{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonToolUse}},
			},
			nil,
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyInterruption(tc.msgs, tc.err)
			if got != tc.want {
				t.Errorf("classifyInterruption() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestIterationCountFromErr pins the parser used in **Status** banner
// formatting. Returning 0 (best-effort) is acceptable for the
// recovery-copy path; the depth counter is the real bound on
// recursion. The cycles observed are the wrap PiG emits (pigrt-style
// "%w (%d turns)"), the clone-style wrap, and a bare integer.
func TestIterationCountFromErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"empty", errors.New(""), 0},
		{"pigrt format", fmt.Errorf("%w (%d turns)", agent.ErrMaxTurnsReached, 12), 12},
		{"pigrt zero", fmt.Errorf("%w (%d turns)", agent.ErrMaxTurnsReached, 0), 0},
		{"plain integer", errors.New("exceeded at 21 iterations"), 21},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := iterationCountFromErr(tc.err); got != tc.want {
				t.Errorf("iterationCountFromErr(%q) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestAppendRecoveryMessageKeepsTailUser pins the synthetic user
// recovery copy lands at the tail of the agent transcript — PiG
// rejects an assistant tail from Continue(), and the recovery copy is
// what makes Continue() re-enter the loop.
func TestAppendRecoveryMessageKeepsTailUser(t *testing.T) {
	msgs := []agent.AgentMessage{
		{User: &agent.UserMessage{Role: "user", Content: ai.UserText("hi")}},
		{Assistant: &agent.AssistantMessage{
			StopReason:   ai.StopReasonError,
			ErrorMessage: "stream chopped",
		}},
	}
	out := appendRecoveryMessage(msgs, AutoContStreamInterrupted)
	if len(out) != len(msgs)+1 {
		t.Fatalf("len(out) = %d, want %d", len(out), len(msgs)+1)
	}
	last := out[len(out)-1]
	if last.User == nil {
		t.Fatalf("tail message is not user: %+v", last)
	}
	if last.User.Role != "user" {
		t.Errorf("tail user role = %q, want %q", last.User.Role, "user")
	}
	if got, ok := last.User.Content.(ai.UserText); !ok || string(got) != AutoContStreamInterrupted {
		t.Errorf("tail user content = %v, want %q", last.User.Content, AutoContStreamInterrupted)
	}
	// The pre-existing assistant message must be preserved verbatim in
	// the new slice so PiG's Continue() sees the same tail the previous
	// turn produced.
	if prev := out[len(out)-2].Assistant; prev == nil || prev.ErrorMessage != "stream chopped" {
		t.Errorf("assistant message drifted: %+v", out[len(out)-2])
	}
}

// TestErrAutoContMaxDepthUnwrap pins the api_error.go contract: the
// give-up error keeps the underlying pre-stream failure so the
// AGY_ERROR envelope still classifies the typed *backend.HTTPError.
func TestErrAutoContMaxDepthUnwrap(t *testing.T) {
	base := errors.New("upstream is down")
	wrapped := &ErrAutoContMaxDepth{Depth: AutoContMaxDepth, Last: base}
	if !errors.Is(wrapped, base) {
		t.Errorf("errors.Is(ErrAutoContMaxDepth, base) = false; chain must preserve the original")
	}
	if !strings.Contains(wrapped.Error(), "pigrt: auto-continue reached max depth (3)") {
		t.Errorf("Error() = %q, missing the pigrt prefix", wrapped.Error())
	}
}

// TestAutoContConstantsAreAGYVerbatim pins the bytes of the recovery
// copy against the AGY verbatim strings (docs/research/agy-feature-
// strings.md §3, loadfile 0x33dc41b / 0x33a3d44). A future cycle that
// wants to do a real byte-match against the binary only needs to read
// the constants — the test here is the witness that the strings in
// this repo match that doc verbatim, byte-for-byte, and that nothing
// (whitespace, punctuation, line endings) has drifted since the
// original AGY capture was loaded into seeds.txt.
func TestAutoContConstantsAreAGYVerbatim(t *testing.T) {
	if got := AutoContStreamInterrupted; got != "The stream was interrupted. Please continue the task you were working on." {
		t.Errorf("AutoContStreamInterrupted drifted: %q", got)
	}
	if got := AutoContIterationCapFormat; got != "**Status**: Cancelled after %d iterations" {
		t.Errorf("AutoContIterationCapFormat drifted: %q", got)
	}
	if AutoContMaxDepth != 3 {
		t.Errorf("AutoContMaxDepth = %d, want 3", AutoContMaxDepth)
	}
}

// TestMsgSliceDoesNotAliasInput pins the snapshot helper used by
// runAutoContCycle: the slice header in msgSlice's return must not
// back to the caller's backing array, or a subsequent SetMessages
// would mutate input through it. Pointer-shared fields (User /
// Assistant / ToolResult) are deep-cloned by PiG's SetMessages
// internally; msgSlice only protects the slice storage itself.
func TestMsgSliceDoesNotAliasInput(t *testing.T) {
	in := []agent.AgentMessage{
		{User: &agent.UserMessage{Role: "user", Content: ai.UserText("a")}},
	}
	out := msgSlice(in)
	if &out[0] == &in[0] {
		t.Errorf("msgSlice aliased the caller's slice backing array")
	}
	// Replace the (shallow) AgentMessage value in the snapshot and
	// confirm the input's slice is unchanged.
	out[0] = agent.AgentMessage{
		User: &agent.UserMessage{Role: "user", Content: ai.UserText("replaced")},
	}
	if got, ok := in[0].User.Content.(ai.UserText); !ok || string(got) != "a" {
		t.Errorf("replacing the snapshot slot mutated the caller's slice: %v", in[0].User.Content)
	}
}

// TestQuotaErrorShortCircuitsPrint pins the changelog 1.3.3 rule
// "reaching your individual quota is reported right away instead of being
// retried" for the print path. A quota-classified error must short-circuit
// maybeAutoContinue on the first iteration: no recovery copy is appended
// (no runAutoContCycle is invoked), the typed *backend.HTTPError is preserved
// so cmd/wagyu/api_error.go still classifies it as RESOURCE_EXHAUSTED, and
// the function returns at depth=0 — never reaching the depth==AutoContMaxDepth
// give-up branch.
//
// We test the no-Continue reachability by constructing a minimal Runtime
// whose session/agent pointers are nil; runAutoContCycle bails out on nil
// session with a fresh error, so a test that recursed would surface that
// error, not the original quota one. The "we returned the quota error
// unchanged" assertion catches that.
func TestQuotaErrorShortCircuitsPrint(t *testing.T) {
	quotaErr := &backend.HTTPError{
		StatusCode: 429,
		Body:       []byte(`{"error":{"message":"Individual quota reached."}}`),
	}
	r := &Runtime{}
	msgs := []agent.AgentMessage{
		{User: &agent.UserMessage{Role: "user", Content: ai.UserText("hi")}},
	}
	gotMsgs, gotErr := r.maybeAutoContinue(context.Background(), msgs, quotaErr, 0)
	if gotErr == nil {
		t.Fatal("maybeAutoContinue returned nil error; want the quota error")
	}
	// Typed HTTPError must survive so api_error.go's errors.As path
	// keeps classifying it as RESOURCE_EXHAUSTED.
	var he *backend.HTTPError
	if !errors.As(gotErr, &he) {
		t.Fatalf("quota error did not preserve *backend.HTTPError chain: %v", gotErr)
	}
	if he.StatusCode != 429 {
		t.Errorf("HTTPError.StatusCode = %d, want 429", he.StatusCode)
	}
	// must NOT wrap in ErrAutoContMaxDepth — that would imply the
	// recovery loop ran all the way through, which is exactly the
	// regression the changelog closes.
	var giveUp *ErrAutoContMaxDepth
	if errors.As(gotErr, &giveUp) {
		t.Errorf("quota error was wrapped in ErrAutoContMaxDepth; want a bare typed error")
	}
	// The messages slice must be returned unchanged — no synthetic
	// recovery copy was appended.
	if len(gotMsgs) != len(msgs) {
		t.Errorf("len(msgs) drifted: got %d, want %d (no recovery copy should have been appended)", len(gotMsgs), len(msgs))
	}
}

// TestMaybeAutoContinueNonQuotaStreams verifies the negative case: a
// 502-class gateway failure (the canonical "stream interrupted" trigger)
// still takes the recovery path; the quota short-circuit must not swallow
// it. Without this test a buggy quota predicate (e.g. one that matches on
// status only) would hide the real stream-recovery flow.
func TestMaybeAutoContinueNonQuotaStreams(t *testing.T) {
	nonQuota := &backend.HTTPError{
		StatusCode: 502,
		Body:       []byte("bad gateway"),
	}
	r := &Runtime{}
	msgs := []agent.AgentMessage{
		{Assistant: &agent.AssistantMessage{StopReason: ai.StopReasonError}},
	}
	gotMsgs, gotErr := r.maybeAutoContinue(context.Background(), msgs, nonQuota, 0)
	// The recovery path runs runAutoContCycle which needs a live
	// session; ours is nil, so the helper returns an explicit
	// "no live session" error rather than the quota short-circuit
	// path. The point of this test is that we did NOT see the
	// quota short-circuit (the original 502 error was consumed
	// somewhere along the path), so any non-nil error here other
	// than the original 502 is acceptable evidence that the
	// non-quota path was actually taken.
	if gotErr == nil {
		t.Fatal("non-quota 502 must not return nil; the recovery path should run")
	}
	if errors.Is(gotErr, nonQuota) {
		t.Errorf("non-quota 502 was returned unchanged; the recovery path did not run")
	}
	// Whatever the recovery path did, the typed 502 should NOT be in
	// the result: it was either consumed by runAutoContCycle or
	// replaced by the no-session sentinel.
	var he *backend.HTTPError
	if errors.As(gotErr, &he) && he.StatusCode == 502 {
		t.Errorf("non-quota 502 should have been consumed by the recovery path")
	}
	_ = gotMsgs // recovery path may or may not allocate a new slice; not asserting
}
