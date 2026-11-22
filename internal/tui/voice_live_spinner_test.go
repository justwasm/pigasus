package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/core/backend"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	voiceviews "github.com/justwasm/pigasus/internal/ui/views/voice"
	voicebackend "github.com/justwasm/pigasus/internal/voice"
)

func TestVoiceLive_SpinnerTicks(t *testing.T) {
	be := &slowFakeClient{delay: 50 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)

	// Start voice live
	m.audio.Surface.Start("/voice live")
	m.audio.Surface.HandleConnectDone(voiceviews.ConnectDoneMsg{})

	if m.audio.Surface.Phase() != voiceviews.PhaseLive {
		t.Fatalf("expected PhaseLive, got %v", m.audio.Surface.Phase())
	}

	// 1. Check if user typed prompt in /voice live
	for _, r := range "list files" {
		m.Update(runeKey(r))
	}
	_, _ = m.Update(enterKey())
	if !m.conversation.streaming {
		t.Fatalf("expected streaming=true after enterKey, got false")
	}

	initialFrame := m.conversation.spinFrame
	initialView := m.conversation.spinner.View()
	t.Logf("initial spinFrame: %d, view: %q", initialFrame, initialView)

	// Send initial tick
	tickMsg := m.conversation.spinner.Tick()
	t.Logf("initial tickMsg: %#v", tickMsg)

	// Now simulate 5 consecutive ticks
	curMsg := tickMsg
	for i := 0; i < 5; i++ {
		_, nextCmd := m.Update(curMsg)
		if nextCmd == nil {
			t.Fatalf("step %d: nextCmd is nil! Spinner tick chain died!", i)
		}
		curMsg = nextCmd()
		t.Logf("step %d: spinFrame=%d, view=%q, returned msg=%#v",
			i, m.conversation.spinFrame, m.conversation.spinner.View(), curMsg)
	}

	// 2. Now check Thinker Handoff during /voice live
	m.conversation.streaming = false

	// Trigger Thinker handoff
	cmd := m.handleVoiceThinkerHandoff("inv-1", "examine directory")
	if cmd == nil {
		t.Fatal("expected handleVoiceThinkerHandoff to return tickCmd, got nil")
	}

	if !m.audio.thinking {
		t.Fatalf("expected audio.thinking=true, got false")
	}
	if !m.conversation.streaming {
		t.Fatalf("expected conversation.streaming=true during thinker, got false")
	}

	live := m.liveRows(80)
	if len(live) == 0 {
		t.Fatalf("expected liveRows during thinker handoff to be non-empty, got empty")
	}
	cleanRow := termcolor.StripSGR(live[0])
	if !strings.Contains(cleanRow, "Think") {
		t.Fatalf("expected liveRows[0] to contain 'Think', got %q", cleanRow)
	}
	t.Logf("liveRows during thinker: %v", live)

	// Finish thinker handoff
	m.handleVoiceThinkerDone(voiceThinkerDoneMsg{invocationID: "inv-1", answer: "All clear"})
	if m.audio.thinking {
		t.Fatalf("expected audio.thinking=false after done, got true")
	}
	if m.conversation.streaming {
		t.Fatalf("expected conversation.streaming=false after thinker done, got true")
	}
}

func TestVoiceLive_TypedPromptRoutesToLiveSession(t *testing.T) {
	be := &slowFakeClient{delay: 50 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)

	// Start live surface
	m.audio.Surface.Start("/voice live")
	m.audio.Surface.HandleConnectDone(voiceviews.ConnectDoneMsg{})

	// Hook up mock live backend and session to RootModel
	mockBackend := voicebackend.NewMockLiveBackend()
	handler := &tuiLiveHandler{m: m}
	liveSess := voicebackend.NewNativeLiveSession(mockBackend, handler)
	m.audio.liveSession = liveSess

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := liveSess.Start(ctx, voicebackend.LiveSessionConfig{Voice: "Zephyr"}); err != nil {
		t.Fatalf("failed to start mock live session: %v", err)
	}
	defer liveSess.HangUp()

	if !m.isLiveVoiceActive() {
		t.Fatalf("expected isLiveVoiceActive=true, got false")
	}

	// Type prompt "hello voice agent"
	for _, r := range "hello voice agent" {
		m.Update(runeKey(r))
	}
	_, cmd := m.Update(enterKey())
	if !m.conversation.streaming {
		t.Fatalf("expected streaming=true after typed prompt in voice live")
	}

	// Execute batch commands returned from enterKey
	if cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					_ = c()
				}
			}
		}
	}

	// Verify text was injected into mock live backend session
	lastSess := mockBackend.LastSession()
	if lastSess == nil {
		t.Fatal("expected mock session to exist")
	}
	injected := lastSess.InjectedTexts()
	if len(injected) == 0 || injected[0] != "hello voice agent" {
		t.Fatalf("expected live session to receive 'hello voice agent', got %v", injected)
	}

	// Simulate streaming speech delta from live backend
	m.Update(voiceAgentDeltaMsg{delta: "Hello there! I hear you."})
	if !m.conversation.streaming {
		t.Fatalf("expected streaming=true while agent deltas arrive")
	}

	// Verify transcript has response
	trans := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(trans, "Hello there! I hear you.") {
		t.Fatalf("expected transcript to contain delta reply, got:\n%s", trans)
	}

	// Agent finishes speaking
	m.Update(voiceAgentDoneMsg{text: "Hello there! I hear you."})
	if m.conversation.streaming {
		t.Fatalf("expected streaming=false after agent speech done")
	}

	// Verify liveRows is now empty
	if rows := m.liveRows(80); len(rows) > 0 {
		t.Fatalf("expected liveRows=nil after turn done, got %v", rows)
	}
}

