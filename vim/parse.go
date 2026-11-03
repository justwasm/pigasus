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
	cmdRedo          // U (ctrl+r is the artifact review, not a redo)
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
	cmdVisReplace    // visual r{char}
)

// command is one parsed Normal/Visual sequence.
//
// There is no count field: the reference's commands take no count prefix
// (official-docs/cli/vim-editor-mode.md — "Commands take no count
// prefix, so `3dd` deletes one line"), so every command acts once.
type command struct {
	kind     cmdKind
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

func parseActionCommand(rest string) (command, parseStatus) {
	switch rest[0] {
	case 'i', 'a', 'I', 'A', 'o', 'O':
		if len(rest) != 1 {
			return command{}, statusInvalid
		}
		return command{kind: cmdInsertEntry, insert: rune(rest[0])}, statusComplete
	case 'x':
		return one(rest, cmdDeleteChar)
	case 'X':
		return one(rest, cmdDeleteCharBwd)
	case 'D':
		return one(rest, cmdDeleteToEnd)
	case 'C':
		return one(rest, cmdChangeToEnd)
	case 'p':
		return one(rest, cmdPaste)
	case 'P':
		return one(rest, cmdPasteBefore)
	case 'u':
		return one(rest, cmdUndo)
	case 'U':
		// In NORMAL mode "U" is the redo (the reference's own note:
		// "In NORMAL mode, `u` and `U` are undo and redo"). In VISUAL
		// mode "U" uppercases the selection — parseVisual claims it
		// before this switch is reached.
		return one(rest, cmdRedo)
	case '~':
		return one(rest, cmdCaseToggle)
	case 'v':
		return one(rest, cmdVisualChar)
	case 'V':
		return one(rest, cmdVisualLine)
	case 'Z':
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
		return command{kind: cmdReplaceChar, arg: rune(rest[1])}, statusComplete
	case '/', '!':
		// The reference's NORMAL-mode "/" and "!": the character is
		// inserted and the editor switches to INSERT mode, so a slash
		// command or a shell command works without pressing "i" first.
		if len(rest) != 1 {
			return command{}, statusInvalid
		}
		return command{kind: cmdInsertEntry, insert: rune(rest[0])}, statusComplete
	}
	return command{}, statusInvalid
}

// parse resolves an accumulated Normal/Visual key sequence. visual
// switches on the disambiguation rules that only exist in Visual mode
// (d/c/y/x are complete operations rather than operator prefixes, and
// i/a start text objects instead of insert entries).
//
// There is no count prefix: the reference's commands take none
// (official-docs/cli/vim-editor-mode.md — "Commands take no count prefix,
// so `3dd` deletes one line"), so a leading digit is an invalid key and
// rings the bell. "0" is still the line-start motion.
func parse(seq string, visual bool) (command, parseStatus) {
	rest := seq
	if rest == "" {
		return command{}, statusInvalid
	}

	if visual {
		if c, st := parseVisual(rest); st != statusInvalid {
			return c, st
		}
		// The "/" and "!" insert entries belong to NORMAL mode: in
		// VISUAL mode they would drop the selection.
		if rest[0] == '/' || rest[0] == '!' {
			return command{}, statusInvalid
		}
	}

	if rest[0] == 'd' || rest[0] == 'c' || rest[0] == 'y' {
		return parseOperator(rest)
	}

	if c, st := parseMotionSeq(rest); st != statusInvalid {
		return c, st
	}

	return parseActionCommand(rest)
}

// one builds a complete single-key command, rejecting stray trailing
// characters.
func one(rest string, kind cmdKind) (command, parseStatus) {
	if len(rest) != 1 {
		return command{}, statusInvalid
	}
	return command{kind: kind}, statusComplete
}

// parseVisual handles the Visual-only keys.
func parseVisual(rest string) (command, parseStatus) {
	switch rest[0] {
	case 'd', 'x':
		return one(rest, cmdVisDelete)
	case 'c':
		return one(rest, cmdVisChange)
	case 'y':
		return one(rest, cmdVisYank)
	case 'p':
		return one(rest, cmdVisPaste)
	case '~', 'u', 'U':
		if len(rest) != 1 {
			return command{}, statusInvalid
		}
		return command{kind: cmdVisCase, caseOp: rune(rest[0])}, statusComplete
	case 'r':
		// r{char}: every selected character is replaced with {char}.
		if len(rest) == 1 {
			return command{}, statusIncomplete
		}
		if len(rest) != 2 {
			return command{}, statusInvalid
		}
		return command{kind: cmdVisReplace, arg: rune(rest[1])}, statusComplete
	}
	if spec, st := objectFromKey(rest); st != statusInvalid {
		if st == statusIncomplete {
			return command{}, statusIncomplete
		}
		return command{kind: cmdObject, obj: spec}, statusComplete
	}
	return command{}, statusInvalid
}

// parseOperator resolves an operator sequence (d/c/y plus a motion or a
// text object).
func parseOperator(rest string) (command, parseStatus) {
	op := rune(rest[0])
	r := rest[1:]
	if r == "" {
		return command{kind: cmdOperator, op: op}, statusIncomplete
	}
	if r[0] == rest[0] { // dd cc yy
		if len(r) != 1 {
			return command{}, statusInvalid
		}
		return command{kind: cmdOperator, op: op, linewise: true}, statusComplete
	}
	if spec, st := objectFromKey(r); st != statusInvalid {
		if st == statusIncomplete {
			return command{kind: cmdOperator, op: op}, statusIncomplete
		}
		return command{kind: cmdOperator, op: op, obj: spec}, statusComplete
	}
	if m, arg, st := motionFromKey(r); st != statusInvalid {
		if st == statusIncomplete {
			return command{kind: cmdOperator, op: op}, statusIncomplete
		}
		return command{kind: cmdOperator, op: op, motion: m, arg: arg}, statusComplete
	}
	return command{}, statusInvalid
}

// parseMotionSeq resolves a bare motion sequence.
func parseMotionSeq(rest string) (command, parseStatus) {
	m, arg, st := motionFromKey(rest)
	if st == statusInvalid {
		return command{}, statusInvalid
	}
	if st == statusIncomplete {
		return command{}, statusIncomplete
	}
	return command{kind: cmdMotion, motion: m, arg: arg}, statusComplete
}

var basicMotions = map[byte]motion{
	'h': motionLeft,
	'l': motionRight,
	'j': motionDown,
	'k': motionUp,
	'w': motionWordNext,
	'b': motionWordBack,
	'e': motionWordEnd,
	'W': motionWORDNext,
	'B': motionWORDBack,
	'E': motionWORDEnd,
	'0': motionLineStart,
	'^': motionFirstNonBlank,
	'$': motionLineEnd,
	'G': motionDocEnd,
	';': motionRepeatFind,
	',': motionRepeatFindRev,
}

func parseFindMotion(rest string) (motion, rune, parseStatus) {
	if len(rest) == 1 {
		return 0, 0, statusIncomplete
	}
	if len(rest) != 2 {
		return 0, 0, statusInvalid
	}
	arg := rune(rest[1])
	switch rest[0] {
	case 'f':
		return motionFind, arg, statusComplete
	case 'F':
		return motionFindBack, arg, statusComplete
	case 't':
		return motionTill, arg, statusComplete
	case 'T':
		return motionTillBack, arg, statusComplete
	}
	return 0, 0, statusInvalid
}

// motionFromKey maps a motion key sequence to a motion. f/F/t/T consume
// one following character; gg consumes two.
func motionFromKey(rest string) (motion, rune, parseStatus) {
	if m, ok := basicMotions[rest[0]]; ok {
		return m, 0, complete(rest)
	}
	if rest[0] == 'g' {
		if len(rest) == 1 {
			return 0, 0, statusIncomplete
		}
		if rest == "gg" {
			return motionDocStart, 0, statusComplete
		}
		return 0, 0, statusInvalid
	}
	if rest[0] == 'f' || rest[0] == 'F' || rest[0] == 't' || rest[0] == 'T' {
		return parseFindMotion(rest)
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
