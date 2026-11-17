package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/composer"
	"github.com/justwasm/pigasus/internal/voice"
)

func TestLiveChatModelTypingAndComposer(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Puck", "gemini-2.5-flash", false)

	// Trigger WindowSizeMsg to mark ready
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !m.ready {
		t.Fatalf("expected model to be ready after WindowSizeMsg")
	}

	// 1. Typing 'h', 'e', 'l', 'l', 'o'
	for _, ch := range "hello" {
		_, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	if got := m.input.Value(); got != "hello" {
		t.Fatalf("input value = %q, want 'hello'", got)
	}

	// 2. Space and more text
	_, _ = m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	for _, ch := range "world" {
		_, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	if got := m.input.Value(); got != "hello world" {
		t.Fatalf("input value = %q, want 'hello world'", got)
	}

	// 3. Test Select All (Ctrl+A / Cmd+A)
	_, _ = m.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModSuper})
	if m.selection.Start != 0 || m.selection.End != len("hello world") {
		t.Fatalf("selection range = %+v, want (0, 11)", m.selection)
	}

	// 4. Test Replace Selection by typing 'X'
	_, _ = m.Update(tea.KeyPressMsg{Code: 'X', Text: "X"})
	if got := m.input.Value(); got != "X" {
		t.Fatalf("input value after replace = %q, want 'X'", got)
	}

	// 5. Test Undo (Ctrl+Z)
	_, _ = m.Update(tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl})
	if got := m.input.Value(); got != "hello world" {
		t.Fatalf("input value after undo = %q, want 'hello world'", got)
	}

	// 6. Test Enter clears input
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.input.Value(); got != "" {
		t.Fatalf("input value after Enter = %q, want empty", got)
	}
}

func TestLiveChatModel_StreamingAgentSpeech(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Puck", "gemini-2.5-flash")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// 1. First delta creates a new agent log entry
	_, _ = m.Update(agentSpeechDeltaMsg{delta: "Checking "})
	if !m.agentSpeaking {
		t.Fatal("expected agentSpeaking == true")
	}
	lastLog := m.logs[len(m.logs)-1]
	if !strings.Contains(lastLog, "🤖 [Agent]:") || !strings.Contains(lastLog, "Checking ") {
		t.Fatalf("expected log to contain '🤖 [Agent]:' and 'Checking ', got %q", lastLog)
	}

	// 2. Second delta updates the same log entry in-place
	_, _ = m.Update(agentSpeechDeltaMsg{delta: "git status now."})
	lastLog = m.logs[len(m.logs)-1]
	if !strings.Contains(lastLog, "🤖 [Agent]:") || !strings.Contains(lastLog, "Checking git status now.") {
		t.Fatalf("expected updated log, got %q", lastLog)
	}

	// 3. Turn complete finalizes
	_, _ = m.Update(agentTurnCompleteMsg{})
	if m.agentSpeaking {
		t.Fatal("expected agentSpeaking == false after turn complete")
	}
}

func TestLiveChatModel_RealtimeUserSpeechInComposer(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Puck", "gemini-2.5-flash")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// 1. Interim recognition appears directly in composer input
	_, _ = m.Update(userSpeechMsg{text: "check", interim: true})
	if got := m.input.Value(); got != "check" {
		t.Fatalf("composer input = %q, want 'check'", got)
	}

	// 2. Progressive interim updates composer in real time
	_, _ = m.Update(userSpeechMsg{text: "check the git diff", interim: true})
	if got := m.input.Value(); got != "check the git diff" {
		t.Fatalf("composer input = %q, want 'check the git diff'", got)
	}

	// 3. Finalized speech commits to conversation logs and clears composer
	_, _ = m.Update(userSpeechMsg{text: "check the git diff please", interim: false})
	if got := m.input.Value(); got != "" {
		t.Fatalf("composer input after finalization = %q, want empty", got)
	}
	lastLog := m.logs[len(m.logs)-1]
	if !strings.Contains(lastLog, "🎤 [User]:") || !strings.Contains(lastLog, "check the git diff please") {
		t.Fatalf("expected user log entry, got %q", lastLog)
	}
}

func TestLiveChatModel_BargeInInterruption(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Puck", "gemini-2.5-flash")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	_, _ = m.Update(agentSpeechDeltaMsg{delta: "I was in the middle of talking"})
	_, _ = m.Update(interruptedMsg{})

	if m.agentSpeaking {
		t.Fatal("expected agentSpeaking == false after interruption")
	}

	foundInterrupted := false
	for _, l := range m.logs {
		if strings.Contains(l, "[interrupted]") {
			foundInterrupted = true
			break
		}
	}
	if !foundInterrupted {
		t.Fatal("expected an [interrupted] tag in agent speech line")
	}
}

func TestLiveChatModel_PasteMsgAndSuperV(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Puck", "gemini-2.5-flash")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// 1. Bracketed paste message (tea.PasteMsg)
	_, _ = m.Update(tea.PasteMsg{Content: "pasted content from terminal\nsecond line"})
	if got := m.input.Value(); got != "pasted content from terminal\nsecond line" {
		t.Fatalf("composer input after PasteMsg = %q, want multi-line pasted text", got)
	}

	// 2. Super+V (Cmd+V) keypress event
	m.input.Reset()
	// Mock clipboard reader
	prevReader := clipboard.Reader
	clipboard.Reader = func() string {
		return "clipboard super-v content"
	}
	defer func() { clipboard.Reader = prevReader }()

	keyMsg := tea.KeyPressMsg(tea.Key{Code: 'v', Mod: tea.ModSuper})
	_, _ = m.Update(keyMsg)

	if got := m.input.Value(); got != "clipboard super-v content" {
		t.Fatalf("composer input after Super+V = %q, want 'clipboard super-v content'", got)
	}
}

func TestLiveChatModel_SelectionHighlightInView(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Puck", "gemini-2.5-flash")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.input.SetValue("selected text")
	// Select "text" (offset 9 to 13)
	m.selection = composer.Range{Start: 9, End: 13}

	v := m.View()
	rendered := v.Content
	// Reverse video ANSI escape is \x1b[7m or contains highlight
	if !strings.Contains(rendered, "\x1b[7m") {
		t.Errorf("expected rendered view to contain reverse-video selection highlight (\\x1b[7m), got:\n%s", rendered)
	}
}

func TestLiveChatModel_WordNavigationAndMultiLine(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Puck", "gemini-2.5-flash")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.input.SetValue("first word second\nthird line")
	m.input.MoveToBegin() // cursor at offset 0

	// Alt+Right moves one word right
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt})
	col := m.input.Column()
	if col == 0 {
		t.Errorf("after Alt+Right cursor should have moved forward, col = %d", col)
	}

	// Alt+Left moves back
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	col = m.input.Column()
	if col != 0 {
		t.Errorf("after Alt+Left cursor should return to begin, col = %d", col)
	}

	// KeyDown moves cursor to line 1
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if line := m.input.Line(); line != 1 {
		t.Errorf("after KeyDown in multi-line input, line = %d, want 1", line)
	}

	// KeyUp moves cursor back to line 0
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if line := m.input.Line(); line != 0 {
		t.Errorf("after KeyUp in multi-line input, line = %d, want 0", line)
	}
}

