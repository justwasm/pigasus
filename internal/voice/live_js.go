//go:build js

package voice

import (
	"sync"
	"syscall/js"
)

// JSWebVoiceBridge connects the Go duplex voice session with the browser's JavaScript runtime.
type JSWebVoiceBridge struct {
	mu            sync.RWMutex
	onAudioCb     js.Value
	onUserCb      js.Value
	onAgentCb     js.Value
	onAgentDeltaCb js.Value
	onTurnCb      js.Value
	onInterruptCb js.Value
	onErrorCb     js.Value
	onCloseCb     js.Value
}

var globalJSVoiceBridge = &JSWebVoiceBridge{}

func init() {
	globalJSVoiceBridge.registerGlobalAPI()
}

// GlobalJSVoiceBridge returns the singleton JSWebVoiceBridge.
func GlobalJSVoiceBridge() *JSWebVoiceBridge {
	return globalJSVoiceBridge
}

func (b *JSWebVoiceBridge) registerGlobalAPI() {
	global := js.Global()
	if !global.Truthy() {
		return
	}

	api := global.Get("Object").New()

	api.Set("onAudio", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Type() == js.TypeFunction {
			b.mu.Lock()
			b.onAudioCb = args[0]
			b.mu.Unlock()
		}
		return nil
	}))

	api.Set("onUserSpeech", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Type() == js.TypeFunction {
			b.mu.Lock()
			b.onUserCb = args[0]
			b.mu.Unlock()
		}
		return nil
	}))

	api.Set("onAgentSpeech", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Type() == js.TypeFunction {
			b.mu.Lock()
			b.onAgentCb = args[0]
			b.mu.Unlock()
		}
		return nil
	}))

	api.Set("onAgentSpeechDelta", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Type() == js.TypeFunction {
			b.mu.Lock()
			b.onAgentDeltaCb = args[0]
			b.mu.Unlock()
		}
		return nil
	}))

	api.Set("onTurnComplete", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Type() == js.TypeFunction {
			b.mu.Lock()
			b.onTurnCb = args[0]
			b.mu.Unlock()
		}
		return nil
	}))

	api.Set("onInterrupted", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Type() == js.TypeFunction {
			b.mu.Lock()
			b.onInterruptCb = args[0]
			b.mu.Unlock()
		}
		return nil
	}))

	api.Set("onError", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Type() == js.TypeFunction {
			b.mu.Lock()
			b.onErrorCb = args[0]
			b.mu.Unlock()
		}
		return nil
	}))

	api.Set("onClose", js.FuncOf(func(this js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].Type() == js.TypeFunction {
			b.mu.Lock()
			b.onCloseCb = args[0]
			b.mu.Unlock()
		}
		return nil
	}))

	global.Set("piggaVoice", api)
	global.Set("openagyVoice", api)
}

// EmitAudio sends a 24kHz 16-bit mono PCM chunk to registered JS listeners and DOM events.
func (b *JSWebVoiceBridge) EmitAudio(pcm []byte) {
	if len(pcm) == 0 {
		return
	}
	global := js.Global()
	if !global.Truthy() {
		return
	}

	u8 := global.Get("Uint8Array").New(len(pcm))
	js.CopyBytesToJS(u8, pcm)

	b.mu.RLock()
	cb := b.onAudioCb
	b.mu.RUnlock()

	if cb.Truthy() && cb.Type() == js.TypeFunction {
		go func(fn js.Value, data js.Value) {
			defer func() { _ = recover() }()
			fn.Invoke(data)
		}(cb, u8)
	}

	// Dispatch DOM CustomEvent
	customEvCtor := global.Get("CustomEvent")
	if customEvCtor.Truthy() && global.Get("dispatchEvent").Truthy() {
		detail := global.Get("Object").New()
		detail.Set("pcm", u8)
		detail.Set("sampleRate", 24000)
		detail.Set("channels", 1)

		opts := global.Get("Object").New()
		opts.Set("detail", detail)

		ev := customEvCtor.New("openagy-voice-audio", opts)
		global.Call("dispatchEvent", ev)
	}
}

