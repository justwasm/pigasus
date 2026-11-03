package vim

import "strings"

func (s *State) executeEdit(c command) (Result, bool) {
	if actsAtCursor(c.kind) {
		s.actOnLineAbove()
	}
	switch c.kind {
	case cmdDeleteChar:
		s.deleteChars(false)
	case cmdDeleteCharBwd:
		s.deleteChars(true)
	case cmdDeleteToEnd:
		s.deleteToEnd(false)
	case cmdChangeToEnd:
		s.deleteToEnd(true)
	case cmdPaste:
		s.doPaste(true)
	case cmdPasteBefore:
		s.doPaste(false)
	case cmdUndo:
		s.undoOp()
	case cmdRedo:
		s.redoOp()
	case cmdReplaceChar:
		return Result{Consumed: true, Bell: !s.replaceChar(c.arg)}, true
	case cmdCaseToggle:
		s.toggleCaseAtCursor()
	default:
		return Result{}, false
	}
	return Result{Consumed: true}, true
}

func (s *State) executeVisual(c command) Result {
	switch c.kind {
	case cmdVisDelete:
		s.pushUndo(false)
		st, en, lw := s.visualRange()
		s.deleteRange(st, en, lw)
		s.Mode = Normal
		s.dropNoopUndo()
	case cmdVisChange:
		s.pushUndo(false)
		st, en, lw := s.visualRange()
		s.changeRange(st, en, lw)
		s.dropNoopUndo()
	case cmdVisYank:
		st, en, lw := s.visualRange()
		s.yank(st, en, lw)
		s.Mode = Normal
	case cmdVisPaste:
		s.visualPaste()
	case cmdVisCase:
		st, en, lw := s.visualRange()
		s.applyCase(st, en, lw, c.caseOp)
		s.Mode = Normal
	case cmdVisReplace:
		s.visReplace(c.arg)
		s.Mode = Normal
	}
	return Result{Consumed: true}
}

// execute runs a parsed command.
func (s *State) execute(c command) Result {
	if res, ok := s.executeEdit(c); ok {
		return res
	}
	switch c.kind {
	case cmdMotion:
		s.recordFind(c.motion, c.arg)
		ok := s.applyMotion(c.motion, c.arg)
		return Result{Consumed: true, Bell: !ok}
	case cmdInsertEntry:
		s.insertEntry(c.insert)
	case cmdOperator:
		s.runOperator(c)
	case cmdSubmit:
		// ZZ: submit without leaving Normal mode.
		return Result{Consumed: true, Submit: true}
	case cmdVisualChar:
		s.enterVisual(false)
	case cmdVisualLine:
		s.enterVisual(true)
	case cmdObject:
		if st, en, _, ok := s.objectRange(c.obj, c.obj.around); ok {
			s.anchorRow, s.anchorCol = st.row, st.col
			s.Row, s.Col = en.row, en.col
			s.clampCursor()
		}
	case cmdVisDelete, cmdVisChange, cmdVisYank, cmdVisPaste, cmdVisCase, cmdVisReplace:
		return s.executeVisual(c)
	}
	return Result{Consumed: true}
}

// recordFind remembers f/F/t/T so ; and , can repeat them.
func (s *State) recordFind(m motion, arg rune) {
	switch m {
	case motionFind, motionFindBack, motionTill, motionTillBack:
		s.lastFindMotion = m
		s.lastFindArg = arg
		s.lastFindOK = true
	}
}

func (s *State) resolveOpMotion(op rune, m motion) motion {
	// cw is Vim's documented exception to the motion: it behaves
	// like ce (it does not eat the following whitespace) whenever
	// the cursor starts on a non-blank.
	if op == 'c' && (m == motionWordNext || m == motionWORDNext) {
		rl := s.lineRunes(s.Row)
		if s.Col < len(rl) && classOf(rl[s.Col]) != 0 {
			if m == motionWordNext {
				return motionWordEnd
			}
			return motionWORDEnd
		}
	}
	return m
}

