package voice

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

type mockLiveHandler struct {
	mu           sync.Mutex
	userSpeech   []string
	agentSpeech  []string
	agentDeltas  []string
	interrupted  int
	turnComplete int
	errs         []error
	handoffs     []string
	reconnecting []int
	reconnected  int
	toolCalls    []LiveToolCall
}

func (h *mockLiveHandler) OnUserSpeech(text string, interim bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.userSpeech = append(h.userSpeech, text)
}

func (h *mockLiveHandler) OnAgentSpeech(text string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agentSpeech = append(h.agentSpeech, text)
}

func (h *mockLiveHandler) OnAgentSpeechDelta(delta string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.agentDeltas = append(h.agentDeltas, delta)
}

func (h *mockLiveHandler) OnTurnComplete() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.turnComplete++
}

func (h *mockLiveHandler) OnInterrupted() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.interrupted++
}

func (h *mockLiveHandler) OnError(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.errs = append(h.errs, err)
}

func (h *mockLiveHandler) OnClose() {}

func (h *mockLiveHandler) OnThinkerHandoff(invocationID string, task string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.handoffs = append(h.handoffs, task)
}

func (h *mockLiveHandler) OnToolCall(call LiveToolCall) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.toolCalls = append(h.toolCalls, call)
}

func (h *mockLiveHandler) OnReconnecting(attempt int, maxAttempts int, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reconnecting = append(h.reconnecting, attempt)
}

func (h *mockLiveHandler) OnReconnected() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.reconnected++
}

func TestNativeLiveSession_Lifecycle(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}

	sess := NewNativeLiveSession(mockBackend, handler)
	if sess.IsActive() {
		t.Fatal("session should not be active initially")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Finn"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !sess.IsActive() {
		t.Fatal("session should be active after Start")
	}

	// Status row should include Live
	status := sess.StatusRow()
	if !strings.Contains(status, "Live") || !strings.Contains(status, "f6 mute") {
		t.Fatalf("expected Live status row, got %q", status)
	}

	// Mute toggle
	if !sess.ToggleMute() {
		t.Fatal("expected muted to be true after toggle")
	}
	if !sess.IsMuted() {
		t.Fatal("expected IsMuted == true")
	}
	statusMuted := sess.StatusRow()
	if !strings.Contains(statusMuted, "f6 unmute") {
		t.Fatalf("expected unmute label when muted, got %q", statusMuted)
	}

	// Unmute
	if sess.ToggleMute() {
		t.Fatal("expected muted to be false after second toggle")
	}

	// Hang up
	if err := sess.HangUp(); err != nil {
		t.Fatalf("HangUp failed: %v", err)
	}
	if sess.IsActive() {
		t.Fatal("session should not be active after HangUp")
	}
}

func TestEchoGuard_HalfDuplexSuppression(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	sess := NewNativeLiveSession(mockBackend, nil)
	sess.echoGracePeriod = 50 * time.Millisecond

	// Synthesize 100ms of active speech (> 600 amplitude)
	speechBuf := make([]byte, 3200)
	for i := 0; i+1 < len(speechBuf); i += 2 {
		speechBuf[i] = 0x00
		speechBuf[i+1] = 0x10 // amplitude = 4096 (> 600)
	}

	// 1. Initially idle: speech should pass
	if sess.echoGuard(speechBuf) {
		t.Fatal("idle session with speech should not be echo-guarded")
	}

	// 2. Agent speaking: speech MUST be dropped
	sess.speaking.Store(true)
	if !sess.echoGuard(speechBuf) {
		t.Fatal("speaking agent MUST echo-guard (drop) mic input")
	}

	// 3. Agent stops speaking: within grace period, speech MUST still be dropped
	sess.speaking.Store(false)
	sess.echoUntil.Store(time.Now().Add(50 * time.Millisecond).UnixNano())
	if !sess.echoGuard(speechBuf) {
		t.Fatal("grace period MUST echo-guard (drop) mic input")
	}

	// 4. Wait for grace period to expire: speech passes again
	time.Sleep(60 * time.Millisecond)
	if sess.echoGuard(speechBuf) {
		t.Fatal("after grace period, speech should pass")
	}
}

