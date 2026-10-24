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

// scriptedGateway is a minimal OpenAI-shaped SSE gateway: one canned list of data
// payloads per request, in order.
type scriptedGateway struct {
	server *httptest.Server
	mu     sync.Mutex
	turns  [][]string
	bodies []map[string]any
}

func newScriptedGateway(t *testing.T, turns ...[]string) *scriptedGateway {
	t.Helper()
	gateway := &scriptedGateway{turns: turns}
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

func (g *scriptedGateway) provider(t *testing.T) *piagentbridge.Provider {
	t.Helper()
	return piagentbridge.NewProvider(piagentbridge.ProviderConfig{
		URL:     g.server.URL,
		APIKey:  "test",
		Model:   "MiniMax-M3",
		Timeout: 5 * time.Second,
	})
}

func (g *scriptedGateway) requestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.bodies)
}

func (g *scriptedGateway) requests() []map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]map[string]any(nil), g.bodies...)
}

func gatewayText(text string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, text)
}

func gatewayTool(id, name, args string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}}]}`,
		id, name, args)
}

func gatewayFinish(reason string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{},"finish_reason":%q}]}`, reason)
}

func gatewayUsage(prompt, completion, reasoning int) string {
	return fmt.Sprintf(`{"choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d,"completion_tokens_details":{"reasoning_tokens":%d}}}`,
		prompt, completion, prompt+completion, reasoning)
}

// TestLoop_NativeProviderDrivesTheToolRoundTrip is the loop equivalent of the
// bridge's own native-provider test, but through the clone's public Loop API:
// Options.Provider is honoured, PiG reaches it through
// agent_loop.go streamWithProvider, the tool runs through ToolRunner, and the
// Handler still sees nothing but backend.StreamChunks.
func TestLoop_NativeProviderDrivesTheToolRoundTrip(t *testing.T) {
	gateway := newScriptedGateway(t,
		[]string{gatewayTool("c1", "run_command", `{"command":"date"}`), gatewayFinish("tool_calls"), "[DONE]"},
		[]string{gatewayText("It is noon."), gatewayFinish("stop"), "[DONE]"},
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

	assertRoundTripRequests(t, gateway.requests())
}

func assertRoundTripRequests(t *testing.T, requests []map[string]any) {
	t.Helper()
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
	gateway := newScriptedGateway(t)
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
	gateway := newScriptedGateway(t, []string{`{"error":{"message":"upstream exploded"}}`})

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

// ─── the gateway transport switch (cycle 183, part 2) ────────────────────────────

// countingGatewayBackend implements backend.Client and describes itself the way
// *backend.GatewayClient does, so nativeProvider derives a provider from it. The
// StreamFn path is poisoned: reaching it is a test failure, not a silent
// fallback.
type countingGatewayBackend struct {
	cfg     backend.GatewayConfig
	mu      sync.Mutex
	streams int
}

func (c *countingGatewayBackend) GatewayConfig() backend.GatewayConfig { return c.cfg }

func (c *countingGatewayBackend) Stream(context.Context, backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	c.mu.Lock()
	c.streams++
	c.mu.Unlock()
	out := make(chan backend.StreamChunk, 1)
	out <- backend.StreamChunk{Error: errors.New("the StreamFn path ran; the derived provider was ignored"), Done: true}
	close(out)
	return out, nil
}

func (c *countingGatewayBackend) streamCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.streams
}

func TestNativeProviderDerivesOnlyFromGatewayClients(t *testing.T) {
	client := backend.NewGatewayWithConfig(backend.GatewayConfig{
		URL:            "http://127.0.0.1:1/v1/chat/completions",
		ExternalModel:  "MiniMax-M3",
		TimeoutSeconds: 7,
	})
	provider := nativeProvider(client)
	if provider == nil {
		t.Fatal("nativeProvider(*backend.GatewayClient) = nil")
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

// TestLoop_GatewayBackendGoesThroughTheDerivedProvider is the switch pic: a backend
// that only describes itself still gets served, by the provider derived from its
// config, and the requested Antigravity model name is replaced by the gateway-side
// ExternalModel on the wire — exactly what GatewayClient does.
func TestLoop_GatewayBackendGoesThroughTheDerivedProvider(t *testing.T) {
	gateway := newScriptedGateway(t, []string{gatewayText("hi"), gatewayFinish("stop"), "[DONE]"})
	client := &countingGatewayBackend{cfg: backend.GatewayConfig{
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

// TestLoop_GatewayBackendFailureKeepsTheHTTPError pins the item that motivated the
// transport switch: cmd/wagyu/api_error.go classifies a turn with
// errors.As(err, &he) to reproduce the reference's message and exit code, and
// PiG copies only err.Error() across its stream boundary.
func TestLoop_GatewayBackendFailureKeepsTheHTTPError(t *testing.T) {
	gateway := newScriptedGateway(t)
	gateway.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream is down"))
	})

	var sawChunkError error
	l, err := New(Options{
		Backend:      backend.NewGatewayWithConfig(backend.GatewayConfig{URL: gateway.server.URL, ExternalModel: "MiniMax-M3"}),
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