func (s *State) resolveOperatorRange(c command) (pos, pos, bool, bool) {
	switch {
	case c.obj.kind != objNone:
		return s.objectRange(c.obj, c.obj.around)
	case c.linewise || c.motion == 0:
		a := s.Row
		b := min(a, len(s.Lines)-1)
		return pos{a, 0}, pos{b, max(s.lineLen(b)-1, 0)}, true, true
	default:
		m := s.resolveOpMotion(c.op, c.motion)
		mt := s.motionTarget(m, c.arg)
		return s.operatorRange(mt)
	}
}

// runOperator resolves and applies d/c/y.
func (s *State) runOperator(c command) {
	s.recordFind(c.motion, c.arg)
	// The operator acts at the cursor, so an empty last line hands the
	// command to the line above (agy 1.4.9).
	s.actOnLineAbove()
	start, end, linewise, ok := s.resolveOperatorRange(c)
	if !ok {
		return
	}
	switch c.op {
	case 'y':
		s.yank(start, end, linewise)
	case 'd':
		s.pushUndo(false)
		s.deleteRange(start, end, linewise)
		s.dropNoopUndo()
	case 'c':
		s.pushUndo(false)
		s.changeRange(start, end, linewise)
		s.dropNoopUndo()
	}
}

// insertEntry switches to Insert mode at the position the entry implies.
// o and O also splice in a fresh blank line (below/above) before the
// cursor moves into it.
func (s *State) insertEntry(r rune) {
	switch r {
	case 'a':
		if s.Col < s.lineLen(s.Row) {
			s.Col = nextClusterCol(s.Lines[s.Row], s.Col)
		} else {
			s.Col = s.lineLen(s.Row)
		}
	case 'I':
		s.Col = firstNonBlank(s.Lines[s.Row])
	case 'A':
		s.Col = s.lineLen(s.Row)
	case 'o':
		s.openLine(true)
	case 'O':
		s.openLine(false)
	case '/', '!':
		// The reference's NORMAL-mode "/" and "!" insert the character
		// and enter INSERT mode, so a slash command or a shell command
		// works without pressing "i" first.
		s.insertRune(r)
	}
	s.Mode = Insert
	s.desiredCol = s.Col
}

// openLine inserts a new empty line just below the cursor ("o") or just
// above it ("O") and places the cursor at its start. The insertion is
// folded into the following insert run, so an "o" plus the text typed
// into it undo as a single step.
func (s *State) openLine(below bool) {
	s.pushUndo(true)
	at := s.Row
	if below {
		at = s.Row + 1
	}
	out := make([]string, 0, len(s.Lines)+1)
	out = append(out, s.Lines[:at]...)
	out = append(out, "")
	out = append(out, s.Lines[at:]...)
	s.Lines = out
	s.Row = at
	s.Col = 0
	s.desiredCol = 0
}

func (s *State) deleteCharsBwd() {
	if s.Col == 0 {
		return
	}
	s.pushUndo(false)
	var del strings.Builder
	rl := s.lineRunes(s.Row)
	start := prevClusterCol(s.Lines[s.Row], s.Col)
	del.WriteString(string(rl[start:s.Col]))
	s.Lines[s.Row] = string(rl[:start]) + string(rl[s.Col:])
	s.Col = start
	if del.Len() > 0 {
		s.register = reverseString(del.String())
		s.registerLinewise = false
	}
	if s.dropNoopUndo() {
		return
	}
	s.clampCursor()
	s.desiredCol = s.Col
}

func (s *State) deleteCharsFwd() {
	if s.lineLen(s.Row) == 0 {
		return
	}
	s.pushUndo(false)
	var del strings.Builder
	rl := s.lineRunes(s.Row)
	if s.Col < len(rl) {
		end := clusterEndCol(s.Lines[s.Row], s.Col)
		del.WriteString(string(rl[s.Col:end]))
		s.Lines[s.Row] = string(rl[:s.Col]) + string(rl[end:])
	}
	if del.Len() > 0 {
		s.register = del.String()
		s.registerLinewise = false
	}
	if s.dropNoopUndo() {
		return
	}
	s.clampCursor()
	s.desiredCol = s.Col
}

