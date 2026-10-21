package piagentbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	pi "github.com/MichaelKinsy/PiG/ai"
)

// ─── response shape ──────────────────────────────────────────────────────────
//
// chatChunk / chatDelta / chatUsage are the agw gateway's wire schema.
// These mirror backend.AgwK0sConfig's frame decoder deliberately, so the
// two transports stay in agreement on what "a frame" means.

type chatChoice struct {
	Delta        chatDelta `json:"delta"`
	FinishReason string    `json:"finish_reason"`
}

type chatChunk struct {
	Choices []chatChoice `json:"choices"`
	Usage   *chatUsage   `json:"usage"`
	// Error is the gateway's upstream-failure record. PiG's OpenAI decoder
	// (ai/openai_stream_decoder.go) treats a truthy top-level "error"
	// member as a hard APIError before it looks at anything else; the agw
	// adapter used to drop the member on the floor, so a gateway that
	// answered with an error record and closed produced an empty turn with
	// no diagnostic. Cycle stream-error-frames routes it through
	// streamState.finish's error path instead.
	Error json.RawMessage `json:"error"`
}

type chatUsageDetails struct {
	ReasoningTokens int `json:"reasoning_tokens"`
}

type chatUsage struct {
	PromptTokens     int              `json:"prompt_tokens"`
	CompletionTokens int              `json:"completion_tokens"`
	TotalTokens      int              `json:"total_tokens"`
	Details          chatUsageDetails `json:"completion_tokens_details"`
}

type chatDeltaToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatDeltaTool struct {
	Index    int                   `json:"index"`
	ID       string                `json:"id"`
	Type     string                `json:"type"`
	Function chatDeltaToolFunction `json:"function"`
}

type chatDelta struct {
	Content   string          `json:"content"`
	ToolCalls []chatDeltaTool `json:"tool_calls"`
}

// ─── SSE frame → event pump ──────────────────────────────────────────────────
//
// streamState accumulates one response into PiG's event union. Text lives in a
// single block at contentIndex 0; tool calls follow it, one block each, in the
// order the gateway introduced them (its `index` field, not arrival order).
// consume, finish, closeText, syncCall all together carry the per-turn state
// machine: splitting them across files is fine because they all share state.

type streamState struct {
	out     *pi.AssistantMessageEventStream
	partial *pi.AssistantMessage

	textIndex int
	text      strings.Builder

	calls     []*toolCallState
	byGateway map[int]*toolCallState

	finishReason string
	usage        *chatUsage
	failed       string
}

type toolCallState struct {
	index     int // content index in partial.Content
	gateway   int // the gateway's own index
	id        string
	name      string
	arguments strings.Builder
	started   bool
}

func newStreamState(out *pi.AssistantMessageEventStream, partial *pi.AssistantMessage) *streamState {
	return &streamState{
		out:       out,
		partial:   partial,
		textIndex: -1,
		byGateway: map[int]*toolCallState{},
	}
}

// consume applies one SSE frame to the stream state. It is the gateway's
// "choices[0].delta" entry point: text delta and tool-call fragments are
// delegated to applyTextDelta and applyToolCallFragment respectively.
// finishReason and usage are captured here so they survive into finish().
func (s *streamState) consume(frame chatChunk) {
	if frame.Usage != nil {
		s.usage = frame.Usage
	}
	if len(frame.Choices) == 0 {
		return
	}
	choice := frame.Choices[0]
	if choice.FinishReason != "" {
		s.finishReason = choice.FinishReason
	}
	s.applyTextDelta(choice.Delta.Content)
	for _, fragment := range choice.Delta.ToolCalls {
		s.applyToolCallFragment(fragment)
	}
}

// applyTextDelta writes one text delta into the partial message and emits
// the TextStart/TextDelta events PiG's loop consumes. The first delta opens
// a text block at the next free content slot; subsequent deltas append.
func (s *streamState) applyTextDelta(content string) {
	if content == "" {
		return
	}
	if s.textIndex < 0 {
		s.textIndex = len(s.partial.Content)
		s.partial.Content = append(s.partial.Content, pi.TextContent{})
		push(s.out, pi.TextStartEvent{ContentIndex: s.textIndex, Partial: s.partial.ShallowCopy()})
	}
	s.text.WriteString(content)
	s.partial.Content[s.textIndex] = pi.TextContent{Text: s.text.String()}
	push(s.out, pi.TextDeltaEvent{
		ContentIndex: s.textIndex,
		Delta:        content,
		Partial:      s.partial.ShallowCopy(),
	})
}

