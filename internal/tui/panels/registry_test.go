package panels

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
)

type mockHost struct {
	height int
	width  int
	panels map[panelregistry.Kind]any
}

func (m *mockHost) TerminalHeight() int { return m.height }
func (m *mockHost) TerminalWidth() int  { return m.width }
func (m *mockHost) Panel(k panelregistry.Kind) any {
	if m.panels != nil {
		return m.panels[k]
	}
	return nil
}
func (m *mockHost) ContextState() contextpanel.State    { return contextpanel.State{} }
func (m *mockHost) PanelUsageLines() []string           { return []string{"usage row"} }
func (m *mockHost) UsageViewerSpec() tuiviewer.Spec     { return tuiviewer.Spec{Total: 10} }
func (m *mockHost) PanelChangelogLines() []string       { return []string{"changelog row"} }
func (m *mockHost) ChangelogViewerSpec() tuiviewer.Spec { return tuiviewer.Spec{Total: 20} }

type mockInteractivePanel struct {
	lines   []string
	handled bool
	rows    int
}

func (p *mockInteractivePanel) Lines() []string { return p.lines }
func (p *mockInteractivePanel) HandleKey(tea.Key) (bool, tea.Cmd) {
	p.handled = true
	return true, nil
}
func (p *mockInteractivePanel) RowCount() int { return p.rows }

type mockSizedPanel struct {
	lines   []string
	handled bool
	h, w    int
	rows    int
}

func (p *mockSizedPanel) Lines(h, w int) []string {
	p.h = h
	p.w = w
	return p.lines
}
func (p *mockSizedPanel) HandleKey(tea.Key) (bool, tea.Cmd) {
	p.handled = true
	return true, nil
}
func (p *mockSizedPanel) RowCount() int { return p.rows }

type mockWidthPanel struct {
	lines   []string
	handled bool
	w       int
	rows    int
}

func (p *mockWidthPanel) Lines(w int) []string {
	p.w = w
	return p.lines
}
func (p *mockWidthPanel) HandleKey(tea.Key) (bool, tea.Cmd) {
	p.handled = true
	return true, nil
}
func (p *mockWidthPanel) RowCount() int { return p.rows }

func TestBuildRegistry_DeclarativeCoverage(t *testing.T) {
	h := &mockHost{
		height: 40,
		width:  100,
		panels: map[panelregistry.Kind]any{
			panelregistry.Tasks: &mockInteractivePanel{
				lines: []string{"task 1", "task 2"},
				rows:  2,
			},
			panelregistry.Config: &mockSizedPanel{
				lines: []string{"cfg 1"},
				rows:  1,
			},
			panelregistry.Plugin: &mockWidthPanel{
				lines: []string{"plg 1"},
				rows:  3,
			},
		},
	}

	reg := BuildRegistry(h)
	if reg == nil {
		t.Fatal("BuildRegistry returned nil")
	}

	// Verify all registered panel kinds exist in registry
	for _, spec := range PanelSpecs {
		handler, ok := reg.Get(spec.Kind)
		if !ok || handler == nil {
			t.Errorf("panel %v not registered in registry", spec.Kind)
		}
	}

	// Verify standard interactive panel routing
	taskHandler, ok := reg.Get(panelregistry.Tasks)
	if !ok {
		t.Fatal("Tasks panel handler not found")
	}
	lines := taskHandler.Lines()
	if len(lines) != 2 || lines[0] != "task 1" {
		t.Errorf("Tasks lines = %v, want [task 1, task 2]", lines)
	}
	handled, _ := taskHandler.HandleKey(tea.Key{})
	if !handled {
		t.Errorf("Tasks HandleKey not handled")
	}
	if rc, ok := taskHandler.(panelregistry.RowCounter); !ok || rc.RowCount() != 2 {
		t.Errorf("Tasks RowCount = %v (ok=%v), want 2", rc, ok)
	}

	// Verify sized panel routing
	cfgHandler, ok := reg.Get(panelregistry.Config)
	if !ok {
		t.Fatal("Config panel handler not found")
	}
	cfgLines := cfgHandler.Lines()
	if len(cfgLines) != 1 || cfgLines[0] != "cfg 1" {
		t.Errorf("Config lines = %v, want [cfg 1]", cfgLines)
	}
	cfgMock := h.panels[panelregistry.Config].(*mockSizedPanel)
	if cfgMock.h != 40 || cfgMock.w != 100 {
		t.Errorf("Config size = (%d, %d), want (40, 100)", cfgMock.h, cfgMock.w)
	}

	// Verify viewer panels
	usageHandler, ok := reg.Get(panelregistry.Usage)
	if !ok {
		t.Fatal("Usage panel handler not found")
	}
	uSpec, ok := usageHandler.ViewerSpec()
	if !ok || uSpec.Total != 10 {
		t.Errorf("Usage ViewerSpec = %+v, ok=%v, want total 10", uSpec, ok)
	}
}