// deleteChars implements x (forward) and X (backward). Both fill the
// unnamed register with the deleted text.
func (s *State) deleteChars(backward bool) {
	if backward {
		s.deleteCharsBwd()
		return
	}
	s.deleteCharsFwd()
}

// deleteToEnd implements D (change=false) and C (change=true).
func (s *State) deleteToEnd(change bool) {
	rl := s.lineRunes(s.Row)
	if s.Col >= len(rl) {
		if change {
			s.Mode = Insert
		}
		return
	}
	s.pushUndo(false)
	s.register = string(rl[s.Col:])
	s.registerLinewise = false
	s.Lines[s.Row] = string(rl[:s.Col])
	if s.dropNoopUndo() {
		return
	}
	if change {
		s.Mode = Insert
		s.Col = s.lineLen(s.Row)
		s.desiredCol = s.Col
		return
	}
	s.clampCursor()
	s.desiredCol = s.Col
}

// doPaste implements p/P.
func (s *State) doPaste(after bool) {
	if s.register == "" {
		return
	}
	s.pushUndo(false)
	s.paste(after)
	s.dropNoopUndo()
}

// visualPaste implements Visual p: the register replaces the selection.
// The unnamed register is preserved (Vim would overwrite it with the
// replaced text, but a prompt editor keeping the yank is the less
// surprising behaviour and matches vimtea's intent).
func (s *State) visualPaste() {
	if s.register == "" {
		s.Mode = Normal
		return
	}
	st, en, lw := s.visualRange()
	oldReg, oldLW := s.register, s.registerLinewise
	s.pushUndo(false)
	s.deleteRange(st, en, lw)
	s.register, s.registerLinewise = oldReg, oldLW
	// The cursor sits at the start of the deleted selection, so the
	// register goes in *before* it (P semantics): the selection is
	// replaced, not appended to.
	s.paste(false)
	s.Mode = Normal
	s.dropNoopUndo()
}

// visReplace implements Visual-mode r{char}: every character the
// selection covers is replaced with arg. A line-wise selection covers
// every character of every selected line, so the lines keep their
// length — the same convention the clone's other line-wise visual
// commands (~, u, U) use.
func (s *State) visReplace(arg rune) bool {
	if arg == 0 {
		return false
	}
	st, en, lw := s.visualRange()
	pr, pc := s.Row, s.Col
	before := copyLines(s.Lines)
	changed := false

	for r := st.row; r <= en.row && r < len(s.Lines); r++ {
		rl := []rune(s.Lines[r])
		lo, hi := 0, len(rl)-1
		if !lw {
			if r == st.row {
				lo = st.col
			}
			if r == en.row {
				hi = en.col
			}
		}
		if hi < lo {
			continue
		}
		rowChanged := false
		for i := lo; i <= hi && i < len(rl); i++ {
			if rl[i] == arg {
				continue
			}
			rl[i] = arg
			rowChanged = true
		}
		// A replacement that changes nothing leaves no undo step (the
		// reference's own rule for the empty undo step).
		if rowChanged {
			s.Lines[r] = string(rl)
			changed = true
		}
	}

	if changed {
		s.recordUndo(snapshot{before, pr, pc})
	}
	s.Row, s.Col = st.row, st.col
	s.clampCursor()
	s.desiredCol = s.Col
	return changed
}

// toggleCaseAtCursor implements Normal-mode ~: flip the case of the
// character under the cursor and leave the cursor after it.
func (s *State) toggleCaseAtCursor() {
	s.pushUndo(false)
	rl := s.lineRunes(s.Row)
	if s.Col < len(rl) {
		rl[s.Col] = toggleCase(rl[s.Col])
		s.Lines[s.Row] = string(rl)
		if s.Col < s.lineLen(s.Row)-1 {
			s.Col = nextClusterCol(s.Lines[s.Row], s.Col)
		}
	}
	if s.dropNoopUndo() {
		return
	}
	s.clampCursor()
	s.desiredCol = s.Col
}
