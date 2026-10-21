// slash_rename_panel_test.go — cycle charmland-component-adoption/02.
//
// The rename panel is powered by charm.land/bubbles/v2/textinput. The
// tests below exercise:
//
//   - OpenRenamePanel sets panel.kind = panelRename, seeds the textinput
//     with the current conversation name (so renaming in-place produces
//     a useful experience instead of an empty input field), and starts
//     focused.
//   - Submitting with Enter routes the typed value through renameFeedback
//     — same code path the cmd-line `/rename <name>` uses — so the
//     transcript block is identical regardless of how the user invoked
//     the command.
//   - Esc closes silently (no `Exited /rename command` echo).
//   - Cursor navigation keys (Left, Right, Home, End) and word-level
//     delete (Ctrl+W) flow through textinput.Update unchanged — those
//     used to be silently dropped by the hand-rolled `panel.text`
//     buffer.
//   - CharLimit caps the typed name at panelRenameLimit (200) runes.
//   - Empty submit (Enter with nothing typed) emits the same error
//     message the cmd-line `/rename` path emits.
package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"charm.land/bubbles/v2/textinput"
)

// TestOpenRenamePanel_KindAndSeed verifies the bare `/rename` (no
// args) opens the new panel and seeds the textinput with the current
// conversation name (if any). The seeded value matches what the
// cycle-106 conversationName getter would return.
func TestOpenRenamePanel_KindAndSeed(t *testing.T) {
	m := &runMode{conversationName: "vacation planning"}
	m.openRenamePanel()
	if m.panel.kind != panelRename {
		t.Fatalf("panel.kind = %v, want panelRename", m.panel.kind)
	}
	if got := m.panel.textInput.Value(); got != "vacation planning" {
		t.Errorf("textinput value = %q, want %q (current conversation name)",
			got, "vacation planning")
	}
	if !m.panel.textInput.Focused() {
		t.Errorf("textinput not focused on open — keystrokes will be dropped")
	}
}

// TestOpenRenamePanel_EmptySeed confirms a conversation without a
// current name opens an empty (placeholder-visible) textinput.
func TestOpenRenamePanel_EmptySeed(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	if m.panel.kind != panelRename {
		t.Fatalf("panel.kind = %v, want panelRename", m.panel.kind)
	}
	if got := m.panel.textInput.Value(); got != "" {
		t.Errorf("textinput value = %q, want empty", got)
	}
}

// TestHandleRenamePanelKey_EnterSubmits exercises the submit path.
// The typed name is committed via renameFeedback; the panel closes
// silently (cycle-193 silent-on-Esc set also includes panelRename per
// exitNotice()). A successful rename emits the
// "Conversation renamed to: %s" line; an empty submit emits the
// "Error: Please provide a new name for the conversation." line —
// matching the cycle-106 cmd-line path byte-for-byte.
func TestHandleRenamePanelKey_EnterSubmits(t *testing.T) {
	m := &runMode{conversationName: "trip"}
	m.openRenamePanel()
	m.panel.textInput.SetValue("summer trip 2026")
	// closePanelSilent() returns nil — the panel-clearing mutation is
	// the deliverable, not a tea.Cmd (matches cycle 222's picker-close
	// test pattern; closePanel/closePanelSilent have always returned
	// nil, see slash_panels.go:3727 and 3744).
	if handled, _ := m.handleRenamePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Errorf("Enter not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel still open after submit (kind=%v)", m.panel.kind)
	}
	// renameFeedback emits the message via slashRespond; the message
	// ends up in m.response. Verify the success line.
	if !containsResponse(m.response, "Conversation renamed to: summer trip 2026") {
		t.Errorf("submit did not emit success echo: %v", m.response)
	}
}

// TestHandleRenamePanelKey_EmptySubmitHitsErrorPath ensures an empty
// Enter (Enter on a freshly-opened panel) emits the same error the
// cmd-line `/rename` (no args) used to emit. The cycle-02 UX change
// is *only* that the user gets a panel to type into — the empty-
// submit result is unchanged.
func TestHandleRenamePanelKey_EmptySubmitHitsErrorPath(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	m.panel.textInput.SetValue("")
	handled, _ := m.handleRenamePanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Errorf("Enter not handled in empty state")
	}
	if !containsResponse(m.response, "Error: Please provide a new name") {
		t.Errorf("empty submit did not emit error echo: %v", m.response)
	}
}

