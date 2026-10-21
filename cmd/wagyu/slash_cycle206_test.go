// slash_cycle206_test.go — cycle 206.
//
// Two behaviours of the four list panels (/tasks /artifact /codesearch
// /context) that the earlier panel cycles left uncovered:
//
//   1. alias resolution before the panel table is consulted, so `/cs`
//      and `/search` open the Code Search panel (REF evidence:
//      agy206/probe_c.py alias, probe_alias.py).
//   2. the panel owns every key until Esc — the reference is inert for
//      every key driven at the empty panel, including the globals the
//      clone used to leak (ctrl+g → $EDITOR, ctrl+l → alt-screen,
//      pgup/pgdn → transcript scroll). REF evidence: agy206/probe_one.py.
//
// The layout assertions for these panels live in slash_panels_test.go
// (TestPanelTasksLayout etc.); this file only covers the two cycle-206
// code paths.

package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestCycle206CanonicalPanelCommand(t *testing.T) {
	cases := map[string]string{
		// codesearch aliases — the in-scope fix.
		"cs":     "codesearch",
		"search": "codesearch",
		// The same mechanism, verified on REF for the other panel
		// commands whose alias table entry already exists.
		"settings": "config",
		"quota":    "usage",
		"undo":     "rewind",
		// Canonical names and non-aliases pass through untouched.
		"codesearch": "codesearch",
		"tasks":      "tasks",
		"artifact":   "artifact",
		"context":    "context",
		"clear":      "clear",
		"mcp":        "mcp",
		"":           "",
	}
	for in, want := range cases {
		if got := canonicalPanelCommand(in); got != want {
			t.Errorf("canonicalPanelCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCycle206OpenSlashPanelViaAlias(t *testing.T) {
	cases := []struct {
		cmd  string
		kind panelKind
	}{
		{"cs", panelCodeSearch},
		{"search", panelCodeSearch},
		{"settings", panelConfig},
		{"quota", panelUsage},
		{"undo", panelRewind},
	}
	for _, c := range cases {
		m := &runMode{}
		if !m.openSlashPanel(c.cmd) {
			t.Errorf("openSlashPanel(%q) = false, want true", c.cmd)
			continue
		}
		if m.panel.kind != c.kind {
			t.Errorf("openSlashPanel(%q) kind = %v, want %v", c.cmd, m.panel.kind, c.kind)
		}
		// The Esc notice quotes what the user typed, not the canonical
		// name; the alias itself is what the user sees dismissed.
		if m.panel.cmd != "/"+c.cmd {
			t.Errorf("openSlashPanel(%q) cmd = %q, want %q", c.cmd, m.panel.cmd, "/"+c.cmd)
		}
	}
	// A name that is not an alias of a panel command still falls through.
	m := &runMode{}
	if m.openSlashPanel("clear") {
		t.Error("openSlashPanel(\"clear\") = true, want false")
	}
}

// TestCycle206ListPanelsOwnEveryKey drives each leaked key at each of the
// four panels and asserts the panel swallows it.
func TestCycle206ListPanelsOwnEveryKey(t *testing.T) {
	kinds := []struct {
		kind panelKind
		cmd  string
	}{
		{panelTasks, "/tasks"},
		{panelArtifact, "/artifact"},
		{panelCodeSearch, "/codesearch"},
		{panelContext, "/context"},
	}
	keys := []tea.Key{
		{Text: "k"}, // advertised by /tasks
		{Text: "x"},
		{Text: "p"}, // advertised by /artifact
		{Code: tea.KeyEnter},
		{Code: tea.KeyTab},
		{Code: tea.KeyBackspace},
		{Code: tea.KeyUp},
		{Code: tea.KeyDown},
		{Code: tea.KeyLeft},
		{Code: tea.KeyRight},
		{Code: tea.KeyPgUp},
		{Code: tea.KeyPgDown},
		{Code: tea.KeyHome, Mod: tea.ModCtrl},
		{Code: tea.KeyEnd, Mod: tea.ModCtrl},
		{Code: 'g', Mod: tea.ModCtrl}, // used to open $EDITOR over the panel
		{Code: 'l', Mod: tea.ModCtrl}, // used to toggle alt-screen
		{Code: 'o', Mod: tea.ModCtrl}, // tool-step expand groups
	}
	for _, k := range kinds {
		m := mkPanel(k.kind, k.cmd, 0)
		before := len(m.response)
		for _, key := range keys {
			handled, _ := m.handlePanelKey(key)
			if !handled {
				t.Errorf("%s: key %+v fell through, want swallowed", k.cmd, key)
			}
		}
		if m.panel.kind != k.kind {
			t.Errorf("%s: panel closed by a non-Esc key", k.cmd)
		}
		if len(m.response) != before {
			t.Errorf("%s: key changed the transcript (%d -> %d)", k.cmd, before, len(m.response))
		}
	}
}

// ctrl+c is deliberately left to the global binding (REF does not quit on
// it even with no panel open, and the clone is inert on it today).
func TestCycle206ListPanelsCtrlCFallsThrough(t *testing.T) {
	for _, k := range []struct {
		kind panelKind
		cmd  string
	}{
		{panelTasks, "/tasks"},
		{panelArtifact, "/artifact"},
		{panelCodeSearch, "/codesearch"},
		{panelContext, "/context"},
	} {
		m := mkPanel(k.kind, k.cmd, 0)
		if handled, _ := m.handlePanelKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
			t.Errorf("%s: ctrl+c must fall through to the global binding", k.cmd)
		}
	}
}

// Esc is the only key that dismisses one of these panels: /tasks and
// /artifact echo the "Exited /cmd command" notice, /codesearch and
// /context close silently.
func TestCycle206ListPanelsEscCloses(t *testing.T) {
	cases := []struct {
		kind   panelKind
		cmd    string
		notice bool
	}{
		{panelTasks, "/tasks", true},
		{panelArtifact, "/artifact", true},
		{panelCodeSearch, "/codesearch", false},
		{panelContext, "/context", false},
	}
	for _, c := range cases {
		m := mkPanel(c.kind, c.cmd, 0)
		handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
		if !handled {
			t.Errorf("%s: esc not handled", c.cmd)
			continue
		}
		if m.panel.kind != panelNone {
			t.Errorf("%s: esc did not close the panel (kind=%v)", c.cmd, m.panel.kind)
		}
		got := stripSGR(strings.Join(m.response, "\n"))
		if c.notice {
			if !strings.Contains(got, "Exited "+c.cmd+" command") {
				t.Errorf("%s: esc notice = %q", c.cmd, got)
			}
		} else if got != "" {
			t.Errorf("%s: esc should be silent, got %q", c.cmd, got)
		}
	}
}

// The cycle-206 router is scoped to the four list panels: the other
// row-less panels keep their previous fall-through semantics.
func TestCycle206ScopedToTheFourListPanels(t *testing.T) {
	for _, k := range []panelKind{panelMCP, panelPermissions} {
		if listPanelKind(k) {
			t.Errorf("listPanelKind(%v) = true, want false", k)
		}
	}
	m := mkPanel(panelMCP, "/mcp", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: 'g', Mod: tea.ModCtrl}); handled {
		t.Error("/mcp: ctrl+g should still fall through (out of cycle-206 scope)")
	}
}