// JSWebLiveHandler implements NativeLiveHandler for the browser environment.
type JSWebLiveHandler struct {
	bridge *JSWebVoiceBridge
}

// NewJSWebLiveHandler creates a new live event handler dispatching to JS.
func NewJSWebLiveHandler() *JSWebLiveHandler {
	return &JSWebLiveHandler{
		bridge: GlobalJSVoiceBridge(),
	}
}

func (h *JSWebLiveHandler) OnUserSpeech(text string, interim bool) {
	h.bridge.mu.RLock()
	cb := h.bridge.onUserCb
	h.bridge.mu.RUnlock()

	if cb.Truthy() && cb.Type() == js.TypeFunction {
		cb.Invoke(text, interim)
	}

	global := js.Global()
	if global.Truthy() && global.Get("CustomEvent").Truthy() && global.Get("dispatchEvent").Truthy() {
		detail := global.Get("Object").New()
		detail.Set("text", text)
		detail.Set("interim", interim)
		opts := global.Get("Object").New()
		opts.Set("detail", detail)
		ev := global.Get("CustomEvent").New("openagy-user-speech", opts)
		global.Call("dispatchEvent", ev)
	}
}

func (h *JSWebLiveHandler) OnAgentSpeech(text string) {
	h.bridge.mu.RLock()
	cb := h.bridge.onAgentCb
	h.bridge.mu.RUnlock()

	if cb.Truthy() && cb.Type() == js.TypeFunction {
		cb.Invoke(text)
	}

	global := js.Global()
	if global.Truthy() && global.Get("CustomEvent").Truthy() && global.Get("dispatchEvent").Truthy() {
		detail := global.Get("Object").New()
		detail.Set("text", text)
		opts := global.Get("Object").New()
		opts.Set("detail", detail)
		ev := global.Get("CustomEvent").New("openagy-agent-speech", opts)
		global.Call("dispatchEvent", ev)
	}
}

func (h *JSWebLiveHandler) OnAgentSpeechDelta(delta string) {
	h.bridge.mu.RLock()
	cb := h.bridge.onAgentDeltaCb
	h.bridge.mu.RUnlock()

	if cb.Truthy() && cb.Type() == js.TypeFunction {
		cb.Invoke(delta)
	}
}

func (h *JSWebLiveHandler) OnTurnComplete() {
	h.bridge.mu.RLock()
	cb := h.bridge.onTurnCb
	h.bridge.mu.RUnlock()

	if cb.Truthy() && cb.Type() == js.TypeFunction {
		cb.Invoke()
	}

	global := js.Global()
	if global.Truthy() && global.Get("CustomEvent").Truthy() && global.Get("dispatchEvent").Truthy() {
		ev := global.Get("CustomEvent").New("openagy-turn-complete")
		global.Call("dispatchEvent", ev)
	}
}

func (h *JSWebLiveHandler) OnInterrupted() {
	h.bridge.mu.RLock()
	cb := h.bridge.onInterruptCb
	h.bridge.mu.RUnlock()

	if cb.Truthy() && cb.Type() == js.TypeFunction {
		cb.Invoke()
	}

	global := js.Global()
	if global.Truthy() && global.Get("CustomEvent").Truthy() && global.Get("dispatchEvent").Truthy() {
		ev := global.Get("CustomEvent").New("openagy-interrupted")
		global.Call("dispatchEvent", ev)
	}
}

func (h *JSWebLiveHandler) OnError(err error) {
	h.bridge.mu.RLock()
	cb := h.bridge.onErrorCb
	h.bridge.mu.RUnlock()

	msg := ""
	if err != nil {
		msg = err.Error()
	}

	if cb.Truthy() && cb.Type() == js.TypeFunction {
		cb.Invoke(msg)
	}

	global := js.Global()
	if global.Truthy() && global.Get("CustomEvent").Truthy() && global.Get("dispatchEvent").Truthy() {
		detail := global.Get("Object").New()
		detail.Set("error", msg)
		opts := global.Get("Object").New()
		opts.Set("detail", detail)
		ev := global.Get("CustomEvent").New("openagy-voice-error", opts)
		global.Call("dispatchEvent", ev)
	}
}

