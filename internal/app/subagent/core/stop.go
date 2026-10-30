package core

// stop.go — cycle feat/subagent-stop-clear-approvals:
// Manager.Stop + OnStop hook.
//
// AGY 1.3.3 changelog line 52:
//
//	Fixed a stopped subagent staying in the active agents list while it
//	still had an approval pending; stopping it now clears its pending
//	approvals right away.
//
// The bug had two symptoms that this file fixes together:
//
//  1. Stopped subagents stayed visible in the active-agents list
//     (`/agents` and the picker). The Manager had no Remove / Stop,
//     so the only way to retire a Subagent was for its `defer`d
//     `Kill()` to land and for callers to forget about it.
//  2. Pending approvals tied to that subagent id (in
//     `internal/cascade.PendingApprovals()`) stayed in the queue and
//     kept the cascader parked in `CascaderAwaitApproval`, so the
//     active-agents picker kept showing it as "blocked on approval"
//     forever.
//
// The fix is the seam named in the gap:
//
//	m.subagentManager.OnStop(id) → clearApprovalsBySubagent(id)
//
// Manager gains:
//   - Stop(id) — Kill + remove + fire OnStop hook
//   - Remove(id) — remove WITHOUT kill (for callers that already
//     terminated the process via another path)
//   - OnStopFunc and SetOnStop — the hook surface that the cascade
//     package wires to State.ClearApprovalsBySubagent via
//     `m.subagentManager.SetOnStop(cascade.ClearApprovalsBySubagent)`
//     at wiring time.
//
// The hook runs synchronously inside Stop / Remove AFTER the
// Manager's agents map has been mutated (so a re-entrant List()
// from the hook sees the active-agents list already without the
// stopped subagent). The Manager's own mu is released BEFORE the
// hook runs so a slow downstream listener cannot deadlock other
// Manager operations.

// OnStopFunc is the hook signature Manager.Stop / Manager.Remove
// invoke. id is the subagent id being stopped. removed is true when
// the subagent was actually deregistered from the Manager; it is
// false when Stop was called for an id that was never registered
// (in which case the hook is NOT called — see Stop for the
// rationale).
//
// id is passed as the first argument because the cycle-feat gap is
// parameterised by subagent id, not by Subagent pointer: every
// downstream listener (the cascade approval queue, the picker view
// that renders /agents, the "stopped X approvals for Y" audit log)
// already keys by id.
//
// The hook may not be called re-entrantly from inside Stop;
// downstream code must not call Manager.Stop with the same id
// while the hook is running. This is enforced by convention —
// the hook is the LAST thing Stop does before returning, after
// which the Manager has fully released the subagent.
type OnStopFunc func(id string)

// SetOnStop wires the OnStop hook. Calling it with nil clears the
// hook. Subsequent Stop / Remove calls invoke the hook under the
// invariant described on Manager.Stop.
//
// The hook field is stored inside Manager via the existing mu;
// callers should set it once at startup. Re-setting it is safe
// (the new hook supersedes the old one), but the wiring test below
// counts the calls so a caller can detect a missed SetOnStop.
func (m *Manager) SetOnStop(fn OnStopFunc) {
	m.mu.Lock()
	m.onStop = fn
	m.mu.Unlock()
}

// onStop returns the wired hook (or nil) while holding mu briefly
// so concurrent SetOnStop from another goroutine doesn't race.
func (m *Manager) currentOnStop() OnStopFunc {
	m.mu.RLock()
	fn := m.onStop
	m.mu.RUnlock()
	return fn
}

// Stop terminates the Subagent at id, removes it from the active
// agents map, and fires the OnStop hook (if any) so the cascade
// approval queue and any other id-keyed listener can clean up.
//
// Behaviour, in order:
//
//  1. Lock the Manager's mu; look up the Subagent. If absent,
//     return (false, false) — no kill, no remove, no hook. This
//     matches the existing KillByID semantics in internal/taskregistry:
//     a Stop on an unknown id is a no-op, never an error, so the
//     picker can re-issue a stop on the same id without crashing.
//  2. Delete the entry from the agents map and release the lock.
//     Doing this BEFORE the Kill matters: the hook (and any code
//     re-entering the Manager via Set / List inside it) observes
//     the post-stop state on its very first read.
//  3. Call Subagent.Kill outside the lock. Kill is a no-op on a
//     Subagent that hasn't Start()ed, so calling it on a subagent
//     that the caller already terminated is harmless. The kill
//     error is intentionally swallowed — stopping is best-effort,
//     and the decompiled `commitSubagentStates` (0x8aa0b20) shows
//     the reference does not surface a stop failure to the user.
//  4. Read the OnStop hook under the Manager's lock and invoke it
//     outside the lock. A nil hook is a no-op.
//
// Returns (killed, removed). killed is true iff the Subagent was
// registered AND its Kill was issued (every registered Subagent
// returns true here — Kill itself does not return an error in the
// success path; we map that to killed=true). removed is true iff
// the Subagent was deregistered. For an unknown id both are false.
//
// Concurrency: safe to call from many goroutines. The lock is held
// only across the lookup + delete, so a Stop during a long List()
// blocks for at most one map op.
func (m *Manager) Stop(id string) (killed, removed bool) {
	if id == "" {
		return false, false
	}
	m.mu.Lock()
	s, ok := m.agents[id]
	if !ok {
		m.mu.Unlock()
		return false, false
	}
	delete(m.agents, id)
	m.mu.Unlock()

	// Kill outside the lock: slow syscall, no Manager state held.
	// We do not propagate the error because the recovered 1.3.3
	// reference's stop path treats kill as best-effort (it still
	// removes from the list even when SIGKILL fails for ESRCH,
	// which is the common "already dead" case).
	killed = true
	removed = true
	if s != nil {
		_ = s.Kill()
	}

	// Fire the hook under no lock; the only shared state the hook
	// touches (the cascade queue, the picker view's snapshot) is
	// the hook's own. If a user-installed hook calls back into
	// the Manager (e.g. it calls Stop again on the same id) the
	// second call's lookup will miss because the map is already
	// purged — by design.
	fn := m.currentOnStop()
	if fn != nil {
		fn(id)
	}
	return killed, removed
}

// Remove unregisters the Subagent at id WITHOUT killing it. Useful
// when the caller has already terminated the process via a separate
// path (e.g. direct SIGKILL via the taskregistry bridge) and only
// wants the bookkeeping. It still fires OnStop so the cascade
// approval queue gets cleaned up.
//
// Returns false (with no hook fired) when no Subagent is registered
// at id. Otherwise returns true.
func (m *Manager) Remove(id string) bool {
	if id == "" {
		return false
	}
	m.mu.Lock()
	_, ok := m.agents[id]
	if !ok {
		m.mu.Unlock()
		return false
	}
	delete(m.agents, id)
	m.mu.Unlock()

	fn := m.currentOnStop()
	if fn != nil {
		fn(id)
	}
	return true
}

// Pause pauses the Subagent at id by killing its current process but keeping
// it in the agents map in "stopped" state so it can be resumed later.
func (m *Manager) Pause(id string) bool {
	if id == "" {
		return false
	}
	m.mu.RLock()
	s, ok := m.agents[id]
	m.mu.RUnlock()
	if !ok || s == nil {
		return false
	}
	_ = s.Stop()
	return true
}

// Resume restarts a paused or stopped Subagent at id.
func (m *Manager) Resume(id string) error {
	if id == "" {
		return nil
	}
	m.mu.RLock()
	s, ok := m.agents[id]
	m.mu.RUnlock()
	if !ok || s == nil {
		return nil
	}
	return s.Resume()
}
