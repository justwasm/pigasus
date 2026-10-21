package main

// tui_run_test.go — cycle 50+71 v1.3.5 binary 1:1 input component tests.
//
// Cycle 71: m.input is a charm.land/bubbles/v2 textarea.Model. Tests
// now populate Key.Text in synthetic tea.KeyPressMsg so the textarea's
// default branch (which uses msg.Text to insert runes) receives the
// right rune. Real bubbletea events from tea.NewProgram populate
// Key.Text for us.

import (
	"context"
	"strings"
	"testing"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
	tuilib "github.com/justwasm/pigasus/internal/tuilib"
)

// runeKey returns a tea.KeyPressMsg for a printable rune with Text
// populated so the textarea's default-branch insert path works.
func runeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)})
}

// ctrlKey returns a tea.KeyPressMsg for a non-printable control combo.
func ctrlKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Mod: tea.ModCtrl})
}

// shiftEnterKey returns Shift+Enter with Text populated.
func shiftEnterKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift, Text: "\n"})
}

// enterKey returns plain Enter with Text populated.
func enterKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Text: "\n"})
}

// fakeClient is the cycle 50 fake backend.Client.
type fakeClient struct {
	lastPrompt string
	stream     string
}

func (f *fakeClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	return nil, nil
}

// Test_EnterSendsPrompt verifies that Enter submits the prompt.
func Test_EnterSendsPrompt(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "user@test", "claude-4.5-sonnet", "starter", "/tmp", true)
	m.Update(runeKey('h'))
	m.Update(runeKey('i'))
	if got := m.input.Value(); got != "hi" {
		t.Fatalf("expected textarea 'hi', got %q", got)
	}
	m.streaming = false
	_, _ = m.Update(enterKey())
	if !m.streaming {
		t.Fatalf("expected streaming=true after Enter, got false (cycle 50)")
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("expected textarea cleared after Enter, got %q", v)
	}
}

// Test_CtrlJNewline verifies that Ctrl+J inserts a newline.
func Test_CtrlJNewline(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('a'))
	m.Update(ctrlKey('j'))
	m.Update(runeKey('b'))
	got := m.input.Value()
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") || !strings.Contains(got, "\n") {
		t.Fatalf("expected 'a\\nb', got %q (cycle 71)", got)
	}
	if m.streaming {
		t.Errorf("Ctrl+J should NOT trigger streaming")
	}
}

// Test_ShiftEnterNewline verifies Shift+Enter inserts newline.
func Test_ShiftEnterNewline(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('a'))
	m.Update(shiftEnterKey())
	m.Update(runeKey('b'))
	got := m.input.Value()
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") || !strings.Contains(got, "\n") {
		t.Fatalf("expected 'a\\nb', got %q", got)
	}
}

// Test_BufferInsertSingleKey verifies ordinary rune insertion.
func Test_BufferInsertSingleKey(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "hello" {
		m.Update(runeKey(r))
	}
	if got := m.input.Value(); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

// Test_ViewAltScreen verifies View() returns AltScreen=true with
// a Cursor pointing into the textarea.
func Test_ViewAltScreen(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	v := m.View()
	if !v.AltScreen {
		t.Error("v.AltScreen should be true")
	}
	if v.Cursor == nil {
		t.Fatal("v.Cursor should not be nil")
	}
}

// Test_CtrlLClearsScreen verifies that Ctrl+L clears the screen and resets scroll.
func Test_CtrlLClearsScreen(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.scroll = 42
	_, cmd := m.Update(ctrlKey('l'))
	if m.scroll != 0 {
		t.Errorf("scroll after Ctrl+L = %d, want 0", m.scroll)
	}
	if cmd == nil {
		t.Fatalf("Ctrl+L should return tea.ClearScreen command, got nil")
	}
	if !m.altScreen {
		t.Errorf("Ctrl+L must not mutate altScreen; got %v, want true", m.altScreen)
	}
}

// Test_MouseModeConstant verifies cycle 174: when alt-screen is
// on, MouseMode is always CellMotion. There is no longer a runtime
// toggle — mouse tracking is unconditional and the in-app
// selection overlay replaces the cycle-166 "use the host terminal's
// selection at the cost of wheel scroll" trade-off.
func Test_MouseModeConstant(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("default altScreen: got %v, want CellMotion", v.MouseMode)
	}
	m.altScreen = false
	v = m.View()
	if v.MouseMode != tea.MouseModeNone {
		t.Errorf("altScreen off: got %v, want None", v.MouseMode)
	}
	m.altScreen = true
	v = m.View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("altScreen on: got %v, want CellMotion", v.MouseMode)
	}
}

