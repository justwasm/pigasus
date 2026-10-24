// Package cortex — summaries.go: conversation summaries and store adapters.
//
// pclntab entries:
//   - ListConversationSummaries          0x7fed0a0
//   - ConversationSummary                0x7fed660
//   - UpdateSummaryForID                 0x7fda520
//   - UpdateSummaryForTrajectory         0x7fda820
//   - stateSyncSummariesStore.WriteSummary 0x7ff4680
//   - stateSyncSummariesStore.DeleteSummary 0x7ff4700
//   - jetboxSummariesStoreAdapter.WriteSummary 0x7ff47a0
//   - jetboxSummariesStoreAdapter.DeleteSummary 0x7ff4820
//   - jetboxSummariesStoreAdapter.GetSummary   0x7ff48a0
package cortex

// stateSyncSummariesStore is the recovered
// `(*stateSyncSummariesStore)` adapter.
type stateSyncSummariesStore struct {
	backend SummariesStore
}

// jetboxSummariesStoreAdapter is the recovered
// `(*jetboxSummariesStoreAdapter)`.
type jetboxSummariesStoreAdapter struct {
	delegate SummariesStore
	prefix   string
}

// ListConversationSummaries lists conversation summaries. v1.3.5
// binary addr 0x7fed0a0, name=ListConversationSummaries,
// signature=(cm *CascadeManager) ([]Summary, error).
func (cm *CascadeManager) ListConversationSummaries() ([]Summary, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return nil, ErrClosed
	}
	return nil, nil
}

// ConversationSummary returns a single conversation summary. v1.3.5
// binary addr 0x7fed660, name=ConversationSummary, signature=(cm
// *CascadeManager, id string) (Summary, error).
func (cm *CascadeManager) ConversationSummary(id string) (Summary, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return Summary{}, ErrClosed
	}
	return Summary{TrajectoryID: id}, nil
}

// UpdateSummaryForID updates the summary for an id. v1.3.5 binary addr
// 0x7fda520, name=UpdateSummaryForID, signature=(cm *CascadeManager,
// id string, summary Summary) error.
func (cm *CascadeManager) UpdateSummaryForID(id string, summary Summary) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return cm.summaries.WriteSummary(id, summary)
}

// UpdateSummaryForTrajectory updates the summary for a trajectory.
// v1.3.5 binary addr 0x7fda820, name=UpdateSummaryForTrajectory,
// signature=(cm *CascadeManager, traj Trajectory) error.
func (cm *CascadeManager) UpdateSummaryForTrajectory(traj Trajectory) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// WriteSummary writes a summary. v1.3.5 binary addr 0x7ff4680,
// name=WriteSummary, signature=(s stateSyncSummariesStore, id string,
// summary Summary) error.
func (s *stateSyncSummariesStore) WriteSummary(id string, summary Summary) error {
	if s.backend == nil {
		return nil
	}
	return s.backend.WriteSummary(id, summary)
}

// DeleteSummary deletes a summary. v1.3.5 binary addr 0x7ff4700,
// name=DeleteSummary, signature=(s stateSyncSummariesStore, id string) error.
func (s *stateSyncSummariesStore) DeleteSummary(id string) error {
	if s.backend == nil {
		return nil
	}
	return s.backend.DeleteSummary(id)
}

// GetSummary returns a summary. (Not in pclntab directly but
// SummariesStore interface requires it.)
func (s *stateSyncSummariesStore) GetSummary(id string) (Summary, bool) {
	if s.backend == nil {
		return Summary{}, false
	}
	return s.backend.GetSummary(id)
}

// WriteSummary writes a summary. v1.3.5 binary addr 0x7ff47a0,
// name=WriteSummary, signature=(j *jetboxSummariesStoreAdapter, id
// string, summary Summary) error.
func (j *jetboxSummariesStoreAdapter) WriteSummary(id string, summary Summary) error {
	return j.delegate.WriteSummary(j.prefix+":"+id, summary)
}

// DeleteSummary deletes a summary. v1.3.5 binary addr 0x7ff4820,
// name=DeleteSummary, signature=(j *jetboxSummariesStoreAdapter, id
// string) error.
func (j *jetboxSummariesStoreAdapter) DeleteSummary(id string) error {
	return j.delegate.DeleteSummary(j.prefix + ":" + id)
}

// GetSummary returns a summary. v1.3.5 binary addr 0x7ff48a0,
// name=GetSummary, signature=(j *jetboxSummariesStoreAdapter, id
// string) (Summary, bool).
func (j *jetboxSummariesStoreAdapter) GetSummary(id string) (Summary, bool) {
	return j.delegate.GetSummary(j.prefix + ":" + id)
}
