package voice

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/justwasm/pigasus/websocket"
)

// NativeLiveHandler receives live events (user/model transcripts, turn states).
type NativeLiveHandler interface {
	OnUserSpeech(text string, interim bool)
	OnAgentSpeech(text string)
	OnAgentSpeechDelta(delta string)
	OnTurnComplete()
	OnInterrupted()
	OnError(err error)
	OnClose()
	OnThinkerHandoff(invocationID string, task string)
	OnToolCall(call LiveToolCall)
	OnReconnecting(attempt int, maxAttempts int, err error)
	OnReconnected()
}

// LiveTurnRecord stores a recent user or model turn for context rehydration across reconnections.
type LiveTurnRecord struct {
	Role    string // "User", "Agent", or "User (Text)"
	Content string
}

// ReconnectConfig configures automatic reconnection upon network disruptions.
type ReconnectConfig struct {
	Enabled        bool
	MaxAttempts    int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	Continuous     bool // When true, does not permanently fail on MaxAttempts; continues slow background polling
}

// ConnectionState represents the live session connectivity state.
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateReconnecting
	StateClosed
)

func (s ConnectionState) String() string {
	switch s {
	case StateDisconnected:
		return "Disconnected"
	case StateConnecting:
		return "Connecting"
	case StateConnected:
		return "Connected"
	case StateReconnecting:
		return "Reconnecting"
	case StateClosed:
		return "Closed"
	default:
		return "Unknown"
	}
}

// LiveSpeaker abstracts a speaker audio output device.
type LiveSpeaker interface {
	Write(pcm []byte) (int, error)
	Flush()
	Clear()
	Buffered() int
	Close() error
}

// LiveMicrophone abstracts a microphone audio capture device.
type LiveMicrophone interface {
	Start() error
	Stop() error
	Close() error
	SetMuted(muted bool)
	ToggleMute() bool
	IsMuted() bool
}

// WireStats holds real-time telemetry of audio and text frames traversing the live session wire.
type WireStats struct {
	AudioSentBytes   int64
	AudioSentChunks  int64
	AudioRecvBytes   int64
	AudioRecvChunks  int64
	TextChunksRecv   int64
	TurnsCompleted   int64
	InterruptedCount int64
	LastTurnLatency  time.Duration
}

// NativeLiveSession coordinates a terminal live audio conversation.
type NativeLiveSession struct {
	mu      sync.RWMutex
	backend LiveBackend
	session LiveSession
	player  LiveSpeaker
	capture LiveMicrophone
	handler NativeLiveHandler

	active          atomic.Bool
	muted           atomic.Bool
	duplex          atomic.Bool
	speaking        atomic.Bool
	echoUntil       atomic.Int64 // UnixNano
	echoGracePeriod time.Duration
	startTime       time.Time

	// Watchdog and audio levels for deadlock prevention & UI visualizers
	lastAudioRecvTime atomic.Int64 // UnixNano
	inputLevel        atomic.Int32 // 0~100 RMS percentage for Gboard/Gemini Live visualizer

	// Wire statistics for diagnostics mode (--debug-wire)
	audioSentBytes   atomic.Int64
	audioSentChunks  atomic.Int64
	audioRecvBytes   atomic.Int64
	audioRecvChunks  atomic.Int64
	textChunksRecv   atomic.Int64
	turnsCompleted   atomic.Int64
	interruptedCount atomic.Int64
	lastSpeechTime   atomic.Int64 // UnixNano of last user speech/silence transition
	lastTurnLatency  atomic.Int64 // Nanoseconds

	cancelCtx context.CancelFunc
	callCtx   context.Context
	cfg       LiveSessionConfig
	doneCh    chan struct{}
	errOnce   sync.Once

	// Reconnection & Resilience
	reconnectCfg     ReconnectConfig
	reconnecting     atomic.Bool
	state            atomic.Int32 // ConnectionState
	reconnectTrigger chan struct{}

	historyMu    sync.RWMutex
	historyTurns []LiveTurnRecord
	maxHistory   int
	compactor    *VoiceHistoryCompactor
}

