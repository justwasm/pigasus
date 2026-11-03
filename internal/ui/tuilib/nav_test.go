package tuilib

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestClassifyViewerNavKey(t *testing.T) {
	tests := []struct {
		name string
		key  tea.Key
		want NavAction
	}{
		{"arrow up", tea.Key{Code: tea.KeyUp}, NavUp},
		{"letter k", tea.Key{Code: 'k', Text: "k"}, NavUp},
		{"arrow down", tea.Key{Code: tea.KeyDown}, NavDown},
		{"letter j", tea.Key{Code: 'j', Text: "j"}, NavDown},
		{"ctrl+u", tea.Key{Code: 'u', Mod: tea.ModCtrl}, NavHalfPageUp},
		{"letter u", tea.Key{Code: 'u', Text: "u"}, NavHalfPageUp},
		{"shift+up", tea.Key{Code: tea.KeyUp, Mod: tea.ModShift}, NavHalfPageUp},
		{"ctrl+d", tea.Key{Code: 'd', Mod: tea.ModCtrl}, NavHalfPageDown},
		{"letter d", tea.Key{Code: 'd', Text: "d"}, NavHalfPageDown},
		{"shift+down", tea.Key{Code: tea.KeyDown, Mod: tea.ModShift}, NavHalfPageDown},
		{"pgup", tea.Key{Code: tea.KeyPgUp}, NavPageUp},
		{"letter b", tea.Key{Code: 'b', Text: "b"}, NavPageUp},
		{"ctrl+b", tea.Key{Code: 'b', Mod: tea.ModCtrl}, NavPageUp},
		{"pgdown", tea.Key{Code: tea.KeyPgDown}, NavPageDown},
		{"space", tea.Key{Code: tea.KeySpace}, NavPageDown},
		{"letter f", tea.Key{Code: 'f', Text: "f"}, NavPageDown},
		{"ctrl+f", tea.Key{Code: 'f', Mod: tea.ModCtrl}, NavPageDown},
		{"letter g", tea.Key{Code: 'g', Text: "g"}, NavTop},
		{"home", tea.Key{Code: tea.KeyHome}, NavTop},
		{"ctrl+home", tea.Key{Code: tea.KeyHome, Mod: tea.ModCtrl}, NavTop},
		{"capital G", tea.Key{Code: 'G', Text: "G"}, NavBottom},
		{"shift+g", tea.Key{Code: 'g', Mod: tea.ModShift}, NavBottom},
		{"end", tea.Key{Code: tea.KeyEnd}, NavBottom},
		{"ctrl+end", tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl}, NavBottom},
		{"esc", tea.Key{Code: tea.KeyEsc}, NavCancel},
		{"letter q", tea.Key{Code: 'q', Text: "q"}, NavCancel},
		{"unrelated key", tea.Key{Code: 'x', Text: "x"}, NavNone},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyViewerNavKey(tc.key)
			if got != tc.want {
				t.Errorf("ClassifyViewerNavKey(%+v) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

func TestClassifyListNavKey(t *testing.T) {
	tests := []struct {
		name string
		key  tea.Key
		want NavAction
	}{
		{"arrow up", tea.Key{Code: tea.KeyUp}, NavUp},
		{"letter k", tea.Key{Code: 'k', Text: "k"}, NavUp},
		{"arrow down", tea.Key{Code: tea.KeyDown}, NavDown},
		{"letter j", tea.Key{Code: 'j', Text: "j"}, NavDown},
		{"arrow left", tea.Key{Code: tea.KeyLeft}, NavLeft},
		{"letter h", tea.Key{Code: 'h', Text: "h"}, NavLeft},
		{"arrow right", tea.Key{Code: tea.KeyRight}, NavRight},
		{"letter l", tea.Key{Code: 'l', Text: "l"}, NavRight},
		{"letter g", tea.Key{Code: 'g', Text: "g"}, NavTop},
		{"home", tea.Key{Code: tea.KeyHome}, NavTop},
		{"letter G", tea.Key{Code: 'G', Text: "G"}, NavBottom},
		{"end", tea.Key{Code: tea.KeyEnd}, NavBottom},
		{"esc", tea.Key{Code: tea.KeyEsc}, NavCancel},
		{"letter q", tea.Key{Code: 'q', Text: "q"}, NavCancel},
		{"enter", tea.Key{Code: tea.KeyEnter}, NavSelect},
		{"space not captured for list", tea.Key{Code: tea.KeySpace}, NavNone},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyListNavKey(tc.key)
			if got != tc.want {
				t.Errorf("ClassifyListNavKey(%+v) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}