func TestEchoGuard_FullDuplexBypass(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	sess := NewNativeLiveSession(mockBackend, nil)
	sess.SetDuplex(true)

	if !sess.IsDuplex() {
		t.Fatal("expected IsDuplex == true")
	}

	speechBuf := make([]byte, 3200)
	for i := 0; i+1 < len(speechBuf); i += 2 {
		speechBuf[i] = 0x00
		speechBuf[i+1] = 0x10 // amplitude = 4096 (> 600)
	}

	// In full-duplex mode, speech passes even when agent is speaking
	sess.speaking.Store(true)
	if sess.echoGuard(speechBuf) {
		t.Fatal("full duplex mode should bypass speaker echo guard")
	}
}

func TestEchoGuard_SilencePassesForServerVAD(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	sess := NewNativeLiveSession(mockBackend, nil)

	// Low-amplitude ambient silence (< 600)
	silenceBuf := make([]byte, 3200)
	for i := 0; i+1 < len(silenceBuf); i += 2 {
		silenceBuf[i] = 0x05
		silenceBuf[i+1] = 0x00 // amplitude = 5 (< 600)
	}

	// Silence MUST pass through to the server so server-side VAD can detect EOS promptly
	if sess.echoGuard(silenceBuf) {
		t.Fatal("ambient silence should pass to server for prompt VAD end-of-speech detection")
	}
}

func TestNativeLiveSession_ErrorStopsCleanlyWithoutLoop(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Aoede"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Trigger error
	testErr := errors.New("simulated network failure")
	sess.fail(testErr)

	// Verify session immediately becomes inactive
	if sess.IsActive() {
		t.Fatal("session should be inactive after fail")
	}

	// Repeatedly calling fail should not fire OnError again
	sess.fail(errors.New("subsequent error"))
	sess.fail(io.ErrClosedPipe)

	handler.mu.Lock()
	errCount := len(handler.errs)
	handler.mu.Unlock()

	if errCount != 1 {
		t.Fatalf("expected exactly 1 error dispatched, got %d", errCount)
	}
}

func TestNativeLiveSession_TranscriptAccumulationAndDispatch(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	mockBackend.SetCustomEvents([]*LiveServerEvent{
		{UserSpeech: "Tell me a joke", Interim: false},
		{VoiceTranscript: "Why did the ", TurnComplete: false},
		{VoiceTranscript: "chicken cross ", TurnComplete: false},
		{VoiceTranscript: "the road?", TurnComplete: true},
	})

	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Aoede"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	_ = sess.HangUp()

	handler.mu.Lock()
	defer handler.mu.Unlock()

	if len(handler.userSpeech) != 1 || handler.userSpeech[0] != "Tell me a joke" {
		t.Fatalf("expected user speech 'Tell me a joke', got %v", handler.userSpeech)
	}

	if len(handler.agentSpeech) != 1 || handler.agentSpeech[0] != "Why did the chicken cross the road?" {
		t.Fatalf("expected accumulated agent speech, got %v", handler.agentSpeech)
	}

	if len(handler.agentDeltas) != 3 {
		t.Fatalf("expected 3 streaming agent deltas, got %d: %v", len(handler.agentDeltas), handler.agentDeltas)
	}
	if handler.agentDeltas[0] != "Why did the " || handler.agentDeltas[1] != "chicken cross " || handler.agentDeltas[2] != "the road?" {
		t.Fatalf("unexpected streaming deltas: %v", handler.agentDeltas)
	}

	if handler.turnComplete != 1 {
		t.Fatalf("expected 1 turnComplete, got %d", handler.turnComplete)
	}
}

