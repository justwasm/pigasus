// scenario_test.go — the shared-prefix Tab rule, pinned as named
// scenarios.
//
// The rule exists so an ambiguous buffer gets *advanced* rather than
// guessed: when several commands match, the first Tab pushes the
// composer to the longest prefix they all share and leaves the popup
// open, so the second Tab still commits the highlighted row. These
// scenarios pin both halves — the advance, and the fall-back that
// keeps the old commit behaviour intact.
package slashpicker

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// newTabPanel builds a panel whose composer buffer is wired to a real
// string, so a Tab can be observed through the buffer rather than
// through private state.
func newTabPanel(t *testing.T, initial string) (*Panel, *string) {
	t.Helper()
	buf := initial
	p := New(Seams{
		Buffer:    func() string { return buf },
		SetBuffer: func(s string) { buf = s },
	})
	p.Update([]rune(initial))
	return p, &buf
}

// pressTab exercises the same entry point the host uses.
func pressTab(t *testing.T, p *Panel) (bool, tea.Cmd) {
	t.Helper()
	return p.UpdateKey(tea.Key{Code: tea.KeyTab})
}

// TestTabScenario_SharedPrefixAdvancesInsteadOfGuessing pins the core
// value of the rule: "/age" matches more than one command, and every
// match shares "/agents", so Tab must write that prefix instead of
// silently picking one of the candidates.
func TestTabScenario_SharedPrefixAdvancesInsteadOfGuessing(t *testing.T) {
	p, buf := newTabPanel(t, "/age")
	if got := len(p.all); got < 2 {
		t.Fatalf("fixture needs an ambiguous prefix; got %d matches", got)
	}
	before := p.Selected().Name

	handled, cmd := pressTab(t, p)
	if !handled {
		t.Fatal("Tab on an ambiguous buffer must be handled")
	}
	if cmd == nil {
		t.Fatal("Tab must report the new buffer through a tea.Cmd")
	}
	want := "/agents"
	if *buf != want {
		t.Fatalf("buffer = %q, want the shared prefix %q", *buf, want)
	}
	msg, ok := cmd().(CompletedMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want CompletedMsg", cmd())
	}
	if msg.Text != want {
		t.Errorf("CompletedMsg.Text = %q, want %q", msg.Text, want)
	}
	// The advance must not be mistaken for a commit: the popup stays up
	// so a second Tab can select.
	if !p.Visible() {
		t.Error("popup must stay open after a shared-prefix advance")
	}
	_ = before
}

// TestTabScenario_SecondTabCommits pins the pairing: after the advance,
// the next Tab takes the highlighted row, so the user is never stuck.
func TestTabScenario_SecondTabCommits(t *testing.T) {
	p, buf := newTabPanel(t, "/age")
	if _, cmd := pressTab(t, p); cmd == nil {
		t.Fatal("first Tab should advance the prefix")
	}
	advanced := *buf

	handled, cmd := pressTab(t, p)
	if !handled || cmd == nil {
		t.Fatal("second Tab should commit the highlighted row")
	}
	if *buf == advanced {
		t.Fatalf("second Tab left the buffer at the prefix %q", advanced)
	}
	if !stringsHasPrefix(*buf, advanced) {
		t.Errorf("committed buffer %q no longer extends the prefix %q", *buf, advanced)
	}
}

// TestTabScenario_UniqueMatchStillCommitsImmediately pins that a
// single match keeps the old behaviour: Tab completes it and closes
// the popup, with no pointless intermediate step.
func TestTabScenario_UniqueMatchStillCommitsImmediately(t *testing.T) {
	p, buf := newTabPanel(t, "/add")
	if got := len(p.all); got != 1 {
		t.Fatalf("fixture needs a unique match; got %d", got)
	}
	handled, cmd := pressTab(t, p)
	if !handled || cmd == nil {
		t.Fatal("Tab on a unique match must commit")
	}
	if *buf == "/add" {
		t.Error("buffer should have been completed to the unique command")
	}
	if p.Visible() {
		t.Error("popup should close after committing a unique match")
	}
}

// TestTabScenario_ExactCommandNameCommitsRatherThanStalling is the
// degenerate case the rule must not trap: once the buffer already *is*
// the shared prefix, there is nothing to add, so Tab must fall through
// to committing the highlighted row instead of doing nothing.
func TestTabScenario_ExactCommandNameCommitsRatherThanStalling(t *testing.T) {
	p, buf := newTabPanel(t, "/agents")
	if !p.Visible() {
		t.Skip("no rows match /agents in this catalogue")
	}
	before := *buf
	handled, _ := pressTab(t, p)
	if !handled {
		t.Fatal("Tab must be handled")
	}
	if *buf == before && p.Visible() {
		t.Error("Tab stalled: neither advanced the buffer nor committed a row")
	}
}

// TestTabScenario_SubModeIsUnaffected pins the scope limit: the
// /model argument mode completes a row's value, where a shared prefix
// over command names would be meaningless. It must commit as before.
func TestTabScenario_SubModeIsUnaffected(t *testing.T) {
	p, buf := newTabPanel(t, "/model ")
	p.SetModelCatalogueSource(func() []ModelRow {
		return []ModelRow{
			{Label: "Alpha One", ID: "alpha-one"},
			{Label: "Alpha Two", ID: "alpha-two"},
		}
	})
	p.Update([]rune("/model "))
	if !p.modelMode {
		t.Fatal("fixture did not enter model mode")
	}
	if len(p.all) < 2 {
		t.Fatalf("fixture needs two model rows; got %d", len(p.all))
	}
	handled, cmd := pressTab(t, p)
	if !handled || cmd == nil {
		t.Fatal("Tab in model mode must commit")
	}
	if !stringsHasPrefix(*buf, "/model ") {
		t.Errorf("model Tab wrote %q, want a /model argument", *buf)
	}
	// The shared prefix over the *labels* is "Alpha ", which must not
	// be what a sub-mode writes: it completes a whole row instead.
	if *buf == "/model Alpha " {
		t.Error("sub-mode must commit a row, not a shared prefix")
	}
}

// TestTabScenario_HostCommandSourceIsLeftAlone pins the other scope
// limit: when the host supplies its own rows, the popup cannot assume
// they are command names, so the shared-prefix rule stays out of the
// way and Tab commits.
func TestTabScenario_HostCommandSourceIsLeftAlone(t *testing.T) {
	buf := "/x"
	p := New(Seams{
		Buffer:    func() string { return buf },
		SetBuffer: func(s string) { buf = s },
	})
	p.SetCommandSource(func(prefix string) []slashcommands.Command {
		return []slashcommands.Command{
			{Name: "/xxx-one"},
			{Name: "/xxx-two"},
		}
	})
	p.Update([]rune(buf))
	if len(p.all) < 2 {
		t.Fatalf("fixture needs two host rows; got %d", len(p.all))
	}
	handled, cmd := pressTab(t, p)
	if !handled || cmd == nil {
		t.Fatal("Tab must commit under a host-supplied source")
	}
	if buf == "/xxx-" {
		t.Error("shared-prefix rule must not run against host-supplied rows")
	}
}

// stringsHasPrefix is a local alias so the scenarios read plainly and
// the file needs no extra import beyond testing and tea.
func stringsHasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return s[:len(prefix)] == prefix
}