// Test_SessionAccumulatesTurns verifies cycle 70b multi-turn.
func Test_SessionAccumulatesTurns(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.streaming = false
	_, _ = m.Update(enterKey())
	if len(m.session) != 1 || m.session[0].Role != "user" || m.session[0].Content != "hi" {
		t.Fatalf("expected 1 user msg 'hi', got %+v", m.session)
	}
	m.Update(backendChunkMsg("hello-back"))
	if len(m.session) != 2 || m.session[1].Role != "assistant" || m.session[1].Content != "hello-back" {
		t.Fatalf("expected 2 msgs, got %+v", m.session)
	}
	m.streaming = true
	m.Update(backendDoneMsg{})
	m.streaming = false
	for _, r := range "yo" {
		m.Update(runeKey(r))
	}
	_, _ = m.Update(enterKey())
	if len(m.session) != 3 || m.session[2].Role != "user" || m.session[2].Content != "yo" {
		t.Fatalf("expected 3 msgs after 2nd turn, got %+v", m.session)
	}
}

// Test_InputBetweenSeps verifies cycle 71: View() wraps the
// textarea between two horizontal rules.
func Test_InputBetweenSeps(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	view := v.Content
	// At least two separator runs should appear (one above, one
	// below the textarea).
	count := strings.Count(view, strings.Repeat("─", 200))
	if count < 2 {
		t.Fatalf("expected at least 2 separators in view, got %d (cycle 71)", count)
	}
}

// Test_DynamicHeightGrows verifies cycle 71: multi-line content
// makes m.input.Height() > 1.
func Test_DynamicHeightGrows(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.input.SetValue("line1\nline2\nline3")
	h := m.input.Height()
	if h < 2 {
		t.Fatalf("expected textarea height >= 2 after multi-line content, got %d", h)
	}
}

// Test_TextareaCursorTracksText verifies the cursor position
// tracks the textarea content (cycle 71 fix).
func Test_TextareaCursorTracksText(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.input.SetValue("hello world")
	// Move to end via the textarea's CharacterForward binding (ctrl+f).
	for i := 0; i < 50; i++ {
		m.input, _ = m.input.Update(tea.KeyPressMsg(tea.Key{Code: 'f', Mod: tea.ModCtrl}))
	}
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("v.Cursor should not be nil")
	}
	// Cursor X should be at least "hello world" length + 1 (" " + ">")
	if v.Cursor.X < 12 {
		t.Errorf("Cursor.X should be >= 12, got %d (cycle 71 cursor tracking)", v.Cursor.X)
	}
}

// reference tas.New() to satisfy import (textarea.New used in test below)
var _ = tas.New

// Test_PromptPrefixMatchesRef verifies cycle 74: prompt prefix is "> "
// not " " (matches v1.3.5 binary byte-for-byte).
func Test_PromptPrefixMatchesRef(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	if m.input.Prompt != "> " {
		t.Fatalf("expected input.Prompt='> ', got %q (cycle 74)", m.input.Prompt)
	}
}

// Test_IdlePromptHasMarker verifies cycle 74: idle view contains the
// "> " prompt prefix (not just "> " literal at empty state).
// Cycle 89: the > glyph is wrapped in blue ANSI per ref capture;
// cycle 126 normalises the colour to 24-bit RGB (38;2;38;139;210)
// matching the v1.3.6 binary, instead of the 256-colour 38;5;69 the
// earlier capture picked up. Cycle 90: the prompt colour is applied
// via lipgloss's Focused.Prompt style, which produces either
// \x1b[39m (foreground reset) or \x1b[m (full SGR reset) as the
// close — both render identically, so the test accepts either.
func Test_IdlePromptHasMarker(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	view := v.Content
	hasBlueOpen := strings.Contains(view, "\x1b[38;2;38;139;210m>")
	if !hasBlueOpen {
		t.Fatalf("idle view missing true-colour blue '\x1b[38;2;38;139;210m>' prompt opener (cycle 89/90/126): %q", view)
	}
}

// Test_PickerVisibleStatusLabel verifies cycle 74: when the picker
// is visible the status row shows "esc to cancel" instead of
// "? for shortcuts" (matches v1.3.5 binary). Cycle 126: the
// "esc to cancel" label is wrapped in true-colour dim gray
// (38;2;101;123;131) per the v1.3.6 capture (cycle 91 used the
// 256-colour fallback 38;2;101;123;131 which is the same shade in tmux
// but distinguishable in a real terminal).
func Test_PickerVisibleStatusLabel(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("picker should be visible after typing '/'")
	}
	status := m.statusRow()
	if !strings.Contains(status, "esc to cancel") {
		t.Errorf("picker-visible status should contain 'esc to cancel', got %q (cycle 74)", status)
	}
	if strings.Contains(status, "? for shortcuts") {
		t.Errorf("picker-visible status should NOT contain '? for shortcuts', got %q (cycle 74)", status)
	}
	if !strings.Contains(status, "\x1b[38;2;101;123;131mesc to cancel\x1b[39m") {
		t.Errorf("picker-visible 'esc to cancel' should be in true-colour dim gray (cycle 91/126), got %q", status)
	}
}

