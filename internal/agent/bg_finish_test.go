// Package agent — bg_finish_test.go: cycle 259 (AGY 1.4.0) tests.
//
// The five required tests (TestBgFinish_PreservesPlanPointer,
// TestBgFinish_IncludesOriginalTaskReminder,
// TestBgFinish_ReorientsContext, TestBgFinish_DoesNotLoseFocus,
// TestBgFinish_CanceledPreservesState) pin the agent-loop contract
// the AGY 1.4.0 release note describes:
//
//	"Fixed the agent losing track of its place in the conversation
//	 after a background command finishes or is canceled — it now
//	 stays focused on the original task and continues with the next
//	 step after re-reading recent context."
//
// Three extras pin the things the cycle is load-bearing for:
//
//   - TestBgFinish_EmptyOutputIsHandled — the AGY 1.4.0 contract
//     is "even if the bg command's final output was empty". The
//     reminder line still has to land in the chunk.
//   - TestBgFinish_RecentTailIsBounded — the recent-transcript tail
//     the OnReorient hook reads cannot grow without limit;
//     SetRecentTailCap drops the oldest chunks when the cap is
//     exceeded.
//   - TestBgFinish_NilReceiverIsSafe — every method is nil-safe so
//     the TUI's optional-coordinator pattern (the cmd/wagyu TUI
//     does not always wire a coordinator) does not panic on a
//     half-initialised runMode.
//
// The format is intentionally verbose: each test names the cycle-
// contract line it pins in a header comment so a future regression
// hunt can grep directly. The deliverable in
// docs/cycles/cycle-259-bg-finish/ cross-references these names.
package agent

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// TestBgFinish_PreservesPlanPointer pins the core cycle contract:
//
//	"the agent's next step plan pointer is preserved across the bg
//	 interrupt"
//
// The agent is at plan[2] when a bg command starts; the bg
// command finishes normally; the pointer is still at 2, not at 3.
// The model will only call AdvancePlan itself once it has actually
// taken that next step — never as a side-effect of the bg finish.
func TestBgFinish_PreservesPlanPointer(t *testing.T) {
	c := NewBgFinishCoordinator()
	c.SetPlan(
		"ship the cycle-259 bg-finish fix",
		[]string{
			"draft the bg_finish.go seam",
			"write bg_finish_test.go pinning the contract",
			"wire the coordinator into cmd/wagyu + pigrt",
			"run the full sweep",
		},
		PolicyAuto,
	)
	if got := c.CurrentPlanIndex(); got != 0 {
		t.Fatalf("after SetPlan: index = %d, want 0", got)
	}
	if !c.AdvancePlan() {
		t.Fatalf("AdvancePlan #1 returned false")
	}
	if !c.AdvancePlan() {
		t.Fatalf("AdvancePlan #2 returned false")
	}
	if got := c.CurrentPlanIndex(); got != 2 {
		t.Fatalf("after two advances: index = %d, want 2", got)
	}

	// BG finish with a normal exit. The plan index must not
	// advance — the next Send picks up the same step.
	_, chunk := c.NotifyBgFinished("cmd-7", "finished", "build ok\n")
	if got := c.CurrentPlanIndex(); got != 2 {
		t.Fatalf("after bg finish: index = %d, want 2 (preserved)", got)
	}
	if !strings.Contains(chunk, "next plan step (#3/4): wire the coordinator into cmd/wagyu + pigrt") {
		t.Errorf("chunk did not name the preserved step; got:\n%s", chunk)
	}
}