func (h *JSWebLiveHandler) OnClose() {
	h.bridge.mu.RLock()
	cb := h.bridge.onCloseCb
	h.bridge.mu.RUnlock()

	if cb.Truthy() && cb.Type() == js.TypeFunction {
		cb.Invoke()
	}

	global := js.Global()
	if global.Truthy() && global.Get("CustomEvent").Truthy() && global.Get("dispatchEvent").Truthy() {
		ev := global.Get("CustomEvent").New("openagy-voice-close")
		global.Call("dispatchEvent", ev)
	}
}

func (h *JSWebLiveHandler) OnThinkerHandoff(invocationID string, task string) {
	global := js.Global()
	if global.Truthy() && global.Get("CustomEvent").Truthy() && global.Get("dispatchEvent").Truthy() {
		detail := global.Get("Object").New()
		detail.Set("id", invocationID)
		detail.Set("task", task)
		opts := global.Get("Object").New()
		opts.Set("detail", detail)
		ev := global.Get("CustomEvent").New("openagy-thinker-handoff", opts)
		global.Call("dispatchEvent", ev)
	}
}

func (h *JSWebLiveHandler) OnToolCall(call LiveToolCall) {
	global := js.Global()
	if global.Truthy() && global.Get("CustomEvent").Truthy() && global.Get("dispatchEvent").Truthy() {
		detail := global.Get("Object").New()
		detail.Set("id", call.ID)
		detail.Set("name", call.Name)
		opts := global.Get("Object").New()
		opts.Set("detail", detail)
		ev := global.Get("CustomEvent").New("openagy-tool-call", opts)
		global.Call("dispatchEvent", ev)
	}
}

func (h *JSWebLiveHandler) OnReconnecting(attempt int, maxAttempts int, err error) {
	global := js.Global()
	if global.Truthy() && global.Get("CustomEvent").Truthy() && global.Get("dispatchEvent").Truthy() {
		detail := global.Get("Object").New()
		detail.Set("attempt", attempt)
		detail.Set("maxAttempts", maxAttempts)
		if err != nil {
			detail.Set("error", err.Error())
		}
		opts := global.Get("Object").New()
		opts.Set("detail", detail)
		ev := global.Get("CustomEvent").New("openagy-reconnecting", opts)
		global.Call("dispatchEvent", ev)
	}
}

func (h *JSWebLiveHandler) OnReconnected() {
	global := js.Global()
	if global.Truthy() && global.Get("CustomEvent").Truthy() && global.Get("dispatchEvent").Truthy() {
		ev := global.Get("CustomEvent").New("openagy-reconnected")
		global.Call("dispatchEvent", ev)
	}
}

// JSWebSpeaker implements LiveSpeaker for the browser by forwarding output audio to JS.
type JSWebSpeaker struct {
	mu     sync.Mutex
	oto    *OtoPlayer
	bridge *JSWebVoiceBridge
}

// NewJSWebSpeaker creates a new speaker that outputs both to Oto (if hardware active) and JS Web API.
func NewJSWebSpeaker(sampleRate int, channels int) (*JSWebSpeaker, error) {
	otoPlayer, _ := NewOtoPlayer(sampleRate, channels)
	return &JSWebSpeaker{
		oto:    otoPlayer,
		bridge: GlobalJSVoiceBridge(),
	}, nil
}

func (s *JSWebSpeaker) Write(pcm []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.bridge.EmitAudio(pcm)

	if s.oto != nil {
		return s.oto.Write(pcm)
	}
	return len(pcm), nil
}

func (s *JSWebSpeaker) Flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oto != nil {
		s.oto.Flush()
	}
}

func (s *JSWebSpeaker) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oto != nil {
		s.oto.Clear()
	}
}

func (s *JSWebSpeaker) Buffered() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oto != nil {
		return s.oto.Buffered()
	}
	return 0
}

func (s *JSWebSpeaker) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.oto != nil {
		return s.oto.Close()
	}
	return nil
}
