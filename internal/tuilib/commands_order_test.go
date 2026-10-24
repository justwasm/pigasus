// commands_order_test.go — cycle 184.
//
// The "/" picker and the "/help" COMMANDS tab use *different* orders in
// the reference binary:
//
//	/help   → flat alphabetical sort of the whole 99-entry catalog
//	"/"     → registration order (built-ins, then skills)
//
// Evidence (both captured at 220×30 against REF v1.4.0):
//
//	/tmp/agycmp/ref_catalog.txt                    (help)
//	/tmp/agycmp/hotpath/picker/ref.000-slash.txt   (picker)
//
// The picker's first three rows are /add-dir, /agents, /artifact; the
// help list's first three are /add-dir, /agents, /agents-sdk.
package tuilib

import "testing"

func TestSortedSlashCommandsMatchesReferenceHelpOrder(t *testing.T) {
	// First 12 rows of the /help COMMANDS tab (reference plus openagy superset).
	want := []string{
		"/accounts",
		"/add-dir",
		"/agents",
		"/agents-sdk",
		"/agy-customizations",
		"/antigravity-guide",
		"/artifact",
		"/boost",
		"/browser",
		"/btw",
		"/changelog",
		"/chrome-devtools-plugin:a11y-debugging",
	}
	got := SortedSlashCommands()
	if len(got) != len(DefaultSlashCommands) {
		t.Fatalf("SortedSlashCommands() has %d entries, want %d", len(got), len(DefaultSlashCommands))
	}
	for i, w := range want {
		if got[i].Name != w {
			t.Errorf("row %d = %q, want %q", i, got[i].Name, w)
		}
	}
}

func TestSortedSlashCommandsIsAscending(t *testing.T) {
	got := SortedSlashCommands()
	for i := 1; i < len(got); i++ {
		if got[i-1].Name >= got[i].Name {
			t.Fatalf("not ascending at %d: %q >= %q", i, got[i-1].Name, got[i].Name)
		}
	}
}

func TestSortedSlashCommandsDoesNotMutateThePickerCatalog(t *testing.T) {
	before := make([]string, len(DefaultSlashCommands))
	for i, c := range DefaultSlashCommands {
		before[i] = c.Name
	}
	_ = SortedSlashCommands()
	for i, c := range DefaultSlashCommands {
		if c.Name != before[i] {
			t.Fatalf("DefaultSlashCommands was reordered at %d: %q != %q", i, c.Name, before[i])
		}
	}
}

// The two orders must actually differ, and in the way the captures show:
// /agents-sdk and /artifact swap places between the picker and /help.
func TestPickerAndHelpOrdersDiffer(t *testing.T) {
	picker := DefaultSlashCommands
	help := SortedSlashCommands()
	if picker[2].Name != "/artifact" {
		t.Fatalf("picker row 2 = %q, want /artifact", picker[2].Name)
	}
	if help[3].Name != "/agents-sdk" {
		t.Fatalf("help row 3 = %q, want /agents-sdk", help[3].Name)
	}
}
