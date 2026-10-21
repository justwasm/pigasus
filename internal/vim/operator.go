package vim

import (
	"strings"
	"unicode"
)

// copyLines returns a shallow copy of a line slice (strings are
// immutable, so this is a sufficient snapshot).
func copyLines(lines []string) []string {
	out := make([]string, len(lines))
	copy(out, lines)
	return out
}

// undoLimit caps the undo history, matching the reference's 50-entry
// ring: once it is full the oldest step is dropped.
const undoLimit = 50

// pushUndo records an undo step. coalesce merges consecutive steps into
// one group so an insert run undoes as a unit rather than per keystroke
// (the behaviour a prompt editor wants, and what the reference's own
// undo grouping implies).
func (s *State) pushUndo(coalesce bool) {
	if coalesce && s.undoOpen {
		return
	}
	s.recordUndo(snapshot{copyLines(s.Lines), s.Row, s.Col})
	s.undoOpen = coalesce
}

// recordUndo pushes a pre-taken snapshot onto the undo stack, dropping
// the oldest once the ring is full. It closes any open coalescing group
// (the caller re-opens one afterwards when it wants to keep merging).
func (s *State) recordUndo(snap snapshot) {
	if len(s.undo) >= undoLimit {
		s.undo = append(s.undo[:0], s.undo[1:]...)
	}
	s.undo = append(s.undo, snap)
	s.redo = nil
	s.undoOpen = false
}

// breakUndo closes any open coalescing group, so the next edit starts a
// fresh undo step.
func (s *State) breakUndo() { s.undoOpen = false }

// undo restores the previous snapshot.
func (s *State) undoOp() bool {
	if len(s.undo) == 0 {
		return false
	}
	snap := s.undo[len(s.undo)-1]
	s.undo = s.undo[:len(s.undo)-1]
	s.redo = append(s.redo, snapshot{copyLines(s.Lines), s.Row, s.Col})
	s.restore(snap)
	return true
}

// redo reapplies the most recently undone snapshot.
func (s *State) redoOp() bool {
	if len(s.redo) == 0 {
		return false
	}
	snap := s.redo[len(s.redo)-1]
	s.redo = s.redo[:len(s.redo)-1]
	s.undo = append(s.undo, snapshot{copyLines(s.Lines), s.Row, s.Col})
	s.restore(snap)
	return true
}

func (s *State) restore(snap snapshot) {
	s.Lines = snap.lines
	s.Row, s.Col = snap.row, snap.col
	s.undoOpen = false
	s.clampCursor()
}

// rangeText extracts the inclusive text covered by (start,end). When
// linewise is set each covered line contributes its full text plus a
// trailing newline, which is how the unnamed register remembers that a
// yank/delete was line-oriented.
func (s *State) rangeText(start, end pos, linewise bool) string {
	if linewise {
		var b strings.Builder
		for r := start.row; r <= end.row; r++ {
			b.WriteString(s.Lines[r])
			b.WriteByte('\n')
		}
		return b.String()
	}
	if start.row == end.row {
		rl := s.lineRunes(start.row)
		return string(rl[start.col : end.col+1])
	}
	var b strings.Builder
	rl := s.lineRunes(start.row)
	b.WriteString(string(rl[start.col:]))
	b.WriteByte('\n')
	for r := start.row + 1; r < end.row; r++ {
		b.WriteString(s.Lines[r])
		b.WriteByte('\n')
	}
	rl2 := s.lineRunes(end.row)
	b.WriteString(string(rl2[:end.col+1]))
	return b.String()
}

// yank fills the unnamed register from a range and leaves the cursor at
// the start, as Vim does.
func (s *State) yank(start, end pos, linewise bool) {
	s.register = s.rangeText(start, end, linewise)
	s.registerLinewise = linewise
	s.Row, s.Col = start.row, start.col
	s.clampCursor()
}

