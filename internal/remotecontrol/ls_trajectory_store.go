// Package remotecontrol — ls_trajectory_store.go: the store that
// keeps one trajectory per conversation.
//
// The store is the clone's own conversation history, as the workbench
// sees it: the Web UI mints one uuid for a conversation and uses it
// for the cascade id, the conversation id and the trajectory id.
//
// Two readers matter and they need different access:
//
//   - a *live* conversation needs Get — it creates the trajectory when
//     the conversation is new, because the first frame it renders has
//     to carry a trajectory;
//   - a *reader* (the trajectory methods, the debug panel) needs
//     Lookup — it must learn that a conversation it has never seen is
//     not there, instead of inventing an empty one.
//
// 【一手】 2026-10-27: the reference's own answers for the trajectory
// methods are in out/langserver/coverage-351.json; the clone answers
// the same shapes from this store.
package remotecontrol

import "sync"

// lsTrajectoryStore keeps one trajectory per conversation id.
type lsTrajectoryStore struct {
	mu    sync.Mutex
	trajs map[string]*lsTrajectory
}

// newLSTrajectoryStore returns an empty store.
func newLSTrajectoryStore() *lsTrajectoryStore {
	return &lsTrajectoryStore{trajs: map[string]*lsTrajectory{}}
}

// Get returns the trajectory for conversationID, creating it when the
// conversation is new. A nil store or an empty id returns nil.
func (s *lsTrajectoryStore) Get(conversationID string) *lsTrajectory {
	if s == nil || conversationID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.trajs[conversationID]; ok {
		return t
	}
	t := newLSTrajectory(conversationID)
	s.trajs[conversationID] = t
	return t
}

// Restart gives the conversation's trajectory a new id and clears its
// steps. It is what a continued conversation starts with.
func (s *lsTrajectoryStore) Restart(conversationID, trajectoryID string) {
	if s == nil || conversationID == "" {
		return
	}
	s.Get(conversationID).Restart(trajectoryID)
}

// Lookup returns the trajectory for conversationID without creating
// one.
//
// Get creates the trajectory of a conversation it has not seen — that
// is what a *live* conversation needs. A reader that asks for a
// conversation that is not there has to learn that it is not there, so
// it uses this one.
func (s *lsTrajectoryStore) Lookup(conversationID string) *lsTrajectory {
	if s == nil || conversationID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.trajs[conversationID]
}

// LookupByTrajectory returns the trajectory with this trajectory id,
// without creating one.
//
// The store is keyed by the conversation id and the trajectory id is
// derived from it, so the lookup walks the store. A reader that asks
// for a trajectory id — GetUserTrajectory — needs this, because the
// id it holds is the server's own, not the conversation's.
func (s *lsTrajectoryStore) LookupByTrajectory(trajectoryID string) *lsTrajectory {
	if s == nil || trajectoryID == "" {
		return nil
	}
	s.mu.Lock()
	trajs := make([]*lsTrajectory, 0, len(s.trajs))
	for _, t := range s.trajs {
		trajs = append(trajs, t)
	}
	s.mu.Unlock()
	for _, t := range trajs {
		if t == nil {
			continue
		}
		if t.TrajectoryID() == trajectoryID {
			return t
		}
	}
	return nil
}

// Delete drops the trajectory of a conversation and reports whether
// there was one.
//
// It is what DeleteCascadeTrajectory answers with: the conversation
// goes away, and a second delete of the same conversation finds
// nothing.
func (s *lsTrajectoryStore) Delete(conversationID string) bool {
	if s == nil || conversationID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.trajs[conversationID]; !ok {
		return false
	}
	delete(s.trajs, conversationID)
	return true
}

// Snapshots returns the snapshot of every trajectory in the store.
//
// The order is the map's own — the caller sorts what it needs. It is
// the list the conversation summary walks.
func (s *lsTrajectoryStore) Snapshots() []lsSnapshot {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	trajs := make([]*lsTrajectory, 0, len(s.trajs))
	for _, t := range s.trajs {
		trajs = append(trajs, t)
	}
	s.mu.Unlock()
	out := make([]lsSnapshot, 0, len(trajs))
	for _, t := range trajs {
		if t == nil {
			continue
		}
		out = append(out, t.Snapshot())
	}
	return out
}

// MostRecent returns the snapshot of the trajectory that was updated
// last, and reports whether the store holds one.
//
// The version is the trajectory's own counter, read under the
// trajectory's lock (Snapshot takes it), so the answer is the one of
// the newest turn rather than of whichever map entry came first.
func (s *lsTrajectoryStore) MostRecent() (lsSnapshot, bool) {
	if s == nil {
		return lsSnapshot{}, false
	}
	s.mu.Lock()
	trajs := make([]*lsTrajectory, 0, len(s.trajs))
	for _, t := range s.trajs {
		trajs = append(trajs, t)
	}
	s.mu.Unlock()
	var best lsSnapshot
	found := false
	for _, t := range trajs {
		if t == nil {
			continue
		}
		snap := t.Snapshot()
		if !found || snap.Version > best.Version {
			best = snap
			found = true
		}
	}
	return best, found
}
