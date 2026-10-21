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

// scriptedAgw is a minimal OpenAI-shaped SSE gateway: one canned list of data
// payloads per request, in order.
type scriptedAgw struct {
	server *httptest.Server
	mu     sync.Mutex
	turns  [][]string
	bodies []map[string]any
}

func newScriptedAgw(t *testing.T, turns ...[]string) *scriptedAgw {
	t.Helper()
	gateway := &scriptedAgw{turns: turns}
	gateway.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gateway.mu.Lock()
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gateway.bodies = append(gateway.bodies, body)
		index := len(gateway.bodies) - 1
		var payloads []string
		if index < len(gateway.turns) {
			payloads = gateway.turns[index]
		}
		gateway.mu.Unlock()

		w.Header().Set("content-type", "text/event-stream")
		for _, payload := range payloads {
			fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(gateway.server.Close)
	return gateway
}

func (g *scriptedAgw) provider(t *testing.T) *piagentbridge.Provider {
	t.Helper()
	return piagentbridge.NewProvider(piagentbridge.ProviderConfig{
		URL:     g.server.URL,
		APIKey:  "test",
		Model:   "MiniMax-M3",
		Timeout: 5 * time.Second,
	})
}

func (g *scriptedAgw) requestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.bodies)
}

func (g *scriptedAgw) requests() []map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]map[string]any(nil), g.bodies...)
}

func agwText(text string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, text)
}

func agwTool(id, name, args string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}}]}`,
		id, name, args)
}

func agwFinish(reason string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{},"finish_reason":%q}]}`, reason)
}

func agwUsage(prompt, completion, reasoning int) string {
	return fmt.Sprintf(`{"choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d,"completion_tokens_details":{"reasoning_tokens":%d}}}`,
		prompt, completion, prompt+completion, reasoning)
}