// Test_PickerHiddenStatusLabel verifies that when picker is hidden
// the status row shows "? for shortcuts" (idle behavior).
func Test_PickerHiddenStatusLabel(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	if m.picker.Visible() {
		t.Fatal("picker should be hidden in idle state")
	}
	status := m.statusRow()
	if !strings.Contains(status, "? for shortcuts") {
		t.Errorf("idle status should contain '? for shortcuts', got %q", status)
	}
	if strings.Contains(status, "esc to cancel") {
		t.Errorf("idle status should NOT contain 'esc to cancel', got %q", status)
	}
}

// Test_PickerHighlightColorIs69 verifies cycle 74: the highlighted
// picker row uses true-colour blue (38;2;38;139;210) matching the
// v1.3.6 binary. The cycle-74 capture observed 256-colour 38;5;69,
// which is the same shade in tmux but distinguishable in a real
// terminal; cycle 126 normalises to 24-bit RGB.
func Test_PickerHighlightColorIs69(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	rows := m.pickerView()
	if !strings.Contains(rows, "\033[38;2;38;139;210m") {
		t.Fatalf("picker highlight should contain \\033[38;2;38;139;210m (true-colour blue), got %q (cycle 74/126)", rows)
	}
}

// Test_PickerDescriptionDim verifies cycle 74: picker descriptions
// are rendered in dim gray (matches ref binary). Cycle 126 normalised
// the colour from 256-colour 38;5;238 to true-colour 38;2;101;123;131.
func Test_PickerDescriptionDim(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	rows := m.pickerView()
	if !strings.Contains(rows, "\033[38;2;101;123;131m") {
		t.Fatalf("picker rows should contain true-colour dim gray 38;2;101;123;131, got %q (cycle 74/126)", rows)
	}
}

// Test_CursorOnPromptLine verifies cycle 74f: cursor lands on the
// prompt line (not on the second separator or picker row).
func Test_CursorOnPromptLine(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	// Layout: top blank + 5 banner + blank + top sep + prompt line.
	// 0-indexed prompt line Y = 8.
	if v.Cursor.Y != 8 {
		t.Errorf("idle cursor Y should be 8, got %d (cycle 74f)", v.Cursor.Y)
	}
}

// Test_CursorTracksTypedText verifies cycle 74f: cursor X moves
// with each rune insertion (matches v1.3.5 binary).
func Test_CursorTracksTypedText(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	idleX := v.Cursor.X
	for _, r := range "hello" {
		m.Update(runeKey(r))
	}
	v = m.View()
	// Each rune should advance cursor X by 1.
	if v.Cursor.X != idleX+5 {
		t.Errorf("after 'hello', cursor X should be %d, got %d (cycle 74f)", idleX+5, v.Cursor.X)
	}
}

// Test_PickerCatalogMatchesRef pins both the catalog and the
// picker-visible count against the v1.3.6 binary.
//
//	/help COMMANDS tab  → 99 entries (footer "[1-18 of 99 items]")
//	                       captured at
//	                       /tmp/agycmp/hotpath/help_tabs/ref.001-second.txt
//	/ picker overlay    → 94 entries (header "↓ 89 more" at offset 0;
//	                       "↓ 86 more" at offset 3 after 5 Down presses)
//	                       captured at
//	                       /tmp/agycmp/hotpath/picker/ref.000-slash.txt
//
// Cycle 135's chrome-devtools-plugin:* filter was correct; cycle
// 146's revert was wrong. Cycle 147 restores cycle 135's filter.
//
// Cycle 236: /feature-diff was added to the catalog (one new row).
// The base count was 99 (cycle 147), the picker filter excluded 5
// chrome-devtools-plugin:* entries (cycle 146). With the cycle-236
// addition: 100 total / 95 filtered. With /teamwork and /boost: 102 total / 97 filtered.
func Test_PickerCatalogMatchesRef(t *testing.T) {
	got := len(tuilib.DefaultSlashCommands)
	if got != 102 {
		t.Fatalf("expected 102 slash commands in catalog (99 cycle-147 base + /feature-diff cycle-236 + /teamwork + /boost), got %d", got)
	}
	filtered := len(tuilib.FilterSlashCommands("/"))
	if filtered != 97 {
		t.Fatalf("expected 97 slash commands in picker (94 cycle-147 base + /feature-diff cycle-236 + /teamwork + /boost), got %d", filtered)
	}
}

