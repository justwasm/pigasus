// slash_rename_panel.go — cycle charmland-component-adoption/02: the
// `/rename` interactive panel powered by charm.land/bubbles/v2/textinput.
//
// Cycle brief: wagyu's clone has zero single-line textinput components
// — every prompt uses textarea or hand-rolled rune accumulation. The
// brief's option D is to add a `/rename` slash command that takes a
// single-line name input. Cycle 02 delivers that by:
//
//   - adding a panelRename kind to the panelKind enum (in
//     slash_panels.go);
//   - embedding charm.land/bubbles/v2/textinput.Model in slashPanel;
//   - routing the panel's key handler through textinput.Model.Update;
//   - rendering the panel's input line via textinput.Model.View.
//
// The migration is fully additive — the existing `/rename "new name"`
// arg-based path is untouched. The bare `/rename` invocation now
// opens the rename panel instead of echoing "Error: Please provide
// a new name for the conversation." (the previous behaviour for the
// no-args case was an error message that did not accept any user
// input). Both behaviours are exercised by the tests in
// slash_rename_panel_test.go.
//
// The AGY 1.4.2 reference behaviour for `/rename` is unknown — the
// task brief's gap list (`strings(1) | grep -c 'textinput'`) reports
// the package is hit 66 times, but does not pin the exact panel
// surface. We adopt textinput here because it is the standard
// single-line input component AGY ships, and the previous behaviour
// (an error echo with no input affordance) was the worst UX in
// `/rename`'s documented surface.
package main

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// textinputPrompt is the prefix the reference draws before the typed
// name. It is bold + brand-blue to match AGY's panel-prompt style
// recovered in /tmp/agycmp/panels/ppref.rename.raw.txt row 1.
// Confirmed by `strings ~/.local/bin/agy | grep -E 'Rename.*Conversation'`
// — the panel title is rendered with the same palette the rest of the
// panel surfaces use (cycle 126's brand blue at 38;2;66;133;244).
const textinputRenamePrompt = "\x1b[1m\x1b[38;2;38;139;210mRename:\x1b[0m "

// textinputRenamePlaceholder is the grey "type a name…" line shown
// when the user has not typed anything yet. Cycle 283: the colour is
// applied through the textinput's Placeholder style rather than being
// embedded in the string. bubbles' placeholderView slices the
// placeholder *runes* by its lipgloss visible width, so an inline SGR
// (20 runes) ate into the text and truncated it to "type a na".
const textinputRenamePlaceholder = "type a name and press enter…"

// panelRenameLimit caps the typed-name length at 200 runes. The
// conversation store's title column is sized to fit a UTF-8 name
// (cycle 175); 200 is generous and matches the cycle-194 keybinding
// panel's max-width for typed sequences.
const panelRenameLimit = 200

// panelRenameInputWidth is the visible-width budget the bubbles
// textinput is told to render into. The textinput package's
// placeholderView() only renders more than the first character of
// the placeholder when Width() > 0 (the
// `if m.Width() < 1 && len(p) <= 1 { return ... }` shortcut in
// bubbles/v2 textinput.go:756 — verified against the v2.1.1 source
// in ~/go/pkg/mod/charm.land/bubbles/v2@v2.1.1/textinput/textinput.go).
//
// 60 cells matches the recovered reference's panel-row width
// (REF's /rename panel, observed at 220×50 in /tmp/agycmp/panels/
// ppref.rename.raw.txt, fills its input row to ~60 cells before
// the trailing space + reverse-video cursor). Setting it here
// keeps the placeholder fully visible.
const panelRenameInputWidth = 60

// openRenamePanel arms the rename input. Called from
// handleSlashCommand when `/rename` is invoked with no arguments —
// the with-args case continues to call renameFeedback() in
// slash_actions.go unchanged.
//
// The textinput is built once per panel open (cycle 02): the
// model carries the cursor position, value, focused state, and
// styles. We focus immediately so the first keystroke edits the
// field rather than opening the prompt.
func (m *runMode) openRenamePanel() {
	m.panel.kind = panelRename
	m.panel.cmd = "/rename"
	m.panel.textInput = newRenameTextinput(m.conversationName)
	// Per the textinput package: Focus() returns a tea.Cmd that
	// arms the cursor blink. Returning it through the bubbletea
	// program is the same flow bubbles' own textarea uses. m.prog
	// is nil in unit tests; the blink arm is best-effort.
	cmd := m.panel.textInput.Focus()
	if cmd != nil && m.prog != nil {
		m.prog.Send(cmd) //nolint:errcheck // best-effort blink arm
	}
}

