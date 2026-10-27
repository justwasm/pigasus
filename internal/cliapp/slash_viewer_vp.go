// slash_viewer_vp.go — viewport.Model integration for viewer panels.
//
// The three isViewer() panels (panelUsage, panelChangelog, panelFeatDiff)
// delegate scroll key handling to charm.land/bubbles/v2/viewport.Model.
// This gives us the standard pager keybindings (j/k, space/b, d/u, f,
// pgup/pgdown, arrows) for free via viewport.DefaultKeyMap. We add g/G
// (top/bottom) and q/esc (cancel) around the viewport delegation.
//
// The viewport owns scroll state; after each Update we sync
// panel.window = vp.YOffset() so existing rendering code (viewerWindow,
// panelUsageLinesREF) keeps working unchanged. Content is loaded into
// the viewport on panel open (initViewerViewport) and on each render
// cycle if the document is dynamic (usage countdown).

package cliapp

import (
	"io"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/featdiff"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/internal/tuiviewer"
)

// initViewerViewport configures the viewport for the open viewer panel.
// Called from openSlashPanel after the panel kind is set.
func (m *runMode) initViewerViewport() {
	spec, ok := m.viewerSpecFor()
	if !ok {
		return
	}
	m.panel.vp = viewport.New(
		viewport.WithWidth(m.viewerVPWidth()),
		viewport.WithHeight(spec.visible),
	)
	m.panel.vp.SetContentLines(spec.lines)
	// Add g/G bindings for top/bottom navigation.
	m.panel.vp.KeyMap = viewerKeyMap()
	// Disable mouse wheel — the TUI handles mouse events separately.
	m.panel.vp.MouseWheelEnabled = false
}

// viewerVPWidth returns the width to use for the viewport. The viewer
// panels render full-terminal-width lines, so the viewport width is set
// to a generous value that won't clip content.
func (m *runMode) viewerVPWidth() int {
	if m.width > 0 {
		return m.width
	}
	return 200 // fallback for tests where width is 0
}

// viewerKeyMap returns the viewport keymap with g/G added for
// top/bottom navigation. The default keymap already binds:
//   - j/k/↑/↓ for line scroll
//   - space/f/pgdown for page down, b/pgup for page up
//   - d/ctrl+d for half page down, u/ctrl+u for half page up
//   - h/l/←/→ for horizontal scroll
func viewerKeyMap() viewport.KeyMap {
	km := viewport.DefaultKeyMap()
	return km
}

func (m *runMode) viewerPageDown() {
	spec, ok := m.viewerSpecFor()
	if ok && m.height == 0 && m.panel.kind == panelChangelog {
		page := spec.visible - 1
		if page < 1 {
			page = 1
		}
		m.panel.vp.ScrollDown(page)
	} else {
		m.panel.vp.PageDown()
	}
}

func (m *runMode) viewerPageUp() {
	spec, ok := m.viewerSpecFor()
	if ok && m.height == 0 && m.panel.kind == panelChangelog {
		page := spec.visible - 1
		if page < 1 {
			page = 1
		}
		m.panel.vp.ScrollUp(page)
	} else {
		m.panel.vp.PageUp()
	}
}

func (m *runMode) handleViewerNavAction(action tuilib.NavAction) (bool, tea.Cmd, bool) {
	switch action {
	case tuilib.NavTop:
		m.panel.vp.GotoTop()
	case tuilib.NavBottom:
		m.panel.vp.GotoBottom()
	case tuilib.NavCancel:
		return true, m.closePanel(), true
	case tuilib.NavPageDown:
		m.viewerPageDown()
	case tuilib.NavPageUp:
		m.viewerPageUp()
	case tuilib.NavHalfPageDown:
		m.panel.vp.HalfPageDown()
	case tuilib.NavHalfPageUp:
		m.panel.vp.HalfPageUp()
	case tuilib.NavDown:
		m.panel.vp.ScrollDown(1)
	case tuilib.NavUp:
		m.panel.vp.ScrollUp(1)
	default:
		return false, nil, false
	}
	m.panel.window = m.panel.vp.YOffset()
	return true, nil, true
}

