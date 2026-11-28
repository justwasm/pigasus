// statusrow_test.go — the status row's own tests.
//
// The row is a view: it takes the facts and writes the line. These
// tests drive it directly with a State; the host's wiring (where the
// facts come from) is tested in internal/cliapp.

package statusrow

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/palette"
)

// plain strips the SGR escapes so a test can assert the text.
func plain(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case inEscape:
			if r == 'm' {
				inEscape = false
			}
		case r == 0x1b:
			inEscape = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// base is the state of an idle session.
func base() State {
	return State{
		Width:          220,
		CanonicalWidth: 220,
		BufferEmpty:    true,
		Model:          "Gemini 3.8 Flash",
	}
}

// TestRender_Idle — the idle row's two segments and its padding.
//
// The row is "? for shortcuts" on the left, the model on the right, and
// the space between them.
func TestRender_Idle(t *testing.T) {
	row := Render(base())
	text := plain(row)
	if !strings.HasPrefix(text, "? for shortcuts") {
		t.Errorf("row = %q, want the hint on the left", text)
	}
	if !strings.HasSuffix(text, "Gemini 3.8 Flash") {
		t.Errorf("row = %q, want the model on the right", text)
	}
	if len([]rune(text)) != 220 {
		t.Errorf("row is %d cells wide, want 220", len([]rune(text)))
	}
}

// TestRender_Padding — the row is exactly the terminal's width, and the
// padding is at least one cell.
func TestRender_Padding(t *testing.T) {
	s := base()
	s.Width = 80
	row := plain(Render(s))
	if len([]rune(row)) != 80 {
		t.Errorf("row is %d cells wide, want 80", len([]rune(row)))
	}

	// A row whose segments do not fit still keeps one cell between
	// them.
	s = base()
	s.Model = strings.Repeat("M", 300)
	row = plain(Render(s))
	if !strings.Contains(row, " ") {
		t.Errorf("row = %q, want a padding cell", row)
	}
}

// TestLeftHint_Rules — the reference's rules for the hint.
func TestLeftHint_Rules(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*State)
		want string
	}{
		{"idle", func(*State) {}, "? for shortcuts"},
		{"picker", func(s *State) { s.PickerVisible = true }, "esc to cancel"},
		{"model mode", func(s *State) {
			s.PickerVisible = true
			s.PickerModelMode = true
		}, ""},
		{"streaming", func(s *State) { s.Streaming = true }, "esc to cancel"},
		{"queued", func(s *State) { s.Queued = 1 }, "  Press up to edit queued messages"},
		{"buffer", func(s *State) { s.BufferEmpty = false }, ""},
		{"resume picker", func(s *State) { s.ResumePickerActive = true }, ""},
		{"bash mode", func(s *State) { s.BashMode = true }, " activated bash mode · esc to cancel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.mut(&s)
			got := leftHint(s)
			if got != tc.want {
				t.Errorf("leftHint = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestLeftHint_PickerWins — the picker keeps its hint even while the
// turn streams and the buffer is empty.
func TestLeftHint_PickerWins(t *testing.T) {
	s := base()
	s.PickerVisible = true
	s.Streaming = true
	s.Queued = 2
	if got := leftHint(s); got != "esc to cancel" {
		t.Errorf("leftHint = %q, want the picker's cancel hint", got)
	}
}

// TestLeftHint_Precedence — the rules are a precedence chain, and the order
// in hintRules is that precedence.
//
// Each case turns on two rules at once and asserts which one wins. The old
// form encoded this in the order of its statements, so a rule inserted in the
// wrong place silently changed the answer; here the slice is the order, and
// this test is what pins it.
func TestLeftHint_Precedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		mut  func(*State)
		want string
		// winner is the rule that must claim the segment.
		winner string
	}{
		{
			name:   "bash mode beats the buffer",
			mut:    func(s *State) { s.BashMode = true; s.BufferEmpty = false },
			want:   " activated bash mode · esc to cancel",
			winner: "bash-mode",
		},
		{
			name:   "bash mode beats queued",
			mut:    func(s *State) { s.BashMode = true; s.Queued = 2 },
			want:   " activated bash mode · esc to cancel",
			winner: "bash-mode",
		},
		{
			name:   "the buffer beats queued",
			mut:    func(s *State) { s.BufferEmpty = false; s.Queued = 2 },
			want:   "",
			winner: "composer-has-text",
		},
		{
			name:   "the buffer beats streaming",
			mut:    func(s *State) { s.BufferEmpty = false; s.Streaming = true },
			want:   "",
			winner: "composer-has-text",
		},
		{
			name:   "queued beats streaming",
			mut:    func(s *State) { s.Queued = 2; s.Streaming = true },
			want:   "  Press up to edit queued messages",
			winner: "queued-prompts",
		},
		{
			// Both rules say the same thing, so the text alone cannot
			// tell them apart — the winner can. The picker owns the
			// segment even while the turn streams, which is why its
			// empty /model answer is not overridden by streaming either.
			name:   "the picker beats streaming",
			mut:    func(s *State) { s.Streaming = true; s.PickerVisible = true },
			want:   "esc to cancel",
			winner: "picker",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base()
			tc.mut(&s)
			if got := leftHint(s); got != tc.want {
				t.Errorf("leftHint = %q, want %q", got, tc.want)
			}
			if got := winningRule(s); got != tc.winner {
				t.Errorf("rule %q claimed the segment, want %q", got, tc.winner)
			}
		})
	}
}

// TestLeftHint_MatchesTheOldCascade — the rule table answers exactly what the
// assignment cascade it replaced answered, over every reachable combination.
//
// The refactor changed the mechanism (last match wins -> first match wins) and
// therefore reversed the precedence list. That is easy to get subtly wrong —
// the first attempt at this table did, letting a broad "picker" rule answer
// before the /model popup's empty one. Eight booleans is small enough to
// enumerate exhaustively, so this does, rather than sampling.
func TestLeftHint_MatchesTheOldCascade(t *testing.T) {
	for mask := 0; mask < 1<<8; mask++ {
		bit := func(i int) bool { return mask&(1<<i) != 0 }
		s := base()
		s.PickerVisible = bit(0)
		s.PickerModelMode = bit(1)
		s.Streaming = bit(2)
		s.BufferEmpty = bit(3)
		s.ResumePickerActive = bit(4)
		s.BashMode = bit(5)
		s.HelpActive = bit(6)
		if bit(7) {
			s.Queued = 2
		}
		if got, want := leftHint(s), hintCascade(s); got != want {
			t.Errorf("mask %d: leftHint = %q, the cascade said %q (state: picker=%v model=%v streaming=%v empty=%v resume=%v bash=%v help=%v queued=%d)",
				mask, got, want,
				s.PickerVisible, s.PickerModelMode, s.Streaming, s.BufferEmpty,
				s.ResumePickerActive, s.BashMode, s.HelpActive, s.Queued)
		}
	}
}

// hintCascade is the assignment cascade the rule table replaced, kept here as
// the equivalence oracle.
//
// It is the original implementation verbatim, and it stays because the
// refactor's whole claim is "same answers". A future rule-table change that
// alters an answer has to either keep this oracle agreeing or say why not.
func hintCascade(s State) string {
	leftPlain := hintDefault

	if s.PickerVisible {
		leftPlain = "esc to cancel"
		if s.PickerModelMode {
			leftPlain = ""
		}
	}
	if s.Streaming && !s.PickerVisible && leftPlain != "" {
		leftPlain = "esc to cancel"
	}
	if s.Queued > 0 && !s.PickerVisible {
		leftPlain = "  Press up to edit queued messages"
	}
	if (!s.BufferEmpty || s.ResumePickerActive) && !s.PickerVisible {
		leftPlain = ""
	}
	if s.BashMode && !s.PickerVisible && !s.HelpActive {
		leftPlain = " activated bash mode · esc to cancel"
	}
	return leftPlain
}

// winningRule returns the name of the rule that claims the segment, or "" for
// the default.
func winningRule(s State) string {
	for _, r := range hintRules {
		if r.when(s) {
			return r.name
		}
	}
	return ""
}

// TestHintRules_AreWellFormed — every rule is complete and the names are
// unique.
//
// The rule name is what a failure reports, so a missing name or a duplicate
// would make two different bugs read the same.
func TestHintRules_AreWellFormed(t *testing.T) {
	seen := map[string]int{}
	for i, r := range hintRules {
		if r.name == "" {
			t.Errorf("rule %d has no name", i)
		}
		if prev, dup := seen[r.name]; dup {
			t.Errorf("rule %d repeats the name %q from rule %d", i, r.name, prev)
		}
		seen[r.name] = i
		if r.when == nil {
			t.Errorf("rule %q has no condition, so it would match everything", r.name)
		}
		if r.text == nil {
			t.Errorf("rule %q has no text, so it would panic when it matches", r.name)
		}
	}
	// The default must not be a rule: a rule would claim the segment and
	// the chain's fallback would become dead.
	for _, r := range hintRules {
		if r.when(base()) {
			t.Errorf("rule %q matches the idle state, so hintDefault is unreachable", r.name)
		}
	}
}

// TestLeftSegment_Badge — the vim badge sits in front of the hint.
func TestLeftSegment_Badge(t *testing.T) {
	s := base()
	s.VimBadge = "-- INSERT --"
	left, leftPlain := leftSegment(s)
	if !strings.HasPrefix(leftPlain, "-- INSERT -- ? for shortcuts") {
		t.Errorf("leftPlain = %q, want the badge in front of the hint", leftPlain)
	}
	if !strings.Contains(left, "-- INSERT --") {
		t.Errorf("left = %q, want the badge", left)
	}

	// A badge alone (no hint) is the whole segment.
	s = base()
	s.BufferEmpty = false
	s.VimBadge = "-- V-LINE --"
	_, leftPlain = leftSegment(s)
	if leftPlain != "-- V-LINE --" {
		t.Errorf("leftPlain = %q, want the badge alone", leftPlain)
	}
}

// TestLeftSegment_Cleared — the /help overlay and a modal panel clear
// the segment, unless the panel keeps the hint.
func TestLeftSegment_Cleared(t *testing.T) {
	s := base()
	s.HelpActive = true
	if _, leftPlain := leftSegment(s); leftPlain != "" {
		t.Errorf("leftPlain = %q, want the help overlay to clear the segment", leftPlain)
	}

	s = base()
	s.PanelOpen = true
	if _, leftPlain := leftSegment(s); leftPlain != "" {
		t.Errorf("leftPlain = %q, want a modal panel to clear the segment", leftPlain)
	}

	// The /codesearch panel keeps the hint.
	s = base()
	s.PanelOpen = true
	s.PanelKeepsHint = true
	if _, leftPlain := leftSegment(s); leftPlain != "? for shortcuts" {
		t.Errorf("leftPlain = %q, want the panel to keep the hint", leftPlain)
	}
}

// TestLeftSegment_ExitConfirm — the two-step Ctrl+C confirmation
// replaces the segment.
func TestLeftSegment_ExitConfirm(t *testing.T) {
	s := base()
	s.ExitConfirm = true
	_, leftPlain := leftSegment(s)
	if leftPlain != "press ctrl+c again to exit" {
		t.Errorf("leftPlain = %q, want the exit confirmation", leftPlain)
	}
}

// TestLeftSegment_Compact — the compaction label replaces the segment.
func TestLeftSegment_Compact(t *testing.T) {
	start := time.Now().Add(-12 * time.Second)
	s := base()
	s.Compact = &Compact{Start: start}
	_, leftPlain := leftSegment(s)
	if !strings.Contains(leftPlain, "Compacting context…") {
		t.Errorf("leftPlain = %q, want the compaction label", leftPlain)
	}

	s.Compact = &Compact{Start: start, End: start.Add(time.Second), TokensBefore: 184000, TokensAfter: 1200000}
	_, leftPlain = leftSegment(s)
	if !strings.HasPrefix(leftPlain, "Compacted 184 K → 1.2 M (1s)") {
		t.Errorf("leftPlain = %q, want the finished compaction label", leftPlain)
	}
}

func TestLeftSegment_CompactBufferNonEmpty(t *testing.T) {
	start := time.Now().Add(-12 * time.Second)
	s := base()
	s.BufferEmpty = false

	// Finished compaction yields when buffer is not empty (user is typing)
	s.Compact = &Compact{Start: start, End: start.Add(time.Second), TokensBefore: 184000, TokensAfter: 1200000}
	_, leftPlain := leftSegment(s)
	if leftPlain != "" {
		t.Errorf("leftPlain = %q, want empty when user is typing in buffer", leftPlain)
	}

	// But running compaction still shows even if buffer has text
	s.Compact = &Compact{Start: start}
	_, leftPlain = leftSegment(s)
	if !strings.Contains(leftPlain, "Compacting context…") {
		t.Errorf("leftPlain = %q, want running compaction label even if buffer has text", leftPlain)
	}
}

// TestCompactLabel — the three shapes of the compaction label.
func TestCompactLabel(t *testing.T) {
	start := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC)

	got := compactLabel(Compact{Start: start})
	if !strings.Contains(got, "Compacting context…") {
		t.Errorf("label = %q, want the running form", got)
	}
	if strings.Contains(got, "Compacted") {
		t.Errorf("label = %q, want no finished form while it runs", got)
	}

	got = compactLabel(Compact{Start: start, TokensBefore: 184000, TokensAfter: 1200})
	if !strings.Contains(got, "184 K → 1.2 K") {
		t.Errorf("label = %q, want the token figures", got)
	}

	got = compactLabel(Compact{Start: start, End: start.Add(3 * time.Second)})
	if got != "Compacted - → - (3s)" {
		t.Errorf("label = %q, want the finished form", got)
	}
}