func TestNativeLiveSession_ThinkerTalkerBridge(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	mockBackend.SetCustomEvents([]*LiveServerEvent{
		{
			ToolCalls: []LiveToolCall{
				{
					ID:   "call_thinker_001",
					Name: "delegate_to_thinker",
					Args: map[string]any{
						"task": "Run tests and inspect failure logs",
					},
				},
			},
		},
	})

	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Finn"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// 1. Verify Handoff received
	handler.mu.Lock()
	handoffCount := len(handler.handoffs)
	taskText := ""
	if handoffCount > 0 {
		taskText = handler.handoffs[0]
	}
	handler.mu.Unlock()

	if handoffCount != 1 || taskText != "Run tests and inspect failure logs" {
		t.Fatalf("expected 1 handoff with task, got %d, task=%q", handoffCount, taskText)
	}

	// 2. Complete handoff tool call
	if err := sess.CompleteHandoff("call_thinker_001", map[string]any{"status": "running"}); err != nil {
		t.Fatalf("CompleteHandoff failed: %v", err)
	}

	// 3. Send Thinker Note
	if err := sess.SendThinkerNote("All 12 unit tests passed"); err != nil {
		t.Fatalf("SendThinkerNote failed: %v", err)
	}

	// 4. Send Thinker Answer
	if err := sess.SendThinkerAnswer("Task completed with 0 errors"); err != nil {
		t.Fatalf("SendThinkerAnswer failed: %v", err)
	}

	_ = sess.HangUp()

	lastSess := mockBackend.LastSession()
	if lastSess == nil {
		t.Fatal("expected non-nil last session")
	}

	lastSess.mu.Lock()
	defer lastSess.mu.Unlock()

	if len(lastSess.inNotes) != 1 || lastSess.inNotes[0] != "All 12 unit tests passed" {
		t.Fatalf("expected thinker note received, got %v", lastSess.inNotes)
	}

	if len(lastSess.inAnswers) != 1 || lastSess.inAnswers[0] != "Task completed with 0 errors" {
		t.Fatalf("expected thinker answer received, got %v", lastSess.inAnswers)
	}

	if lastSess.inToolResp["call_thinker_001"] == nil {
		t.Fatal("expected tool response recorded for call_thinker_001")
	}
}

func TestNativeLiveSession_WithRemoteMicServer(t *testing.T) {
	server, err := NewMicServer("127.0.0.1:0")
	if err != nil {
		t.Fatalf("NewMicServer failed: %v", err)
	}
	defer server.Close()

	go func() {
		_ = server.Serve()
	}()

	t.Setenv("ANTIGRAVITY_MIC", server.Addr().String())

	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Aoede"}); err != nil {
		t.Fatalf("Start with remote ANTIGRAVITY_MIC failed: %v", err)
	}

	if !sess.IsActive() {
		t.Fatal("session should be active with remote mic server")
	}

	if err := sess.HangUp(); err != nil {
		t.Fatalf("HangUp failed: %v", err)
	}
}

func TestNativeLiveSession_WireStatsTracking(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	sess := NewNativeLiveSession(mockBackend, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.HangUp()

	// Initially zero
	stats := sess.WireStats()
	if stats.AudioSentBytes != 0 || stats.AudioSentChunks != 0 {
		t.Fatalf("unexpected initial wire stats: %+v", stats)
	}

	// Trigger receive events via mock session
	mockSess := sess.session.(*MockLiveSession)
	sess.RecordSpeechActivity()

	mockSess.Emit(&LiveServerEvent{VoiceTranscript: "chunk 1"})
	mockSess.Emit(&LiveServerEvent{VoiceTranscript: "chunk 2"})
	mockSess.Emit(&LiveServerEvent{
		Audio:        make([]byte, 2400),
		TurnComplete: true,
	})

	time.Sleep(50 * time.Millisecond)

	stats = sess.WireStats()
	if stats.TextChunksRecv != 2 {
		t.Errorf("TextChunksRecv = %d, want 2", stats.TextChunksRecv)
	}
	if stats.AudioRecvBytes != 2400 {
		t.Errorf("AudioRecvBytes = %d, want 2400", stats.AudioRecvBytes)
	}
	if stats.TurnsCompleted != 1 {
		t.Errorf("TurnsCompleted = %d, want 1", stats.TurnsCompleted)
	}
}

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
	}
	for _, tt := range tests {
		if got := FormatBytes(tt.bytes); got != tt.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", tt.bytes, got, tt.want)
		}
	}
}

