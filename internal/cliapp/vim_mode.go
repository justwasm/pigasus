package cliapp

import (
	tea "charm.land/bubbletea/v2"

	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/ui/editor/vimkeys"
	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	"github.com/justwasm/pigasus/vim"
)

// Cycle 216: the TUI half of the Vim editing mode. The engine lives in
// vim and knows nothing about bubbles or the run loop. This file is the
// host's half of the bridge: it keeps the textarea in step with the
// engine's buffer and cursor, feeds the engine one key at a time and
// hands a submit result to the shared submitPrompt path.
//
// Cycle 381, tranche 58: the translation itself — the bubbletea key to
// the engine's normalised Key — moved to internal/ui/editor/vimkeys,
// which is where a key that knows about the terminal but not about the
// engine belongs. This file names that package; it keeps no copy of
// the mapping.
//
// Everything here is inert unless the "Editor Mode" setting is vim, so the
// default (non-vim) prompt behaves exactly as it did before.

// vimEnabled reports whether "Editor Mode" selects vim. The value is read
// from settings.json at TUI start-up (and again whenever a slash panel
// closes, so toggling Editor Mode in /config applies without a restart);
// it is kept in a field rather than re-read per keystroke so the tests
// stay hermetic and the render path does no file I/O.
func (m *runMode) vimEnabled() bool { return m.vimOn }

// vimInsertFirstEnabled reports the "Editor Mode › Insert First" child
// setting: when on, the prompt opens in Insert mode and a bare Enter
// submits instead of inserting a newline.
func (m *runMode) vimInsertFirstEnabled() bool { return m.vimFirstOn }

// reloadVimSettings reads the two vim settings from the live
// settings.json. It is called once at TUI start-up and again when a slash
// panel closes (/config can change both).
func (m *runMode) reloadVimSettings() {
	m.vimOn = false
	if raw, err := settings.Field("editorMode"); err == nil {
		m.vimOn = configpanel.RawString(raw) == "vim"
	}
	m.vimFirstOn = false
	if raw, err := settings.Field("vimInsertFirst"); err == nil {
		switch v := raw.(type) {
		case bool:
			m.vimFirstOn = v
		case string:
			m.vimFirstOn = v == "on" || v == "true"
		}
	}
}

// vimEngine returns the engine, building it from the current prompt on
// first use. Whenever the host has replaced the buffer behind the
// engine's back — a submit reset, a history recall, the picker's Tab
// substitution, a slash rewrite — the engine is re-seeded so it always
// tracks what is actually on screen.
func (m *runMode) vimEngine() *vim.State {
	if m.vimEng == nil {
		m.vimEng = vim.New(m.input.Value())
	} else if m.vimEng.Text() != m.input.Value() {
		m.vimEng.SetText(m.input.Value())
	}
	// SetInsertFirst is a no-op when the setting is unchanged, so this
	// does not yank the user back into Insert mode on every keystroke.
	m.vimEng.SetInsertFirst(m.vimInsertFirstEnabled())
	return m.vimEng
}

// vimMode reports the mode the status row's badge shows.
//
// The engine is seeded the way the host seeds it for a key press, so the
// badge agrees with the mode the next key will be handled in. It is not
// built when vim mode is off: the badge is empty then, and the host
// keeps its "no engine without vim" invariant.
func (m *runMode) vimMode() vim.Mode {
	if !m.vimEnabled() {
		return vim.Normal
	}
	return m.vimEngine().Mode
}

// syncVimInput mirrors the engine onto the textarea. The textarea is only
// a display surface in vim mode; the engine owns the buffer and cursor.
//
// SetValue leaves the textarea cursor at the end of the buffer, so the
// row is walked back up before the column is set. A column past the end
// of the line is legal in Insert mode (the append point), and the
// textarea clamps it itself.
func (m *runMode) syncVimInput() {
	eng := m.vimEng
	if eng == nil {
		return
	}
	m.input.SetValue(eng.Text())
	for r := m.input.LineCount() - 1; r > eng.Row; r-- {
		m.input.CursorUp()
	}
	m.input.SetCursorColumn(eng.Col)
}

// handleVimKey feeds one key to the engine. handled is false when the
// engine declined the key, which is the signal for the caller to fall
// through to the host's own binding (ctrl+c, ctrl+d, ctrl+o, Tab, …).
//
// The order matters: the special codes are checked first because a
// driver may attach text to them too (Enter arrives with Text "\n" in
// some terminals, and in the clone's own synthetic key events). Only
// then is the literal text consumed — a paste can deliver several runes
// in one event, so each is fed through the engine in order.
func (m *runMode) handleVimKey(key tea.Key) (tea.Cmd, bool) {
	eng := m.vimEngine()

	// When on the last line of the buffer, Down arrow or 'j' (in Normal mode)
	// switches focus into ActiveItems if any are running, matching PromptModel.Update (0x102c91d60).
	if (eng.Mode == vim.Normal && eng.Pending == "") || eng.Mode == vim.Insert {
		isDown := key.Code == tea.KeyDown
		if eng.Mode == vim.Normal {
			isDown = isDown || ((key.Code == 'j' || key.Text == "j") && key.Mod == 0)
		}
		if isDown && eng.Row >= len(eng.Lines)-1 {
			if !m.activeItems.Hidden() && m.activeItems.Count() > 0 {
				m.activeItems.SetFocus(true)
				m.activeItems.SelectFirst()
				return nil, true
			}
		}
	}

	var res vim.Result
	handled := false

	if nk := vimkeys.Named(key); nk != (vim.Key{}) {
		res = eng.Handle(nk)
		handled = res.Consumed
	} else if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		for _, r := range key.Text {
			res = eng.Handle(vim.Key{Rune: r})
			if res.Consumed {
				handled = true
			}
			if res.Submit {
				break
			}
		}
	} else {
		vk, ok := vimkeys.Rune(key)
		if !ok {
			return nil, false
		}
		res = eng.Handle(vk)
		handled = res.Consumed
	}

	if res.Submit {
		m.syncVimInput()
		cmd := m.submitPrompt()
		// 【一手】 agy 1.4.9, the vendor's own release notes: "a
		// half-typed operator such as `d` carrying over after you submit
		// a prompt". The submit clears the composer, so the engine is
		// re-seeded from it — with the half-typed operator intact.
		m.vimEng.SetTextKeepingPending(m.input.Value())
		m.syncVimInput()
		return cmd, true
	}
	if !handled {
		return nil, false
	}
	m.syncVimInput()
	return nil, true
}

// Cycle 381: the /help overlay's vim tab moved to
// internal/ui/views/helppanel, which owns the tab list and the
// binding catalogue it draws.
