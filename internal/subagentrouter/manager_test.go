package subagentrouter

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeSender counts how many times its Send method was called and
// stores the most recent message. Tests assert on both.
type fakeSender struct {
	mu      sync.Mutex
	calls   int
	lastMsg string
	err     error
	lastCtx context.Context
}

func (f *fakeSender) Send(ctx context.Context, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastMsg = msg
	f.lastCtx = ctx
	return f.err
}

func (f *fakeSender) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeSender) LastMsg() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastMsg
}

func TestRegisterAndGet(t *testing.T) {
	m := NewManager()
	fs := &fakeSender{}
	e, err := m.Register("explore-bot", fs)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if e == nil {
		t.Fatal("Register returned nil entry")
	}
	if e.State != StateRunning {
		t.Errorf("new entry state = %q, want running", e.State)
	}
	if e.Started.IsZero() {
		t.Errorf("new entry Started not stamped")
	}

	got, ok := m.Get("explore-bot")
	if !ok {
		t.Fatal("Get(explore-bot) returned !ok")
	}
	if got != e {
		t.Errorf("Get returned different pointer than Register")
	}
}

func TestRegisterRejectsEmptyName(t *testing.T) {
	m := NewManager()
	if _, err := m.Register("", &fakeSender{}); !errors.Is(err, ErrEmptyName) {
		t.Errorf("Register(\"\") = %v, want ErrEmptyName", err)
	}
}

func TestRegisterRejectsDuplicate(t *testing.T) {
	m := NewManager()
	fs := &fakeSender{}
	if _, err := m.Register("alice", fs); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	if _, err := m.Register("alice", fs); !errors.Is(err, ErrAlreadyRegistered) {
		t.Errorf("second Register = %v, want ErrAlreadyRegistered", err)
	}
	if m.Count() != 1 {
		t.Errorf("Count after duplicate = %d, want 1", m.Count())
	}
}

func TestRegisterRejectsNilSender(t *testing.T) {
	m := NewManager()
	if _, err := m.Register("alice", nil); err == nil {
		t.Error("Register with nil sender succeeded, want error")
	}
}

func TestUnregister(t *testing.T) {
	m := NewManager()
	fs := &fakeSender{}
	if _, err := m.Register("alice", fs); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := m.Unregister("alice"); err != nil {
		t.Fatalf("Unregister: %v", err)
	}
	if _, ok := m.Get("alice"); ok {
		t.Errorf("entry still in registry after Unregister")
	}
	if err := m.Unregister("alice"); !errors.Is(err, ErrUnknownName) {
		t.Errorf("Unregister of missing entry = %v, want ErrUnknownName", err)
	}
}

func TestSetState(t *testing.T) {
	m := NewManager()
	fs := &fakeSender{}
	e, _ := m.Register("alice", fs)
	if err := m.SetState("alice", StateCompleted); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	if e.State != StateCompleted {
		t.Errorf("Entry.State after SetState = %q, want completed", e.State)
	}
	if err := m.SetState("bob", StateCompleted); !errors.Is(err, ErrUnknownName) {
		t.Errorf("SetState(unknown) = %v, want ErrUnknownName", err)
	}
}

func TestLookupCaseInsensitive(t *testing.T) {
	m := NewManager()
	fs := &fakeSender{}
	mustRegister(t, m, "explore-bot", fs)
	tests := []struct {
		query string
		want  string
	}{
		{"explore-bot", "explore-bot"},
		{"Explore-Bot", "explore-bot"},
		{"EXPLORE-BOT", "explore-bot"},
		{"x", ""},
		{"", ""},
	}
	for _, tc := range tests {
		e, ok := m.Lookup(tc.query)
		if tc.want == "" {
			if ok {
				t.Errorf("Lookup(%q) ok=true, want false (found %q)", tc.query, e.Name)
			}
			continue
		}
		if !ok || e.Name != tc.want {
			t.Errorf("Lookup(%q) = (%q,%v), want (%q, true)", tc.query, nameOrEmpty(e), ok, tc.want)
		}
	}
}

