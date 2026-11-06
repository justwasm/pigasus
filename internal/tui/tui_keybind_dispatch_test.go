package tui

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"

	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
)

func TestDispatchKeyAction_CustomKeybindings(t *testing.T) {
	kb, err := keybind.Load("/nonexistent")
	if err != nil {
		t.Fatalf("unexpected load err: %v", err)
	}

	m := &runMode{
		composer: composerState{
			keybindings: kb,
		},
	}
	initRunModeInput(m)
	initRunModeInput(m)

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
	m.composer.keybindings = kbCustom

	_, quitCmd, handledQuit := m.handleGlobalOrModalKeys(tea.Key{Code: 'q', Mod: tea.ModCtrl})
	if !handledQuit || quitCmd == nil {
		t.Errorf("expected Ctrl+Q mapped to cli.exit to be handled with quit cmd")
	}

	// 3. Custom binding for confirm.yes and confirm.no
	kbJSONConfirm := `{"confirm.yes": ["ctrl+y"], "confirm.no": ["ctrl+n"]}`
	tmpConfirm := t.TempDir() + "/keybindings.json"
	_ = os.WriteFile(tmpConfirm, []byte(kbJSONConfirm), 0644)
	kbConfirm, _ := keybind.Load(tmpConfirm)
	m.composer.keybindings = kbConfirm

	m.engine.exitConfirm = true
	_, noCmd, handledNo := m.handleGlobalOrModalKeys(tea.Key{Code: 'n', Mod: tea.ModCtrl})
	if !handledNo || noCmd != nil || m.engine.exitConfirm {
		t.Errorf("expected confirm.no to dismiss exit confirmation")
	}

	m.engine.exitConfirm = true
	_, yesCmd, handledYes := m.handleGlobalOrModalKeys(tea.Key{Code: 'y', Mod: tea.ModCtrl})
	if !handledYes || yesCmd == nil {
		t.Errorf("expected confirm.yes to trigger quit on exit confirmation")
	}

	// 5. Custom suspend, undo, redo, paste, and prompt keybindings
	kbJSONExtended := `{
		"cli.suspend": ["ctrl+z"],
		"edit.undo": ["ctrl+_"],
		"edit.redo": ["ctrl+shift+z"],
		"prompt.insert_newline": ["ctrl+j"],
		"prompt.submit_alternate": ["alt+m"],
		"cli.test": ["ctrl+t"]
	}`
	tmpExt := t.TempDir() + "/keybindings.json"
	_ = os.WriteFile(tmpExt, []byte(kbJSONExtended), 0644)
	kbExt, _ := keybind.Load(tmpExt)
	m.composer.keybindings = kbExt

	// Test cli.suspend
	_, suspendCmd, handledSuspend := m.handleGlobalOrModalKeys(tea.Key{Code: 'z', Mod: tea.ModCtrl})
	if !handledSuspend || suspendCmd == nil {
		t.Errorf("expected Ctrl+Z mapped to cli.suspend to produce Suspend command")
	}

	// Test cli.test
	_, _, handledTest := m.handleGlobalOrModalKeys(tea.Key{Code: 't', Mod: tea.ModCtrl})
	if !handledTest {
		t.Errorf("expected Ctrl+T mapped to cli.test to be handled")
	}

	// Test prompt.insert_newline
	m.composer.input.SetValue("first line")
	_, _, handledNewline := m.handleGlobalOrModalKeys(tea.Key{Code: 'j', Mod: tea.ModCtrl})
	if !handledNewline || m.composer.input.Value() != "first line\n" {
		t.Errorf("expected Ctrl+J mapped to prompt.insert_newline to insert newline, got %q", m.composer.input.Value())
	}
}

