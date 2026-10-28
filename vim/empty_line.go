package vim

// actOnLineAbove moves the cursor off an empty last line, so that the
// command the user typed acts on the line above it.
//
// 【一手】 agy 1.4.9, the vendor's own release notes: "commands on an
// empty last line acting on the line above".
//
// A prompt that ends with a newline has an empty last line: the text
// stops there and the line itself holds nothing. There is nothing for a
// Normal-mode command to act on, so the command runs on the line above
// — at that line's last character. The empty line is the continuation
// of the text before it, so the cursor's logical position there is the
// end of that text: "hello\n" plus `x` drops the "o", the way `x` drops
// the last character of a line that ends with the cursor.
//
// Only the commands that act *at the cursor* move: the insert entries
// (i/a/I/A/o/O) must still reach the empty line, and the motions keep
// their own rules (`j` at the last line stays, `k` moves up).
//
// The empty line itself is not touched — it stays in the buffer, so the
// text the host reads back is unchanged.
func (s *State) actOnLineAbove() {
	if s.Mode == Insert {
		return
	}
	if s.Row != len(s.Lines)-1 || s.Row == 0 {
		return
	}
	if s.Lines[s.Row] != "" {
		return
	}
	prev := s.Row - 1
	s.Row = prev
	if n := s.lineLen(prev); n > 0 {
		s.Col = n - 1
	} else {
		s.Col = 0
	}
	s.desiredCol = s.Col
}

// actsAtCursor reports whether a command acts at the cursor position —
// and therefore has to leave an empty last line for the line above.
func actsAtCursor(k cmdKind) bool {
	switch k {
	case cmdDeleteChar, cmdDeleteCharBwd, cmdDeleteToEnd, cmdChangeToEnd,
		cmdReplaceChar, cmdCaseToggle:
		return true
	}
	return false
}
