package tuistore

import "time"

// Compaction holds auto-compaction progress and telemetry state.
type Compaction struct {
	CompactSpinnerFrame    int
	CompactStart           time.Time
	CompactEnd             time.Time
	CompactTokensBefore    int
	CompactTokensAfter     int
	CompactStepsSummarized int
	CompactReason          string
	CompactJustHappened    bool

	// CompactLiveInTranscript says the running compaction draws its live
	// row inside the conversation instead of the status row.
	//
	// A manual /compact owns a conversation row: the reference puts
	// "Compacting conversation…" where "Generating…" appears. The automatic
	// checkpoint has no such row — it runs inside a turn whose own spinner
	// already owns the live line — so it keeps the status row.
	//
	// One compaction, one place: when this is set the status row stays out.
	CompactLiveInTranscript bool
}

// ResetCompaction clears compaction telemetry.
func (s *Store) ResetCompaction() {
	s.CompactSpinnerFrame = 0
	s.CompactStart = time.Time{}
	s.CompactEnd = time.Time{}
	s.CompactTokensBefore = 0
	s.CompactTokensAfter = 0
	s.CompactStepsSummarized = 0
	s.CompactReason = ""
	s.CompactJustHappened = false
	s.CompactLiveInTranscript = false
}

// ClearCompactStatusNotice clears only the transient status row notice telemetry.
func (s *Store) ClearCompactStatusNotice() {
	s.CompactStart = time.Time{}
	s.CompactEnd = time.Time{}
	s.CompactSpinnerFrame = 0
}
