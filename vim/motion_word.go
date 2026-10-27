package vim

// classOf classifies a rune for the w/b/e motions: 0 = blank,
// 1 = keyword (word char), 2 = punctuation/other. Vim's three-class
// split, unlike vimtea's two-class approximation.
func classOf(r rune) int {
	switch {
	case isSpaceRune(r):
		return 0
	case isWordChar(r):
		return 1
	default:
		return 2
	}
}

// classOfBig classifies a rune for the W/B/E motions: 0 = blank,
// 1 = anything else (a WORD is any run of non-blank characters).
func classOfBig(r rune) int {
	if isSpaceRune(r) {
		return 0
	}
	return 1
}

// classFn picks the classifier for big (WORD) motions.
func classFn(big bool) func(rune) int {
	if big {
		return classOfBig
	}
	return classOf
}

// wordStartForward returns the start of the next word at or after
// (row,col), crossing into following lines as needed. used for w/W. The
// bool is false when there is no following word (the caller then treats
// the motion as end-of-line).
//
// The fresh flag tracks a position reached by crossing a line break: the
// first non-blank there *is* the next word start, so it must not be
// skipped over (Vim's w from the end of a line lands on the next line's
// first character).
func (s *State) wordStartForward(row, col int, big bool) (pos, bool) {
	class := classFn(big)
	fresh := false
	for row < len(s.Lines) {
		rl := s.lineRunes(row)
		i := col
		if fresh {
			for i < len(rl) && class(rl[i]) == 0 {
				i++
			}
			if i < len(rl) {
				return pos{row, i}, true
			}
			row++
			col = 0
			continue
		}
		if i < len(rl) && class(rl[i]) != 0 {
			c := class(rl[i])
			for i < len(rl) && class(rl[i]) == c {
				i++
			}
		}
		for i < len(rl) && class(rl[i]) == 0 {
			i++
		}
		if i < len(rl) {
			return pos{row, i}, true
		}
		row++
		col = 0
		fresh = true
	}
	// Fell off the end: Vim leaves the cursor on the last character.
	last := len(s.Lines) - 1
	n := s.lineLen(last)
	if n > 0 {
		n--
	}
	return pos{last, n}, false
}

// wordStartBackward returns the start of the previous word before
// (row,col), crossing into preceding lines. used for b/B.
func (s *State) wordStartBackward(row, col int, big bool) pos {
	class := classFn(big)
	for row >= 0 {
		rl := s.lineRunes(row)
		i := col
		if i > len(rl) {
			i = len(rl)
		}
		i--
		for i >= 0 && class(rl[i]) == 0 {
			i--
		}
		if i >= 0 {
			c := class(rl[i])
			for i > 0 && class(rl[i-1]) == c {
				i--
			}
			return pos{row, i}
		}
		row--
		if row >= 0 {
			col = s.lineLen(row)
		}
	}
	return pos{0, 0}
}

// wordEndForward returns the end (last char) of the next word at or
// after (row,col), crossing lines. used for e/E. It always advances at
// least one cluster, so e from a word end moves to the end of the
// following word rather than staying put.
func (s *State) wordEndForward(row, col int, big bool) pos {
	class := classFn(big)
	fresh := false
	for row < len(s.Lines) {
		rl := s.lineRunes(row)
		i := col
		if !fresh {
			if i < len(rl) {
				i = nextClusterCol(s.Lines[row], i)
			} else {
				row++
				col = 0
				fresh = true
				continue
			}
		}
		for i < len(rl) && class(rl[i]) == 0 {
			i++
		}
		if i < len(rl) {
			c := class(rl[i])
			for i+1 < len(rl) && class(rl[i+1]) == c {
				i++
			}
			return pos{row, i}
		}
		row++
		col = 0
		fresh = true
	}
	last := len(s.Lines) - 1
	n := s.lineLen(last)
	if n > 0 {
		n--
	}
	return pos{last, n}
}

// applyWordMotion moves the cursor for word and WORD motions.
func (s *State) applyWordMotion(m motion, count int) {
	switch m {
	case motionWordNext, motionWORDNext:
		big := m == motionWORDNext
		for i := 0; i < count; i++ {
			p, _ := s.wordStartForward(s.Row, s.Col, big)
			s.Row, s.Col = p.row, p.col
		}
	case motionWordBack, motionWORDBack:
		big := m == motionWORDBack
		for i := 0; i < count; i++ {
			p := s.wordStartBackward(s.Row, s.Col, big)
			s.Row, s.Col = p.row, p.col
		}
	case motionWordEnd, motionWORDEnd:
		big := m == motionWORDEnd
		for i := 0; i < count; i++ {
			p := s.wordEndForward(s.Row, s.Col, big)
			s.Row, s.Col = p.row, p.col
		}
	}
	s.desiredCol = s.Col
}

// targetWordMotion resolves operator targets for word and WORD motions.
func (s *State) targetWordMotion(m motion, count int, from pos) motionTarget {
	switch m {
	case motionWordNext, motionWORDNext:
		big := m == motionWORDNext
		to := from
		found := true
		for i := 0; i < count; i++ {
			to, found = s.wordStartForward(to.row, to.col, big)
			if !found {
				break
			}
		}
		if !found || to.row != from.row {
			// Vim clamps dw/W across a line break (and on the last
			// word of the buffer) to end-of-line, so the newline is
			// never eaten here.
			return motionTarget{to: pos{from.row, max(s.lineLen(from.row)-1, 0)}, inclusive: true, valid: true}
		}
		return motionTarget{to: to, inclusive: false, valid: true}
	case motionWordBack, motionWORDBack:
		big := m == motionWORDBack
		to := from
		for i := 0; i < count; i++ {
			to = s.wordStartBackward(to.row, to.col, big)
		}
		return motionTarget{to: to, inclusive: true, backward: true, valid: true}
	case motionWordEnd, motionWORDEnd:
		big := m == motionWORDEnd
		to := from
		for i := 0; i < count; i++ {
			to = s.wordEndForward(to.row, to.col, big)
		}
		return motionTarget{to: to, inclusive: true, valid: true}
	default:
		return motionTarget{}
	}
}