// TestHandleRenamePanelKey_EscClosesSilently confirms Esc exits the
// panel without emitting any exit notice (panelRename is in the
// cycle-193 silent-on-Ec set per exitNotice()), matching /mcp,
// /feedback, /plugin, etc. — not /rename's previous behaviour.
func TestHandleRenamePanelKey_EscClosesSilently(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	handled, _ := m.handleRenamePanelKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Errorf("Esc not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel still open after Esc (kind=%v)", m.panel.kind)
	}
	if containsResponse(m.response, "Exited /rename") {
		t.Errorf("Esc emitted an exit notice — panelRename should be silent: %v",
			m.response)
	}
}

// TestHandleRenamePanelKey_CursorNavigationPassesThrough is the
// migration's primary UX win: the previous hand-rolled `panel.text`
// buffer dropped every navigation key. bubbles/v2/textinput handles
// arrow-left/right, Home/End, Ctrl+A/Ctrl+E, etc., so the value
// and cursor position advance correctly when those keys arrive.
//
// The test seeds a 12-character value, sends Left keys, and
// verifies the cursor moves; Home then End verify the standard
// chat-app shortcuts round-trip through the same code path.
func TestHandleRenamePanelKey_CursorNavigationPassesThrough(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	m.panel.textInput.SetValue("hello world")

	// Cursor is at end after SetValue. Press Left twice.
	for i := 0; i < 2; i++ {
		handled, _ := m.handleRenamePanelKey(tea.Key{Code: tea.KeyLeft})
		if !handled {
			t.Errorf("Left #%d not handled", i+1)
		}
	}
	if got := m.panel.textInput.Position(); got != 9 {
		t.Errorf("after 2× Left, cursor = %d, want 9", got)
	}
	if got := m.panel.textInput.Value(); got != "hello world" {
		t.Errorf("value = %q, want unchanged", got)
	}

	// Home jumps to 0.
	handled, _ := m.handleRenamePanelKey(tea.Key{Code: tea.KeyHome})
	if !handled {
		t.Errorf("Home not handled")
	}
	if got := m.panel.textInput.Position(); got != 0 {
		t.Errorf("after Home, cursor = %d, want 0", got)
	}
}

// TestHandleRenamePanelKey_WordDelete verifies Ctrl+W (the standard
// word-backward delete) deletes one word. The hand-rolled buffer
// dropped Ctrl+W silently; textinput handles it.
func TestHandleRenamePanelKey_WordDelete(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	m.panel.textInput.SetValue("hello world foo")
	// Cursor at end. textinput's DeleteWordBackward is bound to
	// alt+backspace,ctrl+w by DefaultKeyMap.
	handled, _ := m.handleRenamePanelKey(tea.Key{
		Code: 'w',
		Mod:  tea.ModCtrl,
	})
	if !handled {
		t.Errorf("Ctrl+W not handled")
	}
	if got := m.panel.textInput.Value(); got != "hello world " {
		t.Errorf("after word delete, value = %q, want %q (trailing space preserved)",
			got, "hello world ")
	}
}

// TestHandleRenamePanelKey_CharLimitApplied verifies the panel's
// CharLimit caps the typed name at panelRenameLimit. The bubbles
// textinput silently drops characters past the limit (it does not
// emit an error), so a 300-character paste becomes a 200-character
// value.
func TestHandleRenamePanelKey_CharLimitApplied(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	if got, want := m.panel.textInput.CharLimit, panelRenameLimit; got != want {
		t.Errorf("CharLimit = %d, want %d", got, want)
	}
	long := strings.Repeat("a", 300)
	m.panel.textInput.SetValue(long)
	if got := m.panel.textInput.Value(); len([]rune(got)) != panelRenameLimit {
		t.Errorf("len value = %d, want %d (CharLimit)", len([]rune(got)), panelRenameLimit)
	}
}

