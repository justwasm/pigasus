package helppanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The 1.5.17 changelog line this file pins:
//
//	"Fixed mouse-wheel scrolling over an open bottom panel or /, @, or argument
//	 dropdown - and PgUp, PgDn, Shift+↑, or Shift+↓ in panels that page
//	 themselves (such as /help, /permissions, /skills, /context, and /usage) -
//	 scrolling the conversation behind the panel instead of the panel itself."
//
// The bug was routing, so two things have to hold: the panel must claim the
// gesture (WantsScroll), and its own Update must move the window when the key
// arrives. The tests below cover the second half; the first is what lets the
// host stop the gesture reaching the conversation.

// TestPageKeysMoveTheWindowOnePage is the core assertion: PgDn moves the
// window by exactly one window, and PgUp brings it back.
func TestPageKeysMoveTheWindowOnePage(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(1)

	const total = 99
	size := p.listWindowSize()
	if size <= 0 {
		t.Fatalf("window size = %d, want a positive page", size)
	}
	if p.window != 0 {
		t.Fatalf("window starts at %d, want 0", p.window)
	}

	if handled, _ := p.Update(tea.Key{Code: tea.KeyPgDown}); !handled {
		t.Fatal("PgDn was not handled by the panel")
	}
	if p.window != size || p.cursor != size {
		t.Errorf("after PgDn: cursor=%d window=%d, want %d/%d", p.cursor, p.window, size, size)
	}

	if handled, _ := p.Update(tea.Key{Code: tea.KeyPgUp}); !handled {
		t.Fatal("PgUp was not handled by the panel")
	}
	if p.window != 0 || p.cursor != 0 {
		t.Errorf("after PgUp: cursor=%d window=%d, want 0/0", p.cursor, p.window)
	}
}

// TestShiftArrowsPage pins that the shifted arrows are page keys here.
//
// ClassifyListNavKey reads Shift+↑ as a plain one-row ↑, so without the
// explicit page-key check before it these would have moved the cursor one row
// instead of paging - the same gesture doing the wrong thing quietly.
func TestShiftArrowsPage(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(1)

	size := p.listWindowSize()

	if handled, _ := p.Update(tea.Key{Code: tea.KeyDown, Mod: tea.ModShift}); !handled {
		t.Fatal("Shift+↓ was not handled")
	}
	if p.window != size {
		t.Errorf("after Shift+↓: window=%d, want one page (%d)", p.window, size)
	}

	if handled, _ := p.Update(tea.Key{Code: tea.KeyUp, Mod: tea.ModShift}); !handled {
		t.Fatal("Shift+↑ was not handled")
	}
	if p.window != 0 {
		t.Errorf("after Shift+↑: window=%d, want 0", p.window)
	}
}

// TestUnshiftedArrowStillMovesOneRow is the negative control.
//
// If the page-key check swallowed every arrow, a plain ↓ would page instead of
// moving one row and the list would feel broken. This keeps the two apart.
func TestUnshiftedArrowStillMovesOneRow(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(1)

	before := p.cursor
	if handled, _ := p.Update(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("↓ was not handled")
	}
	if p.cursor != before+1 {
		t.Errorf("plain ↓ moved cursor %d -> %d, want one row", before, p.cursor)
	}
	if p.window != 0 {
		t.Errorf("plain ↓ scrolled the window to %d, want 0", p.window)
	}
}

// TestPageDownClampsAtTheEnd pins the clamp: paging past the last row parks on
// it rather than running off the end.
func TestPageDownClampsAtTheEnd(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(1)

	const total = 99
	for i := 0; i < total; i++ {
		p.pageDown(total)
	}
	if p.cursor != total-1 {
		t.Errorf("cursor after paging past the end = %d, want %d", p.cursor, total-1)
	}
	size := p.listWindowSize()
	if want := total - size; p.window != want {
		t.Errorf("window after paging past the end = %d, want %d", p.window, want)
	}
}

// TestPageUpClampsAtTheTop is the mirror: paging up from the top stays put.
func TestPageUpClampsAtTheTop(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(1)

	for i := 0; i < 5; i++ {
		p.pageUp(99)
	}
	if p.cursor != 0 || p.window != 0 {
		t.Errorf("after paging up from the top: cursor=%d window=%d, want 0/0", p.cursor, p.window)
	}
}

// TestPageKeysHandledOnlyWhileOpen pins the other half of the contract: a
// closed overlay must not swallow the keys, or the conversation would stop
// responding to PgDn once /help had been opened and dismissed.
func TestPageKeysHandledOnlyWhileOpen(t *testing.T) {
	f := newFixture(false)
	p := f.panel

	if handled, _ := p.Update(tea.Key{Code: tea.KeyPgDown}); handled {
		t.Error("closed overlay handled PgDn")
	}

	p.Open(1)
	if handled, _ := p.Update(tea.Key{Code: tea.KeyPgDown}); !handled {
		t.Error("open overlay did not handle PgDn")
	}
}

// TestPageRowsIsOneWindow pins that a page moves a window, not one row and not
// an arbitrary count: the reference pages by the rows it shows.
func TestPageRowsIsOneWindow(t *testing.T) {
	f := newFixture(false)
	p := f.panel
	p.Open(1)

	if got, want := p.PageRows(), p.listWindowSize(); got != want {
		t.Errorf("PageRows = %d, want one window (%d)", got, want)
	}
}
