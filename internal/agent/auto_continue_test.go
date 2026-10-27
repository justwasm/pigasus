// auto_continue_test.go — cover the cycle auto-continue-interrupt helpers
// in agent/auto_continue.go.
//
// The tests target the unit-level pieces (classifyInterruption,
// recoveryForError, recoveryMessageFromMessages, turn budget parsing,
// the ErrAutoContMaxDepth give-up) so the harder Send-loop integration
// is exercised separately by the runtime_test.go parity harness.
package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/piagentbridge"
)

// TestClassifyInterruptionMatrix pins the trigger matrix the doc at the
// top of auto_continue.go advertises: StopReasonError / Aborted / Length
// → stream interrupted; ErrMaxTurnsReached → status banner; everything
// else → "" (no recovery).
func TestClassifyInterruptionMatrix(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		stop     pi.StopReason
		wantText string
	}{
		{"StopReasonError", nil, pi.StopReasonError, AutoContStreamInterrupted},
		{"StopReasonAborted", nil, pi.StopReasonAborted, AutoContStreamInterrupted},
		{"StopReasonLength", nil, pi.StopReasonLength, AutoContStreamInterrupted},
		{"StopReasonStop", nil, pi.StopReasonStop, ""},
		{"StopReasonToolUse", nil, pi.StopReasonToolUse, ""},
		{"nil err StopReasonError", nil, pi.StopReasonError, AutoContStreamInterrupted},
		{"MaxTurnsReached yields status", fmt.Errorf("%w (%d turns)", piagent.ErrMaxTurnsReached, 7), pi.StopReasonStop, fmt.Sprintf(AutoContIterationCapFormat, 7)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyInterruption(tc.err, tc.stop)
			if got != tc.wantText {
				t.Errorf("classifyInterruption(%v, %q) = %q, want %q", tc.err, tc.stop, got, tc.wantText)
			}
		})
	}
}

// TestRecoveryForError pins the simpler wrapper used by sendPigAuto —
// the agent-side wrapper does not see lastStopReason because PiG
// encoded it inside the runner, so the err alone drives the choice.
func TestRecoveryForError(t *testing.T) {
	if got := recoveryForError(nil); got != "" {
		t.Errorf("recoveryForError(nil) = %q, want empty", got)
	}
	if got := recoveryForError(errors.New("stream chopped")); got != AutoContStreamInterrupted {
		t.Errorf("recoveryForError(generic) = %q, want stream-interrupted", got)
	}
	wrapped := fmt.Errorf("%w (%d turns)", piagent.ErrMaxTurnsReached, 4)
	if got := recoveryForError(wrapped); got != fmt.Sprintf(AutoContIterationCapFormat, 4) {
		t.Errorf("recoveryForError(MaxTurnsReached) = %q, want status banner with 4", got)
	}
}

// TestRecoveryMessageFromMessagesPicksLastSynthetic pins the helper
// the cycle auto-continue-interrupt transcript tests use to verify the
// recovery copy landed at byte-fidelity to the AGY verbatim strings.
func TestRecoveryMessageFromMessagesPicksLastSynthetic(t *testing.T) {
	streamInterrupted := AutoContStreamInterrupted
	status := fmt.Sprintf(AutoContIterationCapFormat, 4)
	cases := []struct {
		name string
		msgs []backend.Message
		want string
	}{
		{"none", []backend.Message{{Role: "user", Content: "hi"}}, ""},
		{"only stream", []backend.Message{{Role: "user", Content: streamInterrupted}}, streamInterrupted},
		{"only status", []backend.Message{{Role: "user", Content: status}}, status},
		{"last wins", []backend.Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "thinking"},
			{Role: "user", Content: streamInterrupted},
			{Role: "assistant", Content: "tool call"},
			{Role: "user", Content: status},
		}, status},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := recoveryMessageFromMessages(tc.msgs)
			if got != tc.want {
				t.Errorf("recoveryMessageFromMessages(%v) = %q, want %q", tc.msgs, got, tc.want)
			}
		})
	}
}

