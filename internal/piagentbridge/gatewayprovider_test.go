package piagentbridge

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

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/tools"
)

// ─── fake gateway ────────────────────────────────────────────────────────

type fakeGateway struct {
	server  *httptest.Server
	mu      sync.Mutex
	turns   [][]string // one []string of SSE data payloads per request
	bodies  []chatRequest
	headers []http.Header
	status  int
	rawBody string
	// rawSSE, when set, is written verbatim instead of the per-turn data
	// payloads. It exists for the `event: error` tests, which need an SSE
	// line the `data: <payload>` writer cannot produce.
	rawSSE string
}

func newFakeGateway(t *testing.T, turns ...[]string) *fakeGateway {
	t.Helper()
	gateway := &fakeGateway{turns: turns}
	gateway.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gateway.mu.Lock()
		index := len(gateway.bodies)
		var body chatRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		gateway.bodies = append(gateway.bodies, body)
		gateway.headers = append(gateway.headers, r.Header.Clone())
		status := gateway.status
		raw := gateway.rawBody
		rawSSE := gateway.rawSSE
		var payloads []string
		if index < len(gateway.turns) {
			payloads = gateway.turns[index]
		}
		gateway.mu.Unlock()

		if status != 0 {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(raw))
			return
		}

		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		if rawSSE != "" {
			_, _ = w.Write([]byte(rawSSE))
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
		for _, payload := range payloads {
			fmt.Fprintf(w, "data: %s\n\n", payload)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	t.Cleanup(gateway.server.Close)
	return gateway
}

func (g *fakeGateway) provider(t *testing.T) *Provider {
	t.Helper()
	return NewProvider(ProviderConfig{
		URL:    g.server.URL,
		APIKey: "test-key",
		Model:  "MiniMax-M3",
	})
}

func (g *fakeGateway) requests() []chatRequest {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]chatRequest(nil), g.bodies...)
}

func contentFrame(text string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, text)
}

func usageFrame(prompt, completion, reasoning int) string {
	return fmt.Sprintf(`{"choices":[],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"total_tokens":%d,"completion_tokens_details":{"reasoning_tokens":%d}}}`,
		prompt, completion, prompt+completion, reasoning)
}

func finishFrame(reason string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{},"finish_reason":%q}]}`, reason)
}

func toolFragmentFrame(index int, id, name, arguments string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":%d,"id":%q,"type":"function","function":{"name":%q,"arguments":%q}}]}}]}`,
		index, id, name, arguments)
}

func toolArgsFrame(index int, arguments string) string {
	return fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":%d,"function":{"arguments":%q}}]}}]}`, index, arguments)
}

// ─── provider streaming ──────────────────────────────────────────────────────

func TestProvider_StreamsTextAndUsage(t *testing.T) {
	gateway := newFakeGateway(t, []string{
		contentFrame("Hello"),
		contentFrame(", world"),
		finishFrame("stop"),
		usageFrame(11, 3, 2),
		"[DONE]",
	})

	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("hi")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events := collect(t, stream)

	want := []string{"start", "text_start", "text_delta", "text_delta", "text_end", "done"}
	if got := eventTypes(events); !equalStrings(got, want) {
		t.Errorf("event types = %v, want %v", got, want)
	}
	if got := strings.Join(textChunks(events), ""); got != "Hello, world" {
		t.Errorf("text = %q", got)
	}

	result := stream.Result()
	if result.StopReason != pi.StopReasonStop {
		t.Errorf("StopReason = %q", result.StopReason)
	}
	if result.Usage.Input != 11 || result.Usage.Output != 3 || result.Usage.TotalTokens != 14 {
		t.Errorf("Usage = %+v", result.Usage)
	}
	if result.Usage.Reasoning == nil || *result.Usage.Reasoning != 2 {
		t.Errorf("Reasoning = %v", result.Usage.Reasoning)
	}
	if result.Provider != ProviderID || result.API != ProviderAPI {
		t.Errorf("provenance = %q/%q", result.Provider, result.API)
	}
}

