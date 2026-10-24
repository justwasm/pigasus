package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/backend"
)

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
// present distinguishes the two cases backend.GatewayClient distinguishes: it
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
