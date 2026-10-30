// Package taskregistry panel_rows.go — cached row list for the /tasks panel.
package task

import (
	"sync"
	"time"
)

// TasksPanelLimit caps the panel to the most recent 20 rows.
const TasksPanelLimit = 20

// PanelRows holds the cached row list for the /tasks panel.
type PanelRows struct {
	mu       sync.Mutex
	tasks    []*Task
	updated  time.Time
	filtered int
}

// NewPanelRows creates an empty PanelRows cache.
func NewPanelRows() *PanelRows {
	return &PanelRows{}
}

// Snapshot returns a copy of the cached task rows.
func (r *PanelRows) Snapshot() []*Task {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Task, len(r.tasks))
	copy(out, r.tasks)
	return out
}

// Filtered returns how many older tasks were hidden by the panel limit cap.
func (r *PanelRows) Filtered() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.filtered
}

// Rebuild updates the cache from the registry.
func (r *PanelRows) Rebuild(reg *Registry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if reg == nil {
		r.tasks = nil
		r.filtered = 0
		r.updated = time.Now()
		return
	}
	all := reg.List()
	if len(all) > TasksPanelLimit {
		r.filtered = len(all) - TasksPanelLimit
		all = all[len(all)-TasksPanelLimit:]
	} else {
		r.filtered = 0
	}
	r.tasks = all
	r.updated = time.Now()
}
