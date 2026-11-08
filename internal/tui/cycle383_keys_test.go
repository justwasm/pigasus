// cycle383_keys_test.go — the host's global keys that cycle 383 aligned.
//
// The reference binds Ctrl+R to `view.review_artifact` — it opens the
// Artifact Review panel (official-docs/cli/reference.md:81) — and the
// /permissions panel takes Ctrl+G as the alias of `e`
// (official-docs/cli/commands/permissions.md:41). The host's global key
// pipeline runs before the panel's own handler, so both bindings are the
// host's fact.
//
// The panels themselves are tested in their own packages
// (internal/ui/views/permissionspanel).

package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/permissionspanel"
)

// TestCtrlROpensTheArtifactPanel pins the reference's Ctrl+R: it opens the
// Artifact Review panel, the same panel the /artifact command opens.
func TestCtrlROpensTheArtifactPanel(t *testing.T) {
	m := &RootModel{}
	_, _, handled := m.handleGlobalOrModalKeys(tea.Key{Code: 'r', Mod: tea.ModCtrl})
	if !handled {
		t.Fatal("Ctrl+R was not handled")
	}
	if m.overlays.panel.kind != panelregistry.Artifact {
		t.Errorf("the open panel = %v, want panelregistry.Artifact", m.overlays.panel.kind)
	}
	if m.overlays.panel.cmd != "/artifact" {
		t.Errorf("the open panel's command = %q, want %q", m.overlays.panel.cmd, "/artifact")
	}
}

// TestCtrlRIsIgnoredWhileAPanelIsOpen pins the reference's table: Ctrl+R is
// a prompt-focus key, so an open list panel keeps it and the Artifact
// Review panel does not open.
func TestCtrlRIsIgnoredWhileAPanelIsOpen(t *testing.T) {
	m := &RootModel{}
	if !m.openSlashPanel("context") {
		t.Fatal("openSlashPanel(context) = false, want true")
	}
	m.handleGlobalOrModalKeys(tea.Key{Code: 'r', Mod: tea.ModCtrl})
	if m.overlays.panel.kind != panelregistry.Context {
		t.Errorf("the open panel = %v, want panelregistry.Context", m.overlays.panel.kind)
	}
}

// TestCtrlGReachesThePermissionsPanel pins the /permissions panel's own
// Ctrl+G: the host's global handler must not swallow the key for the
// $EDITOR command while the panel is up.
func TestCtrlGReachesThePermissionsPanel(t *testing.T) {
	m := &RootModel{}
	if !m.openSlashPanel("permissions") {
		t.Fatal("openSlashPanel(permissions) = false, want true")
	}
	// Leave the scope picker: the row actions live in the tab view. The
	// panel's keys are the host's second stage — the global handlers run
	// first.
	if _, _, handled := m.handleFunctionAndModeKeys(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter was not handled by the /permissions panel")
	}
	_, cmd, handled := m.handleGlobalOrModalKeys(tea.Key{Code: 'g', Mod: tea.ModCtrl})
	if !handled {
		t.Fatal("Ctrl+G was not handled")
	}
	if cmd != nil {
		t.Error("Ctrl+G asked for the $EDITOR command while the /permissions panel was up")
	}
	if got := m.permissionsPanel().Stage(); got != permissionspanel.StageForm {
		t.Errorf("the panel's stage = %d, want %d (the rule form)", got, permissionspanel.StageForm)
	}
}
