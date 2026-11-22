package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/ui/views/historypanel"
)

func (m *RootModel) historyPanel() *historypanel.Panel {
	if m.overlays.panel.hist != nil {
		return m.overlays.panel.hist
	}
	m.overlays.panel.hist = m.newHistoryPanel()
	return m.overlays.panel.hist
}

func (m *RootModel) newHistoryPanel() *historypanel.Panel {
	return historypanel.New(historypanel.Seams{
		Entries: func() []historypanel.Entry {
			// 1. Load persistent entries from history.jsonl
			jsonlEntries := convstore.LoadHistoryEntries("", 500)

			// 2. Map to historypanel.Entry
			var items []historypanel.Entry
			for _, e := range jsonlEntries {
				items = append(items, historypanel.Entry{
					Text:      e.Display,
					Workspace: e.Workspace,
					Timestamp: e.Timestamp,
				})
			}

			// 3. Include any session prompts that might not be in history.jsonl yet
			now := time.Now().UnixMilli()
			for _, p := range m.prompt.promptHistory.Entries() {
				items = append(items, historypanel.Entry{
					Text:      p,
					Workspace: m.session.cwd,
					Timestamp: now,
				})
			}

			return items
		},
		CurrentWorkspace: func() string {
			return m.session.cwd
		},
		Height: func() int {
			return m.viewport.height
		},
		Pick: func(text string) tea.Cmd {
			m.applyHistoryEntry(text)
			return m.closePanel()
		},
		Close: func() tea.Cmd {
			return m.closePanel()
		},
	})
}
