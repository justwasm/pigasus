package tui

import (
	"flag"
	"testing"
)

func TestRootModel_PluginReloadMsg_RefreshesTools(t *testing.T) {
	ws := t.TempDir()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	m := newRootModel(nil, "user@test", "gemini-2.5-pro", "tier1", ws, false)
	setupTUIToolsAndPolicy(m, fs)

	initialTools := len(m.tools.decls)
	if initialTools == 0 {
		t.Fatalf("expected initial tools to be populated, got %d", initialTools)
	}

	// Send pluginReloadMsg to Update via updateMouseAndMiscMsg
	_, cmd, handled := m.updateMouseAndMiscMsg(pluginReloadMsg{})
	if !handled {
		t.Fatalf("expected pluginReloadMsg to be handled by updateMouseAndMiscMsg")
	}
	if cmd != nil {
		t.Fatalf("expected cmd to be nil, got %v", cmd)
	}

	// Verify tools catalog is valid and active
	if m.tools.catalog == nil {
		t.Fatalf("expected tools catalog to be non-nil after reload")
	}
	if len(m.tools.decls) == 0 {
		t.Fatalf("expected tools decls to be non-empty after reload")
	}
}
