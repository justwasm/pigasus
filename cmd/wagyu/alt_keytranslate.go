package main

// alt_keytranslate.go — cycle alt-keybinding-option-meta.
//
// AGY 1.3.3 changelog line: "Fixed custom `alt` keybindings such
// as `alt+s` not working with the macOS Option key in terminals
// that don't treat Option as Meta, such as Terminal.app and VS
// Code-based terminals."
//
// Background: on macOS, the Option key normally forwards as Meta
// (ESC prefix) so the bubbletea event arrives as a single
// KeyPressMsg with Mod = ModAlt and Code = the letter. Two
// terminals do not behave this way by default:
//
//   - Terminal.app (TERM_PROGRAM=Apple_Terminal) — sends 'ß' for
//     Option+s unless "Use Option as Meta key" is enabled.
//   - VS Code's integrated terminal (TERM_PROGRAM=vscode) — same
//     default, same workaround.
//
// Modern terminals (iTerm2, kitty, WezTerm, Alacritty, ghostty)
// already forward Option as Meta and need no help. This file is
// a no-op on those, by design.
//
// The translation uses a 50 ms ESC buffer: a bare Esc keypress is
// held for up to one key (or 50 ms, whichever comes first). If the
// next key is a letter that has a binding in
// ~/.gemini/config/keybindings.json, we synthesise
// alt+<letter> instead. Otherwise the buffered Esc is released
// through the normal Esc branch (clear input, dismiss help, …)
// and the follow-up key is dispatched normally.
//
// The buffer is gated on three conditions:
//   - terminal is Apple_Terminal or vscode
//   - the dispatcher has at least one alt+letter binding
//   - the buffered Esc is still within EscReleaseWindow
//
// All three must hold; otherwise the buffer is bypassed entirely
// and the host sees the original Esc / letter, exactly as before
// this cycle. Tests drive all three states explicitly.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/keybind"
)

// altKeyFire records one synthesised alt+letter dispatch. Tests
// assert on the slice; production code consumes the action name
// (currently a stub — see altDispatchAction).
type altKeyFire struct {
	Letter rune   // lowercase letter that was synthesised
	Action string // action name from keybindings.json
}

// altDispatchAction is the production action table. The AGY
// 1.3.3 changelog closes the alt-keybinding gap but the reference
// binary's full action table is a separate cycle (the dispatcher
// already exists in the reference but not in the clone). For now,
// every configured binding appends a feedback row to the
// transcript so the user can see their keypress was honoured,
// and records the fire for tests. Adding real actions (submit,
// cancel, clear, …) is straightforward — extend the switch below
// and wire it into the rest of the TUI.
//
// Storing on runMode rather than as a function call keeps the
// seam testable without standing up a fake dispatcher.
func (m *runMode) altDispatchAction(letter rune, action string) {
	m.altKeyFires = append(m.altKeyFires, altKeyFire{Letter: letter, Action: action})
	m.response = append(m.response,
		cWarningBlueOpen+"  ⎿  "+cWarningBlueClose+
			cDimGrayOpen+"alt+"+string(letter)+" → "+action+cDimGrayClose)
}

// handleEscKey runs the bare-Esc branch: dismiss help, dismiss
// picker, clear input, dismiss the head announcement, or absorb
// silently. Returns (cmd, true) when the Esc was consumed.
//
// Extracted from Update() so the alt-keytranslation layer can
// replay a buffered Esc after deciding not to synthesise an
// alt+letter — without recursing through Update(). The ordering
// matches tui_run.go's original Esc handler byte-for-byte; only
// the body has moved.
func (m *runMode) handleEscKey() (tea.Cmd, bool) {
	if m.helpMode {
		m.helpMode = false
		m.response = append(m.response,
			cWarningBlueOpen+"  \u23bf  "+cWarningBlueClose+
				cDimGrayOpen+"Exited /help command"+cDimGrayClose)
		return nil, true
	}
	if m.picker.Visible() {
		m.picker.Reset()
		return nil, true
	}
	if m.input.Value() != "" {
		m.input.Reset()
		return nil, true
	}
	if m.activeAnnouncement() != nil {
		m.dismissCurrentAnnouncement()
		if msg := m.announcementSaveErrorMessage(); msg != "" {
			m.response = append(m.response, msg)
		}
		return nil, true
	}
	// Absorb Esc silently — ref binary does not quit on Esc,
	// only on Ctrl+C and Ctrl+D.
	return nil, true
}

// armEscReleaseTick returns a tea.Cmd that fires after
// keybind.EscReleaseWindow. The TUI installs this whenever it
// buffers a bare Esc, so a stale buffer (no follow-up key
// arrived in time) is released without a second keystroke from
// the user. The handler (Update → case keybind.EscTimeoutMsg)
// reads m.altEscBuf and runs the same release path.
func (m *runMode) armEscReleaseTick() tea.Cmd {
	return tea.Tick(keybind.EscReleaseWindow, func(time.Time) tea.Msg {
		return keybind.EscTimeoutMsg{}
	})
}

// consumeAltEsc is the alt-keytranslation entry point. It runs
// at the top of Update's `case tea.KeyMsg:` branch and either:
//
//   - returns (newMsg, consumed=true, cmd) when the current key
//     was a bare Esc that should be buffered for a follow-up —
//     the caller returns without dispatching, and the cmd arms
//     the release tick. newMsg is the original message so the
//     caller can pass it through unchanged.
//   - returns (newMsg, consumed=false, nil) when no buffer was
//     active or the buffer was released normally; newMsg may
//     differ from the input when an alt+letter was synthesised
//     (the caller should dispatch the synthesised event).
//
// `key` and `msg` are passed by pointer so the synthesised
// alt+letter can replace the caller's view of the event without
// allocating a new message.
func (m *runMode) consumeAltEsc(msg tea.KeyMsg) (newMsg tea.KeyMsg, consumed bool, cmd tea.Cmd) {
	key := msg.Key()
	// Gate 1: terminal must be one that does not forward Option
	// as Meta natively. On the right terminals ultraviolet's
	// decoder already produces alt+letter in a single event, and
	// the dispatcher sees the original event without our help.
	if !keybind.TermNeedsOptionMeta() {
		return msg, false, nil
	}

	// Gate 2: the dispatcher must have at least one alt+letter
	// binding configured. We can't synthesise an alt+letter that
	// nothing will receive, and we must NOT swallow an Esc that
	// a user with no keybindings.json expects to clear their
	// input — see TestAlt_NoAltsConfigured_FallsThrough.
	if m.keybindings == nil || len(m.keybindings.RawBindings()) == 0 {
		return msg, false, nil
	}

	// Stage A: we have a buffered Esc from a previous key.
	if m.altEscBuf.Pending {
		consumedKey := m.altEscBuf.Feed(
			key,
			func(r rune) bool { return m.keybindings != nil && m.keybindings.Has(r) },
			func(r rune) {
				key = tea.Key{Code: r, Mod: tea.ModAlt, Text: string(r)}
			},
			func() { _, _ = m.handleEscKey() },
			time.Now(),
		)
		if consumedKey {
			// Synthesised: caller dispatches the new event.
			return tea.KeyPressMsg(key), false, nil
		}
		// Buffer released (window elapsed, no binding, or
		// non-letter) — fall through to dispatch the original.
		return msg, false, nil
	}

	// Stage B: no buffer active. Maybe start one.
	if key.Code == tea.KeyEsc && key.Mod == 0 {
		if m.altEscBuf.Start(time.Now()) {
			return msg, true, m.armEscReleaseTick()
		}
	}

	return msg, false, nil
}