func TestProvider_StreamsFragmentedToolCall(t *testing.T) {
	gateway := newFakeGateway(t, []string{
		contentFrame("let me look"),
		toolFragmentFrame(0, "call_1", "read_file", `{"pa`),
		toolArgsFrame(0, `th":"go.mod"}`),
		finishFrame("tool_calls"),
		"[DONE]",
	})

	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("read it")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events := collect(t, stream)

	want := []string{
		"start", "text_start", "text_delta", "text_end",
		"toolcall_start", "toolcall_delta", "toolcall_delta", "toolcall_end",
		"done",
	}
	if got := eventTypes(events); !equalStrings(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}

	result := stream.Result()
	if result.StopReason != pi.StopReasonToolUse {
		t.Errorf("StopReason = %q, want toolUse", result.StopReason)
	}
	var call pi.ToolCall
	for _, block := range result.Content {
		if typed, ok := block.(pi.ToolCall); ok {
			call = typed
		}
	}
	if call.ID != "call_1" || call.Name != "read_file" {
		t.Errorf("tool call = %+v", call)
	}
	if call.Arguments["path"] != "go.mod" {
		t.Errorf("arguments = %+v", call.Arguments)
	}
}

func TestProvider_StopReasonLength(t *testing.T) {
	gateway := newFakeGateway(t, []string{contentFrame("truncated"), finishFrame("length"), "[DONE]"})
	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	collect(t, stream)
	if got := stream.Result().StopReason; got != pi.StopReasonLength {
		t.Errorf("StopReason = %q, want length", got)
	}
}

func TestProvider_HTTPErrorIsReturnedConcrete(t *testing.T) {
	gateway := newFakeGateway(t)
	gateway.status = http.StatusBadGateway
	gateway.rawBody = "upstream is down"

	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err == nil {
		t.Fatalf("Stream = nil error, want the HTTP failure; stream=%v", stream)
	}
	if stream != nil {
		t.Errorf("Stream returned a stream alongside the error")
	}
	// The concrete type is the whole point: cmd/wagyu/api_error.go classifies
	// the turn with errors.As(err, &he).
	var httpErr *backend.HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error %v (%T) is not a *backend.HTTPError", err, err)
	}
	if httpErr.StatusCode != http.StatusBadGateway {
		t.Errorf("StatusCode = %d", httpErr.StatusCode)
	}
	if !strings.Contains(httpErr.Message(), "upstream is down") {
		t.Errorf("Message() = %q", httpErr.Message())
	}
	if httpErr.URL != gateway.server.URL {
		t.Errorf("URL = %q, want %q", httpErr.URL, gateway.server.URL)
	}
}

func TestProvider_MidStreamCancellationAborts(t *testing.T) {
	// A gateway that sends one frame and then holds the connection open until
	// the caller gives up. Cancelling after the first frame exercises the
	// post-stream path, which has no error left to return and must therefore
	// use an ErrorEvent.
	release := make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\n", contentFrame("partial"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() {
		once.Do(func() { close(release) })
		server.Close()
	})

	provider := NewProvider(ProviderConfig{URL: server.URL, APIKey: "test-key", Model: "MiniMax-M3"})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	stream, err := provider.Stream(ctx, transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	// Wait for the first delta, then pull the rug.
	for event := range stream.Events(context.Background()) {
		if _, ok := event.(pi.TextDeltaEvent); ok {
			break
		}
	}
	cancel()

	events := collect(t, stream)
	last := events[len(events)-1]
	if _, ok := last.(pi.ErrorEvent); !ok {
		t.Fatalf("last event = %T, want ErrorEvent", last)
	}
	if got := stream.Result().StopReason; got != pi.StopReasonAborted {
		t.Errorf("StopReason = %q, want aborted", got)
	}
}

