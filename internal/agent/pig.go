// Package agent — pig.go: the PiG-backed implementation of Loop.Send.
//
// internal/agent used to own the whole agent loop: open the backend stream,
// collect tool calls, run them, feed the results back, repeat, all hand-rolled
// in loop.go. Since cycle 183 that loop is PiG's
// (github.com/MichaelKinsy/PiG/agent), and this file is the seam.
//
// The public surface did not move. Loop.Options, Loop.Send, ToolRunner,
// Handler, TurnInput, Cascade and the StreamChunk vocabulary are all
// unchanged, so cmd/wagyu, cmd/openai-shim, cmd/anthropic-shim and the Cascade
// observers are untouched. What changed is who walks the model -> tool -> model
// cycle.
//
// Translation, both directions:
//
//	out:  []backend.Message          -> []agent.AgentMessage
//	      backend.ToolDecl           -> agent.AgentTool (Execute -> ToolRunner)
//	      agent.AgentEvent           -> backend.StreamChunk
//	in:   Loop.Options.Backend       -> agent.StreamFn (piagentbridge)
//	      TurnInput.MaxTokens/…      -> ai.StreamOptions
//
// The Handler never sees an AgentEvent; it still sees the same chunk shapes it
// always did, which is what keeps the TUI's sanitizer, the print-mode emitter
// and both HTTP shims byte-compatible.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/tools"
)