// handleViewerPanelKeyVP is the viewport-backed replacement for
// handleViewerPanelKey. It delegates scroll keys to vp.Update, handles
// g/G (top/bottom) and q/esc (cancel) externally, and syncs
// panel.window after each update.
func (m *runMode) handleViewerPanelKeyVP(key tea.Key) (bool, tea.Cmd) {
	// Refresh the viewport content and height for dynamic documents
	// (e.g. /usage countdown ticks).
	m.syncViewerViewport()

	action := tuilib.ClassifyViewerNavKey(key)
	if handled, cmd, ok := m.handleViewerNavAction(action); ok {
		return handled, cmd
	}

	// Delegate to the viewport for any other key events (e.g. horizontal scroll).
	updated, cmd := m.panel.vp.Update(tea.KeyPressMsg(key))
	m.panel.vp = updated
	m.panel.window = m.panel.vp.YOffset()

	if key.Mod == 0 {
		return true, cmd
	}
	return false, cmd
}

// syncViewerViewport refreshes the viewport's content and dimensions
// to match the current viewerSpec. This handles dynamic content like
// the /usage countdown and terminal resizes.
func (m *runMode) syncViewerViewport() {
	spec, ok := m.viewerSpecFor()
	if !ok {
		return
	}
	m.panel.vp.SetHeight(spec.visible)
	m.panel.vp.SetWidth(m.viewerVPWidth())
	m.panel.vp.SetContentLines(spec.lines)
	if m.panel.vp.YOffset() != m.panel.window {
		m.panel.vp.SetYOffset(m.panel.window)
	}
}

type viewerSpec struct {
	lines   []string
	visible int
	total   int
}

func fromViewerSpec(s tuiviewer.Spec) viewerSpec {
	return viewerSpec{lines: s.Lines, visible: s.Visible, total: s.Total}
}

func toViewerSpec(s viewerSpec) tuiviewer.Spec {
	return tuiviewer.Spec{Lines: s.lines, Visible: s.visible, Total: s.total}
}

func (m *runMode) usageViewerSpec() viewerSpec {
	if m.height > 0 {
		return m.usageViewerSpecREF()
	}
	lines := usageViewerLinesStatic()
	return viewerSpec{lines: lines, visible: 17, total: 20}
}

func (m *runMode) changelogViewerSpec() viewerSpec {
	if m.height > 0 {
		vis := m.height - 4
		if vis < 1 {
			vis = 1
		}
		return viewerSpec{lines: m.changelogViewerLines(), visible: vis, total: changelogTotalLines}
	}
	lines := m.changelogViewerLines()
	return viewerSpec{lines: lines, visible: changelogWindowRows(), total: changelogTotalLines}
}

func (m *runMode) viewerSpecFor() (viewerSpec, bool) {
	switch m.panel.kind {
	case panelUsage:
		return m.usageViewerSpec(), true
	case panelChangelog:
		return m.changelogViewerSpec(), true
	case panelFeatDiff:
		return m.featDiffViewerSpec(), true
	}
	return viewerSpec{}, false
}

func (m *runMode) viewerWindow(spec viewerSpec) (rows []string, start, end int) {
	r, s, e, clamped := tuiviewer.Window(toViewerSpec(spec), m.panel.window)
	m.panel.window = clamped
	return r, s, e
}

func viewerCounter(start, end, total int) string {
	return tuiviewer.Counter(start, end, total)
}

// runFeatDiffCLI is the `--diff-with-agy` CLI handler.
func runFeatDiffCLI(w io.Writer, mode string) {
	featdiff.RunCLI(w, mode)
}

func featDiffPanelTitle() string {
	return featdiff.PanelTitle()
}

func (m *runMode) featDiffPanelLines() []string {
	t := featdiff.SourceData()
	if t == nil {
		return []string{
			featDiffPanelTitle(),
			"",
			cDimGrayOpen + "  data file not found (run from the project root)." + cDimGrayClose,
			"",
			panelDotHint("  ", "esc", "Close"),
		}
	}
	return featdiff.PanelDoc(t)
}

func (m *runMode) featDiffViewerSpec() viewerSpec {
	t := featdiff.SourceData()
	if t == nil {
		return viewerSpec{}
	}
	doc := featdiff.PanelDoc(t)
	vis := 17
	if m.height > 0 {
		vis = m.height - 4
		if vis < 1 {
			vis = 1
		}
	}
	return viewerSpec{lines: doc, visible: vis, total: len(doc)}
}

func (m *runMode) handleFeatDiffPanelKey(key tea.Key) (bool, tea.Cmd) {
	if !m.panelFeatDiffOpen() {
		return false, nil
	}
	return m.handleViewerPanelKey(key)
}

func (m *runMode) panelFeatDiffOpen() bool {
	return m.panel.kind == panelFeatDiff
}
