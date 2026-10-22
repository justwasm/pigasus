package main

import (
	"fmt"
)

// ---- Cycle 201: scrollable viewers ----------------------------------------

// viewerSpec describes a scrollable documentary panel: the document lines,
// how many of them fit in the panel's window, and the total line count the
// footer's "(a–b of K lines)" counter quotes.
type viewerSpec struct {
	lines   []string
	visible int
	total   int
}

// viewerSpecFor returns the viewer spec for the open panel, or ok=false
// when the panel is not a document viewer.
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

// viewerWindow returns the rows of spec that the current scroll offset
// shows, plus the 1-based [start,end] range the counter quotes. It clamps
// the offset into [0, len(lines)-visible] so a scrolled window is always
// full of content.
func (m *runMode) viewerWindow(spec viewerSpec) (rows []string, start, end int) {
	n := len(spec.lines)
	vis := spec.visible
	if vis < 1 {
		vis = 1
	}
	maxOffset := n - vis
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if m.panel.window > maxOffset {
		m.panel.window = maxOffset
	}
	hi := m.panel.window + vis
	if hi > n {
		hi = n
	}
	rows = spec.lines[m.panel.window:hi]
	start = m.panel.window + 1
	end = m.panel.window + vis
	if end > n {
		end = n
	}
	if end < start {
		end = start
	}
	return rows, start, end
}

// viewerCounter renders the dim gray "(a–b of K lines)" row every viewer
// footer carries, with the same en-dash REF uses.
func viewerCounter(start, end, total int) string {
	return cDimGrayOpen + fmt.Sprintf("  (%d\u2013%d of %d lines)", start, end, total) + cDimGrayClose
}

// handlePanelKey routes a key to the open panel. Returns (handled, cmd)
// so the caller can return early without falling through to the prompt.
//
// Cycle 201 wires up the key actions the reference's footers announce:
//
//	/usage  /changelog   ↑/↓ line, pgup/pgdown page, ctrl+home/end ends
//	/plugin              tab cycles Installed ⇄ Discover
//	/skills              enter opens the per-skill detail, esc returns
//	/diff                ←/→ switch the active tab
//	/feedback            enter (or 1-6) advances to the free-text stage
//
// Caveat (routing, not this function): tui_run.go runs the transcript
// scroll bindings (pgup / pgdown / ctrl+home / ctrl+end) before the panel
// branch, and those consume the keys while alt-screen mode is on (the
// default). The viewer handles the full set regardless, so the page/end
// bindings work under --no-alt-screen and would work for panels too if
// the routing order changed.



