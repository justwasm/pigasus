// statusrow_test.go — the status row's own tests.
//
// The row is a view: it takes the facts and writes the line. These
// tests drive it directly with a State; the host's wiring (where the
// facts come from) is tested in internal/cliapp.

package statusrow

import (
	"strings"
	"testing"
	"time"
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

// TestRightSegment_Mode — the mode is painted in the mode's colour and
// counted on its own for the padding.
func TestRightSegment_Mode(t *testing.T) {
	s := base()
	s.Mode = "auto"
	right := rightSegment(s)
	if right.modePlain != "auto · " {
		t.Errorf("modePlain = %q, want %q", right.modePlain, "auto · ")
	}
	if !strings.Contains(plain(right.styled), "auto · Gemini 3.8 Flash") {
		t.Errorf("right = %q, want the mode in front of the model", plain(right.styled))
	}
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
