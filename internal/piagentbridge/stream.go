package piagentbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/tools"
)

// BackendStreamFn adapts a backend.Client into an agent.StreamFn.
//
// modelID is the Antigravity model id placed on the wire; the *ai.Model PiG
// passes in is used only to stamp the assistant message it comes back with.
// The returned function is safe to call concurrently — backend.Client.Stream
// is itself concurrency-safe, and each call gets its own pump goroutine.
//
// A pre-stream failure (client.Stream returning an error) is returned as-is,
// which is what keeps a *backend.HTTPError concrete on the way out of PiG: see
// the note on Provider.stream. Everything after the stream opened is folded
// into the event stream.
func BackendStreamFn(client backend.Client, modelID string) piagent.StreamFn {
	return BackendStreamFnWithObserver(client, modelID, nil)
}

// BackendStreamFnWithObserver is BackendStreamFn plus a hook for each raw
// backend.StreamChunk's metadata.
//
// It exists for exactly one field: backend.ChunkMeta.SystemFingerprint, which
// switch-provider transports set on their usage record and which ai.Usage has
// no counterpart for, so it cannot survive the trip through PiG's message
// model. observe is called synchronously from the pump goroutine, before the
// chunk is translated, and may be nil.
func BackendStreamFnWithObserver(client backend.Client, modelID string, observe func(backend.ChunkMeta)) piagent.StreamFn {
	return func(ctx context.Context, model *pi.Model, transcript pi.TranscriptContext, opts pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
		req := ToStreamRequest(transcript, modelID, opts)
		chunks, err := client.Stream(ctx, req)
		if err != nil {
			return nil, err
		}
		out := pi.NewAssistantMessageEventStream()
		go pump(ctx, out, chunks, model, observe)
		return out, nil
	}
}

// ToStreamRequest converts a normalized PiG transcript into the clone's
// provider-neutral request body.
//
// Mapping, one PiG message type per clone role:
//
//	ai.SystemMessage      -> StreamRequest.SystemPrompt   (+ StreamRequest.Tools)
//	ai.UserMessage        -> Message{Role: "user"}
//	ai.AssistantMessage   -> Message{Role: "assistant", ToolCalls: [...]}
//	ai.ToolResultMessage  -> Message{Role: "tool", ToolCallID, Name}
//
// PiG carries the advertised tool set on the initial system message
// (ai.CreateInitialSystemMessage), which is where StreamRequest.Tools is read
// from. Image blocks and thinking blocks have no representation in
// backend.StreamRequest and are dropped.
func ToStreamRequest(transcript pi.TranscriptContext, modelID string, opts pi.StreamOptions) backend.StreamRequest {
	req := backend.StreamRequest{
		Model:       modelID,
		MaxTokens:   opts.MaxTokens,
		Temperature: opts.Temperature,
	}
	for _, message := range transcript.Messages() {
		appendStreamRequestMessage(&req, message)
	}
	return req
}

// appendStreamRequestMessage maps one PiG message into the request fields
// (SystemPrompt/Tools for SystemMessage; Messages otherwise). Split out of
// ToStreamRequest so the orchestrator stays small and the per-message
// branches read independently.
func appendStreamRequestMessage(req *backend.StreamRequest, message pi.Message) {
	switch m := message.(type) {
	case pi.SystemMessage:
		if text := systemText(m); text != "" {
			req.SystemPrompt = joinPrompt(req.SystemPrompt, text)
		}
		appendStreamRequestTools(req, m.ToolsAdded)
	case pi.UserMessage:
		req.Messages = append(req.Messages, backend.Message{
			Role:    "user",
			Content: userText(m),
			Images:  userImages(m),
		})
	case pi.AssistantMessage:
		req.Messages = append(req.Messages, buildStreamRequestAssistant(m))
	case pi.ToolResultMessage:
		text := toolResultText(m)
		req.Messages = append(req.Messages, backend.Message{
			Role:       "tool",
			Content:    text,
			ToolCallID: m.ToolCallID,
			Name:       m.ToolName,
			Result:     toolResultPayload(m),
			// A tool that *threw* is flagged by the runtime; a tool
			// that failed the way the reference's own tools fail — by
			// writing its failure text — is not, because the wording
			// is what the clone has to read. Both are errors on the
			// wire (【一手】 agy 1.4.8, see internal/tools/failure.go).
			IsError: m.IsError || tools.IsFailureText(text),
		})
	}
}