func TestLiveChatModel_DebugWireStatusBar(t *testing.T) {
	mockBackend := voice.NewMockLiveBackend()
	sess := voice.NewNativeLiveSession(mockBackend, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sess.Start(ctx, voice.LiveSessionConfig{}); err != nil {
		t.Fatalf("failed to start live session: %v", err)
	}
	defer sess.HangUp()

	m := newLiveChatModel(sess, nil, "/tmp/testws", "Puck", "gemini-2.5-flash", true)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	status := m.renderStatusBar()
	if !strings.Contains(status, "[WIRE]") {
		t.Errorf("expected status bar with --debug-wire to contain [WIRE], got:\n%s", status)
	}
	if !strings.Contains(status, "TX:") || !strings.Contains(status, "RX Audio:") {
		t.Errorf("expected wire telemetry in status bar, got:\n%s", status)
	}
}

func TestLiveChatModel_GboardModeIndicators(t *testing.T) {
	mockBackend := voice.NewMockLiveBackend()
	sess := voice.NewNativeLiveSession(mockBackend, nil)
	m := newLiveChatModel(sess, nil, "/tmp/testws", "Puck", "gemini-2.5-flash", false)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Idle mode -> LISTENING
	v := m.View()
	if !strings.Contains(v.Content, "LISTENING") {
		t.Errorf("expected LISTENING indicator in view, got:\n%s", v.Content)
	}

	// 2. Interim user speech -> HEARING
	_, _ = m.Update(userSpeechMsg{text: "running tests", interim: true})
	v = m.View()
	if !strings.Contains(v.Content, "HEARING") {
		t.Errorf("expected HEARING indicator in view during speech, got:\n%s", v.Content)
	}

	// 3. User finishes -> THINKING
	_, _ = m.Update(userSpeechMsg{text: "running tests", interim: false})
	v = m.View()
	if !strings.Contains(v.Content, "THINKING") {
		t.Errorf("expected THINKING indicator in view after user finalization, got:\n%s", v.Content)
	}

	// 4. Model replies -> SPEAKING
	_, _ = m.Update(agentSpeechDeltaMsg{delta: "Understood."})
	v = m.View()
	if !strings.Contains(v.Content, "SPEAKING") {
		t.Errorf("expected SPEAKING indicator in view during playback, got:\n%s", v.Content)
	}
}

func TestLiveChatModel_CtrlJAndBackslashMultiline(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Puck", "gemini-2.5-flash")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.input.SetValue("line 1")
	m.input.MoveToEnd()

	// Press Ctrl+J -> should insert newline
	_, _ = m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	if m.input.Value() != "line 1\n" {
		t.Errorf("expected 'line 1\\n' after Ctrl+J, got %q", m.input.Value())
	}
	if line := m.input.Line(); line != 1 {
		t.Errorf("expected cursor at line 1 after Ctrl+J, got %d", line)
	}

	// Press Backslash then unmodified Enter -> should replace backslash with newline
	m.input.SetValue("first line\\")
	m.input.MoveToEnd()
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.input.Value() != "first line\n" {
		t.Errorf("expected 'first line\\n' after Backslash+Enter, got %q", m.input.Value())
	}

	// Verify multiline content renders in View()
	v := m.View()
	if !strings.Contains(v.Content, "first line") {
		t.Errorf("expected View() to contain multiline prompt content, got:\n%s", v.Content)
	}
}

func TestLiveChatModel_ThinkerFailedIsolated(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Puck", "gemini-2.5-flash", false)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Simulate Thinker failed message
	_, _ = m.Update(thinkerDoneMsg{
		task:   "run broken test",
		result: "FAIL: TestBoom (0.01s)",
		err:    context.DeadlineExceeded,
	})

	v := m.View()
	if !strings.Contains(v.Content, "❌ [Thinker Failed]") {
		t.Errorf("expected '❌ [Thinker Failed]' in view logs, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "FAIL: TestBoom") {
		t.Errorf("expected failed output details in view logs, got:\n%s", v.Content)
	}
}

func TestLiveChatModel_ReconnectionStateAndStatusBar(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Aoede", "gemini-3.8-live", false)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Initial view shows LISTENING
	v := m.View()
	if !strings.Contains(v.Content, "LISTENING") {
		t.Errorf("expected LISTENING indicator in initial view, got:\n%s", v.Content)
	}

	// 2. Reconnecting message triggers RECONNECTING indicator
	_, _ = m.Update(reconnectingMsg{attempt: 1, maxAttempts: 5})
	if !m.reconnecting {
		t.Fatal("expected m.reconnecting == true")
	}

	v = m.View()
	if !strings.Contains(v.Content, "RECONNECTING (1/5)") {
		t.Errorf("expected 'RECONNECTING (1/5)' in status bar, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "🔄 [Network Disrupted]: Reconnecting") {
		t.Errorf("expected reconnection log in view, got:\n%s", v.Content)
	}

	// 3. Reconnected message restores normal operation
	_, _ = m.Update(reconnectedMsg{})
	if m.reconnecting {
		t.Fatal("expected m.reconnecting == false after reconnected")
	}

	v = m.View()
	if !strings.Contains(v.Content, "LISTENING") {
		t.Errorf("expected LISTENING restored in status bar, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "✨ [Reconnected]") {
		t.Errorf("expected reconnected log in view, got:\n%s", v.Content)
	}
}

func TestLiveChatModel_InputQueuedDuringReconnecting(t *testing.T) {
	mockBackend := voice.NewMockLiveBackend()
	sess := voice.NewNativeLiveSession(mockBackend, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = sess.Start(ctx, voice.LiveSessionConfig{Voice: "Aoede"})
	defer sess.HangUp()

	m := newLiveChatModel(sess, nil, "/tmp/testws", "Aoede", "gemini-3.8-live", false)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Enter reconnecting state
	_, _ = m.Update(reconnectingMsg{attempt: 2, maxAttempts: 5})

	// Type text
	m.input.SetValue("hello while disconnected")

	// Press Enter
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Message should be queued and trigger immediate reconnection
	if m.pendingText != "hello while disconnected" {
		t.Fatalf("expected pendingText to be queued, got %q", m.pendingText)
	}
	v := m.View()
	if !strings.Contains(v.Content, "[You (Queued)]") || !strings.Contains(v.Content, "hello while disconnected") {
		t.Errorf("expected queued message in view, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "Triggering immediate") {
		t.Errorf("expected immediate reconnection notice in view, got:\n%s", v.Content)
	}
}

func TestExtractShellCommand(t *testing.T) {
	tests := []struct {
		task string
		want string
	}{
		{
			task: "run the cal command in the terminal and report the output.",
			want: "cal",
		},
		{
			task: "what is the result of running cal",
			want: "cal",
		},
		{
			task: "run `cal` in terminal",
			want: "cal",
		},
		{
			task: "check git status",
			want: "git status -s",
		},
		{
			task: "git diff",
			want: "git diff --stat",
		},
		{
			task: "https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md",
			// Must NOT be mistaken for git status just because it contains "github"
			want: "https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md",
		},
	}

	for _, tt := range tests {
		got := extractShellCommand("/tmp", tt.task)
		if got != tt.want {
			t.Errorf("extractShellCommand(%q) = %q, want %q", tt.task, got, tt.want)
		}
	}
}

func TestExtractURLs(t *testing.T) {
	text := "Check this link: https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md#1510 and 'https://raw.githubusercontent.com/test'"
	urls := extractURLs(text)
	if len(urls) != 2 {
		t.Fatalf("expected 2 URLs, got %d: %v", len(urls), urls)
	}
	if urls[0] != "https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md#1510" {
		t.Errorf("unexpected URL 0: %s", urls[0])
	}
	if urls[1] != "https://raw.githubusercontent.com/test" {
		t.Errorf("unexpected URL 1: %s", urls[1])
	}
}

func TestExecuteThinkerTask_CalCommand(t *testing.T) {
	out, err := executeThinkerTask("/tmp", "run the cal command in the terminal and report the output.")
	if err != nil {
		t.Fatalf("executeThinkerTask(cal) failed: %v", err)
	}
	// Output should contain days or current month/year
	if !strings.Contains(out, "202") && !strings.Contains(out, "Su") && !strings.Contains(out, "Mo") {
		t.Fatalf("expected calendar output from cal command, got:\n%s", out)
	}
}

func TestExecuteThinkerTask_HistoryURLResolution(t *testing.T) {
	history := []voice.LiveTurnRecord{
		{
			Role:    "User",
			Content: "https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md#1510 这个文档你解释 1.5.10 逐条的更新内容",
		},
	}

	// Task from model does not have URL in it, but refers to changelog
	out, err := executeThinkerTask("/tmp", "Fetch and summarize the changes in version 1.5.10 from the antigravity-cli CHANGELOG.", history)
	if err != nil {
		t.Fatalf("executeThinkerTask with history failed: %v", err)
	}

	// Should have fetched the actual 1.5.10 section
	if !strings.Contains(out, "1.5.10") {
		t.Fatalf("expected output to contain 1.5.10 changelog, got:\n%s", out)
	}
	if !strings.Contains(out, "accept-edits") && !strings.Contains(out, "Orchestrator") {
		t.Fatalf("expected output to contain 1.5.10 features, got:\n%s", out)
	}
}

func TestExtractSearchQuery(t *testing.T) {
	tests := []struct {
		task       string
		wantQuery  string
		wantDomain string
		wantOK     bool
	}{
		{
			task:      "Search the web for golang 1.24 release",
			wantQuery: "golang 1.24 release",
			wantOK:    true,
		},
		{
			task:       "search google for pigasus github site:github.com",
			wantQuery:  "pigasus github",
			wantDomain: "github.com",
			wantOK:     true,
		},
		{
			task:      "web search: python 3.14 features",
			wantQuery: "python 3.14 features",
			wantOK:    true,
		},
		{
			task:      "search kubernetes architecture on the web",
			wantQuery: "kubernetes architecture",
			wantOK:    true,
		},
		{
			task:   "run the cal command in the terminal",
			wantOK: false,
		},
		{
			task:   "git status -s",
			wantOK: false,
		},
	}

	for _, tc := range tests {
		q, d, ok := extractSearchQuery(tc.task)
		if ok != tc.wantOK {
			t.Errorf("extractSearchQuery(%q) ok = %v, want %v", tc.task, ok, tc.wantOK)
			continue
		}
		if ok {
			if q != tc.wantQuery {
				t.Errorf("extractSearchQuery(%q) query = %q, want %q", tc.task, q, tc.wantQuery)
			}
			if d != tc.wantDomain {
				t.Errorf("extractSearchQuery(%q) domain = %q, want %q", tc.task, d, tc.wantDomain)
			}
		}
	}
}

func TestLiveChatModel_DualHorizontalRulesAndAutoExpandHeight(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Puck", "gemini-2.5-flash")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// 1. Initial State: Single-line input
	if h := m.input.Height(); h != 1 {
		t.Fatalf("expected initial input height = 1, got %d", h)
	}
	if vph := m.viewport.Height(); vph != 20 {
		t.Fatalf("expected initial viewport height = 20, got %d", vph)
	}

	v := m.View()
	contentLines := strings.Split(v.Content, "\n")
	if len(contentLines) < 24 {
		t.Fatalf("expected at least 24 lines in view, got %d", len(contentLines))
	}

	// Line 20: Top rule
	if !strings.Contains(contentLines[20], "─") {
		t.Errorf("expected line 20 to be top rule, got: %q", contentLines[20])
	}
	// Line 21: Composer input
	if !strings.Contains(contentLines[21], "> ") {
		t.Errorf("expected line 21 to contain prompt '> ', got: %q", contentLines[21])
	}
	// Line 22: Bottom rule
	if !strings.Contains(contentLines[22], "─") {
		t.Errorf("expected line 22 to be bottom rule, got: %q", contentLines[22])
	}
	// Line 23: Status bar
	if !strings.Contains(contentLines[23], "Live") {
		t.Errorf("expected line 23 to be status bar, got: %q", contentLines[23])
	}

	// Hardware cursor positioning
	if v.Cursor == nil {
		t.Fatal("expected v.Cursor to be non-nil")
	}
	if v.Cursor.Y != 21 {
		t.Errorf("expected cursor Y = 21, got %d", v.Cursor.Y)
	}

	// 2. Type multi-line text using Ctrl+J
	_, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	_, _ = m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	_, _ = m.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
	_, _ = m.Update(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	_, _ = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})

	// Verify input height grew to 3, and viewport height shrank to 18
	if h := m.input.Height(); h != 3 {
		t.Fatalf("expected input height to auto-expand to 3, got %d", h)
	}
	if vph := m.viewport.Height(); vph != 18 {
		t.Fatalf("expected viewport height to shrink to 18, got %d", vph)
	}

	v2 := m.View()
	lines2 := strings.Split(v2.Content, "\n")

	// Line 18: Top rule
	if !strings.Contains(lines2[18], "─") {
		t.Errorf("expected line 18 to be top rule, got: %q", lines2[18])
	}
	// Line 19: Input line 1
	if !strings.Contains(lines2[19], "a") {
		t.Errorf("expected line 19 to contain 'a', got: %q", lines2[19])
	}
	// Line 20: Input line 2
	if !strings.Contains(lines2[20], "b") {
		t.Errorf("expected line 20 to contain 'b', got: %q", lines2[20])
	}
	// Line 21: Input line 3
	if !strings.Contains(lines2[21], "c") {
		t.Errorf("expected line 21 to contain 'c', got: %q", lines2[21])
	}
	// Line 22: Bottom rule
	if !strings.Contains(lines2[22], "─") {
		t.Errorf("expected line 22 to be bottom rule, got: %q", lines2[22])
	}
	// Line 23: Status bar
	if !strings.Contains(lines2[23], "Live") {
		t.Errorf("expected line 23 to be status bar, got: %q", lines2[23])
	}

	// Hardware cursor on line 3 of input (18 top-lines + 1 top-rule + 2 input-offset = 21)
	if v2.Cursor == nil || v2.Cursor.Y != 21 {
		t.Errorf("expected cursor Y = 21 on line 3, got %+v", v2.Cursor)
	}

	// 3. Clear via Esc -> height recovers immediately
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if h := m.input.Height(); h != 1 {
		t.Fatalf("expected input height to shrink back to 1, got %d", h)
	}
	if vph := m.viewport.Height(); vph != 20 {
		t.Fatalf("expected viewport height to recover to 20, got %d", vph)
	}
}

func TestLiveChatModel_SessionAutoSaveAndResume(t *testing.T) {
	tempDir := t.TempDir()
	voice.SetLiveSessionDirOverride(tempDir)
	defer voice.SetLiveSessionDirOverride("")

	ws := "/tmp/testws"
	m := newLiveChatModel(nil, nil, ws, "Puck", "gemini-2.5-flash", false)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	origSessionID := m.sessionID
	if origSessionID == "" {
		t.Fatal("expected non-empty initial sessionID")
	}

	// 1. Simulate user typing and sending a message
	m.input.SetValue("check repository health")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Simulate agent response
	_, _ = m.Update(agentSpeechMsg{text: "All checks passed cleanly."})

	// Verify session file was auto-saved to tempDir
	sessionPath := filepath.Join(tempDir, origSessionID+".json")
	if _, err := os.Stat(sessionPath); err != nil {
		t.Fatalf("expected auto-saved session file %s, err = %v", sessionPath, err)
	}

	loaded, err := voice.LoadLiveSession(origSessionID)
	if err != nil {
		t.Fatalf("failed to load saved session: %v", err)
	}
	if loaded.Workspace != ws {
		t.Errorf("saved workspace = %q, want %q", loaded.Workspace, ws)
	}
	if len(loaded.Logs) == 0 {
		t.Error("expected non-empty logs in saved session")
	}

	// 2. Test /sessions or /resume lists sessions
	m.input.SetValue("/resume")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	// Viewport should contain the session list header and session ID
	v := m.View()
	if !strings.Contains(v.Content, "[Saved LiveChat Sessions]") {
		t.Errorf("expected view to contain session list header, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, origSessionID) {
		t.Errorf("expected view to contain session ID %s, got:\n%s", origSessionID, v.Content)
	}

	// 3. Create a second session in the same store
	secondSession := &voice.LiveSessionData{
		ID:        "20261117-120000-abcd",
		Workspace: ws,
		Model:     "gemini-2.5-flash",
		Voice:     "Aoede",
		CreatedAt: time.Now().Add(-1 * time.Hour),
		Turns: []voice.LiveTurnRecord{
			{Role: "User", Content: "how do I configure pigasus"},
			{Role: "Agent", Content: "You can edit ~/.openagy/config.json."},
		},
		Logs: []string{
			"💬 [You]: how do I configure pigasus",
			"🤖 [Agent]: You can edit ~/.openagy/config.json.",
		},
	}
	if err := voice.SaveLiveSession(secondSession); err != nil {
		t.Fatalf("failed to save second session: %v", err)
	}

	// 4. Test switching to the second session using /resume <id>
	m.input.SetValue("/resume 20261117-120000-abcd")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.sessionID != "20261117-120000-abcd" {
		t.Fatalf("m.sessionID = %q, want 20261117-120000-abcd", m.sessionID)
	}

	v2 := m.View()
	if !strings.Contains(v2.Content, "Switched to session 20261117-120000-abcd") {
		t.Errorf("expected session switch confirmation in view, got:\n%s", v2.Content)
	}
	if !strings.Contains(v2.Content, "how do I configure pigasus") {
		t.Errorf("expected restored chat log in view, got:\n%s", v2.Content)
	}

	// Test prompt history recall (Up arrow should recall restored user turn)
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.input.Value(); got != "how do I configure pigasus" {
		t.Errorf("recalled prompt history = %q, want 'how do I configure pigasus'", got)
	}
}

func TestLiveChatModel_ResumeCLIWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	voice.SetLiveSessionDirOverride(tempDir)
	defer voice.SetLiveSessionDirOverride("")

	ws := "/tmp/testws"
	savedSession := &voice.LiveSessionData{
		ID:        "20261117-100000-feed",
		Workspace: ws,
		Model:     "gemini-2.5-flash",
		Voice:     "Puck",
		CreatedAt: time.Now().Add(-2 * time.Hour),
		Turns: []voice.LiveTurnRecord{
			{Role: "User", Content: "run cal"},
			{Role: "Agent", Content: "November 2026"},
		},
		Logs: []string{
			"💬 [You]: run cal",
			"🤖 [Agent]: November 2026",
		},
	}

	m := newLiveChatModel(nil, nil, ws, "Puck", "gemini-2.5-flash", false)
	m.LoadSession(savedSession)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	if m.sessionID != "20261117-100000-feed" {
		t.Fatalf("sessionID = %q, want 20261117-100000-feed", m.sessionID)
	}

	v := m.View()
	if !strings.Contains(v.Content, "Restored context from 20261117-100000-feed") {
		t.Errorf("expected restore notice in view, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "run cal") {
		t.Errorf("expected restored user log in view, got:\n%s", v.Content)
	}

	// Verify Up arrow recalls restored command
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	if got := m.input.Value(); got != "run cal" {
		t.Errorf("recalled input = %q, want 'run cal'", got)
	}
}

func TestNativeLiveSession_RehydrateResumeContext(t *testing.T) {
	mockB := voice.NewMockLiveBackend()
	sess := voice.NewNativeLiveSession(mockB, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sess.Start(ctx, voice.LiveSessionConfig{Voice: "Puck"}); err != nil {
		t.Fatalf("failed to start live session: %v", err)
	}
	defer sess.HangUp()

	sess.SetConversationHistory([]voice.LiveTurnRecord{
		{Role: "User", Content: "Can you analyze the changelog?"},
		{Role: "Agent", Content: "I can check the 1.5.10 updates."},
	})

	if err := sess.RehydrateResumeContext(); err != nil {
		t.Fatalf("RehydrateResumeContext failed: %v", err)
	}

	last := mockB.LastSession()
	if last == nil {
		t.Fatal("expected mock session to exist")
	}
	injected := last.InjectedTexts()
	if len(injected) == 0 {
		t.Fatal("expected rehydration text to be injected into session")
	}
	if !strings.Contains(injected[0], "Can you analyze the changelog?") {
		t.Errorf("expected rehydrated text to contain user turn, got %q", injected[0])
	}
	if !strings.Contains(injected[0], "I can check the 1.5.10 updates.") {
		t.Errorf("expected rehydrated text to contain agent turn, got %q", injected[0])
	}
}

func TestLiveChatModel_AltScreenAndMouseMode(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	v := m.View()
	if !v.AltScreen {
		t.Errorf("expected view.AltScreen == true, got false")
	}
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("expected view.MouseMode == tea.MouseModeCellMotion, got %v", v.MouseMode)
	}
}

func TestLiveChatModel_LineWrapLongLogs(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 24})

	longMsg := "🔔 [Proactive Observer]: Detected file changes in openagy (AGENTS.md, cmd/tui-inventory/main.go, docs/guide/remote-agents.md, docs/guide/tui-storybook-blueprint.md, +1400 more files changed)"
	m.appendLog(longMsg)

	// Verify that renderLogs wraps text so that lines are within 50 columns
	rendered := m.renderLogs(50)
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected long message to be wrapped into multiple lines, got %d lines:\n%s", len(lines), rendered)
	}

	// Verify the viewport content contains the wrapped lines and no text was lost
	vpView := m.viewport.View()
	if !strings.Contains(vpView, "Proactive Observer") || !strings.Contains(vpView, "+1400 more") {
		t.Fatalf("expected wrapped content to be visible in viewport, got:\n%s", vpView)
	}
}

func TestLiveChatModel_ViewportScrolling(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

	// Add 40 lines to trigger scrolling
	for i := 1; i <= 40; i++ {
		m.appendLog(fmt.Sprintf("Log message line %02d", i))
	}

	if !m.viewport.AtBottom() {
		t.Fatal("expected viewport to initially be at bottom after appending logs")
	}

	// 1. Mouse Wheel Up scrolls up
	wheelUp := tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp})
	_, _ = m.Update(wheelUp)
	if m.viewport.AtBottom() {
		t.Error("expected viewport to scroll up and not be AtBottom after MouseWheelUp")
	}

	// 2. Incoming agent delta speech while scrolled up must NOT force viewport to bottom
	_, _ = m.Update(agentSpeechDeltaMsg{delta: "streaming token while user reads history"})
	if m.viewport.AtBottom() {
		t.Error("expected viewport scroll position to be preserved while user is reading history")
	}

	// 3. Mouse Wheel Down scrolls back down
	for i := 0; i < 5; i++ {
		wheelDown := tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown})
		_, _ = m.Update(wheelDown)
	}
	if !m.viewport.AtBottom() {
		t.Error("expected viewport to return to bottom after scrolling down")
	}

	// 4. Test KeyPgUp and KeyPgDown
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.viewport.AtBottom() {
		t.Error("expected KeyPgUp to move viewport up")
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})

	// 5. Shift+Up / Shift+Down
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModShift})
	if m.viewport.AtBottom() {
		t.Error("expected Shift+Up to scroll viewport up by 1")
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModShift})
	if !m.viewport.AtBottom() {
		t.Error("expected Shift+Down to return viewport to bottom")
	}
}

