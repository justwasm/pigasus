// hidden_slash_test.go — the TUI's hidden slash commands, pinned.
//
// The picker shows the built-in catalog (slashcommands.Default);
// eight more commands are handled but never listed. The inventory lives
// in docs/guide/hidden-commands-and-flags.md — this test is its other
// half: a handler that the catalog neither names nor lists as an alias
// is a hidden command.
package tui

import (
	"sort"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// TestHiddenSlashCommands pins the hidden commands the TUI handles but
// does not list.
func TestHiddenSlashCommands(t *testing.T) {
	for _, name := range []string{
		"continue", "login", "narration", "quote", "share",
		"terminal", "upload", "triggers",
	} {
		if _, ok := slashHandlers[name]; !ok {
			t.Errorf("slashHandlers[%q] is missing; the command would fall through to the model", name)
		}
		if listed(name) {
			t.Errorf("%q is listed in the picker; it is not hidden any more", "/"+name)
		}
	}
}

// TestHiddenSlashCommandsAreNotTheCatalog checks the whole handler map
// against the catalog: the difference is the hidden set, and it must
// stay exactly the eight commands above. A catalog row's aliases count
// as listed — `/exit` advertises `quit`, `/fork` advertises `branch`.
func TestHiddenSlashCommandsAreNotTheCatalog(t *testing.T) {
	listed := map[string]bool{}
	for _, cmd := range slashcommands.Default {
		name := strings.TrimPrefix(cmd.Name, "/")
		listed[name] = true
		for _, alias := range cmd.Aliases {
			listed[alias] = true
		}
	}
	var hidden []string
	for name := range slashHandlers {
		if !listed[name] {
			hidden = append(hidden, name)
		}
	}
	sort.Strings(hidden)
	want := []string{
		"continue", "login", "narration", "quote", "share",
		"terminal", "triggers", "upload",
	}
	if len(hidden) != len(want) {
		t.Fatalf("hidden slash commands = %v\nwant %v", hidden, want)
	}
	for i := range want {
		if hidden[i] != want[i] {
			t.Fatalf("hidden slash commands = %v\nwant %v", hidden, want)
		}
	}
}

// TestVocabularyDispatchedMatchesTheHostTable pins the host's command
// table against the vocabulary's Dispatched set.
//
// The vocabulary owns the rows; the host owns the handlers. The two
// must agree exactly: a row the vocabulary marks as dispatched but the
// host has no handler for falls through to the model, and a handler the
// vocabulary does not know is a command the picker never offers.
//
// The table is keyed by the *canonical* name only (cycle 381, tranche
// 88), so a key that is an alias — "quit", "featdiff", "new" — is a
// second copy of the alias rule and is rejected here.
func TestVocabularyDispatchedMatchesTheHostTable(t *testing.T) {
	dispatched := map[string]bool{}
	for _, cmd := range slashcommands.Default {
		if cmd.Dispatched {
			dispatched[strings.TrimPrefix(cmd.Name, "/")] = true
		}
	}
	for _, cmd := range slashcommands.Hidden {
		if !cmd.Dispatched {
			t.Errorf("hidden command %q is not marked dispatched", cmd.Name)
		}
		dispatched[strings.TrimPrefix(cmd.Name, "/")] = true
	}

	for name := range slashHandlers {
		if !dispatched[name] {
			t.Errorf("slashHandlers[%q] is not in the vocabulary's dispatched set", name)
		}
	}
	// The host answers a command from the legacy table or from the
	// migrated dispatcher (internal/tui/commands); the vocabulary only
	// has to be reachable through one of them.
	m := &RootModel{}
	for name := range dispatched {
		if !m.dispatchesCommand(name) {
			t.Errorf("the vocabulary dispatches %q but the host has no handler for it", name)
		}
	}
}

// TestSlashHandlerKeysAreCanonicalNames checks that the host's table
// carries no alias key. The alias rule belongs to the vocabulary, so a
// second copy in the map is a second owner of it.
func TestSlashHandlerKeysAreCanonicalNames(t *testing.T) {
	for name := range slashHandlers {
		canonical, ok := slashcommands.Canonical(name)
		if !ok {
			t.Errorf("slashHandlers key %q is not a known command", name)
			continue
		}
		if canonical != name {
			t.Errorf("slashHandlers key %q is an alias; the canonical name is %q", name, canonical)
		}
	}
}

// listed reports whether the picker shows the command under that name.
func listed(name string) bool {
	for _, cmd := range slashcommands.Default {
		if cmd.Name == "/"+name {
			return true
		}
		for _, alias := range cmd.Aliases {
			if alias == name {
				return true
			}
		}
	}
	return false
}

// TestHiddenOpenagyCommandsDispatch pins that the three openagy-only
// hidden commands still reach their handlers now that they are registered
// in the vocabulary's Hidden list.
func TestHiddenOpenagyCommandsDispatch(t *testing.T) {
	for _, command := range []string{"/terminal", "/upload", "/triggers"} {
		m := &RootModel{}
		initRootModelInput(m)
		handled, _ := m.handleSlashCommand(command)
		if !handled {
			t.Errorf("%s was not dispatched", command)
		}
		joined := strings.Join(m.conversation.response, "\n")
		if !strings.Contains(joined, "only available in a --remote-agent session") {
			t.Errorf("%s in a local session = %q", command, joined)
		}
	}
}