// deleteRange removes the inclusive range, fills the register, and
// leaves the cursor at the start. It never empties the buffer.
func (s *State) deleteRange(start, end pos, linewise bool) {
	// Defensive: a range end that ran past the line (an inclusive motion
	// clamped to end-of-line) must not slice out of bounds.
	if n := s.lineLen(end.row); end.col > n-1 {
		end.col = max(n-1, 0)
	}
	s.register = s.rangeText(start, end, linewise)
	s.registerLinewise = linewise
	switch {
	case linewise:
		s.Lines = append(s.Lines[:start.row], s.Lines[end.row+1:]...)
		if len(s.Lines) == 0 {
			s.Lines = []string{""}
		}
		s.Row = min(start.row, len(s.Lines)-1)
		s.Col = firstNonBlank(s.Lines[s.Row])
	case start.row == end.row:
		rl := s.lineRunes(start.row)
		s.Lines[start.row] = string(rl[:start.col]) + string(rl[end.col+1:])
		s.Row, s.Col = start.row, start.col
	default:
		rl := s.lineRunes(start.row)
		rl2 := s.lineRunes(end.row)
		joined := string(rl[:start.col]) + string(rl2[end.col+1:])
		s.Lines = append(s.Lines[:start.row+1], s.Lines[end.row+1:]...)
		s.Lines[start.row] = joined
		s.Row, s.Col = start.row, start.col
	}
	s.clampCursor()
	s.desiredCol = s.Col
	s.breakUndo()
}

// changeRange deletes the covered text and enters Insert mode. A
// linewise change (cc, or V then c) keeps the line — it empties it and
// preserves the leading whitespace — rather than removing it, so cc on
// "a\nb" leaves an indented empty first line instead of joining the
// neighbours.
func (s *State) changeRange(start, end pos, linewise bool) {
	if !linewise {
		s.deleteRange(start, end, false)
		s.Mode = Insert
		s.clampCursor()
		return
	}
	s.register = s.rangeText(start, end, true)
	s.registerLinewise = true
	indent := leadingWhitespace(s.Lines[start.row])
	out := make([]string, 0, len(s.Lines)-(end.row-start.row))
	out = append(out, s.Lines[:start.row]...)
	out = append(out, indent)
	out = append(out, s.Lines[end.row+1:]...)
	s.Lines = out
	s.Row = start.row
	s.Col = len([]rune(indent))
	s.Mode = Insert
	s.desiredCol = s.Col
	s.breakUndo()
}

// leadingWhitespace returns the run of spaces/tabs that starts line.
func leadingWhitespace(line string) string {
	rl := []rune(line)
	i := 0
	for i < len(rl) && isSpaceRune(rl[i]) {
		i++
	}
	return string(rl[:i])
}

// registerLines splits a linewise register ("a\nb\n") into its lines.
func registerLines(reg string) []string {
	trimmed := strings.TrimSuffix(reg, "\n")
	if trimmed == "" {
		return []string{""}
	}
	return strings.Split(trimmed, "\n")
}

// paste inserts the unnamed register. after=true is p (below / after the
// cursor), false is P.
func (s *State) paste(after bool) bool {
	if s.register == "" {
		return false
	}
	if s.registerLinewise {
		lines := registerLines(s.register)
		at := s.Row
		if after {
			at++
		}
		out := make([]string, 0, len(s.Lines)+len(lines))
		out = append(out, s.Lines[:at]...)
		out = append(out, lines...)
		out = append(out, s.Lines[at:]...)
		s.Lines = out
		s.Row = at
		s.Col = firstNonBlank(s.Lines[at])
		s.breakUndo()
		return true
	}
	rl := s.lineRunes(s.Row)
	insertCol := s.Col
	if after && len(rl) > 0 {
		insertCol = clusterEndCol(s.Lines[s.Row], s.Col)
	}
	head := string(rl[:insertCol])
	tail := string(rl[insertCol:])
	parts := strings.Split(s.register, "\n")
	if len(parts) == 1 {
		s.Lines[s.Row] = head + parts[0] + tail
		s.Col = insertCol + max(len([]rune(parts[0]))-1, 0)
	} else {
		newLines := make([]string, 0, len(parts)+1)
		newLines = append(newLines, head+parts[0])
		newLines = append(newLines, parts[1:len(parts)-1]...)
		newLines = append(newLines, parts[len(parts)-1]+tail)
		out := make([]string, 0, len(s.Lines)+len(newLines))
		out = append(out, s.Lines[:s.Row]...)
		out = append(out, newLines...)
		out = append(out, s.Lines[s.Row+1:]...)
		s.Lines = out
		s.Row = s.Row + len(parts) - 1
		s.Col = max(len([]rune(parts[len(parts)-1]))-1, 0)
	}
	s.clampCursor()
	s.desiredCol = s.Col
	s.breakUndo()
	return true
}

