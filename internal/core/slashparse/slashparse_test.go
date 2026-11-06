package slashparse

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// parityFixtures is the fixture set the byte-parity proof drove: every
// separator the two rules disagree about, crossed with the shapes a
// slash line can take.
//
// The set is deliberately exhaustive over the separator characters and
// over the Unicode whitespace runes, because the two rules differ
// exactly there — the ASCII three (space, tab, newline) is the whole
// of rule A's separator set, and rule B's set is every Unicode
// whitespace rune.
func parityFixtures() []string {
	base := []string{
		"",
		"/",
		"//",
		"/cmd",
		"/cmd ",
		"/cmd  ",
		"/cmd   args  ",
		"/cmd args",
		"/cmd\targs",
		"/cmd\nargs",
		"/cmd\vargs",
		"/cmd\fargs",
		"/cmd\rargs",
		"cmd args",
		"cmd",
		"/ cmd",
		" cmd",
		"/cmd  args",
		"/cmd args more",
		"/cmd\vargs more",
		"/cmd\ra",
		" /cmd args",
		"/cmd\t\targs",
		"/cmd\n\nargs",
		"/cmd\v\vargs",
		"/\vcmd",
		"/cmd\v",
		"cmd\vargs",
		"\v",
		"\vcmd\vargs",
		"\u00a0",
		"/cmd\u00a0args",
		"cmd\u00a0args",
		"/cmd \u00a0 args",
	}
	// Every separator rule A and rule B disagree about, plus the
	// Unicode whitespace runes strings.Fields uses.
	seps := []string{
		" ", "\t", "\n", "\v", "\f", "\r",
		"\u0085", "\u00a0", "\u1680", "\u2000", "\u2003", "\u2007",
		"\u2028", "\u2029", "\u202f", "\u205f", "\u3000",
	}
	out := append([]string{}, base...)
	for _, sep := range seps {
		out = append(out,
			"cmd"+sep+"args",
			"/cmd"+sep+"args",
			sep+"cmd",
			"cmd"+sep,
			"/cmd"+sep,
			"/cmd"+sep+"args"+sep,
			sep+"/cmd"+sep+"args",
			"/cmd"+sep+" "+sep+"args",
			"cmd"+sep+sep+"args",
			"/cmd"+sep+"args"+sep+"more",
			"cmd"+sep+"args"+sep+"more",
			"/"+sep+"cmd",
			"/cmd"+sep+"\u00a0",
			"/cmd"+sep+"\u00a0args\u00a0",
			"/cmd"+sep+"args"+"\u00a0",
		)
	}
	return out
}

// dumpLines renders the whole fixture set through every rule, one line
// per (label, input). The labels are the names the byte-parity proof
// uses on both sides of the move, so the two dumps are comparable
// byte for byte.
//
//	A.Split       rule A's "/cmd args" parse
//	A.SplitVerb   rule A's "verb rest" parse
//	A.Trim        rule A's trim
//	B.Split       rule B's "/cmd args" parse
//	B.TrimUnicode rule B's trim
//	A.Trim#2      the second copy of rule A's trim (tuilib's)
func dumpLines() []string {
	var out []string
	emit := func(label string, fn func(string) (string, string)) {
		for _, in := range parityFixtures() {
			a, b := fn(in)
			out = append(out, fmt.Sprintf("%s\t%s\t%s\t%s",
				label, strconv.Quote(in), strconv.Quote(a), strconv.Quote(b)))
		}
	}
	emit("A.Split", Split)
	emit("A.SplitVerb", SplitVerb)
	emit("A.Trim", func(s string) (string, string) { return Trim(s), "" })
	emit("B.Split", SplitFields)
	emit("B.TrimUnicode", func(s string) (string, string) { return TrimUnicode(s), "" })
	emit("A.Trim#2", func(s string) (string, string) { return Trim(s), "" })
	return out
}

// TestSlashParseGolden pins the parse results byte for byte.
//
// The golden is the dump of the whole fixture set, so a change in any
// rule — a separator set, a trim, an off-by-one in the scan — shows up
// as a diff rather than as a quiet behaviour change.
func TestSlashParseGolden(t *testing.T) {
	golden := filepath.Join("testdata", "golden.txt")
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("reading the golden: %v", err)
	}
	got := strings.Join(dumpLines(), "\n") + "\n"
	if got != string(want) {
		t.Errorf("the parse results changed:\n--- want (%d bytes)\n%s\n--- got (%d bytes)\n%s",
			len(want), want, len(got), got)
	}
}

