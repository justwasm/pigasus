package main

import (
	"github.com/justwasm/pigasus/internal/auth"
)

// keep it on a row index that no longer exists.
func (m *runMode) pluginRowCount() int {
	if m.panel.pluginTab == panelDiscover {
		return m.pluginDiscoverCount()
	}
	if m.panel.pluginSearch {
		return m.pluginSearchCount()
	}
	return len(installedPluginRowsFiltered(m.panel.pluginEnabledOnly))
}

// togglePluginEnabled flips the highlighted Installed row's
// plugins.<name>.enabled flag in ~/.gemini/config/config.json (probed: the
// toggle persists across launches; the badge re-reads the flag each frame).
//
// Cycle psif: the toggle acts on the row the cursor visually points at.
// That is the full installed list while the box is unfocused, and the
// filter result once the typed query has been committed (Enter) — see
// installedActiveRows.
func (m *runMode) togglePluginEnabled() {
	rows := m.installedActiveRows()
	i := m.pluginCursorFor(len(rows))
	if i >= len(rows) {
		return
	}
	name := rows[i].name
	on, err := auth.PluginEnabled(name)
	if err != nil {
		return
	}
	_ = auth.SetPluginEnabled(name, !on)
}

// uninstallHighlightedPlugin removes the plugin under the cursor on the
// Installed tab. It is the ctrl+s handler — the legend has listed the
// action since cycle 198, but this commit is the first to wire the
// dispatcher to a real path.
//
// Steps:
//
//  1. Read the row list (the cycle-198 reader; the disabled-filter and
//     the typed-search filter compose on top of it but a ctrl+s on a
//     visible row targets that row directly — so we read the same set
//     the cursor is pointing at, not the unfiltered disk scan).
//  2. Resolve the highlighted name.
//  3. Call uninstallFeedback (which runs validatePluginID first). The
//     return string is the message we paint in the panel title — empty
//     on success means the title reverts to its cycle-198 form.
//
// On success the directory disappears, config.json's row is cleared,
// and the next frame's installedPluginRowsFiltered call produces a
// shorter list; the cursor is clamped to row 0 by pluginCursorFor.
func (m *runMode) uninstallHighlightedPlugin() (string, error) {
	rows := installedPluginRows()
	i := m.pluginCursorFor(len(rows))
	if i >= len(rows) {
		// Cursor lands in an empty list; the keystroke is a no-op.
		return "", nil
	}
	name := rows[i].name
	msg, err := uninstallFeedback(name)
	if err != nil && msg == "" {
		// Defensive: uninstallFeedback always returns a non-empty
		// message on error. If it does not, surface the bare error so
		// the panel still says *something* went wrong.
		return "Error: " + err.Error(), err
	}
	// Drop any cached detail-block expansion so the next render does
	// not leave a stale "Description:" / "Skills:" block under a row
	// that no longer exists.
	if m.panel.pluginExpanded != nil {
		delete(m.panel.pluginExpanded, name)
	}
	return msg, err
}

// togglePluginExpanded shows/hides the detail block under the highlighted
// Installed row. Expansion is tracked per plugin name, not by cursor index:
// the reference leaves a block open when the cursor moves away (probed on the
// Discover tab, where the cursor jumped back to row 0 with a block still
// open).
//
// Cycle psif: same active-rows rule as togglePluginEnabled — once a
// focused-mode commit has narrowed the list, Enter on a committed
// Installed box opens the block under the filtered row the cursor is
// parked on, not the same-index row of the full list.
func (m *runMode) togglePluginExpanded() {
	rows := m.installedActiveRows()
	i := m.pluginCursorFor(len(rows))
	if i >= len(rows) {
		return
	}
	name := rows[i].name
	if m.panel.pluginExpanded == nil {
		m.panel.pluginExpanded = map[string]bool{}
	}
	if m.panel.pluginExpanded[name] {
		delete(m.panel.pluginExpanded, name)
		return
	}
	m.panel.pluginExpanded[name] = true
}

// installedActiveRows is the Installed-tab row list the cursor
// currently indexes. With no focused-mode commit it is the full
// installed list; once the typed query has been committed (Enter) it
// is the filter result, so a Space / Enter on the focused state acts
// on the row the cursor visually points at.
//
// Cycle psif: previously both togglePluginEnabled and
// togglePluginExpanded read installedPluginRows() unconditionally,
// which indexed the cursor into the *full* list even after a commit
// narrowed the visible rows. With more than one installed plugin the
// cursor and the action would disagree — Space would flip the wrong
// row's enabled flag, Enter would open the wrong row's detail block.
// The fix is one helper that both callers share.
func (m *runMode) installedActiveRows() []pluginRow {
	rows := installedPluginRows()
	if m.panel.pluginTab == panelInstalled &&
		m.panel.pluginSearch && m.panel.pluginQueryCommitted {
		return pluginInstalledFilter(rows, m.panel.pluginQuery)
	}
	return rows
}


