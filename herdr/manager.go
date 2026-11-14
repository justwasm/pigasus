package herdr

import (
	"fmt"
	"sync"
)

// AgentEntry represents an active agent discovered in a workspace pane.
type AgentEntry struct {
	Pane      *Pane
	Workspace *Workspace
	AgentName string
	State     string
	Message   string
}

// Manager coordinates workspaces and multiplexed terminal panes.
// Matches upstream Herdr's multi-workspace session and agent discovery model.
type Manager struct {
	mu         sync.RWMutex
	workspaces []*Workspace
	activeWS   int
	rows       int
	cols       int
	nextID     int

	socketPath string
	server     *Server
	hits       HitMap
	theme      Palette

	onActiveChange func(idx int)
	onUpdate       func()
	onPaneExit     func(paneID string, exitCode int)
}

// NewManager creates an empty manager with starting viewport geometry.
func NewManager(rows, cols int) *Manager {
	if rows < 4 {
		rows = 24
	}
	if cols < 10 {
		cols = 80
	}
	return &Manager{
		rows:   rows,
		cols:   cols,
		nextID: 1,
		theme:  DetectPalette(),
	}
}

// ensureWorkspaceLocked guarantees at least one workspace exists. Must be called with lock held.
func (m *Manager) ensureWorkspaceLocked(dir string) *Workspace {
	if len(m.workspaces) == 0 {
		wsID := fmt.Sprintf("ws-%d", m.nextID)
		m.nextID++
		ws := NewWorkspace(wsID, "", dir, "")
		m.workspaces = append(m.workspaces, ws)
		m.activeWS = 0
		return ws
	}
	if m.activeWS < 0 {
		m.activeWS = 0
	} else if m.activeWS >= len(m.workspaces) {
		m.activeWS = len(m.workspaces) - 1
	}
	return m.workspaces[m.activeWS]
}

// AddWorkspace explicitly creates and appends a new project workspace.
func (m *Manager) AddWorkspace(name, dir string) *Workspace {
	m.mu.Lock()
	defer m.mu.Unlock()

	wsID := fmt.Sprintf("ws-%d", m.nextID)
	m.nextID++
	ws := NewWorkspace(wsID, name, dir, "")
	m.workspaces = append(m.workspaces, ws)
	m.activeWS = len(m.workspaces) - 1

	cb := m.onUpdate
	if cb != nil {
		go cb()
	}
	return ws
}

// Workspaces returns a snapshot slice of all active workspaces.
func (m *Manager) Workspaces() []*Workspace {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ensureWorkspaceLocked("")
	out := make([]*Workspace, len(m.workspaces))
	copy(out, m.workspaces)
	return out
}

// ActiveWorkspace returns the currently selected project workspace.
func (m *Manager) ActiveWorkspace() *Workspace {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureWorkspaceLocked("")
}

// ActiveWorkspaceIndex returns the index of the active workspace.
func (m *Manager) ActiveWorkspaceIndex() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeWS
}

// SelectWorkspace switches focus to the specified workspace index.
func (m *Manager) SelectWorkspace(idx int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if idx < 0 || idx >= len(m.workspaces) {
		return false
	}
	m.activeWS = idx
	ws := m.workspaces[idx]
	if active := ws.Active(); active != nil {
		active.MarkRead()
	}

	cb := m.onActiveChange
	paneIdx := ws.ActiveIndex()
	if cb != nil {
		go cb(paneIdx)
	}

	updateCb := m.onUpdate
	if updateCb != nil {
		go updateCb()
	}
	return true
}

// SetOnActiveChange registers a callback triggered when active pane changes.
func (m *Manager) SetOnActiveChange(fn func(idx int)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onActiveChange = fn
}

// SetOnUpdate registers a callback triggered when pane content, titles, or agent states update.
func (m *Manager) SetOnUpdate(fn func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onUpdate = fn
}

// SetOnPaneExit registers a callback triggered when a child process pane terminates.
func (m *Manager) SetOnPaneExit(fn func(paneID string, exitCode int)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPaneExit = fn
}

// TriggerUpdate notifies observers that manager or pane state has changed.
func (m *Manager) TriggerUpdate() {
	m.mu.RLock()
	cb := m.onUpdate
	m.mu.RUnlock()
	if cb != nil {
		cb()
	}
}

// SetSocketPath stores the active Herdr Unix socket path.
func (m *Manager) SetSocketPath(path string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.socketPath = path
}

// SocketPath returns the active Herdr Unix socket path.
func (m *Manager) SocketPath() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.socketPath
}

// SetServer attaches the running Herdr Unix socket server to this manager.
func (m *Manager) SetServer(s *Server) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.server = s
}

// Server returns the running Herdr Unix socket server if any.
func (m *Manager) Server() *Server {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.server
}

// SetHits stores the current mouse clickable element coordinates.
func (m *Manager) SetHits(h HitMap) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hits = h
}

