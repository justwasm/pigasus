package tuistore

import "time"

// Compaction holds auto-compaction progress and telemetry state.
type Compaction struct {
	CompactSpinnerFrame int
	CompactStart        time.Time
	CompactEnd          time.Time
	CompactTokensBefore int
	CompactTokensAfter  int
	CompactReason       string
}

// ResetCompaction clears compaction telemetry.
func (s *Store) ResetCompaction() {
	s.CompactSpinnerFrame = 0
	s.CompactStart = time.Time{}
	s.CompactEnd = time.Time{}
	s.CompactTokensBefore = 0
	s.CompactTokensAfter = 0
	s.CompactReason = ""
}
