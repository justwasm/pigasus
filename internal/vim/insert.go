package vim

// --- Insert mode ------------------------------------------------------

func (s *State) handleInsertNav(name string) bool {
	switch name {
	case "left":
		if s.Col > 0 {
			s.Col = prevClusterCol(s.Lines[s.Row], s.Col)
			s.desiredCol = s.Col
		}
		return true
	case "right":
		if s.Col < s.lineLen(s.Row) {
			s.Col = nextClusterCol(s.Lines[s.Row], s.Col)
			s.desiredCol = s.Col
		}
		return true
	case "up":
		s.moveVertical(-1)
		return true
	case "down":
		s.moveVertical(1)
		return true
	case "home":
		s.Col = 0
		s.desiredCol = 0
		return true
	case "end":
		s.Col = s.lineLen(s.Row)
		s.desiredCol = s.Col
		return true
	}
	return false
}

func (s *State) handleInsert(k Key) Result {
	// vim.insert.submit: ctrl+enter and ctrl+s submit from Insert mode.
	if k.Mod&Ctrl != 0 && (k.Name == "enter" || k.Rune == 's') {
		return Result{Consumed: true, Submit: true}
	}
	switch k.Name {
	case "esc":
		s.Mode = Normal
		s.clampCursor()
		s.breakUndo()
		return Result{Consumed: true}
	case "enter":
		if s.InsertFirst && k.Mod&(Alt|Shift) == 0 {
			// Insert First: a bare Enter submits; the modified
			// variants (alt/shift+enter) insert a newline instead.
			return Result{Consumed: true, Submit: true}
		}
		s.insertNewline()
		return Result{Consumed: true}
	case "backspace":
		s.insertBackspace()
		return Result{Consumed: true}
	case "delete":
		s.insertDelete()
		return Result{Consumed: true}
	case "tab":
		// Tab belongs to the host (picker completion / mode cycle).
		return Result{}
	default:
		if s.handleInsertNav(k.Name) {
			return Result{Consumed: true}
		}
	}
	// ctrl+j inserts a newline, even under Insert First.
	if k.Mod&Ctrl != 0 && k.Rune == 'j' {
		s.insertNewline()
		return Result{Consumed: true}
	}
	if k.Mod&(Ctrl|Alt) != 0 {
		return Result{} // global / editor chords belong to the host
	}
	if k.Rune != 0 {
		s.insertRune(k.Rune)
		return Result{Consumed: true}
	}
	return Result{}
}

// moveVertical moves the cursor by delta lines in Insert mode (where the
// column may rest one past the last character).
func (s *State) moveVertical(delta int) {
	for i := 0; i < abs(delta); i++ {
		nr := s.Row + sign(delta)
		if nr < 0 || nr >= len(s.Lines) {
			break
		}
		s.Row = nr
	}
	s.Col = clampDesired(s, s.Row, s.desiredCol)
}

func (s *State) insertRune(r rune) {
	s.pushUndo(true)
	rl := s.lineRunes(s.Row)
	next := make([]rune, 0, len(rl)+1)
	next = append(next, rl[:s.Col]...)
	next = append(next, r)
	next = append(next, rl[s.Col:]...)
	s.Lines[s.Row] = string(next)
	s.Col++
	s.desiredCol = s.Col
}

func (s *State) insertNewline() {
	s.pushUndo(true)
	rl := s.lineRunes(s.Row)
	head := string(rl[:s.Col])
	tail := string(rl[s.Col:])
	out := make([]string, 0, len(s.Lines)+1)
	out = append(out, s.Lines[:s.Row]...)
	out = append(out, head, tail)
	out = append(out, s.Lines[s.Row+1:]...)
	s.Lines = out
	s.Row++
	s.Col = 0
	s.desiredCol = 0
}

func (s *State) insertBackspace() {
	if s.Col == 0 {
		if s.Row == 0 {
			return
		}
		s.pushUndo(true)
		prevLen := s.lineLen(s.Row - 1)
		joined := s.Lines[s.Row-1] + s.Lines[s.Row]
		out := make([]string, 0, len(s.Lines)-1)
		out = append(out, s.Lines[:s.Row-1]...)
		out = append(out, joined)
		out = append(out, s.Lines[s.Row+1:]...)
		s.Lines = out
		s.Row--
		s.Col = prevLen
		s.desiredCol = s.Col
		return
	}
	s.pushUndo(true)
	rl := s.lineRunes(s.Row)
	start := prevClusterCol(s.Lines[s.Row], s.Col)
	s.Lines[s.Row] = string(rl[:start]) + string(rl[s.Col:])
	s.Col = start
	s.desiredCol = s.Col
}

func (s *State) insertDelete() {
	rl := s.lineRunes(s.Row)
	if s.Col >= len(rl) {
		if s.Row < len(s.Lines)-1 {
			s.pushUndo(true)
			joined := s.Lines[s.Row] + s.Lines[s.Row+1]
			out := make([]string, 0, len(s.Lines)-1)
			out = append(out, s.Lines[:s.Row]...)
			out = append(out, joined)
			out = append(out, s.Lines[s.Row+2:]...)
			s.Lines = out
		}
		return
	}
	s.pushUndo(true)
	end := clusterEndCol(s.Lines[s.Row], s.Col)
	s.Lines[s.Row] = string(rl[:s.Col]) + string(rl[end:])
}
