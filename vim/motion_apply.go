package vim

// clampDesired clamps a remembered column onto line row (0-based, and
// one less than the rune length in non-Insert mode).
func clampDesired(s *State, row, col int) int {
	n := s.lineLen(row)
	if n == 0 {
		return 0
	}
	if col >= n {
		if s.Mode == Insert {
			return n
		}
		return n - 1
	}
	return col
}

// firstNonBlank returns the rune column of the first non-blank rune, or
// 0 for an all-blank line.
func firstNonBlank(line string) int {
	for i, r := range []rune(line) {
		if !isSpaceRune(r) {
			return i
		}
	}
	return 0
}

// applyDirectionalMotion handles h/j/k/l cursor motions.
func (s *State) applyDirectionalMotion(m motion, count int) {
	switch m {
	case motionLeft:
		for i := 0; i < count; i++ {
			if s.Col > 0 {
				s.Col = prevClusterCol(s.Lines[s.Row], s.Col)
			}
		}
		s.desiredCol = s.Col
	case motionRight:
		for i := 0; i < count; i++ {
			if s.Col < s.lineLen(s.Row)-1 {
				s.Col = nextClusterCol(s.Lines[s.Row], s.Col)
				if s.Col >= s.lineLen(s.Row) {
					s.Col = s.lineLen(s.Row) - 1
				}
			}
		}
		s.desiredCol = s.Col
	case motionDown:
		for i := 0; i < count && s.Row < len(s.Lines)-1; i++ {
			s.Row++
		}
		s.Col = clampDesired(s, s.Row, s.desiredCol)
	case motionUp:
		for i := 0; i < count && s.Row > 0; i++ {
			s.Row--
		}
		s.Col = clampDesired(s, s.Row, s.desiredCol)
	}
}

// applyLineOrDocMotion handles 0, ^, $, gg, and G motions.
func (s *State) applyLineOrDocMotion(m motion, count int) {
	switch m {
	case motionLineStart:
		s.Col = 0
		s.desiredCol = 0
	case motionFirstNonBlank:
		s.Col = firstNonBlank(s.Lines[s.Row])
		s.desiredCol = s.Col
	case motionLineEnd:
		if count > 1 {
			s.Row = min(s.Row+count-1, len(s.Lines)-1)
		}
		n := s.lineLen(s.Row)
		if n > 0 {
			s.Col = n - 1
		} else {
			s.Col = 0
		}
		s.desiredCol = s.Col
	case motionDocStart:
		if count > 1 {
			s.Row = min(count-1, len(s.Lines)-1)
		} else {
			s.Row = 0
		}
		s.Col = clampDesired(s, s.Row, s.desiredCol)
		s.desiredCol = s.Col
	case motionDocEnd:
		if count > 1 {
			s.Row = min(count-1, len(s.Lines)-1)
		} else {
			s.Row = len(s.Lines) - 1
		}
		s.Col = clampDesired(s, s.Row, s.desiredCol)
		s.desiredCol = s.Col
	}
}

// applyMotion moves the cursor by a bare motion. count multiplies the
// motion, arg carries the target character for f/F/t/T.
func (s *State) applyMotion(m motion, count int, arg rune) bool {
	if count < 1 {
		count = 1
	}
	switch m {
	case motionLeft, motionRight, motionDown, motionUp:
		s.applyDirectionalMotion(m, count)
	case motionLineStart, motionFirstNonBlank, motionLineEnd, motionDocStart, motionDocEnd:
		s.applyLineOrDocMotion(m, count)
	case motionWordNext, motionWordBack, motionWordEnd, motionWORDNext, motionWORDBack, motionWORDEnd:
		s.applyWordMotion(m, count)
	case motionFind, motionTill, motionFindBack, motionTillBack, motionRepeatFind, motionRepeatFindRev:
		if !s.applyFindMotion(m, count, arg) {
			return false
		}
	}
	s.clampCursor()
	return true
}

