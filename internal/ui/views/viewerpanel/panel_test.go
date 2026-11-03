package viewerpanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/foundations/viewer"
)

// lines builds a document of n one-word lines.
func lines(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "line"
	}
	return out
}

// harness is a panel with a document and a counter of the closes.
type harness struct {
	panel  *Panel
	closed int
	doc    []string
	vis    int
}

// newHarness builds a panel over a document of n lines with the given
// visible row count.
func newHarness(n, visible int) *harness {
	h := &harness{doc: lines(n), vis: visible}
	h.panel = New(Seams{
		Spec: func() (viewer.Spec, bool) {
			return viewer.Spec{Lines: h.doc, Visible: h.vis, Total: len(h.doc)}, true
		},
		Width: func() int { return 80 },
		Close: func() tea.Cmd {
			h.closed++
			return nil
		},
	})
	h.panel.Open()
	return h
}

// key presses one key and returns the panel's answer.
func (h *harness) key(t *testing.T, k tea.Key, offset int) (bool, tea.Cmd, int) {
	t.Helper()
	return h.panel.HandleKey(k, offset)
}

// The line keys scroll one line at a time.
func TestPanel_ScrollsOneLine(t *testing.T) {
	h := newHarness(20, 5)

	handled, _, offset := h.key(t, tea.Key{Code: 'j', Text: "j"}, 0)
	if !handled || offset != 1 {
		t.Errorf("after 'j': handled=%v, offset=%d, want 1", handled, offset)
	}

	handled, _, offset = h.key(t, tea.Key{Code: 'k', Text: "k"}, offset)
	if !handled || offset != 0 {
		t.Errorf("after 'k': handled=%v, offset=%d, want 0", handled, offset)
	}
}

// g and G jump to the two ends of the document.
func TestPanel_GotoTopAndBottom(t *testing.T) {
	h := newHarness(20, 5)

	handled, _, offset := h.key(t, tea.Key{Code: 'G', Text: "G"}, 0)
	// The window is five rows of a twenty-row document, so the last
	// window starts at row 15.
	if !handled || offset != 15 {
		t.Errorf("after 'G': handled=%v, offset=%d, want 15", handled, offset)
	}

	handled, _, offset = h.key(t, tea.Key{Code: 'g', Text: "g"}, offset)
	if !handled || offset != 0 {
		t.Errorf("after 'g': handled=%v, offset=%d, want 0", handled, offset)
	}
}

// A page key scrolls a whole window.
func TestPanel_PageDownAndUp(t *testing.T) {
	h := newHarness(20, 5)

	handled, _, offset := h.key(t, tea.Key{Code: tea.KeyPgDown}, 0)
	if !handled || offset != 5 {
		t.Errorf("after pgdown: handled=%v, offset=%d, want 5", handled, offset)
	}

	handled, _, offset = h.key(t, tea.Key{Code: tea.KeyPgUp}, offset)
	if !handled || offset != 0 {
		t.Errorf("after pgup: handled=%v, offset=%d, want 0", handled, offset)
	}
}

// d and u scroll half a window.
func TestPanel_HalfPageDownAndUp(t *testing.T) {
	h := newHarness(20, 5)

	handled, _, offset := h.key(t, tea.Key{Code: 'd', Text: "d"}, 0)
	if !handled || offset != 2 {
		t.Errorf("after 'd': handled=%v, offset=%d, want 2", handled, offset)
	}

	handled, _, offset = h.key(t, tea.Key{Code: 'u', Text: "u"}, offset)
	if !handled || offset != 0 {
		t.Errorf("after 'u': handled=%v, offset=%d, want 0", handled, offset)
	}
}

// q and esc leave the panel.
func TestPanel_ClosesOnCancel(t *testing.T) {
	for _, k := range []tea.Key{{Code: tea.KeyEsc}, {Code: 'q', Text: "q"}} {
		h := newHarness(20, 5)
		handled, _, _ := h.key(t, k, 0)
		if !handled {
			t.Errorf("%v: not handled", k)
		}
		if h.closed != 1 {
			t.Errorf("%v: closed %d times, want 1", k, h.closed)
		}
	}
}

// A plain key is the panel's; a modified one falls through to the host.
func TestPanel_SwallowsPlainKeyAndFallsThroughModified(t *testing.T) {
	h := newHarness(20, 5)

	handled, _, _ := h.key(t, tea.Key{Code: 'x', Text: "x"}, 0)
	if !handled {
		t.Error("a plain key must be swallowed by the panel")
	}

	handled, _, _ = h.key(t, tea.Key{Code: 'x', Text: "x", Mod: tea.ModAlt}, 0)
	if handled {
		t.Error("a modified key must fall through to the host")
	}
}

// The offset is clamped into the document.
func TestPanel_ClampsTheOffset(t *testing.T) {
	h := newHarness(20, 5)

	_, _, offset := h.key(t, tea.Key{Code: 'j', Text: "j"}, 100)
	if offset != 15 {
		t.Errorf("offset = %d, want 15 (the last window of a 20-row document)", offset)
	}
}

// The host names the page size, so the reference's one-row-shorter page
// is the host's to give.
func TestPanel_HonoursThePageRowsSeam(t *testing.T) {
	h := newHarness(20, 5)
	h.panel = New(Seams{
		Spec: func() (viewer.Spec, bool) {
			return viewer.Spec{Lines: h.doc, Visible: h.vis, Total: len(h.doc)}, true
		},
		Width: func() int { return 80 },
		PageRows: func(spec viewer.Spec) int {
			return spec.Visible - 1
		},
	})
	h.panel.Open()

	_, _, offset := h.key(t, tea.Key{Code: tea.KeyPgDown}, 0)
	if offset != 4 {
		t.Errorf("offset = %d, want 4 (a window of five, scrolled by four)", offset)
	}
}

// The document is read again on every key, so a document that changed
// while the panel was up scrolls in its new shape.
func TestPanel_SyncReadsTheDocumentAgain(t *testing.T) {
	h := newHarness(20, 5)

	_, _, offset := h.key(t, tea.Key{Code: 'j', Text: "j"}, 0)
	if offset != 1 {
		t.Fatalf("offset = %d, want 1", offset)
	}

	// The document shrank to three rows: the window of five now shows
	// all of it, so the offset clamps to zero.
	h.doc = lines(3)
	_, _, offset = h.key(t, tea.Key{Code: 'j', Text: "j"}, offset)
	if offset != 0 {
		t.Errorf("offset = %d, want 0 (the document shrank)", offset)
	}
}

// A panel without a width seam falls back to the panel's own number —
// the host names the same one when the terminal's width is unknown.
func TestPanel_FallsBackToItsOwnWidth(t *testing.T) {
	p := New(Seams{})
	if got := p.width(); got != DefaultWidth {
		t.Errorf("width = %d, want %d", got, DefaultWidth)
	}
}

// A panel without a document has nothing to scroll: the viewport is
// empty, so the offset is the viewport's own.
func TestPanel_WithoutADocument(t *testing.T) {
	p := New(Seams{})
	p.Open()

	handled, _, offset := p.HandleKey(tea.Key{Code: 'j', Text: "j"}, 7)
	if !handled {
		t.Error("the navigation key must still be claimed")
	}
	if offset != 0 {
		t.Errorf("offset = %d, want 0 (an empty viewport)", offset)
	}
}