func TestEchoGuard_DeadlockSelfHealingWatchdog(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	sess := NewNativeLiveSession(mockBackend, nil)
	sess.duplex.Store(false) // half-duplex

	// Agent was speaking
	sess.speaking.Store(true)
	// Last audio arrived 1.0 second ago
	sess.lastAudioRecvTime.Store(time.Now().Add(-1000 * time.Millisecond).UnixNano())

	// echoGuard should detect that audio stopped arriving >800ms ago and self-heal
	suppressed := sess.echoGuard(make([]byte, 320))
	if suppressed {
		t.Fatalf("echoGuard should have self-healed and allowed audio through after timeout")
	}
	if sess.speaking.Load() {
		t.Fatalf("sess.speaking should have been cleared to false by deadlock watchdog")
	}
}

func TestComputeRMSLevel(t *testing.T) {
	silence := make([]byte, 320)
	if level := computeRMSLevel(silence); level != 0 {
		t.Errorf("computeRMSLevel(silence) = %d, want 0", level)
	}

	// High energy square wave
	loud := make([]byte, 320)
	for i := 0; i+1 < len(loud); i += 2 {
		loud[i] = 0x00
		loud[i+1] = 0x40 // ~16384 amplitude
	}
	if level := computeRMSLevel(loud); level < 50 {
		t.Errorf("computeRMSLevel(loud) = %d, want >= 50", level)
	}
}

