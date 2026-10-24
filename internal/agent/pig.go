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
	"errors"
	"fmt"
	"sync"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/piagentbridge"
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
	failures := &errorSlot{}

	events := make(chan piagent.AgentEvent, 128)
	drained := drainEvents(events, sink)

	maxTurns := pigMaxTurns(l.opts)
	cascadeTransition(l.cascade, StatePlan, "selected backend+model")

	agentOptions := l.buildAgentOptions(model, events, sink, failures, input)
	runner, err := piagent.NewAgent(agentOptions)
	if err != nil {
		close(events)
		<-drained
		cascadeTransition(l.cascade, StateError, fmt.Sprintf("agent: %v", err))
		sink.emit(backend.StreamChunk{Error: err, Done: true})
		return err
	}

	if l.opts.Effort != "" {
		runner.SetThinkingLevel(pi.ModelThinkingLevel(l.opts.Effort))
	}

	_, err = runner.SendMessages(ctx, pigMessages(input.Messages))
	close(events)
	<-drained

	return l.handleSendPigResult(ctx, err, sink, failures, maxTurns)
}

func drainEvents(events <-chan piagent.AgentEvent, sink *chunkSink) <-chan struct{} {
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for event := range events {
			sink.observe(event)
		}
	}()
	return drained
}

func (l *Loop) buildAgentOptions(
	model string,
	events chan piagent.AgentEvent,
	sink *chunkSink,
	failures *errorSlot,
	input TurnInput,
) piagent.AgentOptions {
	provider := l.opts.Provider
	if provider == nil {
		provider = nativeProvider(l.opts.Backend)
	}

	agentOptions := piagent.AgentOptions{
		Tools:         l.pigTools(),
		SystemPrompt:  l.opts.SystemPrompt,
		EventCh:       events,
		MaxTurns:      pigMaxTurns(l.opts),
		ToolExecution: piagent.ToolModeSequential,
	}

	if provider != nil {
		bound := provider.Model(model)
		bound.Provider = &recordingProvider{inner: bound.Provider, failures: failures}
		agentOptions.Model = bound
		agentOptions.StreamFn = piagentbridge.ProviderStreamFn()
		sink.noteTransport(backend.ChunkMeta{
			SystemFingerprint: backend.GatewaySystemFingerprint(provider.Endpoint(), provider.WireModel()),
		})
		return agentOptions
	}

	agentOptions.Model = piagentbridge.Model(model)
	agentOptions.StreamFn = l.buildBackendStreamFn(model, sink, failures, input)
	return agentOptions
}

func (l *Loop) buildBackendStreamFn(
	model string,
	sink *chunkSink,
	failures *errorSlot,
	input TurnInput,
) piagent.StreamFn {
	base := piagentbridge.BackendStreamFnWithObserver(l.opts.Backend, model, sink.noteTransport)
	return func(ctx context.Context, m *pi.Model, transcript pi.TranscriptContext, opts pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
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

func (l *Loop) handleSendPigResult(
	ctx context.Context,
	err error,
	sink *chunkSink,
	failures *errorSlot,
	maxTurns int,
) error {
	cascade := l.cascade
	if ctxErr := ctx.Err(); ctxErr != nil {
		cascadeTransition(cascade, StateError, fmt.Sprintf("ctx canceled: %v", ctxErr))
		sink.emit(backend.StreamChunk{Error: ctxErr, Done: true})
		return ctxErr
	}

	failure := sink.err()
	if concrete := failures.get(); concrete != nil {
		failure = concrete
	}

	switch {
	case errors.Is(err, piagent.ErrMaxTurnsReached) && l.opts.ToolRunner == nil:
		cascadeTransition(cascade, StateSave, "tool calls surfaced without a runner")
		cascadeTransition(cascade, StateDone, "turn complete")
		return failure
	case errors.Is(err, piagent.ErrMaxTurnsReached):
		cascadeTransition(cascade, StateError,
			fmt.Sprintf("tool-iteration budget (%d) exhausted", maxTurns))
		return fmt.Errorf("agent: exceeded tool-iteration budget (%d): %w", maxTurns, err)
	case failure != nil:
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

// NativeProvider exposes piagentbridge.NativeProvider for callers that
// already import this package. The implementation moved during cycle
// rc-piagent-bind so the cycle's cascade seam (piagent_remote.go) could
// reuse it without an import cycle through internal/agent.
//
// It returns nil for a backend that does not describe a gateway endpoint
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
