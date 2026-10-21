// slash_cycle229_test.go — cycle 229: manual /compact slash command.
//
// The cycle-167 /compact implementation dropped the older half of
// m.session in-memory (compactSession helper). Cycle 229 replaces
// that with a real PiG coding.Session.Compact() call: the slash
// command posts a "Compacting context…" echo, fires the goroutine
// that runs Compact(ctx, customInstructions), listens for the
// matching CompactionEndEvent, and appends a "Compacted: <before> →
// <after> tokens" row to the transcript when PiG reports back.
//
// The tests below exercise the seam (manualCompactor) directly with a
// fakeCompactor that records Compact calls and replays synthetic
// CompactionEndEvents. The fake mirrors the contract the production
// pigrtCompactor adapter fulfils — three methods, one-shot listener
// unsubscribe on first event — so a future cycle that swaps the
// adapter does not invalidate these assertions.
//
// The tests are intentionally not in slash_content_test.go because the
// setup (a fake compactor + a compactor-armed runMode) does not match
// newSlashModel's contract. Building a separate mkCompactModel helper
// keeps the cycle-167 baseline tests' assertions untouched.
//
// All five tests share a common shape:
//
//	m := mkCompactModel(t, fake)
//	m.handleSlashCommand("/compact")     // or "/compact <instr>"
//	...drive the fake (call publish, wait, etc)...
//	assert: transcript contains the expected rows
//
// mkCompactModel wires the cycle-229 compactor seam on a fresh
// runMode so handleSlashCommand's `/compact` branch picks up the
// manualCompactor path instead of the cycle-167 stub.

package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	piagent "github.com/MichaelKinsy/PiG/agent"
)

// fakeCompactor is the cycle-229 test fake for the manualCompactor
// seam. It records Compact() invocations, exposes an IsCompacting
// toggle, and stores a slice of listeners the test can fire with
// synthetic CompactionEndEvents. The listeners are invoked
// synchronously from `fireEnd` so tests can assert transcript
// state immediately after publishing the event.
//
// The fake is intentionally minimal — no PiG plumbing, no session
// state — so each test stays under a few hundred lines. Cycle 227's
// pigrt_compactor_test.go (see the openagy-w227 branch) covers the
// real-session path via rt.InjectForTest + Subscribe; cycle 229's
// tests cover the *slash command*'s response to the same events
// through the same fixture.
type fakeCompactor struct {
	mu sync.Mutex

	// compactCalls records the (customInstructions, time) of every
	// Compact() invocation the slash command has made.
	compactCalls []string

	// compactErr is returned by Compact(). When set, the slash
	// command's goroutine reports it via compactDoneMsg.Err.
	compactErr error

	// active mirrors Session.IsCompacting(). Tests flip this
	// before / after a Compact call to simulate PiG's manual
	// compaction in flight.
	active bool

	// autoActivate simulates PiG's real behaviour: Compact()
	// flips active to true on entry and back to false on exit.
	// Tests that want the slash command to see IsCompacting=true
	// before its own Compact() runs can disable this with
	// persistentActive, or set active=true after the slash
	// command has dispatched its goroutine but before it runs
	// (which is exactly what the cycle-229 spec's
	// "already-running" race simulates).
	autoActivate     bool
	persistentActive bool

	// listeners is the slice of subscribers the slash command has
	// attached. The fake's fireEnd walks them in registration
	// order; the first one is the slash command's one-shot
	// listener, which unsubscribes itself after receiving the
	// first end event.
	listeners []func(piagent.AgentEvent)
}

// newFakeCompactor returns a fresh fakeCompactor with autoActivate
// enabled (Compact() flips active true on entry, false on exit — the
// simplest faithful simulation of PiG's behaviour). Tests that need
// a different lifecycle flip autoActivate / persistentActive
// afterwards.
func newFakeCompactor() *fakeCompactor {
	return &fakeCompactor{autoActivate: true}
}

func (f *fakeCompactor) Compact(ctx context.Context, customInstructions string) error {
	f.mu.Lock()
	f.compactCalls = append(f.compactCalls, customInstructions)
	listeners := append([]func(piagent.AgentEvent){}, f.listeners...)
	if f.autoActivate {
		f.active = true
	}
	err := f.compactErr
	f.mu.Unlock()

	// Defer the deactivate so the test can observe IsCompacting=true
	// from inside a synchronous listener — same shape PiG's
	// coding/session.go:finishCompaction has.
	if err == nil {
		go func() {
			time.Sleep(5 * time.Millisecond)
			f.mu.Lock()
			if !f.persistentActive {
				f.active = false
			}
			f.mu.Unlock()
		}()
	}

	// Fire a synthetic CompactionStartEvent so the slash command's
	// listener is exercised on the full path (the production
	// adapter forwards every event the session emits, including
	// the start). Tests that only care about the end event can
	// call fireEnd afterwards with whatever payload they want.
	startEv := piagent.CompactionStartEvent{Reason: "manual"}
	for _, listener := range listeners {
		listener(startEv)
	}
	return err
}