// NewNativeLiveSession creates a new terminal native live session manager.
func NewNativeLiveSession(backend LiveBackend, handler NativeLiveHandler) *NativeLiveSession {
	if backend == nil {
		backend = NewMockLiveBackend()
	}
	n := &NativeLiveSession{
		backend:          backend,
		handler:          handler,
		echoGracePeriod:  500 * time.Millisecond,
		doneCh:           make(chan struct{}),
		reconnectTrigger: make(chan struct{}, 1),
		reconnectCfg: ReconnectConfig{
			Enabled:        true,
			MaxAttempts:    5,
			InitialBackoff: 500 * time.Millisecond,
			MaxBackoff:     8 * time.Second,
		},
		maxHistory: 10,
		compactor:  NewVoiceHistoryCompactor(),
	}
	n.state.Store(int32(StateDisconnected))
	return n
}

// SetDuplex sets full duplex mode. When false (default), echoGuard suppresses the microphone
// while the agent speaks or while audio playback drains to prevent acoustic feedback loops.
func (n *NativeLiveSession) SetDuplex(duplex bool) {
	n.duplex.Store(duplex)
}

// IsDuplex reports whether full duplex mode is enabled.
func (n *NativeLiveSession) IsDuplex() bool {
	return n.duplex.Load()
}

// echoGuard implements upstream google3/third_party/jetski/cli/live/live.(*session).echoGuard.
//
// In half-duplex mode (default), it suppresses microphone capture while the agent is speaking
// or within the reverberation / playback drain grace period (500ms).
func (n *NativeLiveSession) echoGuard(pcm16k []byte) bool {
	// 1. In full duplex (headphones mode), echo guard is completely bypassed
	if n.duplex.Load() {
		return false
	}

	now := time.Now().UnixNano()

	// 2. Deadlock self-healing watchdog:
	// If speaking flag is set, but no audio packet has been received for > 800ms
	// and the player buffer is completely drained, the server turn has ended (or TurnComplete was lost).
	// Clear speaking state to prevent permanent microphone lockup.
	lastRecv := n.lastAudioRecvTime.Load()
	playerBuffered := 0
	if n.player != nil {
		playerBuffered = n.player.Buffered()
	}
	if n.speaking.Load() && playerBuffered == 0 {
		if lastRecv > 0 && now-lastRecv > int64(800*time.Millisecond) {
			n.speaking.Store(false)
		}
	}

	// 3. In half duplex (default):
	// If agent is actively generating audio or player has queued audio, extend echoUntil and suppress mic
	if n.speaking.Load() || playerBuffered > 0 {
		n.echoUntil.Store(now + int64(n.echoGracePeriod))
		return true
	}

	// 4. If within echo grace period after playback finished, suppress mic
	if now < n.echoUntil.Load() {
		return true
	}

	// Audio is continuously forwarded to the live backend.
	// Gemini server-side VAD relies on continuous trailing silence frames to detect
	// End-Of-Speech (EOS) in ~500ms. Suppressing silence here starves the server's VAD
	// and forces it to wait for an inactivity timeout (2-4 seconds).
	return false
}

// computeRMSLevel calculates the RMS amplitude of 16-bit PCM audio samples and maps to 0-100.
func computeRMSLevel(pcm16k []byte) int {
	if len(pcm16k) < 2 {
		return 0
	}
	var sumSquares float64
	numSamples := len(pcm16k) / 2
	for i := 0; i+1 < len(pcm16k); i += 2 {
		sample := float64(int16(binary.LittleEndian.Uint16(pcm16k[i:])))
		sumSquares += sample * sample
	}
	rms := math.Sqrt(sumSquares / float64(numSamples))
	level := int((rms / 2500.0) * 100.0)
	if level > 100 {
		level = 100
	}
	return level
}

// InputLevel reports the real-time RMS microphone energy level (0-100).
func (n *NativeLiveSession) InputLevel() int {
	if n == nil || !n.active.Load() || n.muted.Load() {
		return 0
	}
	return int(n.inputLevel.Load())
}