func TestNamesSorted(t *testing.T) {
	m := NewManager()
	for _, n := range []string{"gamma", "alpha", "beta"} {
		mustRegister(t, m, n, &fakeSender{})
	}
	got := m.Names()
	want := []string{"alpha", "beta", "gamma"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("Names() = %v, want %v", got, want)
	}
}

func TestNamesWithPrefix(t *testing.T) {
	m := NewManager()
	for _, n := range []string{"explore-bot", "explorer", "code-reviewer", "general"} {
		mustRegister(t, m, n, &fakeSender{})
	}
	tests := []struct {
		prefix string
		want   []string
	}{
		{"exp", []string{"explore-bot", "explorer"}},
		{"EXP", []string{"explore-bot", "explorer"}},
		{"e", []string{"explore-bot", "explorer"}},
		{"g", []string{"general"}},
		{"x", nil},
		{"", []string{"code-reviewer", "explore-bot", "explorer", "general"}},
	}
	for _, tc := range tests {
		got := m.NamesWithPrefix(tc.prefix)
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("NamesWithPrefix(%q) = %v, want %v", tc.prefix, got, tc.want)
		}
	}
}

func TestListSorted(t *testing.T) {
	m := NewManager()
	fs := &fakeSender{}
	mustRegister(t, m, "zebra", fs)
	mustRegister(t, m, "apple", fs)
	ents := m.List()
	if len(ents) != 2 {
		t.Fatalf("List length = %d, want 2", len(ents))
	}
	if ents[0].Name != "apple" || ents[1].Name != "zebra" {
		t.Errorf("List order = %s, %s, want apple, zebra", ents[0].Name, ents[1].Name)
	}
}

func TestDispatchToKnownName(t *testing.T) {
	m := NewManager()
	fs := &fakeSender{}
	mustRegister(t, m, "explore-bot", fs)
	if err := m.Dispatch(context.Background(), "Explore-Bot", "find the auth code"); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if fs.Calls() != 1 {
		t.Errorf("Sender.calls = %d, want 1", fs.Calls())
	}
	if fs.LastMsg() != "find the auth code" {
		t.Errorf("Sender.lastMsg = %q, want %q", fs.LastMsg(), "find the auth code")
	}
}

func TestDispatchToUnknownName(t *testing.T) {
	m := NewManager()
	err := m.Dispatch(context.Background(), "ghost", "hello")
	if err == nil {
		t.Fatal("Dispatch on unknown name succeeded; want error")
	}
	if !errors.Is(err, ErrUnknownName) {
		t.Errorf("Dispatch error = %v, want wrap ErrUnknownName", err)
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("Dispatch error %q should mention the name", err)
	}
}

func TestDispatchPropagatesSenderError(t *testing.T) {
	m := NewManager()
	fs := &fakeSender{err: errors.New("pipe closed")}
	mustRegister(t, m, "alice", fs)
	err := m.Dispatch(context.Background(), "alice", "hi")
	if err == nil || err.Error() != "pipe closed" {
		t.Errorf("Dispatch returned %v, want pipe-closed propagated", err)
	}
}

func TestDispatchHonoursContextCancellation(t *testing.T) {
	m := NewManager()
	blocking := SenderFunc(func(ctx context.Context, _ string) error {
		<-ctx.Done()
		return ctx.Err()
	})
	mustRegister(t, m, "slow", blocking)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel up-front; Sender should observe it
	if err := m.Dispatch(ctx, "slow", "hi"); !errors.Is(err, context.Canceled) {
		t.Errorf("Dispatch after cancel = %v, want context.Canceled", err)
	}
}

