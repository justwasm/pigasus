package cliapp

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
)

func TestDispatchKeyAction_CustomKeybindings(t *testing.T) {
	kb, err := keybind.Load("/nonexistent")
	if err != nil {
		t.Fatalf("unexpected load err: %v", err)
	}

	m := &runMode{
		keybindings: kb,
	}

	// 1. Dispatch without binding should fall through to hardcoded keys
	_, cmd, handled := m.handleGlobalOrModalKeys(tea.Key{Code: 'l', Mod: tea.ModCtrl})
	if !handled || cmd == nil {
		t.Errorf("expected Ctrl+L default clear screen to be handled")
	}

	// 2. Custom binding remap: "cli.exit" to "ctrl+q"
	kbJSON := `{"cli.exit": ["ctrl+q"]}`
	tmp := t.TempDir() + "/keybindings.json"
	_ = os.WriteFile(tmp, []byte(kbJSON), 0644) // load custom
	kbCustom, _ := keybind.Load(tmp)
	m.keybindings = kbCustom

	_, quitCmd, handledQuit := m.handleGlobalOrModalKeys(tea.Key{Code: 'q', Mod: tea.ModCtrl})
	if !handledQuit || quitCmd == nil {
		t.Errorf("expected Ctrl+Q mapped to cli.exit to be handled with quit cmd")
	}
}
