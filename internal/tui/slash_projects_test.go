package tui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

func TestSlashProjects_PanelRouting(t *testing.T) {
	// Verify /projects is registered in slashcommands
	cmd, ok := slashcommands.Find("/projects")
	if !ok {
		t.Fatal("slash command /projects not found")
	}
	if cmd.Panel != panelregistry.Projects {
		t.Fatalf("cmd.Panel = %v, want %v", cmd.Panel, panelregistry.Projects)
	}

	kind, ok := slashcommands.PanelFor("projects")
	if !ok || kind != panelregistry.Projects {
		t.Fatalf("PanelFor(projects) = (%v, %v), want (%v, true)", kind, ok, panelregistry.Projects)
	}

	m := mkPanel(panelregistry.Projects, "/projects", 0)
	if m.overlays.panel.kind != panelregistry.Projects {
		t.Fatalf("m.overlays.panel.kind = %v, want %v", m.overlays.panel.kind, panelregistry.Projects)
	}

	// Test Lines returns rendered content
	lines := m.projectsPanel().Lines(80)
	if len(lines) == 0 {
		t.Fatal("projectsPanel.Lines(80) returned empty")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Projects") {
		t.Fatalf("expected 'Projects' in lines, got:\n%s", joined)
	}
}