func TestNativeLiveSession_AutoReconnectOnNetworkDrop(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)
	sess.SetReconnectConfig(ReconnectConfig{
		Enabled:        true,
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     30 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Puck"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.HangUp()

	if sess.ConnectionState() != StateConnected {
		t.Fatalf("expected StateConnected, got %v", sess.ConnectionState())
	}

	// 1. First session is live
	sess1 := mockBackend.LastSession()
	if sess1 == nil {
		t.Fatal("expected session 1 created")
	}

	// 2. Simulate network disruption (e.g. WiFi drops or server reset)
	sess1.InjectError(errors.New("connection reset by peer"))

	// 3. Wait for automatic reconnection
	deadline := time.Now().Add(500 * time.Millisecond)
	reconnected := false
	for time.Now().Before(deadline) {
		handler.mu.Lock()
		count := handler.reconnected
		handler.mu.Unlock()
		if count >= 1 {
			reconnected = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !reconnected {
		t.Fatalf("expected handler.reconnected >= 1, got 0")
	}

	handler.mu.Lock()
	reconnAttempts := len(handler.reconnecting)
	handler.mu.Unlock()
	if reconnAttempts == 0 {
		t.Fatal("expected at least 1 OnReconnecting notification")
	}

	// 4. Verify session state is restored to Connected and active
	if !sess.IsActive() {
		t.Fatal("session should remain active after successful auto-reconnect")
	}
	if sess.ConnectionState() != StateConnected {
		t.Fatalf("expected ConnectionState == StateConnected, got %v", sess.ConnectionState())
	}
	if mockBackend.CreatedCount() != 2 {
		t.Fatalf("expected 2 backend sessions created, got %d", mockBackend.CreatedCount())
	}
}

func TestNativeLiveSession_ContextRehydrationOnReconnect(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)
	sess.SetReconnectConfig(ReconnectConfig{
		Enabled:        true,
		MaxAttempts:    3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     30 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Aoede"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.HangUp()

	sess1 := mockBackend.LastSession()

	// Feed turn 1: user speech
	sess1.eventsCh <- &LiveServerEvent{UserSpeech: "How do I reverse a linked list?", Interim: false}
	// Feed turn 2: agent response
	sess1.eventsCh <- &LiveServerEvent{VoiceTranscript: "Swap next pointers iteratively.", TurnComplete: true}

	time.Sleep(50 * time.Millisecond)

	history := sess.ConversationHistory()
	if len(history) != 2 {
		t.Fatalf("expected 2 history turns recorded, got %d: %+v", len(history), history)
	}

	// Simulate unexpected drop
	sess1.InjectError(errors.New("broken pipe"))

	// Wait for reconnect
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		handler.mu.Lock()
		count := handler.reconnected
		handler.mu.Unlock()
		if count >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	sess2 := mockBackend.LastSession()
	if sess2 == sess1 {
		t.Fatal("expected new session to be created after reconnect")
	}

	injected := sess2.InjectedTexts()
	if len(injected) == 0 {
		t.Fatal("expected context rehydration prompt injected into session 2")
	}

	prompt := injected[0]
	if !strings.Contains(prompt, "How do I reverse a linked list?") {
		t.Fatalf("expected rehydration prompt to contain user speech, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Swap next pointers iteratively.") {
		t.Fatalf("expected rehydration prompt to contain agent speech, got: %s", prompt)
	}
}

func TestNativeLiveSession_ReconnectExhaustionFailsGracefully(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)
	sess.SetReconnectConfig(ReconnectConfig{
		Enabled:        true,
		MaxAttempts:    2,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Charon"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.HangUp()

	// Configure mock backend so all subsequent CreateSession calls fail
	mockBackend.SetFailAttempts(10, errors.New("network is unreachable"))

	// Inject error on active session
	mockBackend.LastSession().InjectError(errors.New("network reset"))

	// Wait for retry attempts to be exhausted
	deadline := time.Now().Add(1 * time.Second)
	failed := false
	for time.Now().Before(deadline) {
		handler.mu.Lock()
		errCount := len(handler.errs)
		handler.mu.Unlock()
		if errCount > 0 && !sess.IsActive() {
			failed = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !failed {
		t.Fatal("session should become inactive and dispatch error after all reconnect attempts fail")
	}

	handler.mu.Lock()
	errCount := len(handler.errs)
	handler.mu.Unlock()

	if errCount != 1 {
		t.Fatalf("expected 1 error dispatched upon exhaustion, got %d", errCount)
	}
}

func TestNativeLiveSession_UserHangupDuringReconnect(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)
	sess.SetReconnectConfig(ReconnectConfig{
		Enabled:        true,
		MaxAttempts:    5,
		InitialBackoff: 300 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Fenrir"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Trigger error
	mockBackend.LastSession().InjectError(errors.New("network reset"))

	// Wait briefly so it enters Reconnecting state
	time.Sleep(50 * time.Millisecond)
	if !sess.IsReconnecting() {
		t.Fatalf("expected session to be in reconnecting state")
	}

	// User decides to hang up
	if err := sess.HangUp(); err != nil {
		t.Fatalf("HangUp failed: %v", err)
	}

	if sess.IsActive() {
		t.Fatal("session should be inactive after user HangUp")
	}
	if sess.ConnectionState() != StateClosed {
		t.Fatalf("expected StateClosed, got %v", sess.ConnectionState())
	}
}

func TestNativeLiveSession_ContinuousReconnectAndManualTrigger(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)
	sess.SetReconnectConfig(ReconnectConfig{
		Enabled:        true,
		MaxAttempts:    2,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     20 * time.Millisecond,
		Continuous:     true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Charon"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.HangUp()

	// 1. Next 2 reconnect attempts fail
	mockBackend.SetFailAttempts(2, errors.New("temporary network drop"))

	// Trigger reconnect by injecting error on active session
	mockBackend.LastSession().InjectError(errors.New("socket broken"))

	// Wait for 2 initial attempts to be exhausted
	deadline := time.Now().Add(1 * time.Second)
	exhausted := false
	for time.Now().Before(deadline) {
		handler.mu.Lock()
		errCount := len(handler.errs)
		handler.mu.Unlock()
		if errCount > 0 && sess.ConnectionState() == StateDisconnected {
			exhausted = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !exhausted {
		t.Fatal("session should transition to StateDisconnected after initial attempts fail with Continuous=true")
	}

	// Verify session is still active (not dead/bricked)
	if !sess.IsActive() {
		t.Fatal("session should remain active in Continuous mode after initial retries fail")
	}

	// 2. Call ReconnectNow() to immediately wake background retry worker
	if err := sess.ReconnectNow(); err != nil {
		t.Fatalf("ReconnectNow failed: %v", err)
	}

	// Since failAttempts was 2 and both were consumed, next CreateSession will succeed!
	deadline = time.Now().Add(1 * time.Second)
	restored := false
	for time.Now().Before(deadline) {
		handler.mu.Lock()
		reconnCount := handler.reconnected
		handler.mu.Unlock()
		if reconnCount > 0 && sess.ConnectionState() == StateConnected {
			restored = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !restored {
		t.Fatal("session failed to reconnect immediately upon ReconnectNow()")
	}
}

func TestNativeLiveSession_SwitchModel(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := LiveSessionConfig{
		Model: "gemini-3.8-live",
		Voice: "Fola",
	}
	if err := sess.Start(ctx, cfg); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.HangUp()

	if err := sess.SwitchModel(ctx, "gemini-3.8-live-extended-thinking"); err != nil {
		t.Fatalf("SwitchModel failed: %v", err)
	}

	last := mockBackend.LastSession()
	if last == nil {
		t.Fatal("expected LastSession() != nil")
	}
	if last.cfg.Model != "gemini-3.8-live-extended-thinking" {
		t.Fatalf("expected switched model to be gemini-3.8-live-extended-thinking, got %s", last.cfg.Model)
	}
}

func TestNativeLiveSession_FatalErrorDoesNotLoop(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	mockBackend.SetFailAttempts(1, errors.New("websocket: close 1008 (policy violation): Request had insufficient authentication scopes"))
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := sess.Start(ctx, LiveSessionConfig{Voice: "Fola"})
	if err == nil {
		t.Fatal("expected Start to fail with fatal error, got nil")
	}

	if !isFatalLiveError(err) {
		t.Fatalf("expected isFatalLiveError(err) to be true for %v", err)
	}
}

func TestNativeLiveSession_ThinkerCues(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Aoede"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.HangUp()

	// 1. Send WaitingOnUser
	if err := sess.SendWaitingOnUser("Do you approve deleting file.txt?"); err != nil {
		t.Fatalf("SendWaitingOnUser failed: %v", err)
	}

	// 2. Send TaskStopped
	if err := sess.SendTaskStopped(); err != nil {
		t.Fatalf("SendTaskStopped failed: %v", err)
	}

	// 3. Send ThinkerNote
	if err := sess.SendThinkerNote("Compiling project"); err != nil {
		t.Fatalf("SendThinkerNote failed: %v", err)
	}
}

func TestNativeLiveSession_VisualAndTerminalFrame(t *testing.T) {
	mockBackend := NewMockLiveBackend()
	handler := &mockLiveHandler{}
	sess := NewNativeLiveSession(mockBackend, handler)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sess.Start(ctx, LiveSessionConfig{Voice: "Aoede"}); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer sess.HangUp()

	// 1. Send TerminalFrame
	if err := sess.SendTerminalFrame("panic: runtime error: invalid memory address"); err != nil {
		t.Fatalf("SendTerminalFrame failed: %v", err)
	}

	// 2. Send Realtime Media Frame
	mockJPEG := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10}
	if err := sess.SendMedia("image/jpeg", mockJPEG); err != nil {
		t.Fatalf("SendMedia failed: %v", err)
	}
}

type mockCancelAwareHandler struct {
	mockLiveHandler
	cancels []string
}

func (h *mockCancelAwareHandler) OnThinkerCancel(invocationID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cancels = append(h.cancels, invocationID)
}

func TestNativeLiveSession_NonDuplicateToolAndHandoffDispatch(t *testing.T) {
	t.Run("direct tools vs thinker handoffs vs cancel_thinker_agent with ThinkerCancelHandler", func(t *testing.T) {
		mockBackend := NewMockLiveBackend()
		mockBackend.SetCustomEvents([]*LiveServerEvent{
			{
				ToolCalls: []LiveToolCall{
					{ID: "tc-search", Name: "search_web", Args: map[string]any{"query": "golang"}},
					{ID: "tc-fetch", Name: "fetch_url", Args: map[string]any{"url": "https://example.com"}},
					{ID: "tc-cmd", Name: "run_command", Args: map[string]any{"command": "go test ./..."}},
					{ID: "tc-read", Name: "read_file", Args: map[string]any{"path": "main.go"}},
					{ID: "tc-think", Name: "call_thinker_agent", Args: map[string]any{"task": "refactor auth"}},
					{ID: "tc-delegate", Name: "delegate_to_thinker", Args: map[string]any{"query": "inspect tests"}},
					{ID: "tc-cancel", Name: "cancel_thinker_agent", Args: map[string]any{}},
				},
			},
		})

		handler := &mockCancelAwareHandler{}
		sess := NewNativeLiveSession(mockBackend, handler)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := sess.Start(ctx, LiveSessionConfig{Voice: "Aoede"}); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		deadline := time.Now().Add(1 * time.Second)
		for time.Now().Before(deadline) {
			handler.mu.Lock()
			done := len(handler.toolCalls) == 4 && len(handler.handoffs) == 2 && len(handler.cancels) == 1
			handler.mu.Unlock()
			if done {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		_ = sess.HangUp()

		handler.mu.Lock()
		defer handler.mu.Unlock()

		if len(handler.toolCalls) != 4 {
			t.Fatalf("expected 4 direct tool calls (no handoff tools), got %d: %+v", len(handler.toolCalls), handler.toolCalls)
		}
		for _, tc := range handler.toolCalls {
			if tc.Name == "call_thinker_agent" || tc.Name == "delegate_to_thinker" || tc.Name == "cancel_thinker_agent" {
				t.Fatalf("unexpected handoff/cancel tool in OnToolCall: %+v", tc)
			}
		}

		if len(handler.handoffs) != 2 {
			t.Fatalf("expected 2 thinker handoffs, got %d: %v", len(handler.handoffs), handler.handoffs)
		}
		if handler.handoffs[0] != "refactor auth" || handler.handoffs[1] != "inspect tests" {
			t.Fatalf("unexpected handoff tasks: %v", handler.handoffs)
		}

		if len(handler.cancels) != 1 || handler.cancels[0] != "tc-cancel" {
			t.Fatalf("expected OnThinkerCancel('tc-cancel'), got %v", handler.cancels)
		}
	})

	t.Run("cancel_thinker_agent fallback without ThinkerCancelHandler", func(t *testing.T) {
		mockBackend := NewMockLiveBackend()
		mockBackend.SetCustomEvents([]*LiveServerEvent{
			{
				ToolCalls: []LiveToolCall{
					{ID: "tc-cancel-fb", Name: "cancel_thinker_agent", Args: map[string]any{}},
				},
			},
		})

		handler := &mockLiveHandler{}
		sess := NewNativeLiveSession(mockBackend, handler)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := sess.Start(ctx, LiveSessionConfig{Voice: "Aoede"}); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		deadline := time.Now().Add(1 * time.Second)
		for time.Now().Before(deadline) {
			handler.mu.Lock()
			done := len(handler.toolCalls) == 1 && len(handler.handoffs) == 1
			handler.mu.Unlock()
			if done {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		_ = sess.HangUp()

		handler.mu.Lock()
		defer handler.mu.Unlock()
		if len(handler.toolCalls) != 1 || handler.toolCalls[0].Name != "cancel_thinker_agent" {
			t.Fatalf("expected fallback OnToolCall for cancel_thinker_agent, got %+v", handler.toolCalls)
		}
		if len(handler.handoffs) != 1 || handler.handoffs[0] != "cancel active task" {
			t.Fatalf("expected fallback OnThinkerHandoff('cancel active task'), got %v", handler.handoffs)
		}
	})

	t.Run("set_progress_updates toggles SendThinkerNote and hang_up closes session", func(t *testing.T) {
		mockBackend := NewMockLiveBackend()
		mockBackend.SetCustomEvents([]*LiveServerEvent{
			{
				ToolCalls: []LiveToolCall{
					{ID: "tc-prog-off", Name: "set_progress_updates", Args: map[string]any{"enabled": false}},
				},
			},
		})

		handler := &mockLiveHandler{}
		sess := NewNativeLiveSession(mockBackend, handler)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := sess.Start(ctx, LiveSessionConfig{Voice: "Aoede"}); err != nil {
			t.Fatalf("Start failed: %v", err)
		}

		deadline := time.Now().Add(1 * time.Second)
		for time.Now().Before(deadline) {
			if !sess.ProgressUpdatesEnabled() {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if sess.ProgressUpdatesEnabled() {
			t.Fatal("expected ProgressUpdatesEnabled() == false after set_progress_updates(false)")
		}

		// SendThinkerNote should be suppressed while disabled
		if err := sess.SendThinkerNote("suppressed note"); err != nil {
			t.Fatalf("SendThinkerNote failed: %v", err)
		}
		last := mockBackend.LastSession()
		if len(last.InNotes()) != 0 {
			t.Fatalf("expected 0 notes sent while progress updates disabled, got %v", last.InNotes())
		}

		// Re-enable and verify note passes
		sess.SetProgressUpdatesEnabled(true)
		if err := sess.SendThinkerNote("active note"); err != nil {
			t.Fatalf("SendThinkerNote failed: %v", err)
		}
		if len(last.InNotes()) != 1 || last.InNotes()[0] != "active note" {
			t.Fatalf("expected 'active note' after re-enabling, got %v", last.InNotes())
		}

		// Verify hang_up tool call terminates session
		mockBackend2 := NewMockLiveBackend()
		mockBackend2.SetCustomEvents([]*LiveServerEvent{
			{
				ToolCalls: []LiveToolCall{
					{ID: "tc-hangup", Name: "hang_up", Args: map[string]any{}},
				},
			},
		})
		handler2 := &mockLiveHandler{}
		sess2 := NewNativeLiveSession(mockBackend2, handler2)
		if err := sess2.Start(ctx, LiveSessionConfig{Voice: "Aoede"}); err != nil {
			t.Fatalf("Start failed: %v", err)
		}
		deadline = time.Now().Add(1 * time.Second)
		for time.Now().Before(deadline) {
			if !sess2.IsActive() {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if sess2.IsActive() {
			t.Fatal("expected hang_up tool call to terminate session")
		}
		handler2.mu.Lock()
		defer handler2.mu.Unlock()
		if len(handler2.toolCalls) != 1 || handler2.toolCalls[0].Name != "hang_up" {
			t.Fatalf("expected hang_up OnToolCall, got %+v", handler2.toolCalls)
		}
		if len(handler2.handoffs) != 0 {
			t.Fatalf("expected 0 handoffs for hang_up, got %v", handler2.handoffs)
		}
	})
}

func TestGeminiLiveFunctionDeclarations(t *testing.T) {
	decls := geminiLiveFunctionDeclarations()
	names := make(map[string]bool, len(decls))
	for _, d := range decls {
		names[d.Name] = true
	}
	for _, want := range []string{"call_thinker_agent", "cancel_thinker_agent", "set_progress_updates", "hang_up"} {
		if !names[want] {
			t.Fatalf("expected Gemini Live declaration %q, got %v", want, names)
		}
	}
}