// sendPig drives one Send through PiG's agent loop.
//
// A fresh PiG agent is built per Send. That is not an optimisation miss: the
// clone's callers already hand the loop the whole conversation in
// TurnInput.Messages (the TUI copies its session, the print path replays a
// resumed conversation), so there is no state worth carrying between Sends,
// and a per-call agent cannot accumulate a duplicate transcript.
//
// Cycle auto-continue-interrupt: sendPig is the no-recovery path. The
// public Loop.Send dispatches to sendPigAuto (above this file) which
// wraps sendPig and recovers from a stream-interrupt or iteration-cap
// by appending a recovery message and re-running. Both paths share the
// same PiG plumbing — only the failure-handling tail differs.
func (l *Loop) sendPig(ctx context.Context, input TurnInput) error {
	model := input.ModelOr(l.opts.DefaultModel)

	sink := &chunkSink{handler: l.opts.Handler, cascade: l.cascade, loop: l}
	// PiG flattens a stream-function error to err.Error() before the sink sees
	// it (agent_loop.go:494 copies only the text into the assistant message),
	// which would cost the concrete type. Both transports hand it here first.
	failures := &errorSlot{}

	// PiG's emit blocks until the event is received, so the translator must be
	// draining independently of the Send goroutine.
	events := make(chan piagent.AgentEvent, 128)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for event := range events {
			sink.observe(event)
		}
	}()

	maxTurns := pigMaxTurns(l.opts)

	cascadeTransition(l.cascade, StatePlan, "selected backend+model")

	provider := l.opts.Provider
	if provider == nil {
		provider = nativeProvider(l.opts.Backend)
	}

	agentOptions := piagent.AgentOptions{
		Tools:         l.pigTools(),
		SystemPrompt:  l.opts.SystemPrompt,
		EventCh:       events,
		MaxTurns:      maxTurns,
		ToolExecution: piagent.ToolModeSequential,
	}
	if provider != nil {
		// The native path: the model's own Provider.Stream answers the
		// request. PiG 0.5.1 no longer installs that dispatch as the default
		// stream function, so the path supplies ProviderStreamFn itself. The
		// provider is wrapped so its concrete errors survive the trip.
		bound := provider.Model(model)
		bound.Provider = &recordingProvider{inner: bound.Provider, failures: failures}
		agentOptions.Model = bound
		agentOptions.StreamFn = piagentbridge.ProviderStreamFn()
		// The provider is agw-shaped, so it reports the agw fingerprint. The
		// StreamFn path takes the same value off the wire instead, and only
		// when the wire actually carried metadata; here the provider is the
		// transport, and agw always sends a usage record because the request
		// sets stream_options.include_usage.
		sink.noteTransport(backend.ChunkMeta{
			SystemFingerprint: backend.AgwSystemFingerprint(provider.WireModel()),
		})
	} else {
		agentOptions.Model = piagentbridge.Model(model)
		base := piagentbridge.BackendStreamFnWithObserver(l.opts.Backend, model, sink.noteTransport)
		agentOptions.StreamFn = func(ctx context.Context, m *pi.Model, transcript pi.TranscriptContext, opts pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
			// TurnInput carries the per-turn sampling knobs; PiG's
			// StreamOptions are built from the agent's options, which have no
			// per-turn view.
			if input.MaxTokens > 0 {
				opts.MaxTokens = input.MaxTokens
			}
			if input.Temperature != 0 {
				opts.Temperature = input.Temperature
				opts.TemperatureSet = true
			}
			cascadeTransition(l.cascade, StateExecute, "stream opened")
			stream, err := base(ctx, m, transcript, opts)
			if err != nil {
				failures.set(err)
			}
			return stream, err
		}
	}

	runner, err := piagent.NewAgent(agentOptions)
	if err != nil {
		close(events)
		<-drained
		cascadeTransition(l.cascade, StateError, fmt.Sprintf("agent: %v", err))
		sink.emit(backend.StreamChunk{Error: err, Done: true})
		return err
	}

	// Cycle effort-picker: the loop's configured Effort (--effort
	// or /effort <level>, plus whatever OpenOptions carries) is the
	// level the next Send should use. PiG clamps to the model's
	// ThinkingLevelMap, so a level the model doesn't support silently
	// narrows to the closest valid one — matching agy's behaviour.
	if l.opts.Effort != "" {
		runner.SetThinkingLevel(pi.ModelThinkingLevel(l.opts.Effort))
	}

	_, err = runner.SendMessages(ctx, pigMessages(input.Messages))
	close(events)
	<-drained

	cascade := l.cascade
	// A canceled context is the caller's own failure and outranks whatever the
	// provider reported on the way out (the pre-cycle-183 loop returned
	// ctx.Err() here too).
	if ctxErr := ctx.Err(); ctxErr != nil {
		cascadeTransition(cascade, StateError, fmt.Sprintf("ctx canceled: %v", ctxErr))
		sink.emit(backend.StreamChunk{Error: ctxErr, Done: true})
		return ctxErr
	}

	failure := sink.err()
	if concrete := failures.get(); concrete != nil {
		// Hand back the original error, not PiG's copy of its text, so
		// errors.As still reaches it: cmd/wagyu/api_error.go classifies
		// *backend.HTTPError off the turn error to recover the reference's
		// user-facing message and exit code.
		failure = concrete
	}

	switch {
	case errors.Is(err, piagent.ErrMaxTurnsReached) && l.opts.ToolRunner == nil:
		// The clone's contract for "the model asked to act and nobody can run
		// it" is a finished turn, not a failure: loop.go used to return nil as
		// soon as it saw tool calls with a nil runner.
		cascadeTransition(cascade, StateSave, "tool calls surfaced without a runner")
		cascadeTransition(cascade, StateDone, "turn complete")
		return failure
	case errors.Is(err, piagent.ErrMaxTurnsReached):
		cascadeTransition(cascade, StateError,
			fmt.Sprintf("tool-iteration budget (%d) exhausted", maxTurns))
		// Cycle 279: wrap PiG's sentinel instead of flattening it. Auto-
		// continue classifies iteration-cap separately from a stream
		// interrupt (it injects the AGY "**Status**: Cancelled after %d
		// iterations" copy for the former), and that branch keys off
		// errors.Is(err, ErrMaxTurnsReached) — which the old
		// fmt.Errorf("… (%d)") string lost, so a budget exhaustion was
		// replayed as if the stream had dropped.
		return fmt.Errorf("agent: exceeded tool-iteration budget (%d): %w", maxTurns, err)
	case failure != nil:
		// PiG encoded the failure in the stream and the sink surfaced it to
		// the handler already; the clone's Send returns it.
		return failure
	case err != nil:
		cascadeTransition(cascade, StateError, fmt.Sprintf("agent run failed: %v", err))
		sink.emit(backend.StreamChunk{Error: err, Done: true})
		return err
	}
	return nil
}