// replaceChar implements r<x>: overwrite up to count clusters starting
// at the cursor with arg. A short line consumes only what is there.
func (s *State) replaceChar(arg rune, count int) bool {
	if arg == 0 || count < 1 {
		return false
	}
	rl := s.lineRunes(s.Row)
	if s.Col >= len(rl) {
		return false
	}
	end := s.Col
	replaced := 0
	for replaced < count && end < len(rl) {
		ne := clusterEndCol(s.Lines[s.Row], end)
		if ne <= end {
			break
		}
		end = ne
		replaced++
	}
	if replaced == 0 {
		return false
	}
	s.pushUndo(false)
	head := append([]rune{}, rl[:s.Col]...)
	mid := make([]rune, replaced)
	for i := range mid {
		mid[i] = arg
	}
	s.Lines[s.Row] = string(append(append(head, mid...), rl[end:]...))
	s.clampCursor()
	s.desiredCol = s.Col
	return true
}

// applyCase transforms the text in an inclusive range: "~" toggles,
// "u" lowercases and "U" uppercases.
func (s *State) applyCase(start, end pos, linewise bool, op rune) bool {
	// Capture the pre-edit buffer so the step is undoable: the snapshot
	// has to be taken before the in-place transform below, not after.
	pr, pc := s.Row, s.Col
	before := copyLines(s.Lines)
	changed := false
	transform := func(line string) string {
		rl := []rune(line)
		for i, r := range rl {
			var nr rune
			switch op {
			case '~':
				nr = toggleCase(r)
			case 'u':
				nr = toLower(r)
			case 'U':
				nr = toUpper(r)
			}
			if nr != r {
				rl[i] = nr
				changed = true
			}
		}
		return string(rl)
	}
	if linewise {
		for r := start.row; r <= end.row; r++ {
			s.Lines[r] = transform(s.Lines[r])
		}
	} else if start.row == end.row {
		rl := []rune(s.Lines[start.row])
		for i := start.col; i <= end.col && i < len(rl); i++ {
			var nr rune
			switch op {
			case '~':
				nr = toggleCase(rl[i])
			case 'u':
				nr = toLower(rl[i])
			case 'U':
				nr = toUpper(rl[i])
			}
			if nr != rl[i] {
				rl[i] = nr
				changed = true
			}
		}
		s.Lines[start.row] = string(rl)
	} else {
		for r := start.row; r <= end.row; r++ {
			rl := []rune(s.Lines[r])
			lo, hi := 0, len(rl)-1
			if r == start.row {
				lo = start.col
			}
			if r == end.row {
				hi = end.col
			}
			for i := lo; i <= hi && i < len(rl); i++ {
				var nr rune
				switch op {
				case '~':
					nr = toggleCase(rl[i])
				case 'u':
					nr = toLower(rl[i])
				case 'U':
					nr = toUpper(rl[i])
				}
				if nr != rl[i] {
					rl[i] = nr
					changed = true
				}
			}
			s.Lines[r] = string(rl)
		}
	}
	if changed {
		s.recordUndo(snapshot{before, pr, pc})
	}
	// The cursor lands on the first changed character, clamped.
	s.Row, s.Col = start.row, start.col
	s.clampCursor()
	s.desiredCol = s.Col
	return changed
}

func toggleCase(r rune) rune {
	switch {
	case r >= 'a' && r <= 'z':
		return r - 32
	case r >= 'A' && r <= 'Z':
		return r + 32
	}
	return r
}

func toLower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + 32
	}
	return unicode.ToLower(r)
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 32
	}
	return unicode.ToUpper(r)
}
