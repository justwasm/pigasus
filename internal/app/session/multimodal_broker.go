package session

import (
	"strings"
	"sync"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// TurnModality identifies the sensory/interaction modality of a conversation turn.
type TurnModality string

const (
	ModalityText    TurnModality = "text"
	ModalityVoice   TurnModality = "voice"
	ModalityLive    TurnModality = "live"
	ModalityThinker TurnModality = "thinker"
	ModalityVision  TurnModality = "vision"
)

// LiveVoiceRouter is the minimal interface required for routing interactive turns
// to an active full-duplex live session.
type LiveVoiceRouter interface {
	IsActive() bool
	SendText(text string) error
}

// MultimodalBroker coordinates conversation turns across diverse input modalities
// (keyboard text, microphone speech, live duplex audio, and thinker delegator).
type MultimodalBroker struct {
	mu          sync.RWMutex
	session     *Session
	voiceRouter LiveVoiceRouter
	listeners   []func(turn backend.Message, modality TurnModality)
}

// NewMultimodalBroker constructs a multimodal turn broker bound to a session.
func NewMultimodalBroker(sess *Session) *MultimodalBroker {
	return &MultimodalBroker{
		session: sess,
	}
}

// SetVoiceRouter registers the active live voice session router.
func (b *MultimodalBroker) SetVoiceRouter(r LiveVoiceRouter) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.voiceRouter = r
}

// VoiceRouter returns the registered live voice session router, if any.
func (b *MultimodalBroker) VoiceRouter() LiveVoiceRouter {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.voiceRouter
}

// IsLiveVoiceActive reports whether a live voice session is connected and active.
func (b *MultimodalBroker) IsLiveVoiceActive() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.voiceRouter != nil && b.voiceRouter.IsActive()
}

// RecordVoiceTurn records a speech turn into the session domain transcript.
func (b *MultimodalBroker) RecordVoiceTurn(role, text string) backend.Message {
	text = strings.TrimSpace(text)
	if text == "" {
		return backend.Message{}
	}
	role = strings.ToLower(role)
	if role != "user" {
		role = "assistant"
	}
	msg := backend.Message{
		Role:    role,
		Content: text,
	}
	if b.session != nil {
		b.session.AppendMessage(msg)
	}
	b.notify(msg, ModalityVoice)
	return msg
}

// DispatchPrompt routes an incoming user prompt through the broker.
// If a live voice session is active, it forwards text to the live session
// (prompting speech synthesis) and records it to the session; otherwise returns
// handledByVoice = false so standard model turn execution continues.
func (b *MultimodalBroker) DispatchPrompt(prompt string) (handledByVoice bool, err error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return false, nil
	}
	b.mu.RLock()
	r := b.voiceRouter
	b.mu.RUnlock()

	if r != nil && r.IsActive() {
		msg := backend.Message{
			Role:    "user",
			Content: prompt,
		}
		if b.session != nil {
			b.session.AppendMessage(msg)
		}
		b.notify(msg, ModalityLive)
		if err := r.SendText(prompt); err != nil {
			return true, err
		}
		return true, nil
	}
	return false, nil
}

// DispatchVisionFrame sends a visual frame or terminal screen snapshot into the live session if active.
func (b *MultimodalBroker) DispatchVisionFrame(mimeType string, data []byte, textSummary string) error {
	b.mu.RLock()
	r := b.voiceRouter
	b.mu.RUnlock()

	if r != nil && r.IsActive() {
		if ms, ok := r.(interface{ SendMedia(string, []byte) error }); ok && len(data) > 0 {
			_ = ms.SendMedia(mimeType, data)
		}
		if textSummary != "" {
			_ = r.SendText(textSummary)
		}
		msg := backend.Message{
			Role:    "user",
			Content: textSummary,
		}
		if b.session != nil {
			b.session.AppendMessage(msg)
		}
		b.notify(msg, ModalityVision)
		return nil
	}
	return nil
}

// Subscribe registers a listener for turns processed by this broker.
func (b *MultimodalBroker) Subscribe(fn func(turn backend.Message, modality TurnModality)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.listeners = append(b.listeners, fn)
}

func (b *MultimodalBroker) notify(turn backend.Message, modality TurnModality) {
	b.mu.RLock()
	listeners := append([]func(turn backend.Message, modality TurnModality){}, b.listeners...)
	b.mu.RUnlock()
	for _, l := range listeners {
		if l != nil {
			l(turn, modality)
		}
	}
}