// Hits returns the current mouse clickable element coordinates.
func (m *Manager) Hits() HitMap {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hits
}

// AddPane creates and appends a new terminal pane in the currently active workspace.
func (m *Manager) AddPane(title, dir string, opts ...PaneOption) (*Pane, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws := m.ensureWorkspaceLocked(dir)
	if dir == "" {
		dir = ws.Dir
	}

	id := fmt.Sprintf("pane-%d", m.nextID)
	tabID := fmt.Sprintf("tab-%d", m.nextID)
	m.nextID++

	if title == "" {
		title = fmt.Sprintf("term %d", ws.Count()+1)
	}

	allOpts := make([]PaneOption, 0, len(opts)+2)
	if m.socketPath != "" {
		allOpts = append(allOpts, WithSocket(m.socketPath, ws.ID, tabID))
	}
	allOpts = append(allOpts, opts...)

	p, err := NewPane(id, title, dir, m.rows, m.cols, allOpts...)
	if err != nil {
		return nil, err
	}

	paneID := p.ID
	userOnExit := p.onExit
	p.onExit = func(code int) {
		if userOnExit != nil {
			userOnExit(code)
		}
		m.mu.RLock()
		mgrOnExit := m.onPaneExit
		m.mu.RUnlock()
		if mgrOnExit != nil {
			mgrOnExit(paneID, code)
		}
	}

	ws.AddPane(p)
	p.MarkRead()

	cb := m.onActiveChange
	idx := ws.ActiveIndex()
	if cb != nil {
		go cb(idx)
	}

	updateCb := m.onUpdate
	if updateCb != nil {
		go updateCb()
	}

	return p, nil
}

// Panes returns a thread-safe snapshot slice of tabs in the active workspace.
func (m *Manager) Panes() []*Pane {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := m.ensureWorkspaceLocked("")
	return ws.PaneList()
}

// AllPanes returns all panes across all workspaces.
func (m *Manager) AllPanes() []*Pane {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var all []*Pane
	for _, ws := range m.workspaces {
		all = append(all, ws.PaneList()...)
	}
	return all
}

// Active returns the currently focused pane within the active workspace.
func (m *Manager) Active() *Pane {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := m.ensureWorkspaceLocked("")
	return ws.Active()
}

// ActiveIndex returns the index of the active pane in the active workspace.
func (m *Manager) ActiveIndex() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := m.ensureWorkspaceLocked("")
	return ws.ActiveIndex()
}

// SelectPane switches focus to the specified pane index in the active workspace.
func (m *Manager) SelectPane(idx int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := m.ensureWorkspaceLocked("")
	ok := ws.SelectPane(idx)
	if ok {
		cb := m.onActiveChange
		if cb != nil {
			go cb(idx)
		}
		updateCb := m.onUpdate
		if updateCb != nil {
			go updateCb()
		}
	}
	return ok
}

// NextPane cycles forward to the next tab in the active workspace.
func (m *Manager) NextPane() {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := m.ensureWorkspaceLocked("")
	ws.NextPane()

	cb := m.onActiveChange
	idx := ws.ActiveIndex()
	if cb != nil {
		go cb(idx)
	}
}

// PrevPane cycles backward to the previous tab in the active workspace.
func (m *Manager) PrevPane() {
	m.mu.Lock()
	defer m.mu.Unlock()
	ws := m.ensureWorkspaceLocked("")
	ws.PrevPane()

	cb := m.onActiveChange
	idx := ws.ActiveIndex()
	if cb != nil {
		go cb(idx)
	}
}

// CloseCurrent closes and removes the active pane in the active workspace.
func (m *Manager) CloseCurrent() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.workspaces) == 0 {
		return
	}
	ws := m.workspaces[m.activeWS]
	active := ws.Active()
	if active == nil {
		return
	}

	ws.RemovePane(active.ID)

	// If workspace is completely empty and there are other workspaces, prune it
	if ws.Count() == 0 {
		if len(m.workspaces) > 1 {
			m.workspaces = append(m.workspaces[:m.activeWS], m.workspaces[m.activeWS+1:]...)
			if m.activeWS >= len(m.workspaces) {
				m.activeWS = len(m.workspaces) - 1
			}
		}
	}

	cb := m.onActiveChange
	if len(m.workspaces) > 0 {
		activeIdx := m.workspaces[m.activeWS].ActiveIndex()
		if cb != nil {
			go cb(activeIdx)
		}
	}

	updateCb := m.onUpdate
	if updateCb != nil {
		go updateCb()
	}
}

// Resize updates the geometry for all managed panes across all workspaces.
func (m *Manager) Resize(rows, cols int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.rows = rows
	m.cols = cols
	for _, ws := range m.workspaces {
		for _, p := range ws.Panes {
			p.Resize(rows, cols)
		}
	}
}