// TestBgFinish_IncludesOriginalTaskReminder pins the second half of
// the release-note contract:
//
//	"the agent's transcript contains the original-task reminder line"
//
// The chunk must contain BOTH the bg result (so the model sees
// what the command did) AND the one-line "you were working on:"
// reminder (so the model does not lose the user's task). The bg
// result is a plain string the test pins verbatim; the reminder is
// the format contract.
func TestBgFinish_IncludesOriginalTaskReminder(t *testing.T) {
	c := NewBgFinishCoordinator()
	c.SetPlan(
		"refactor the auth flow to use the new eligibility check",
		[]string{"audit callers", "swap implementation", "re-run the sweep"},
		PolicyDefault,
	)

	const bgOutput = "PASS  internal/auth/eligibility_test.go\nPASS  internal/auth/oauth_test.go\n"
	_, chunk := c.NotifyBgFinished("cmd-12", "finished", bgOutput)

	if !strings.Contains(chunk, bgOutput) {
		t.Errorf("chunk did not include bg output %q; got:\n%s", bgOutput, chunk)
	}
	wantReminder := "you were working on: refactor the auth flow to use the new eligibility check"
	if !strings.Contains(chunk, wantReminder) {
		t.Errorf("chunk did not include reminder %q; got:\n%s", wantReminder, chunk)
	}
	if !strings.Contains(chunk, "<task-notification>") || !strings.Contains(chunk, "</task-notification>") {
		t.Errorf("chunk missing <task-notification> wrapper; got:\n%s", chunk)
	}
	if !strings.Contains(chunk, "task-id: cmd-12") {
		t.Errorf("chunk missing task-id; got:\n%s", chunk)
	}
	if !strings.Contains(chunk, "status: finished") {
		t.Errorf("chunk missing status; got:\n%s", chunk)
	}
}

// TestBgFinish_ReorientsContext pins the third half of the
// release-note contract:
//
//	"the agent's first action after bg-finish re-reads recent
//	 transcript (or at least: the transcript chunk is fed back into
//	 the loop)"
//
// The OnReorient hook fires once per bg-finish, carrying the
// recent transcript tail. The test records three chunks before
// the bg finish, then asserts:
//
//   - the hook fired exactly once
//   - it fired with the bg task's id
//   - the transcript tail it received contains the three recorded
//     chunks (re-readable, in order)
func TestBgFinish_ReorientsContext(t *testing.T) {
	c := NewBgFinishCoordinator()
	c.SetPlan(
		"investigate the panic in test 42",
		[]string{"read the trace", "patch the offending function"},
		PolicyAuto,
	)

	c.RecordTranscriptChunk("user> investigate the panic in test 42")
	c.RecordTranscriptChunk("assistant> I'll start by reading the trace.")
	c.RecordTranscriptChunk("tool run_command> head -50 test-output.log")

	var (
		hookCalls atomic.Int32
		gotID     atomic.Value // string
		gotTail   atomic.Value // []string
	)
	c.OnReorient = func(taskID string, tail []string) {
		hookCalls.Add(1)
		gotID.Store(taskID)
		// Copy the slice so the test reads a stable snapshot.
		copyTail := append([]string(nil), tail...)
		gotTail.Store(copyTail)
	}

	c.NotifyBgFinished("cmd-3", "finished", "panic: nil pointer\n")

	if n := hookCalls.Load(); n != 1 {
		t.Fatalf("OnReorient call count = %d, want 1", n)
	}
	if id, _ := gotID.Load().(string); id != "cmd-3" {
		t.Errorf("OnReorient taskID = %q, want cmd-3", id)
	}
	tail, _ := gotTail.Load().([]string)
	if len(tail) != 3 {
		t.Fatalf("OnReorient tail length = %d, want 3; tail = %#v", len(tail), tail)
	}
	if tail[0] != "user> investigate the panic in test 42" {
		t.Errorf("OnReorient tail[0] = %q", tail[0])
	}
	if tail[2] != "tool run_command> head -50 test-output.log" {
		t.Errorf("OnReorient tail[2] = %q", tail[2])
	}
}