// TestRightSegment_Model — the model, the mode and the suffixes.
func TestRightSegment_Model(t *testing.T) {
	s := base()
	s.StatusModel = "Gemini 3.8 Flash"
	s.EffortSuffix = " · high"
	s.TaskSuffix = " · 2 tasks"
	right := rightSegment(s)
	if !strings.Contains(plain(right.styled), "Gemini 3.8 Flash · high · 2 tasks") {
		t.Errorf("right = %q, want the model with its suffixes", plain(right.styled))
	}

	// The status model's name wins over the model's.
	s = base()
	s.Model = "m"
	s.StatusModel = "Gemini 3.8 Flash"
	if !strings.Contains(plain(rightSegment(s).styled), "Gemini 3.8 Flash") {
		t.Errorf("right = %q, want the status model's name", plain(rightSegment(s).styled))
	}

	// With no status model the model's own name is used.
	s = base()
	s.Model = "MiniMax M3"
	s.StatusModel = ""
	if !strings.Contains(plain(rightSegment(s).styled), "MiniMax M3") {
		t.Errorf("right = %q, want the model's name", plain(rightSegment(s).styled))
	}
}

// TestRightSegment_Mode — a mode that carries a prefix is painted in the
// mode's colour and counted on its own for the padding.
//
// The cycle and which modes carry a prefix are pinned live by tuialign
// scene 49_mode_cycle_order; this test covers the drawing given a mode.
func TestRightSegment_Mode(t *testing.T) {
	s := base()
	s.Mode = "plan"
	right := rightSegment(s)
	if right.modePlain != "plan · " {
		t.Errorf("modePlain = %q, want %q", right.modePlain, "plan · ")
	}
	if !strings.Contains(plain(right.styled), "plan · Gemini 3.8 Flash") {
		t.Errorf("right = %q, want the mode in front of the model", plain(right.styled))
	}

	// The mode word takes the mode's colour; the separator after it goes
	// back to the dim foreground.
	if !strings.Contains(right.styled, palette.StatusModeColor("plan")+"plan"+palette.StatusModeClose("plan")+" · ") {
		t.Errorf("right = %q, want the mode in its own colour and the separator dim", right.styled)
	}
}