func TestVoiceLive_MicSpeechStartsSpinner(t *testing.T) {
	be := &slowFakeClient{delay: 50 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)

	m.audio.Surface.Start("/voice live")
	m.audio.Surface.HandleConnectDone(voiceviews.ConnectDoneMsg{})

	// Simulate user speaking via mic
	_, cmd := m.Update(voiceUserSpeechMsg{text: "How's the weather?"})
	if !m.conversation.streaming {
		t.Fatalf("expected streaming=true when user finishes speaking")
	}
	if cmd == nil {
		t.Fatalf("expected spinner tick command when user speech is committed")
	}

	// liveRows should display Thinking/Generating
	rows := m.liveRows(80)
	if len(rows) == 0 {
		t.Fatalf("expected liveRows to be non-empty while agent is processing speech")
	}
	t.Logf("liveRows during mic turn: %v", rows)

	// Agent finishes
	m.Update(voiceAgentDoneMsg{text: "It is sunny."})
	if m.conversation.streaming {
		t.Fatalf("expected streaming=false after agent done")
	}
	if rows := m.liveRows(80); len(rows) > 0 {
		t.Fatalf("expected liveRows to be empty after turn finishes, got %v", rows)
	}
}

func TestVoiceLive_ContextIntegratedWithTextChat(t *testing.T) {
	be := &slowFakeClient{delay: 50 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)

	// 1. Prior text turn in normal chat
	m.appendUserTurn("My secret code is 42.")
	m.session.appendMessage(backend.Message{Role: "assistant", Content: "Got it, secret code 42."})

	// 2. Start /voice live — verify sysPrompt gets seeded with prior conversation history
	t.Setenv("PIGASUS_MOCK_VOICE", "interim")
	if err := m.StartLiveVoice(); err != nil {
		t.Fatalf("StartLiveVoice failed: %v", err)
	}
	defer m.audio.liveSession.HangUp()

	m.audio.Surface.Start("/voice live")
	m.audio.Surface.HandleConnectDone(voiceviews.ConnectDoneMsg{})

	if !m.isLiveVoiceActive() {
		t.Fatalf("expected isLiveVoiceActive=true")
	}

	// 3. User speaks in voice live: "What is 1 + 1?"
	m.Update(voiceUserSpeechMsg{text: "What is 1 + 1?"})
	m.Update(voiceAgentDoneMsg{text: "1 + 1 is 2."})

	// 4. User types in voice live: "刚刚我问了什么"
	for _, r := range "刚刚我问了什么" {
		m.Update(runeKey(r))
	}
	_, cmd := m.Update(enterKey())
	if cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				if c != nil {
					_ = c()
				}
			}
		}
	}
	m.Update(voiceAgentDoneMsg{text: "你刚刚问的是 What is 1 + 1?"})

	// 5. Hang up voice session
	m.audio.Surface.Stop()
	if m.isLiveVoiceActive() {
		t.Fatalf("expected voice live to be stopped after hang up")
	}

	// 6. User continues in normal text mode: "+3"
	m.appendUserTurn("+3")

	// 7. Inspect prepareStreamTurnMessages() — must contain the full, seamless, unified transcript
	streamMsgs := m.prepareStreamTurnMessages()
	var turns []string
	for _, msg := range streamMsgs {
		turns = append(turns, msg.Role+": "+msg.Content)
	}
	joined := strings.Join(turns, "\n")
	t.Logf("Unified session messages:\n%s", joined)

	// Assert every single turn from both text mode and voice mode is present in chronological order
	expectedSnippets := []string{
		"My secret code is 42.",
		"Got it, secret code 42.",
		"What is 1 + 1?",
		"1 + 1 is 2.",
		"刚刚我问了什么",
		"你刚刚问的是 What is 1 + 1?",
		"+3",
	}
	lastIdx := -1
	for _, snippet := range expectedSnippets {
		idx := strings.Index(joined, snippet)
		if idx == -1 {
			t.Fatalf("expected snippet %q to be in unified session history, but was missing:\n%s", snippet, joined)
		}
		if idx < lastIdx {
			t.Fatalf("snippet %q appeared out of order in unified session history:\n%s", snippet, joined)
		}
		lastIdx = idx
	}
}