// applyToolCallFragment starts or appends to one tool-call accumulator,
// keyed by the gateway's `index`. A tool call's id, name, and arguments
// may arrive across many frames; this stitches them into partial.Content
// in arrival order and emits ToolCallStart/Delta as the fragments arrive.
func (s *streamState) applyToolCallFragment(fragment chatDeltaTool) {
	call, ok := s.byGateway[fragment.Index]
	if !ok {
		// A tool call starts, so any text block before it is complete.
		s.closeText()
		call = &toolCallState{index: len(s.partial.Content), gateway: fragment.Index}
		s.byGateway[fragment.Index] = call
		s.calls = append(s.calls, call)
		s.partial.Content = append(s.partial.Content, pi.ToolCall{})
	}
	if fragment.ID != "" {
		call.id = fragment.ID
	}
	if fragment.Function.Name != "" {
		call.name = fragment.Function.Name
	}
	if !call.started {
		call.started = true
		s.syncCall(call)
		push(s.out, pi.ToolCallStartEvent{ContentIndex: call.index, Partial: s.partial.ShallowCopy()})
	}
	if fragment.Function.Arguments != "" {
		call.arguments.WriteString(fragment.Function.Arguments)
		s.syncCall(call)
		push(s.out, pi.ToolCallDeltaEvent{
			ContentIndex: call.index,
			Delta:        fragment.Function.Arguments,
			Partial:      s.partial.ShallowCopy(),
		})
	}
}

// syncCall writes the accumulating call back into the partial message so an
// event's Partial view matches what the reader just saw. Called from
// applyToolCallFragment's start/delta branches; finish() also routes through
// syncToolCall to ensure the final ToolCallEnd carries the assembled form.
func (s *streamState) syncCall(call *toolCallState) {
	s.partial.Content[call.index] = pi.ToolCall{
		ID:        call.id,
		Name:      call.name,
		Arguments: decodeArguments(json.RawMessage(call.arguments.String())),
	}
}