func TestLiveChatModel_VoiceCatalogAndSwitch(t *testing.T) {
	mockB := voice.NewMockLiveBackend()
	sess := voice.NewNativeLiveSession(mockB, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = sess.Start(ctx, voice.LiveSessionConfig{Voice: "Aoede"})
	defer sess.HangUp()

	m := newLiveChatModel(sess, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// 1. /voice lists personas
	m.input.SetValue("/voice")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	v := m.View()
	if !strings.Contains(v.Content, "Antigravity 9 Canonical Personas") {
		t.Errorf("expected view to contain Antigravity personas header, got:\n%s", v.Content)
	}
	if !strings.Contains(v.Content, "Fola") || !strings.Contains(v.Content, "Cleo") || !strings.Contains(v.Content, "Neno") {
		t.Errorf("expected view to list canonical personas, got:\n%s", v.Content)
	}

	// 2. /voice Cleo switches voice to Cleo (Gemini Kore)
	m.input.SetValue("/voice Cleo")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if m.voiceName != "Cleo" {
		t.Errorf("expected m.voiceName == 'Cleo', got %q", m.voiceName)
	}

	v2 := m.View()
	if !strings.Contains(v2.Content, "Switched to persona Cleo") {
		t.Errorf("expected switch confirmation, got:\n%s", v2.Content)
	}
	if !strings.Contains(v2.Content, "Cleo") {
		t.Errorf("expected status bar to show Cleo, got:\n%s", v2.Content)
	}

	// 3. /voice speaker Fenrir switches to direct Gemini voice Fenrir
	m.input.SetValue("/voice speaker Fenrir")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.voiceName != "Fenrir" {
		t.Errorf("expected m.voiceName == 'Fenrir', got %q", m.voiceName)
	}
}

func TestLiveChatModel_ProactiveWatcherToggleAndDebounce(t *testing.T) {
	watcher := NewProactiveWatcher("/tmp", nil, 10*time.Second)
	m := newLiveChatModel(nil, watcher, "/tmp", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// 1. /p off pauses watcher
	m.input.SetValue("/p off")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !watcher.IsPaused() {
		t.Error("expected watcher to be paused after /p off")
	}
	v := m.View()
	if !strings.Contains(v.Content, "Workspace file monitoring PAUSED") {
		t.Errorf("expected pause message in view, got:\n%s", v.Content)
	}

	// 2. /p on resumes watcher
	m.input.SetValue("/p on")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if watcher.IsPaused() {
		t.Error("expected watcher to be resumed after /p on")
	}

	// 3. User turn resets pending reminder
	watcher.pendingUserTurn.Store(true)
	m.input.SetValue("I am writing code now")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if watcher.pendingUserTurn.Load() {
		t.Error("expected user sending message to reset pendingUserTurn")
	}
}

func TestLiveChatModel_ContinuousReconnectionAndOfflineQueue(t *testing.T) {
	mockBackend := voice.NewMockLiveBackend()
	sess := voice.NewNativeLiveSession(mockBackend, nil)
	m := newLiveChatModel(sess, nil, "/tmp/testws", "Puck", "gemini-2.5-flash")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Initial reconnecting attempts
	_, _ = m.Update(reconnectingMsg{attempt: 1, maxAttempts: 5})
	if !m.reconnecting || m.disconnected {
		t.Fatalf("expected reconnecting=true, disconnected=false")
	}
	v1 := m.View()
	if !strings.Contains(v1.Content, "RECONNECTING (1/5)") {
		t.Fatalf("expected status bar to show RECONNECTING (1/5), got:\n%s", v1.Content)
	}

	// 2. Reconnect attempts exhausted -> errorMsg transitions to OFFLINE
	_, _ = m.Update(errorMsg{err: fmt.Errorf("network connection refused")})
	if m.reconnecting || !m.disconnected {
		t.Fatalf("expected reconnecting=false, disconnected=true")
	}
	v2 := m.View()
	if !strings.Contains(v2.Content, "OFFLINE") || !strings.Contains(v2.Content, "Press Enter to retry") {
		t.Fatalf("expected status bar to show OFFLINE with retry hint, got:\n%s", v2.Content)
	}
	if !strings.Contains(v2.Content, "Tip: Type a message and press Enter, or type '/retry'") {
		t.Fatalf("expected chat logs to show helpful reconnection tip, got:\n%s", v2.Content)
	}

	// 3. User types message "hihi" while offline and presses Enter -> Queued!
	m.input.SetValue("hihi")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.pendingText != "hihi" {
		t.Fatalf("expected m.pendingText == 'hihi', got %q", m.pendingText)
	}
	v3 := m.View()
	if !strings.Contains(v3.Content, "[You (Queued)]") || !strings.Contains(v3.Content, "hihi") {
		t.Fatalf("expected view to show queued user message, got:\n%s", v3.Content)
	}
	if !strings.Contains(v3.Content, "Triggering immediate") {
		t.Fatalf("expected view to indicate reconnection triggered, got:\n%s", v3.Content)
	}

	// 4. Session successfully reconnected -> Flushes queued message
	_, _ = m.Update(reconnectedMsg{})
	if m.reconnecting || m.disconnected {
		t.Fatalf("expected reconnecting=false, disconnected=false after reconnectedMsg")
	}
	if m.pendingText != "" {
		t.Fatalf("expected m.pendingText to be empty after flush, got %q", m.pendingText)
	}
	v4 := m.View()
	if !strings.Contains(v4.Content, "Reconnected") {
		t.Fatalf("expected view to show reconnected header, got:\n%s", v4.Content)
	}
	if !strings.Contains(v4.Content, "Dispatched Queued Message") || !strings.Contains(v4.Content, "hihi") {
		t.Fatalf("expected view to show dispatched queued message, got:\n%s", v4.Content)
	}
}

func TestLiveChatModel_ManualRetrySlashCommand(t *testing.T) {
	mockBackend := voice.NewMockLiveBackend()
	sess := voice.NewNativeLiveSession(mockBackend, nil)
	m := newLiveChatModel(sess, nil, "/tmp/testws", "Puck", "gemini-2.5-flash")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// Enter disconnected state
	_, _ = m.Update(errorMsg{err: fmt.Errorf("connection timed out")})
	if !m.disconnected {
		t.Fatal("expected disconnected == true")
	}

	// 1. /retry
	m.input.SetValue("/retry")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	v1 := m.View()
	if !strings.Contains(v1.Content, "Triggering connection retry immediately...") {
		t.Fatalf("expected /retry to trigger connection retry, got:\n%s", v1.Content)
	}

	// 2. /reconnect
	m.input.SetValue("/reconnect")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	v2 := m.View()
	if !strings.Contains(v2.Content, "Triggering connection retry immediately...") {
		t.Fatalf("expected /reconnect to trigger connection retry, got:\n%s", v2.Content)
	}
}

func TestLiveChatModel_SlashPickerVisibilityAndNavigation(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Initially picker is hidden
	if m.picker.Visible() {
		t.Fatal("picker should be hidden initially")
	}

	// 2. Type '/' -> picker opens
	_, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.picker.Visible() {
		t.Fatal("picker should be visible after typing '/'")
	}
	v := m.View()
	if !strings.Contains(v.Content, "/voice") || !strings.Contains(v.Content, "Navigate") {
		t.Fatalf("expected slashpicker overlay in view, got:\n%s", v.Content)
	}

	// 3. Down arrow navigates to next command
	firstSel := m.picker.Selected().Name
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	secondSel := m.picker.Selected().Name
	if firstSel == secondSel {
		t.Fatalf("expected selection to advance on KeyDown, stayed on %q", firstSel)
	}

	// 4. Esc closes the picker popup without clearing the composer input
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.picker.Visible() {
		t.Fatal("picker should be dismissed on Esc")
	}
	if m.input.Value() != "/" {
		t.Fatalf("composer input should be preserved on first Esc, got %q", m.input.Value())
	}
}

func TestLiveChatModel_VoiceAutocompletionAffordance(t *testing.T) {
	mockBackend := voice.NewMockLiveBackend()
	sess := voice.NewNativeLiveSession(mockBackend, nil)
	m := newLiveChatModel(sess, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Type '/v' -> filters to /voice
	m.input.SetValue("/v")
	m.picker.Update([]rune("/v"))
	if !m.picker.Visible() {
		t.Fatal("expected picker visible on '/v'")
	}
	if m.picker.Selected() == nil || m.picker.Selected().Name != "/voice" {
		t.Fatalf("expected /voice selected on '/v', got %v", m.picker.Selected())
	}

	// 2. Press Tab -> completes to '/voice ' and opens voice subcommands & personas
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "/voice " {
		t.Fatalf("expected input == '/voice ', got %q", m.input.Value())
	}
	if !m.picker.Visible() {
		t.Fatal("picker should stay visible in voice mode after completing '/voice '")
	}
	v := m.View()
	if !strings.Contains(v.Content, "speaker") || !strings.Contains(v.Content, "more") {
		t.Fatalf("expected voice options in view, got:\n%s", v.Content)
	}

	// 3. Type 'cl' -> narrows to 'cleo'
	for _, ch := range "cl" {
		_, _ = m.Update(tea.KeyPressMsg{Code: ch, Text: string(ch)})
	}
	if m.input.Value() != "/voice cl" {
		t.Fatalf("expected input == '/voice cl', got %q", m.input.Value())
	}
	if m.picker.Selected() == nil || m.picker.Selected().Name != "cleo" {
		t.Fatalf("expected 'cleo' selected on '/voice cl', got %v", m.picker.Selected())
	}

	// 4. Press Enter -> completes '/voice cleo', dismisses picker, and executes voice switch
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.voiceName != "Cleo" {
		t.Fatalf("expected voice switched to 'Cleo', got %q", m.voiceName)
	}
	v4 := m.View()
	if !strings.Contains(v4.Content, "Switched to persona Cleo") {
		t.Fatalf("expected switch confirmation in view, got:\n%s", v4.Content)
	}
	if !strings.Contains(v4.Content, "Cleo") {
		t.Fatalf("expected status bar to show Cleo, got:\n%s", v4.Content)
	}

	// 5. Autocomplete /voice speaker fenrir
	m.input.SetValue("/voice speaker fe")
	m.picker.Update([]rune("/voice speaker fe"))
	if !m.picker.Visible() {
		t.Fatal("expected picker visible on '/voice speaker fe'")
	}
	if m.picker.Selected() == nil || m.picker.Selected().Name != "fenrir" {
		t.Fatalf("expected 'fenrir' selected, got %v", m.picker.Selected())
	}
	// Press Tab -> completes to '/voice speaker fenrir'
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "/voice speaker fenrir" {
		t.Fatalf("expected '/voice speaker fenrir', got %q", m.input.Value())
	}
	// Press Enter -> executes switch
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.voiceName != "Fenrir" {
		t.Fatalf("expected voice switched to 'Fenrir', got %q", m.voiceName)
	}

	// 6. Direct /speaker puck
	m.input.SetValue("/speaker pu")
	m.picker.Update([]rune("/speaker pu"))
	if !m.picker.Visible() {
		t.Fatal("expected picker visible on '/speaker pu'")
	}
	if m.picker.Selected() == nil || m.picker.Selected().Name != "puck" {
		t.Fatalf("expected 'puck' selected, got %v", m.picker.Selected())
	}
	// Press Enter -> directly commits and executes
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.voiceName != "Puck" {
		t.Fatalf("expected voice switched to 'Puck', got %q", m.voiceName)
	}
}

func TestLiveChatModel_ResumeAutocompletion(t *testing.T) {
	tmpDir := t.TempDir()
	sessionData := &voice.LiveSessionData{
		ID:        "20261117-test-affordance",
		Workspace: tmpDir,
		Model:     "gemini-3.8-live",
		Voice:     "Cleo",
		CreatedAt: time.Now(),
		Turns: []voice.LiveTurnRecord{
			{Role: "User", Content: "Hello test"},
		},
		Logs: []string{"Hello test"},
	}
	_ = voice.SaveLiveSession(sessionData)

	m := newLiveChatModel(nil, nil, tmpDir, "Cleo", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	// 1. Type '/resume ' -> shows saved sessions + latest
	m.input.SetValue("/resume ")
	m.picker.Update([]rune("/resume "))
	if !m.picker.Visible() {
		t.Fatal("expected picker visible on '/resume '")
	}
	v := m.View()
	if !strings.Contains(v.Content, "latest") || !strings.Contains(v.Content, "20261117-test-affordance") {
		t.Fatalf("expected resume session IDs in view, got:\n%s", v.Content)
	}

	// 2. Type 'lat' -> narrows to latest
	m.input.SetValue("/resume lat")
	m.picker.Update([]rune("/resume lat"))
	if m.picker.Selected() == nil || m.picker.Selected().Name != "latest" {
		t.Fatalf("expected 'latest' selected, got %v", m.picker.Selected())
	}

	// 3. Tab completes to '/resume latest'
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.input.Value() != "/resume latest" {
		t.Fatalf("expected input == '/resume latest', got %q", m.input.Value())
	}
}

func TestLiveChatModel_ViewportMouseSelectionAndCopy(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Cleo", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.appendLog("🤖 [Agent]: Hello, I am your proactive voice assistant.")
	m.appendLog("💬 [You]: Please check my git status.")

	// Mock clipboard writer
	var copiedText string
	origWriter := clipboard.Writer
	clipboard.Writer = func(text string) error {
		copiedText = text
		return nil
	}
	defer func() { clipboard.Writer = origWriter }()

	// 1. Mouse press on line 0 (row 0, col 5)
	m.Update(tea.MouseClickMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	if !m.vpSel.SelDragging || m.vpSel.SelStartRow != 0 || m.vpSel.SelStartCol != 5 {
		t.Fatalf("expected drag start at (0, 5), got: %+v", m.vpSel)
	}

	// 2. Mouse motion to col 25 on row 0
	m.Update(tea.MouseMotionMsg{X: 25, Y: 0, Button: tea.MouseLeft})
	if !m.vpSel.SelDragging || m.vpSel.SelEndRow != 0 || m.vpSel.SelEndCol != 25 {
		t.Fatalf("expected drag end at (0, 25), got: %+v", m.vpSel)
	}

	// Invariant 13: UI text selection capability must be backed by rendered ANSI sequences (\x1b[7m)
	v := m.View()
	if !strings.Contains(v.Content, "\x1b[7m") {
		t.Fatalf("expected rendered view to contain reverse-video selection highlight (\\x1b[7m), got:\n%s", v.Content)
	}

	// 3. Mouse release on col 25
	_, cmd := m.Update(tea.MouseReleaseMsg{X: 25, Y: 0, Button: tea.MouseLeft})
	if m.vpSel.SelDragging {
		t.Fatal("expected SelDragging to be false after mouse release")
	}
	if !m.hasVpSelection() {
		t.Fatal("expected selection highlight to persist after release until next action")
	}
	if copiedText == "" {
		t.Fatal("expected text to be copied to clipboard on release")
	}
	if !strings.Contains(copiedText, "Proactive") {
		t.Fatalf("expected copied text to contain 'Proactive', got %q", copiedText)
	}
	if cmd == nil {
		t.Fatal("expected tea.SetClipboard cmd on release")
	}

	// 4. Cmd+C copies again while selection is active
	copiedText = ""
	_, _ = m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModSuper})
	if copiedText == "" || !strings.Contains(copiedText, "Proactive") {
		t.Fatalf("expected Cmd+C to copy selection, got %q", copiedText)
	}

	// 5. Esc clears the selection
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.hasVpSelection() {
		t.Fatal("expected Esc to clear viewport selection")
	}
	vAfterEsc := m.View()
	if strings.Contains(vAfterEsc.Content, "\x1b[7m") {
		t.Fatalf("expected reverse-video highlight to be cleared after Esc, got:\n%s", vAfterEsc.Content)
	}
}

func TestLiveChatModel_ClickWithoutDragClearsSelection(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Cleo", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.appendLog("Sample line of log output")

	var copied string
	origWriter := clipboard.Writer
	clipboard.Writer = func(text string) error {
		copied = text
		return nil
	}
	defer func() { clipboard.Writer = origWriter }()

	// Click and release on same cell (row 0, col 5) without dragging
	m.Update(tea.MouseClickMsg{X: 5, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 5, Y: 0, Button: tea.MouseLeft})

	if m.hasVpSelection() {
		t.Fatal("expected no selection after click-without-drag")
	}
	if copied != "" {
		t.Fatalf("expected no clipboard copy on click-without-drag, got %q", copied)
	}
	if strings.Contains(m.View().Content, "\x1b[7m") {
		t.Fatal("expected no highlight on click-without-drag")
	}
}

func TestLiveChatModel_ClickInComposerClearsViewportSelection(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Cleo", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.appendLog("Sample line of log output")

	// 1. Establish viewport selection
	m.Update(tea.MouseClickMsg{X: 2, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 10, Y: 0, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 10, Y: 0, Button: tea.MouseLeft})

	if !m.hasVpSelection() {
		t.Fatal("expected selection to be active")
	}

	// 2. Click in composer area (near the bottom)
	m.Update(tea.MouseClickMsg{X: 5, Y: 22, Button: tea.MouseLeft})
	if m.hasVpSelection() {
		t.Fatal("expected click in composer to clear viewport selection")
	}
}

func TestLiveChatModel_SearchSlashCommand(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Cleo", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// 1. Send /search without query -> shows usage tip
	m.input.SetValue("/search")
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !strings.Contains(m.logs[len(m.logs)-1], "Usage: /search <query>") {
		t.Fatalf("expected usage tip, got: %s", m.logs[len(m.logs)-1])
	}

	// 2. Send /search with query -> triggers search and appends search indicator
	m.input.SetValue("/search golang concurrency patterns")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected search command on /search with query")
	}

	// Verify log contains the search indicator
	found := false
	for _, l := range m.logs {
		if strings.Contains(l, "[Web Search]: golang concurrency patterns") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected search log entry in logs, got:\n%s", strings.Join(m.logs, "\n"))
	}

	// 3. Dispatch searchDoneMsg -> formats search results in logs
	m.Update(searchDoneMsg{
		query:  "golang concurrency patterns",
		result: "1. Concurrency in Go\n   URL: https://go.dev/doc/concurrency\n   Comprehensive guide to goroutines and channels.",
		err:    nil,
	})
	lastLog := m.logs[len(m.logs)-1]
	if !strings.Contains(lastLog, "[Search Results]") || !strings.Contains(lastLog, "Concurrency in Go") {
		t.Fatalf("expected search result log entry, got:\n%s", lastLog)
	}
}

func TestLiveChatHandlerBridge_OnToolCall_SearchWeb(t *testing.T) {
	origSearch := performWebSearch
	performWebSearch = func(ctx context.Context, query, domain string) (string, error) {
		return "1. Mock result for " + query, nil
	}
	defer func() { performWebSearch = origSearch }()

	mockB := voice.NewMockLiveBackend()
	bridge := &liveChatHandlerBridge{workspace: "/tmp"}
	sess := voice.NewNativeLiveSession(mockB, bridge)
	bridge.sess = sess

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := sess.Start(ctx, voice.LiveSessionConfig{Voice: "Aoede"}); err != nil {
		t.Fatalf("sess.Start failed: %v", err)
	}
	defer sess.HangUp()

	// Simulate tool call from Gemini Live
	call := voice.LiveToolCall{
		ID:   "call-search-123",
		Name: "search_web",
		Args: map[string]any{
			"query": "bubbletea framework",
		},
	}
	bridge.OnToolCall(call)

	// Verify response was sent within 5 seconds
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resps := mockB.LastSession().ToolResponses()
		if _, ok := resps["call-search-123"]; ok {
			return // Success
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("expected ToolResponse for search_web to be sent to LiveSession")
}

// stubVoicePreview swaps the recorded-sample player for a recorder and returns
// the calls it saw plus a restore func.
func stubVoicePreview(t *testing.T, err error) (*[]string, func()) {
	t.Helper()
	orig := playVoicePreview
	var calls []string
	playVoicePreview = func(ctx context.Context, name string, variant int) (voice.VoicePreviewSample, error) {
		calls = append(calls, fmt.Sprintf("%s#%d", name, variant))
		if err != nil {
			return voice.VoicePreviewSample{}, err
		}
		return voice.ResolveVoicePreviewSample(name, variant)
	}
	return &calls, func() { playVoicePreview = orig }
}

func TestLiveChatModel_VoiceListMarksRecordedPersonas(t *testing.T) {
	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.input.SetValue("/voice")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("listing voices should not fire a command, got %T", cmd())
	}

	joined := strings.Join(m.logs, "\n")
	if !strings.Contains(joined, "♪ • Tari") || !strings.Contains(joined, "♪ • Fola") {
		t.Fatalf("expected recorded personas to carry the ♪ marker, got:\n%s", joined)
	}
	if !strings.Contains(joined, "/voice preview <name> [1-3|all]") {
		t.Fatalf("expected the preview tip in the voice list, got:\n%s", joined)
	}
}

func TestLiveChatModel_VoicePreviewSlashCommand(t *testing.T) {
	calls, restore := stubVoicePreview(t, nil)
	defer restore()

	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.input.SetValue("/voice preview tari 2")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a preview command for '/voice preview tari 2'")
	}
	if !strings.Contains(strings.Join(m.logs, "\n"), "playing tari sample 2/3") {
		t.Fatalf("expected the preview announcement, got:\n%s", strings.Join(m.logs, "\n"))
	}

	msg, ok := cmd().(voicePreviewMsg)
	if !ok {
		t.Fatalf("expected voicePreviewMsg, got %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("preview command failed: %v", msg.err)
	}
	if msg.sample.Key != "tari_2" || msg.sample.Persona != "Tari" {
		t.Fatalf("played %+v, want tari_2/Tari", msg.sample)
	}
	if len(*calls) != 1 || (*calls)[0] != "tari#2" {
		t.Fatalf("player calls = %v, want [tari#2]", *calls)
	}

	_, _ = m.Update(msg)
	if !strings.Contains(m.logs[len(m.logs)-1], "Played Tari (tari_2.mp3, sample 2/3)") {
		t.Fatalf("expected the played line, got: %s", m.logs[len(m.logs)-1])
	}
}

func TestLiveChatModel_VoicePreviewAllVariants(t *testing.T) {
	calls, restore := stubVoicePreview(t, nil)
	defer restore()

	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.input.SetValue("/voice preview finn all")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a preview command for 'all'")
	}
	msg := cmd().(voicePreviewMsg)
	if msg.err != nil {
		t.Fatalf("preview all failed: %v", msg.err)
	}
	want := []string{"finn#1", "finn#2", "finn#3"}
	if len(*calls) != len(want) {
		t.Fatalf("player calls = %v, want %v", *calls, want)
	}
	for i := range want {
		if (*calls)[i] != want[i] {
			t.Fatalf("player calls = %v, want %v", *calls, want)
		}
	}
	if msg.sample.Key != "finn_3" {
		t.Fatalf("last sample = %s, want finn_3", msg.sample.Key)
	}
}

func TestLiveChatModel_VoicePreviewRejectsUnknownVoice(t *testing.T) {
	calls, restore := stubVoicePreview(t, nil)
	defer restore()

	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.input.SetValue("/voice preview Zephyr")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("no clip exists for Zephyr, so no command should fire (got %T)", cmd())
	}
	if len(*calls) != 0 {
		t.Fatalf("player should not be invoked, got %v", *calls)
	}
	last := m.logs[len(m.logs)-1]
	if !strings.Contains(last, "no recording for \"Zephyr\"") || !strings.Contains(last, "Tari") {
		t.Fatalf("expected a no-recording line listing the recorded personas, got: %s", last)
	}
}

func TestLiveChatModel_VoicePreviewRejectsBadVariant(t *testing.T) {
	_, restore := stubVoicePreview(t, nil)
	defer restore()

	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.input.SetValue("/voice preview tari 9")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("variant 9 is out of range, no command expected (got %T)", cmd())
	}
	if !strings.Contains(m.logs[len(m.logs)-1], "out of range") {
		t.Fatalf("expected an out-of-range line, got: %s", m.logs[len(m.logs)-1])
	}
}