func execCmd(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			execCmd(c)
		}
	}
}

func TestVoiceLive_QueuedMessageDispatchedOnAgentDone(t *testing.T) {
	be := &slowFakeClient{delay: 50 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)

	// Start live surface
	m.audio.Surface.Start("/voice live")
	m.audio.Surface.HandleConnectDone(voiceviews.ConnectDoneMsg{})

	mockBackend := voicebackend.NewMockLiveBackend()
	handler := &tuiLiveHandler{m: m}
	liveSess := voicebackend.NewNativeLiveSession(mockBackend, handler)
	m.audio.liveSession = liveSess

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := liveSess.Start(ctx, voicebackend.LiveSessionConfig{Voice: "Zephyr"}); err != nil {
		t.Fatalf("failed to start mock live session: %v", err)
	}
	defer liveSess.HangUp()

	// Simulate streaming turn in progress
	m.conversation.streaming = true

	// User types a message while agent is speaking; it lands in StreamQueue
	m.session.store.StreamQueue = append(m.session.store.StreamQueue, "queued question 1")

	// Agent finishes speaking
	_, cmd := m.Update(voiceAgentDoneMsg{text: "I finished my spoken sentence."})

	// Verify StreamQueue was dequeued
	if len(m.session.store.StreamQueue) != 0 {
		t.Fatalf("expected StreamQueue to be empty after turn done, got %v", m.session.store.StreamQueue)
	}

	// Execute returned command batch to dispatch the queued prompt
	if cmd == nil {
		t.Fatal("expected non-nil cmd after agent done with queued prompt")
	}
	execCmd(cmd)

	// Verify text was injected into mock live backend session
	lastSess := mockBackend.LastSession()
	if lastSess == nil {
		t.Fatal("expected mock session to exist")
	}
	injected := lastSess.InjectedTexts()
	if len(injected) == 0 || injected[len(injected)-1] != "queued question 1" {
		t.Fatalf("expected live session to receive 'queued question 1', got %v", injected)
	}
}

func TestVoiceLive_QueuedMessageDispatchedOnThinkerDone(t *testing.T) {
	be := &slowFakeClient{delay: 50 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)

	// Start live surface
	m.audio.Surface.Start("/voice live")
	m.audio.Surface.HandleConnectDone(voiceviews.ConnectDoneMsg{})

	mockBackend := voicebackend.NewMockLiveBackend()
	handler := &tuiLiveHandler{m: m}
	liveSess := voicebackend.NewNativeLiveSession(mockBackend, handler)
	m.audio.liveSession = liveSess

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := liveSess.Start(ctx, voicebackend.LiveSessionConfig{Voice: "Zephyr"}); err != nil {
		t.Fatalf("failed to start mock live session: %v", err)
	}
	defer liveSess.HangUp()

	// 1. Trigger Thinker handoff
	m.handleVoiceThinkerHandoff("inv-1", "check system status")
	if !m.audio.thinking {
		t.Fatal("expected audio.thinking=true")
	}

	// 2. Queue a user prompt while thinker is running
	m.session.store.StreamQueue = append(m.session.store.StreamQueue, "queued after thinker")

	// 3. Agent finishes interim spoken note while thinker is STILL running
	_, cmd1 := m.Update(voiceAgentDoneMsg{text: "Thinking..."})
	if cmd1 != nil {
		execCmd(cmd1)
	}
	// Verify prompt is NOT yet dequeued because thinker is running
	if len(m.session.store.StreamQueue) != 1 {
		t.Fatalf("expected prompt to remain queued while thinker is active, got %v", m.session.store.StreamQueue)
	}

	// 4. Thinker finishes
	_, cmd2 := m.Update(voiceThinkerDoneMsg{invocationID: "inv-1", answer: "System normal"})
	if len(m.session.store.StreamQueue) != 0 {
		t.Fatalf("expected prompt to be dequeued after thinker done, got %v", m.session.store.StreamQueue)
	}
	if cmd2 == nil {
		t.Fatal("expected non-nil cmd after thinker done with queued prompt")
	}
	execCmd(cmd2)

	// Verify text was injected into mock live backend session
	lastSess := mockBackend.LastSession()
	if lastSess == nil {
		t.Fatal("expected mock session to exist")
	}
	injected := lastSess.InjectedTexts()
	if len(injected) == 0 || injected[len(injected)-1] != "queued after thinker" {
		t.Fatalf("expected live session to receive 'queued after thinker', got %v", injected)
	}
}

