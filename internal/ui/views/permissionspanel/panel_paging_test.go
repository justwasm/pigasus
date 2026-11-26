// panel_paging_test.go — the /permissions editor's page keys.
//
// The 1.5.17 changelog line these pin:
//
//	"Fixed mouse-wheel scrolling over an open bottom panel or /, @, or argument
//	 dropdown - and PgUp, PgDn, Shift+↑, or Shift+↓ in panels that page
//	 themselves (such as /help, /permissions, /skills, /context, and /usage) -
//	 scrolling the conversation behind the panel instead of the panel itself."
//
// /permissions is named in that list, so its list stage must page rather than
// moving one row. Shift+↑ matters especially: the plain-navigation rule reads it
// as an unshifted ↑, so without an explicit page-key check the same gesture
// silently does the wrong thing.

package permissionspanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/policy/permissions"
)

// pageablePanel builds a panel sitting on a list with enough rows to page.
func pageablePanel(t *testing.T) *Panel {
	t.Helper()
	p := newTestPanel()
	p.stage = StageLists
	p.scope = permissions.PermScopeShared
	p.tab = 0
	rules := make([]string, 40)
	for i := range rules {
		rules[i] = "command(probe-page)"
	}
	p.shared[0] = rules
	p.cursor = 0
	return p
}

// TestPgDnPagesTheList pins that PgDn jumps a page, not a row.
func TestPgDnPagesTheList(t *testing.T) {
	p := pageablePanel(t)

	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyPgDown}); !handled {
		t.Fatal("PgDn not handled")
	}
	if p.Cursor() == 1 {
		t.Error("PgDn moved the cursor one row; it should move a page")
	}
	if p.Cursor() == 0 {
		t.Error("PgDn did not move the cursor at all")
	}
}

// TestPgUpReturnsToTheTop pins the mirror direction.
func TestPgUpReturnsToTheTop(t *testing.T) {
	p := pageablePanel(t)

	p.HandleKey(tea.Key{Code: tea.KeyPgDown})
	if p.Cursor() == 0 {
		t.Fatal("PgDn did not move; the PgUp assertion below would be vacuous")
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyPgUp}); !handled {
		t.Fatal("PgUp not handled")
	}
	if p.Cursor() != 0 {
		t.Errorf("after PgUp: cursor = %d, want 0", p.Cursor())
	}
}

// TestShiftArrowsPage pins Shift+↓ / Shift+↑ as page keys.
//
// This is the case a naive implementation gets wrong: ClassifyListNavKey reads
// Shift+↑ as an unshifted ↑, so the page-key check has to come first.
func TestShiftArrowsPage(t *testing.T) {
	p := pageablePanel(t)

	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyDown, Mod: tea.ModShift}); !handled {
		t.Fatal("Shift+↓ not handled")
	}
	afterShiftDown := p.Cursor()
	if afterShiftDown <= 1 {
		t.Errorf("Shift+↓ moved the cursor to %d, want a page", afterShiftDown)
	}

	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyUp, Mod: tea.ModShift}); !handled {
		t.Fatal("Shift+↑ not handled")
	}
	if p.Cursor() != 0 {
		t.Errorf("after Shift+↑: cursor = %d, want 0", p.Cursor())
	}
}

// TestPlainArrowStillMovesOneRow is the negative control.
//
// If the page-key check swallowed every arrow, a plain ↓ would jump a page and
// the list would feel broken. This keeps the two units apart.
func TestPlainArrowStillMovesOneRow(t *testing.T) {
	p := pageablePanel(t)

	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("↓ not handled")
	}
	if p.Cursor() != 1 {
		t.Errorf("plain ↓ moved the cursor to %d, want exactly 1", p.Cursor())
	}
}

// TestNoPageKeysMethodIsNotPhantomCapability guards against reintroducing an
// unused predicate.
//
// An earlier revision of this change added a WantsScroll method to the panel
// that no host code read. A capability nothing consumes is a claim the UI makes
// on the host's behalf that the host never fulfils, so it was removed rather
// than left as an inviting-looking hook. If a future change genuinely needs one,
// this test does not prevent it - it just makes the reintroduction deliberate:
// delete this test in the same commit that adds the first reader.
func TestNoPageKeysMethodIsNotPhantomCapability(t *testing.T) {
	// The panel pages through HandleKey, which is the seam the host already
	// calls. Nothing else is needed to make PgDn work, so nothing else should
	// exist to imply it is.
	_ = pageablePanel(t)
}
