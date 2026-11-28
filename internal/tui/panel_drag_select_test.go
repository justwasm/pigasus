// panel_drag_select_test.go — dragging across a panel's body selects its
// text, in both directions, from any row.
//
// Two defects are pinned here, and they had the same cause: a body row's
// action ran on the *press*. The press therefore claimed the gesture, so a
// drag could not begin on any body row of the Wire or Trajectory view — the
// text under the pointer could only be dragged from some other row. The
// action now waits for the release, so one press can be either a click or
// the anchor of a drag.

package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/artifactpanel"
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
)

// newArtifactViewer builds the /artifact detail viewer over a 40-line
// document, which is the surface the report named.
func newArtifactViewer(t *testing.T) *RootModel {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.md")
	var b strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&b, "line %d of the artifact body\n", i)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &RootModel{}
	m.overlays.panel.kind = panelregistry.Artifact
	m.overlays.panel.cmd = "/artifact"
	m.viewport.width = 120
	m.viewport.height = 30
	m.viewport.altScreen = true
	m.overlays.panelScreenY = 0
	m.overlays.panel.art = artifactpanel.New(artifactpanel.Seams{
		Refs: func() []artifactpanel.Artifact {
			return []artifactpanel.Artifact{{Name: "plan", Path: path}}
		},
		Size: func() (int, int) { return 120, 30 },
		Read: func(p string) (string, bool) {
			b, err := os.ReadFile(p)
			return string(b), err == nil
		},
	})
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter did not open the artifact viewer")
	}
	_ = m.panelView()
	return m
}

// dragPanelBody presses a body row and drags to another with the pointer
// column fixed, through Update(), and answers the text the drag selected.
func dragPanelBody(t *testing.T, m *RootModel, fromRow, toRow int) (string, bool) {
	t.Helper()
	const col = 12
	fromY := m.overlays.panelScreenY + fromRow
	toY := m.overlays.panelScreenY + toRow

	m.Update(tea.MouseClickMsg(tea.Mouse{X: col, Y: fromY, Button: tea.MouseLeft}))
	if !m.overlays.panelSelection.SelDragging {
		return "", false
	}
	step := 1
	if toY < fromY {
		step = -1
	}
	for y := fromY + step; ; y += step {
		m.Update(tea.MouseMotionMsg(tea.Mouse{X: col, Y: y, Button: tea.MouseLeft}))
		if y == toY {
			break
		}
	}
	return m.panelSelectionText(), true
}

// TestWireBodyDragSelectsInBothDirections — the Wire view's body is text,
// and a drag across it selects whether the pointer moves down or up.
func TestWireBodyDragSelectsInBothDirections(t *testing.T) {
	for _, dir := range []struct {
		name     string
		from, to int
	}{{"down", 8, 13}, {"up", 13, 8}} {
		t.Run(dir.name, func(t *testing.T) {
			m := mkPanel(panelregistry.Debug, "/debug", 0)
			m.viewport.width = 120
			m.viewport.height = 30
			m.viewport.altScreen = true
			m.overlays.panelScreenY = 0
			m.debugPanel().SetView(debugpanel.ViewWire)
			m.View()

			text, started := dragPanelBody(t, m, dir.from, dir.to)
			if !started {
				t.Fatalf("a press on a Wire body row did not start a drag")
			}
			if strings.TrimSpace(text) == "" {
				t.Errorf("a %s drag across the Wire body selected nothing", dir.name)
			}
		})
	}
}

// TestTrajectoryBodyDragStartsADrag — the Trajectory view answers a press
// on its body the same way, which is the part that was broken: the press
// must start a drag instead of running the row's action.
//
// The fixture's session has no steps, so the frame's body is blank and
// there is no text to copy. What is pinned is the press: before the fix it
// was claimed by the fold and left SelDragging false, so no drag could
// begin on a body row at all.
func TestTrajectoryBodyDragStartsADrag(t *testing.T) {
	m := mkPanel(panelregistry.Debug, "/debug", 0)
	m.viewport.width = 120
	m.viewport.height = 30
	m.viewport.altScreen = true
	m.overlays.panelScreenY = 0
	m.debugPanel().SetView(debugpanel.ViewTrajectory)
	m.View()

	const col = 12
	fromY := m.overlays.panelScreenY + 8
	m.Update(tea.MouseClickMsg(tea.Mouse{X: col, Y: fromY, Button: tea.MouseLeft}))
	if !m.overlays.panelSelection.SelDragging {
		t.Fatal("a press on a Trajectory body row did not start a drag")
	}
	m.Update(tea.MouseMotionMsg(tea.Mouse{X: col, Y: fromY + 5, Button: tea.MouseLeft}))
	if got := m.overlays.panelSelection.SelEndRow; got != fromY+5-m.overlays.panelScreenY {
		t.Errorf("the drag's end row is %d, want %d", got, fromY+5-m.overlays.panelScreenY)
	}
}

// TestArtifactDragSelectsInBothDirections — the /artifact viewer is the
// surface the report named, and its body is a plain document.
func TestArtifactDragSelectsInBothDirections(t *testing.T) {
	for _, dir := range []struct {
		name     string
		from, to int
	}{{"down", 3, 8}, {"up", 8, 3}} {
		t.Run(dir.name, func(t *testing.T) {
			m := newArtifactViewer(t)
			_ = m.View()

			text, started := dragPanelBody(t, m, dir.from, dir.to)
			if !started {
				t.Fatalf("a press on the artifact body did not start a drag")
			}
			if strings.TrimSpace(text) == "" {
				t.Errorf("a %s drag across the artifact body selected nothing", dir.name)
			}
		})
	}
}

// TestCopiedArtifactOmitsTheLineNumberGutter — the gutter is the viewer's
// furniture, so a drag that began at the left edge must not paste the
// numbers.
func TestCopiedArtifactOmitsTheLineNumberGutter(t *testing.T) {
	m := newArtifactViewer(t)
	_ = m.View()

	// Anchor at column 0, which is inside the gutter, and drag down.
	m.updateMouseClick(tea.Mouse{X: 0, Y: 2, Button: tea.MouseLeft})
	m.updateMouseMotion(tea.Mouse{X: 40, Y: 4, Button: tea.MouseLeft})
	text := m.panelSelectionText()
	if text == "" {
		t.Fatal("the gutter-anchored drag selected nothing")
	}
	if strings.Contains(text, "2      ") || strings.HasPrefix(text, " ") {
		t.Errorf("the copy carries the line-number gutter:\n%q", text)
	}
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "line ") {
			continue
		}
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		t.Errorf("a copied row is not the document's text: %q", line)
	}
}
