package vim

// cmdKind is the resolved action for one Normal/Visual key sequence.
type cmdKind uint8

const (
	cmdNone cmdKind = iota
	cmdMotion
	cmdInsertEntry   // i a I A o O
	cmdOperator      // d c y (+ motion/object/linewise)
	cmdDeleteChar    // x
	cmdDeleteCharBwd // X
	cmdDeleteToEnd   // D
	cmdChangeToEnd   // C
	cmdPaste         // p
	cmdPasteBefore   // P
	cmdUndo          // u
	cmdRedo          // ctrl+r
	cmdSubmit        // ZZ
	cmdReplaceChar   // r
	cmdVisualChar    // v
	cmdVisualLine    // V
	cmdCaseToggle    // ~
	cmdObject        // visual-mode text object (extends the selection)
	cmdVisDelete     // visual d / x
	cmdVisChange     // visual c
	cmdVisYank       // visual y
	cmdVisPaste      // visual p
	cmdVisCase       // visual ~ / u / U
)

// command is one parsed Normal/Visual sequence.
type command struct {
	kind     cmdKind
	count    int // leading count (before the operator/action)
	opCount  int // count between an operator and its motion/object
	op       rune
	motion   motion
	arg      rune
	obj      objectSpec
	linewise bool
	insert   rune
	caseOp   rune
}

// parseStatus is the tri-state result of parsing a key sequence.
type parseStatus uint8

const (
	statusIncomplete parseStatus = iota
	statusInvalid
	statusComplete
)

// parse resolves an accumulated Normal/Visual key sequence. visual
// switches on the disambiguation rules that only exist in Visual mode
// (d/c/y/x are complete operations rather than operator prefixes, and
// i/a start text objects instead of insert entries).
func parse(seq string, visual bool) (command, parseStatus) {
	i := 0
	count := readCount(seq, &i)
	rest := seq[i:]
	if rest == "" {
		if count > 0 {
			return command{}, statusIncomplete
		}
		return command{}, statusInvalid
	}

	if visual {
		if c, st := parseVisual(rest, count); st != statusInvalid {
			return c, st
		}
	}

	// Operators d/c/y: either doubled (linewise) or followed by a count
	// plus a motion or text object.
	if rest[0] == 'd' || rest[0] == 'c' || rest[0] == 'y' {
		return parseOperator(rest, count)
	}

	if c, st := parseMotionSeq(rest, count); st != statusInvalid {
		return c, st
	}

	switch rest[0] {
	case 'i', 'a', 'I', 'A', 'o', 'O':
		if len(rest) != 1 {
			return command{}, statusInvalid
		}
		return command{kind: cmdInsertEntry, insert: rune(rest[0]), count: count}, statusComplete
	case 'x':
		return one(rest, cmdDeleteChar, count)
	case 'X':
		return one(rest, cmdDeleteCharBwd, count)
	case 'D':
		return one(rest, cmdDeleteToEnd, count)
	case 'C':
		return one(rest, cmdChangeToEnd, count)
	case 'p':
		return one(rest, cmdPaste, count)
	case 'P':
		return one(rest, cmdPasteBefore, count)
	case 'u':
		return one(rest, cmdUndo, count)
	case '~':
		return one(rest, cmdCaseToggle, count)
	case 'v':
		return one(rest, cmdVisualChar, count)
	case 'V':
		return one(rest, cmdVisualLine, count)
	case 'Z':
		// ZZ writes-and-quits in Vim; here it maps to the reference's
		// "submit the current query" (its own /help labels it that way).
		if len(rest) == 1 {
			return command{}, statusIncomplete
		}
		if rest == "ZZ" {
			return command{kind: cmdSubmit}, statusComplete
		}
		return command{}, statusInvalid
	case 'r':
		if len(rest) == 1 {
			return command{}, statusIncomplete
		}
		if len(rest) != 2 {
			return command{}, statusInvalid
		}
		return command{kind: cmdReplaceChar, arg: rune(rest[1]), count: count}, statusComplete
	}
	return command{}, statusInvalid
}

// one builds a complete single-key command, rejecting stray trailing
// characters.
func one(rest string, kind cmdKind, count int) (command, parseStatus) {
	if len(rest) != 1 {
		return command{}, statusInvalid
	}
	return command{kind: kind, count: count}, statusComplete
}

// parseVisual handles the Visual-only keys.
func parseVisual(rest string, count int) (command, parseStatus) {
	switch rest[0] {
	case 'd', 'x':
		return one(rest, cmdVisDelete, count)
	case 'c':
		return one(rest, cmdVisChange, count)
	case 'y':
		return one(rest, cmdVisYank, count)
	case 'p':
		return one(rest, cmdVisPaste, count)
	case '~', 'u', 'U':
		if len(rest) != 1 {
			return command{}, statusInvalid
		}
		return command{kind: cmdVisCase, caseOp: rune(rest[0]), count: count}, statusComplete
	}
	if spec, st := objectFromKey(rest); st != statusInvalid {
		if st == statusIncomplete {
			return command{}, statusIncomplete
		}
		return command{kind: cmdObject, obj: spec, count: count}, statusComplete
	}
	return command{}, statusInvalid
}