func (s *streamState) closeText() {
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

// finish terminates the stream exactly once. errText, when non-empty, turns
// the response into an error regardless of what was streamed before it.
func (s *streamState) finish(ctx context.Context, errText string) {
	if s.failed != "" {
		return
	}
	s.failed = "done"

	s.closeText()
	for _, call := range s.calls {
		syncToolCall(s.partial, call)
		push(s.out, pi.ToolCallEndEvent{
			ContentIndex: call.index,
			ToolCall:     s.partial.Content[call.index].(pi.ToolCall),
			Partial:      s.partial.ShallowCopy(),
		})
	}
	if s.usage != nil {
		s.partial.Usage = usageFromChat(s.usage)
	}

	if errText != "" {
		reason := pi.StopReasonError
		if ctx.Err() != nil {
			reason = pi.StopReasonAborted
		}
		s.partial.StopReason = reason
		s.partial.ErrorMessage = errText
		snapshot := s.partial.ShallowCopy()
		push(s.out, pi.ErrorEvent{Reason: reason, Error: snapshot})
		s.out.End(snapshot)
		return
	}

	reason := stopReasonFromFinish(s.finishReason, len(s.calls) > 0)
	s.partial.StopReason = reason
	snapshot := s.partial.ShallowCopy()
	push(s.out, pi.DoneEvent{Reason: reason, Message: snapshot})
	s.out.End(snapshot)
}

func syncToolCall(partial *pi.AssistantMessage, call *toolCallState) {
	partial.Content[call.index] = pi.ToolCall{
		ID:        call.id,
		Name:      call.name,
		Arguments: decodeArguments(json.RawMessage(call.arguments.String())),
	}
}

// stopReasonFromFinish picks PiG's StopReason for the OpenAI finish_reason the
// gateway returned. An empty finish_reason (the common case, because the
// gateway does not always set one) follows the tool-call presence: any
// toolCalls present → tool_use, else stop. "length" maps to StopReasonLength.
func stopReasonFromFinish(finish string, hasCalls bool) pi.StopReason {
	switch finish {
	case "tool_calls", "function_call":
		return pi.StopReasonToolUse
	case "length":
		return pi.StopReasonLength
	case "":
		if hasCalls {
			return pi.StopReasonToolUse
		}
		return pi.StopReasonStop
	default:
		return pi.StopReasonStop
	}
}

func usageFromChat(usage *chatUsage) pi.Usage {
	out := pi.Usage{
		Input:       usage.PromptTokens,
		Output:      usage.CompletionTokens,
		TotalTokens: usage.TotalTokens,
	}
	if usage.Details.ReasoningTokens > 0 {
		reasoning := usage.Details.ReasoningTokens
		out.Reasoning = &reasoning
	}
	return out
}

// ─── SSE scanner pump ────────────────────────────────────────────────────────
//
// pumpSSE is the native provider's response-side adapter. It reads lines
// from the agw gateway's stream and forwards every decoded frame into a
// streamState. It is the equivalent of stream.go's pump, but consumes an
// SSE body (io.ReadCloser) rather than a backend.StreamChunk channel.

func pumpSSE(ctx context.Context, out *pi.AssistantMessageEventStream, partial *pi.AssistantMessage, reader io.ReadCloser) {
	defer reader.Close()
	push(out, pi.StartEvent{Partial: partial.ShallowCopy()})

	// Cycle stall-detection: AGY 1.3.6 line 5 wraps the pump with a 5-minute
	// idle watcher. Every event we read (text delta, tool-call fragment,
	// usage frame) resets the timer; if the gateway stops sending frames
	// without sending [DONE], the watcher cancels ctx and the scanner loop
	// below exits through pumpScan's scanner.Err branch with the cancellation
	// surfaced as StopReasonAborted.
	ctx, watcher := StartStallWatcher(ctx)
	defer watcher.Stop()

	state := newStreamState(out, partial)
	state.finish(ctx, pumpScan(ctx, state, watcher, reader))
}

// streamErrorText reports the upstream failure carried by one SSE record, or
// "" when the record is not an error. It mirrors PiG's OpenAI stream decoder
// (ai/openai_stream_decoder.go): a truthy top-level "error" member is a hard
// APIError, and an `event: error` record is an error even when the record
// itself carries no "error" member (the record then is the error value, as in
// the SDK's `data?.error ?? data`). Message extraction follows
// APIError.makeMessage: a string "message", then the JSON of the value, then
// the SDK's "(no status code or body)".
//
// One deliberate deviation: an `event: error` whose data is not JSON returns
// the raw payload rather than the SDK's "Error reading response: malformed
// server-sent event JSON." The payload is what the user needs to see; the
// SDK wording hides it.
func streamErrorText(member json.RawMessage, payload string, isErrorEvent bool) string {
	raw := member
	if !jsonTruthy(raw) {
		if !isErrorEvent {
			return ""
		}
		raw = json.RawMessage(payload)
	}
	if !jsonTruthy(raw) {
		return "(no status code or body)"
	}
	if text := apiErrorMessage(raw); text != "" {
		return text
	}
	return "(no status code or body)"
}

// apiErrorMessage is APIError.makeMessage for a decoded error value: an
// object's string "message" wins; any other value renders as its JSON text.
func apiErrorMessage(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "{") {
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err == nil {
			if message, ok := object["message"]; ok && jsonTruthy(message) {
				var text string
				if json.Unmarshal(message, &text) == nil {
					return text
				}
				return strings.TrimSpace(string(message))
			}
		}
	}
	return trimmed
}

// jsonTruthy mirrors openAIStreamTruthy: JSON null, false, 0 and the empty
// string are falsy; everything else (objects, arrays, numbers, non-empty
// strings) is an error value.
func jsonTruthy(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "", "null", "false", "0", `""`:
		return false
	}
	return true
}

// scanProgress is the little state pumpScan carries across lines: whether
// the terminal marker was seen, whether any data record arrived at all, and
// the pending `event:` name for the current SSE block.
type scanProgress struct {
	gotDone     bool
	gotAnyFrame bool
	lastEvent   string
}