// toolResultPayload returns the structured value a tool answered with, when
// it answered with one.
//
// A tool's details are where a structured answer travels: the reference's
// `grep_search` answers with the files it matched rather than with text, so
// its details carry that value and the transport sends it as it is instead
// of wrapping the text in an `output` member.
//
// Anything that is not an object is dropped — the transports have no way to
// send a bare value, and the text still carries whatever the tool wrote.
func toolResultPayload(m pi.ToolResultMessage) map[string]any {
	switch details := m.Details.(type) {
	case map[string]any:
		if len(details) == 0 {
			return nil
		}
		return details
	default:
		return nil
	}
}

// appendStreamRequestTools converts PiG tool schemas into the clone's
// ToolDecl struct, omitting Parameters when the PiG schema is empty or
// fails to marshal — callers that want an empty schema get the zero value.
func appendStreamRequestTools(req *backend.StreamRequest, decls []pi.ToolSchema) {
	for _, decl := range decls {
		tool := backend.ToolDecl{Name: decl.Name, Description: decl.Description}
		if len(decl.Parameters) > 0 {
			if raw, err := json.Marshal(decl.Parameters); err == nil {
				tool.Parameters = raw
			}
		}
		req.Tools = append(req.Tools, tool)
	}
}

// buildStreamRequestAssistant converts one PiG assistant turn into the
// clone's Message shape. Image blocks and thinking blocks have no
// representation and are dropped; tool calls carry the JSON-encoded
// arguments verbatim.
func buildStreamRequestAssistant(m pi.AssistantMessage) backend.Message {
	out := backend.Message{Role: "assistant", Content: assistantText(m.Content)}
	for _, block := range m.Content {
		call, ok := block.(pi.ToolCall)
		if !ok {
			continue
		}
		raw, err := json.Marshal(call.Arguments)
		if err != nil {
			raw = []byte("{}")
		}
		out.ToolCalls = append(out.ToolCalls, backend.ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: raw,
		})
	}
	return out
}

// pump translates the clone's StreamChunk channel into the event sequence PiG
// expects on an AssistantMessageEventStream:
//
//	StartEvent
//	(TextStartEvent TextDeltaEvent* TextEndEvent)?
//	(ToolCallStartEvent ToolCallEndEvent)*
//	(DoneEvent | ErrorEvent)
//
// followed by End(result). That is exactly the order ai/stream_builder.go
// produces, so agent.consumeStream reads it the same way it reads a native
// provider response.
//
// Cycle stall-detection: AGY 1.3.6 line 5. Every chunk read from `chunks`
// resets a 5-minute idle watcher; if the upstream backend stops emitting
// chunks for that window, the watcher cancels ctx and the next iteration
// of the for-range exits through the ctx.Done branch. The terminal error
// is reported as "stalled after 5m0s" so the TUI's recovery prompt can
// distinguish a stall from a server-side 5xx.
func pump(ctx context.Context, out *pi.AssistantMessageEventStream, chunks <-chan backend.StreamChunk, model *pi.Model, observe func(backend.ChunkMeta)) {
	partial := newAssistantMessage(model, BridgeAPI)
	push(out, pi.StartEvent{Partial: partial.ShallowCopy()})

	// Wrap ctx with a stall watcher. Each chunk read resets the timer; if
	// no chunk arrives inside the idle window the watcher cancels ctx and
	// the loop below exits with StopReasonAborted.
	ctx, watcher := StartStallWatcher(ctx)
	defer watcher.Stop()

	state := newBridgePumpState(out, partial, observe)
	state.run(ctx, watcher, chunks)
}

