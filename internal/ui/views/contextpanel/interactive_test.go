// interactive_test.go — the interactive /context panel.
//
// The panel is a function of the seams it is given, so every test here
// builds one with hand-made facts: no host, no widget tree, no session.
// What is pinned is the panel's own behaviour — the single view the
// reference draws, the scroll bound that makes its tail reachable, and
// what a key returns.

package contextpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// stubSeams is a panel wired to fixed facts, recording the side effects
// so a test can see what the panel asked the host to do.
type stubSeams struct {
	state  State
	height int
	width  int

	closed   bool
	closeCmd func() tea.Cmd
}

func (s *stubSeams) seams() Seams {
	return Seams{
		State:  func() State { return s.state },
		Height: func() int { return s.height },
		Width:  func() int { return s.width },
		Close: func() tea.Cmd {
			s.closed = true
			if s.closeCmd != nil {
				return s.closeCmd()
			}
			return nil
		},
	}
}

// newStubPanel is a panel of the fixture's facts, sized to a 40-row
// terminal so the window is wide enough to show a document.
func newStubPanel() (*Panel, *stubSeams) {
	s := &stubSeams{
		state:  State{Model: "Gemini 3.8 Flash (High)", ContextMax: 1_000_000, Mode: ModeLive},
		height: 40,
		width:  120,
	}
	return New(s.seams()), s
}

// TestPanelRendersTheReferenceView — the panel has one view, and it is
// the reference's: the title first, with no tab bar above it.
func TestPanelRendersTheReferenceView(t *testing.T) {
	p, _ := newStubPanel()
	lines := plainLines(p.Lines())
	if len(lines) == 0 {
		t.Fatal("the panel rendered nothing")
	}
	if lines[0] != "└ Context Usage" {
		t.Errorf("row 0 = %q, want the reference's title with no bar above it", lines[0])
	}
	// The bar's own hint is what a tabbed panel prints on row 0; finding
	// it here would mean the panel grew a second view again.
	if strings.Contains(lines[0], "or tab to cycle") {
		t.Errorf("row 0 is a tab bar: %q", lines[0])
	}
}

// TestPanelRendersTheSameRowsAsThePackage — the interactive panel adds
// nothing to the package's rows: /context is byte-for-byte the reference
// layout, which is what the move of the two extra views to /logs was for.
func TestPanelRendersTheSameRowsAsThePackage(t *testing.T) {
	p, s := newStubPanel()
	got := plainLines(p.Lines())
	// The package's own render, given the width and the window the panel
	// resolved: the comparison is about the rows the panel adds, not about
	// a different grid width.
	wantState := s.state
	wantState.Width = s.width
	wantState.MaxRows = p.contextMaxRows()
	want := plainLines(Lines(wantState))
	if len(got) != len(want) {
		t.Fatalf("the panel rendered %d rows, the package %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, the package's is %q", i, got[i], want[i])
		}
	}
}

// TestPanelContextViewScrollsToItsTail — the last body row is reachable:
// the bound and the render agree.
func TestPanelContextViewScrollsToItsTail(t *testing.T) {
	p, s := newStubPanel()
	// A document longer than the window: a session with checkpoints and
	// file sections is the shape that used to stop short of its tail.
	s.state.TotalCheckpoints = 3
	s.state.ActiveCheckpointIndex = 2
	s.state.FileSections = []FileSection{{Title: "a"}, {Title: "b"}, {Title: "c"}}

	max := p.contextMaxScroll()
	if max <= 0 {
		t.Skip("the fixture's document fits its window; nothing to scroll")
	}
	for i := 0; i < max+5; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	joined := strings.Join(plainLines(p.Lines()), "\n")
	if !strings.Contains(joined, "Related: /artifact") {
		t.Errorf("the tail is unreachable at the bottom:\n%s", joined)
	}
}

// TestPanelScrollBoundIsTheRendersOwn — the up key is live at the tail: an
// offset pushed past the window is the bug this bound exists to prevent.
func TestPanelScrollBoundIsTheRendersOwn(t *testing.T) {
	p, s := newStubPanel()
	s.state.TotalCheckpoints = 3
	s.state.FileSections = []FileSection{{Title: "a"}, {Title: "b"}, {Title: "c"}}

	max := p.contextMaxScroll()
	if max <= 0 {
		t.Skip("the fixture's document fits its window; nothing to scroll")
	}
	// Walking down far past the end then one step up must move: a bound
	// that disagreed with the render would leave the offset stranded.
	for i := 0; i < max+20; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	atBottom := p.scroll
	p.HandleKey(tea.Key{Code: tea.KeyUp})
	if p.scroll != atBottom-1 {
		t.Errorf("up from the bottom moved %d -> %d, want one row", atBottom, p.scroll)
	}
}

// TestPanelEscCloses — Esc leaves the panel.
func TestPanelEscCloses(t *testing.T) {
	p, s := newStubPanel()
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("Esc was not handled")
	}
	if !s.closed {
		t.Error("Esc did not close the panel")
	}
}

// TestPanelEmptySeamsDegrade — a panel built without seams renders the
// empty frame rather than panicking. The package is usable from a test
// with no host.
func TestPanelEmptySeamsDegrade(t *testing.T) {
	p := New(Seams{})
	lines := p.Lines()
	if len(lines) == 0 {
		t.Fatal("a panel with no seams rendered nothing")
	}
	if !strings.Contains(strings.Join(plainLines(lines), "\n"), "Context Usage") {
		t.Errorf("the empty panel does not render the usage frame:\n%s",
			strings.Join(plainLines(lines), "\n"))
	}
}

// TestPanelRowCountIsTheDocumentsLength — the router's cursor bound is the
// document's own count.
func TestPanelRowCountIsTheDocumentsLength(t *testing.T) {
	p, _ := newStubPanel()
	if got, want := p.RowCount(), len(AllLines(p.viewState())); got != want {
		t.Errorf("RowCount = %d, want the document's %d", got, want)
	}
}