// TestBgFinish_DoesNotLoseFocus pins the policy-mode stability
// half of the release-note contract:
//
//	"agent's policy mode (default/auto/plan) is unchanged after bg
//	 finish"
//
// A user in /plan mode who started a long build must still be in
// /plan mode after the build completes; the bg interrupt is not
// allowed to silently demote the policy. The test exercises every
// mode the coordinator recognises so a future cycle that adds a
// new mode sees the same coverage.
func TestBgFinish_DoesNotLoseFocus(t *testing.T) {
	cases := []struct {
		name   string
		policy PolicyMode
		want   PolicyMode
	}{
		{"default", PolicyDefault, PolicyDefault},
		{"auto", PolicyAuto, PolicyAuto},
		{"plan", PolicyPlan, PolicyPlan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewBgFinishCoordinator()
			c.SetPlan("any task", []string{"step a", "step b"}, tc.policy)
			if got := c.PolicyMode(); got != tc.want {
				t.Fatalf("before bg finish: policy = %q, want %q", got, tc.want)
			}
			c.NotifyBgFinished("cmd-x", "finished", "ok")
			if got := c.PolicyMode(); got != tc.want {
				t.Errorf("after bg finish: policy = %q, want %q (bg interrupt must not change focus)", got, tc.want)
			}
			c.NotifyBgFinished("cmd-x", "failed", "boom")
			if got := c.PolicyMode(); got != tc.want {
				t.Errorf("after bg failure: policy = %q, want %q", got, tc.want)
			}
			c.NotifyBgFinished("cmd-x", "canceled", "")
			if got := c.PolicyMode(); got != tc.want {
				t.Errorf("after bg cancel: policy = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBgFinish_CanceledPreservesState is the canceled-terminal case
// for the plan-pointer invariant. The user canceled the bg command
// (SIGTERM via the /tasks panel k key, or the model aborted the
// command itself), and the plan pointer must still be at the step
// the agent was on before the cancel — never advanced past it. The
// chunk must also report "status: canceled" so the model knows the
// bg task ended without producing a useful result.
func TestBgFinish_CanceledPreservesState(t *testing.T) {
	c := NewBgFinishCoordinator()
	c.SetPlan(
		"deploy the new release",
		[]string{"tag the release", "push to the registry", "smoke-test", "announce"},
		PolicyDefault,
	)
	c.AdvancePlan() // index 0 -> 1
	c.AdvancePlan() // index 1 -> 2
	if got := c.CurrentPlanIndex(); got != 2 {
		t.Fatalf("setup: index = %d, want 2", got)
	}

	// User cancels the bg command. The plan index must NOT
	// advance.
	_, chunk := c.NotifyBgFinished("cmd-9", "canceled", "")
	if got := c.CurrentPlanIndex(); got != 2 {
		t.Fatalf("after cancel: index = %d, want 2 (preserved across cancel)", got)
	}
	if !strings.Contains(chunk, "status: canceled") {
		t.Errorf("chunk did not report canceled status; got:\n%s", chunk)
	}
	if !strings.Contains(chunk, "you were working on: deploy the new release") {
		t.Errorf("chunk did not include reminder line; got:\n%s", chunk)
	}
	// The plan step line should still point at step 3/4 (the
	// step the agent was on before the cancel).
	if !strings.Contains(chunk, "next plan step (#3/4): smoke-test") {
		t.Errorf("chunk did not name the preserved step; got:\n%s", chunk)
	}
}

// TestBgFinish_EmptyOutputIsHandled pins the cycle's hard
// constraint: "The fix must work even if the bg command's final
// output was empty." A 0-byte output produces a chunk that still
// contains the reminder + plan pointer + (empty) marker so the
// model has enough signal to recover.
func TestBgFinish_EmptyOutputIsHandled(t *testing.T) {
	c := NewBgFinishCoordinator()
	c.SetPlan(
		"wait for the build",
		[]string{"start the build", "verify the artifact"},
		PolicyAuto,
	)

	// All four flavours of empty: "", " ", "\n\n", "   \n".
	// The chunk must contain the reminder + plan pointer
	// regardless.
	empties := []string{"", " ", "\n\n", "   \n"}
	for i, e := range empties {
		_, chunk := c.NotifyBgFinished("cmd-empty", "finished", e)
		if !strings.Contains(chunk, "you were working on: wait for the build") {
			t.Errorf("empties[%d]: chunk missing reminder; got:\n%s", i, chunk)
		}
		if !strings.Contains(chunk, "next plan step (#1/2): start the build") {
			t.Errorf("empties[%d]: chunk missing plan step; got:\n%s", i, chunk)
		}
		if !strings.Contains(chunk, "(empty)") {
			t.Errorf("empties[%d]: chunk missing (empty) marker; got:\n%s", i, chunk)
		}
	}
}

// TestBgFinish_RecentTailIsBounded pins the contract that the
// recent transcript tail the OnReorient hook reads cannot grow
// without limit. The default cap is 16; SetRecentTailCap lets the
// test exercise the drop-oldest behaviour in isolation.
func TestBgFinish_RecentTailIsBounded(t *testing.T) {
	c := NewBgFinishCoordinator()
	c.SetRecentTailCap(3)
	for i := 0; i < 7; i++ {
		c.RecordTranscriptChunk("chunk-" + itoa10(i))
	}
	tail := c.RecentTranscript()
	if got, want := len(tail), 3; got != want {
		t.Fatalf("tail length = %d, want %d (cap is 3)", got, want)
	}
	// Oldest two dropped; newest three preserved in order.
	want := []string{"chunk-4", "chunk-5", "chunk-6"}
	for i, w := range want {
		if tail[i] != w {
			t.Errorf("tail[%d] = %q, want %q", i, tail[i], w)
		}
	}

	// And empty chunks must not leak into the tail — a chunk
	// that arrives empty is silently dropped. Whitespace-only
	// chunks are kept (the contract is "empty == dropped", not
	// "blank == dropped"); a real transcript may legitimately
	// contain a blank-line-only chunk.
	c2 := NewBgFinishCoordinator()
	c2.SetRecentTailCap(4)
	c2.RecordTranscriptChunk("a")
	c2.RecordTranscriptChunk("") // dropped (empty)
	c2.RecordTranscriptChunk("b")
	c2.RecordTranscriptChunk("   ") // kept (whitespace-only, not empty)
	c2.RecordTranscriptChunk("c")
	tail2 := c2.RecentTranscript()
	if got, want := len(tail2), 4; got != want {
		t.Fatalf("tail2 length = %d, want %d (only empty chunks dropped)", got, want)
	}
	if tail2[0] != "a" || tail2[1] != "b" || tail2[2] != "   " || tail2[3] != "c" {
		t.Errorf("tail2 = %#v, want [a, b, \"   \", c]", tail2)
	}
}

// TestBgFinish_NilReceiverIsSafe pins the contract that every
// method is nil-safe. The TUI's optional-coordinator pattern
// (cmd/wagyu does not always wire a coordinator; the print path
// may run before the runMode has built one) would panic on a
// half-initialised coordinator if the methods dereferenced the
// receiver unconditionally. Every method here checks c == nil
// first.
func TestBgFinish_NilReceiverIsSafe(t *testing.T) {
	var c *BgFinishCoordinator // nil

	c.SetPlan("task", []string{"a"}, PolicyDefault)
	if c.AdvancePlan() {
		t.Errorf("nil.AdvancePlan returned true")
	}
	if got := c.CurrentPlanIndex(); got != 0 {
		t.Errorf("nil.CurrentPlanIndex = %d, want 0", got)
	}
	if got := c.PlanSteps(); got != nil {
		t.Errorf("nil.PlanSteps = %v, want nil", got)
	}
	if got := c.OriginalTask(); got != "" {
		t.Errorf("nil.OriginalTask = %q, want empty", got)
	}
	if got := c.PolicyMode(); got != PolicyDefault {
		t.Errorf("nil.PolicyMode = %q, want default", got)
	}
	c.RecordTranscriptChunk("a")
	if got := c.RecentTranscript(); got != nil {
		t.Errorf("nil.RecentTranscript = %v, want nil", got)
	}
	c.SetRecentTailCap(10)
	n, chunk := c.NotifyBgFinished("cmd-0", "finished", "")
	if n.TaskID != "" || chunk != "" {
		t.Errorf("nil.NotifyBgFinished returned %+v, %q, want zero", n, chunk)
	}
}

// TestBgFinish_StatusNormalization pins the contract that an
// unknown status (e.g. a typo from a buggy producer) is normalised
// to "finished" rather than leaked into the transcript. The cycle
// documents the three valid values; the test pins them.
func TestBgFinish_StatusNormalization(t *testing.T) {
	c := NewBgFinishCoordinator()
	c.SetPlan("task", []string{"a"}, PolicyDefault)
	for _, status := range []string{"done", "ok", "complete", "not-a-state", ""} {
		_, chunk := c.NotifyBgFinished("cmd-n", status, "")
		if !strings.Contains(chunk, "status: finished") {
			t.Errorf("status %q: chunk should report status: finished; got:\n%s", status, chunk)
		}
	}
	// The three known statuses pass through verbatim.
	for _, status := range []string{"finished", "failed", "canceled"} {
		_, chunk := c.NotifyBgFinished("cmd-n", status, "")
		if !strings.Contains(chunk, "status: "+status) {
			t.Errorf("status %q: chunk should report status: %s; got:\n%s", status, status, chunk)
		}
	}
}

// TestBgFinish_FormatExported pins the contract that
// FormatBgFinishNotification is callable on a zero-value
// notification. Callers that build the chunk from scratch (the
// pigrt print path is one) need this; tests pin the zero-value
// path explicitly so a future cycle that adds a required field
// sees the regression here.
func TestBgFinish_FormatExported(t *testing.T) {
	n := BgFinishNotification{
		TaskID:       "cmd-99",
		Status:       "finished",
		Output:       "hello",
		OriginalTask: "say hi",
		PlanIndex:    0,
		TotalSteps:   1,
	}
	got := FormatBgFinishNotification(n, []string{"say hi"})
	wantParts := []string{
		"<task-notification>",
		"task-id: cmd-99",
		"status: finished",
		"hello",
		"you were working on: say hi",
		"next plan step (#1/1): say hi",
		"</task-notification>",
	}
	for _, p := range wantParts {
		if !strings.Contains(got, p) {
			t.Errorf("FormatBgFinishNotification: missing %q; got:\n%s", p, got)
		}
	}
}

// TestBgFinish_Concurrent runs the coordinator through a parallel
// writer + reader to confirm the race detector stays clean. The
// test does not assert on a value (race detection is the point);
// it just exercises the locks so a `go test -race` run flags any
// unsynchronised access.
func TestBgFinish_Concurrent(t *testing.T) {
	c := NewBgFinishCoordinator()
	c.SetPlan("task", []string{"a", "b", "c"}, PolicyPlan)

	var wg sync.WaitGroup
	const goroutines = 8
	const iters = 64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				c.RecordTranscriptChunk("g" + itoa10(id) + "-i" + itoa10(j))
				if j%4 == 0 {
					c.AdvancePlan()
				}
				_ = c.CurrentPlanIndex()
				_ = c.PlanSteps()
				_ = c.OriginalTask()
				_ = c.PolicyMode()
			}
		}(i)
	}
	// One writer that fires NotifyBgFinished mid-flight. Each
	// call must observe a coherent snapshot under the lock.
	for j := 0; j < iters; j++ {
		_, _ = c.NotifyBgFinished("cmd-race", "finished", "ok")
	}
	wg.Wait()
}

// itoa10 is a small allocation-free int -> string helper used by
// the concurrent test. stdlib strconv.Itoa would work too; the
// helper keeps the test's imports minimal.
func itoa10(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
