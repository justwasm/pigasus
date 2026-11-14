package herdr

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Workspace represents a project directory containing one or more terminal tabs.
// It matches upstream Herdr's workspace identity with Git branch discovery.
type Workspace struct {
	mu sync.RWMutex

	ID         string   // e.g. "ws-1", "openagy"
	Name       string   // e.g. "openagy", "herdr", "tuios"
	Dir        string   // absolute path to project directory
	Branch     string   // git branch name e.g. "main", "feat/sidebar"
	Ahead      int      // commits ahead upstream
	Behind     int      // commits behind upstream
	Panes      []*Pane  // tabs belonging to this workspace
	ActivePane int      // active tab index
}

// NewWorkspace creates a workspace initialized with Git branch metadata.
func NewWorkspace(id, name, dir, branch string) *Workspace {
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if name == "" {
		name = filepath.Base(dir)
		if name == "." || name == "/" || name == "" {
			name = "default"
		}
	}
	if branch == "" {
		branch, _, _ = DiscoverGitBranch(dir)
	}

	return &Workspace{
		ID:     id,
		Name:   name,
		Dir:    dir,
		Branch: branch,
	}
}

// AddPane appends a pane to the workspace.
func (w *Workspace) AddPane(p *Pane) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Panes = append(w.Panes, p)
	w.ActivePane = len(w.Panes) - 1
	p.workspaceID = w.ID
}

// RemovePane removes a pane by ID from the workspace.
func (w *Workspace) RemovePane(id string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	targetIdx := -1
	for i, p := range w.Panes {
		if p.ID == id {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		return false
	}

	_ = w.Panes[targetIdx].Close()
	w.Panes = append(w.Panes[:targetIdx], w.Panes[targetIdx+1:]...)

	if len(w.Panes) == 0 {
		w.ActivePane = 0
	} else if w.ActivePane >= len(w.Panes) {
		w.ActivePane = len(w.Panes) - 1
		w.Panes[w.ActivePane].MarkRead()
	} else {
		w.Panes[w.ActivePane].MarkRead()
	}

	return true
}

// Active returns the currently focused pane within this workspace.
func (w *Workspace) Active() *Pane {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if len(w.Panes) == 0 || w.ActivePane < 0 || w.ActivePane >= len(w.Panes) {
		return nil
	}
	return w.Panes[w.ActivePane]
}

// ActiveIndex returns the index of the active pane.
func (w *Workspace) ActiveIndex() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.ActivePane
}

// SelectPane focuses the pane at the specified index.
func (w *Workspace) SelectPane(idx int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if idx < 0 || idx >= len(w.Panes) {
		return false
	}
	w.ActivePane = idx
	w.Panes[idx].MarkRead()
	return true
}

// NextPane cycles forward to the next tab.
func (w *Workspace) NextPane() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.Panes) <= 1 {
		return
	}
	w.ActivePane = (w.ActivePane + 1) % len(w.Panes)
	w.Panes[w.ActivePane].MarkRead()
}

// PrevPane cycles backward to the previous tab.
func (w *Workspace) PrevPane() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.Panes) <= 1 {
		return
	}
	w.ActivePane = (w.ActivePane - 1 + len(w.Panes)) % len(w.Panes)
	w.Panes[w.ActivePane].MarkRead()
}

// Count returns the number of panes in this workspace.
func (w *Workspace) Count() int {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return len(w.Panes)
}

// PaneList returns a snapshot of all panes in this workspace.
func (w *Workspace) PaneList() []*Pane {
	w.mu.RLock()
	defer w.mu.RUnlock()
	out := make([]*Pane, len(w.Panes))
	copy(out, w.Panes)
	return out
}

// DiscoverGitBranch detects the active Git branch and ahead/behind counts for a directory.
func DiscoverGitBranch(dir string) (branch string, ahead, behind int) {
	if dir == "" {
		return "main", 0, 0
	}

	// 1. Check direct .git/HEAD
	headPath := filepath.Join(dir, ".git", "HEAD")
	if data, err := os.ReadFile(headPath); err == nil {
		line := strings.TrimSpace(string(data))
		if strings.HasPrefix(line, "ref: refs/heads/") {
			return strings.TrimPrefix(line, "ref: refs/heads/"), 0, 0
		}
		if len(line) >= 7 {
			return line[:7], 0, 0
		}
	}

	// 2. Check worktree gitdir pointer
	gitFile := filepath.Join(dir, ".git")
	if data, err := os.ReadFile(gitFile); err == nil {
		content := strings.TrimSpace(string(data))
		if strings.HasPrefix(content, "gitdir: ") {
			gitDir := strings.TrimPrefix(content, "gitdir: ")
			if !filepath.IsAbs(gitDir) {
				gitDir = filepath.Join(dir, gitDir)
			}
			if headData, err := os.ReadFile(filepath.Join(gitDir, "HEAD")); err == nil {
				line := strings.TrimSpace(string(headData))
				if strings.HasPrefix(line, "ref: refs/heads/") {
					return strings.TrimPrefix(line, "ref: refs/heads/"), 0, 0
				}
				if len(line) >= 7 {
					return line[:7], 0, 0
				}
			}
		}
	}

	return "main", 0, 0
}

// GitStatusString formats the branch and ahead/behind status.
func (w *Workspace) GitStatusString() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	branch := w.Branch
	if branch == "" {
		branch = "main"
	}

	if w.Ahead > 0 && w.Behind > 0 {
		return fmt.Sprintf("%s ↑%d ↓%d", branch, w.Ahead, w.Behind)
	} else if w.Ahead > 0 {
		return fmt.Sprintf("%s ↑%d", branch, w.Ahead)
	} else if w.Behind > 0 {
		return fmt.Sprintf("%s ↓%d", branch, w.Behind)
	}
	return branch + " · clean"
}
