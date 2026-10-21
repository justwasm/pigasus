package vim

// motion identifies a cursor movement. Motions are resolved two ways:
// applyMotion moves the cursor for a bare motion key, and
// motionTarget reports where the motion would land plus how an
// operator should treat it (inclusive/exclusive, charwise/linewise).
type motion uint8

const (
	motionNone motion = iota
	motionLeft
	motionRight
	motionDown
	motionUp
	motionWordNext  // w
	motionWordBack  // b
	motionWordEnd   // e
	motionWORDNext  // W
	motionWORDBack  // B
	motionWORDEnd   // E
	motionLineStart // 0
	motionFirstNonBlank
	motionLineEnd
	motionDocStart // gg
	motionDocEnd   // G
	motionFind     // f
	motionFindBack // F
	motionTill     // t
	motionTillBack // T
	motionRepeatFind
	motionRepeatFindRev // ,
)

// pos is a (row, col) rune position inside the buffer.
type pos struct{ row, col int }

// motionTarget is the outcome of resolving a motion for an operator.
type motionTarget struct {
	to        pos
	linewise  bool // reduces to whole lines (j/k/gg/G)
	inclusive bool // the destination character is part of the range
	backward  bool // the motion moved toward the top of the buffer
	valid     bool
}

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

// applyMotion moves the cursor by a bare motion. count multiplies the
// motion, arg carries the target character for f/F/t/T.
func (s *State) applyMotion(m motion, count int, arg rune) bool {
	if count < 1 {
		count = 1
	}
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
	case motionWordNext:
		for i := 0; i < count; i++ {
			p, _ := s.wordStartForward(s.Row, s.Col, false)
			s.Row, s.Col = p.row, p.col
		}
		s.desiredCol = s.Col
	case motionWordBack:
		for i := 0; i < count; i++ {
			p := s.wordStartBackward(s.Row, s.Col, false)
			s.Row, s.Col = p.row, p.col
		}
		s.desiredCol = s.Col
	case motionWordEnd:
		for i := 0; i < count; i++ {
			p := s.wordEndForward(s.Row, s.Col, false)
			s.Row, s.Col = p.row, p.col
		}
		s.desiredCol = s.Col
	case motionWORDNext:
		for i := 0; i < count; i++ {
			p, _ := s.wordStartForward(s.Row, s.Col, true)
			s.Row, s.Col = p.row, p.col
		}
		s.desiredCol = s.Col
	case motionWORDBack:
		for i := 0; i < count; i++ {
			p := s.wordStartBackward(s.Row, s.Col, true)
			s.Row, s.Col = p.row, p.col
		}
		s.desiredCol = s.Col
	case motionWORDEnd:
		for i := 0; i < count; i++ {
			p := s.wordEndForward(s.Row, s.Col, true)
			s.Row, s.Col = p.row, p.col
		}
		s.desiredCol = s.Col
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
	case motionFind, motionTill, motionFindBack, motionTillBack:
		if p, ok := s.findChar(m, arg, count); ok {
			s.Row, s.Col = p.row, p.col
			s.desiredCol = s.Col
		} else {
			return false
		}
	case motionRepeatFind, motionRepeatFindRev:
		if !s.lastFindOK {
			return false
		}
		m2 := s.lastFindMotion
		if m == motionRepeatFindRev {
			switch m2 {
			case motionFind:
				m2 = motionFindBack
			case motionFindBack:
				m2 = motionFind
			case motionTill:
				m2 = motionTillBack
			case motionTillBack:
				m2 = motionTill
			}
		}
		arg2 := s.lastFindArg
		if p, ok := s.findChar(m2, arg2, count); ok {
			s.Row, s.Col = p.row, p.col
			s.desiredCol = s.Col
			s.lastFindMotion = m2
		} else {
			return false
		}
	}
	s.clampCursor()
	return true
}

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

// findChar implements f/F/t/T. It searches only the current line, as
// Vim does. t/T stop one character before the target.
func (s *State) findChar(m motion, arg rune, count int) (pos, bool) {
	rl := s.lineRunes(s.Row)
	if arg == 0 || len(rl) == 0 {
		return pos{}, false
	}
	switch m {
	case motionFind, motionTill:
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
		if m == motionTill {
			at = prevClusterCol(s.Lines[s.Row], at)
			if at <= s.Col {
				return pos{}, false
			}
		}
		return pos{s.Row, at}, true
	case motionFindBack, motionTillBack:
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
		if m == motionTillBack {
			at = nextClusterCol(s.Lines[s.Row], at)
			if at >= s.Col {
				return pos{}, false
			}
		}
		return pos{s.Row, at}, true
	}
	return pos{}, false
}

// motionTarget computes where a motion lands from the current cursor
// and how an operator should consume it, without moving the cursor.
func (s *State) motionTarget(m motion, count int, arg rune) motionTarget {
	if count < 1 {
		count = 1
	}
	from := pos{s.Row, s.Col}
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
	case motionFind, motionTill, motionFindBack, motionTillBack:
		to, ok := s.findChar(m, arg, count)
		if !ok {
			return motionTarget{}
		}
		inc := m == motionFind || m == motionFindBack
		return motionTarget{to: to, inclusive: inc, backward: m == motionFindBack || m == motionTillBack, valid: true}
	}
	return motionTarget{}
}

// operatorRange turns a motion target into a concrete inclusive
// (start,end) selection and a linewise flag, applying Vim's
// inclusive/exclusive rules.
func (s *State) operatorRange(mt motionTarget) (start, end pos, linewise, ok bool) {
	if !mt.valid {
		return pos{}, pos{}, false, false
	}
	if mt.linewise {
		a, b := s.Row, mt.to.row
		if a > b {
			a, b = b, a
		}
		return pos{a, 0}, pos{b, max(s.lineLen(b)-1, 0)}, true, true
	}
	from := pos{s.Row, s.Col}
	to := mt.to
	if mt.backward {
		start, end = to, from
	} else {
		start, end = from, to
	}
	if !mt.inclusive {
		// Drop the destination character (exclusive motion).
		if mt.backward {
			end = pos{end.row, prevClusterCol(s.Lines[end.row], end.col)}
			if end.row == start.row && end.col < start.col {
				return pos{}, pos{}, false, false
			}
		} else {
			if to.col == 0 && to.row > start.row {
				// Crossed into the next line at col 0: stop at the
				// previous line's last character.
				end = pos{to.row - 1, max(s.lineLen(to.row-1)-1, 0)}
			} else {
				end = pos{to.row, prevClusterCol(s.Lines[to.row], to.col)}
			}
			if end.row < start.row || (end.row == start.row && end.col < start.col) {
				return pos{}, pos{}, false, false
			}
		}
	}
	return start, end, false, true
}