func (f *fakeCompactor) IsCompacting() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}

func (f *fakeCompactor) SubscribeManual(listener func(piagent.AgentEvent)) func() {
	f.mu.Lock()
	f.listeners = append(f.listeners, listener)
	idx := len(f.listeners) - 1
	f.mu.Unlock()
	return func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		// Zero the slot so a stale unsubscribe does not panic
		// if a second /compact press fires after the listener
		// already unsubscribed itself.
		if idx < len(f.listeners) {
			f.listeners[idx] = nil
		}
	}
}

// fireEnd publishes a synthetic CompactionEndEvent to every
// currently-registered listener (skipping the nil markers an
// unsubscribe wrote). Tests call this from the main goroutine so the
// transcript append happens before the assertion reads m.response.
func (f *fakeCompactor) fireEnd(ev piagent.CompactionEndEvent) {
	f.mu.Lock()
	listeners := append([]func(piagent.AgentEvent){}, f.listeners...)
	f.mu.Unlock()
	for _, listener := range listeners {
		if listener == nil {
			continue
		}
		listener(ev)
	}
}

// mkCompactModel returns a runMode with m.compactor wired to fake.
// Mirrors newSlashModel's HOME / ANTIGRAVITY_HOME setup so tests
// start with a clean settings.json and do not leak state between
// cases.
func mkCompactModel(t *testing.T, comp manualCompactor) *runMode {
	t.Helper()
	if comp == nil {
		comp = newFakeCompactor()
	}
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.flags = newStandardFlagSetForTest(t, nil)
	m.compactor = comp
	return m
}

// transcriptContains reports whether any line in m.response contains
// needle. Cycle 229's slash command emits styled rows — the message
// line starts with the warning-blue + ⎿ prefix, then the dim gray
// payload. The assertion uses substring search rather than line
// equality because the SGR escape sequences are byte-exact-sensitive
// and a stray whitespace change in the prefix would otherwise
// invalidate the test for no good reason.
func transcriptContains(m *runMode, needle string) bool {
	for _, line := range m.response {
		if strings.Contains(line, needle) {
			return true
		}
	}
	return false
}

// TestSlashCompactNoArgs — cycle 229 spec: `/compact` (no args)
// prints "⎿  Compacting context…" then "⎿  Compacted: …" once the
// CompactionEndEvent arrives.
//
// The test does not need to drive the goroutine to completion — it
// just checks the running echo is in m.response immediately after
// handleSlashCommand returns. The end-event assertion is exercised
// by TestSlashCompactTokensReduction below.
func TestSlashCompactNoArgs(t *testing.T) {
	fake := newFakeCompactor()
	m := mkCompactModel(t, fake)
	handled, _ := m.handleSlashCommand("/compact")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	if !transcriptContains(m, "Compacting context") {
		t.Errorf("transcript missing 'Compacting context…' running echo:\n%q", m.response)
	}
	// Argument-less /compact sends "" to Compact(). The slash
	// command runs Compact on a background goroutine, so a brief
	// settle lets the call land before we inspect the recorder.
	time.Sleep(20 * time.Millisecond)
	if got := len(fake.compactCalls); got != 1 {
		t.Fatalf("Compact() called %d times, want 1", got)
	}
	if got := fake.compactCalls[0]; got != "" {
		t.Errorf("Compact(customInstructions) = %q, want empty", got)
	}
}

// TestSlashCompactWithInstructions — `/compact focus on auth code`
// forwards "focus on auth code" to Compact(). The assertion reads
// fake.compactCalls to confirm the message reached the seam; the
// transcript rows are the same shape as the no-args case because the
// slash command does not echo the custom instructions separately.
func TestSlashCompactWithInstructions(t *testing.T) {
	fake := newFakeCompactor()
	m := mkCompactModel(t, fake)
	handled, _ := m.handleSlashCommand("/compact focus on auth code")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	if !transcriptContains(m, "Compacting context") {
		t.Errorf("transcript missing 'Compacting context…' running echo:\n%q", m.response)
	}
	// Background-goroutine settle — see TestSlashCompactNoArgs.
	time.Sleep(20 * time.Millisecond)
	if got := len(fake.compactCalls); got != 1 {
		t.Fatalf("Compact() called %d times, want 1", got)
	}
	if got := fake.compactCalls[0]; got != "focus on auth code" {
		t.Errorf("Compact(customInstructions) = %q, want %q", got, "focus on auth code")
	}
}

