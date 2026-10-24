package cliapp

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/tuilib"
)

func (m *runMode) handleEffortKey(key tea.Key) (bool, tea.Cmd, bool) {
	if m.panel.kind != panelEffort {
		return false, nil, false
	}
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavLeft:
		if m.panel.cursor > effortLow {
			m.panel.cursor--
		}
		return true, nil, true
	case tuilib.NavRight:
		if m.panel.cursor < effortHigh {
			m.panel.cursor++
		}
		return true, nil, true
	case tuilib.NavSelect:
		return true, m.applyEffortSelection(m.panel.cursor), true
	case tuilib.NavCancel:
		return true, m.closePanel(), true
	case tuilib.NavUp, tuilib.NavDown:
		return true, nil, true
	}
	return false, nil, false
}

// handleModelKey routes the /model panel's keys.
//
// The reference's picker is a *search field* first: every printable
// character goes into "Search:" and filters the rows, and only the arrow
// keys navigate. Verified live against agy 1.4.5 — typing "j" filters
// (it does not move the cursor down) and the panel answers
// `No models match "j"`. So this handler does not use
// tuilib.ClassifyListNavKey, which would read "j"/"k"/"h"/"l"/"g"/"q" as
// navigation.
func (m *runMode) handleModelKey(key tea.Key, n int) (bool, tea.Cmd, bool) {
	if m.panel.kind != panelModel {
		return false, nil, false
	}
	if handled, cmd, ok := m.handleModelQueryKey(key); handled {
		return true, cmd, ok
	}
	if handled, cmd, ok := m.handleModelNavKey(key, n); handled {
		return true, cmd, ok
	}
	if key.Mod == 0 && key.Text != "" && key.Text != "\t" && key.Text != "\n" && key.Text != "\r" {
		m.panel.modelQuery += key.Text
		m.panel.cursor = 0
		return true, nil, true
	}
	return false, nil, false
}

// handleModelQueryKey handles the keys that edit the search box: Esc
// (which clears a live query and keeps the panel open), Enter and
// Backspace.
func (m *runMode) handleModelQueryKey(key tea.Key) (bool, tea.Cmd, bool) {
	switch key.Code {
	case tea.KeyEsc:
		// A live query makes Esc clear it and keep the panel open
		// ("esc Clear search"); an empty box closes the panel.
		if m.panel.modelQuery != "" {
			m.panel.modelQuery = ""
			m.panel.cursor = 0
			return true, nil, true
		}
		return true, m.closePanel(), true
	case tea.KeyEnter:
		m.selectModelEffort()
		return true, m.closePanel(), true
	case tea.KeyBackspace:
		if m.panel.modelQuery != "" {
			r := []rune(m.panel.modelQuery)
			m.panel.modelQuery = string(r[:len(r)-1])
			m.panel.cursor = 0
		}
		return true, nil, true
	}
	return false, nil, false
}

// handleModelNavKey handles the keys that move the picker's cursor or
// its effort slider.
func (m *runMode) handleModelNavKey(key tea.Key, n int) (bool, tea.Cmd, bool) {
	switch key.Code {
	case tea.KeyUp:
		if m.panel.cursor > 0 {
			m.panel.cursor--
			m.panel.effortCursor = effortCursorForLabel(m.panelCurrentModelLabel())
		}
		return true, nil, true
	case tea.KeyDown:
		if m.panel.cursor < n-1 {
			m.panel.cursor++
			m.panel.effortCursor = effortCursorForLabel(m.panelCurrentModelLabel())
		}
		return true, nil, true
	case tea.KeyLeft:
		m.cycleModelEffort(-1)
		return true, nil, true
	case tea.KeyRight:
		m.cycleModelEffort(+1)
		return true, nil, true
	case tea.KeyHome:
		m.panel.cursor = 0
		m.panel.effortCursor = effortCursorForLabel(m.panelCurrentModelLabel())
		return true, nil, true
	case tea.KeyEnd:
		if n > 0 {
			m.panel.cursor = n - 1
			m.panel.effortCursor = effortCursorForLabel(m.panelCurrentModelLabel())
		}
		return true, nil, true
	}
	return false, nil, false
}