// bridgePumpState holds the per-stream state shared by the chunk
// handlers. It is the bridge adapter's mirror of gatewayprovider_stream.go's
// streamState: both play the same role (accumulate one response into PiG's
// event union), just with different data sources.
type bridgePumpState struct {
	out     *pi.AssistantMessageEventStream
	partial *pi.AssistantMessage
	observe func(backend.ChunkMeta)

	textIndex int
	text      strings.Builder
}

func newBridgePumpState(out *pi.AssistantMessageEventStream, partial *pi.AssistantMessage, observe func(backend.ChunkMeta)) *bridgePumpState {
	return &bridgePumpState{
		out:       out,
		partial:   partial,
		observe:   observe,
		textIndex: -1,
	}
}

// run walks every chunk on the channel and dispatches it to the per-chunk
// handler. The loop ends on Done, Error, or a stall-cancelled context.
func (s *bridgePumpState) run(ctx context.Context, watcher *StallWatcher, chunks <-chan backend.StreamChunk) {
	var sawDone bool
	var sawAnyChunk bool
	for chunk := range chunks {
		// Reset the stall watcher on every chunk we read — including
		// Error and Done, so the agent cannot park on an Error that
		// never delivers a Done while a friendly chunk-bearing retry
		// is already ready.
		watcher.Note()
		sawAnyChunk = true
		if chunk.Done {
			sawDone = true
		}
		if reason, errMsg, done := s.applyChunk(ctx, chunk); done {
			s.finish(reason, errMsg)
			return
		}
	}
	// Cycle gatewayprovider-eof-without-done: the channel closed without a
	// Done chunk. backend.Client promises one, but a misbehaving
	// transport that closes without Done (network blip, idle-evict,
	// abrupt disconnect) used to be classified as a clean stop,
	// which made the TUI render an empty or truncated transcript as a
	// normal turn: chunkSink.endAssistant's StopReasonStop branch
	// fired, no ErrorEvent was ever pushed, the cascade ended at
	// StateDone, and the user had to type "continue" to recover.
	//
	// The fix distinguishes "no chunks at all" from "some chunks but
	// no terminal Done": only the former is an error. The latter is
	// the documented behaviour of the configured gateway — the gateway closes the
	// channel after the usage frame without sending an explicit Done
	// — and is preserved for back-compat with the production
	// gateway. Trade-off #1 of this cycle, accepted explicitly: a
	// half-streamed reply that saw at least one chunk still appears
	// as a clean stop; the user usually sees the partial text so
	// the cost of leaving it alone is lower than the cost of
	// mis-reporting the configured gateway normal streams as errors.
	if watcher.Fired() {
		s.finish(pi.StopReasonAborted, ErrStallCancelled.Error())
		return
	}
	if !sawDone && !sawAnyChunk {
		s.finish(pi.StopReasonError, "EOF: stream closed without Done chunk and no chunks")
		return
	}
	s.finish(stopReasonFor(s.partial), "")
}

// applyChunk dispatches one chunk to the right sub-handler. Returns
// (reason, errMsg, done=true) when the chunk ends the stream.
//
// noteMeta is called once per chunk (forwarding the raw metadata) so a
// caller that needs a field ai.Usage has no home for sees it in arrival
// order — that is the contract BackendStreamFnWithObserver was added for.
func (s *bridgePumpState) applyChunk(ctx context.Context, chunk backend.StreamChunk) (pi.StopReason, string, bool) {
	if chunk.Error != nil {
		reason := pi.StopReasonError
		if ctx.Err() != nil {
			reason = pi.StopReasonAborted
		}
		return reason, chunk.Error.Error(), true
	}
	s.applyText(chunk.Text)
	s.applyToolCalls(chunk.ToolCalls)
	if chunk.Done {
		s.observeMeta(chunk.Meta)
		if chunk.Meta != nil {
			s.partial.Usage = usageFromMeta(chunk.Meta)
		}
		return stopReasonFor(s.partial), "", true
	}
	s.observeMeta(chunk.Meta)
	return "", "", false
}