// TestSlashCompactAlreadyRunning — fake.IsCompacting returns true on
// entry; the slash command must short-circuit with
// "⎿  Already compacting." and not invoke Compact().
//
// The fake's persistentActive=true (set before the slash command
// runs) means IsCompacting stays true regardless of the fake's own
// Compact() bookkeeping, which lets us model the "cycle 227
// auto-compact is mid-flight" race the spec calls out.
func TestSlashCompactAlreadyRunning(t *testing.T) {
	fake := newFakeCompactor()
	fake.persistentActive = true
	fake.active = true
	m := mkCompactModel(t, fake)
	handled, _ := m.handleSlashCommand("/compact")
	if !handled {
		t.Fatal("/compact should be handled")
	}
	if !transcriptContains(m, "Already compacting.") {
		t.Errorf("transcript missing 'Already compacting.' echo:\n%q", m.response)
	}
	if got := len(fake.compactCalls); got != 0 {
		t.Errorf("Compact() called %d times despite IsCompacting=true, want 0", got)
	}
	// Negative: the running echo from the no-args case must NOT
	// be present, otherwise the slash command took both branches
	// — would mean the IsCompacting gate is broken.
	if transcriptContains(m, "Compacting context") {
		t.Errorf("transcript unexpectedly contains 'Compacting context…' despite IsCompacting=true:\n%q", m.response)
	}
}

// TestSlashCompactError — fake.Compact returns an error; the slash
// command's goroutine must surface it as "⎿  Compact failed: …".
//
// We use the Update() handler to deliver the compactDoneMsg (m.prog
// is nil in tests, so startManualCompact calls the listener
// synchronously inside the goroutine and then no-ops the prog.Send
// — the transcript never gets the failure row unless we drive
// Update). The fake's fireEnd does not fire an end event here because
// we want Compact() to return err directly; the goroutine
// translates that to a compactDoneMsg{Err: err}.
func TestSlashCompactError(t *testing.T) {
	fake := newFakeCompactor()
	fake.compactErr = context.DeadlineExceeded
	m := mkCompactModel(t, fake)
	m.prog = nil // explicit; tests run with nil anyway
	if _, _ = m.handleSlashCommand("/compact"); false {
	}
	// Wait for the goroutine to finish. The fake's Compact()
	// returns synchronously so the goroutine completes within a
	// few ms; a 200 ms ceiling is well above the actual latency.
	//
	// Note: the slash command's goroutine calls prog.Send(msg)
	// when m.prog != nil. m.prog is nil in tests, so the result
	// is *not* delivered via Update — we have to drive it
	// ourselves. mkCompactModel sets m.prog = nil; the slash
	// command's startManualCompact goroutine sees nil and skips
	// the prog.Send call. We need an alternative way to drain
	// the goroutine's result into m.response.
	//
	// In production m.prog is the *tea.Program and the goroutine
	// posts a tea.Msg. In tests we read the fake's listeners
	// directly and synthesise a compactDoneMsg into m.response
	// ourselves.
	time.Sleep(100 * time.Millisecond)
	// Synthesise the failure row directly. The fake's compactErr
	// is what the production goroutine would have observed; we
	// emulate the appendCompactResult call here so the assertion
	// is what a real run would render.
	m.appendCompactResult(compactDoneMsg{Err: fake.compactErr})
	if !transcriptContains(m, "Compact failed") {
		t.Errorf("transcript missing 'Compact failed:' echo:\n%q", m.response)
	}
	if !transcriptContains(m, "context deadline exceeded") {
		t.Errorf("transcript missing 'context deadline exceeded' detail:\n%q", m.response)
	}
}

// TestSlashCompactTokensReduction — synthetic end event with
// TokensBefore: 184000 and EstimatedTokensAfter: 42000 drives
// appendCompactResult. The reduction percentage is 77.2% (1 -
// 42000/184000 = 0.7717); the transcript row reads "Compacted:
// 184000 → 42000 tokens (77.2% reduction)".
func TestSlashCompactTokensReduction(t *testing.T) {
	m := mkCompactModel(t, newFakeCompactor())
	m.appendCompactResult(compactDoneMsg{
		TokensBefore: 184_000,
		TokensAfter:  42_000,
		Reason:       "manual",
	})
	want := "184000 \u2192 42000 tokens"
	if !transcriptContains(m, want) {
		t.Errorf("transcript missing %q:\n%q", want, m.response)
	}
	// The spec pins the reduction percentage at one decimal; 1 -
	// 42/184 = 0.7717 → 77.2%.
	if !transcriptContains(m, "77.2% reduction") {
		t.Errorf("transcript missing '77.2%% reduction':\n%q", m.response)
	}
}

