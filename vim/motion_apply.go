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
func (s *State) applyDirectionalMotion(m motion) {
	switch m {
	case motionLeft:
		if s.Col > 0 {
			s.Col = prevClusterCol(s.Lines[s.Row], s.Col)
		}
		s.desiredCol = s.Col
	case motionRight:
		if s.Col < s.lineLen(s.Row)-1 {
			s.Col = nextClusterCol(s.Lines[s.Row], s.Col)
			if s.Col >= s.lineLen(s.Row) {
				s.Col = s.lineLen(s.Row) - 1
			}
		}
		s.desiredCol = s.Col
	case motionDown:
		if s.Row < len(s.Lines)-1 {
			s.Row++
		}
		s.Col = clampDesired(s, s.Row, s.desiredCol)
	case motionUp:
		if s.Row > 0 {
			s.Row--
		}
		s.Col = clampDesired(s, s.Row, s.desiredCol)
	}
}

// applyLineOrDocMotion handles 0, ^, $, gg, and G motions.
func (s *State) applyLineOrDocMotion(m motion) {
	switch m {
	case motionLineStart:
		s.Col = 0
		s.desiredCol = 0
	case motionFirstNonBlank:
		s.Col = firstNonBlank(s.Lines[s.Row])
		s.desiredCol = s.Col
	case motionLineEnd:
		n := s.lineLen(s.Row)
		if n > 0 {
			s.Col = n - 1
		} else {
			s.Col = 0
		}
		s.desiredCol = s.Col
	case motionDocStart:
		s.Row = 0
		s.Col = clampDesired(s, s.Row, s.desiredCol)
		s.desiredCol = s.Col
	case motionDocEnd:
		s.Row = len(s.Lines) - 1
		s.Col = clampDesired(s, s.Row, s.desiredCol)
		s.desiredCol = s.Col
	}
}

// applyMotion moves the cursor by a bare motion. arg carries the target
// character for f/F/t/T.
func (s *State) applyMotion(m motion, arg rune) bool {
	switch m {
	case motionLeft, motionRight, motionDown, motionUp:
		s.applyDirectionalMotion(m)
	case motionLineStart, motionFirstNonBlank, motionLineEnd, motionDocStart, motionDocEnd:
		s.applyLineOrDocMotion(m)
	case motionWordNext, motionWordBack, motionWordEnd, motionWORDNext, motionWORDBack, motionWORDEnd:
		s.applyWordMotion(m)
	case motionFind, motionTill, motionFindBack, motionTillBack, motionRepeatFind, motionRepeatFindRev:
		if !s.applyFindMotion(m, arg) {
			return false
		}
	}
	s.clampCursor()
	return true
}

// targetDirectionalMotion computes targets for h/j/k/l motions.
func (s *State) targetDirectionalMotion(m motion, from pos) motionTarget {
	switch m {
	case motionLeft:
		if s.Col == 0 {
			return motionTarget{} // h at col 0 is invalid for an operator
		}
		to := from
		to.col = prevClusterCol(s.Lines[to.row], to.col)
		return motionTarget{to: to, inclusive: true, backward: true, valid: true}
	case motionRight:
		to := from
		if to.col < s.lineLen(to.row)-1 {
			to.col = nextClusterCol(s.Lines[to.row], to.col)
		}
		return motionTarget{to: to, inclusive: true, valid: true}
	case motionDown:
		to := from
		to.row = min(s.Row+1, len(s.Lines)-1)
		return motionTarget{to: to, linewise: true, inclusive: true, valid: true}
	case motionUp:
		to := from
		to.row = max(s.Row-1, 0)
		return motionTarget{to: to, linewise: true, inclusive: true, backward: true, valid: true}
	default:
		return motionTarget{}
	}
}

// targetLineOrDocMotion computes targets for line/document motions (0, ^, $, gg, G).
func (s *State) targetLineOrDocMotion(m motion, from pos) motionTarget {
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
		n := s.lineLen(row)
		end := 0
		if n > 0 {
			end = n - 1
		}
		return motionTarget{to: pos{row, end}, inclusive: true, valid: true}
	case motionDocStart:
		return motionTarget{to: pos{0, 0}, linewise: true, inclusive: true, backward: true, valid: true}
	case motionDocEnd:
		h := len(s.Lines) - 1
		return motionTarget{to: pos{h, 0}, linewise: true, inclusive: true, valid: true}
	default:
		return motionTarget{}
	}
}

// motionTarget computes where a motion lands from the current cursor
// and how an operator should consume it, without moving the cursor.
func (s *State) motionTarget(m motion, arg rune) motionTarget {
	from := pos{s.Row, s.Col}
	switch m {
	case motionLeft, motionRight, motionDown, motionUp:
		return s.targetDirectionalMotion(m, from)
	case motionLineStart, motionFirstNonBlank, motionLineEnd, motionDocStart, motionDocEnd:
		return s.targetLineOrDocMotion(m, from)
	case motionWordNext, motionWORDNext, motionWordBack, motionWORDBack, motionWordEnd, motionWORDEnd:
		return s.targetWordMotion(m, from)
	case motionFind, motionTill, motionFindBack, motionTillBack:
		return s.targetFindMotion(m, arg)
	default:
		return motionTarget{}
	}
}