// hasSpeechEnergy calculates whether the 16-bit PCM buffer contains samples exceeding the threshold.
func hasSpeechEnergy(pcm16k []byte, threshold int16) bool {
	for i := 0; i+1 < len(pcm16k); i += 2 {
		val := int16(binary.LittleEndian.Uint16(pcm16k[i:]))
		if val > threshold || val < -threshold {
			return true
		}
	}
	return false
}

// Start initiates the live call: connects to LiveBackend, arms mic capture and speaker playback.
func (n *NativeLiveSession) Start(ctx context.Context, cfg LiveSessionConfig) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.active.Load() {
		return errors.New("native live: session already active")
	}

	callCtx, cancel := context.WithCancel(ctx)
	n.cancelCtx = cancel
	n.callCtx = callCtx
	n.cfg = cfg
	n.state.Store(int32(StateConnecting))

	// 1. Establish full-duplex live session with AI engine
	sess, err := n.backend.CreateSession(callCtx, cfg)
	if err != nil {
		cancel()
		n.state.Store(int32(StateClosed))
		return fmt.Errorf("native live: create live session: %w", err)
	}
	n.session = sess

	// 2. Initialize speaker and microphone devices
	// Check if ANTIGRAVITY_MIC (or OPENAGY_MIC / PIGASUS_MIC) is configured
	micServerAddr := os.Getenv("ANTIGRAVITY_MIC")
	if micServerAddr == "" {
		micServerAddr = os.Getenv("OPENAGY_MIC")
	}
	if micServerAddr == "" {
		micServerAddr = os.Getenv("PIGASUS_MIC")
	}

	onAudioChunk := func(pcm16k []byte) {
		if !n.active.Load() || n.muted.Load() {
			n.inputLevel.Store(0)
			return
		}
		level := computeRMSLevel(pcm16k)
		n.inputLevel.Store(int32(level))

		if n.reconnecting.Load() {
			return
		}

		if n.echoGuard(pcm16k) {
			return
		}

		n.mu.RLock()
		sess := n.session
		n.mu.RUnlock()
		if sess == nil {
			return
		}

		if err := sess.SendAudio(pcm16k); err != nil {
			if !isBenignClose(err) && n.active.Load() {
				if n.reconnectCfg.Enabled {
					n.triggerReconnect(err)
				} else {
					n.fail(err)
				}
			}
		} else {
			n.audioSentBytes.Add(int64(len(pcm16k)))
			n.audioSentChunks.Add(1)
			if level > 5 {
				n.lastSpeechTime.Store(time.Now().UnixNano())
			}
		}
	}

	var player LiveSpeaker
	var capture LiveMicrophone

	if micServerAddr != "" {
		remoteStream, err := DialRemoteAudio(micServerAddr, onAudioChunk)
		if err != nil {
			sess.Close()
			cancel()
			return fmt.Errorf("native live: connect remote mic server (%s): %w", micServerAddr, err)
		}
		player = NewRemoteAudioPlayer(remoteStream)
		capture = NewRemoteAudioCapture(remoteStream)
	} else {
		localPlayer, err := NewOtoPlayer(24000, 1)
		if err != nil {
			sess.Close()
			cancel()
			return fmt.Errorf("native live: init speaker player: %w", err)
		}
		player = localPlayer

		localCapture, err := NewAudioCapture(16000, onAudioChunk)
		if err != nil {
			localPlayer.Close()
			sess.Close()
			cancel()
			return fmt.Errorf("native live: init mic capture: %w", err)
		}
		capture = localCapture
	}

	n.player = player
	n.capture = capture

	if err := capture.Start(); err != nil {
		capture.Close()
		player.Close()
		sess.Close()
		cancel()
		return fmt.Errorf("native live: start mic: %w", err)
	}

	n.startTime = time.Now()
	n.errOnce = sync.Once{}
	n.active.Store(true)
	n.state.Store(int32(StateConnected))
	n.doneCh = make(chan struct{})

	// 4. Start background receive pump
	go n.receivePump(callCtx, sess)

	return nil
}