// pumpScan reads one line at a time from `reader`, decoding every JSON frame
// into the stream state. It returns the terminal error string the
// streamState.finish should carry (empty means "clean stop"). The function
// is split out of pumpSSE so the orchestrator stays small and the scanner
// loop stays a tight per-line concern; scanSSELine owns the per-line work.
func pumpScan(ctx context.Context, state *streamState, watcher *StallWatcher, reader io.ReadCloser) string {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var progress scanProgress
	for scanner.Scan() {
		if stop, done := scanSSELine(scanner.Text(), state, watcher, &progress); done {
			return stop
		}
	}
	if err := scanner.Err(); err != nil {
		// A stall-cancelled scanner returns the wrapped context error;
		// surface it as StopReasonAborted so callers can branch on the
		// reason rather than parsing the message.
		if errors.Is(err, context.Canceled) && watcher.Fired() {
			return ErrStallCancelled.Error()
		}
		return err.Error()
	}
	return eofStop(progress)
}

// scanSSELine decodes one raw SSE line. It returns (stop, true) when the
// line ends the stream — `[DONE]`, an upstream error record, or an
// `event: error` body — and ("", false) otherwise.
func scanSSELine(raw string, state *streamState, watcher *StallWatcher, p *scanProgress) (string, bool) {
	line := strings.TrimSpace(raw)
	if line == "" {
		// A blank line closes the SSE event block, so `event:` applies
		// only to the data lines that follow it directly.
		p.lastEvent = ""
		return "", false
	}
	if strings.HasPrefix(line, ":") {
		return "", false
	}
	if strings.HasPrefix(line, "event:") {
		p.lastEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		return "", false
	}
	if !strings.HasPrefix(line, "data:") {
		return "", false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	isErrorEvent := p.lastEvent == "error"
	p.lastEvent = ""
	if payload == "[DONE]" && !isErrorEvent {
		p.gotDone = true
		return "", true
	}
	p.gotAnyFrame = true
	var frame chatChunk
	if err := json.Unmarshal([]byte(payload), &frame); err != nil {
		if isErrorEvent {
			// `event: error` with a non-JSON body is still an upstream
			// failure; see streamErrorText's note on the deviation.
			return payload, true
		}
		// A frame we cannot decode is not fatal: the gateway interleaves
		// keep-alives and vendor-specific frames. Skipping keeps the text
		// we already have.
		watcher.Note() // undecodable frame still counts as gateway liveness
		return "", false
	}
	watcher.Note()
	// Cycle stream-error-frames: an upstream error record is a hard failure,
	// not a frame to ignore. Before this it counted as "one frame" and the
	// run ended as a silent clean stop with an empty transcript (measured:
	// events [start done], StopReason=stop, text="").
	if text := streamErrorText(frame.Error, payload, isErrorEvent); text != "" {
		return text, true
	}
	state.consume(frame)
	return "", false
}

// eofStop classifies a stream whose scanner reached EOF without a terminal
// line.
//
// Cycle agwprovider-eof-without-done: a clean EOF without [DONE] AND without
// any frames is an incomplete response — the gateway never sent anything
// (request severed before the first byte, network blip on connect, etc.).
// Cycle stall-detection previously classified this as a clean stop, which
// left the TUI rendering an empty transcript and the user had to type
// "continue" to recover. EOF-without-DONE WITH frames is the agw.k0s.io
// default behaviour (the gateway closes the SSE stream without sending
// [DONE]); accept that as long as we saw at least one frame, so back-compat
// with the production gateway is preserved.
//
// Trade-off #1, accepted explicitly: a half-streamed reply that saw at least
// one frame still appears as a clean stop — the partial text already pushed
// to chunkSink stays visible and no error rides below it. That case is rarer
// than 0-frame drops and the user usually sees the partial text, so the cost
// of leaving it alone is lower than the cost of mis-reporting agw.k0s.io
// normal streams as errors.
func eofStop(p scanProgress) string {
	if !p.gotDone && !p.gotAnyFrame {
		return "EOF: stream ended without [DONE] marker and no frames"
	}
	return ""
}