func TestConcurrencySafe(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := []string{"alpha", "beta", "gamma", "delta"}[i%4]
			if _, err := m.Register(nameOf(i), &fakeSender{}); err != nil && !errors.Is(err, ErrAlreadyRegistered) {
				t.Errorf("Register(%s): %v", nameOf(i), err)
			}
			_, _ = m.Lookup(name)
			_ = m.Dispatch(context.Background(), name, "x")
			_ = m.SetState(name, StateRunning)
		}(i)
	}
	wg.Wait()
}

func nameOf(i int) string {
	return []string{"alpha", "beta", "gamma", "delta"}[i%4]
}

func mustRegister(t *testing.T, m *Manager, name string, s Sender) {
	t.Helper()
	if _, err := m.Register(name, s); err != nil && !errors.Is(err, ErrAlreadyRegistered) {
		t.Fatalf("Register(%q): %v", name, err)
	}
}

func nameOrEmpty(e *Entry) string {
	if e == nil {
		return ""
	}
	return e.Name
}

// ─── Stop-reason display (cycle subagent-stopped-clear-on-progress) ─────────
//
// AGY 1.2.13: "Fixed subagent Stop reason being cleared automatically
// by new progress, leaving stale 'Done' labels visible in the UI.
// Subagent Stop reason should remain visible until a NEW Stop event
// replaces it, but progress events while running should never clear
// the displayed Stop reason." These tests pin that contract on the
// two new mutators ReportStop and ReportProgress.

