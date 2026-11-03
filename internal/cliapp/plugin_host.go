// plugin_host.go — the host's side of the /plugin panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/pluginsui: it owns the two tabs,
// the search box, the Discover shelves, the inline local-install prompt,
// the cursor, the keys and the writes. The host supplies the one thing
// the panel cannot answer itself — the way out of the panel — and
// nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel struct
// is reset when a panel closes, so a stale copy of the search box, the
// cursor or the "enabled only" filter is never shown.

package cliapp

import (
	plugins "github.com/justwasm/pigasus/internal/features/plugins/core"
	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
)

// newPluginPanel builds the /plugin panel with the host's seams.
func (m *runMode) newPluginPanel() *pluginsui.Panel {
	return pluginsui.New(pluginsui.Seams{
		// Esc — and the 'q' the list navigation answers with — leave the
		// panel.
		Close: m.closePanel,
	})
}

// pluginPanel returns the open /plugin panel.
//
// The panel is built when the /plugin panel opens — the host's slashPanel
// struct is reset whenever a panel closes — so the accessor is here so
// that a caller which finds the panel missing still gets one that
// answers.
func (m *runMode) pluginPanel() *pluginsui.Panel {
	if m.panel.plg == nil {
		m.panel.plg = m.newPluginPanel()
	}
	return m.panel.plg
}

// pluginWatcherPtr points at the plugin package's own watcher pointer.
//
// The address matters: the value behind it is an atomic.Pointer, and
// the pointer is what the TUI stores through. The retired facade
// exposed `&core.WatcherPtr` for the same reason — a copy of an
// atomic.Pointer is a copy of a lock.
var pluginWatcherPtr = &plugins.WatcherPtr