// applyText appends one text delta to the partial and emits the TextStart /
// TextDelta events PiG consumes. The first delta opens a text block at the
// next free content slot; subsequent deltas append.
func (s *bridgePumpState) applyText(text string) {
	if text == "" {
		return
	}
	if s.textIndex < 0 {
		s.textIndex = len(s.partial.Content)
		s.partial.Content = append(s.partial.Content, pi.TextContent{})
		push(s.out, pi.TextStartEvent{ContentIndex: s.textIndex, Partial: s.partial.ShallowCopy()})
	}
	s.text.WriteString(text)
	s.partial.Content[s.textIndex] = pi.TextContent{Text: s.text.String()}
	push(s.out, pi.TextDeltaEvent{
		ContentIndex: s.textIndex,
		Delta:        text,
		Partial:      s.partial.ShallowCopy(),
	})
}

// applyToolCalls closes any open text block, then emits one ToolCallStart +
// ToolCallEnd pair per call. PiG's loop reads tool_use from the merged
// start+end pair, which is why this stream emits both.
func (s *bridgePumpState) applyToolCalls(calls []backend.ToolCall) {
	if len(calls) == 0 {
		return
	}
	s.closeText()
	for _, call := range calls {
		index := len(s.partial.Content)
		invocation := pi.ToolCall{
			ID:        call.ID,
			Name:      call.Name,
			Arguments: decodeArguments(call.Arguments),
		}
		s.partial.Content = append(s.partial.Content, invocation)
		push(s.out, pi.ToolCallStartEvent{ContentIndex: index, Partial: s.partial.ShallowCopy()})
		push(s.out, pi.ToolCallEndEvent{
			ContentIndex: index,
			ToolCall:     invocation,
			Partial:      s.partial.ShallowCopy(),
		})
	}
}

// observeMeta forwards the raw chunk metadata to the observer (when one is
// installed). Called before the chunk is translated, so the observer sees
// every meta in arrival order — applyChunk drives both Done (with usage)
// and non-terminal metas through this single hook.
func (s *bridgePumpState) observeMeta(meta *backend.ChunkMeta) {
	if s.observe != nil && meta != nil {
		s.observe(*meta)
	}
}

// closeText emits the TextEnd event and resets the per-block state.
func (s *bridgePumpState) closeText() {
	if s.textIndex < 0 {
		return
	}
	push(s.out, pi.TextEndEvent{
		ContentIndex: s.textIndex,
		Content:      s.text.String(),
		Partial:      s.partial.ShallowCopy(),
	})
	s.textIndex = -1
}

// finish emits the terminal Done/Error event and calls End. errMessage is
// non-empty only for StopReasonError / StopReasonAborted cases.
func (s *bridgePumpState) finish(reason pi.StopReason, errMessage string) {
	s.closeText()
	s.partial.StopReason = reason
	s.partial.ErrorMessage = errMessage
	snapshot := s.partial.ShallowCopy()
	if reason == pi.StopReasonError || reason == pi.StopReasonAborted {
		push(s.out, pi.ErrorEvent{Reason: reason, Error: snapshot})
	} else {
		push(s.out, pi.DoneEvent{Reason: reason, Message: snapshot})
	}
	s.out.End(snapshot)
}

// push ignores the error from AssistantMessageEventStream.Push: the only
// failure mode is pushing after End, which this pump never does.
func push(out *pi.AssistantMessageEventStream, event pi.AssistantMessageEvent) {
	_ = out.Push(event)
}

func newAssistantMessage(model *pi.Model, api pi.API) *pi.AssistantMessage {
	message := &pi.AssistantMessage{
		Content:   []pi.AssistantContentBlock{},
		API:       api,
		Provider:  ProviderID,
		Timestamp: time.Now().UnixMilli(),
	}
	if model != nil {
		message.Model = model.ID
		message.ResponseModel = model.ID
	}
	return message
}