// newRenameTextinput builds a textinput.Model primed for the rename
// panel: prompt = "Rename: ", placeholder visible when empty,
// focused, CharLimit = panelRenameLimit, Width = panelRenameInputWidth.
// The textinput is reusable across open/close cycles because
// Bubble Tea's Model is value-typed — we replace the field rather
// than mutate it in place.
func newRenameTextinput(seed string) textinput.Model {
	in := textinput.New()
	in.Prompt = textinputRenamePrompt
	in.Placeholder = textinputRenamePlaceholder
	// Cycle 283: Solarized base00 #657B83 — the same dim grey the
	// reference uses for secondary text (see colors.go).
	inStyles := in.Styles()
	placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#657B83"))
	inStyles.Focused.Placeholder = placeholderStyle
	inStyles.Blurred.Placeholder = placeholderStyle
	in.SetStyles(inStyles)
	in.CharLimit = panelRenameLimit
	in.SetWidth(panelRenameInputWidth)
	in.Focus()
	in.SetValue(seed)
	return in
}

// handleRenamePanelKey is the bubbletea key handler for the rename
// panel. It returns true (handled) for every keystroke that reaches
// the panel, mirroring handleAddDirKey() and handleFeedbackTextKey().
//
// Implementation notes:
//
//   - The textinput.Model is value-typed. We pass the msg through
//     textinput.Update and assign the returned model back to the
//     panel field.
//   - Esc and Enter are intercepted at the bubble level so the
//     submit / cancel paths can run their cycle-167 echo logic.
//     Other keys (left/right, Home/End, Ctrl+W, Ctrl+U, …) flow
//     through textinput.Update unchanged — that's the migration's
//     primary UX win (none of those worked in the old hand-rolled
//     `panel.text` buffer).
func (m *runMode) handleRenamePanelKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyEsc:
		m.closePanel()
		return true, nil
	case tea.KeyEnter:
		return true, m.submitRenamePanel()
	}
	// Delegate everything else to the bubbles textinput.
	updated, cmd := m.panel.textInput.Update(keyMsgFromKey(key))
	m.panel.textInput = updated
	return true, cmd
}

// submitRenamePanel commits the typed name via renameFeedback and
// closes the panel (silent — exit-notice=true group). It mirrors
// the cmd-line rename path's behaviour byte-for-byte so the
// transcript entry looks the same regardless of whether the user
// typed `/rename foo` or opened the panel and typed `foo`.
//
// renameFeedback is the cycle-106 implementation: it normalises
// the input (drops leading/trailing whitespace and embedded
// newlines), returns (newName, message) where message is the
// echo line shown to the user. On error (empty input after
// normalisation) newName == current and message starts with
// "Error: …". On success newName is the chosen name and message
// starts with "Conversation renamed to: …".
func (m *runMode) submitRenamePanel() tea.Cmd {
	name := m.panel.textInput.Value()
	newName, msg := renameFeedback(name, m.conversationName)
	if msg != "" {
		m.slashRespond("/rename", msg)
	}
	if newName != "" {
		m.conversationName = newName
	}
	return m.closePanelSilent()
}

// panelRenameLines renders the rename panel's body lines. The
// layout mirrors panelAddDirLines: title + blank + prompt+input +
// keyboard hint + blank. The prompt/input row uses the textinput's
// View() so the virtual cursor and placeholder styling match the
// reference's panel surfaces.
func (m *runMode) panelRenameLines() []string {
	return []string{
		panelTitleOpen + "Rename Conversation" + panelTitleClose,
		"",
		"  " + m.panel.textInput.View(),
		"",
		panelKeyboardHint("enter", "Rename", "esc", "Cancel"),
	}
}

// keyMsgFromKey converts a bubbletea tea.Key into a bubbletea
// KeyPressMsg the textinput.Model consumes. The textinput package
// does not handle tea.Key directly (it predates the bubble tea v2
// key-by-name rename); the adapter is the canonical seam between
// the two.
//
// A real consumer (the TUI loop) would build the same message
// from bubbletea's WindowSizeMsg / KeyPressMsg machinery, but the
// panel-level handler has a tea.Key in hand so we synthesise.
func keyMsgFromKey(k tea.Key) tea.KeyPressMsg {
	// tea.KeyPressMsg is a tea.Msg whose concrete type is exported
	// by bubbletea v2 — the field names are kept stable across
	// minor versions (verified by reading bubbletea v2.0.9's
	// key.go).
	return tea.KeyPressMsg{Code: k.Code, Mod: k.Mod, Text: k.Text}
}