// parseOperator resolves an operator sequence (d/c/y plus optional count
// and motion or text object).
func parseOperator(rest string, count int) (command, parseStatus) {
	op := rune(rest[0])
	r := rest[1:]
	if r == "" {
		return command{kind: cmdOperator, op: op, count: count}, statusIncomplete
	}
	if r[0] == rest[0] { // dd cc yy
		if len(r) != 1 {
			return command{}, statusInvalid
		}
		return command{kind: cmdOperator, op: op, count: count, linewise: true}, statusComplete
	}
	j := 0
	opCount := readCount(r, &j)
	r2 := r[j:]
	if r2 == "" {
		return command{kind: cmdOperator, op: op, count: count, opCount: opCount}, statusIncomplete
	}
	if spec, st := objectFromKey(r2); st != statusInvalid {
		if st == statusIncomplete {
			return command{kind: cmdOperator, op: op, count: count, opCount: opCount}, statusIncomplete
		}
		return command{kind: cmdOperator, op: op, count: count, opCount: opCount, obj: spec}, statusComplete
	}
	if m, arg, st := motionFromKey(r2); st != statusInvalid {
		if st == statusIncomplete {
			return command{kind: cmdOperator, op: op, count: count, opCount: opCount}, statusIncomplete
		}
		return command{kind: cmdOperator, op: op, count: count, opCount: opCount, motion: m, arg: arg}, statusComplete
	}
	return command{}, statusInvalid
}

// parseMotionSeq resolves a bare motion sequence.
func parseMotionSeq(rest string, count int) (command, parseStatus) {
	m, arg, st := motionFromKey(rest)
	if st == statusInvalid {
		return command{}, statusInvalid
	}
	if st == statusIncomplete {
		return command{}, statusIncomplete
	}
	return command{kind: cmdMotion, motion: m, arg: arg, count: count}, statusComplete
}

// readCount reads a leading count, which must start with 1-9 so that a
// leading 0 stays available as the line-start motion.
func readCount(seq string, i *int) int {
	if *i >= len(seq) {
		return 0
	}
	c := seq[*i]
	if c < '1' || c > '9' {
		return 0
	}
	n := 0
	for *i < len(seq) && seq[*i] >= '0' && seq[*i] <= '9' {
		n = n*10 + int(seq[*i]-'0')
		*i++
	}
	return n
}

// motionFromKey maps a motion key sequence to a motion. f/F/t/T consume
// one following character; gg consumes two.
func motionFromKey(rest string) (motion, rune, parseStatus) {
	switch rest[0] {
	case 'h':
		return motionLeft, 0, complete(rest)
	case 'l':
		return motionRight, 0, complete(rest)
	case 'j':
		return motionDown, 0, complete(rest)
	case 'k':
		return motionUp, 0, complete(rest)
	case 'w':
		return motionWordNext, 0, complete(rest)
	case 'b':
		return motionWordBack, 0, complete(rest)
	case 'e':
		return motionWordEnd, 0, complete(rest)
	case 'W':
		return motionWORDNext, 0, complete(rest)
	case 'B':
		return motionWORDBack, 0, complete(rest)
	case 'E':
		return motionWORDEnd, 0, complete(rest)
	case '0':
		return motionLineStart, 0, complete(rest)
	case '^':
		return motionFirstNonBlank, 0, complete(rest)
	case '$':
		return motionLineEnd, 0, complete(rest)
	case 'G':
		return motionDocEnd, 0, complete(rest)
	case ';':
		return motionRepeatFind, 0, complete(rest)
	case ',':
		return motionRepeatFindRev, 0, complete(rest)
	case 'g':
		if len(rest) == 1 {
			return 0, 0, statusIncomplete
		}
		if rest == "gg" {
			return motionDocStart, 0, statusComplete
		}
		return 0, 0, statusInvalid
	case 'f', 'F', 't', 'T':
		if len(rest) == 1 {
			return 0, 0, statusIncomplete
		}
		if len(rest) != 2 {
			return 0, 0, statusInvalid
		}
		arg := rune(rest[1])
		var m motion
		switch rest[0] {
		case 'f':
			m = motionFind
		case 'F':
			m = motionFindBack
		case 't':
			m = motionTill
		case 'T':
			m = motionTillBack
		}
		return m, arg, statusComplete
	}
	return 0, 0, statusInvalid
}

// complete reports statusComplete when rest is exactly one key.
func complete(rest string) parseStatus {
	if len(rest) != 1 {
		return statusInvalid
	}
	return statusComplete
}

// objectFromKey maps "i"/"a" plus an object key to a text-object spec.
func objectFromKey(rest string) (objectSpec, parseStatus) {
	if len(rest) < 1 || (rest[0] != 'i' && rest[0] != 'a') {
		return objectSpec{}, statusInvalid
	}
	if len(rest) < 2 {
		return objectSpec{}, statusIncomplete
	}
	spec := objectSpec{around: rest[0] == 'a'}
	switch rest[1:2] {
	case "w":
		spec.kind = objWord
	case "W":
		spec.kind = objWORD
	case "p":
		spec.kind = objParagraph
	case "(", ")", "b":
		spec.kind, spec.open, spec.close = objPair, '(', ')'
	case "[", "]":
		spec.kind, spec.open, spec.close = objPair, '[', ']'
	case "{", "}", "B":
		spec.kind, spec.open, spec.close = objPair, '{', '}'
	case "<", ">":
		spec.kind, spec.open, spec.close = objPair, '<', '>'
	case "\"":
		spec.kind, spec.open, spec.close, spec.quote = objPair, '"', '"', true
	case "'":
		spec.kind, spec.open, spec.close, spec.quote = objPair, '\'', '\'', true
	case "`":
		spec.kind, spec.open, spec.close, spec.quote = objPair, '`', '`', true
	default:
		return objectSpec{}, statusInvalid
	}
	if len(rest) != 2 {
		return objectSpec{}, statusInvalid
	}
	return spec, statusComplete
}