// TestSlashParseDump writes the dump to the path in
// SLASHPARSE_DUMP. The byte-parity proof uses it to compare the
// pre-change tree's copies against this package's functions.
func TestSlashParseDump(t *testing.T) {
	path := os.Getenv("SLASHPARSE_DUMP")
	if path == "" {
		t.Skip("SLASHPARSE_DUMP is not set")
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating the dump: %v", err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	for _, line := range dumpLines() {
		if _, err := w.WriteString(line + "\n"); err != nil {
			t.Fatalf("writing the dump: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flushing the dump: %v", err)
	}
}

// splitPair collects a parse result into a comparable pair.
func splitPair(a, b string) [2]string { return [2]string{a, b} }

// TestTheTwoRulesDiffer pins the difference between rule A and rule B.
//
// The package carries two rules because they are not the same rule.
// This test states where they differ, so a future unification is a
// deliberate change and not an accident.
func TestTheTwoRulesDiffer(t *testing.T) {
	cases := []struct {
		in    string
		wantA [2]string
		wantB [2]string
	}{
		{"/cmd\vargs", [2]string{"cmd\vargs", ""}, [2]string{"cmd", "args"}},
		{"/cmd\fargs", [2]string{"cmd\fargs", ""}, [2]string{"cmd", "args"}},
		{"/cmd\rargs", [2]string{"cmd\rargs", ""}, [2]string{"cmd", "args"}},
		{"/cmd\u00a0args", [2]string{"cmd\u00a0args", ""}, [2]string{"cmd", "args"}},
		{"/cmd args", [2]string{"cmd", "args"}, [2]string{"cmd", "args"}},
		{"cmd args", [2]string{"", ""}, [2]string{"cmd", "args"}},
	}
	for _, tc := range cases {
		if got := splitPair(Split(tc.in)); got != tc.wantA {
			t.Errorf("Split(%q) = %q, want %q", tc.in, got, tc.wantA)
		}
		if got := splitPair(SplitFields(tc.in)); got != tc.wantB {
			t.Errorf("SplitFields(%q) = %q, want %q", tc.in, got, tc.wantB)
		}
	}
}

// TestSplitVerb pins the "verb rest" shape: the `/plugin` tail.
func TestSplitVerb(t *testing.T) {
	cases := []struct {
		in   string
		want [2]string
	}{
		{"", [2]string{"", ""}},
		{"list", [2]string{"list", ""}},
		{"list ", [2]string{"list", ""}},
		{"list plugin", [2]string{"list", "plugin"}},
		{"list\tplugin", [2]string{"list", "plugin"}},
		{"list\nplugin", [2]string{"list", "plugin"}},
		{"list\vplugin", [2]string{"list\vplugin", ""}},
		{"list  plugin  ", [2]string{"list", "plugin"}},
	}
	for _, tc := range cases {
		if got := splitPair(SplitVerb(tc.in)); got != tc.want {
			t.Errorf("SplitVerb(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseLeadingCommands(t *testing.T) {
	cases := []struct {
		in           string
		wantCommands []string
		wantTask     string
	}{
		{"", nil, ""},
		{"   ", nil, ""},
		{"hello world", nil, "hello world"},
		{"/", nil, "/"},
		{"//", nil, "//"},
		{"// comment", nil, "// comment"},
		{"/plan", []string{"plan"}, ""},
		{"/plan   ", []string{"plan"}, ""},
		{"/plan fix bug", []string{"plan"}, "fix bug"},
		{"/boost /plan /goal", []string{"boost", "plan", "goal"}, ""},
		{"/boost /plan /goal fix the build", []string{"boost", "plan", "goal"}, "fix the build"},
		{"  /boost \t /plan\n/goal   review code  ", []string{"boost", "plan", "goal"}, "review code"},
		{"/boost /plan /goal:test do this", []string{"boost", "plan", "goal:test"}, "do this"},
	}

	for _, tc := range cases {
		cmds, task := ParseLeadingCommands(tc.in)
		if len(cmds) != len(tc.wantCommands) {
			t.Errorf("ParseLeadingCommands(%q) commands = %v, want %v", tc.in, cmds, tc.wantCommands)
			continue
		}
		for i := range cmds {
			if cmds[i] != tc.wantCommands[i] {
				t.Errorf("ParseLeadingCommands(%q) commands[%d] = %q, want %q", tc.in, i, cmds[i], tc.wantCommands[i])
			}
		}
		if task != tc.wantTask {
			t.Errorf("ParseLeadingCommands(%q) task = %q, want %q", tc.in, task, tc.wantTask)
		}
	}
}