// TestErrAutoContMaxDepthUnwraps pins the contract cmd/wagyu/api_error.go
// relies on: the give-up error preserves the typed *backend.HTTPError
// (or whichever pre-stream failure) underneath it, so the AGY_ERROR
// envelope and exit code 3 still fire when the original was a gateway
// failure.
func TestErrAutoContMaxDepthUnwraps(t *testing.T) {
	base := errors.New("upstream is down")
	wrap := &ErrAutoContMaxDepth{Depth: AutoContMaxDepth, Last: base}
	if !errors.Is(wrap, base) {
		t.Errorf("errors.Is(ErrAutoContMaxDepth, base) = false; chain must preserve the original")
	}
	if !strings.Contains(wrap.Error(), "agent: auto-continue reached max depth (3)") {
		t.Errorf("Error() = %q, missing the cycle prefix", wrap.Error())
	}
}

// TestQuotaErrorShortCircuitsTUI pins the changelog 1.3.3 rule "reaching
// your individual quota is reported right away instead of being retried" for
// the TUI path. A quota-classified error must short-circuit sendPigAuto on
// the first iteration: no recovery copy is appended, the typed
// *backend.HTTPError is preserved through errors.As so cmd/wagyu/api_error.go
// still classifies it as RESOURCE_EXHAUSTED, and the banner callback never
// fires (the user is not "auto-continuing", the session is at its quota).
//
// The test wires a scripted gateway that returns 429 + the canonical
// "Individual quota reached." JSON body. If sendPigAuto ever added a second
// invocation, gateway.requestCount() would rise above 1 and the test would
// catch the regression.
func TestQuotaErrorShortCircuitsTUI(t *testing.T) {
	var requestCount int32
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"Individual quota reached. Please upgrade your subscription."}}`))
	}))
	t.Cleanup(server.Close)

	provider := piagentbridge.NewProvider(piagentbridge.ProviderConfig{
		URL:     server.URL,
		APIKey:  "test",
		Model:   "MiniMax-M3",
		Timeout: 5 * time.Second,
	})

	bannerCalls := 0
	l, err := New(Options{
		Provider:     provider,
		DefaultModel: "MiniMax-M3",
		AutoCont:     true,
		AutoContBanner: func(int, int) {
			bannerCalls++
		},
		Handler: func(chunk backend.StreamChunk) error { return nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	got := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	})
	if got == nil {
		t.Fatal("Send returned nil; expected the quota error")
	}
	// The typed HTTPError must survive the wrapper so the TUI's
	// errors.As chain still classifies it.
	var he *backend.HTTPError
	if !errors.As(got, &he) {
		t.Fatalf("quota error did not preserve *backend.HTTPError chain: %v", got)
	}
	if he.StatusCode != 429 {
		t.Errorf("HTTPError.StatusCode = %d, want 429", he.StatusCode)
	}
	if !strings.Contains(he.Message(), "quota") {
		t.Errorf("HTTPError.Message() = %q, missing quota keyword", he.Message())
	}
	// sendPigAuto must NOT wrap a quota error in ErrAutoContMaxDepth
	// (that would imply a retry happened) — the user wants the typed
	// error to surface unchanged.
	var giveUp *ErrAutoContMaxDepth
	if errors.As(got, &giveUp) {
		t.Errorf("quota error was wrapped in ErrAutoContMaxDepth; want a bare typed error")
	}
	// The banner must NOT have fired — the session is not auto-
	// continuing, it has been told the quota is exhausted.
	if bannerCalls != 0 {
		t.Errorf("banner calls = %d, want 0 (quota is not auto-continuable)", bannerCalls)
	}
	// Crucially: the gateway saw exactly ONE request, not the three
	// AutoContMaxDepth would imply. This is the observable the user
	// asked for in the changelog: "reported right away instead of
	// being retried".
	mu.Lock()
	got2 := requestCount
	mu.Unlock()
	if got2 != 1 {
		t.Errorf("upstream requests = %d, want 1 (no auto-continue retry)", got2)
	}
}

// TestAutoContDepthTracker is a small fuzz for the tracker used by
// future recovery-loop refactors; today only the sendPigAuto wrapper
// uses it, but its semantics (depth grows monotonically, resets on
// call) matter once more than one place drives the budget.
func TestAutoContDepthTracker(t *testing.T) {
	tr := &autoContTracker{}
	tr.reset()
	for i := 0; i < AutoContMaxDepth; i++ {
		got, ok := tr.next()
		if !ok {
			t.Fatalf("next at i=%d returned !ok (depth exhausted too early)", i)
		}
		if got != i+1 {
			t.Errorf("next at i=%d = %d, want %d", i, got, i+1)
		}
	}
	if _, ok := tr.next(); ok {
		t.Error("next beyond max depth must return ok=false")
	}
	tr.reset()
	if got, ok := tr.next(); !ok || got != 1 {
		t.Errorf("after reset, next = (%d, %v), want (1, true)", got, ok)
	}
}

// TestAutoContVerbatimCopy pins the bytes of the recovery copy against
// the AGY verbatim strings (docs/research/agy-feature-strings.md §3,
// loadfile 0x33dc41b / 0x33a3d44).  A future cycle that wants to do a
// real byte-match against the binary only needs to read the constants
// — the test here is the witness that the strings in this repo match
// that doc verbatim, byte-for-byte.
func TestAutoContVerbatimCopy(t *testing.T) {
	if got := AutoContStreamInterrupted; got != "The stream was interrupted. Please continue the task you were working on." {
		t.Errorf("AutoContStreamInterrupted drifted: %q", got)
	}
	if got := AutoContIterationCapFormat; got != "**Status**: Cancelled after %d iterations" {
		t.Errorf("AutoContIterationCapFormat drifted: %q", got)
	}
}

// TestAutoContBudgetDefaults pins the numeric ceilings delivered by
// cycle 266: AutoContMaxDepth stays at 3 for the historical "fail
// fast on permanent errors" path, AutoContMaxNetworkDepth bumps to 8
// so transient socket disconnects can ride out several wifi
// handoffs before the user is told the network is the problem.
// A drift in either constant is a wire-format break — every
// downstream test that depends on the count pin would silently start
// doing the wrong thing.
func TestAutoContBudgetDefaults(t *testing.T) {
	if got := AutoContMaxDepth; got != 3 {
		t.Errorf("AutoContMaxDepth = %d, want 3 (historical ceiling; cycle 266 only widens the network path)", got)
	}
	if got := AutoContMaxNetworkDepth; got != 8 {
		t.Errorf("AutoContMaxNetworkDepth = %d, want 8 (cycle 266 transient-network ceiling)", got)
	}
	// Sanity: the network budget must be strictly larger than the
	// generic budget, otherwise cycle 266 is a no-op.
	if AutoContMaxNetworkDepth <= AutoContMaxDepth {
		t.Errorf("AutoContMaxNetworkDepth (%d) must exceed AutoContMaxDepth (%d)",
			AutoContMaxNetworkDepth, AutoContMaxDepth)
	}
}

// TestIsTransientNetworkErr_PresetMatrix is a brief reconciliation
// against the test in transient_network_test.go — it exists here so a
// future cycle that adds a phrase but forgets to update both files
// surfaces a single red light.
func TestIsTransientNetworkErr_PresetMatrix(t *testing.T) {
	mustCover := []string{
		"broken pipe",
		"connection reset by peer",
		"http2: client connection lost",
		"server closed idle connection",
	}
	for _, s := range mustCover {
		if !IsTransientNetworkErr(fmt.Errorf("upstream: %s", s)) {
			t.Errorf("IsTransientNetworkErr must cover %q", s)
		}
	}
	mustSkip := []string{
		"insufficient_scope",
		"individual quota reached",
		"401 unauthorized",
	}
	for _, s := range mustSkip {
		if IsTransientNetworkErr(fmt.Errorf("upstream: %s", s)) {
			t.Errorf("IsTransientNetworkErr must NOT cover %q", s)
		}
	}
}