func TestProvider_ContextCanceledBeforeRequest(t *testing.T) {
	gateway := newFakeGateway(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stream, err := gateway.provider(t).Stream(ctx, transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err == nil {
		t.Fatalf("Stream = nil error, want the cancellation; stream=%v", stream)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if requests := gateway.requests(); len(requests) != 0 {
		t.Errorf("the gateway was called anyway: %d request(s)", len(requests))
	}
}

// TestProvider_EmptyBodySurfacesError pins cycle
// gatewayprovider-eof-without-done: when the gateway closes the stream
// without sending ANY frame and without sending [DONE], the stream
// must surface an error rather than pretending the empty payload is
// a clean turn. Pre-cycle the body-end path returned "" and
// streamState.finish took its clean-stop branch, so chunkSink ended
// in StateDone with no error — the TUI showed an empty transcript
// and the user had to type "continue" to recover. Post-cycle
// pumpScan returns a diagnostic on EOF-without-[DONE]-and-no-frames,
// the finish path takes the error branch, the chunkSink emits
// StreamChunk{Error, Done: true}, and the TUI surfaces the failure
// inline.
func TestProvider_EmptyBodySurfacesError(t *testing.T) {
	gateway := newFakeGateway(t, nil) // zero frames
	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events := collect(t, stream)
	if got := eventTypes(events); got[len(got)-1] != "error" {
		t.Errorf("event types = %v, want a trailing error", got)
	}
	result := stream.Result()
	if result.StopReason != pi.StopReasonError {
		t.Errorf("StopReason = %v, want StopReasonError", result.StopReason)
	}
	if !strings.Contains(result.ErrorMessage, "[DONE]") {
		t.Errorf("ErrorMessage = %q; want it to mention [DONE]", result.ErrorMessage)
	}
	if got := assistantText(result.Content); got != "" {
		t.Errorf("text = %q; want empty (no frames were streamed)", got)
	}
}

// TestProvider_ErrorFrameSurfacesError covers the upstream-failure shape
// the empty-turn bug was most likely to take: the gateway answers with a
// record carrying a top-level "error" member and then closes the stream.
// PiG's OpenAI decoder treats that member as a hard APIError; the clone
// used to ignore it, so the record counted as "one frame" and the run
// ended as a silent clean stop with an empty transcript (measured before
// the fix: events [start done], StopReason=stop, text="").
func TestProvider_ErrorFrameSurfacesError(t *testing.T) {
	gateway := newFakeGateway(t, []string{`{"error":{"message":"upstream exploded","type":"server_error"}}`})
	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events := collect(t, stream)
	if got := eventTypes(events); got[len(got)-1] != "error" {
		t.Fatalf("event types = %v, want a trailing error", got)
	}
	result := stream.Result()
	if result.StopReason != pi.StopReasonError {
		t.Errorf("StopReason = %v, want StopReasonError", result.StopReason)
	}
	if !strings.Contains(result.ErrorMessage, "upstream exploded") {
		t.Errorf("ErrorMessage = %q; want the gateway's message", result.ErrorMessage)
	}
}

// TestProvider_ErrorEventSurfacesError covers the `event: error` SSE form,
// where the record itself need not carry an "error" member. PiG's decoder
// throws the SDK's APIError for that event too; ours must not fall through
// to a clean stop.
func TestProvider_ErrorEventSurfacesError(t *testing.T) {
	gateway := newFakeGateway(t)
	gateway.rawSSE = "event: error\ndata: {\"message\":\"relay refused the request\"}\n\n"
	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events := collect(t, stream)
	if got := eventTypes(events); got[len(got)-1] != "error" {
		t.Fatalf("event types = %v, want a trailing error", got)
	}
	result := stream.Result()
	if result.StopReason != pi.StopReasonError {
		t.Errorf("StopReason = %v, want StopReasonError", result.StopReason)
	}
	if !strings.Contains(result.ErrorMessage, "relay refused the request") {
		t.Errorf("ErrorMessage = %q; want the relay's message", result.ErrorMessage)
	}
}

// TestProvider_ErrorFrameAfterTextPreservesText pins the trade-off: the
// text streamed before the error record stays in the transcript, and the
// diagnostic rides below it. No rollback.
func TestProvider_ErrorFrameAfterTextPreservesText(t *testing.T) {
	gateway := newFakeGateway(t, []string{contentFrame("partial answer"), `{"error":{"message":"boom"}}`})
	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events := collect(t, stream)
	if got := eventTypes(events); got[len(got)-1] != "error" {
		t.Fatalf("event types = %v, want a trailing error", got)
	}
	result := stream.Result()
	if got := assistantText(result.Content); got != "partial answer" {
		t.Errorf("text = %q; want the streamed prefix preserved", got)
	}
	if !strings.Contains(result.ErrorMessage, "boom") {
		t.Errorf("ErrorMessage = %q; want the gateway's message", result.ErrorMessage)
	}
}

// TestProvider_FalsyErrorMemberIsIgnored pins PiG's truthiness rule: a
// null/false/0/"" error member is not a failure, and the record's choices
// are consumed normally.
func TestProvider_FalsyErrorMemberIsIgnored(t *testing.T) {
	gateway := newFakeGateway(t, []string{
		`{"error":null,"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`,
		"[DONE]",
	})
	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events := collect(t, stream)
	if got := eventTypes(events); got[len(got)-1] != "done" {
		t.Fatalf("event types = %v, want a trailing done", got)
	}
	if got := assistantText(stream.Result().Content); got != "ok" {
		t.Errorf("text = %q, want ok", got)
	}
	if got := stream.Result().StopReason; got != pi.StopReasonStop {
		t.Errorf("StopReason = %v, want StopReasonStop", got)
	}
}

// TestStreamErrorText pins the APIError.makeMessage extraction directly:
// string message, JSON of a message-less object, a bare JSON string, and
// the SDK fallback for a null event payload.
func TestStreamErrorText(t *testing.T) {
	cases := []struct {
		name     string
		member   string
		payload  string
		isEvent  bool
		wantText string
	}{
		{"object message", `{"message":"m"}`, `{}`, false, "m"},
		{"object without message", `{"code":"c"}`, `{}`, false, `{"code":"c"}`},
		{"bare string", `"s"`, `{}`, false, `"s"`},
		{"null member, normal record", `null`, `{}`, false, ""},
		{"null member, error event", `null`, `null`, true, "(no status code or body)"},
		{"absent member, error event", ``, `{"message":"outer"}`, true, "outer"},
		{"absent member, normal record", ``, `{}`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var member json.RawMessage
			if tc.member != "" {
				member = json.RawMessage(tc.member)
			}
			if got := streamErrorText(member, tc.payload, tc.isEvent); got != tc.wantText {
				t.Errorf("streamErrorText(%s, %s, %v) = %q, want %q",
					tc.member, tc.payload, tc.isEvent, got, tc.wantText)
			}
		})
	}
}

// TestProvider_BodyEndingWithoutDoneWithFramesStillTerminates pins
// back-compat with the the configured gateway default behaviour: the gateway
// closes the SSE stream after the final usage frame without sending
// an explicit [DONE] sentinel. As long as at least one frame
// reached the scanner, the run is a clean stop and the streamed
// text is preserved verbatim. Cycle gatewayprovider-eof-without-done
// deliberately keeps this path unchanged because changing it would
// mis-report every normal the configured gateway request as a stream error
// (cmd/wagyu's scriptedGateway test helper and the production
// the configured gateway both rely on this contract).
func TestProvider_BodyEndingWithoutDoneWithFramesStillTerminates(t *testing.T) {
	gateway := newFakeGateway(t, []string{contentFrame("no done marker")})
	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events := collect(t, stream)
	if got := eventTypes(events); got[len(got)-1] != "done" {
		t.Errorf("event types = %v, want a trailing done (back-compat with the configured gateway)", got)
	}
	if got := assistantText(stream.Result().Content); got != "no done marker" {
		t.Errorf("text = %q", got)
	}
}

func TestProvider_SkipsUndecodableFrames(t *testing.T) {
	gateway := newFakeGateway(t, []string{
		"this is not json",
		": keep-alive comment",
		"",
		contentFrame("survived"),
		"[DONE]",
	})
	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	collect(t, stream)
	if got := assistantText(stream.Result().Content); got != "survived" {
		t.Errorf("text = %q", got)
	}
}

// ─── request shape ───────────────────────────────────────────────────────────

func TestProvider_SendsOpenAIRequestBody(t *testing.T) {
	gateway := newFakeGateway(t, []string{contentFrame("ok"), "[DONE]"})

	transcript := pi.NormalizeContext(pi.Context{
		SystemPrompt: "be terse",
		Tools: []pi.ToolSchema{{
			Name:        "read_file",
			Description: "Read a file.",
			Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
		}},
		Messages: []pi.Message{
			pi.UserMessage{Content: pi.UserText("read go.mod")},
			pi.AssistantMessage{Content: []pi.AssistantContentBlock{
				pi.ToolCall{ID: "call_1", Name: "read_file", Arguments: pi.JsonObject{"path": "go.mod"}},
			}},
			pi.ToolResultMessage{
				ToolCallID: "call_1",
				ToolName:   "read_file",
				Content:    []pi.ToolResultMessageContent{pi.TextContent{Text: "module x"}},
			},
		},
	})

	stream, err := gateway.provider(t).Stream(context.Background(), transcript, pi.StreamOptions{MaxTokens: 512, Temperature: 0.3, TemperatureSet: true})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	collect(t, stream)

	requests := gateway.requests()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	got := requests[0]

	if got.Model != "MiniMax-M3" {
		t.Errorf("Model = %q", got.Model)
	}
	if !got.Stream || got.StreamOptions == nil || !got.StreamOptions.IncludeUsage {
		t.Errorf("stream/stream_options = %v/%+v", got.Stream, got.StreamOptions)
	}
	if got.MaxTokens != 512 {
		t.Errorf("MaxTokens = %d", got.MaxTokens)
	}
	if got.Temperature == nil || *got.Temperature != 0.3 {
		t.Errorf("Temperature = %v", got.Temperature)
	}
	if len(got.Tools) != 1 || got.Tools[0].Type != "function" || got.Tools[0].Function.Name != "read_file" {
		t.Fatalf("Tools = %+v", got.Tools)
	}
	if got.Tools[0].Function.Description != "Read a file." {
		t.Errorf("tool description = %q", got.Tools[0].Function.Description)
	}

	if len(got.Messages) != 4 {
		t.Fatalf("Messages = %d, want system+user+assistant+tool: %+v", len(got.Messages), got.Messages)
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "be terse" {
		t.Errorf("messages[0] = %+v", got.Messages[0])
	}
	if got.Messages[2].Role != "assistant" || len(got.Messages[2].ToolCalls) != 1 {
		t.Fatalf("messages[2] = %+v", got.Messages[2])
	}
	call := got.Messages[2].ToolCalls[0]
	if call.ID != "call_1" || call.Type != "function" || call.Function.Name != "read_file" || call.Function.Arguments != `{"path":"go.mod"}` {
		t.Errorf("tool call = %+v", call)
	}
	if got.Messages[3].Role != "tool" || got.Messages[3].ToolCallID != "call_1" || got.Messages[3].Content != "module x" {
		t.Errorf("messages[3] = %+v", got.Messages[3])
	}

	headers := gateway.headers[0]
	if headers.Get("authorization") != "Bearer test-key" {
		t.Errorf("authorization = %q", headers.Get("authorization"))
	}
	if headers.Get("accept") != "text/event-stream" {
		t.Errorf("accept = %q", headers.Get("accept"))
	}
	// The upstream must not be able to tell the provider from the clone's own
	// gateway client, so the identity header is shared rather than duplicated.
	if got := headers.Get("User-Agent"); got != backend.GatewayUserAgent {
		t.Errorf("User-Agent = %q, want %q", got, backend.GatewayUserAgent)
	}
}

// TestProvider_SendsAnImageInTheContentArray pins the picture's way
// to a configured endpoint: the OpenAI content array, with the image
// part's url a `data:` URL carrying the base64 payload.
//
// 【一手】 the shape is the OpenAI Chat Completions one, and it is the
// shape PiG's own OpenAI dialect writes (ai/openai.go). The clone's
// own transport writes the same array
// (backend.formatOpenAIContentParts), so a picture reaches a gateway
// the same way whichever transport carries it.
func TestProvider_SendsAnImageInTheContentArray(t *testing.T) {
	gateway := newFakeGateway(t, []string{contentFrame("ok"), "[DONE]"})

	transcript := pi.NormalizeContext(pi.Context{
		Messages: []pi.Message{
			pi.UserMessage{Content: pi.UserContentBlocks{
				pi.TextContent{Text: "what color?"},
				pi.ImageContent{Data: "aGk=", MimeType: "image/png"},
			}},
		},
	})

	stream, err := gateway.provider(t).Stream(context.Background(), transcript, pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	collect(t, stream)

	requests := gateway.requests()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	parts, ok := requests[0].Messages[0].Content.([]any)
	if !ok {
		t.Fatalf("the user message carries no content array: %+v", requests[0].Messages[0])
	}
	if len(parts) != 2 {
		t.Fatalf("the content array holds %d parts, want the text and the picture: %+v", len(parts), parts)
	}
	text, _ := parts[0].(map[string]any)
	if text["type"] != "text" || text["text"] != "what color?" {
		t.Errorf("parts[0] = %+v, want the text part", text)
	}
	image, _ := parts[1].(map[string]any)
	if image["type"] != "image_url" {
		t.Fatalf("parts[1] = %+v, want the image part", image)
	}
	payload, _ := image["image_url"].(map[string]any)
	if payload["url"] != "data:image/png;base64,aGk=" {
		t.Errorf("the image url = %v, want the data url of the picture", payload["url"])
	}
}

// TestProvider_KeepsTheStringContentForATextOnlyMessage pins the other
// half: a message with no picture keeps the string form, so the request
// the clone sent before the picture support is the request it sends now.
func TestProvider_KeepsTheStringContentForATextOnlyMessage(t *testing.T) {
	gateway := newFakeGateway(t, []string{contentFrame("ok"), "[DONE]"})

	transcript := pi.NormalizeContext(pi.Context{
		Messages: []pi.Message{
			pi.UserMessage{Content: pi.UserContentBlocks{pi.TextContent{Text: "read go.mod"}}},
		},
	})

	stream, err := gateway.provider(t).Stream(context.Background(), transcript, pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	collect(t, stream)

	requests := gateway.requests()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	content, ok := requests[0].Messages[0].Content.(string)
	if !ok || content != "read go.mod" {
		t.Errorf("content = %+v, want the plain text", requests[0].Messages[0].Content)
	}
}

func TestProvider_RejectsEmptyModel(t *testing.T) {
	gateway := newFakeGateway(t)
	provider := gateway.provider(t)
	provider.cfg.Model = ""

	stream, err := provider.Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err == nil {
		t.Fatalf("Stream = nil error, want the empty-model rejection; stream=%v", stream)
	}
	if !strings.Contains(err.Error(), "model is required") {
		t.Errorf("error = %q", err)
	}
	if requests := gateway.requests(); len(requests) != 0 {
		t.Errorf("the gateway was called anyway: %d request(s)", len(requests))
	}
}

// TestProvider_WireModelOverridesRequestedLabel pins the ExternalModel
// semantics: whatever label the caller binds, the request carries the
// configured model. GatewayClient does the same, and cmd/wagyu asks for
// Antigravity names like "claude-sonnet-4-6" that gateway does not serve.
func TestProvider_WireModelOverridesRequestedLabel(t *testing.T) {
	gateway := newFakeGateway(t, []string{contentFrame("ok"), "[DONE]"})
	provider := gateway.provider(t)

	bound := provider.Model("claude-sonnet-4-6")
	if bound.ID != "claude-sonnet-4-6" {
		t.Errorf("label = %q, want the requested one", bound.ID)
	}
	if _, err := bound.Provider.Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{}); err != nil {
		t.Fatalf("Stream: %v", err)
	}

	requests := gateway.requests()
	if len(requests) != 1 {
		t.Fatalf("requests = %d", len(requests))
	}
	if requests[0].Model != "MiniMax-M3" {
		t.Errorf("wire model = %q, want the configured MiniMax-M3", requests[0].Model)
	}
	if got := provider.WireModel(); got != "MiniMax-M3" {
		t.Errorf("WireModel() = %q", got)
	}
}

// TestProvider_NoBuiltInEndpoint pins that the provider invents nothing:
// the endpoint, the credential and the wire model are configuration, and a
// zero-value config therefore describes no upstream at all.
func TestProvider_NoBuiltInEndpoint(t *testing.T) {
	t.Setenv("OPENAGY_GATEWAY_URL", "")
	t.Setenv("OPENAGY_GATEWAY_KEY", "")
	provider := NewProvider(ProviderConfig{})
	if provider.Endpoint() != "" {
		t.Errorf("URL = %q, want empty", provider.Endpoint())
	}
	if provider.cfg.APIKey != "" {
		t.Errorf("APIKey = %q, want empty", provider.cfg.APIKey)
	}
	if provider.cfg.Model != "" {
		t.Errorf("Model = %q, want empty", provider.cfg.Model)
	}
	if provider.ID() != ProviderID {
		t.Errorf("ID = %q", provider.ID())
	}
	if err := provider.Close(); err != nil {
		t.Errorf("Close = %v", err)
	}
}

func TestProvider_ModelBindsProviderAndModelID(t *testing.T) {
	provider := NewProvider(ProviderConfig{Model: "fallback"})

	bound := provider.Model("MiniMax-M3")
	if bound.ID != "MiniMax-M3" || bound.DisplayName != "MiniMax-M3" {
		t.Errorf("model = %+v", bound)
	}
	if bound.Provider == nil {
		t.Fatal("Provider = nil")
	}
	if bound.Provider.ID() != ProviderID {
		t.Errorf("provider ID = %q", bound.Provider.ID())
	}
	if bound.ProviderMeta.API != ProviderAPI {
		t.Errorf("api = %q", bound.ProviderMeta.API)
	}
	if fallback := provider.Model(""); fallback.ID != "fallback" {
		t.Errorf("empty id = %q, want the configured default", fallback.ID)
	}
}

// ─── the loop over the native provider ───────────────────────────────────────

func TestNewAgent_NativeProviderDrivesTheToolRoundTrip(t *testing.T) {
	gateway := newFakeGateway(t,
		[]string{
			toolFragmentFrame(0, "call_1", "read_file", `{"path":"go.mod"}`),
			finishFrame("tool_calls"),
			"[DONE]",
		},
		[]string{
			contentFrame("the module is "),
			contentFrame("github.com/justwasm/pigasus"),
			finishFrame("stop"),
			"[DONE]",
		},
	)

	inner := &stubTool{name: "read_file", result: "module github.com/justwasm/pigasus\n"}
	registry := tools.NewRegistry()
	registry.Register(inner)

	agent := mustNewAgent(t, Options{
		Provider: gateway.provider(t),
		ModelID:  "MiniMax-M3",
		Registry: registry,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	messages, err := agent.Send(ctx, "what module is this?")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if got := inner.invocations(); len(got) != 1 || got[0] != `{"path":"go.mod"}` {
		t.Fatalf("tool invocations = %v", got)
	}
	requests := gateway.requests()
	if len(requests) != 2 {
		t.Fatalf("upstream calls = %d, want 2", len(requests))
	}
	if requests[0].Model != "MiniMax-M3" {
		t.Errorf("model = %q", requests[0].Model)
	}
	if len(requests[0].Tools) != 1 || requests[0].Tools[0].Function.Name != "read_file" {
		t.Errorf("advertised tools = %+v", requests[0].Tools)
	}
	var sawToolResult bool
	for _, message := range requests[1].Messages {
		if message.Role == "tool" && message.ToolCallID == "call_1" {
			sawToolResult = true
		}
	}
	if !sawToolResult {
		t.Errorf("follow-up has no tool result: %+v", requests[1].Messages)
	}

	last := messages[len(messages)-1]
	if last.Assistant == nil {
		t.Fatalf("final message = %+v", last)
	}
	if got := assistantText(last.Assistant.Content); got != "the module is github.com/justwasm/pigasus" {
		t.Errorf("final text = %q", got)
	}
}

func TestNewAgent_PrefersProviderOverClient(t *testing.T) {
	gateway := newFakeGateway(t, []string{contentFrame("from the provider"), finishFrame("stop"), "[DONE]"})

	client := &fakeClient{turns: [][]backend.StreamChunk{{{Text: "from the client"}, {Done: true}}}}
	agent := mustNewAgent(t, Options{
		Client:   client,
		Provider: gateway.provider(t),
		ModelID:  "MiniMax-M3",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	messages, err := agent.Send(ctx, "go")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	last := messages[len(messages)-1]
	if got := assistantText(last.Assistant.Content); got != "from the provider" {
		t.Errorf("text = %q, want the provider path", got)
	}
	if requests := client.requests(); len(requests) != 0 {
		t.Errorf("the clone's Client was called %d time(s)", len(requests))
	}
}

func TestNewAgent_NativeProviderUsesPiGsProviderPath(t *testing.T) {
	gateway := newFakeGateway(t, []string{contentFrame("hi"), finishFrame("stop"), "[DONE]"})
	agent := mustNewAgent(t, Options{Provider: gateway.provider(t), ModelID: "MiniMax-M3"})

	// PiG 0.5.1 no longer falls back to model.Provider.Stream when neither
	// StreamFn nor DefaultStreamFn is installed, so the native path installs
	// ProviderStreamFn itself; a non-nil provider on the model is what that
	// function dispatches to.
	if agent.Model().Provider == nil {
		t.Fatal("agent model has no provider: the loop would not reach gateway")
	}
}

func transcriptOf(content pi.UserContent) pi.TranscriptContext {
	return pi.NormalizeContext(pi.Context{Messages: []pi.Message{pi.UserMessage{Content: content}}})
}

func TestProvider_EmitsToolCallEndOnlyOncePerCall(t *testing.T) {
	gateway := newFakeGateway(t, []string{
		toolFragmentFrame(0, "call_a", "read_file", `{"path":"a"}`),
		toolFragmentFrame(1, "call_b", "read_file", `{"path":"b"}`),
		finishFrame("tool_calls"),
		"[DONE]",
	})
	stream, err := gateway.provider(t).Stream(context.Background(), transcriptOf(pi.UserText("x")), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	events := collect(t, stream)

	ends := 0
	var ids []string
	for _, event := range events {
		if end, ok := event.(pi.ToolCallEndEvent); ok {
			ends++
			ids = append(ids, end.ToolCall.ID)
		}
	}
	if ends != 2 {
		t.Errorf("toolcall_end events = %d, want 2", ends)
	}
	if !equalStrings(ids, []string{"call_a", "call_b"}) {
		t.Errorf("ids = %v", ids)
	}
}

// TestProvider_SatisfiesAiProvider pins the interface at compile time.
func TestProvider_SatisfiesAiProvider(t *testing.T) {
	var _ pi.Provider = NewProvider(ProviderConfig{})
	var _ piagent.AgentTool = Tool(&stubTool{name: "x"})
}