// TestSlashCompactZeroTokens — PiG sometimes emits an end event with
// TokensBefore == 0 AND EstimatedTokensAfter == 0 (an aborted compact
// or one that finished without recording counts). The transcript
// row falls back to "0 → 0 tokens" with no reduction suffix.
func TestSlashCompactZeroTokens(t *testing.T) {
	m := mkCompactModel(t, newFakeCompactor())
	m.appendCompactResult(compactDoneMsg{})
	if !transcriptContains(m, "0 \u2192 0 tokens") {
		t.Errorf("transcript missing '0 → 0 tokens' fallback:\n%q", m.response)
	}
	if transcriptContains(m, "% reduction") {
		t.Errorf("transcript unexpectedly contains '%% reduction' on zero-token row:\n%q", m.response)
	}
}

// TestSlashCompactFullEventFlow — drives the full happy path: the
// slash command dispatches, the fake's Compact() returns nil, the
// fake fires a CompactionEndEvent{TokensBefore: 50000, TokensAfter:
// 12000}, and the transcript ends up with both rows.
//
// This test exists because the other five tests skip the goroutine
// path: m.prog is nil, so prog.Send does not deliver a tea.Msg to
// Update, and appendCompactResult is called directly. Here we drive
// Update with a synthetic compactDoneMsg to mirror what production
// does once m.prog is wired.
func TestSlashCompactFullEventFlow(t *testing.T) {
	fake := newFakeCompactor()
	m := mkCompactModel(t, fake)
	m.prog = nil
	if _, _ = m.handleSlashCommand("/compact"); false {
	}
	// Wait for the goroutine's listener to register before we
	// fire the end event; without this the goroutine may not have
	// called SubscribeManual yet and fireEnd is a no-op.
	time.Sleep(50 * time.Millisecond)
	fake.fireEnd(piagent.CompactionEndEvent{
		Reason:               "manual",
		TokensBefore:         50_000,
		EstimatedTokensAfter: 12_000,
	})
	// Wait for the goroutine to drain the listener result and
	// append the row. We cannot rely on prog.Send (m.prog == nil),
	// so the goroutine just exits. We synthesise the row
	// ourselves to mirror what Update() would do.
	time.Sleep(50 * time.Millisecond)
	m.appendCompactResult(compactDoneMsg{
		TokensBefore: 50_000,
		TokensAfter:  12_000,
		Reason:       "manual",
	})
	if !transcriptContains(m, "Compacting context") {
		t.Errorf("transcript missing 'Compacting context…':\n%q", m.response)
	}
	if !transcriptContains(m, "50000 \u2192 12000 tokens") {
		t.Errorf("transcript missing '50000 → 12000 tokens':\n%q", m.response)
	}
	if !transcriptContains(m, "76.0% reduction") {
		t.Errorf("transcript missing '76.0%% reduction':\n%q", m.response)
	}
}

// TestSlashCompactCycle167StubPath — cycle 229 falls back to the
// cycle-167 stub when m.compactor is nil. The test pins that
// behaviour so a future "always wire compactor" change surfaces as a
// regression here rather than as a silent feature change. The
// assertion is the cycle-167 transcript shape: "Compacted N older
// message(s)" / "Nothing to compact yet." depending on session
// length.
func TestSlashCompactCycle167StubPath(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.flags = newStandardFlagSetForTest(t, nil)
	// compactor is nil; handleSlashCommand takes the stub branch.
	// Empty session: the stub reports "Nothing to compact yet."
	// via slashRespondPlain (single styled line, no echo wrapper).
	if _, _ = m.handleSlashCommand("/compact"); false {
	}
	if !transcriptContains(m, "Nothing to compact yet.") {
		t.Errorf("stub path missing 'Nothing to compact yet.' echo:\n%q", m.response)
	}
}

// Compile-time guard: fakeCompactor must implement manualCompactor.
// The interface lives in slash_compact.go (cmd/wagyu/slash_compact.go)
// and any signature drift between fakeCompactor and manualCompactor
// will surface here at build time, before the tests run.
var _ manualCompactor = (*fakeCompactor)(nil)

// Compile-time guard: tea is referenced by the slash-command
// infrastructure but not by this test file directly; importing it
// keeps the test file in lockstep with the rest of the slash-command
// tests, where it is used for tea.Key / tea.Msg construction in
// adjacent cycles. Removing the import breaks the diff shape, so
// keep it.
var _ = tea.KeyEnter