func isBenignClose(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	status := websocket.CloseStatus(err)
	if status == websocket.StatusNormalClosure || status == websocket.StatusGoingAway {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "context canceled")
}

func (n *NativeLiveSession) fail(err error) {
	if isBenignClose(err) || !n.active.Load() {
		wasActive := n.active.Swap(false)
		_ = n.HangUp()
		if wasActive && n.handler != nil {
			n.handler.OnClose()
		}
		return
	}
	n.errOnce.Do(func() {
		_ = n.HangUp()
		if n.handler != nil {
			n.handler.OnError(err)
		}
	})
}

func (n *NativeLiveSession) receivePump(ctx context.Context, sess LiveSession) {
	var agentSpeechBuilder strings.Builder

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ev, err := sess.Receive()
		if err != nil {
			if !isBenignClose(err) && n.active.Load() {
				if n.reconnectCfg.Enabled {
					n.triggerReconnect(err)
				} else {
					n.fail(err)
				}
			}
			return
		}

		if ev.UserSpeech != "" {
			if !ev.Interim {
				n.recordTurn("User", ev.UserSpeech)
			}
			if n.handler != nil {
				n.handler.OnUserSpeech(ev.UserSpeech, ev.Interim)
			}
		}

		if len(ev.ToolCalls) > 0 {
			for _, tc := range ev.ToolCalls {
				if n.handler != nil {
					n.handler.OnToolCall(tc)
					if tc.Name == "delegate_to_thinker" || tc.Name == "call_thinker_agent" {
						task, _ := tc.Args["task"].(string)
						if task == "" {
							task, _ = tc.Args["query"].(string)
						}
						n.handler.OnThinkerHandoff(tc.ID, task)
					}
				}
			}
		}

		if ev.VoiceTranscript != "" {
			n.textChunksRecv.Add(1)
			agentSpeechBuilder.WriteString(ev.VoiceTranscript)
			if n.handler != nil {
				n.handler.OnAgentSpeechDelta(ev.VoiceTranscript)
			}
		}

		if ev.Interrupted {
			// Instant Barge-In: immediately flush speaker audio buffer
			n.interruptedCount.Add(1)
			n.speaking.Store(false)
			if n.player != nil {
				n.player.Clear()
			}
			if agentSpeechBuilder.Len() > 0 && n.handler != nil {
				n.handler.OnAgentSpeech(agentSpeechBuilder.String())
				agentSpeechBuilder.Reset()
			}
			if n.handler != nil {
				n.handler.OnInterrupted()
			}
		}

		if len(ev.Audio) > 0 && n.player != nil {
			n.lastAudioRecvTime.Store(time.Now().UnixNano())
			n.audioRecvBytes.Add(int64(len(ev.Audio)))
			n.audioRecvChunks.Add(1)
			n.speaking.Store(true)
			// ev.Audio is 24kHz 16-bit mono = 48,000 bytes/sec
			playDuration := time.Duration(len(ev.Audio)) * time.Second / 48000
			if playDuration < 200*time.Millisecond {
				playDuration = 200 * time.Millisecond
			}
			n.echoUntil.Store(time.Now().Add(playDuration + n.echoGracePeriod).UnixNano())
			_, _ = n.player.Write(ev.Audio)
		}

		if ev.TurnComplete {
			n.turnsCompleted.Add(1)
			if last := n.lastSpeechTime.Load(); last > 0 {
				lat := time.Now().UnixNano() - last
				if lat > 0 {
					n.lastTurnLatency.Store(lat)
				}
			}
			n.speaking.Store(false)
			if n.player != nil {
				n.player.Flush()
			}
			n.echoUntil.Store(time.Now().Add(n.echoGracePeriod).UnixNano())
			if agentSpeechBuilder.Len() > 0 {
				fullText := agentSpeechBuilder.String()
				n.recordTurn("Agent", fullText)
				if n.handler != nil {
					n.handler.OnAgentSpeech(fullText)
				}
				agentSpeechBuilder.Reset()
			}
			if n.handler != nil {
				n.handler.OnTurnComplete()
			}
		}
	}
}

