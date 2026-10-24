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