func TestLiveChatModel_VoiceSwitchPlaysPreview(t *testing.T) {
	calls, restore := stubVoicePreview(t, nil)
	defer restore()

	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	m.voicePreview = true
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.input.SetValue("/voice Cleo")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected switch-time preview to be enabled")
	}
	if msg, ok := cmd().(voicePreviewMsg); !ok || msg.sample.Key != "cleo_1" {
		t.Fatalf("switch preview = %#v, want cleo_1", msg)
	}
	if len(*calls) != 1 || (*calls)[0] != "Cleo#1" {
		t.Fatalf("player calls = %v, want [Cleo#1]", *calls)
	}
}

func TestLiveChatModel_VoicePreviewToggleSilencesSwitchClip(t *testing.T) {
	calls, restore := stubVoicePreview(t, nil)
	defer restore()

	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	m.voicePreview = true
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	m.input.SetValue("/voice preview off")
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("toggling preview off should not play anything (got %T)", cmd())
	}
	if m.voicePreview {
		t.Fatal("expected m.voicePreview == false after '/voice preview off'")
	}

	m.input.SetValue("/voice Arlo")
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("switch-time clip is off, so no command expected (got %T)", cmd())
	}
	if len(*calls) != 0 {
		t.Fatalf("player should not be invoked while muted, got %v", *calls)
	}

	// An explicit request still plays while the toggle is off.
	m.input.SetValue("/voice preview Arlo 3")
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("explicit /voice preview must play even with switch clips off")
	}
	if msg := cmd().(voicePreviewMsg); msg.sample.Key != "arlo_3" {
		t.Fatalf("explicit preview = %s, want arlo_3", msg.sample.Key)
	}

	m.input.SetValue("/voice preview on")
	if _, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatalf("re-enabling preview should not play anything (got %T)", cmd())
	}
	if !m.voicePreview {
		t.Fatal("expected m.voicePreview == true after '/voice preview on'")
	}
}

