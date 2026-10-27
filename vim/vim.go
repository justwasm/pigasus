// Package vim implements the modal editing layer behind the clone's
// prompt editor ("Editor Mode › Vim").
//
// It is a pure, dependency-light state machine: the host owns the
// terminal widget and hands the engine one normalized key at a time,
// then reads the (unchanged) text plus the new cursor/mode back out.
// Nothing here knows about bubbletea, lipgloss or the run loop, so the
// whole surface is unit-testable without a terminal.
//
// The feature set is fixed by the reference's own changelog (see
// cmd/pigasus/testdata/print_changelog.md, entries collapsed under
// 1.1.11) and was re-confirmed against the decompiled Vim engine of the
// reference binary: Normal/Insert/Visual/Visual-Line modes, the
// h/j/k/l, w/b/e and W/B/E motions, the 0/^/$/gg/G boundaries, the
// f/F/t/T character searches with ;/, repeats, the i/a/I/A and o/O
// insert entries, the d/c/y operators alongside x, D and C filling the
// unnamed register for p, and the iw/aw/iW/aW/ip/ap text objects, with
// numeric count multipliers applying to operators, motions and
// actions. ZZ submits from Normal mode, like the reference's own /help
// documents.
//
// Two deliberate deviations from the binary, both recorded in the cycle
// report: `ip`/`ap` are kept because the 1.1.11 changelog promises them
// even though the shipped binary no-ops `dip`, and space is left
// unbound because the binary does not treat it as a right-motion.
package vim

import (
	"strings"
	"unicode"

	"github.com/rivo/uniseg"
)

// Mode is the active editing mode. The zero value is Normal, matching
// Vim's own default.
type Mode uint8

const (
	Normal Mode = iota
	Insert
	Visual
	VisualLine
)

// Badge returns the status-line mode badge. Normal mode has no badge —
// Vim shows nothing there — and the other three are byte-for-byte the
// reference's recovered strings ("-- INSERT --", "-- VISUAL --",
// "-- V-LINE --").
func (m Mode) Badge() string {
	switch m {
	case Insert:
		return "-- INSERT --"
	case Visual:
		return "-- VISUAL --"
	case VisualLine:
		return "-- V-LINE --"
	}
	return ""
}

// Mod is a bitmask of the modifier keys held with a key press.
type Mod uint8

const (
	Ctrl Mod = 1 << iota
	Alt
	Shift
)

// Key is one normalized key event. Exactly one of Rune/Name is set:
// Rune carries a printable character (including the space key) and
// Name carries a named key such as "esc" or "enter" for the keys the
// engine treats specially.
type Key struct {
	Rune rune
	Name string
	Mod  Mod
}

// Printable reports whether the key carries a printable rune with no
// modifier (the only shape Insert mode inserts literally).
func (k Key) Printable() bool { return k.Rune != 0 }

// Result reports what the engine did with a key.
type Result struct {
	// Consumed is true when the engine handled the key and the host
	// must not pass it on to the plain textarea.
	Consumed bool
	// Submit is true when the key asked for the prompt to be sent
	// (the vim.insert.submit / vim.normal.submit bindings).
	Submit bool
	// Bell is true when the key completed an invalid sequence; the
	// host may flash but need not do anything else.
	Bell bool
}

// snapshot is one undo step: the full buffer plus the cursor to
// restore. Prompts are small, so whole-buffer snapshots are cheap.
type snapshot struct {
	lines    []string
	row, col int
}

// State is the whole modal editor. Construct it with New and drive it
// with Handle; read Text/Row/Col/Mode back out afterwards.
//
// Fields are exported so the host can seed and inspect them directly,
// but only Line/Col/Mode/InsertFirst should be written from outside.
type State struct {
	// Lines is the buffer, one string per logical line. It never
	// contains a '\n'; the host joins it with "\n" for display.
	Lines []string
	// Row/Col are the cursor, in rune indices (Col counts runes on
	// Lines[Row]). Files/motions that straddle grapheme clusters step
	// whole clusters, but the stored position stays a rune index.
	Row, Col int
	Mode     Mode
	// InsertFirst mirrors the "Editor Mode › Insert First" setting:
	// when on, the prompt opens in Insert mode and a bare Enter
	// submits rather than inserting a newline.
	InsertFirst bool

	// Pending is the unfinished Normal/Visual key sequence (e.g. "d"
	// while the engine waits for the motion of "dw"). It is exposed
	// for tests.
	Pending string

	// register is the unnamed register filled by yanks/deletes and
	// read by p/P. registerLinewise records whether it holds whole
	// lines (yy/dd), which changes how p/P splice it back.
	register         string
	registerLinewise bool

	// desiredCol is the sticky column for j/k (and gg/G): vertical
	// motion tries to return to this column so a run of j/k down
	// ragged lines does not drift left.
	desiredCol int

	// anchor is the fixed end of a Visual selection.
	anchorRow, anchorCol int

	// lastFind remembers the most recent f/F/t/T for ; and , repeats.
	lastFind       pos
	lastFindMotion motion
	lastFindArg    rune
	lastFindOK     bool

	undo []snapshot
	redo []snapshot
	// undoOpen marks an in-progress coalesced edit run (a sequence of
	// inserted characters sharing one undo step).
	undoOpen bool
}