// TestRightSegment_AutoHasPrefix — auto names itself on the status row.
//
// This diverges from agy 1.5.18, which paints nothing for auto. A cycle stop
// that renders identically to a dropped row is unusable: plan -> auto ->
// accept-edits has no way to tell the middle stop landed. auto takes the dim
// foreground so it reads as the baseline rather than competing with plan's
// accent and accept-edits' green.
func TestRightSegment_AutoHasPrefix(t *testing.T) {
	s := base()
	s.Mode = "auto"
	right := rightSegment(s)
	if right.modePlain != "auto · " {
		t.Errorf("modePlain = %q, want %q", right.modePlain, "auto · ")
	}
	if plain(right.styled) != "auto · Gemini 3.8 Flash" {
		t.Errorf("right = %q, want the mode in front of the model", plain(right.styled))
	}
	if !strings.HasPrefix(right.styled, palette.DimGrayBoldOpen) {
		t.Errorf("right = %q, want auto painted with the dim foreground", right.styled)
	}
}

// TestRender_ModelStylingIndependentOfMode — changing the mode must not
// restyle the model.
//
// The bug this pins: `auto` paints its word with the dim foreground *and*
// bold, and the mode's close was a colour-only escape (DimGrayOpen), which
// leaves the intensity alone. Under auto the bold therefore ran on through
// the " · " and the model, so the model read bold in auto and plain in plan
// and accept-edits — switching mode appeared to change the model's font
// weight, though the model never moved. The row also ended with the terminal
// still bold, which bled into whatever was drawn after it.
//
// The assertion walks the SGR escapes and compares the *state* in effect at
// the model's first character, which is the only place the difference shows.
func TestRender_ModelStylingIndependentOfMode(t *testing.T) {
	model := "Gemini 3.8 Flash"
	states := map[string]string{}
	for _, mode := range []string{"", "auto", "plan", "accept-edits"} {
		s := base()
		s.Mode = mode
		states[mode] = sgrStateAt(Render(s), model)
	}

	want := states[""]
	for _, mode := range []string{"auto", "plan", "accept-edits"} {
		if states[mode] != want {
			t.Errorf("mode %q renders the model as [%s], but no mode renders it as [%s]; switching mode must not restyle the model",
				mode, states[mode], want)
		}
	}
}