// stopReasonFor reports toolUse when the response asked for tools — that is
// the signal agent.consumeStream uses to run them and loop.
func stopReasonFor(message *pi.AssistantMessage) pi.StopReason {
	for _, block := range message.Content {
		if _, ok := block.(pi.ToolCall); ok {
			return pi.StopReasonToolUse
		}
	}
	return pi.StopReasonStop
}

func usageFromMeta(meta *backend.ChunkMeta) pi.Usage {
	usage := pi.Usage{
		Input:       meta.PromptTokens,
		Output:      meta.CompletionTokens,
		TotalTokens: meta.TotalTokens,
	}
	if meta.ReasoningTokens > 0 {
		reasoning := meta.ReasoningTokens
		usage.Reasoning = &reasoning
	}
	return usage
}

func decodeArguments(raw json.RawMessage) pi.JsonObject {
	if len(raw) == 0 {
		return pi.JsonObject{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		return pi.JsonObject{}
	}
	return pi.JsonObject(decoded)
}

func joinPrompt(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "\n\n" + next
}

// systemText renders a system message the way PiG's own
// ai.GetCurrentSystemPrompt does: the content text, then every section value,
// joined with a blank line. Reading Content alone is not enough — a Session's
// system message carries the instructions in Sections and leaves Content
// empty, so a Content-only reader drops the whole system prompt on the floor.
func systemText(message pi.SystemMessage) string {
	parts := make([]string, 0, 1+len(message.Sections))
	switch content := message.Content.(type) {
	case pi.SystemText:
		if text := string(content); text != "" {
			parts = append(parts, text)
		}
	case pi.SystemTextBlocks:
		var builder strings.Builder
		for _, block := range content {
			builder.WriteString(block.Text)
		}
		if builder.Len() > 0 {
			parts = append(parts, builder.String())
		}
	}
	for _, section := range message.Sections {
		if section.Value != nil && *section.Value != "" {
			parts = append(parts, *section.Value)
		}
	}
	return strings.Join(parts, "\n\n")
}

func userText(message pi.UserMessage) string {
	switch content := message.Content.(type) {
	case pi.UserText:
		return string(content)
	case pi.UserContentBlocks:
		var builder strings.Builder
		for _, block := range content {
			if text, ok := block.(pi.TextContent); ok {
				builder.WriteString(text.Text)
			}
		}
		return builder.String()
	}
	return ""
}

// userImages reads the images a user message carries.
//
// PiG's image block holds the base64 payload (ai.ImageContent.Data) and
// the media type beside it; the clone's neutral shape holds the bytes.
// A block whose payload is not valid base64 is dropped rather than sent
// as garbage — the model would answer a broken image with a guess.
func userImages(message pi.UserMessage) []backend.ImagePart {
	content, ok := message.Content.(pi.UserContentBlocks)
	if !ok {
		return nil
	}
	var images []backend.ImagePart
	for _, block := range content {
		image, ok := block.(pi.ImageContent)
		if !ok || image.Data == "" {
			continue
		}
		data, err := base64.StdEncoding.DecodeString(image.Data)
		if err != nil || len(data) == 0 {
			continue
		}
		images = append(images, backend.ImagePart{MIMEType: image.MimeType, Data: data})
	}
	return images
}

// assistantText joins the text blocks of an assistant turn. It takes the block
// slice rather than ai.AssistantMessage so it also accepts the agent loop's
// own agent.AssistantMessage, which shares the block type.
func assistantText(blocks []pi.AssistantContentBlock) string {
	var builder strings.Builder
	for _, block := range blocks {
		if text, ok := block.(pi.TextContent); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}

func toolResultText(message pi.ToolResultMessage) string {
	var builder strings.Builder
	for _, block := range message.Content {
		if text, ok := block.(pi.TextContent); ok {
			builder.WriteString(text.Text)
		}
	}
	return builder.String()
}