// New returns a fresh engine over text, in Normal mode. The Insert
// First setting is applied separately with SetInsertFirst, because it
// is read from the live settings file rather than baked into the
// buffer's contents.
func New(text string) *State {
	s := &State{Lines: strings.Split(text, "\n")}
	s.clampCursor()
	return s
}

// SetInsertFirst applies the "Editor Mode › Insert First" setting. When
// it turns on, the prompt opens in Insert mode; toggling the setting
// also flips the current mode so the change is immediately visible.
func (s *State) SetInsertFirst(v bool) {
	if s.InsertFirst == v {
		return
	}
	s.InsertFirst = v
	if v {
		s.Mode = Insert
	} else if s.Mode == Insert {
		s.Mode = Normal
	}
	s.clampCursor()
}

// SetText replaces the buffer and re-clamps the cursor. The host calls
// it when something outside the engine (history recall, a /-command
// substitution, the external editor) changes the prompt text.
func (s *State) SetText(text string) {
	s.Lines = strings.Split(text, "\n")
	s.Pending = ""
	s.clampCursor()
}

// Text returns the buffer as one string with "\n" line separators.
func (s *State) Text() string { return strings.Join(s.Lines, "\n") }

// Value is an alias for Text, named to match the textarea API the host
// syncs from.
func (s *State) Value() string { return s.Text() }

// LineCount returns the number of logical lines (always ≥ 1).
func (s *State) LineCount() int { return len(s.Lines) }

// lineRunes returns line row as a rune slice, or nil when out of range.
func (s *State) lineRunes(row int) []rune {
	if row < 0 || row >= len(s.Lines) {
		return nil
	}
	return []rune(s.Lines[row])
}

// lineLen returns the rune length of line row (0 when out of range).
func (s *State) lineLen(row int) int { return len(s.lineRunes(row)) }

// isBlank reports whether every line is empty/whitespace.
func (s *State) isBlank() bool {
	for _, l := range s.Lines {
		if strings.TrimSpace(l) != "" {
			return false
		}
	}
	return true
}

// clampCursor keeps (Row,Col) inside the buffer and applies the
// Normal-vs-Insert column rule: Insert may rest one past the last
// character (the append point), Normal/Visual must sit on a character
// and therefore stop at len-1 (or 0 on an empty line).
func (s *State) clampCursor() {
	if s.Row < 0 {
		s.Row = 0
	}
	if s.Row >= len(s.Lines) {
		s.Row = len(s.Lines) - 1
	}
	n := s.lineLen(s.Row)
	if s.Mode == Insert {
		if s.Col > n {
			s.Col = n
		}
	} else {
		if n == 0 {
			s.Col = 0
		} else if s.Col >= n {
			s.Col = n - 1
		}
	}
	if s.Col < 0 {
		s.Col = 0
	}
}

// --- grapheme helpers -------------------------------------------------

// clusters returns the rune start/end offset pairs of the grapheme
// clusters in line. Motions and single-character edits step whole
// clusters so emoji (👩‍💻) and combining sequences are never split.
func clusters(line string) [][2]int {
	if line == "" {
		return nil
	}
	var out [][2]int
	g := uniseg.NewGraphemes(line)
	runePos := 0
	for g.Next() {
		rs := g.Runes()
		n := len(rs)
		out = append(out, [2]int{runePos, runePos + n})
		runePos += n
	}
	return out
}

// nextClusterCol returns the rune column of the first cluster boundary
// strictly after col (or the line length).
func nextClusterCol(line string, col int) int {
	for _, c := range clusters(line) {
		if c[0] > col {
			return c[0]
		}
	}
	if l := len([]rune(line)); col < l {
		return l
	}
	return col
}

// prevClusterCol returns the rune column of the last cluster boundary
// strictly before col (or 0).
func prevClusterCol(line string, col int) int {
	prev := 0
	for _, c := range clusters(line) {
		if c[0] >= col {
			return prev
		}
		prev = c[0]
	}
	return prev
}

// clusterEndCol returns the rune column just past the cluster that
// contains col (the exclusive end used by x and r).
func clusterEndCol(line string, col int) int {
	for _, c := range clusters(line) {
		if col >= c[0] && col < c[1] {
			return c[1]
		}
	}
	return col
}

// isWordChar reports whether r is a Vim "word" character (letters,
// digits and underscore). Everything else — including punctuation — is
// treated as a separator by the w/b/e classifier.
func isWordChar(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// isWORDChar reports whether r belongs to a Vim "WORD" (any run of
// non-blank characters, so punctuation counts).
func isWORDChar(r rune) bool { return !unicode.IsSpace(r) }

// isSpaceRune reports whether r is horizontal whitespace.
func isSpaceRune(r rune) bool { return r == ' ' || r == '\t' }
