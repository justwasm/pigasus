package main

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
)

// ---- Cycle 223: panelAddDir /add-dir picker -------------------------------

// panelAddDirLines renders the /add-dir picker. REF 1.4.0 does not render
// a panel for `/add-dir` no-arg — it just echoes `> /add-dir ⎿ error: no
// directory specified` (`/private/tmp/agy217/caps/ref.adddir-bare.raw.txt`).
// Cycle 223 closes TODO §2.1's last A-class panel gap by giving OUR (which
// otherwise mirrors the same echo) a modal overlay:
//
//	Add Directory to Workspace
//	                       <- blank
//	  Trusted workspaces:   <- dim gray 241 heading
//	  /Users/...            <- dim gray 241 line per entry (from
//	                          auth.LoadTrustedWorkspaces)
//	  ...
//	                       <- blank
//	  Path: <text>          <- input row; the cursor is a single reverse-
//	                          video cell (searchCursorBlock) appended to
//	                          the typed text, same primitive /model and
//	                          /config use
//	                       <- blank
//	Keyboard: enter Add Directory  esc Cancel
//
// The title is the natural-language rendering of the cycle-217 success
// line ("Added %s to workspace") — same wording the cycle picker applies
// to its other modals. The keyboard hint's `enter Add Directory` is the
// closest analogue of REF's `/mcp` and `/tasks` legends; the brief
// explicitly calls out "press Enter to add, esc to close" as the
// expected closing.
//
// No-arg /add-dir is the only path that opens this panel — see
// openSlashPanelArgs in slash_codesearch.go.
func (m *runMode) panelAddDirLines() []string {
	trusted := auth.LoadTrustedWorkspaces()
	lines := []string{
		panelTitleOpen + "Add Directory to Workspace" + panelTitleClose,
		"",
	}
	if len(trusted) == 0 {
		lines = append(lines,
			cDimGrayOpen+"  No trusted workspaces yet."+cDimGrayClose)
	} else {
		lines = append(lines,
			cDimGrayOpen+"  Trusted workspaces:"+cDimGrayClose)
		for _, dir := range trusted {
			lines = append(lines,
				"  "+cDimGrayOpen+dir+cDimGrayClose)
		}
	}
	// The cursor sits at the end of the typed text. The reverse-video
	// cell is the same primitive /model's Search: box and /config's Search:
	// box use (searchCursorBlock). On every render we draw the latest
	// m.panel.text + the cursor — no blink, since the REF path this
	// replaces has no panel to mirror the cadence from.
	lines = append(lines,
		"",
		"  Path: "+m.panel.text+searchCursorBlock,
		"",
		panelKeyboardHint("enter", "Add Directory", "esc", "Cancel"),
	)
	return lines
}

// handleAddDirKey is the /add-dir picker's key handler. The panel owns
// every key until it is dismissed:
//
//   - Esc → closePanel (writes the standard "Exited /add-dir command"
//     echo — /add-dir stays in the exit-notice=true group because the
//     cycle-193 set explicitly carved out only specific panel kinds
//     like /mcp /feedback for silent close).
//   - Enter → run addDirFeedback on the typed text, emit its return
//     value through slashRespond, and closePanelSilent. The cycle-217
//     echo path handles "error: no directory specified" (empty input),
//     "error: not a directory: …" (regular file) and "Added <path> to
//     workspace" (success); the panel reuses it byte-for-byte so the
//     panel-form and with-args forms share one renderer. Going through
//     the silent close — not closePanel — keeps the standard exit
//     notice from doubling up with the slashRespond block.
//   - Backspace → drop one rune.
//   - Any printable → append.
//
// Falls through to the global bindings only when ctrl+*, alt+* or
// another modified key shows up (mirrors handleFeedbackTextKey).
func (m *runMode) handleAddDirKey(key tea.Key) (bool, tea.Cmd) {
	switch {
	case key.Code == tea.KeyEsc:
		return true, m.closePanel()
	case key.Code == tea.KeyEnter:
		msg := addDirFeedback(m.panel.text)
		m.slashRespond("/add-dir", msg)
		return true, m.closePanelSilent()
	case key.Code == tea.KeyBackspace:
		if r := []rune(m.panel.text); len(r) > 0 {
			m.panel.text = string(r[:len(r)-1])
		}
		return true, nil
	}
	if isTypedText(key) {
		m.panel.text += key.Text
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}