func TestVoiceLive_DeltaStartsSpinnerTick(t *testing.T) {
	be := &slowFakeClient{delay: 50 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)

	m.audio.Surface.Start("/voice live")
	m.audio.Surface.HandleConnectDone(voiceviews.ConnectDoneMsg{})

	// Ensure no voice reply is active initially
	m.conversation.voiceReplyStart = -1
	m.conversation.streaming = false

	// Spontaneous speech delta arrives from model
	_, cmd := m.Update(voiceAgentDeltaMsg{delta: "Hello there"})
	if !m.conversation.streaming {
		t.Fatal("expected streaming=true on delta")
	}
	if cmd == nil {
		t.Fatal("expected spinner tick command on first delta")
	}
	msg := cmd()
	if _, ok := msg.(sp.TickMsg); !ok {
		t.Fatalf("expected spinner.TickMsg, got %T (%#v)", msg, msg)
	}

	// Second delta should NOT return another spinner tick (tick chain is already running)
	_, cmd2 := m.Update(voiceAgentDeltaMsg{delta: ", how can I help?"})
	if cmd2 != nil {
		t.Fatalf("expected second delta to return nil cmd to avoid duplicate tick loops, got %v", cmd2)
	}
}

func TestVoiceLive_ProgramSpinnerContinuousAnimation(t *testing.T) {
	be := &slowFakeClient{delay: 50 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)

	// Start voice live
	m.audio.Surface.Start("/voice live")
	m.audio.Surface.HandleConnectDone(voiceviews.ConnectDoneMsg{})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	prog := tea.NewProgram(m, tea.WithoutRenderer(), tea.WithContext(ctx))
	m.turn.prog = prog

	go func() {
		_, _ = prog.Run()
	}()

	// Wait for program to start
	time.Sleep(50 * time.Millisecond)

	prog.Send(voiceviews.ConnectDoneMsg{})
	prog.Send(voiceUserSpeechMsg{text: "Please search the web"})

	// Give it 350ms to tick (spinner FPS is 100ms, should tick 3+ times)
	time.Sleep(350 * time.Millisecond)

	frameA := m.conversation.spinFrame
	viewA := m.conversation.spinner.View()
	t.Logf("After 350ms user speech: spinFrame=%d, view=%q", frameA, viewA)

	if frameA < 2 {
		t.Fatalf("expected spinFrame >= 2 after 350ms, got %d (spinner is STATIC!)", frameA)
	}

	// Now send delta
	prog.Send(voiceAgentDeltaMsg{delta: "Searching..."})
	time.Sleep(350 * time.Millisecond)

	frameB := m.conversation.spinFrame
	viewB := m.conversation.spinner.View()
	t.Logf("After 350ms delta: spinFrame=%d, view=%q", frameB, viewB)

	if frameB <= frameA {
		t.Fatalf("expected spinFrame to advance further from %d, got %d (spinner froze during deltas!)", frameA, frameB)
	}

	// Trigger Thinker handoff
	prog.Send(voiceThinkerHandoffMsg{invocationID: "inv-99", task: "reading codebase"})
	time.Sleep(350 * time.Millisecond)

	frameC := m.conversation.spinFrame
	viewC := m.conversation.spinner.View()
	t.Logf("After 350ms thinker handoff: spinFrame=%d, view=%q", frameC, viewC)

	if frameC <= frameB {
		t.Fatalf("expected spinFrame to advance further from %d during thinker handoff, got %d", frameB, frameC)
	}

	// Thinker finishes
	prog.Send(voiceThinkerDoneMsg{invocationID: "inv-99", answer: "found results"})
	time.Sleep(100 * time.Millisecond)

	frameD := m.conversation.spinFrame
	viewD := m.conversation.spinner.View()
	t.Logf("After thinker done: spinFrame=%d, view=%q", frameD, viewD)

	// Agent resumes streaming spoken response
	prog.Send(voiceAgentDeltaMsg{delta: "I found the results in the codebase"})
	time.Sleep(350 * time.Millisecond)

	frameE := m.conversation.spinFrame
	viewE := m.conversation.spinner.View()
	t.Logf("After 350ms agent resumption delta: spinFrame=%d, view=%q", frameE, viewE)

	if frameE <= frameD {
		t.Fatalf("expected spinFrame to advance further from %d during agent resumption delta, got %d", frameD, frameE)
	}

	// Turn finishes
	prog.Send(voiceAgentDoneMsg{text: "I found the results in the codebase"})
	time.Sleep(100 * time.Millisecond)

	if m.conversation.streaming {
		t.Fatalf("expected streaming=false after voiceAgentDoneMsg, got true")
	}
	if live := m.liveRows(80); len(live) > 0 {
		t.Fatalf("expected liveRows=nil after voiceAgentDoneMsg, got %v", live)
	}
}