// HangUp terminates the active call cleanly.
func (n *NativeLiveSession) HangUp() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if !n.active.Swap(false) {
		return nil
	}

	n.state.Store(int32(StateClosed))
	n.reconnecting.Store(false)
	n.speaking.Store(false)
	n.echoUntil.Store(0)

	if n.cancelCtx != nil {
		n.cancelCtx()
	}

	select {
	case <-n.doneCh:
	default:
		close(n.doneCh)
	}

	var errs []error
	if n.capture != nil {
		if err := n.capture.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if n.player != nil {
		n.player.Clear()
		if err := n.player.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if n.session != nil {
		if err := n.session.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// ToggleMute toggles microphone mute state.
func (n *NativeLiveSession) ToggleMute() bool {
	newVal := !n.muted.Load()
	n.muted.Store(newVal)
	if n.capture != nil {
		n.capture.SetMuted(newVal)
	}
	return newVal
}

// IsMuted reports whether the microphone is currently muted.
func (n *NativeLiveSession) IsMuted() bool {
	return n.muted.Load()
}

// IsActive reports whether a live call is ongoing.
func (n *NativeLiveSession) IsActive() bool {
	return n.active.Load()
}

// Elapsed returns the duration of the current live call.
func (n *NativeLiveSession) Elapsed() time.Duration {
	if !n.active.Load() || n.startTime.IsZero() {
		return 0
	}
	return time.Since(n.startTime)
}

// StatusRow renders the live session status bar (e.g. "● Live · 00:23 · m: mute · esc: hang up").
func (n *NativeLiveSession) StatusRow() string {
	if !n.active.Load() {
		return ""
	}
	if n.reconnecting.Load() {
		return "● Reconnecting (network disrupted)..."
	}
	elapsed := n.Elapsed()
	mins := int(elapsed.Minutes())
	secs := int(elapsed.Seconds()) % 60
	timeStr := fmt.Sprintf("%02d:%02d", mins, secs)

	state := "listening"
	muteHint := "f6 mute"
	if n.muted.Load() {
		state = "muted"
		muteHint = "f6 unmute"
	} else if n.speaking.Load() {
		state = "speaking"
	}

	return fmt.Sprintf("● Live %s · %s · %s · esc hang up", timeStr, state, muteHint)
}

// Speaking reports whether the live session is currently outputting speech or draining playback buffer.
func (n *NativeLiveSession) Speaking() bool {
	if n == nil {
		return false
	}
	if n.speaking.Load() {
		return true
	}
	if n.player != nil && n.player.Buffered() > 0 {
		return true
	}
	return time.Now().UnixNano() < n.echoUntil.Load()
}

// SendThinkerNote relays an asynchronous Thinker progress note into the live talker session.
func (n *NativeLiveSession) SendThinkerNote(note string) error {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if !n.active.Load() || n.session == nil {
		return errors.New("native live: session is not active")
	}
	return n.session.SendThinkerNote(note)
}

// WireStats answers real-time telemetry metrics across the live duplex wire.
func (n *NativeLiveSession) WireStats() WireStats {
	if n == nil {
		return WireStats{}
	}
	return WireStats{
		AudioSentBytes:   n.audioSentBytes.Load(),
		AudioSentChunks:  n.audioSentChunks.Load(),
		AudioRecvBytes:   n.audioRecvBytes.Load(),
		AudioRecvChunks:  n.audioRecvChunks.Load(),
		TextChunksRecv:   n.textChunksRecv.Load(),
		TurnsCompleted:   n.turnsCompleted.Load(),
		InterruptedCount: n.interruptedCount.Load(),
		LastTurnLatency:  time.Duration(n.lastTurnLatency.Load()),
	}
}

// RecordSpeechActivity marks user activity time for turn latency calculations.
func (n *NativeLiveSession) RecordSpeechActivity() {
	if n != nil {
		n.lastSpeechTime.Store(time.Now().UnixNano())
	}
}

// FormatBytes formats a byte count into a human-readable string (e.g. "12.4 KB").
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// SendThinkerAnswer feeds the completed Thinker result back to the talker for spoken summarization.
func (n *NativeLiveSession) SendThinkerAnswer(answer string) error {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if !n.active.Load() || n.session == nil {
		return errors.New("native live: session is not active")
	}
	return n.session.SendThinkerAnswer(answer)
}

// SendText sends a text message or proactive nudge to the live talker session.
func (n *NativeLiveSession) SendText(text string) error {
	if n.reconnecting.Load() {
		return errors.New("native live: connection is reconnecting, please wait...")
	}
	n.mu.RLock()
	defer n.mu.RUnlock()
	if !n.active.Load() || n.session == nil {
		return errors.New("native live: session is not active")
	}
	n.recordTurn("User (Text)", text)
	return n.session.SendText(text)
}

// CompleteHandoff acknowledges the tool call with an execution response.
func (n *NativeLiveSession) CompleteHandoff(id string, result any) error {
	return n.SendToolResponse(id, "delegate_to_thinker", result)
}

// Voice returns the current configured voice name.
func (n *NativeLiveSession) Voice() string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.cfg.Voice
}

// SwitchVoice updates the voice configuration and reconnects the live session with the new voice persona.
func (n *NativeLiveSession) SwitchVoice(ctx context.Context, voiceName string) error {
	n.mu.Lock()
	n.cfg.Voice = voiceName
	callCtx := n.callCtx
	if callCtx == nil {
		callCtx = ctx
	}
	oldSess := n.session
	cfg := n.cfg
	n.mu.Unlock()

	if oldSess != nil {
		newSess, err := n.backend.CreateSession(callCtx, cfg)
		if err != nil {
			return fmt.Errorf("native live: switch voice failed: %w", err)
		}

		n.mu.Lock()
		n.session = newSess
		n.mu.Unlock()

		_ = oldSess.Close()
		n.rehydrateContext(newSess)
		go n.receivePump(callCtx, newSess)
	}
	return nil
}

// SwitchModel switches the active Gemini Live model dynamically in real time and rehydrates context.
func (n *NativeLiveSession) SwitchModel(ctx context.Context, modelName string) error {
	n.mu.Lock()
	n.cfg.Model = modelName
	callCtx := n.callCtx
	if callCtx == nil {
		callCtx = ctx
	}
	oldSess := n.session
	cfg := n.cfg
	n.mu.Unlock()

	if oldSess != nil {
		newSess, err := n.backend.CreateSession(callCtx, cfg)
		if err != nil {
			return fmt.Errorf("native live: switch model failed: %w", err)
		}

		n.mu.Lock()
		n.session = newSess
		n.mu.Unlock()

		_ = oldSess.Close()
		n.rehydrateContext(newSess)
		go n.receivePump(callCtx, newSess)
	}
	return nil
}

// SendToolResponse sends the execution result of any tool call back to the live talker session.
func (n *NativeLiveSession) SendToolResponse(id string, name string, result any) error {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if !n.active.Load() || n.session == nil {
		return errors.New("native live: session is not active")
	}
	return n.session.SendToolResponse(id, name, result)
}

func (n *NativeLiveSession) triggerReconnect(err error) {
	if !n.active.Load() || isBenignClose(err) {
		return
	}
	if !n.reconnecting.CompareAndSwap(false, true) {
		return
	}
	n.state.Store(int32(StateReconnecting))

	// Close the broken session
	n.mu.Lock()
	if n.session != nil {
		_ = n.session.Close()
	}
	ctx := n.callCtx
	n.mu.Unlock()

	go n.reconnectWorker(ctx, err)
}

func (n *NativeLiveSession) reconnectWorker(ctx context.Context, triggerErr error) {
	cfg := n.reconnectCfg
	maxAttempts := cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 5
	}

	var lastErr = triggerErr

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if n.handler != nil {
			n.handler.OnReconnecting(attempt, maxAttempts, lastErr)
		}

		backoff := cfg.InitialBackoff * time.Duration(1<<(attempt-1))
		if cfg.MaxBackoff > 0 && backoff > cfg.MaxBackoff {
			backoff = cfg.MaxBackoff
		}
		if backoff <= 0 {
			backoff = 50 * time.Millisecond
		}

		select {
		case <-ctx.Done():
			n.reconnecting.Store(false)
			n.state.Store(int32(StateClosed))
			return
		case <-n.reconnectTrigger:
			// Immediate manual retry requested
		case <-time.After(backoff):
		}

		newSess, err := n.backend.CreateSession(ctx, n.cfg)
		if err != nil {
			lastErr = err
			continue
		}

		// Reconnected successfully!
		n.mu.Lock()
		n.session = newSess
		n.reconnecting.Store(false)
		n.state.Store(int32(StateConnected))
		n.mu.Unlock()

		// Rehydrate context turns
		n.rehydrateContext(newSess)

		// Notify handler
		if n.handler != nil {
			n.handler.OnReconnected()
		}

		// Spawn new receive pump for replacement session
		go n.receivePump(ctx, newSess)
		return
	}

	if !cfg.Continuous {
		// All attempts exhausted in non-continuous mode
		n.reconnecting.Store(false)
		n.state.Store(int32(StateClosed))
		finalErr := fmt.Errorf("native live: connection lost and failed to reconnect after %d attempts: %w", maxAttempts, lastErr)
		n.fail(finalErr)
		return
	}

	// In continuous mode: notify handler of failure, mark disconnected, and keep trying
	n.state.Store(int32(StateDisconnected))
	if n.handler != nil {
		n.handler.OnError(fmt.Errorf("native live: connection lost and failed to reconnect after %d attempts: %w", maxAttempts, lastErr))
	}

	attempt := maxAttempts
	for {
		attempt++
		select {
		case <-ctx.Done():
			n.reconnecting.Store(false)
			n.state.Store(int32(StateClosed))
			return
		case <-n.reconnectTrigger:
			// Immediate retry requested
		case <-time.After(8 * time.Second):
			// Steady background retry
		}

		n.state.Store(int32(StateReconnecting))
		if n.handler != nil {
			n.handler.OnReconnecting(attempt, 0, lastErr)
		}

		newSess, err := n.backend.CreateSession(ctx, n.cfg)
		if err != nil {
			lastErr = err
			n.state.Store(int32(StateDisconnected))
			continue
		}

		// Reconnected successfully!
		n.mu.Lock()
		n.session = newSess
		n.reconnecting.Store(false)
		n.state.Store(int32(StateConnected))
		n.mu.Unlock()

		// Rehydrate context turns
		n.rehydrateContext(newSess)

		// Notify handler
		if n.handler != nil {
			n.handler.OnReconnected()
		}

		// Spawn new receive pump for replacement session
		go n.receivePump(ctx, newSess)
		return
	}
}