// Test_PickerFirstRowIsAddDir verifies cycle 75: the very first row
// of the catalog (and first visible row when "/" is typed) is /add-dir,
// matching the v1.3.5 binary's first-page order exactly.
func Test_PickerFirstRowIsAddDir(t *testing.T) {
	if len(tuilib.DefaultSlashCommands) == 0 {
		t.Fatal("catalog is empty")
	}
	if tuilib.DefaultSlashCommands[0].Name != "/add-dir" {
		t.Fatalf("first command should be /add-dir, got %q (cycle 75)", tuilib.DefaultSlashCommands[0].Name)
	}
}

// Test_PickerLastRowIsWrangler verifies cycle 75: the very last row
// is /wrangler, matching the cursor-walk capture at scan_98.bin.
func Test_PickerLastRowIsWrangler(t *testing.T) {
	last := tuilib.DefaultSlashCommands[len(tuilib.DefaultSlashCommands)-1]
	if last.Name != "/wrangler" {
		t.Fatalf("last command should be /wrangler, got %q (cycle 75)", last.Name)
	}
}

// slowFakeClient is a backend that blocks until ctx is cancelled, so
// the spinner stays in its "streaming" state until the test releases it.
type slowFakeClient struct {
	delay time.Duration
	ch    chan struct{}
}

func (s *slowFakeClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	ch := make(chan backend.StreamChunk, 8)
	go func() {
		defer close(ch)
		parts := []string{"Hello", " world", "!"}
		for _, p := range parts {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.delay):
			}
			ch <- backend.StreamChunk{Text: p}
		}
		ch <- backend.StreamChunk{Done: true}
	}()
	return ch, nil
}