// TestRender_RowEndsWithoutAttributes — the row must not leave an SGR
// attribute on for whatever the terminal draws next.
//
// The status row is the last thing on the screen, so a bold or a colour it
// leaves open bleeds into the shell prompt after exit.
func TestRender_RowEndsWithoutAttributes(t *testing.T) {
	for _, mode := range []string{"", "auto", "plan", "accept-edits"} {
		s := base()
		s.Mode = mode
		if got := sgrStateAtEnd(Render(s)); got != "" {
			t.Errorf("mode %q leaves [%s] set at the end of the row, want nothing", mode, got)
		}
	}
}

// sgrStateAt returns the SGR attributes in effect at the first rune of
// needle, as a canonical string.
func sgrStateAt(s, needle string) string {
	idx := strings.Index(s, needle)
	if idx < 0 {
		return "<needle absent>"
	}
	return sgrState(s[:idx])
}

// sgrStateAtEnd returns the SGR attributes in effect after the last rune.
func sgrStateAtEnd(s string) string { return sgrState(s) }

// sgrState replays the SGR escapes in s and returns the attributes left set.
//
// It understands the codes the status row emits: bold, faint, italic,
// underline, the resets 0 / 22 / 23 / 24, the default foreground 39 and the
// two foreground forms (38;2;r;g;b and 38;5;n).
func sgrState(s string) string {
	var (
		fg                  string
		bold, faint, italic bool
		under               bool
	)
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b || i+1 >= len(s) || s[i+1] != '[' {
			continue
		}
		j := i + 2
		start := j
		for j < len(s) && s[j] != 'm' {
			j++
		}
		if j >= len(s) {
			break
		}
		params := strings.Split(s[start:j], ";")
		i = j
		for k := 0; k < len(params); k++ {
			n, err := strconv.Atoi(params[k])
			if err != nil {
				continue
			}
			switch {
			case n == 0:
				fg, bold, faint, italic, under = "", false, false, false, false
			case n == 1:
				bold = true
			case n == 2:
				faint = true
			case n == 3:
				italic = true
			case n == 4:
				under = true
			case n == 22:
				bold, faint = false, false
			case n == 23:
				italic = false
			case n == 24:
				under = false
			case n == 39:
				fg = ""
			case n == 38 && k+1 < len(params) && params[k+1] == "2" && k+4 < len(params):
				fg = "rgb(" + strings.Join(params[k+2:k+5], ",") + ")"
				k += 4
			case n == 38 && k+1 < len(params) && params[k+1] == "5" && k+2 < len(params):
				fg = "ansi(" + params[k+2] + ")"
				k += 2
			}
		}
	}
	var parts []string
	if fg != "" {
		parts = append(parts, "fg="+fg)
	}
	for _, a := range []struct {
		name string
		on   bool
	}{{"bold", bold}, {"faint", faint}, {"italic", italic}, {"underline", under}} {
		if a.on {
			parts = append(parts, a.name)
		}
	}
	return strings.Join(parts, " ")
}