func TestDispatchKeyAction_SubagentApproveFastVsJumpToWaiting(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	m.engine.subAgentQuestions.Enqueue(subagentquestions.ParentKey, "Approve operation?", []string{"yes", "no"})

	// 1. subagent.jump_to_waiting should toggle question panel focus without approving
	_, _, handledJump := m.dispatchKeyAction("subagent.jump_to_waiting")
	if !handledJump {
		t.Fatal("expected subagent.jump_to_waiting to be handled")
	}
	if !m.engine.subAgentQuestionPanel.Active() {
		t.Fatal("expected subagent.jump_to_waiting to activate question panel")
	}
	if m.engine.subAgentQuestions.PendingCount(subagentquestions.ParentKey) != 1 {
		t.Fatalf("question should remain pending after jump, got %d", m.engine.subAgentQuestions.PendingCount(subagentquestions.ParentKey))
	}

	// Close panel
	m.engine.subAgentQuestionPanel.Toggle()
	if m.engine.subAgentQuestionPanel.Active() {
		t.Fatal("expected panel to be closed")
	}

	// 2. subagent.approve_fast should fast-approve without activating the question panel
	_, _, handledApprove := m.dispatchKeyAction("subagent.approve_fast")
	if !handledApprove {
		t.Fatal("expected subagent.approve_fast to be handled")
	}
	if m.engine.subAgentQuestionPanel.Active() {
		t.Fatal("subagent.approve_fast must not open or focus the question panel")
	}
	if m.engine.subAgentQuestions.PendingCount(subagentquestions.ParentKey) != 0 {
		t.Fatalf("expected pending questions to be 0 after approve_fast, got %d", m.engine.subAgentQuestions.PendingCount(subagentquestions.ParentKey))
	}
}

func TestDispatchKeyAction_FullReferenceKeybindings(t *testing.T) {
	fullJSON := `{
  "cli.clear_screen": ["ctrl+l"],
  "cli.cycle_mode": ["shift+tab"],
  "cli.enter": ["enter"],
  "cli.escape": ["ctrl+c", "esc"],
  "cli.exit": ["ctrl+d"],
  "cli.suspend": ["ctrl+z"],
  "cli.test": ["ctrl+t"],
  "confirm.no": ["n"],
  "confirm.yes": ["y"],
  "edit.open_editor": ["ctrl+g"],
  "edit.paste": ["alt+v", "ctrl+v"],
  "edit.redo": ["ctrl+shift+z"],
  "edit.undo": ["ctrl+_", "ctrl+shift+-"],
  "edit.yank": ["ctrl+y"],
  "item.delete": ["f4"],
  "item.rename": ["f2"],
  "navigation.down": ["down"],
  "navigation.go_to_bottom": ["ctrl+end"],
  "navigation.go_to_top": ["ctrl+home"],
  "navigation.half_page_down": ["shift+down"],
  "navigation.half_page_up": ["shift+up"],
  "navigation.left": ["left"],
  "navigation.page_down": ["pgdown"],
  "navigation.page_up": ["pgup"],
  "navigation.right": ["right"],
  "navigation.tab": ["tab"],
  "navigation.up": ["up"],
  "prompt.insert_newline": ["alt+enter", "ctrl+j", "shift+enter"],
  "prompt.submit_alternate": ["alt+m"],
  "subagent.approve_fast": ["ctrl+k"],
  "subagent.jump_to_waiting": ["alt+j"],
  "view.review_artifact": ["ctrl+r"],
  "view.toggle_trajectory": ["ctrl+o"],
  "vim.insert.insert_newline": ["alt+enter", "ctrl+j", "enter", "shift+enter"],
  "vim.insert.submit": ["ctrl+enter", "ctrl+s"],
  "vim.normal.submit": ["ctrl+enter", "ctrl+s"],
  "voice.start_dictation": ["f5"]
}`
	tmp := t.TempDir() + "/keybindings.json"
	if err := os.WriteFile(tmp, []byte(fullJSON), 0644); err != nil {
		t.Fatal(err)
	}

	kb, err := keybind.Load(tmp)
	if err != nil {
		t.Fatalf("failed to load full keybindings: %v", err)
	}

	// Verify all 37 action keys are parsed from the reference JSON
	if len(kb.ActionKeys()) != 37 {
		t.Fatalf("parsed %d actions, want 37", len(kb.ActionKeys()))
	}

	// Verify key string normalization and canonical lookup for each
	for action, keys := range kb.ActionKeys() {
		for _, k := range keys {
			resolved := kb.ActionForExactKey(k)
			// Non-vim keys should resolve directly to action
			if resolved == "" {
				t.Errorf("ActionForExactKey(%q) returned empty for action %q", k, action)
			}
		}
	}
}