// TestLoop_NativeProviderDrivesTheToolRoundTrip is the loop equivalent of the
// bridge's own native-provider test, but through the clone's public Loop API:
// Options.Provider is honoured, PiG reaches it through
// agent_loop.go streamWithProvider, the tool runs through ToolRunner, and the
// Handler still sees nothing but backend.StreamChunks.
func TestLoop_NativeProviderDrivesTheToolRoundTrip(t *testing.T) {
	gateway := newScriptedAgw(t,
		[]string{agwTool("c1", "run_command", `{"command":"date"}`), agwFinish("tool_calls"), "[DONE]"},
		[]string{agwText("It is noon."), agwFinish("stop"), "[DONE]"},
	)

	runner := &fakeRunner{}
	var text strings.Builder
	var surfaced int

	l, err := New(Options{
		Provider:     gateway.provider(t),
		DefaultModel: "MiniMax-M3",
		Handler: func(chunk backend.StreamChunk) error {
			text.WriteString(chunk.Text)
			surfaced += len(chunk.ToolCalls)
			return nil
		},
		Tools:      []backend.ToolDecl{{Name: "run_command", Parameters: json.RawMessage(`{"type":"object"}`)}},
		ToolRunner: runner,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "what time is it"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if len(runner.calls) != 1 || runner.calls[0].Name != "run_command" {
		t.Fatalf("runner calls = %+v", runner.calls)
	}
	if got := string(runner.calls[0].Arguments); got != `{"command":"date"}` {
		t.Errorf("tool arguments = %q", got)
	}
	if text.String() != "It is noon." {
		t.Errorf("text = %q", text.String())
	}
	if surfaced != 1 {
		t.Errorf("surfaced tool calls = %d, want 1", surfaced)
	}
	if got := gateway.requestCount(); got != 2 {
		t.Fatalf("upstream requests = %d, want 2", got)
	}

	requests := gateway.requests()
	if got, _ := requests[0]["model"].(string); got != "MiniMax-M3" {
		t.Errorf("wire model = %q", got)
	}
	if _, ok := requests[0]["tools"]; !ok {
		t.Error("first request advertised no tools")
	}
	// The follow-up must carry the assistant tool call and the tool result.
	encoded, _ := json.Marshal(requests[1])
	if !strings.Contains(string(encoded), `"tool_call_id":"c1"`) {
		t.Errorf("follow-up is missing the tool result: %s", encoded)
	}
	if !strings.Contains(string(encoded), "result-of-run_command") {
		t.Errorf("follow-up is missing the tool output: %s", encoded)
	}
}

// TestLoop_NativeProviderSurfacesUpstreamFailure pins that a gateway failure
// reaches the Handler as an error chunk and comes back out of Send, the same
// contract the StreamFn path has.
func TestLoop_NativeProviderSurfacesUpstreamFailure(t *testing.T) {
	gateway := newScriptedAgw(t)
	gateway.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream is down"))
	})

	var gotErr error
	l, err := New(Options{
		Provider:     gateway.provider(t),
		DefaultModel: "MiniMax-M3",
		Handler:      func(chunk backend.StreamChunk) error { gotErr = chunk.Error; return nil },
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	err = l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Fatal("Send = nil, want the upstream failure")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("Send error = %v", err)
	}
	if gotErr == nil {
		t.Error("Handler never saw the failure chunk")
	}
}

// TestLoop_ErrorFrameSurfacesAsTurnFailure pins the user-visible half of
// cycle stream-error-frames: an SSE error record reaches the TUI's handler
// as StreamChunk{Error}, and Loop.Send returns it, so the turn renders an
// inline failure instead of the empty transcript the gateway's error frame
// used to leave behind. AutoCont is off here so the first attempt's error is
// the one under test (an auto-continue retry would mask the frame).
func TestLoop_ErrorFrameSurfacesAsTurnFailure(t *testing.T) {
	gateway := newScriptedAgw(t, []string{`{"error":{"message":"upstream exploded"}}`})

	var gotErr error
	var text strings.Builder
	l, err := New(Options{
		Provider:     gateway.provider(t),
		DefaultModel: "MiniMax-M3",
		Handler: func(chunk backend.StreamChunk) error {
			text.WriteString(chunk.Text)
			if chunk.Error != nil {
				gotErr = chunk.Error
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	err = l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Fatal("Send = nil, want the gateway's error frame surfaced")
	}
	if !strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("Send error = %v; want the gateway's message", err)
	}
	if gotErr == nil {
		t.Error("Handler never saw the failure chunk")
	}
	if got := text.String(); got != "" {
		t.Errorf("text = %q; want empty (the gateway sent no content)", got)
	}
}

// TestNew_RequiresBackendOrProvider guards the constructor contract.
func TestNew_RequiresBackendOrProvider(t *testing.T) {
	if _, err := New(Options{Handler: func(backend.StreamChunk) error { return nil }}); err == nil {
		t.Fatal("New without Backend/Provider = nil error")
	}
	provider := piagentbridge.NewProvider(piagentbridge.ProviderConfig{})
	if _, err := New(Options{Provider: provider}); err == nil {
		t.Fatal("New without Handler = nil error")
	}
	if _, err := New(Options{Provider: provider, Handler: func(backend.StreamChunk) error { return nil }}); err != nil {
		t.Fatalf("New with Provider: %v", err)
	}
}

// ─── the agw transport switch (cycle 183, part 2) ────────────────────────────

// countingAgwBackend implements backend.Client and describes itself the way
// *backend.AgwK0sClient does, so nativeProvider derives a provider from it. The
// StreamFn path is poisoned: reaching it is a test failure, not a silent
// fallback.
type countingAgwBackend struct {
	cfg     backend.AgwK0sConfig
	mu      sync.Mutex
	streams int
}

func (c *countingAgwBackend) AgwConfig() backend.AgwK0sConfig { return c.cfg }

func (c *countingAgwBackend) Stream(context.Context, backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	c.mu.Lock()
	c.streams++
	c.mu.Unlock()
	out := make(chan backend.StreamChunk, 1)
	out <- backend.StreamChunk{Error: errors.New("the StreamFn path ran; the derived provider was ignored"), Done: true}
	close(out)
	return out, nil
}

func (c *countingAgwBackend) streamCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streams
}

func TestNativeProviderDerivesOnlyFromAgwClients(t *testing.T) {
	client := backend.NewAgwK0sWithConfig(backend.AgwK0sConfig{
		URL:            "http://127.0.0.1:1/v1/chat/completions",
		ExternalModel:  "MiniMax-M3",
		TimeoutSeconds: 7,
	})
	provider := nativeProvider(client)
	if provider == nil {
		t.Fatal("nativeProvider(*backend.AgwK0sClient) = nil")
	}
	if got := provider.Endpoint(); got != "http://127.0.0.1:1/v1/chat/completions" {
		t.Errorf("Endpoint() = %q", got)
	}
	// The resolved config, not the zero value: withDefaults owns the fallbacks.
	if got := provider.WireModel(); got != "MiniMax-M3" {
		t.Errorf("WireModel() = %q", got)
	}
	if p := nativeProvider(backend.NewMock()); p != nil {
		t.Errorf("nativeProvider(mock) = %v, want nil", p)
	}
	if p := nativeProvider(nil); p != nil {
		t.Errorf("nativeProvider(nil) = %v, want nil", p)
	}
}

// TestLoop_AgwBackendGoesThroughTheDerivedProvider is the switch pic: a backend
// that only describes itself still gets served, by the provider derived from its
// config, and the requested Antigravity model name is replaced by the agw-side
// ExternalModel on the wire — exactly what AgwK0sClient does.
func TestLoop_AgwBackendGoesThroughTheDerivedProvider(t *testing.T) {
	gateway := newScriptedAgw(t, []string{agwText("hi"), agwFinish("stop"), "[DONE]"})
	client := &countingAgwBackend{cfg: backend.AgwK0sConfig{
		URL:            gateway.server.URL,
		APIKey:         "test",
		ExternalModel:  "MiniMax-M3",
		TimeoutSeconds: 5,
	}}

	var text strings.Builder
	l, err := New(Options{
		Backend:      client,
		DefaultModel: "claude-sonnet-4-6",
		Handler: func(chunk backend.StreamChunk) error {
			text.WriteString(chunk.Text)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if text.String() != "hi" {
		t.Errorf("text = %q, want the gateway's answer", text.String())
	}
	if got := client.streamCount(); got != 0 {
		t.Errorf("backend.Stream called %d time(s); the derived provider was bypassed", got)
	}
	requests := gateway.requests()
	if len(requests) != 1 {
		t.Fatalf("gateway requests = %d", len(requests))
	}
	if got, _ := requests[0]["model"].(string); got != "MiniMax-M3" {
		t.Errorf("wire model = %q, want the ExternalModel remap", got)
	}
}

// TestLoop_AgwBackendFailureKeepsTheHTTPError pins the item that motivated the
// transport switch: cmd/wagyu/api_error.go classifies a turn with
// errors.As(err, &he) to reproduce the reference's message and exit code, and
// PiG copies only err.Error() across its stream boundary.
func TestLoop_AgwBackendFailureKeepsTheHTTPError(t *testing.T) {
	gateway := newScriptedAgw(t)
	gateway.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream is down"))
	})

	var sawChunkError error
	l, err := New(Options{
		Backend:      backend.NewAgwK0sWithConfig(backend.AgwK0sConfig{URL: gateway.server.URL}),
		DefaultModel: "MiniMax-M3",
		Handler: func(chunk backend.StreamChunk) error {
			sawChunkError = chunk.Error
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	sendErr := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "x"}},
	})
	if sendErr == nil {
		t.Fatal("Send = nil, want the upstream failure")
	}
	var httpErr *backend.HTTPError
	if !errors.As(sendErr, &httpErr) {
		t.Fatalf("Send error %v (%T) does not keep the *backend.HTTPError", sendErr, sendErr)
	}
	if httpErr.StatusCode != http.StatusBadGateway {
		t.Errorf("StatusCode = %d", httpErr.StatusCode)
	}
	if !strings.Contains(httpErr.Message(), "upstream is down") {
		t.Errorf("Message() = %q", httpErr.Message())
	}
	// The Handler sees it too, and the reference's own message is what the
	// recovered api_error path would have printed.
	if sawChunkError == nil {
		t.Error("Handler never saw the failure chunk")
	}
}

// TestLoop_ChunkFingerprintSurvivesPiG covers the third item. backend/agw_k0s.go
// stamps SystemFingerprint on its usage record and cmd/openai-shim echoes it;
// ai.Usage has no field for it, so it only survives if it is carried beside the
// chunks. The mock stamps a different value, which also pins that the value
// comes off the wire rather than being derived from the provider.
func TestLoop_ChunkFingerprintSurvivesPiG(t *testing.T) {
	var meta *backend.ChunkMeta
	l, err := New(Options{
		Backend:      backend.NewMock(),
		DefaultModel: "MiniMax-M3",
		Handler: func(chunk backend.StreamChunk) error {
			if chunk.Done {
				meta = chunk.Meta
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hello"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if meta == nil {
		t.Fatal("done chunk carried no metadata")
	}
	if got := meta.SystemFingerprint; got != "shim-mock-1" {
		t.Errorf("SystemFingerprint = %q, want the transport's own value", got)
	}
	if meta.PromptTokens == 0 {
		t.Error("PromptTokens = 0; the usage record was dropped")
	}
}

// TestLoop_NativeProviderStampsTheAgwFingerprint is the other half: with no
// usage record to read, the native agw provider supplies the same value
// backend.AgwK0sClient would have.
func TestLoop_NativeProviderStampsTheAgwFingerprint(t *testing.T) {
	gateway := newScriptedAgw(t, []string{
		agwText("hi"),
		agwUsage(7, 3, 0),
		agwFinish("stop"),
		"[DONE]",
	})

	var meta *backend.ChunkMeta
	l, err := New(Options{
		Provider:     gateway.provider(t),
		DefaultModel: "MiniMax-M3",
		Handler: func(chunk backend.StreamChunk) error {
			if chunk.Done {
				meta = chunk.Meta
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if meta == nil {
		t.Fatal("done chunk carried no metadata")
	}
	want := backend.AgwSystemFingerprint("MiniMax-M3")
	if got := meta.SystemFingerprint; got != want {
		t.Errorf("SystemFingerprint = %q, want %q", got, want)
	}
	if meta.PromptTokens != 7 || meta.CompletionTokens != 3 {
		t.Errorf("tokens = %d/%d, want 7/3", meta.PromptTokens, meta.CompletionTokens)
	}
}

// TestLoop_NoUsageMeansNoMetadata pins the guard backend.AgwK0sClient has: its
// ChunkMeta is built inside the usage branch, so a transport that reports no
// metadata at all yields no metadata — not an empty one carrying a fingerprint.
func TestLoop_NoUsageMeansNoMetadata(t *testing.T) {
	var done bool
	var meta *backend.ChunkMeta
	l, err := New(Options{
		Backend:      &bareClient{text: "hi"},
		DefaultModel: "MiniMax-M3",
		Handler: func(chunk backend.StreamChunk) error {
			if chunk.Done {
				done, meta = true, chunk.Meta
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer l.Close()

	if err := l.Send(context.Background(), TurnInput{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !done {
		t.Fatal("Handler never saw the done chunk")
	}
	if meta != nil {
		t.Errorf("meta = %+v, want nil without a usage record", meta)
	}
}

// bareClient answers with text and closes, carrying no metadata at all: the
// shape a gateway that ignores stream_options.include_usage produces.
type bareClient struct{ text string }

func (c *bareClient) Stream(ctx context.Context, _ backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	out := make(chan backend.StreamChunk, 2)
	out <- backend.StreamChunk{Text: c.text}
	out <- backend.StreamChunk{Done: true}
	close(out)
	return out, nil
}

// ─── cycle auto-continue-interrupt ───────────────────────────────────────

// flakyAgw is a scripted gateway that fails the first N requests with
// HTTP 502 and answers the rest from a fixed set of payloads. The cycle
// auto-continue-interrupt spec exercises this exact shape: the upstream
// stream closes mid-tool-call (e.g. a network drop) and the runtime
// retries with the verbatim AGY recovery copy.
type flakyAgw struct {
	server *httptest.Server
	mu     sync.Mutex
	failN  int
	turns  [][]string
	bodies []map[string]any
}

func newFlakyAgw(t *testing.T, failN int, turns ...[]string) *flakyAgw {
	t.Helper()
	f := &flakyAgw{failN: failN, turns: turns}
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

func (f *flakyAgw) requestCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.bodies)
}

func (f *flakyAgw) requests() []map[string]any {
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
	flaky := newFlakyAgw(t, 1,
		[]string{agwText("recovered"), agwFinish("stop"), "[DONE]"},
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

	// Two upstream requests: the failing one and the recovered one.
	if got := flaky.requestCount(); got != 2 {
		t.Errorf("upstream requests = %d, want 2 (1 fail + 1 recovery)", got)
	}

	// The second request's body must carry the AGY recovery copy verbatim.
	// PiG routes it in as a user message appended after the original
	// prompt; reading the wire body is the only place we can guarantee
	// the constants showed up at byte-fidelity.
	requests := flaky.requests()
	if len(requests) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(requests))
	}
	encoded, _ := json.Marshal(requests[1])
	if !strings.Contains(string(encoded), "The stream was interrupted. Please continue the task you were working on.") {
		t.Errorf("follow-up body missing the verbatim recovery copy: %s", encoded)
	}
}

// TestLoop_AutoContGiveUpAfterMaxDepth pins the cycle spec's
// AutoContMaxDepth (=3) ceiling: three consecutive stream-interrupts
// must trip the typed ErrAutoContMaxDepth give-up error so the TUI can
// surface a user-input-required prompt. Recovery copy still uses the
// stream-interrupted text (the iteration-cap banner only fires when
// the wrap actually is ErrMaxTurnsReached), and the unwrap chain keeps
// the original *backend.HTTPError for api_error.go classification.
func TestLoop_AutoContGiveUpAfterMaxDepth(t *testing.T) {
	flaky := newFlakyAgw(t, AutoContMaxDepth+1) // every request fails

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
	flaky := newFlakyAgw(t, 1)

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

// flakyAgw.serverProvider is a small inline helper mirroring the
// scriptedAgw.Provider method but inlined here because flakyAgw is a
// separate type.
func (f *flakyAgw) serverProvider(t *testing.T) *piagentbridge.Provider {
	t.Helper()
	return piagentbridge.NewProvider(piagentbridge.ProviderConfig{
		URL:     f.server.URL,
		APIKey:  "test",
		Model:   "MiniMax-M3",
		Timeout: 5 * time.Second,
	})
}
