package vim

import "strings"

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

// --- Insert mode ------------------------------------------------------

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
	case "left":
		if s.Col > 0 {
			s.Col = prevClusterCol(s.Lines[s.Row], s.Col)
			s.desiredCol = s.Col
		}
		return Result{Consumed: true}
	case "right":
		if s.Col < s.lineLen(s.Row) {
			s.Col = nextClusterCol(s.Lines[s.Row], s.Col)
			s.desiredCol = s.Col
		}
		return Result{Consumed: true}
	case "up":
		s.moveVertical(-1)
		return Result{Consumed: true}
	case "down":
		s.moveVertical(1)
		return Result{Consumed: true}
	case "home":
		s.Col = 0
		s.desiredCol = 0
		return Result{Consumed: true}
	case "end":
		s.Col = s.lineLen(s.Row)
		s.desiredCol = s.Col
		return Result{Consumed: true}
	case "tab":
		// Tab belongs to the host (picker completion / mode cycle).
		return Result{}
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

// --- Normal / Visual mode --------------------------------------------

func (s *State) handleNormal(k Key) Result {
	if k.Mod&Ctrl != 0 {
		switch {
		case k.Name == "enter" || k.Rune == 's':
			return Result{Consumed: true, Submit: true}
		case k.Rune == 'r':
			s.Pending = ""
			s.redoOp()
			return Result{Consumed: true}
		case k.Rune == 'j':
			// Vim's <C-j> is a plain down-motion. Consume it here so the
			// host's insert-newline fallback never fires in Normal mode.
			s.Pending = ""
			s.applyMotion(motionDown, 1, 0)
			return Result{Consumed: true}
		}
		return Result{} // other chords (ctrl+o/g/c/d/l…) belong to the host
	}
	if k.Name == "esc" {
		s.Pending = ""
		if s.Mode == Visual || s.Mode == VisualLine {
			s.Mode = Normal
		}
		s.breakUndo()
		return Result{Consumed: true}
	}
	switch k.Name {
	case "enter":
		// vim.normal.submit itself binds only ctrl+s / ctrl+enter, so a
		// bare Enter misses that map and falls through to the global one,
		// where the reference's PromptModel submits on KeyEnter whenever
		// the mode is not Insert. Normal, Visual and Visual-Line all
		// submit; only Insert treats Enter as "newline" (see handleInsert).
		return Result{Consumed: true, Submit: true}
	case "left":
		s.Pending = ""
		s.applyMotion(motionLeft, 1, 0)
		return Result{Consumed: true}
	case "right":
		s.Pending = ""
		s.applyMotion(motionRight, 1, 0)
		return Result{Consumed: true}
	case "up":
		s.Pending = ""
		s.applyMotion(motionUp, 1, 0)
		return Result{Consumed: true}
	case "down":
		s.Pending = ""
		s.applyMotion(motionDown, 1, 0)
		return Result{Consumed: true}
	case "home":
		s.Pending = ""
		s.applyMotion(motionLineStart, 1, 0)
		return Result{Consumed: true}
	case "end":
		s.Pending = ""
		s.applyMotion(motionLineEnd, 1, 0)
		return Result{Consumed: true}
	}
	if k.Rune == 0 {
		return Result{}
	}
	s.Pending += string(k.Rune)
	c, st := parse(s.Pending, s.Mode == Visual || s.Mode == VisualLine)
	switch st {
	case statusIncomplete:
		return Result{Consumed: true}
	case statusInvalid:
		s.Pending = ""
		return Result{Consumed: true, Bell: true}
	}
	s.Pending = ""
	return s.execute(c)
}

