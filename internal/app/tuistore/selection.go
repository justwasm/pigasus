package tuistore

// ResetSelection clears the transcript viewport text selection.
func (s *Store) ResetSelection() {
	s.SelDragging = false
	s.SelStartRow = 0
	s.SelStartCol = 0
	s.SelEndRow = 0
	s.SelEndCol = 0
}

// ResetInputSelection clears the input textarea text selection.
func (s *Store) ResetInputSelection() {
	s.InputSelDragging = false
	s.InputSelStart = 0
	s.InputSelEnd = 0
}

// InputSelectionRange returns normalized [start, end] byte offsets.
func (s *Store) InputSelectionRange() (start, end int) {
	if s.InputSelStart <= s.InputSelEnd {
		return s.InputSelStart, s.InputSelEnd
	}
	return s.InputSelEnd, s.InputSelStart
}
