package tuilib

import (
	tea "charm.land/bubbletea/v2"
)

// NavAction represents a directional or navigation intent from keyboard input.
type NavAction int

const (
	NavNone NavAction = iota
	NavUp
	NavDown
	NavLeft
	NavRight
	NavPageUp
	NavPageDown
	NavHalfPageUp
	NavHalfPageDown
	NavTop
	NavBottom
	NavCancel
	NavSelect
)

// ClassifyViewerNavKey maps key events in document viewers (such as /changelog,
// /usage, and /feature-diff) into navigation actions adhering to Bubbletea's
// viewport and standard pager conventions (j/k, g/G, d/u, f/b, space, q, esc).
func ClassifyViewerNavKey(key tea.Key) NavAction {
	str := key.String()
	switch {
	case isHalfPageUp(key, str):
		return NavHalfPageUp
	case isHalfPageDown(key, str):
		return NavHalfPageDown
	case key.Code == tea.KeyUp || str == "up" || str == "k" || (key.Mod == 0 && key.Code == 'k'):
		return NavUp
	case key.Code == tea.KeyDown || str == "down" || str == "j" || (key.Mod == 0 && key.Code == 'j'):
		return NavDown
	case isPageUp(key, str):
		return NavPageUp
	case isPageDown(key, str):
		return NavPageDown
	case isTop(key, str):
		return NavTop
	case isBottom(key, str):
		return NavBottom
	case key.Code == tea.KeyEsc || str == "esc" || str == "q" || (key.Mod == 0 && key.Code == 'q'):
		return NavCancel
	default:
		return NavNone
	}
}

// ClassifyListNavKey maps key events for list-based modal pickers without
// text inputs. Space is excluded so selection toggles can handle it.
func ClassifyListNavKey(key tea.Key) NavAction {
	str := key.String()
	switch {
	case key.Code == tea.KeyUp || str == "up" || str == "k" || (key.Mod == 0 && key.Code == 'k'):
		return NavUp
	case key.Code == tea.KeyDown || str == "down" || str == "j" || (key.Mod == 0 && key.Code == 'j'):
		return NavDown
	case key.Code == tea.KeyLeft || str == "left" || str == "h" || (key.Mod == 0 && key.Code == 'h'):
		return NavLeft
	case key.Code == tea.KeyRight || str == "right" || str == "l" || (key.Mod == 0 && key.Code == 'l'):
		return NavRight
	case key.Code == tea.KeyPgUp || str == "pgup":
		return NavPageUp
	case key.Code == tea.KeyPgDown || str == "pgdown":
		return NavPageDown
	case isTop(key, str):
		return NavTop
	case isBottom(key, str):
		return NavBottom
	case key.Code == tea.KeyEsc || str == "esc" || str == "q" || (key.Mod == 0 && key.Code == 'q'):
		return NavCancel
	case key.Code == tea.KeyEnter || str == "enter":
		return NavSelect
	default:
		return NavNone
	}
}

func isTop(key tea.Key, str string) bool {
	return key.Code == tea.KeyHome || str == "home" || str == "ctrl+home" ||
		str == "g" || (key.Mod == 0 && key.Code == 'g')
}

func isBottom(key tea.Key, str string) bool {
	return key.Code == tea.KeyEnd || str == "end" || str == "ctrl+end" ||
		str == "G" || (key.Mod == 0 && key.Code == 'G') ||
		(key.Mod.Contains(tea.ModShift) && (key.Code == 'G' || key.Code == 'g'))
}

func isHalfPageUp(key tea.Key, str string) bool {
	return str == "ctrl+u" || (key.Mod.Contains(tea.ModCtrl) && key.Code == 'u') ||
		(key.Mod == 0 && key.Code == 'u') ||
		(key.Code == tea.KeyUp && key.Mod.Contains(tea.ModShift))
}

func isHalfPageDown(key tea.Key, str string) bool {
	return str == "ctrl+d" || (key.Mod.Contains(tea.ModCtrl) && key.Code == 'd') ||
		(key.Mod == 0 && key.Code == 'd') ||
		(key.Code == tea.KeyDown && key.Mod.Contains(tea.ModShift))
}

func isPageUp(key tea.Key, str string) bool {
	return key.Code == tea.KeyPgUp || str == "pgup" || str == "b" || str == "ctrl+b" ||
		(key.Mod == 0 && key.Code == 'b') ||
		(key.Mod.Contains(tea.ModCtrl) && key.Code == 'b')
}

func isPageDown(key tea.Key, str string) bool {
	return key.Code == tea.KeyPgDown || key.Code == tea.KeySpace ||
		str == "pgdown" || str == "space" || str == "f" || str == "ctrl+f" ||
		(key.Mod == 0 && key.Code == 'f') ||
		(key.Mod.Contains(tea.ModCtrl) && key.Code == 'f')
}
