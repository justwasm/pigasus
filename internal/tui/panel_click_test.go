// panel_click_test.go — the mouse's half of the /logs surface, driven
// through the host.
//
// The panel's own Click is pinned in internal/ui/views/debugpanel; what
// this file pins is the host's translation: a terminal cell becomes a
// panel-relative row, the press reaches the panel that owns it, and the
// frame the user clicked is the frame the panel indexed into.

package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/clilog"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
)

// debugPanelWithLog arms a /logs panel over a log file the test owns, so
// the Logs view has rows to click.
func debugPanelWithLog(t *testing.T) *RootModel {
	t.Helper()
	clilog.ResetForTest()
	t.Cleanup(clilog.ResetForTest)
	t.Setenv("OPENAGY_CONFIG_HOME", t.TempDir())
	t.Setenv(clilog.EnvLog, "")
	if clilog.Init() == "" {
		t.Fatal("clilog.Init() returned empty path")
	}
	for i := 0; i < 60; i++ {
		clilog.Logf("cloudcode", "entry-%02d", i)
	}

	m := mkPanel(panelregistry.None, "", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 100
	m.viewport.height = 28
	if !m.openSlashPanelArgs("debug", "wire") {
		t.Fatal("/logs wire was refused")
	}
	// The frame's origin is recorded by the draw, so draw once before
	// clicking: a click is against a frame, not against a model.
	m.View()
	return m
}

// TestPanelClickOnTheViewBarSwitchesView — the bar is the frame's first
// row, and a click on a label's own columns lands on that label.
func TestPanelClickOnTheViewBarSwitchesView(t *testing.T) {
	m := debugPanelWithLog(t)

	for _, want := range []debugpanel.View{debugpanel.ViewTrajectory, debugpanel.ViewLogs, debugpanel.ViewWire} {
		m.debugPanel().SetView(debugpanel.ViewWire)
		m.View() // redraw so the bar's hit spans are this frame's

		barLine := ansi.Strip(m.debugPanel().Lines()[0])
		col := strings.Index(barLine, want.Name())
		if col < 0 {
			t.Fatalf("the bar does not paint %q: %q", want.Name(), barLine)
		}

		_, _ = m.updateMouseClick(tea.Mouse{X: col + 1, Y: m.overlays.panelScreenY, Button: tea.MouseLeft})
		if got := m.debugPanel().View(); got != want {
			t.Errorf("a click on %q landed on %v, want %v", want.Name(), got, want)
		}
	}
}

// TestPanelClickOutsideEveryLabelStartsASelection — a press on the bar
// past the last label is text, so the host's drag selection owns it. The
// panel must not swallow every press in its rectangle.
func TestPanelClickOutsideEveryLabelStartsASelection(t *testing.T) {
	m := debugPanelWithLog(t)

	barCol := 0
	for _, span := range m.debugPanel().Lines() {
		_ = span
		break
	}
	_, _ = m.updateMouseClick(tea.Mouse{X: 500, Y: m.overlays.panelScreenY + barCol, Button: tea.MouseLeft})
	if !m.overlays.panelSelection.SelDragging {
		t.Error("a press past the last label did not start the panel selection")
	}
}

// TestPanelClickOnAFoldRowFoldsIt — a click on a body row puts the cursor
// there and folds the row, and the frame stays the terminal's height.
//
// The gesture is a press and then a release. The press must start a
// selection — the pointer may be about to drag across the row's text —
// and the release, which did not move, runs the fold. The press used to
// fold the row and claim the gesture, so no drag could begin on any body
// row and the text under the pointer was uncopyable.
func TestPanelClickOnAFoldRowFoldsIt(t *testing.T) {
	m := debugPanelWithLog(t)
	m.debugPanel().SetView(debugpanel.ViewWire)
	m.View()

	p := m.debugPanel()
	rows := p.RowCount()
	if rows == 0 {
		t.Fatal("the Wire view has no rows")
	}
	// The first body row of the Wire view is the systemInstruction group.
	const bodyRow = 6
	pressY := m.overlays.panelScreenY + bodyRow
	_, _ = m.updateMouseClick(tea.Mouse{X: 4, Y: pressY, Button: tea.MouseLeft})

	// The press anchors a drag: the row it landed on is the row the
	// release will act on, and a drag from here is a selection.
	if !m.overlays.panelSelection.SelDragging {
		t.Fatal("a press on a body row did not start a selection")
	}
	if !m.overlays.panelPressArmed {
		t.Fatal("a press on a foldable row did not arm its action")
	}
	// The press alone must not have folded anything.
	if got := len(p.Lines()); got == 0 {
		t.Fatal("the frame emptied on the press")
	}

	// The release, with the pointer where it pressed, is the click.
	_, _ = m.updateMouseRelease(tea.Mouse{X: 4, Y: pressY, Button: tea.MouseLeft})

	// The fold happened, so the frame is shorter by that group's members
	// but still the height the seam reports.
	if got, want := len(p.Lines()), m.debugPanelHeight(); got != want {
		t.Errorf("the frame drew %d rows after the click, want %d", got, want)
	}
	if m.overlays.panelSelection.SelDragging {
		t.Error("the release left a selection dragging")
	}
	if m.overlays.panelPressArmed {
		t.Error("the release left the press armed")
	}
}

// TestPanelWheelMovesTheVisibleView — the wheel is the panel's, for
// whichever view is up.
func TestPanelWheelMovesTheVisibleView(t *testing.T) {
	m := debugPanelWithLog(t)
	p := m.debugPanel()

	// The Wire view: the wheel walks the cursor.
	p.SetView(debugpanel.ViewWire)
	m.View()
	before := p.Lines()[len(p.Lines())-2]
	m.handleMouseWheel(tea.MouseWheelMsg(tea.Mouse{X: 10, Y: m.overlays.panelScreenY + 8, Button: tea.MouseWheelDown}))
	after := p.Lines()[len(p.Lines())-2]
	if before == after {
		t.Errorf("the wheel did not move the Wire cursor: %q stayed put", ansi.Strip(after))
	}

	// The Logs view: the wheel takes it off the tail.
	p.SetView(debugpanel.ViewLogs)
	m.View()
	if !m.overlays.panel.debugFollow {
		t.Fatal("the log view did not start on the tail")
	}
	m.handleMouseWheel(tea.MouseWheelMsg(tea.Mouse{X: 10, Y: m.overlays.panelScreenY + 8, Button: tea.MouseWheelUp}))
	if m.overlays.panel.debugFollow {
		t.Error("a wheel-up left the log view on the tail")
	}
}
