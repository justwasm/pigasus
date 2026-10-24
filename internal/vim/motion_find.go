package vim

// findCharForward searches forward on the current line for arg.
// If till is true, it stops one character before the target.
func (s *State) findCharForward(arg rune, count int, till bool) (pos, bool) {
	rl := s.lineRunes(s.Row)
	at := s.Col
	for n := 0; n < count; n++ {
		found := -1
		for i := at + 1; i < len(rl); i++ {
			if rl[i] == arg {
				found = i
				break
			}
		}
		if found < 0 {
			return pos{}, false
		}
		at = found
	}
	if till {
		at = prevClusterCol(s.Lines[s.Row], at)
		if at <= s.Col {
			return pos{}, false
		}
	}
	return pos{s.Row, at}, true
}

// findCharBackward searches backward on the current line for arg.
// If till is true, it stops one character after the target.
func (s *State) findCharBackward(arg rune, count int, till bool) (pos, bool) {
	rl := s.lineRunes(s.Row)
	at := s.Col
	for n := 0; n < count; n++ {
		found := -1
		for i := at - 1; i >= 0; i-- {
			if rl[i] == arg {
				found = i
				break
			}
		}
		if found < 0 {
			return pos{}, false
		}
		at = found
	}
	if till {
		at = nextClusterCol(s.Lines[s.Row], at)
		if at >= s.Col {
			return pos{}, false
		}
	}
	return pos{s.Row, at}, true
}

// findChar implements f/F/t/T. It searches only the current line, as
// Vim does. t/T stop one character before the target.
func (s *State) findChar(m motion, arg rune, count int) (pos, bool) {
	rl := s.lineRunes(s.Row)
	if arg == 0 || len(rl) == 0 {
		return pos{}, false
	}
	switch m {
	case motionFind:
		return s.findCharForward(arg, count, false)
	case motionTill:
		return s.findCharForward(arg, count, true)
	case motionFindBack:
		return s.findCharBackward(arg, count, false)
	case motionTillBack:
		return s.findCharBackward(arg, count, true)
	}
	return pos{}, false
}

// reverseFindMotion returns the reversed motion for the repeat find ',' command.
func reverseFindMotion(m motion) motion {
	switch m {
	case motionFind:
		return motionFindBack
	case motionFindBack:
		return motionFind
	case motionTill:
		return motionTillBack
	case motionTillBack:
		return motionTill
	default:
		return m
	}
}

// applyRepeatFind implements ';' and ',' repeat search motions.
func (s *State) applyRepeatFind(m motion, count int) bool {
	if !s.lastFindOK {
		return false
	}
	m2 := s.lastFindMotion
	if m == motionRepeatFindRev {
		m2 = reverseFindMotion(m2)
	}
	arg2 := s.lastFindArg
	if p, ok := s.findChar(m2, arg2, count); ok {
		s.Row, s.Col = p.row, p.col
		s.desiredCol = s.Col
		s.lastFindMotion = m2
		return true
	}
	return false
}

// applyFindMotion moves the cursor for character search motions (f, F, t, T, ;, ,).
func (s *State) applyFindMotion(m motion, count int, arg rune) bool {
	switch m {
	case motionFind, motionTill, motionFindBack, motionTillBack:
		if p, ok := s.findChar(m, arg, count); ok {
			s.Row, s.Col = p.row, p.col
			s.desiredCol = s.Col
			return true
		}
		return false
	case motionRepeatFind, motionRepeatFindRev:
		return s.applyRepeatFind(m, count)
	default:
		return false
	}
}

// targetFindMotion computes operator targets for character search motions.
func (s *State) targetFindMotion(m motion, count int, arg rune) motionTarget {
	to, ok := s.findChar(m, arg, count)
	if !ok {
		return motionTarget{}
	}
	inc := m == motionFind || m == motionFindBack
	backward := m == motionFindBack || m == motionTillBack
	return motionTarget{to: to, inclusive: inc, backward: backward, valid: true}
}