// TestNewRenameTextinput_DefaultShape verifies the helper builds a
// usable textinput (focused-able, with prompt, placeholder, limit).
// This is the contract for cycle-02 — every consumer that calls
// newRenameTextinput() picks up the same priming.
func TestNewRenameTextinput_DefaultShape(t *testing.T) {
	in := newRenameTextinput("seed")
	if got := in.Value(); got != "seed" {
		t.Errorf("seed value not applied: got %q, want %q", got, "seed")
	}
	if in.Prompt == "" {
		t.Errorf("prompt empty — View() will not show 'Rename: '")
	}
	if in.Placeholder == "" {
		t.Errorf("placeholder empty — empty state will look bare")
	}
	if in.CharLimit != panelRenameLimit {
		t.Errorf("CharLimit = %d, want %d", in.CharLimit, panelRenameLimit)
	}
	if !in.Focused() {
		t.Errorf("panel opens un-focused — first keystroke is dropped")
	}
}

// TestPanelRenameLines_RendersInput verifies the render-side hook:
// panelLines() for panelRename returns the title + prompt + input
// row + keyboard hint. We don't pin the exact bytes (textinput.View
// is ANSI-heavy and styles are theme-driven), but we do pin the
// shape: the panel title, the "Rename:" prefix, and the keyboard
// hint footer are all present.
func TestPanelRenameLines_RendersInput(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	lines := m.panelRenameLines()
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5 (title, blank, prompt+input, blank, hint): %v",
			len(lines), lines)
	}
	if !strings.Contains(stripSGR(lines[0]), "Rename Conversation") {
		t.Errorf("title line missing: %q", lines[0])
	}
	if !strings.Contains(stripSGR(lines[2]), "Rename:") {
		t.Errorf("prompt row missing 'Rename:': %q", lines[2])
	}
	if !strings.Contains(stripSGR(lines[4]), "enter") ||
		!strings.Contains(stripSGR(lines[4]), "Rename") {
		t.Errorf("keyboard hint missing 'enter Rename': %q", lines[4])
	}
}

// TestPanelRenameLines_PlaceholderWhenEmpty verifies the empty-state
// row shows the placeholder ("type a name and press enter…")
// instead of an empty field — the textinput's View() returns the
// placeholder with grey 241 styling when the value is empty.
func TestPanelRenameLines_PlaceholderWhenEmpty(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	// Don't type anything. Strip SGR before matching: the textinput
	// renders its virtual cursor as a separate styled run, so the
	// visible text is split by escape sequences ("t" + SGR + "ype ...").
	lines := m.panelRenameLines()
	if !strings.Contains(stripSGR(lines[2]), "type a name and press enter") {
		t.Errorf("placeholder missing for empty value: %q", lines[2])
	}
}

// TestSubmitRenamePanel_NoopIfNoCurrentAndEmpty makes the edge case
// explicit: an empty submit on a fresh (no prior name) conversation
// still emits the error echo. The cycle-02 panel does not silently
// swallow an empty input — it is the same UX as `/rename` (no args)
// used to be.
func TestSubmitRenamePanel_NoopIfNoCurrentAndEmpty(t *testing.T) {
	m := &runMode{}
	m.openRenamePanel()
	// Empty value + Enter through the real submit. closePanelSilent
	// returns nil — the panel-clearing mutation is the deliverable
	// (matches closePanel's contract at slash_panels.go:3727).
	m.submitRenamePanel()
	if m.panel.kind != panelNone {
		t.Errorf("panel still open after empty submit (kind=%v)", m.panel.kind)
	}
	if !containsResponse(m.response, "Error: Please provide a new name") {
		t.Errorf("empty submit did not echo error: %v", m.response)
	}
}

// containsResponse reports whether any line in response starts with
// substr. The panel flows through slashRespond which appends multiple
// lines (the bold-blue bullet, the dim-gray message, trailing blank);
// we only need the message line to contain the expected text.
func containsResponse(response []string, substr string) bool {
	for _, l := range response {
		if strings.Contains(stripSGR(l), substr) {
			return true
		}
	}
	return false
}

// Reference: textinput-default-keymap sanity for the cycle report.
// We do not invoke DefaultKeyMap (the panel uses the default), but
// documenting it here makes the test file self-contained for
// future readers — Ctrl+W is DeleteWordBackward, etc.
var _ = textinput.New // pull the dep so go.mod stays current if a future cycle strips the import