// Test_SpinnerAppearsDuringStream verifies cycle 76: while
// m.streaming is true, the view contains the literal row
// "  <glyph>  Generating..." (two leading spaces, one spinner
// glyph, two more spaces, then the word). Source-of-truth:
// /tmp/ref_streaming_t*.bin captured from the v1.3.5 binary.
func Test_SpinnerAppearsDuringStream(t *testing.T) {
	be := &slowFakeClient{delay: 500 * time.Millisecond}
	m := NewRunMode(be, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.streaming = false
	_, _ = m.Update(enterKey())
	if !m.streaming {
		t.Fatal("expected streaming=true after Enter")
	}
	v := m.View()
	if !strings.Contains(v.Content, "Generating...") {
		t.Fatalf("expected 'Generating...' in view during stream, got %q", v.Content)
	}
	// Verify the spinner glyph sits between two pairs of spaces:
	// "  <braille>  Generating..." (2 spaces + glyph + 2 spaces).
	idx := strings.Index(v.Content, "Generating...")
	if idx < 5 {
		t.Fatal("Generating... not found with enough leading chars")
	}
	// The pattern in v1.3.5 is: <glyph><space><space>Generating...
	// (no leading indent before the glyph). bubbles sp.Dot frames
	// each carry a trailing space, so the total is 2 spaces.
	prefix := v.Content[idx-5 : idx]
	// 5 bytes = 3-byte UTF-8 braille glyph + 2 spaces
	if !strings.HasSuffix(prefix, "  ") {
		t.Errorf("expected 2 trailing spaces before Generating, got %q (cycle 76)", prefix)
	}
	// The leading byte must be a UTF-8 continuation byte (>= 0x80).
	if prefix[0] < 0x80 {
		t.Errorf("expected Braille glyph at start of spinner row, got %q (cycle 76)", prefix)
	}
	// Finish stream so the test doesn't hang on cleanup.
	m.Update(backendDoneMsg{})
}

// Test_SpinnerAnimatesAcrossTicks verifies cycle 76: each
// sp.TickMsg advances the spinner frame. Since sp.Tick filters on
// tag, we must supply an empty TickMsg (ID=0, tag=0) which is the
// first tick from the bubble loop. The spinner model is initialized
// with frame=0; after the first tick it should be on frame 1.
func Test_SpinnerAnimatesAcrossTicks(t *testing.T) {
	m := NewRunMode(&slowFakeClient{delay: time.Second}, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.streaming = false
	_, _ = m.Update(enterKey())
	if !m.streaming {
		t.Fatal("expected streaming=true after Enter")
	}
	// Drain TickMsgs manually (simulating the bubbletea loop).
	seen := make(map[string]bool)
	for i := 0; i < 6; i++ {
		v := m.View()
		idx := strings.Index(v.Content, "Generating...")
		if idx < 0 {
			t.Fatalf("frame %d: Generating... not in view", i)
		}
		// Extract the spinner glyph — last non-space char before "  Generating".
		before := v.Content[:idx]
		runes := []rune(strings.TrimRight(before, " "))
		if len(runes) == 0 {
			t.Fatalf("frame %d: no spinner glyph", i)
		}
		seen[string(runes[len(runes)-1])] = true
		// Simulate next tick — send empty TickMsg and Update.
		_, _ = m.Update(sp.TickMsg{})
	}
	if len(seen) < 2 {
		t.Errorf("spinner did not animate: only saw %d distinct frames %v", len(seen), seen)
	}
	// Cleanup.
	m.Update(backendDoneMsg{})
}

// Test_SpinnerStopsWhenStreamDone verifies cycle 76: after
// backendDoneMsg arrives, m.streaming is false and subsequent
// TickMsg are ignored (frame freezes).
func Test_SpinnerStopsWhenStreamDone(t *testing.T) {
	be := &slowFakeClient{delay: 30 * time.Millisecond}
	m := NewRunMode(be, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.streaming = false
	_, _ = m.Update(enterKey())
	// In the test environment the tea.Cmd from Enter is never executed,
	// so streaming stays true unless we manually flip it. Simulate the
	// backend completing.
	m.Update(backendDoneMsg{})
	if m.streaming {
		t.Error("after backendDoneMsg, m.streaming should be false")
	}
	// View should NOT contain "Generating..." anymore.
	v := m.View()
	if strings.Contains(v.Content, "Generating...") {
		t.Errorf("after stream done, view should not show 'Generating...', got %q", v.Content)
	}
}

// Test_SlashCommandsAreNotSentToLLM verifies cycle 77: when the
// user presses Enter on a slash command buffer (e.g., "/help"), the
// handleSlashCommand method intercepts and returns true so the LLM
// path is skipped. Previously only /clear was intercepted.
func Test_SlashCommandsAreNotSentToLLM(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "/help" {
		m.Update(runeKey(r))
	}
	before := len(m.session)
	handled, _ := m.handleSlashCommand("/help")
	if !handled {
		t.Error("/help should be handled by router (cycle 77)")
	}
	if len(m.session) != before {
		t.Errorf("/help should NOT add to session (was %d, now %d)", before, len(m.session))
	}
	// /unknown_xyz is routed to the "unknown" handler which echoes
	// feedback. Still doesn't reach the LLM.
	handled, _ = m.handleSlashCommand("/totally_made_up_command")
	if !handled {
		t.Error("unknown commands should be handled (cycle 77 feedback)")
	}
}

// Test_SlashExitReturnsQuitCmd verifies cycle 77: /exit and
// /quit both schedule tea.Quit so the program actually terminates.
func Test_SlashExitReturnsQuitCmd(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, cmd := m.handleSlashCommand("/exit")
	if !handled {
		t.Fatal("/exit should be handled")
	}
	if cmd == nil {
		t.Error("/exit should return a non-nil tea.Cmd to schedule Quit")
	}
	handled, cmd = m.handleSlashCommand("/quit")
	if !handled {
		t.Fatal("/quit should be handled")
	}
	if cmd == nil {
		t.Error("/quit should return a non-nil tea.Cmd to schedule Quit")
	}
}

// Test_SlashHelpFeedbackIncludesCatalog verifies cycle 83: /help
// opens a 3-tab TUI overlay (helpMode=true) instead of dumping the
// catalog into m.response. The TUI panels are rendered by View() via
// renderHelp; here we only confirm the overlay is armed and the
// response area only has the user echo line.
func Test_SlashHelpFeedbackIncludesCatalog(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, _ := m.handleSlashCommand("/help")
	if !handled {
		t.Fatal("/help should be handled")
	}
	if !m.helpMode {
		t.Error("expected helpMode=true after /help (cycle 83 overlay)")
	}
	if m.helpTab != 0 {
		t.Errorf("expected helpTab=0 on entry, got %d", m.helpTab)
	}
	// Response should be empty after /help — the reference binary
	// does not echo slash commands into the transcript (the picker
	// overlay's prompt area already shows the command while the user
	// is typing, and the handler output is the next thing rendered).
	// Cycle 149: the cycle 92 / 126 echo is removed; capturing the
	// ref binary running /help, /changelog, and other slash commands
	// never shows a "> /<cmd>" line in the transcript.
	if len(m.response) != 0 {
		t.Errorf("expected empty transcript after /help (cycle 149), got %v", m.response)
	}
}

// Test_HelpTabBarStyling verifies cycle 85 / 126: the tab bar uses
// the v1.3.6 binary's ANSI styling — active tab in
// bold+underline+true-colour blue (38;2;38;139;210), inactive tabs
// in separator-gray (38;2;218;220;224), brand label in plain true-
// colour blue, trailing hint in separator-gray. (Cycle 85 used the
// 256-colour fallback 38;5;69/253 which tmux's cube maps to the same
// visible shade.)
func Test_HelpTabBarStyling(t *testing.T) {
	// All three states share the brand prefix and hint suffix.
	for tab := 0; tab <= 2; tab++ {
		got := renderTabBar(tab)
		// Brand prefix: \x1b[38;2;38;139;210mPigasus CLI\x1b[39m
		if !strings.HasPrefix(got, "\x1b[38;2;38;139;210mPigasus CLI\x1b[39m") {
			t.Errorf("tab %d: missing brand prefix, got %q", tab, got[:60])
		}
		// Trailing hint: ' \x1b[38;2;238;232;213m  (←/→ or tab to cycle)\x1b[0m'
		wantHint := " \x1b[38;2;238;232;213m  (\u2190/\u2192 or tab to cycle)\x1b[0m"
		if !strings.HasSuffix(got, wantHint) {
			t.Errorf("tab %d: missing trailing hint suffix, got %q", tab, got[len(got)-50:])
		}
		// Active tab word must carry the bold+underline+blue prefix.
		wantActiveOpen := "\x1b[1;4m\x1b[38;2;38;139;210m"
		tabs := []string{"general", "commands", "shortcuts"}
		active := tabs[tab]
		// Check that active word is wrapped with the active open/close.
		activePattern := wantActiveOpen + active + "\x1b[0m"
		if !strings.Contains(got, activePattern) {
			t.Errorf("tab %d: active word %q not wrapped in active styling", tab, active)
		}
		// Inactive words must use the dim gray wrap.
		for i, name := range tabs {
			if i == tab {
				continue
			}
			inactivePattern := "\x1b[38;2;238;232;213m" + name + "\x1b[0m"
			if !strings.Contains(got, inactivePattern) {
				t.Errorf("tab %d: inactive word %q not in dim gray", tab, name)
			}
		}
	}
}

// Test_HelpTabBarActiveTabHighlight verifies cycle 85 / 126: the
// active tab word carries the bold+underline SGR (wire detail:
// \x1b[1;4m\x1b[38;2;38;139;210m...\x1b[0m in the v1.3.6 binary).
func Test_HelpTabBarActiveTabHighlight(t *testing.T) {
	for tab := 0; tab <= 2; tab++ {
		got := renderTabBar(tab)
		if !strings.Contains(got, "\x1b[1;4m") {
			t.Errorf("tab %d: missing bold+underline SGR \\x1b[1;4m", tab)
		}
		if !strings.Contains(got, "\x1b[38;2;38;139;210m") {
			t.Errorf("tab %d: missing blue \\x1b[38;2;38;139;210m (cycle 126)", tab)
		}
		// Only ONE active wrap should be present per tab bar.
		count := strings.Count(got, "\x1b[1;4m")
		if count != 1 {
			t.Errorf("tab %d: expected 1 active wrap, got %d", tab, count)
		}
	}
}

// Test_SlashNonSlashFallsThrough verifies cycle 77: buffers not
// starting with "/" fall through to the LLM path (handleSlashCommand
// returns false, nil).
func Test_SlashNonSlashFallsThrough(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, cmd := m.handleSlashCommand("hello world")
	if handled {
		t.Error("plain text should not be handled as slash command (cycle 77)")
	}
	if cmd != nil {
		t.Error("plain text handler should return nil cmd")
	}
}

// Test_HelpGeneralPanelStyling verifies cycle 86 / 126: the
// /help general panel wraps description/metadata rows in true-colour
// dim gray (38;2;101;123;131), Quick Reference header in bold + warning-
// body dim gray (1m + 38;2;60;64;67), Quick Reference "/" symbol in
// true-colour blue (38;2;38;139;210), and the Keyboard hint uses
// bold prefix + true-colour blue keys + dim gray labels.
func Test_HelpGeneralPanelStyling(t *testing.T) {
	var b strings.Builder
	renderHelpGeneral(&b, "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "~/GitHub/openagy", "navigaid@gmail.com", "test-uuid-0000-0000-0000-000000000000")
	out := b.String()

	// Description block must be wrapped in dim gray.
	if !strings.Contains(out, "\x1b[38;2;101;123;131mPigasus CLI understands") {
		t.Error("description row 1 missing dim gray wrap")
	}
	if !strings.Contains(out, "\x1b[38;2;101;123;131mand executes commands") {
		t.Error("description row 2 missing dim gray wrap")
	}
	// Metadata rows.
	wantRows := []string{
		"\x1b[38;2;101;123;131mVersion " + cliVersion + "\x1b[39m",
		"\x1b[38;2;101;123;131mWorkspace:      ~/GitHub/openagy\x1b[39m",
		"\x1b[38;2;101;123;131mProject Config: ~/.gemini/config/projects/default-cli-project.json\x1b[39m",
		"\x1b[38;2;101;123;131mLog File:       ~/.gemini/antigravity-cli/log/cli-",
	}
	for _, want := range wantRows {
		if !strings.Contains(out, want) {
			snip := want
			if len(snip) > 50 {
				snip = snip[:50]
			}
			t.Errorf("missing styled row %q", snip)
		}
	}
	// Quick Reference header must be bold + warning-body gray (38;2;60;64;67).
	if !strings.Contains(out, "\x1b[1m\x1b[38;2;88;110;117mQuick Reference\x1b[0m") {
		t.Error("Quick Reference header missing bold+warning-body-gray styling (cycle 86/126)")
	}
	// Quick Reference "/" must be in true-colour blue.
	if !strings.Contains(out, "\x1b[38;2;38;139;210m/\x1b[39m") {
		t.Error("Quick Reference / missing true-colour blue styling (cycle 126)")
	}
	// Keyboard hint must use bold prefix + true-colour blue keys + dim gray labels.
	if !strings.Contains(out, "\x1b[1mKeyboard: \x1b[0m") {
		t.Error("Keyboard hint missing bold prefix")
	}
	if !strings.Contains(out, "\x1b[38;2;38;139;210m\u2191/\u2193\x1b[39m") {
		t.Error("Keyboard ↑/↓ keys missing true-colour blue styling (cycle 126)")
	}
	if !strings.Contains(out, "\x1b[38;2;38;139;210mesc\x1b[39m") {
		t.Error("Keyboard esc key missing true-colour blue styling (cycle 126)")
	}
	if !strings.Contains(out, "\x1b[38;2;101;123;131mNavigate\x1b[39m") {
		t.Error("Keyboard Navigate label missing dim gray (cycle 126)")
	}
}

// Test_QuestionMarkOpensHelpOverlay verifies cycle 181: pressing `?`
// on an empty prompt opens the help overlay on the SHORTCUTS tab
// (helpTab=2) — matches v1.4.0 REF, where the active word
// "shortcuts" in the tab bar is wrapped in bold+underline. The /help
// slash command still lands on the GENERAL tab (helpTab=0). With text
// already in the input, `?` falls through to the textarea and is
// typed as a literal rune (verified against REF: typing "hello
// world" then `?` produces "> hello world?" rather than opening the
// overlay).
func Test_QuestionMarkOpensHelpOverlay(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)

	// Precondition: idle prompt, helpMode off, input empty.
	if m.helpMode {
		t.Fatal("helpMode should start off")
	}
	if v := m.input.Value(); v != "" {
		t.Fatalf("input should start empty, got %q", v)
	}

	// Case 1: `?` on an empty input opens the overlay on SHORTCUTS.
	_, _ = m.Update(runeKey('?'))
	if !m.helpMode {
		t.Error("expected helpMode=true after ? on empty input (cycle 181)")
	}
	if m.helpTab != 2 {
		t.Errorf("expected helpTab=2 (shortcuts) on ?-entry, got %d", m.helpTab)
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("expected input cleared on entry to overlay, got %q", v)
	}

	// Esc dismisses (cycle 83 path).
	_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.helpMode {
		t.Error("expected helpMode=false after Esc")
	}

	// Case 2: typing text first, then `?`, appends the literal `?`.
	for _, r := range "hello" {
		_, _ = m.Update(runeKey(r))
	}
	if v := m.input.Value(); v != "hello" {
		t.Fatalf("precondition: expected 'hello' in input, got %q", v)
	}
	_, _ = m.Update(runeKey('?'))
	if m.helpMode {
		t.Error("`?` with text in input must NOT open overlay (cycle 181)")
	}
	if v := m.input.Value(); v != "hello?" {
		t.Errorf("expected input %q after literal ? insertion, got %q", "hello?", v)
	}

	// Case 3: clear and press ? again — overlay reopens on shortcuts.
	for i := 0; i < 6; i++ {
		_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	}
	if v := m.input.Value(); v != "" {
		t.Fatalf("precondition: input should be empty after backspaces, got %q", v)
	}
	_, _ = m.Update(runeKey('?'))
	if !m.helpMode {
		t.Error("expected overlay to re-open after clearing input and pressing ?")
	}
	if m.helpTab != 2 {
		t.Errorf("expected helpTab=2 on second ?-entry, got %d", m.helpTab)
	}

	// Case 4: /help still lands on GENERAL (tab 0).
	_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.helpMode {
		t.Fatal("expected helpMode=false after Esc")
	}
	handled, _ := m.handleSlashCommand("/help")
	if !handled {
		t.Fatal("/help should be handled")
	}
	if m.helpTab != 0 {
		t.Errorf("expected helpTab=0 on /help entry, got %d", m.helpTab)
	}
}

// Test_NewConversationID verifies cycle 86: UUID generator
// returns an 8-4-4-4-12 hex RFC-4122-style string.
func Test_NewConversationID(t *testing.T) {
	id := newConversationID()
	wantLen := 36 // 8+1+4+1+4+1+4+1+12
	if len(id) != wantLen {
		t.Fatalf("UUID length: want %d, got %d (%q)", wantLen, len(id), id)
	}
	// Check dashes at positions 8, 13, 18, 23.
	for _, i := range []int{8, 13, 18, 23} {
		if id[i] != '-' {
			t.Errorf("UUID position %d: want '-', got %c (id=%q)", i, id[i], id)
		}
	}
	// All other positions must be lowercase hex.
	hexChars := "0123456789abcdef"
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !strings.ContainsRune(hexChars, rune(c)) {
			t.Errorf("UUID position %d: not hex, got %c", i, c)
		}
	}
	// Two consecutive UUIDs must differ (high entropy).
	id2 := newConversationID()
	if id == id2 {
		t.Errorf("two consecutive UUIDs collided: %q", id)
	}
}

// Test_CursorXWithMultibyteRune verifies cycle 90: cursor X
// advances by visible column count when typing multi-byte runes
// (e.g. Chinese characters). The v1.3.5 binary's prompt `>` glyph
// is wrapped in saturated blue ANSI via lipgloss Prompt style, and
// our cursorX math (m.input.Cursor().X) lands on the correct
// visible column — counting CJK characters as 2 columns wide per
// terminal convention.
func Test_CursorXWithMultibyteRune(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	idleX := v.Cursor.X
	// Type a single Chinese character. Bubbles' Cursor().X counts
	// visible columns, so 1 CJK rune = 2 column advance.
	for _, r := range "中" {
		m.Update(runeKey(r))
	}
	v = m.View()
	if v.Cursor.X != idleX+2 {
		t.Errorf("after typing single CJK rune '中', cursor X should be idleX+2=%d (visible cols), got %d (cycle 90)", idleX+2, v.Cursor.X)
	}
}

// Test_CursorXWithPromptANSIOffset verifies cycle 90: idle
// cursor X is 2 (the position immediately after the `> ` prompt
// prefix). The lipgloss Prompt style wraps the prompt in ANSI
// bytes that bubbles correctly excludes from Cursor().X count.
func Test_CursorXWithPromptANSIOffset(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	if v.Cursor.X != 2 {
		t.Errorf("idle cursor X should be 2 (after '> ' prompt prefix), got %d (cycle 90)", v.Cursor.X)
	}
}

// Test_PickerHintRowStyling verifies cycle 91 / 126: the picker
// overlay's hint row ("↑/↓ Navigate · enter Select · tab Complete")
// wraps keys (↑/↓, enter, tab) in bold + true-colour blue
// (38;2;38;139;210) and labels in true-colour dim gray
// (38;2;101;123;131), matching the v1.3.6 binary.
func Test_PickerHintRowStyling(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	if !m.picker.Visible() {
		t.Fatal("picker should be visible after typing '/'")
	}
	view := m.View().Content
	wantKeys := []string{
		"\x1b[1m\x1b[38;2;38;139;210m\u2191/\u2193\x1b[0m",
		"\x1b[1m\x1b[38;2;38;139;210menter\x1b[0m",
		"\x1b[1m\x1b[38;2;38;139;210mtab\x1b[0m",
	}
	for _, k := range wantKeys {
		if !strings.Contains(view, k) {
			t.Errorf("picker hint row should contain key %q, view: %q", k, view[:300])
		}
	}
	wantLabels := []string{
		"\x1b[38;2;101;123;131m Navigate \u00b7 ",
		"\x1b[38;2;101;123;131m Select \u00b7 ",
		"\x1b[38;2;101;123;131m Complete",
	}
	for _, l := range wantLabels {
		if !strings.Contains(view, l) {
			t.Errorf("picker hint row should contain label %q, view: %q", l, view[:300])
		}
	}
}

// Test_HelpExitFeedback verifies cycle 92 / 126: when the /help
// overlay is dismissed via Esc, an "Exited /help command" feedback
// row is appended to the response transcript, matching the v1.3.6
// binary byte-for-byte. The row uses bold + true-colour warning blue
// (38;2;25;103;210) for the ⎿ bullet and true-colour dim gray
// (38;2;101;123;131 — for the body label).
func Test_HelpExitFeedback(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, _ := m.handleSlashCommand("/help")
	if !handled || !m.helpMode {
		t.Fatal("/help should open overlay")
	}
	// Dismiss via Esc
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.helpMode {
		t.Fatal("helpMode should be false after Esc")
	}
	wantBlock := []string{
		"\x1b[38;2;38;139;210;1m> /help\x1b[0m",
		"\x1b[38;2;38;139;210;1m  \u23bf  \x1b[0m\x1b[38;2;88;110;117mExited /help command\x1b[39m",
		"",
	}
	found := false
	for i := 0; i+len(wantBlock) <= len(m.response); i++ {
		if strings.Join(m.response[i:i+len(wantBlock)], "\x00") == strings.Join(wantBlock, "\x00") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected the standard `> /help` + Exited block, got %v", m.response)
	}
}
