package vim

// Handle processes one key press and reports whether the engine
// consumed it. The host is expected to route genuinely global keys
// (ctrl+c, ctrl+d, shift+tab, the empty-prompt "?", …) itself first, so
// anything the engine does not recognise is left for the host.
func (s *State) Handle(k Key) Result {
	if s.Mode == Insert {
		return s.handleInsert(k)
	}
	return s.handleNormal(k)
}

// InsertPending reports whether the engine is mid-sequence (e.g. a lone
// "d" waiting for "w"). The host uses it to keep the mode badge or the
// key routing stable.
func (s *State) InsertPending() bool { return s.Pending != "" }

// enterVisual starts a Visual selection anchored at the cursor.
func (s *State) enterVisual(line bool) {
	s.anchorRow, s.anchorCol = s.Row, s.Col
	if line {
		s.Mode = VisualLine
	} else {
		s.Mode = Visual
	}
	s.desiredCol = s.Col
}

// visualRange returns the normalised, inclusive selection.
func (s *State) visualRange() (pos, pos, bool) {
	a := pos{s.anchorRow, s.anchorCol}
	b := pos{s.Row, s.Col}
	if a.row > b.row || (a.row == b.row && a.col > b.col) {
		a, b = b, a
	}
	if s.Mode == VisualLine {
		return pos{a.row, 0}, pos{b.row, max(s.lineLen(b.row)-1, 0)}, true
	}
	return a, b, false
}

// dropNoopUndo discards the top undo snapshot when it is identical to
// the current buffer, so an edit that changed nothing leaves no empty
// undo step (the behaviour the reference's changelog pins for D/C and
// the Visual case operators). It reports whether the snapshot was
// dropped.
func (s *State) dropNoopUndo() bool {
	if len(s.undo) == 0 {
		return false
	}
	top := s.undo[len(s.undo)-1]
	if equalLines(top.lines, s.Lines) {
		s.undo = s.undo[:len(s.undo)-1]
		return true
	}
	return false
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func reverseString(s string) string {
	rl := []rune(s)
	for i, j := 0, len(rl)-1; i < j; i, j = i+1, j-1 {
		rl[i], rl[j] = rl[j], rl[i]
	}
	return string(rl)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func sign(n int) int {
	if n < 0 {
		return -1
	}
	if n > 0 {
		return 1
	}
	return 0
}