func TestLiveChatModel_VoicePreviewMissingPlayerIsExplained(t *testing.T) {
	_, restore := stubVoicePreview(t, fmt.Errorf("%w (tried afplay, ffplay)", voice.ErrNoAudioPlayer))
	defer restore()

	m := newLiveChatModel(nil, nil, "/tmp/testws", "Fola", "gemini-3.8-live")
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})

	_, _ = m.Update(voicePreviewMsg{err: fmt.Errorf("%w (tried afplay, ffplay)", voice.ErrNoAudioPlayer)})
	last := m.logs[len(m.logs)-1]
	if !strings.Contains(last, "no audio player on PATH") || !strings.Contains(last, "afplay") {
		t.Fatalf("expected the missing-player hint, got: %s", last)
	}
}

func TestLiveChatModel_VoicePreviewCmdUsesBoundedContext(t *testing.T) {
	var sawDeadline bool
	orig := playVoicePreview
	playVoicePreview = func(ctx context.Context, name string, variant int) (voice.VoicePreviewSample, error) {
		_, sawDeadline = ctx.Deadline()
		return voice.ResolveVoicePreviewSample(name, variant)
	}
	defer func() { playVoicePreview = orig }()

	msg := voicePreviewCmd("Fola", 1, false)()
	if msg.(voicePreviewMsg).err != nil {
		t.Fatalf("preview cmd failed: %v", msg.(voicePreviewMsg).err)
	}
	if !sawDeadline {
		t.Fatal("preview playback must run under a context deadline")
	}
}
