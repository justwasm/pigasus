package keybindings

import (
	"strings"
	"testing"
)

// TestReport_AbsentFileAnswersTheReferenceString pins the reference's
// own answer for a file that holds no bindings.
func TestReport_AbsentFileAnswersTheReferenceString(t *testing.T) {
	got, err := Report("/home/u/.gemini/antigravity-cli/keybindings.json", nil)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if got != "No keybindings found." {
		t.Errorf("Report = %q, want %q", got, "No keybindings found.")
	}
}

// TestReport_EmptyFileAnswersTheReferenceString pins the same answer for
// a file that exists but carries nothing.
func TestReport_EmptyFileAnswersTheReferenceString(t *testing.T) {
	for _, data := range [][]byte{nil, {}, []byte("\n"), []byte("{}"), []byte("{}\n")} {
		got, err := Report("keybindings.json", data)
		if err != nil {
			t.Fatalf("Report(%q): %v", data, err)
		}
		if got != "No keybindings found." {
			t.Errorf("Report(%q) = %q, want %q", data, got, "No keybindings found.")
		}
	}
}

// TestReport_ListsTheConfiguredBindings pins the report's bytes: the
// header, then one row per configured action — the keys, then the
// action's label.
func TestReport_ListsTheConfiguredBindings(t *testing.T) {
	data := []byte(`{"cli.exit": ["ctrl+d"], "cli.clear_screen": ["ctrl+l"]}`)
	got, err := Report("keybindings.json", data)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	want := strings.Join([]string{
		"Set custom keybindings",
		"  ctrl+d\tExit the CLI",
		"  ctrl+l\tClear CLI screen",
	}, "\n")
	if got != want {
		t.Errorf("Report =\n%q\nwant\n%q", got, want)
	}
}

// TestReport_NamesTheExitAction pins that the exit binding is reported
// under the catalogue's own name for the /exit command.
func TestReport_NamesTheExitAction(t *testing.T) {
	got, err := Report("keybindings.json", []byte(`{"cli.exit": ["ctrl+d"]}`))
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !strings.Contains(got, "Exit the CLI") {
		t.Errorf("Report = %q, want it to name the exit action", got)
	}
}

// TestReport_JoinsSeveralKeys pins that one action with several keys is
// one row whose keys are comma-separated — the reference's /help
// SHORTCUTS shape ("alt+enter, ctrl+j, shift+enter").
func TestReport_JoinsSeveralKeys(t *testing.T) {
	got, err := Report("keybindings.json", []byte(`{"prompt.insert_newline": ["shift+enter", "ctrl+j"]}`))
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !strings.Contains(got, "shift+enter, ctrl+j") {
		t.Errorf("Report = %q, want the keys joined with a comma", got)
	}
}

// TestReport_KeepsTheFileOrder pins that the rows follow the file's own
// order — the report lists what the user wrote, not an alphabetical
// sort of it.
func TestReport_KeepsTheFileOrder(t *testing.T) {
	data := []byte(`{"cli.clear_screen": ["ctrl+l"], "cli.exit": ["ctrl+d"]}`)
	got, err := Report("keybindings.json", data)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	clear := strings.Index(got, "ctrl+l")
	exit := strings.Index(got, "ctrl+d")
	if clear < 0 || exit < 0 {
		t.Fatalf("Report = %q, want both rows", got)
	}
	if clear > exit {
		t.Errorf("Report = %q, want the file's order (ctrl+l first)", got)
	}
}

// TestReport_SkipsADisabledAction pins that an action mapped to an
// empty list is disabled — there is no binding to report.
func TestReport_SkipsADisabledAction(t *testing.T) {
	got, err := Report("keybindings.json", []byte(`{"cli.clear_screen": [], "cli.exit": ["ctrl+d"]}`))
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if strings.Contains(got, "Clear CLI screen") {
		t.Errorf("Report = %q, want the disabled action skipped", got)
	}
	if !strings.Contains(got, "Exit the CLI") {
		t.Errorf("Report = %q, want the enabled action reported", got)
	}
}

// TestReport_UnknownActionIsNamedItself pins the fallback: an action
// the label table does not hold is reported under its own name.
func TestReport_UnknownActionIsNamedItself(t *testing.T) {
	got, err := Report("keybindings.json", []byte(`{"cli.not_a_real_action": ["ctrl+q"]}`))
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !strings.Contains(got, "cli.not_a_real_action") {
		t.Errorf("Report = %q, want the action's own name", got)
	}
}

// TestReport_MalformedFileCarriesTheReferenceError pins the error
// sentence: the reference names the file and tells the user how to
// reset it.
func TestReport_MalformedFileCarriesTheReferenceError(t *testing.T) {
	got, err := Report("/home/u/keybindings.json", []byte("{not json"))
	if err == nil {
		t.Fatalf("Report = %q, want an error", got)
	}
	msg := err.Error()
	for _, want := range []string{
		"error loading keybindings from /home/u/keybindings.json",
		"delete /home/u/keybindings.json and run /keybindings to reset",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want it to carry %q", msg, want)
		}
	}
}

// TestReport_MalformedEntryFallsBackToTheDefault pins that one broken
// entry does not take the rest of the file with it.
func TestReport_MalformedEntryFallsBackToTheDefault(t *testing.T) {
	data := []byte(`{"cli.broken": 42, "cli.exit": ["ctrl+d"]}`)
	got, err := Report("keybindings.json", data)
	if err != nil {
		t.Fatalf("Report: %v", err)
	}
	if !strings.Contains(got, "Exit the CLI") {
		t.Errorf("Report = %q, want the readable action reported", got)
	}
}

// TestCanonicalKey pins the key normalisation: trimmed, lower-cased and
// without the spaces around the "+" separators.
func TestCanonicalKey(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"Ctrl+L", "ctrl+l"},
		{" ctrl+l ", "ctrl+l"},
		{"ctrl + shift + -", "ctrl+shift+-"},
		{"Shift+Enter", "shift+enter"},
	} {
		if got := canonicalKey(tc.in); got != tc.want {
			t.Errorf("canonicalKey(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
