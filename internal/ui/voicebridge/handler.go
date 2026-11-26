package voicebridge

import (
	"strings"

	voicebackend "github.com/justwasm/pigasus/internal/voice"
)

// BridgeHandler adapts the bridge to the live session's handler contracts.
//
// It is the module's own seam: the session calls it from its audio
// goroutines, and every event becomes a progress-sink call (or, for a
// Thinker cancellation, a state transition on the bridge). The host's sink
// decides where each event is rendered.
type BridgeHandler struct {
	// Bridge is the model the handler reports into.
	Bridge *Model
}

// compile-time proof that the adapter satisfies both handler contracts.
var (
	_ voicebackend.NativeLiveHandler    = (*BridgeHandler)(nil)
	_ voicebackend.ThinkerCancelHandler = (*BridgeHandler)(nil)
)

// OnUserSpeech forwards interim and committed recognition to the sink.
func (h *BridgeHandler) OnUserSpeech(text string, interim bool) {
	sink := h.sink()
	if sink == nil {
		return
	}
	if interim {
		sink.UserSpeechInterim(text)
		return
	}
	if text != "" {
		sink.UserSpeechInterim("")
		sink.UserSpeech(text)
	}
}

// OnAgentSpeech forwards the agent's complete spoken reply.
func (h *BridgeHandler) OnAgentSpeech(text string) {
	if text == "" {
		return
	}
	if sink := h.sink(); sink != nil {
		sink.AgentSpeech(text)
	}
}

// OnAgentSpeechDelta forwards one streamed chunk of the reply.
func (h *BridgeHandler) OnAgentSpeechDelta(delta string) {
	if delta == "" {
		return
	}
	if sink := h.sink(); sink != nil {
		sink.AgentSpeechDelta(delta)
	}
}

// OnTurnComplete is a no-op: the host learns of a finished turn through the
// agent's speech, which carries the text.
func (h *BridgeHandler) OnTurnComplete() {}

// OnInterrupted reports a barge-in.
func (h *BridgeHandler) OnInterrupted() {
	if sink := h.sink(); sink != nil {
		sink.Interrupted()
	}
}

// OnToolCall is a no-op: tool activity is rendered from the Thinker's
// progress notes, which arrive through RunThinker.
func (h *BridgeHandler) OnToolCall(call voicebackend.LiveToolCall) {}

// OnThinkerHandoff forwards a task handed to the Thinker agent.
func (h *BridgeHandler) OnThinkerHandoff(invocationID string, task string) {
	if strings.TrimSpace(task) == "" {
		return
	}
	if sink := h.sink(); sink != nil {
		sink.ThinkerHandoff(invocationID, task)
	}
}

// OnThinkerCancel cancels the armed Thinker turn and tells the caller what
// the agent hears.
func (h *BridgeHandler) OnThinkerCancel(invocationID string) {
	if h == nil || h.Bridge == nil {
		return
	}
	h.Bridge.CancelThinker(invocationID)
}

// OnError rewrites the transport's authentication rejection into the
// reference's own guidance and reports it.
func (h *BridgeHandler) OnError(err error) {
	if err == nil {
		return
	}
	msg := err.Error()
	if strings.Contains(msg, "insufficient authentication scopes") {
		msg = "Google AI Studio live connection rejected (insufficient authentication scopes). Set GEMINI_API_KEY or configure a proxy endpoint in ~/.openagy/config.json."
	}
	if sink := h.sink(); sink != nil {
		sink.LiveError(errorString(msg))
	}
}

// OnClose drops the session from the bridge. The session hangs itself up on
// the way out, so the bridge only forgets it.
func (h *BridgeHandler) OnClose() {
	if h == nil || h.Bridge == nil {
		return
	}
	h.Bridge.SetLive(nil)
}

// OnReconnecting is a no-op: a reconnection is seamless and surfaces through
// the session's connection state, not the transcript.
func (h *BridgeHandler) OnReconnecting(attempt int, maxAttempts int, err error) {}

// OnReconnected is a no-op: the transcript is not polluted with notices.
func (h *BridgeHandler) OnReconnected() {}

func (h *BridgeHandler) sink() ProgressSink {
	if h == nil || h.Bridge == nil {
		return nil
	}
	return h.Bridge.sink()
}

// errorString carries an already-formatted failure message, so the sink
// receives the reference's wording rather than the transport's.
type errorString string

func (e errorString) Error() string { return string(e) }