// execute runs a parsed command.
func (s *State) execute(c command) Result {
	switch c.kind {
	case cmdMotion:
		s.recordFind(c.motion, c.arg)
		ok := s.applyMotion(c.motion, max(c.count, 1), c.arg)
		return Result{Consumed: true, Bell: !ok}
	case cmdInsertEntry:
		s.insertEntry(c.insert)
		return Result{Consumed: true}
	case cmdOperator:
		s.runOperator(c)
		return Result{Consumed: true}
	case cmdSubmit:
		// ZZ: submit without leaving Normal mode.
		return Result{Consumed: true, Submit: true}
	case cmdDeleteChar:
		s.deleteChars(c.count, false)
		return Result{Consumed: true}
	case cmdDeleteCharBwd:
		s.deleteChars(c.count, true)
		return Result{Consumed: true}
	case cmdDeleteToEnd:
		s.deleteToEnd(false)
		return Result{Consumed: true}
	case cmdChangeToEnd:
		s.deleteToEnd(true)
		return Result{Consumed: true}
	case cmdPaste:
		s.doPaste(true, max(c.count, 1))
		return Result{Consumed: true}
	case cmdPasteBefore:
		s.doPaste(false, max(c.count, 1))
		return Result{Consumed: true}
	case cmdUndo:
		for i := 0; i < max(c.count, 1); i++ {
			if !s.undoOp() {
				break
			}
		}
		return Result{Consumed: true}
	case cmdRedo:
		for i := 0; i < max(c.count, 1); i++ {
			if !s.redoOp() {
				break
			}
		}
		return Result{Consumed: true}
	case cmdReplaceChar:
		return Result{Consumed: true, Bell: !s.replaceChar(c.arg, max(c.count, 1))}
	case cmdCaseToggle:
		s.toggleCaseAtCursor(max(c.count, 1))
		return Result{Consumed: true}
	case cmdVisualChar:
		s.enterVisual(false)
		return Result{Consumed: true}
	case cmdVisualLine:
		s.enterVisual(true)
		return Result{Consumed: true}
	case cmdObject:
		if st, en, _, ok := s.objectRange(c.obj, c.obj.around, max(c.count, 1)); ok {
			s.anchorRow, s.anchorCol = st.row, st.col
			s.Row, s.Col = en.row, en.col
			s.clampCursor()
		}
		return Result{Consumed: true}
	case cmdVisDelete:
		s.pushUndo(false)
		st, en, lw := s.visualRange()
		s.deleteRange(st, en, lw)
		s.Mode = Normal
		s.dropNoopUndo()
		return Result{Consumed: true}
	case cmdVisChange:
		s.pushUndo(false)
		st, en, lw := s.visualRange()
		s.changeRange(st, en, lw)
		s.dropNoopUndo()
		return Result{Consumed: true}
	case cmdVisYank:
		st, en, lw := s.visualRange()
		s.yank(st, en, lw)
		s.Mode = Normal
		return Result{Consumed: true}
	case cmdVisPaste:
		s.visualPaste()
		return Result{Consumed: true}
	case cmdVisCase:
		st, en, lw := s.visualRange()
		s.applyCase(st, en, lw, c.caseOp)
		s.Mode = Normal
		return Result{Consumed: true}
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

// runOperator resolves and applies d/c/y.
func (s *State) runOperator(c command) {
	s.recordFind(c.motion, c.arg)
	mc := max(c.count, 1) * max(c.opCount, 1)
	var start, end pos
	var linewise, ok bool
	switch {
	case c.obj.kind != objNone:
		start, end, linewise, ok = s.objectRange(c.obj, c.obj.around, mc)
	case c.linewise || c.motion == 0:
		n := max(c.count, 1)
		a := s.Row
		b := min(s.Row+n-1, len(s.Lines)-1)
		start, end = pos{a, 0}, pos{b, max(s.lineLen(b)-1, 0)}
		linewise, ok = true, true
	default:
		m := c.motion
		// cw is Vim's documented exception to the motion: it behaves
		// like ce (it does not eat the following whitespace) whenever
		// the cursor starts on a non-blank.
		if c.op == 'c' && (m == motionWordNext || m == motionWORDNext) {
			rl := s.lineRunes(s.Row)
			if s.Col < len(rl) && classOf(rl[s.Col]) != 0 {
				if m == motionWordNext {
					m = motionWordEnd
				} else {
					m = motionWORDEnd
				}
			}
		}
		mt := s.motionTarget(m, mc, c.arg)
		start, end, linewise, ok = s.operatorRange(mt)
	}
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

// deleteChars implements x (forward) and X (backward). Both fill the
// unnamed register with the deleted text.
func (s *State) deleteChars(count int, backward bool) {
	count = max(count, 1)
	if backward {
		if s.Col == 0 {
			return
		}
		s.pushUndo(false)
		var del strings.Builder
		for i := 0; i < count && s.Col > 0; i++ {
			rl := s.lineRunes(s.Row)
			start := prevClusterCol(s.Lines[s.Row], s.Col)
			del.WriteString(string(rl[start:s.Col]))
			s.Lines[s.Row] = string(rl[:start]) + string(rl[s.Col:])
			s.Col = start
		}
		if del.Len() > 0 {
			s.register = reverseString(del.String())
			s.registerLinewise = false
		}
		if s.dropNoopUndo() {
			return
		}
		s.clampCursor()
		s.desiredCol = s.Col
		return
	}
	if s.lineLen(s.Row) == 0 {
		return
	}
	s.pushUndo(false)
	var del strings.Builder
	for i := 0; i < count; i++ {
		rl := s.lineRunes(s.Row)
		if s.Col >= len(rl) {
			break
		}
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

// doPaste implements p/P, repeated count times.
func (s *State) doPaste(after bool, count int) {
	if s.register == "" {
		return
	}
	s.pushUndo(false)
	for i := 0; i < count; i++ {
		s.paste(after)
	}
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

// toggleCaseAtCursor implements Normal-mode ~: flip the case of count
// characters and leave the cursor after the last one.
func (s *State) toggleCaseAtCursor(count int) {
	s.pushUndo(false)
	for i := 0; i < count; i++ {
		rl := s.lineRunes(s.Row)
		if s.Col >= len(rl) {
			break
		}
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
