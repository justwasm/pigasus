package vim

// objectKind identifies a text object asked for by i/a followed by a
// key: iw, aw, iW, aW, ip, ap and the bracket/quote pairs.
type objectKind uint8

const (
	// objNone must stay the zero value: an unset objectSpec has to mean
	// "no text object" or every operator would take the object branch.
	objNone objectKind = iota
	objWord
	objWORD
	objParagraph
	objPair // bracket or quote pair, delimiters in objOpen/objClose
)

// objectSpec is a resolved text object request. around records whether
// the key was the a-prefixed form (aw/ip/a") rather than the inner form:
// operators and Visual mode both need that bit to pick the range.
type objectSpec struct {
	kind   objectKind
	open   rune
	close  rune
	quote  bool
	around bool
}

// blankLine reports whether line row is empty or all whitespace.
func (s *State) blankLine(row int) bool {
	if row < 0 || row >= len(s.Lines) {
		return true
	}
	return trimBlank(s.Lines[row])
}

// trimBlank is a tiny local TrimSpace check avoiding the strings import
// in the hot path.
func trimBlank(line string) bool {
	for _, r := range line {
		if !isSpaceRune(r) {
			return false
		}
	}
	return true
}

// objectRange resolves a text object to an inclusive (start,end) range.
// around selects the a-prefixed form (which swallows surrounding
// whitespace / the delimiters themselves). count picks the nesting level
// for bracket and quote pairs (2di( reaches one level out), matching the
// reference changelog's counted text objects.
func (s *State) objectRange(spec objectSpec, around bool, count int) (start, end pos, linewise, ok bool) {
	if count < 1 {
		count = 1
	}
	switch spec.kind {
	case objWord:
		return s.wordObject(around, false)
	case objWORD:
		return s.wordObject(around, true)
	case objParagraph:
		return s.paragraphObject(around)
	case objPair:
		return s.pairObject(spec, around, count)
	}
	return pos{}, pos{}, false, false
}

// wordObject implements iw/aw (big=false) and iW/aW (big=true) on the
// current line, matching Vim's rule that these objects never span a
// line break.
func (s *State) wordObject(around, big bool) (pos, pos, bool, bool) {
	class := classFn(big)
	rl := s.lineRunes(s.Row)
	if len(rl) == 0 {
		return pos{}, pos{}, false, false
	}
	col := s.Col
	if col >= len(rl) {
		col = len(rl) - 1
	}
	if col < 0 {
		return pos{}, pos{}, false, false
	}
	cl := class(rl[col])
	start, end := col, col
	for start > 0 && class(rl[start-1]) == cl {
		start--
	}
	for end+1 < len(rl) && class(rl[end+1]) == cl {
		end++
	}
	if around {
		// Prefer trailing whitespace; fall back to leading when the
		// object sits at the end of the line, exactly like aw.
		ws := end + 1
		for ws < len(rl) && class(rl[ws]) == 0 {
			ws++
		}
		if ws > end+1 {
			end = ws - 1
		} else {
			for start > 0 && class(rl[start-1]) == 0 {
				start--
			}
		}
	}
	return pos{s.Row, start}, pos{s.Row, end}, false, true
}

// paragraphObject implements ip/ap: a run of consecutive non-blank
// lines (a blank run when the cursor is itself on a blank line). ap
// additionally swallows the trailing blank lines, as Vim does.
func (s *State) paragraphObject(around bool) (pos, pos, bool, bool) {
	blank := s.blankLine(s.Row)
	start, end := s.Row, s.Row
	for start > 0 && s.blankLine(start-1) == blank {
		start--
	}
	for end+1 < len(s.Lines) && s.blankLine(end+1) == blank {
		end++
	}
	if around {
		for end+1 < len(s.Lines) && s.blankLine(end+1) {
			end++
		}
	}
	return pos{start, 0}, pos{end, max(s.lineLen(end)-1, 0)}, true, true
}

// pairObject implements i(/a(/i"/a" and friends. It first looks for the
// innermost pair enclosing the cursor on the current line; if there is
// none it takes the next pair opening after the cursor, matching Vim.
// count walks outward through the enclosing pairs.
func (s *State) pairObject(spec objectSpec, around bool, count int) (pos, pos, bool, bool) {
	rl := s.lineRunes(s.Row)
	if len(rl) == 0 {
		return pos{}, pos{}, false, false
	}
	inner, ok := pairBounds(rl, s.Col, spec, count)
	if !ok {
		return pos{}, pos{}, false, false
	}
	start, end := inner[0], inner[1]
	if spec.quote {
		// The quotes themselves are at start-1 / end+1.
		if around {
			start--
			end++
		}
	} else {
		// The brackets are at start-1 / end+1.
		if around {
			start--
			end++
			// a( also swallows one trailing space, like Vim.
			if end+1 < len(rl) && rl[end+1] == ' ' {
				end++
			}
		}
	}
	if start < 0 {
		start = 0
	}
	return pos{s.Row, start}, pos{s.Row, end}, false, true
}

// pairBounds finds the delimiter pair enclosing col, or the first pair
// starting after col. count selects the nesting level (1 = innermost,
// clamped to the outermost enclosing pair). It returns the inclusive
// bounds of the *contents* (delimiters excluded).
func pairBounds(rl []rune, col int, spec objectSpec, count int) ([2]int, bool) {
	if count < 1 {
		count = 1
	}
	if spec.quote {
		var pairs [][2]int
		i := 0
		for i < len(rl) {
			if rl[i] == spec.open {
				j := i + 1
				for j < len(rl) && rl[j] != spec.open {
					j++
				}
				if j < len(rl) {
					pairs = append(pairs, [2]int{i + 1, j - 1})
					i = j + 1
					continue
				}
			}
			i++
		}
		return pickPair(pairs, col, count)
	}
	// Bracket pairs with nesting. Nested pairs are appended
	// inner-closes-first, so the enclosing list is naturally ordered
	// innermost-first.
	var stack []int
	var pairs [][2]int
	for i, r := range rl {
		switch r {
		case spec.open:
			stack = append(stack, i)
		case spec.close:
			if n := len(stack); n > 0 {
				o := stack[n-1]
				stack = stack[:n-1]
				pairs = append(pairs, [2]int{o + 1, i - 1})
			}
		}
	}
	return pickPair(pairs, col, count)
}

// pickPair chooses the enclosing pair at nesting level count (inner-first,
// clamped to the outermost), else the first pair that opens after col.
func pickPair(pairs [][2]int, col, count int) ([2]int, bool) {
	var enc [][2]int
	for _, p := range pairs {
		// p[0]-1 is the opening delimiter position.
		if p[0]-1 <= col && col <= p[1]+1 {
			enc = append(enc, p)
		}
	}
	if len(enc) > 0 {
		idx := count - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(enc) {
			idx = len(enc) - 1
		}
		return enc[idx], true
	}
	for _, p := range pairs {
		if p[0]-1 > col {
			return p, true
		}
	}
	return [2]int{}, false
}
