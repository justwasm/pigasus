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
}

// ClearCompactStatusNotice clears only the transient status row notice telemetry.
func (s *Store) ClearCompactStatusNotice() {
	s.CompactStart = time.Time{}
	s.CompactEnd = time.Time{}
	s.CompactSpinnerFrame = 0
}
