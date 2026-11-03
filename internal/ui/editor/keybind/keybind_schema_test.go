package keybind

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_ActionToListOfKeysSchema(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "keybindings.json")

	content := `{
		"vim.insert.submit": ["ctrl+enter", "ctrl+s"],
		"vim.normal.submit": ["ctrl+enter", "ctrl+s"],
		"vim.insert.insert_newline": ["alt+enter", "ctrl+j", "enter", "shift+enter"],
		"cli.exit": ["ctrl+d"]
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	kb, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if act := kb.ActionForExactKey("ctrl+d"); act != "cli.exit" {
		t.Errorf("ActionForExactKey(ctrl+d) = %q, want cli.exit", act)
	}

	if act := kb.ActionForExactKey("ctrl+s"); act != "vim.insert.submit" && act != "vim.normal.submit" {
		t.Errorf("ActionForExactKey(ctrl+s) = %q, want vim submit", act)
	}

	keys := kb.KeysForAction("vim.insert.insert_newline")
	if len(keys) != 4 {
		t.Errorf("len(KeysForAction(vim.insert.insert_newline)) = %d, want 4", len(keys))
	}
}