// Count returns the total number of managed panes across all workspaces.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	total := 0
	for _, ws := range m.workspaces {
		total += ws.Count()
	}
	return total
}

// CloseAll terminates all running panes and cleans up the server socket.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server != nil {
		_ = m.server.Close()
		m.server = nil
	}
	for _, ws := range m.workspaces {
		for _, p := range ws.Panes {
			_ = p.Close()
		}
	}
	m.workspaces = nil
	m.activeWS = 0
}

// FindPane locates a pane by its exact ID across all workspaces.
func (m *Manager) FindPane(id string) *Pane {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, ws := range m.workspaces {
		for _, p := range ws.Panes {
			if p.ID == id {
				return p
			}
		}
	}
	return nil
}

// FindPaneByTarget resolves a target identifier across all workspaces.
func (m *Manager) FindPaneByTarget(target string) *Pane {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if target == "" || target == "active" {
		if len(m.workspaces) == 0 {
			return nil
		}
		return m.workspaces[m.activeWS].Active()
	}

	// 1. Exact ID
	for _, ws := range m.workspaces {
		for _, p := range ws.Panes {
			if p.ID == target {
				return p
			}
		}
	}

	// 2. 1-based index or index with 'p' prefix (e.g. "p1")
	trimmed := target
	if len(trimmed) > 1 && (trimmed[0] == 'p' || trimmed[0] == 'P') {
		trimmed = trimmed[1:]
	}
	var idx int
	if _, err := fmt.Sscanf(trimmed, "%d", &idx); err == nil && idx >= 1 {
		curr := 1
		for _, ws := range m.workspaces {
			for _, p := range ws.Panes {
				if curr == idx {
					return p
				}
				curr++
			}
		}
	}

	// 3. Agent name
	for _, ws := range m.workspaces {
		for _, p := range ws.Panes {
			name, _, _ := p.AgentState()
			if canonical, ok := LookupAgent(name); ok && canonical == target {
				return p
			}
			if name == target {
				return p
			}
		}
	}

	// 4. Title
	for _, ws := range m.workspaces {
		for _, p := range ws.Panes {
			if p.Title == target {
				return p
			}
		}
	}

	return nil
}

// ClosePaneByID terminates and removes a pane by its ID across all workspaces.
func (m *Manager) ClosePaneByID(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for wsIdx, ws := range m.workspaces {
		if ws.RemovePane(id) {
			if ws.Count() == 0 && len(m.workspaces) > 1 {
				m.workspaces = append(m.workspaces[:wsIdx], m.workspaces[wsIdx+1:]...)
				if m.activeWS >= len(m.workspaces) {
					m.activeWS = len(m.workspaces) - 1
				}
			}

			cb := m.onActiveChange
			if len(m.workspaces) > 0 {
				activeIdx := m.workspaces[m.activeWS].ActiveIndex()
				if cb != nil {
					go cb(activeIdx)
				}
			}

			updateCb := m.onUpdate
			if updateCb != nil {
				go updateCb()
			}
			return true
		}
	}

	return false
}

// AllAgents scans all workspaces and returns all panes running recognized coding agents.
func (m *Manager) AllAgents() []AgentEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var agents []AgentEntry
	for _, ws := range m.workspaces {
		for _, p := range ws.Panes {
			agentName, state, message, detected := DetectAgentFromPane(p)
			if detected {
				agents = append(agents, AgentEntry{
					Pane:      p,
					Workspace: ws,
					AgentName: agentName,
					State:     state,
					Message:   message,
				})
			}
		}
	}
	return agents
}

// SelectAgentPane finds the workspace containing the given pane ID, switches to it, and focuses the pane.
func (m *Manager) SelectAgentPane(paneID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	for wsIdx, ws := range m.workspaces {
		for paneIdx, p := range ws.Panes {
			if p.ID == paneID {
				m.activeWS = wsIdx
				ws.SelectPane(paneIdx)

				cb := m.onActiveChange
				if cb != nil {
					go cb(paneIdx)
				}

				updateCb := m.onUpdate
				if updateCb != nil {
					go updateCb()
				}
				return true
			}
		}
	}
	return false
}

// Theme returns the active color palette.
func (m *Manager) Theme() Palette {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.theme.Name != "" {
		return m.theme
	}
	return DetectPalette()
}

// SetTheme overrides the active color palette.
func (m *Manager) SetTheme(p Palette) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.theme = p
	if m.onUpdate != nil {
		m.onUpdate()
	}
}

// ToggleTheme switches between dark (Mocha) and light (Latte) themes.
func (m *Manager) ToggleTheme() Palette {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.theme.IsDark {
		m.theme = ThemeLatte
	} else {
		m.theme = ThemeMocha
	}
	if m.onUpdate != nil {
		m.onUpdate()
	}
	return m.theme
}