// errorSlot carries a concrete error past PiG's string-only boundary. One slot
// belongs to one Send, written from the transport and read after the run
// settles, so the mutex guards ordering rather than contention.
type errorSlot struct {
	mu  sync.Mutex
	err error
}

func (s *errorSlot) set(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

func (s *errorSlot) get() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// recordingProvider forwards to the real provider and remembers any error it
// returns. PiG calls ai.Provider.Stream without a wrapper of its own
// (agent_loop.go:412), so this is the only place the type can be kept.
type recordingProvider struct {
	inner    pi.Provider
	failures *errorSlot
}

func (p *recordingProvider) ID() string   { return p.inner.ID() }
func (p *recordingProvider) Close() error { return p.inner.Close() }

func (p *recordingProvider) Stream(ctx context.Context, transcript pi.TranscriptContext, opts pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
	stream, err := p.inner.Stream(ctx, transcript, opts)
	if err != nil {
		p.failures.set(err)
	}
	return stream, err
}

// agwBackend is implemented by backend clients that can describe how they
// reach agw. The interface and the nativeProvider derivation moved to
// internal/piagentbridge/native_provider.go during cycle rc-piagent-bind
// so the cycle's cascade seam (piagent_remote.go) could call them without
// a cycle through this package. The behaviour is unchanged; this
// delegation keeps every existing caller's contract identical.

// NativeProvider exposes piagentbridge.NativeProvider for callers that
// already import this package. The implementation moved during cycle
// rc-piagent-bind so the cycle's cascade seam (piagent_remote.go) could
// reuse it without an import cycle through internal/agent.
//
// It returns nil for a backend that does not describe an agw endpoint
// (mock, lifecycle wrappers), which is the signal to stay on the
// clone's own loop.
func NativeProvider(client backend.Client) *piagentbridge.Provider {
	return piagentbridge.NativeProvider(client)
}

// nativeProvider is the internal alias the Loop.Send helper above uses.
// Same name as before so the call site is unchanged; just a one-line
// trampoline now that the implementation lives in piagentbridge.
func nativeProvider(client backend.Client) *piagentbridge.Provider {
	return piagentbridge.NativeProvider(client)
}

// pigMaxTurns maps the clone's tool-iteration budget onto PiG's turn cap.
//
// With a ToolRunner, MaxToolIterations is exactly PiG's notion of a turn (one
// model response plus the tool batch it asked for), so the numbers line up:
// budget N runs the tool batch N times and then reports exhaustion. A zero
// budget means no cap (PiG's MaxTurns 0 = unlimited), which is the default
// since cycle 279 — see defaultMaxToolIterations.
//
// Without a ToolRunner the turn is capped at 1. The caller only wants the tool
// calls surfaced — the pre-cycle-183 loop returned after the first stream with
// nobody to run them — and sendPig reads PiG's turn-limit error as that
// finished turn rather than a failure.
func pigMaxTurns(opts Options) int {
	if opts.ToolRunner == nil {
		return 1
	}
	if opts.MaxToolIterations > 0 {
		return opts.MaxToolIterations
	}
	return defaultMaxToolIterations
}

// pigTools adapts the advertised tool declarations into PiG tools, each backed
// by the caller's ToolRunner.
//
// A nil ToolRunner advertises nothing. The clone's loop still surfaced the
// model's tool calls to the handler in that case, and it still does — the calls
// ride on the assistant message, which the translator reads regardless of
// whether anything was advertised.
//
// ExtraAgentTools are appended verbatim. Cycle 228 uses this seam for
// subagent tools (internal/subagenttools) that drive their own LLM turn and
// therefore cannot share the clone's runner pipeline.
func (l *Loop) pigTools() []piagent.AgentTool {
	extraByName := make(map[string]piagent.AgentTool, len(l.opts.ExtraAgentTools))
	for _, t := range l.opts.ExtraAgentTools {
		if t != nil {
			extraByName[t.Name()] = t
		}
	}
	used := make(map[string]bool, len(extraByName))

	var tools []piagent.AgentTool
	if l.opts.ToolRunner != nil {
		tools = make([]piagent.AgentTool, 0, len(l.opts.Tools)+len(l.opts.ExtraAgentTools))
		for _, decl := range l.opts.Tools {
			if extra, ok := extraByName[decl.Name]; ok {
				// Cycle 293: the declaration is the reference's captured
				// schema, which is what goes on the wire. The extra keeps its
				// real implementation behind that declaration (its own schema
				// describes a different, clone-shaped wire).
				used[decl.Name] = true
				tools = append(tools, &declOverrideTool{decl: decl, exec: extra})
				continue
			}
			tools = append(tools, &runnerTool{decl: decl, runner: l.opts.ToolRunner})
		}
	} else if len(l.opts.Tools) == 0 {
		// No runner + no decls: still append any extras the caller wired in,
		// so a single-turn loop with subagenttools.Extras is not silently
		// empty.
		if len(l.opts.ExtraAgentTools) == 0 {
			return nil
		}
		tools = make([]piagent.AgentTool, 0, len(l.opts.ExtraAgentTools))
	}
	for _, t := range l.opts.ExtraAgentTools {
		if t != nil && !used[t.Name()] {
			tools = append(tools, t)
		}
	}
	return tools
}

// declOverrideTool advertises one captured backend.ToolDecl while executing
// through a PiG AgentTool that owns the same name. Cycle 293 introduced it for
// the three subagent tools: their clone-shaped PiG schemas (prompt /
// subagent_id / action) must not replace the reference's captured ones
// (Prompt / Recipient+Message / Action+ConversationIds) on the wire, but the
// implementations are still the live PiG ones.
type declOverrideTool struct {
	decl backend.ToolDecl
	exec piagent.AgentTool
}

func (t *declOverrideTool) Name() string { return t.decl.Name }

func (t *declOverrideTool) Label() string { return t.exec.Label() }

func (t *declOverrideTool) Schema() pi.ToolSchema {
	schema := pi.ToolSchema{Name: t.decl.Name, Description: t.decl.Description}
	if params := tools.SchemaParams(t.decl.Parameters); params != nil {
		schema.Parameters = params
	}
	return schema
}

func (t *declOverrideTool) ExecutionMode() piagent.ToolExecutionMode {
	return t.exec.ExecutionMode()
}

func (t *declOverrideTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, onUpdate piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	return t.exec.Execute(ctx, toolCallID, args, onUpdate)
}

// runnerTool is one advertised tool declaration wired to the loop's
// ToolRunner. It preserves the clone's error convention: a failing tool is
// reported to the model as the text "tool error: …" and never aborts the turn.
type runnerTool struct {
	decl   backend.ToolDecl
	runner ToolRunner
}

func (t *runnerTool) Name() string  { return t.decl.Name }
func (t *runnerTool) Label() string { return t.decl.Name }

func (t *runnerTool) Schema() pi.ToolSchema {
	schema := pi.ToolSchema{Name: t.decl.Name, Description: t.decl.Description}
	if params := tools.SchemaParams(t.decl.Parameters); params != nil {
		schema.Parameters = params
	}
	return schema
}

// ExecutionMode is sequential — the clone's loop ran one call at a time, and
// the fs/shell tools mutate shared workspace state.
func (t *runnerTool) ExecutionMode() piagent.ToolExecutionMode {
	return piagent.ToolModeSequential
}

func (t *runnerTool) Execute(ctx context.Context, toolCallID string, args json.RawMessage, _ piagent.ToolUpdateCallback) (piagent.AgentToolResult, error) {
	result, err := t.runner.Run(ctx, backend.ToolCall{
		ID:        toolCallID,
		Name:      t.decl.Name,
		Arguments: args,
	})
	if err != nil {
		result = "tool error: " + err.Error()
	}
	return piagent.AgentToolResult{
		Content: []pi.ToolResultMessageContent{pi.TextContent{Text: result}},
	}, nil
}

// pigMessages converts the clone's conversation into PiG's transcript.
//
// Role mapping is the inverse of piagentbridge.ToStreamRequest: the clone's
// flat Message becomes one PiG message union arm, with an assistant turn's
// ToolCalls becoming ToolCall content blocks and a `tool` turn becoming a
// ToolResultMessage carrying ToolCallID + Name.
//
// An empty conversation gets one empty user message. PiG's SendMessages is a
// documented no-op on an empty slice (agent.go:1126) because upstream's
// prompt(messages) always has a message to start the turn from, whereas the
// clone's loop would happily issue a request with no messages at all. The
// synthesized turn is the narrowest thing that still starts exactly one
// upstream request; every real caller hands over a populated conversation.
func pigMessages(messages []backend.Message) []piagent.AgentMessage {
	out := make([]piagent.AgentMessage, 0, len(messages))
	now := time.Now().UnixMilli()
	for _, message := range messages {
		switch message.Role {
		case "user":
			out = append(out, piagent.AgentMessage{User: &piagent.UserMessage{
				Role:      piagent.RoleUser,
				Content:   pi.UserText(message.Content),
				Timestamp: now,
			}})
		case "assistant":
			blocks := make([]pi.AssistantContentBlock, 0, len(message.ToolCalls)+1)
			if message.Content != "" {
				blocks = append(blocks, pi.TextContent{Text: message.Content})
			}
			for _, call := range message.ToolCalls {
				blocks = append(blocks, pi.ToolCall{
					ID:        call.ID,
					Name:      call.Name,
					Arguments: decodeToolArguments(call.Arguments),
				})
			}
			out = append(out, piagent.AgentMessage{Assistant: &piagent.AssistantMessage{
				Role:      piagent.RoleAssistant,
				Content:   blocks,
				Timestamp: now,
			}})
		case "tool":
			out = append(out, piagent.AgentMessage{ToolResult: &piagent.ToolResultMessage{
				Role:       piagent.RoleToolResult,
				ToolCallID: message.ToolCallID,
				ToolName:   message.Name,
				Content:    []pi.ToolResultMessageContent{pi.TextContent{Text: message.Content}},
				Timestamp:  now,
			}})
		case "system":
			out = append(out, piagent.AgentMessage{System: &pi.SystemMessage{
				Content:   pi.SystemText(message.Content),
				Timestamp: now,
			}})
		}
	}
	if len(out) == 0 {
		out = append(out, piagent.AgentMessage{User: &piagent.UserMessage{
			Role:      piagent.RoleUser,
			Content:   pi.UserText(""),
			Timestamp: now,
		}})
	}
	return out
}

func decodeToolArguments(raw json.RawMessage) pi.JsonObject {
	if len(raw) == 0 {
		return pi.JsonObject{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil || decoded == nil {
		return pi.JsonObject{}
	}
	return pi.JsonObject(decoded)
}

// ─── PiG events -> the clone's StreamChunk ───────────────────────────────────

// chunkSink translates PiG's event stream back into the chunk vocabulary the
// Handler and the Cascade observer have always spoken.
//
// Ordering mirrors the pre-cycle-183 loop: text deltas as they arrive, then the
// turn's tool calls, then a terminating chunk carrying usage (or the error).
// One PiG assistant message == one upstream request == one Done chunk, which is
// exactly the cadence streamOnce produced.
// transportMeta is what the transport reported that ai.Usage has no field for.
//
// present exists because PiG always hands the loop a non-nil *ai.Usage, zeroed
// when the stream carried none, so "no metadata" and "metadata with zero tokens"
// are indistinguishable from Usage alone. The observer only fires for a chunk
// that actually carried a ChunkMeta, so it knows the difference.
type transportMeta struct {
	present     bool
	fingerprint string
}

type chunkSink struct {
	handler Handler
	cascade *Cascade
	// loop is the Loop that owns this sink. Cycle 227: the sink
	// forwards CompactionStart/End events to l.recordCompact, which
	// updates LastCompact and fires the OnCompactionStart/End hooks.
	// Nil when the sink is built outside a Loop (tests that drive
	// their own drain).
	loop *Loop

	// transport is the switch-provider usage record's non-Usage part,
	// recorded beside the chunks rather than through PiG's message model.
	// Only cmd/openai-shim reads the fingerprint, but dropping it would make
	// the shims report a different system_fingerprint than they did before
	// the loop moved onto PiG.
	transport transportMeta

	mu      sync.Mutex
	failure error
}

// noteTransport records that the transport reported chunk metadata, and which
// fingerprint it carried. Called from the pump goroutine, and again by the
// native path before the run starts, so it is serialised with the read.
func (s *chunkSink) noteTransport(meta backend.ChunkMeta) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transport = transportMeta{present: true, fingerprint: meta.SystemFingerprint}
}

func (s *chunkSink) transportMeta() transportMeta {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transport
}

func (s *chunkSink) err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failure
}

func (s *chunkSink) record(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failure == nil {
		s.failure = err
	}
}

func (s *chunkSink) observe(event piagent.AgentEvent) {
	// Cycle 227: route PiG compaction events to the Loop's record before
	// they reach the chunk handler. The Loop only invokes
	// OnCompactionStart/End when the caller set them, so observers that
	// don't care (print path) pay nothing.
	if _, isCompact := event.(piagent.CompactionStartEvent); isCompact {
		if s.loop != nil {
			s.loop.recordCompact(event)
		}
	} else if _, isCompact := event.(piagent.CompactionEndEvent); isCompact {
		if s.loop != nil {
			s.loop.recordCompact(event)
		}
	}
	switch value := event.(type) {
	case piagent.MessageUpdateEvent:
		switch update := value.AssistantMessageEvent.(type) {
		case pi.TextDeltaEvent:
			if update.Delta != "" {
				s.emit(backend.StreamChunk{Text: update.Delta})
			}
		case pi.ToolCallEndEvent:
			s.emit(backend.StreamChunk{ToolCalls: []backend.ToolCall{toolCallFromPig(update.ToolCall)}})
		}
	case piagent.MessageEndEvent:
		if value.Message.Assistant != nil {
			s.endAssistant(value.Message.Assistant)
		}
	}
}

// endAssistant closes one upstream response, which is where the clone used to
// see its Done (or Error) chunk.
func (s *chunkSink) endAssistant(message *piagent.AssistantMessage) {
	switch message.StopReason {
	case pi.StopReasonError, pi.StopReasonAborted:
		text := message.ErrorMessage
		if text == "" {
			text = "agent: upstream stream failed"
		}
		failure := errors.New(text)
		s.record(failure)
		cascadeTransition(s.cascade, StateError, fmt.Sprintf("stream error: %v", failure))
		s.emit(backend.StreamChunk{Error: failure, Done: true})
	default:
		cascadeTransition(s.cascade, StateSave, "received done marker")
		cascadeTransition(s.cascade, StateDone, "turn complete")
		s.emit(backend.StreamChunk{Done: true, Meta: metaFromPig(message.Usage, s.transportMeta())})
	}
}

func (s *chunkSink) emit(chunk backend.StreamChunk) {
	if chunk.Text != "" {
		cascadeTransition(s.cascade, StateStream, fmt.Sprintf("chunk %d bytes", len(chunk.Text)))
	}
	if err := safeCall(s.handler, chunk); err != nil {
		// The pre-cycle-183 loop stopped the stream and returned this; PiG's
		// run is already in flight, so it is recorded and returned once the
		// run settles.
		s.record(fmt.Errorf("agent: handler error: %w", err))
	}
}

func toolCallFromPig(call pi.ToolCall) backend.ToolCall {
	arguments, err := json.Marshal(call.Arguments)
	if err != nil {
		arguments = []byte("{}")
	}
	return backend.ToolCall{ID: call.ID, Name: call.Name, Arguments: arguments}
}

// metaFromPig builds the chunk metadata the clone's Handler has always
// received. The token counts come from PiG's usage; the fingerprint cannot,
// because ai.Usage has no field for it, so the transport passes it in.
//
// present distinguishes the two cases backend.AgwK0sClient distinguishes: it
// builds its ChunkMeta inside the `if chunk.Usage != nil` branch, so a gateway
// that reports no usage produces no metadata and no fingerprint either. PiG
// zeroes Usage instead of nil-ing it, which is why the flag cannot be derived
// from the usage alone.
func metaFromPig(usage *pi.Usage, transport transportMeta) *backend.ChunkMeta {
	if !transport.present {
		return nil
	}
	meta := &backend.ChunkMeta{SystemFingerprint: transport.fingerprint}
	if usage == nil {
		return meta
	}
	meta.PromptTokens = usage.Input
	meta.CompletionTokens = usage.Output
	meta.TotalTokens = usage.TotalTokens
	if usage.Reasoning != nil {
		meta.ReasoningTokens = *usage.Reasoning
	}
	return meta
}
