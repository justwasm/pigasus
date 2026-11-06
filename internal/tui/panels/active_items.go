package panels

import (
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"
)

// ActiveItemsHost defines the host operations needed by the active items panel.
type ActiveItemsHost interface {
	TaskRegistry() *taskregistry.Registry
	TerminalSize() (width int, height int)
	VimEnabled() bool
	OpenTasksDetail(id string, isSubagent bool)
}

// NewActiveItemsPanel builds the active items panel with the given host seams.
func NewActiveItemsPanel(h ActiveItemsHost) *activeitemspanel.Panel {
	return activeitemspanel.New(activeitemspanel.Seams{
		Registry:   h.TaskRegistry,
		Width:      func() int { w, _ := h.TerminalSize(); return w },
		Height:     func() int { _, hgt := h.TerminalSize(); return hgt },
		VimEnabled: h.VimEnabled,
		OpenDetail: h.OpenTasksDetail,
	})
}