// TestRightSegment_Agent — the --agent's name wraps the model.
func TestRightSegment_Agent(t *testing.T) {
	s := base()
	s.AgentName = "reviewer"
	s.EffortSuffix = " · high"
	right := rightSegment(s)
	if !strings.Contains(plain(right.styled), "reviewer (Gemini 3.8 Flash · high)  /agents") {
		t.Errorf("right = %q, want the agent's name around the model", plain(right.styled))
	}
	// The padding counts the agent part, not the model.
	if right.rightPlainLen != len([]rune("reviewer (Gemini 3.8 Flash · high)  /agents")) {
		t.Errorf("rightPlainLen = %d, want the agent part's length", right.rightPlainLen)
	}
}

// TestRightSegment_QueueMode — the queue-mode marker sits after the
// model.
func TestRightSegment_QueueMode(t *testing.T) {
	s := base()
	s.QueueMode = true
	right := rightSegment(s)
	if !strings.Contains(plain(right.styled), "Gemini 3.8 Flash · Queue mode") {
		t.Errorf("right = %q, want the queue-mode marker", plain(right.styled))
	}
}

// TestRightSegment_StatusCells — the agent state, the context usage and
// the credit balance are the row's status cells.
//
// Each cell is omitted when its fact is absent: a cell without a fact
// would be a guess.
func TestRightSegment_StatusCells(t *testing.T) {
	s := base()
	s.AgentState = "working"
	s.ContextUsedPercent = 12.3
	s.Credits = 42
	s.CreditsKnown = true
	right := plain(rightSegment(s).styled)
	if !strings.HasSuffix(right, "Gemini 3.8 Flash · working · 12.3% · AI Credits: 42") {
		t.Errorf("right = %q, want the three status cells", right)
	}

	// No facts: no cells.
	s = base()
	right = plain(rightSegment(s).styled)
	if right != "Gemini 3.8 Flash" {
		t.Errorf("right = %q, want no status cells", right)
	}

	// An unknown credit balance is not a zero balance.
	s = base()
	s.Credits = 0
	s.CreditsKnown = false
	if strings.Contains(plain(rightSegment(s).styled), "AI Credits") {
		t.Errorf("right = %q, want no credits cell while the balance is unknown",
			plain(rightSegment(s).styled))
	}
}

