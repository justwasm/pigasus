package piagentbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/tools"
)

// ─── fixtures ────────────────────────────────────────────────────────────────

// mustNewAgent is NewAgent with the error checked. PiG 0.5.1's
// agent.NewAgent returns ErrNoDefaultStreamFunction for an Options that
// installs neither transport, which every Options here avoids.
func mustNewAgent(t *testing.T, opts Options) *piagent.Agent {
	t.Helper()
	agent, err := NewAgent(opts)
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	return agent
}

// fakeClient is a backend.Client whose responses are scripted per turn.
type fakeClient struct {
	mu      sync.Mutex
	turns   [][]backend.StreamChunk
	history []backend.StreamRequest
}

func (c *fakeClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	c.mu.Lock()
	c.history = append(c.history, req)
	turn := len(c.history) - 1
	chunks := []backend.StreamChunk{}
	if turn < len(c.turns) {
		chunks = c.turns[turn]
	}
	c.mu.Unlock()

	out := make(chan backend.StreamChunk, len(chunks)+1)
	go func() {
		defer close(out)
		for _, chunk := range chunks {
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func (c *fakeClient) requests() []backend.StreamRequest {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]backend.StreamRequest(nil), c.history...)
}

// stubTool is a tools.Tool that records its invocations.
type stubTool struct {
	name     string
	schema   string
	seenArgs []string
	result   any
	err      error
}

func (t *stubTool) Name() string { return t.name }
func (t *stubTool) Schema() json.RawMessage {
	if t.schema == "" {
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return json.RawMessage(t.schema)
}
func (t *stubTool) Run(_ context.Context, args json.RawMessage) (any, error) {
	t.seenArgs = append(t.seenArgs, string(args))
	if t.err != nil {
		return nil, t.err
	}
	return t.result, nil
}

func (t *stubTool) invocations() []string { return append([]string(nil), t.seenArgs...) }

func collect(t *testing.T, stream *pi.AssistantMessageEventStream) []pi.AssistantMessageEvent {
	t.Helper()
	var events []pi.AssistantMessageEvent
	for event := range stream.Events(context.Background()) {
		events = append(events, event)
	}
	return events
}

func eventTypes(events []pi.AssistantMessageEvent) []string {
	out := make([]string, 0, len(events))
	for _, event := range events {
		out = append(out, string(event.EventType()))
	}
	return out
}

func textChunks(events []pi.AssistantMessageEvent) []string {
	var out []string
	for _, event := range events {
		if delta, ok := event.(pi.TextDeltaEvent); ok {
			out = append(out, delta.Delta)
		}
	}
	return out
}

// ─── transcript conversion ───────────────────────────────────────────────────

func TestToStreamRequest_MapsEveryMessageKind(t *testing.T) {
	transcript := pi.NormalizeContext(pi.Context{
		SystemPrompt: "be terse",
		Tools: []pi.ToolSchema{{
			Name:        "read_file",
			Description: "read a file",
			Parameters:  map[string]any{"type": "object"},
		}},
		Messages: []pi.Message{
			pi.UserMessage{Content: pi.UserText("read go.mod")},
			pi.AssistantMessage{Content: []pi.AssistantContentBlock{
				pi.TextContent{Text: "sure"},
				pi.ToolCall{ID: "call_1", Name: "read_file", Arguments: pi.JsonObject{"path": "go.mod"}},
			}},
			pi.ToolResultMessage{
				ToolCallID: "call_1",
				ToolName:   "read_file",
				Content:    []pi.ToolResultMessageContent{pi.TextContent{Text: "module x"}},
			},
		},
	})

	got := ToStreamRequest(transcript, "claude-sonnet-4-6", pi.StreamOptions{MaxTokens: 4096, Temperature: 0.2})

	if got.Model != "claude-sonnet-4-6" {
		t.Errorf("Model = %q", got.Model)
	}
	if got.SystemPrompt != "be terse" {
		t.Errorf("SystemPrompt = %q", got.SystemPrompt)
	}
	if got.MaxTokens != 4096 || got.Temperature != 0.2 {
		t.Errorf("MaxTokens/Temperature = %d/%v", got.MaxTokens, got.Temperature)
	}
	if len(got.Tools) != 1 || got.Tools[0].Name != "read_file" || got.Tools[0].Description != "read a file" {
		t.Fatalf("Tools = %+v", got.Tools)
	}
	if len(got.Messages) != 3 {
		t.Fatalf("Messages = %d, want 3: %+v", len(got.Messages), got.Messages)
	}
	if got.Messages[0].Role != "user" || got.Messages[0].Content != "read go.mod" {
		t.Errorf("messages[0] = %+v", got.Messages[0])
	}
	if got.Messages[1].Role != "assistant" || got.Messages[1].Content != "sure" {
		t.Errorf("messages[1] = %+v", got.Messages[1])
	}
	if len(got.Messages[1].ToolCalls) != 1 {
		t.Fatalf("messages[1].ToolCalls = %+v", got.Messages[1].ToolCalls)
	}
	if call := got.Messages[1].ToolCalls[0]; call.ID != "call_1" || call.Name != "read_file" || string(call.Arguments) != `{"path":"go.mod"}` {
		t.Errorf("tool call = %+v", call)
	}
	if got.Messages[2].Role != "tool" || got.Messages[2].ToolCallID != "call_1" || got.Messages[2].Name != "read_file" {
		t.Errorf("messages[2] = %+v", got.Messages[2])
	}
	if got.Messages[2].Content != "module x" {
		t.Errorf("messages[2].Content = %q", got.Messages[2].Content)
	}
}

// TestToStreamRequest_CarriesImageContent pins that a picture the
// user attached rides with the message.
//
// The clone used to drop the image blocks: the neutral message shape
// had nowhere to put them. It has a place now — backend.Message.Images
// — and every transport spells it its own way.
func TestToStreamRequest_CarriesImageContent(t *testing.T) {
	transcript := pi.NormalizeContext(pi.Context{
		Messages: []pi.Message{pi.UserMessage{Content: pi.UserContentBlocks{
			pi.TextContent{Text: "look"},
			pi.ImageContent{Data: "AAAA", MimeType: "image/png"},
		}}},
	})

	got := ToStreamRequest(transcript, "m", pi.StreamOptions{})
	if len(got.Messages) != 1 || got.Messages[0].Content != "look" {
		t.Fatalf("Messages = %+v", got.Messages)
	}
	images := got.Messages[0].Images
	if len(images) != 1 {
		t.Fatalf("the message carries %d images, want 1", len(images))
	}
	if images[0].MIMEType != "image/png" {
		t.Errorf("the image's mime type = %q, want image/png", images[0].MIMEType)
	}
	if len(images[0].Data) != 3 {
		t.Errorf("the image holds %d bytes, want the three its base64 spells", len(images[0].Data))
	}
}

// ─── stream adaptation ───────────────────────────────────────────────────────

func TestBackendStreamFn_EventSequenceAndResult(t *testing.T) {
	client := &fakeClient{turns: [][]backend.StreamChunk{{
		{Text: "hello "},
		{Text: "world"},
		{Done: true, Meta: &backend.ChunkMeta{PromptTokens: 7, CompletionTokens: 2, TotalTokens: 9, ReasoningTokens: 1}},
	}}}

	stream, err := BackendStreamFn(client, "claude-sonnet-4-6")(context.Background(), Model("claude-sonnet-4-6"), pi.TranscriptContext{}, pi.StreamOptions{})
	if err != nil {
		t.Fatalf("stream fn: %v", err)
	}
	events := collect(t, stream)

	want := []string{"start", "text_start", "text_delta", "text_delta", "text_end", "done"}
	if got := eventTypes(events); !equalStrings(got, want) {
		t.Errorf("event types = %v, want %v", got, want)
	}
	if got := strings.Join(textChunks(events), ""); got != "hello world" {
		t.Errorf("deltas = %q", got)
	}

	result := stream.Result()
	if result == nil {
		t.Fatal("Result() = nil")
	}
	if result.StopReason != pi.StopReasonStop {
		t.Errorf("StopReason = %q", result.StopReason)
	}
	if result.Usage.Input != 7 || result.Usage.Output != 2 || result.Usage.TotalTokens != 9 {
		t.Errorf("Usage = %+v", result.Usage)
	}
	if result.Usage.Reasoning == nil || *result.Usage.Reasoning != 1 {
		t.Errorf("Usage.Reasoning = %v", result.Usage.Reasoning)
	}
	if got := assistantText(result.Content); got != "hello world" {
		t.Errorf("result text = %q", got)
	}
	if result.Provider != ProviderID || result.API != BridgeAPI {
		t.Errorf("provenance = %s/%s", result.Provider, result.API)
	}
}

func TestBackendStreamFn_ToolCallsBecomeToolUse(t *testing.T) {
	client := &fakeClient{turns: [][]backend.StreamChunk{{
		{Text: "let me look"},
		{ToolCalls: []backend.ToolCall{{
			ID:        "call_9",
			Name:      "read_file",
			Arguments: json.RawMessage(`{"path":"go.mod"}`),
		}}},
		{Done: true},
	}}}

	stream, err := BackendStreamFn(client, "m")(context.Background(), Model("m"), pi.TranscriptContext{}, pi.StreamOptions{})
	if err != nil {
		t.Fatalf("stream fn: %v", err)
	}
	events := collect(t, stream)

	want := []string{"start", "text_start", "text_delta", "text_end", "toolcall_start", "toolcall_end", "done"}
	if got := eventTypes(events); !equalStrings(got, want) {
		t.Fatalf("event types = %v, want %v", got, want)
	}

	result := stream.Result()
	if result.StopReason != pi.StopReasonToolUse {
		t.Errorf("StopReason = %q, want toolUse", result.StopReason)
	}
	found := false
	for _, block := range result.Content {
		if call, ok := block.(pi.ToolCall); ok {
			found = true
			if call.ID != "call_9" || call.Name != "read_file" || call.Arguments["path"] != "go.mod" {
				t.Errorf("tool call = %+v", call)
			}
		}
	}
	if !found {
		t.Fatalf("no ToolCall block in %+v", result.Content)
	}
}

func TestBackendStreamFn_ErrorChunkBecomesErrorEvent(t *testing.T) {
	client := &fakeClient{turns: [][]backend.StreamChunk{{
		{Text: "partial"},
		{Error: errors.New("upstream exploded"), Done: true},
	}}}

	stream, err := BackendStreamFn(client, "m")(context.Background(), Model("m"), pi.TranscriptContext{}, pi.StreamOptions{})
	if err != nil {
		t.Fatalf("stream fn: %v", err)
	}
	events := collect(t, stream)

	last := events[len(events)-1]
	errorEvent, ok := last.(pi.ErrorEvent)
	if !ok {
		t.Fatalf("last event = %T, want ErrorEvent", last)
	}
	if errorEvent.Reason != pi.StopReasonError {
		t.Errorf("Reason = %q", errorEvent.Reason)
	}
	if errorEvent.Error.ErrorMessage != "upstream exploded" {
		t.Errorf("ErrorMessage = %q", errorEvent.Error.ErrorMessage)
	}
	if stream.Result() == nil {
		t.Error("Result() = nil")
	}
}

// TestBackendStreamFn_EmptyChannelSurfacesError is the backendprovider
// twin of gatewayprovider_test.go's TestProvider_EmptyBodySurfacesError
// (cycle gatewayprovider-eof-without-done): when the backend.StreamChunk
// channel closes without sending any chunk and without a terminal
// Done chunk, the stream must surface an error rather than
// pretending the empty payload is a clean turn. Pre-cycle the
// channel-end path returned stopReasonFor(partial), which is
// StopReasonStop when no tool calls were issued — the same "treat
// whatever we have as the answer" pattern that left the TUI showing
// an empty transcript. Post-cycle the channel-end path routes
// through StopReasonError with an EOF-without-Done diagnostic;
// chunkSink.endAssistant takes its StopReasonError branch and the
// TUI surfaces the failure inline.
func TestBackendStreamFn_EmptyChannelSurfacesError(t *testing.T) {
	client := &fakeClient{turns: [][]backend.StreamChunk{{}}}
	stream, err := BackendStreamFn(client, "m")(context.Background(), Model("m"), pi.TranscriptContext{}, pi.StreamOptions{})
	if err != nil {
		t.Fatalf("stream fn: %v", err)
	}
	events := collect(t, stream)
	if got := eventTypes(events); got[len(got)-1] != "error" {
		t.Errorf("event types = %v, want a trailing error", got)
	}
	result := stream.Result()
	if result.StopReason != pi.StopReasonError {
		t.Errorf("StopReason = %v, want StopReasonError", result.StopReason)
	}
	if !strings.Contains(result.ErrorMessage, "Done") {
		t.Errorf("ErrorMessage = %q; want it to mention Done", result.ErrorMessage)
	}
	if got := assistantText(result.Content); got != "" {
		t.Errorf("text = %q; want empty (no chunks were sent)", got)
	}
}

// TestBackendStreamFn_ChannelClosedWithoutDoneWithChunksStillTerminates
// pins back-compat with the the configured gateway default behaviour: the
// backend.StreamChunk channel closes after the final usage chunk
// without an explicit Done chunk. As long as at least one chunk
// reached the channel, the run is a clean stop and the streamed
// text is preserved verbatim. Cycle gatewayprovider-eof-without-done
// deliberately keeps this path unchanged because changing it would
// mis-report every normal the configured gateway request as a stream error.
func TestBackendStreamFn_ChannelClosedWithoutDoneWithChunksStillTerminates(t *testing.T) {
	client := &fakeClient{turns: [][]backend.StreamChunk{{{Text: "no done chunk"}}}}
	stream, err := BackendStreamFn(client, "m")(context.Background(), Model("m"), pi.TranscriptContext{}, pi.StreamOptions{})
	if err != nil {
		t.Fatalf("stream fn: %v", err)
	}

	events := collect(t, stream)
	if got := eventTypes(events); got[len(got)-1] != "done" {
		t.Errorf("event types = %v, want a trailing done (back-compat with the configured gateway)", got)
	}
	if got := assistantText(stream.Result().Content); got != "no done chunk" {
		t.Errorf("result text = %q", got)
	}
}

func TestBackendStreamFn_PropagatesSetupFailure(t *testing.T) {
	_, err := BackendStreamFn(backend.NewMock(), "")(context.Background(), Model(""), pi.NormalizeContext(pi.Context{}), pi.StreamOptions{})
	if err == nil {
		t.Fatal("want the empty-model error from backend.Client.Stream")
	}
	if !strings.Contains(err.Error(), "model is required") {
		t.Errorf("err = %v", err)
	}
}

// ─── tool adaptation ─────────────────────────────────────────────────────────

func TestToolAdapter_SchemaAndExecution(t *testing.T) {
	inner := &stubTool{
		name:   "read_file",
		schema: `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`,
		result: map[string]any{"content": "module x"},
	}
	adapted := Tool(inner)

	if adapted.Name() != "read_file" || adapted.Label() != "read_file" {
		t.Errorf("Name/Label = %q/%q", adapted.Name(), adapted.Label())
	}
	if adapted.ExecutionMode() != piagent.ToolModeSequential {
		t.Errorf("ExecutionMode = %q, want sequential", adapted.ExecutionMode())
	}
	schema := adapted.Schema()
	if schema.Name != "read_file" || schema.Description == "" {
		t.Errorf("Schema = %+v", schema)
	}
	if schema.Parameters["type"] != "object" {
		t.Errorf("Schema.Parameters = %+v", schema.Parameters)
	}

	got, err := adapted.Execute(context.Background(), "call_1", json.RawMessage(`{"path":"go.mod"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got.Content) != 1 {
		t.Fatalf("Content = %+v", got.Content)
	}
	text, ok := got.Content[0].(pi.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T", got.Content[0])
	}
	if !strings.Contains(text.Text, `"content": "module x"`) {
		t.Errorf("rendered result = %q", text.Text)
	}
	if invocations := inner.invocations(); len(invocations) != 1 || invocations[0] != `{"path":"go.mod"}` {
		t.Errorf("tool saw %v", invocations)
	}
}

func TestToolAdapter_ToolErrorIsReturned(t *testing.T) {
	adapted := Tool(&stubTool{name: "boom", err: errors.New("nope")})
	if _, err := adapted.Execute(context.Background(), "c", nil, nil); err == nil || err.Error() != "nope" {
		t.Fatalf("err = %v, want nope", err)
	}
}

func TestToolAdapter_AppliesOutputBudget(t *testing.T) {
	items := make([]string, 5000)
	for i := range items {
		items[i] = fmt.Sprintf(`{"id":%d,"data":"huge-payload-to-exceed-token-budget"}`, i)
	}
	largeJSON := "[" + strings.Join(items, ",") + "]"
	inner := &stubTool{
		name:   "large_tool",
		result: largeJSON,
	}
	adapted := Tool(inner)
	got, err := adapted.Execute(context.Background(), "call_large", json.RawMessage(`{}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(got.Content) != 1 {
		t.Fatalf("Content = %+v", got.Content)
	}
	text, ok := got.Content[0].(pi.TextContent)
	if !ok {
		t.Fatalf("content[0] = %T", got.Content[0])
	}
	if !strings.HasPrefix(text.Text, `{"_truncated":true`) {
		preview := text.Text
		if len(preview) > 100 {
			preview = preview[:100]
		}
		t.Errorf("expected truncated envelope, got: %s", preview)
	}
}

func TestToolAdapter_SchemaFallsBackWhenSchemaIsUnparseable(t *testing.T) {
	inner := &stubTool{name: "odd", schema: "not json"}
	schema := Tool(inner).Schema()
	if schema.Name != "odd" || schema.Description == "" {
		t.Errorf("Schema = %+v", schema)
	}
	if schema.Parameters != nil {
		t.Errorf("Parameters = %+v, want nil", schema.Parameters)
	}
}

func TestTools_IsSortedAndSkipsNilRegistry(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "zeta"})
	registry.Register(&stubTool{name: "alpha"})
	registry.Register(&stubTool{name: "mid"})

	adapted := Tools(registry)
	if len(adapted) != 3 {
		t.Fatalf("len = %d", len(adapted))
	}
	for i, want := range []string{"alpha", "mid", "zeta"} {
		if adapted[i].Name() != want {
			t.Errorf("tools[%d] = %q, want %q", i, adapted[i].Name(), want)
		}
	}
	if Tools(nil) != nil {
		t.Error("Tools(nil) should be nil")
	}
}

func TestTools_CarriesTheReferenceDescriptions(t *testing.T) {
	adapted := Tools(tools.Catalog(t.TempDir()))

	// Cycle 275 replaced the reconstructed run_command prose with the
	// reference's captured 1.4.x declaration (一手). The OS / shell sentence
	// is generated per host, so assert the stable parts rather than the whole
	// string.
	substrings := map[string][]string{
		"run_command": {
			"PROPOSE a command to run on behalf of the user.",
			"Make sure to specify CommandLine exactly as it should be run in the shell.",
		},
		// Cycle 305: the catalogue's descriptions are the reference's own
		// text now, not the clone's paraphrase.
		"list_dir": {"List the contents of a directory"},
	}
	seen := map[string]bool{}
	for _, tool := range adapted {
		wanted, ok := substrings[tool.Name()]
		if !ok {
			continue
		}
		seen[tool.Name()] = true
		got := tool.Schema().Description
		for _, description := range wanted {
			if !strings.Contains(got, description) {
				t.Errorf("%s description = %q, want it to contain %q", tool.Name(), got, description)
			}
		}
	}
	if len(seen) != len(substrings) {
		t.Errorf("missing advertised tools: saw %v", seen)
	}
}

func TestTools_FallsBackWhenNoDescriptionIsCarried(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&stubTool{name: "bare"})
	adapted := Tools(registry)
	if got := adapted[0].Schema().Description; got != "bare (openagy tool)" {
		t.Errorf("description = %q", got)
	}
}

// ─── the loop, end to end ────────────────────────────────────────────────────

func TestNewAgent_ToolRoundTripThroughCloneRegistry(t *testing.T) {
	client := &fakeClient{turns: [][]backend.StreamChunk{
		{
			{ToolCalls: []backend.ToolCall{{ID: "call_1", Name: "read_file", Arguments: json.RawMessage(`{"path":"go.mod"}`)}}},
			{Done: true},
		},
		{
			{Text: "the module is "},
			{Text: "github.com/justwasm/pigasus"},
			{Done: true},
		},
	}}

	inner := &stubTool{name: "read_file", result: "module github.com/justwasm/pigasus\n"}
	registry := tools.NewRegistry()
	registry.Register(inner)

	events := make(chan piagent.AgentEvent, 256)
	drained := make(chan struct{})
	var seen []string
	go func() {
		defer close(drained)
		for event := range events {
			if value, ok := event.(piagent.ToolExecutionEndEvent); ok {
				seen = append(seen, value.ToolName+"/"+fmt.Sprint(value.IsError))
			}
		}
	}()

	agent := mustNewAgent(t, Options{
		Client:       client,
		ModelID:      "claude-sonnet-4-6",
		SystemPrompt: "be terse",
		Registry:     registry,
		Events:       events,
	})

	messages, err := agent.Send(context.Background(), "what module is this?")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	close(events)
	<-drained

	if got := inner.invocations(); len(got) != 1 || got[0] != `{"path":"go.mod"}` {
		t.Fatalf("tool invocations = %v", got)
	}
	if len(seen) != 1 || seen[0] != "read_file/false" {
		t.Errorf("tool events = %v", seen)
	}

	// PiG must issue two upstream calls: the tool request and the follow-up.
	requests := client.requests()
	if len(requests) != 2 {
		t.Fatalf("upstream calls = %d, want 2", len(requests))
	}
	if requests[0].Model != "claude-sonnet-4-6" {
		t.Errorf("model = %q", requests[0].Model)
	}
	// The advertised tool set rides on the initial system message PiG builds.
	if len(requests[0].Tools) != 1 || requests[0].Tools[0].Name != "read_file" {
		t.Errorf("advertised tools = %+v", requests[0].Tools)
	}
	// The follow-up must carry the tool result back to the provider.
	var sawToolResult bool
	for _, message := range requests[1].Messages {
		if message.Role == "tool" && message.ToolCallID == "call_1" {
			sawToolResult = true
			if !strings.Contains(message.Content, "github.com/justwasm/pigasus") {
				t.Errorf("tool result content = %q", message.Content)
			}
		}
	}
	if !sawToolResult {
		t.Errorf("follow-up request has no tool result: %+v", requests[1].Messages)
	}

	// The final assistant message is the loop's own answer.
	last := messages[len(messages)-1]
	if last.Assistant == nil || assistantText(last.Assistant.Content) != "the module is github.com/justwasm/pigasus" {
		t.Errorf("final message = %+v", last)
	}
}

func TestNewAgent_RunsAgainstTheCloneMockBackend(t *testing.T) {
	agent := mustNewAgent(t, Options{Client: backend.NewMock(), ModelID: "claude-sonnet-4-6"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	messages, err := agent.Send(ctx, "hello")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(messages) < 2 {
		t.Fatalf("messages = %d, want a user and an assistant turn", len(messages))
	}
	last := messages[len(messages)-1]
	if last.Assistant == nil {
		t.Fatalf("final message = %+v", last)
	}
	if !strings.Contains(assistantText(last.Assistant.Content), "mock reply from model=claude-sonnet-4-6") {
		t.Errorf("reply = %q", assistantText(last.Assistant.Content))
	}
}

func TestNewAgent_DefaultsToSequentialToolExecution(t *testing.T) {
	agent := mustNewAgent(t, Options{Client: backend.NewMock(), ModelID: "m"})
	if got := agent.ToolExecutionMode(); got != piagent.ToolModeSequential {
		t.Errorf("ToolExecutionMode() = %q, want sequential", got)
	}
}

func TestModel_StampsSyntheticProvenance(t *testing.T) {
	model := Model("")
	if model.ID != "unknown" {
		t.Errorf("ID = %q", model.ID)
	}
	if model.ProviderMeta.ProviderID != ProviderID || model.ProviderMeta.API != BridgeAPI {
		t.Errorf("ProviderMeta = %+v", model.ProviderMeta)
	}
	if model.Type != pi.ModelTypeChat {
		t.Errorf("Type = %q", model.Type)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