// TestEntryInitialStateHasNoReason: a freshly registered subagent has
// no StopReason — the running state has nothing to display yet. The
// UI's default `Done` label is suppressed until a Stop event lands.
func TestEntryInitialStateHasNoReason(t *testing.T) {
	m := NewManager()
	e, err := m.Register("alice", &fakeSender{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if e.StopReason != "" {
		t.Errorf("fresh entry StopReason = %q, want empty", e.StopReason)
	}
	if e.State != StateRunning {
		t.Errorf("fresh entry State = %q, want %q", e.State, StateRunning)
	}
	got, ok := m.Get("alice")
	if !ok || got.StopReason != "" {
		t.Errorf("Get(alice) StopReason = %q ok=%v, want empty+true", got.StopReason, ok)
	}
}

// TestRunningSubagentShowsNoReason: a subagent whose runtime has
// emitted several Progress events but no Stop event yet must still
// have an empty StopReason. The "no Stop reason" state is the
// running state.
func TestRunningSubagentShowsNoReason(t *testing.T) {
	m := NewManager()
	mustRegister(t, m, "alice", &fakeSender{})

	// A flurry of progress events while the subagent is running.
	for i := 0; i < 25; i++ {
		if err := m.ReportProgress("alice"); err != nil {
			t.Fatalf("ReportProgress[%d]: %v", i, err)
		}
	}

	e, ok := m.Get("alice")
	if !ok {
		t.Fatal("Get(alice) returned !ok")
	}
	if e.StopReason != "" {
		t.Errorf("StopReason after 25 progress events = %q, want empty", e.StopReason)
	}
	if e.State != StateRunning {
		t.Errorf("State = %q, want %q", e.State, StateRunning)
	}
}

// TestReportStopSetsReason: a single Stop event with a non-empty
// reason sets the displayed StopReason. The wiring layer reaches
// this branch when the subagent's runtime emits a terminal event
// (e.g. quota-exhausted, model-capacity).
func TestReportStopSetsReason(t *testing.T) {
	m := NewManager()
	mustRegister(t, m, "alice", &fakeSender{})

	if err := m.ReportStop("alice", "Error: quota exceeded"); err != nil {
		t.Fatalf("ReportStop: %v", err)
	}
	e, _ := m.Get("alice")
	if e.StopReason != "Error: quota exceeded" {
		t.Errorf("StopReason = %q, want %q", e.StopReason, "Error: quota exceeded")
	}
}

// TestReportStopSameReasonIsNoop: re-emitting the same Stop reason
// is a no-op. This is the idempotence half of the invariant; without
// it, a flapping subagent that re-stops with the same reason would
// flicker the UI on every re-emission. The test asserts the field
// is unchanged AND that no spurious side-effect runs (we count
// calls through a guard only the no-op path touches).
func TestReportStopSameReasonIsNoop(t *testing.T) {
	m := NewManager()
	mustRegister(t, m, "alice", &fakeSender{})

	if err := m.ReportStop("alice", "Error: rate limit"); err != nil {
		t.Fatalf("first ReportStop: %v", err)
	}
	e, _ := m.Get("alice")
	if e.StopReason != "Error: rate limit" {
		t.Fatalf("setup: StopReason = %q", e.StopReason)
	}
	started := e.Started

	// Re-emit the same reason five times; the Started timestamp and
	// the StopReason must both be untouched.
	for i := 0; i < 5; i++ {
		if err := m.ReportStop("alice", "Error: rate limit"); err != nil {
			t.Fatalf("re-ReportStop[%d]: %v", i, err)
		}
	}
	after, _ := m.Get("alice")
	if after.StopReason != "Error: rate limit" {
		t.Errorf("StopReason after no-op re-emit = %q, want unchanged", after.StopReason)
	}
	if !after.Started.Equal(started) {
		t.Errorf("Started changed: %v -> %v", started, after.Started)
	}
}

// TestReportStopDifferentReasonReplaces: a NEW Stop event whose
// reason differs from the displayed one REPLACES it. This is the
// second half of the invariant: the displayed reason tracks the
// most recent Stop event, full stop.
func TestReportStopDifferentReasonReplaces(t *testing.T) {
	m := NewManager()
	mustRegister(t, m, "alice", &fakeSender{})

	m.ReportStop("alice", "Error: rate limit")
	m.ReportStop("alice", "Error: quota exceeded")
	e, _ := m.Get("alice")
	if e.StopReason != "Error: quota exceeded" {
		t.Errorf("StopReason after second stop = %q, want %q", e.StopReason, "Error: quota exceeded")
	}

	// And a third stop with yet another reason.
	m.ReportStop("alice", "killed by parent")
	e, _ = m.Get("alice")
	if e.StopReason != "killed by parent" {
		t.Errorf("StopReason after third stop = %q, want %q", e.StopReason, "killed by parent")
	}
}

// TestReportProgressPreservesReason: this is the headline test for
// the AGY 1.2.13 fix. Once a Stop event has set a reason, any
// number of subsequent Progress events leave it in place. Without
// the fix, each progress event would clobber the reason and leave
// a bare `Done` label on the UI.
func TestReportProgressPreservesReason(t *testing.T) {
	m := NewManager()
	mustRegister(t, m, "alice", &fakeSender{})

	// 1. Subagent stops with a reason.
	if err := m.ReportStop("alice", "Error: model capacity"); err != nil {
		t.Fatalf("ReportStop: %v", err)
	}
	// 2. A burst of progress events streams in (the bug scenario).
	for i := 0; i < 50; i++ {
		if err := m.ReportProgress("alice"); err != nil {
			t.Fatalf("ReportProgress[%d]: %v", i, err)
		}
	}
	// 3. The displayed reason must still be the original stop.
	e, _ := m.Get("alice")
	if e.StopReason != "Error: model capacity" {
		t.Errorf("StopReason after 50 progress events = %q, want %q (unchanged)",
			e.StopReason, "Error: model capacity")
	}
}

// TestEmptyProgressDoesNotClear: even the "empty" shape of a progress
// event — calling it with a fresh entry that already has a reason —
// must not clear it. This is a defensive test for callers that
// might pass reason="empty" or "" by accident to a progress-shaped
// API; on the wire the entry method takes no reason at all, so the
// caller cannot accidentally pass a non-empty reason here.
func TestEmptyProgressDoesNotClear(t *testing.T) {
	m := NewManager()
	mustRegister(t, m, "alice", &fakeSender{})

	m.ReportStop("alice", "Error: rate limit")
	// Progress events with no arguments, called repeatedly.
	for i := 0; i < 10; i++ {
		m.ReportProgress("alice")
	}
	e, _ := m.Get("alice")
	if e.StopReason != "Error: rate limit" {
		t.Errorf("StopReason after empty progress = %q, want unchanged %q",
			e.StopReason, "Error: rate limit")
	}
}

// TestReportStopEmptyReasonClears: a NEW Stop event with an empty
// reason is itself a "different reason" by definition and so clears
// the displayed reason. This is the recover-the-UI path for a
// subagent that fully resumes after having stopped — a resume
// emits a fresh Stop with no body.
func TestReportStopEmptyReasonClears(t *testing.T) {
	m := NewManager()
	mustRegister(t, m, "alice", &fakeSender{})

	m.ReportStop("alice", "Error: timeout")
	m.ReportStop("alice", "")
	e, _ := m.Get("alice")
	if e.StopReason != "" {
		t.Errorf("StopReason after empty-reason Stop = %q, want cleared", e.StopReason)
	}

	// And again, calling Stop("") when already empty is a no-op
	// (idempotent on empty reasons).
	if err := m.ReportStop("alice", ""); err != nil {
		t.Fatalf("ReportStop(\"\") when empty: %v", err)
	}
	e, _ = m.Get("alice")
	if e.StopReason != "" {
		t.Errorf("StopReason after idempotent empty Stop = %q, want empty", e.StopReason)
	}
}

// TestReportStopUnknownName: ReportStop returns ErrUnknownName for a
// name not in the registry. This is the same contract SetState
// honours, and it is what a caller can use to distinguish "wrong
// subagent id" from "transient state".
func TestReportStopUnknownName(t *testing.T) {
	m := NewManager()
	if err := m.ReportStop("ghost", "whatever"); !errors.Is(err, ErrUnknownName) {
		t.Errorf("ReportStop(unknown) = %v, want ErrUnknownName", err)
	}
}

// TestReportProgressUnknownName: same contract as ReportStop.
func TestReportProgressUnknownName(t *testing.T) {
	m := NewManager()
	if err := m.ReportProgress("ghost"); !errors.Is(err, ErrUnknownName) {
		t.Errorf("ReportProgress(unknown) = %v, want ErrUnknownName", err)
	}
}

// TestReportStopAndProgressEmptyName: empty-name calls return
// ErrEmptyName, matching the contract SetState/Get/Unregister
// already expose. Belt-and-braces for the new methods.
func TestReportStopAndProgressEmptyName(t *testing.T) {
	m := NewManager()
	if err := m.ReportStop("", "x"); !errors.Is(err, ErrEmptyName) {
		t.Errorf("ReportStop(\"\") = %v, want ErrEmptyName", err)
	}
	if err := m.ReportProgress(""); !errors.Is(err, ErrEmptyName) {
		t.Errorf("ReportProgress(\"\") = %v, want ErrEmptyName", err)
	}
}

// TestReportProgressPreservesReasonThroughStateChange: the AGY
// invariant talks about the *displayed* reason. While the wiring
// may legitimately transition the entry State to completed/failed,
// the StopReason must persist — the UI reads StopReason alongside
// State, so a completed subagent that briefly emitted progress must
// still show the reason on its row.
func TestReportProgressPreservesReasonThroughStateChange(t *testing.T) {
	m := NewManager()
	mustRegister(t, m, "alice", &fakeSender{})

	m.ReportStop("alice", "Error: model capacity")
	m.SetState("alice", StateCompleted)
	for i := 0; i < 10; i++ {
		m.ReportProgress("alice")
	}
	e, _ := m.Get("alice")
	if e.State != StateCompleted {
		t.Errorf("State = %q, want %q", e.State, StateCompleted)
	}
	if e.StopReason != "Error: model capacity" {
		t.Errorf("StopReason after State=completed + 10 progress = %q, want unchanged",
			e.StopReason)
	}
}