// ReconnectNow triggers an immediate reconnection attempt without waiting for the backoff timer.
func (n *NativeLiveSession) ReconnectNow() error {
	if !n.active.Load() {
		return errors.New("native live: session is not active")
	}
	if n.reconnecting.Load() || ConnectionState(n.state.Load()) == StateDisconnected {
		select {
		case n.reconnectTrigger <- struct{}{}:
		default:
		}
		return nil
	}
	n.triggerReconnect(errors.New("manual reconnect requested"))
	return nil
}

func (n *NativeLiveSession) recordTurn(role, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	n.historyMu.Lock()
	defer n.historyMu.Unlock()

	n.historyTurns = append(n.historyTurns, LiveTurnRecord{
		Role:    role,
		Content: text,
	})
	if n.compactor != nil {
		n.compactor.AddTurn(role, text)
	}
	if len(n.historyTurns) > n.maxHistory {
		n.historyTurns = n.historyTurns[len(n.historyTurns)-n.maxHistory:]
	}
}

// ConversationHistory returns a copy of recorded recent speech turns.
func (n *NativeLiveSession) ConversationHistory() []LiveTurnRecord {
	n.historyMu.RLock()
	defer n.historyMu.RUnlock()
	copied := make([]LiveTurnRecord, len(n.historyTurns))
	copy(copied, n.historyTurns)
	return copied
}

