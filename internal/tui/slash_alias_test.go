// slash_alias_test.go — the alias rule reaches the same handler as the
// command it names.
//
// Cycle 381, tranche 88: the alias rule lives in
// internal/ui/slashcommands. The host resolves a typed name through it
// before the dispatch table is consulted, so an alias and its command
// are the same command. The picker advertises the aliases in the /help
// COMMANDS tab ("/exit (quit)"), and the reference resolves them — the
// cycle-206 work proved it for the panel commands ("/cs" and "/search"
// open the Code Search panel like "/codesearch").
//
// The host's own table used to carry a second, shorter copy of the
// rule: five alias keys ("new", "quit", "featdiff", "diff-agy",
// "record") beside the canonical ones. Two advertised aliases were in
// neither copy and fell through to "Unknown command: /switch" — the
// gap this tranche closes.
package tui

import (
	"reflect"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// handlerPointer is the identity of a handler: a Go func value is not
// comparable, so the code pointer is what says "the same handler".
func handlerPointer(h slashHandler) uintptr {
	return reflect.ValueOf(h).Pointer()
}

// TestAdvertisedAliasesReachTheirCommand pins the one rule: an alias
// reaches the same handler as the command it names.
//
// A row may be dispatched *and* open a panel — the panel is tried first
// and the handler answers the rest — so this test checks the handler,
// which is what the alias rule reaches after the panel declined.
func TestAdvertisedAliasesReachTheirCommand(t *testing.T) {
	for _, row := range slashcommands.Default {
		if !row.Dispatched {
			continue
		}
		canonical := row.Name[1:]
		want, ok := slashHandlers[canonical]
		if !ok {
			t.Errorf("%s is dispatched but the host has no handler for it", row.Name)
			continue
		}
		for _, alias := range row.Aliases {
			resolved, ok := slashcommands.Canonical(alias)
			if !ok {
				t.Errorf("the alias %q of %s does not resolve", alias, row.Name)
				continue
			}
			if resolved != canonical {
				// The one exception the vocabulary knows: an alias
				// that is a command of its own never shadows it.
				// "branch" is an alias of "/fork" in the catalogue
				// and a command of its own in the host, so it
				// resolves to itself — and the host answers it.
				if _, ok := slashHandlers[resolved]; !ok {
					t.Errorf("the alias %q of %s resolves to %q, which the host does not dispatch", alias, row.Name, resolved)
				}
				continue
			}
			got, ok := slashHandlers[resolved]
			if !ok {
				t.Errorf("the alias %q of %s resolves to %q, which the host does not dispatch", alias, row.Name, resolved)
				continue
			}
			if handlerPointer(got) != handlerPointer(want) {
				t.Errorf("the alias %q reaches %v, want the handler of %s", alias, got, row.Name)
			}
		}
	}
}

// TestResumeAliasesAreAnswered pins the two aliases the host's own
// table used to leave out: "/switch" and "/conversation" are the
// spellings the /help COMMANDS tab advertises for "/resume", and the
// reference answers them.
//
// Before cycle 381, tranche 88, the alias rule existed twice — once
// as five explicit keys in the host's table and once as the
// catalogue's Aliases field — and the two copies disagreed: these two
// spellings were advertised but answered with "Unknown command".
func TestResumeAliasesAreAnswered(t *testing.T) {
	for _, alias := range []string{"switch", "conversation"} {
		resolved, ok := slashcommands.Canonical(alias)
		if !ok || resolved != "resume" {
			t.Fatalf("Canonical(%q) = (%q, %v), want (\"resume\", true)", alias, resolved, ok)
		}
		if _, ok := slashHandlers[resolved]; !ok {
			t.Errorf("the alias %q resolves to %q, which the host does not dispatch", alias, resolved)
		}
	}
}
