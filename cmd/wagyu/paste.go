// paste.go — cycle 278: bracketed paste and ctrl+v.
//
// The clone ignored both. bubbletea v2 delivers a terminal paste as
// tea.PasteMsg (produced from ultraviolet's PasteEvent when the terminal
// has bracketed paste enabled) and runMode's typed switch had no case for
// it, so the text was silently dropped. ctrl+v was worse: it fell through
// to the bubbles textarea, whose default keymap maps ctrl+v to its Paste
// command (textarea.go KeyMap.Paste), which reads the OS clipboard and
// returns an *unexported* textarea.pasteMsg that the same catch-all
// discarded. Net effect: the clipboard was read and the result thrown
// away — "ctrl+v 无法粘贴".
//
// The reference vendors github.com/atotto/clipboard (recovered symbols
// clipboard.readAll and clipboard.getPasteCommand), i.e. it shells out to
// pbpaste / xclip / xsel. It inserts the text at the cursor, does not
// submit, and treats an empty clipboard as a silent no-op.
//
// Scope note: images are not wired. backend.Message has no media field,
// so an accepted image would have nowhere to go; pbpaste returns text
// only, and an image-only clipboard therefore yields "" and a no-op.
package main

import (
	"os/exec"
	"runtime"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// clipboardReadText is the seam tests replace so they never touch the
// real clipboard. Empty string means "nothing to paste" — the OS tool is
// missing, the clipboard is empty, or the read failed. All three are the
// same silent no-op, matching the reference.
var clipboardReadText = readClipboardText

// readClipboardText runs the platform paste utility. The command shape
// mirrors copyToClipboard's switch in slash_actions.go so both halves of
// the clipboard story use the same tools.
func readClipboardText() string {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbpaste")
	case "linux":
		switch {
		case haveExec("wl-paste"):
			cmd = exec.Command("wl-paste", "--no-newline")
		case haveExec("xclip"):
			cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
		case haveExec("xsel"):
			cmd = exec.Command("xsel", "--clipboard", "--output")
		default:
			return ""
		}
	default:
		return ""
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// normalizePaste strips the CR that a Windows-style clipboard or a
// terminal may include. A textarea line break is "\n"; leaving "\r" in
// the buffer would show up as a stray cell in the prompt echo and in the
// submitted message.
func normalizePaste(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "")
}

// insertPastedText is the single insertion point for both the terminal's
// bracketed paste and ctrl+v. It never submits.
func (m *runMode) insertPastedText(s string) tea.Cmd {
	s = normalizePaste(s)
	if s == "" {
		return nil
	}
	// The /rename panel owns a real textinput; let it consume the paste
	// so the name can be pasted too.
	if m.panel.kind == panelRename {
		updated, cmd := m.panel.textInput.Update(tea.PasteMsg{Content: s})
		m.panel.textInput = updated
		return cmd
	}
	// Other overlays (help, pickers, viewers) own the screen; drop rather
	// than queue the text somewhere the user cannot see it.
	if m.helpMode || m.panel.kind != panelNone {
		return nil
	}
	// /resume's search field is a plain string, not the textarea.
	if m.resumeMode {
		m.resumeQuery += s
		m.resumeWindow, m.resumeCursor = 0, 0
		return nil
	}
	if m.hasInputSelection() {
		m.replaceInputSelection(s)
		m.modeHintDismissed = true
		m.picker.Update([]rune(firstLine(m.input.Value())))
		m.atFileDropdown.Update(m.input.Value(), m.cwd)
		m.clearEligibilityNotice()
		return nil
	}
	m.input.InsertString(s)
	m.picker.Update([]rune(firstLine(m.input.Value())))
	m.atFileDropdown.Update(m.input.Value(), m.cwd)
	m.clearEligibilityNotice()
	return nil
}