// SetConversationHistory replaces or primes the stored recent turns (e.g. on session resume).
func (n *NativeLiveSession) SetConversationHistory(turns []LiveTurnRecord) {
	n.historyMu.Lock()
	defer n.historyMu.Unlock()
	n.historyTurns = make([]LiveTurnRecord, len(turns))
	copy(n.historyTurns, turns)
	if n.compactor != nil {
		n.compactor.SetTurns(turns)
	}
	if n.maxHistory > 0 && len(n.historyTurns) > n.maxHistory {
		n.historyTurns = n.historyTurns[len(n.historyTurns)-n.maxHistory:]
	}
}

// Compactor returns the speech transcript compactor managing session context.
func (n *NativeLiveSession) Compactor() *VoiceHistoryCompactor {
	if n == nil {
		return nil
	}
	return n.compactor
}

// RehydrateSessionContext sends stored conversation context into the active live session after reconnection.
func (n *NativeLiveSession) RehydrateSessionContext() error {
	n.mu.RLock()
	sess := n.session
	n.mu.RUnlock()
	if sess == nil {
		return errors.New("live session not currently connected")
	}
	n.rehydrateContext(sess)
	return nil
}

// RehydrateResumeContext sends stored conversation context into the active live session when resuming a saved session.
func (n *NativeLiveSession) RehydrateResumeContext() error {
	n.mu.RLock()
	sess := n.session
	n.mu.RUnlock()
	if sess == nil {
		return errors.New("live session not currently connected")
	}
	if n.compactor != nil && len(n.ConversationHistory()) > 0 {
		return sess.SendText(n.compactor.FormatResumeContext())
	}
	turns := n.ConversationHistory()
	if len(turns) == 0 {
		return nil
	}
	var sb strings.Builder
	sb.WriteString("[SYSTEM NOTE: User has resumed a previous LiveChat session. Below is the prior conversation transcript from that session. Maintain full context continuity and acknowledge that you are ready to continue where you left off:\n")
	for _, t := range turns {
		sb.WriteString(fmt.Sprintf("%s: %s\n", t.Role, t.Content))
	}
	sb.WriteString("]")
	return sess.SendText(sb.String())
}

