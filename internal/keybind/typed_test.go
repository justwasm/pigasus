package keybind

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestIsTypedText(t *testing.T) {
	cases := []struct {
		name string
		key  tea.Key
		want bool
	}{
		{
			name: "plain letter",
			key:  tea.Key{Code: 'a', Text: "a"},
			want: true,
		},
		{
			name: "shifted uppercase letter",
			key:  tea.Key{Code: 'A', Mod: tea.ModShift, Text: "A"},
			want: true,
		},
		{
			name: "ctrl key",
			key:  tea.Key{Code: 'c', Mod: tea.ModCtrl, Text: "c"},
			want: false,
		},
		{
			name: "alt key",
			key:  tea.Key{Code: 's', Mod: tea.ModAlt, Text: "s"},
			want: false,
		},
		{
			name: "empty text (arrow key)",
			key:  tea.Key{Code: tea.KeyLeft, Text: ""},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTypedText(tc.key); got != tc.want {
				t.Errorf("IsTypedText(%+v) = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}