// targetDirectionalMotion computes targets for h/j/k/l motions.
func (s *State) targetDirectionalMotion(m motion, count int, from pos) motionTarget {
	switch m {
	case motionLeft:
		if s.Col == 0 {
			return motionTarget{} // h at col 0 is invalid for an operator
		}
		to := from
		for i := 0; i < count; i++ {
			to.col = prevClusterCol(s.Lines[to.row], to.col)
			if to.col == 0 {
				break
			}
		}
		return motionTarget{to: to, inclusive: true, backward: true, valid: true}
	case motionRight:
		to := from
		for i := 0; i < count; i++ {
			if to.col < s.lineLen(to.row)-1 {
				to.col = nextClusterCol(s.Lines[to.row], to.col)
			}
		}
		return motionTarget{to: to, inclusive: true, valid: true}
	case motionDown:
		to := from
		to.row = min(s.Row+count, len(s.Lines)-1)
		return motionTarget{to: to, linewise: true, inclusive: true, valid: true}
	case motionUp:
		to := from
		to.row = max(s.Row-count, 0)
		return motionTarget{to: to, linewise: true, inclusive: true, backward: true, valid: true}
	default:
		return motionTarget{}
	}
}

// targetLineOrDocMotion computes targets for line/document motions (0, ^, $, gg, G).
func (s *State) targetLineOrDocMotion(m motion, count int, from pos) motionTarget {
	switch m {
	case motionLineStart:
		if s.Col == 0 {
			return motionTarget{to: from, inclusive: true, backward: true, valid: true}
		}
		return motionTarget{to: pos{from.row, 0}, inclusive: false, backward: true, valid: true}
	case motionFirstNonBlank:
		fnb := firstNonBlank(s.Lines[from.row])
		return motionTarget{to: pos{from.row, fnb}, inclusive: false, backward: fnb <= s.Col, valid: true}
	case motionLineEnd:
		row := from.row
		if count > 1 {
			row = min(from.row+count-1, len(s.Lines)-1)
		}
		n := s.lineLen(row)
		end := 0
		if n > 0 {
			end = n - 1
		}
		return motionTarget{to: pos{row, end}, inclusive: true, valid: true}
	case motionDocStart:
		to := pos{0, 0}
		if count > 1 {
			to.row = min(count-1, len(s.Lines)-1)
			to.col = clampDesired(s, to.row, s.Col)
			return motionTarget{to: to, linewise: count > 1, inclusive: true, backward: true, valid: true}
		}
		return motionTarget{to: to, linewise: true, inclusive: true, backward: true, valid: true}
	case motionDocEnd:
		if count > 1 {
			to := pos{min(count-1, len(s.Lines)-1), 0}
			to.col = clampDesired(s, to.row, s.Col)
			return motionTarget{to: to, linewise: count > 1, inclusive: true, valid: true}
		}
		h := len(s.Lines) - 1
		return motionTarget{to: pos{h, 0}, linewise: true, inclusive: true, valid: true}
	default:
		return motionTarget{}
	}
}

// motionTarget computes where a motion lands from the current cursor
// and how an operator should consume it, without moving the cursor.
func (s *State) motionTarget(m motion, count int, arg rune) motionTarget {
	if count < 1 {
		count = 1
	}
	from := pos{s.Row, s.Col}
	switch m {
	case motionLeft, motionRight, motionDown, motionUp:
		return s.targetDirectionalMotion(m, count, from)
	case motionLineStart, motionFirstNonBlank, motionLineEnd, motionDocStart, motionDocEnd:
		return s.targetLineOrDocMotion(m, count, from)
	case motionWordNext, motionWORDNext, motionWordBack, motionWORDBack, motionWordEnd, motionWORDEnd:
		return s.targetWordMotion(m, count, from)
	case motionFind, motionTill, motionFindBack, motionTillBack:
		return s.targetFindMotion(m, count, arg)
	default:
		return motionTarget{}
	}
}
