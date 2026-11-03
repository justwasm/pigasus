package tuistore

// ClearInFlightBash uninstalls the in-flight execution hooks and command buffer.
func (s *Store) ClearInFlightBash() {
	s.BashCancel = nil
	s.BashRun = nil
	s.BashCmd = ""
}

// ResetBash terminates any in-flight bash command and clears its state.
func (s *Store) ResetBash() {
	if s.BashCancel != nil {
		s.BashCancel()
		s.BashCancel = nil
	}
	s.BashRun = nil
	s.BashCmd = ""
	s.BashRowIdx = -1
}
