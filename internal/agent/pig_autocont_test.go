package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/piagentbridge"
)

// ─── cycle auto-continue-interrupt ───────────────────────────────────────

// flakyGateway is a scripted gateway that fails the first N requests with
// HTTP 502 and answers the rest from a fixed set of payloads. The cycle
// auto-continue-interrupt spec exercises this exact shape: the upstream
// stream closes mid-tool-call (e.g. a network drop) and the runtime
// retries with the verbatim AGY recovery copy.
type flakyGateway struct {
	server *httptest.Server
	mu     sync.Mutex
	failN  int
	turns  [][]string
	bodies []map[string]any
}

func newFlakyGateway(t *testing.T, failN int, turns ...[]string) *flakyGateway {
	t.Helper()
	f := &flakyGateway{failN: failN, turns: turns}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.bodies = append(f.bodies, body)
		index := len(f.bodies) - 1
		f.mu.Unlock()
		if index < f.failN {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("upstream is down"))
			return
		}
		var payloads []string
		if j := index - f.failN; j >= 0 && j < len(f.turns) {
			payloads = f.turns[j]
		}
		w.Header().Set("content-type", "text/event-stream")
		for _, payload := range payloads {
			fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *flakyGateway) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func (f *flakyGateway) requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.bodies...)
}

// TestLoop_AutoContRecoversFromUpstreamStreamInterrupt pins the cycle
// auto-continue-interrupt happy path: the first upstream call returns
// 502 (the kind of network stream-interrupt PiG encodes as
// StopReasonError and returns nil for), Options.AutoCont is set, and
// the wrapper pushes the verbatim AGY recovery copy as a user message
// and replays. The second call succeeds; the banner callback is fired
// exactly once and observes depth=1; the response text carries the
// gateway's answer.
func TestLoop_AutoContRecoversFromUpstreamStreamInterrupt(t *testing.T) {
	flaky := newFlakyGateway(t, 1,
		[]string{gatewayText("recovered"), gatewayFinish("stop"), "[DONE]"},
	)

	var bannerCalls int
	var lastDepth int
	var text strings.Builder
	l, err := New(Options{
		Provider:     flaky.serverProvider(t),
		DefaultModel: "MiniMax-M3",
		Handler: func(chunk backend.StreamChunk) error {
			text.WriteString(chunk.Text)
			return nil
		},
		AutoCont: true,
		AutoContBanner: func(depth, maxDepth int) {
			bannerCalls++
			lastDepth = depth
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Send: %v (auto-continue should have recovered)", err)
	}

	if bannerCalls != 1 {
		t.Errorf("banner calls = %d, want 1 (one retry was needed)", bannerCalls)
	}
	if lastDepth != 1 {
		t.Errorf("last banner depth = %d, want 1", lastDepth)
	}
	if got, want := text.String(), "recovered"; got != want {
		t.Errorf("recovered text = %q, want %q", got, want)
	}

	if got := flaky.requestCount(); got != 2 {
		t.Errorf("upstream requests = %d, want 2 (1 fail + 1 recovery)", got)
	}

	assertAutoContRecoveryRequest(t, flaky.requests())
}

func assertAutoContRecoveryRequest(t *testing.T, requests []map[string]any) {
	t.Helper()
	if len(requests) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(requests))
	}
	encoded, _ := json.Marshal(requests[1])
	if !strings.Contains(string(encoded), "The stream was interrupted. Please continue the task you were working on.") {
		t.Errorf("follow-up body missing the verbatim recovery copy: %s", encoded)
	}
}

// TestLoop_AutoContGiveUpAfterMaxDepth pins the cycle's bounding guard:
// a stream that keeps failing terminates after AutoContMaxDepth attempts
// and Loop.Send returns ErrAutoContMaxDepth (which the TUI maps to the
// AGY "Failed after 3 attempts" red status bar).
func TestLoop_AutoContGiveUpAfterMaxDepth(t *testing.T) {
	flaky := newFlakyGateway(t, AutoContMaxDepth+1) // every request fails

	var bannerCalls int
	l, err := New(Options{
		Provider:     flaky.serverProvider(t),
		DefaultModel: "MiniMax-M3",
		Handler:      func(chunk backend.StreamChunk) error { return nil },
		AutoCont:     true,
		AutoContBanner: func(depth, maxDepth int) {
			bannerCalls++
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	sendErr := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if sendErr == nil {
		t.Fatal("Send = nil, want ErrAutoContMaxDepth after exhausting retries")
	}
	var maxDepthErr *ErrAutoContMaxDepth
	if !errors.As(sendErr, &maxDepthErr) {
		t.Fatalf("Send err = %v (%T), want *ErrAutoContMaxDepth", sendErr, sendErr)
	}
	if maxDepthErr.Depth != AutoContMaxDepth {
		t.Errorf("ErrAutoContMaxDepth.Depth = %d, want %d", maxDepthErr.Depth, AutoContMaxDepth)
	}
	// The original failure (a *backend.HTTPError for a 502) must still be
	// reachable through errors.Is, so api_error.go keeps classifying it.
	var httpErr *backend.HTTPError
	if !errors.As(sendErr, &httpErr) {
		t.Errorf("Send err = %v (%T) does not preserve the *backend.HTTPError underneath ErrAutoContMaxDepth", sendErr, sendErr)
	} else if httpErr.StatusCode != http.StatusBadGateway {
		t.Errorf("HTTPError.StatusCode = %d, want 502", httpErr.StatusCode)
	}
	// Banners: one before each retry. With AutoContMaxDepth (=3) attempts
	// at most, we get exactly AutoContMaxDepth-1 = 2 banner firings
	// (depth 1 and 2). Depth 3 is reached only on give-up.
	if bannerCalls != AutoContMaxDepth-1 {
		t.Errorf("banner calls = %d, want %d", bannerCalls, AutoContMaxDepth-1)
	}
}

// TestLoop_AutoContOffSurfacesFailure confirms the default behaviour
// (Options.AutoCont unset) is unchanged: a stream-interrupt surfaces as
// a typed error and the runtime does not retry.
func TestLoop_AutoContOffSurfacesFailure(t *testing.T) {
	flaky := newFlakyGateway(t, 1)

	var bannerCalls int
	l, err := New(Options{
		Provider:       flaky.serverProvider(t),
		DefaultModel:   "MiniMax-M3",
		Handler:        func(chunk backend.StreamChunk) error { return nil },
		AutoContBanner: func(int, int) { bannerCalls++ },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}); err == nil {
		t.Fatal("Send = nil, want the 502 with AutoCont off")
	}
	if bannerCalls != 0 {
		t.Errorf("banner calls = %d, want 0 (AutoCont off)", bannerCalls)
	}
	if got := flaky.requestCount(); got != 1 {
		t.Errorf("upstream requests = %d, want 1 (no retries with AutoCont off)", got)
	}
}

// flakyGateway.serverProvider is a small inline helper mirroring the
// scriptedGateway.Provider method but inlined here because flakyGateway is a
// separate type.
func (f *flakyGateway) serverProvider(t *testing.T) *piagentbridge.Provider {
	t.Helper()
	return piagentbridge.NewProvider(piagentbridge.ProviderConfig{
		URL:     f.server.URL,
		APIKey:  "test",
		Model:   "MiniMax-M3",
		Timeout: 5 * time.Second,
	})
}