func (n *NativeLiveSession) rehydrateContext(sess LiveSession) {
	if n.compactor != nil && len(n.ConversationHistory()) > 0 {
		_ = sess.SendText(n.compactor.FormatReconnectionContext())
		return
	}
	turns := n.ConversationHistory()
	if len(turns) == 0 {
		return
	}

	var sb strings.Builder
	sb.WriteString("[SYSTEM NOTE: Network connection was briefly disrupted by network jitter and has just successfully reconnected. Below is the recent conversation transcript prior to disconnection. Please maintain seamless conversational continuity without greeting from scratch:\n")
	for _, t := range turns {
		sb.WriteString(fmt.Sprintf("%s: %s\n", t.Role, t.Content))
	}
	sb.WriteString("]")
	_ = sess.SendText(sb.String())
}

// SetReconnectConfig updates the reconnection configuration.
func (n *NativeLiveSession) SetReconnectConfig(cfg ReconnectConfig) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.reconnectCfg = cfg
}

// IsReconnecting reports whether the session is currently attempting to reconnect.
func (n *NativeLiveSession) IsReconnecting() bool {
	return n.reconnecting.Load()
}

// ConnectionState reports the current connection state.
func (n *NativeLiveSession) ConnectionState() ConnectionState {
	return ConnectionState(n.state.Load())
}
