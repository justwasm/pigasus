package storybook_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/storybook"
)

func TestInputSelectionStoryScenarios(t *testing.T) {
	story, ok := storybook.DefaultCatalog().Find("composer-input-selection")
	if !ok {
		t.Fatal("real input-selection module story is missing from the catalog")
	}
	const open = "\x1b[0m\x1b[7m"
	const close = "\x1b[0m"
	want := map[string]string{
		"initial":           "> hello world",
		"forward-selection": "> " + open + "hello" + close + " world",
		"reverse-selection": "> hello " + open + "world" + close,
		"cleared":           "> hello world",
	}
	if len(story.States) != len(want) {
		t.Fatalf("catalog has %d selection states, want %d", len(story.States), len(want))
	}
	for index, state := range story.States {
		t.Run(state.ID, func(t *testing.T) {
			expected, exists := want[state.ID]
			if !exists {
				t.Fatalf("catalog state %q has no render assertion", state.ID)
			}
			inst := story.Factory()
			inst.SetSize(40, 12)
			inst.SetState(index)
			if view := inst.View(); !strings.Contains(view, expected+"\n") {
				t.Fatalf("state render = %q, want composer row %q", view, expected)
			}
			_, handled := inst.HandleKey(tea.KeyPressMsg{Code: tea.KeyEsc})
			if !handled || strings.Contains(inst.View(), open) {
				t.Fatalf("Esc did not clear real module highlight: %q", inst.View())
			}
			_, handled = inst.HandleKey(tea.KeyPressMsg{Code: 'a', Mod: tea.ModSuper})
			if !handled || !strings.Contains(inst.View(), "> "+open+"hello world"+close) {
				t.Fatalf("select-all did not update real module render: %q", inst.View())
			}
			_, handled = inst.HandleKey(tea.KeyPressMsg{Code: tea.KeyDelete})
			if !handled || !strings.Contains(inst.View(), "\n> \n") || strings.Contains(inst.View(), "hello") {
				t.Fatalf("delete did not update real module editor: %q", inst.View())
			}
		})
	}
}