// TestRightSegment_StatusCellsPadding — the padding counts the status
// cells.
//
// The built-in case keeps the reference's asymmetry: the queue mode's
// suffix is not counted, and the cells are.
func TestRightSegment_StatusCellsPadding(t *testing.T) {
	s := base()
	s.AgentState = "working"
	right := rightSegment(s)
	if right.rightPlainLen != len([]rune("Gemini 3.8 Flash · working")) {
		t.Errorf("rightPlainLen = %d, want the model's and the cells' length",
			right.rightPlainLen)
	}

	// The --agent case counts the whole agent part, the cells included.
	s = base()
	s.AgentName = "reviewer"
	s.AgentState = "working"
	right = rightSegment(s)
	if right.rightPlainLen != len([]rune("reviewer (Gemini 3.8 Flash)  /agents · working")) {
		t.Errorf("rightPlainLen = %d, want the agent part's length", right.rightPlainLen)
	}
}

// TestStatic — the non-tty row.
//
// The static row carries the literal " · high" and the reference's own
// grey — a value the palette does not carry.
func TestStatic(t *testing.T) {
	row := Static("Gemini 3.8 Flash", 220)
	if !strings.HasPrefix(row, "\033[38;2;154;160;166m? for shortcuts\033[39m") {
		t.Errorf("row = %q, want the static grey hint", row)
	}
	if !strings.Contains(row, "Gemini 3.8 Flash · high") {
		t.Errorf("row = %q, want the model and the effort label", row)
	}
	if len([]rune(plain(row))) != 220 {
		t.Errorf("row is %d cells wide, want 220", len([]rune(plain(row))))
	}
}

func TestStatusRow_OutOfCredits(t *testing.T) {
	s := base()
	s.OutOfCredits = true
	s.CreditsKnown = true
	s.Credits = 0
	s.IsGeminiAPIKey = false
	rendered := Render(s)
	// AGY 1.5.14: The status line shows AI credits only when you have a positive balance.
	if strings.Contains(rendered, "AI: Out of credits") {
		t.Errorf("rendered unexpectedly contains AI: Out of credits: %q", rendered)
	}

	// With positive credits:
	s.Credits = 42
	renderedPositive := Render(s)
	if !strings.Contains(renderedPositive, "AI Credits: 42") {
		t.Errorf("renderedPositive missing AI Credits: 42: %q", renderedPositive)
	}

	// Under GEMINI_API_KEY, AI credits must NOT appear (1.5.12).
	s.IsGeminiAPIKey = true
	renderedAPIKey := Render(s)
	if strings.Contains(renderedAPIKey, "AI Credits") {
		t.Errorf("rendered under GEMINI_API_KEY unexpectedly contains AI Credits: %q", renderedAPIKey)
	}
}
