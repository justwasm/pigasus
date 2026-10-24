package cliapp

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/vim"
)

// Cycle 216: the TUI half of the Vim editing mode. The engine lives in
// internal/vim and knows nothing about bubbles or the run loop; this file
// is the only bridge. It translates a bubbletea key into the engine's
// normalised Key, keeps the textarea in step with the engine's buffer and
// cursor, and hands a submit result to the shared submitPrompt path.
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
	if raw, err := auth.SettingsField("editorMode"); err == nil {
		m.vimOn = rawString(raw) == "vim"
	}
	m.vimFirstOn = false
	if raw, err := auth.SettingsField("vimInsertFirst"); err == nil {
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

// vimBadge is the mode badge for the status row. It is empty when vim
// mode is off, and also in Normal mode — like Vim, Normal shows nothing.
func (m *runMode) vimBadge() string {
	if !m.vimEnabled() {
		return ""
	}
	return m.vimEngine().Mode.Badge()
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
	var res vim.Result
	handled := false

	if nk := vimNamedKey(key); nk != (vim.Key{}) {
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
		vk, ok := vimRuneKey(key)
		if !ok {
			return nil, false
		}
		res = eng.Handle(vk)
		handled = res.Consumed
	}

	if res.Submit {
		m.syncVimInput()
		return m.submitPrompt(), true
	}
	if !handled {
		return nil, false
	}
	m.syncVimInput()
	return nil, true
}

// vimMod converts the bubbletea modifier set to the engine's.
func vimMod(mod tea.KeyMod) vim.Mod {
	var out vim.Mod
	if mod.Contains(tea.ModCtrl) {
		out |= vim.Ctrl
	}
	if mod.Contains(tea.ModAlt) {
		out |= vim.Alt
	}
	if mod.Contains(tea.ModShift) {
		out |= vim.Shift
	}
	return out
}

// vimNamedKey maps a special key code to the engine's named key. It
// reports the zero Key when the code is not one of the named keys, which
// lets the caller test it with a plain comparison.
func vimNamedKey(key tea.Key) vim.Key {
	mod := vimMod(key.Mod)
	switch key.Code {
	case tea.KeyEsc:
		return vim.Key{Name: "esc", Mod: mod}
	case tea.KeyEnter:
		return vim.Key{Name: "enter", Mod: mod}
	case tea.KeyBackspace:
		return vim.Key{Name: "backspace", Mod: mod}
	case tea.KeyDelete:
		return vim.Key{Name: "delete", Mod: mod}
	case tea.KeyLeft:
		return vim.Key{Name: "left", Mod: mod}
	case tea.KeyRight:
		return vim.Key{Name: "right", Mod: mod}
	case tea.KeyUp:
		return vim.Key{Name: "up", Mod: mod}
	case tea.KeyDown:
		return vim.Key{Name: "down", Mod: mod}
	case tea.KeyHome:
		return vim.Key{Name: "home", Mod: mod}
	case tea.KeyEnd:
		return vim.Key{Name: "end", Mod: mod}
	case tea.KeyTab:
		return vim.Key{Name: "tab", Mod: mod}
	}
	return vim.Key{}
}

// vimRuneKey maps anything with a key code to a rune key. Reports false
// for a bare modifier press (no code) or an unknown function key.
func vimRuneKey(key tea.Key) (vim.Key, bool) {
	if key.Code == 0 {
		return vim.Key{}, false
	}
	return vim.Key{Rune: key.Code, Mod: vimMod(key.Mod)}, true
}

// helpVim is the binding catalogue shown on the /help VIM tab.
var helpVim = vim.HelpRows

// helpVimTitle is the /help tab label for the vim catalogue.
const helpVimTitle = vim.HelpTitle

// helpTabs is the /help tab list. The vim catalogue tab is appended only
// when vim mode is enabled, so the default three-tab bar (and every
// byte-verified capture of it) is unchanged for non-vim users.
func helpTabs(m *runMode) []string {
	if m != nil && m.vimEnabled() {
		return []string{"general", "commands", "shortcuts", helpVimTitle}
	}
	return []string{"general", "commands", "shortcuts"}
}

// renderHelpVim draws the VIM tab.
func renderHelpVim(b *strings.Builder, m *runMode) {
	b.WriteString(helpHeaderOpen)
	b.WriteString(vim.HelpHeader)
	b.WriteString(helpHeaderClose)
	b.WriteString("\n")
	b.WriteString("  ")
	b.WriteString(helpDimGrayOpen)
	b.WriteString(vim.HelpSubtitle)
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n\n")
	total := len(helpVim)
	m.helpListClampWindow(total)
	start := m.helpWindow
	visible := helpListVisible(total, start, m.helpListWindowSize())
	for i := 0; i < visible; i++ {
		idx := start + i
		b.WriteString(buildShortcutRow(helpVim[idx], idx == m.helpCursor))
		b.WriteString("\n")
	}
	from := start + 1
	to := start + visible
	fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n", helpDimGrayOpen, from, to, total, helpDimGrayClose)
	b.WriteString("\n")
	b.WriteString(helpKeyboardHint)
	b.WriteString("\n\n")
}
