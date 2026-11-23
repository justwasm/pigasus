package battlemode

import (
	"context"
	"errors"
	"sync"
)

// Manager owns the active Battle Mode session and provides a high-level
// lifecycle API for hosts and UI controllers.
type Manager struct {
	mu       sync.RWMutex
	session  *Session
	onUpdate func(SessionSnapshot)
}

// NewManager creates a new Battle Mode manager.
func NewManager() *Manager {
	return &Manager{}
}

// OnUpdate registers a callback invoked whenever the active session updates.
func (m *Manager) OnUpdate(fn func(SessionSnapshot)) {
	m.mu.Lock()
	m.onUpdate = fn
	sess := m.session
	m.mu.Unlock()
	if sess != nil {
		sess.OnUpdate(fn)
	}
}

// Session returns the current active session, or nil if none has been started.
func (m *Manager) Session() *Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.session
}

// PrepareWorkspaces starts a fresh session (cleaning up any previous session)
// and prepares isolated workspaces for the given arms.
func (m *Manager) PrepareWorkspaces(ctx context.Context, baseDir string, arms []ArmSpec) error {
	m.mu.Lock()
	prev := m.session
	sess := NewSession()
	if m.onUpdate != nil {
		sess.OnUpdate(m.onUpdate)
	}
	m.session = sess
	m.mu.Unlock()

	if prev != nil {
		_ = prev.Cleanup()
	}
	return sess.PrepareWorkspaces(ctx, baseDir, arms)
}

// Run executes the task across all prepared arms in the active session.
func (m *Manager) Run(ctx context.Context, task string, runner ArmRunner) error {
	sess := m.Session()
	if sess == nil {
		return errors.New("battlemode: no active session")
	}
	return sess.Run(ctx, task, runner)
}

// EliminateArm eliminates the arm at index in the active session.
func (m *Manager) EliminateArm(index int) error {
	sess := m.Session()
	if sess == nil {
		return errors.New("battlemode: no active session")
	}
	return sess.EliminateArm(index)
}

// PickWinner applies the winning arm's changes back to baseDir.
func (m *Manager) PickWinner(ctx context.Context, index int) (ApplyResult, error) {
	sess := m.Session()
	if sess == nil {
		return ApplyResult{}, errors.New("battlemode: no active session")
	}
	return sess.PickWinner(ctx, index)
}

// Cleanup cleans up the active session's temporary worktrees and directories.
func (m *Manager) Cleanup() error {
	sess := m.Session()
	if sess == nil {
		return nil
	}
	return sess.Cleanup()
}

// Snapshot returns a snapshot of the active session, or an empty snapshot if none.
func (m *Manager) Snapshot() SessionSnapshot {
	sess := m.Session()
	if sess == nil {
		return SessionSnapshot{WinnerIndex: -1}
	}
	return sess.Snapshot()
}
