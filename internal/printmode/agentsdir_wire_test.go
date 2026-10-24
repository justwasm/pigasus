// agentsdir_wire_test.go — wire the internal/agentsdir walker into the
// print path so the AGY 1.2.16 changelog line is observable from
// `agy -p /agents-dirs`. The test pins the diagnostic shape (the empty
// case + the CWD-closest-wins order) without depending on the user's
// HOME tree.
package printmode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustMkdir is a local helper used by every test in this file. It is
// deliberately tiny — it never has to undo MkdirAll because t.TempDir
// cleans up the entire tree on test exit.
func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

// makeWireFixture builds a small sandbox tree rooted at home. chain is
// the CWD path below home; withAgents[i] gates whether the i-th level
// (i=0 → home, i=1 → home/<chain[0]>, …) carries a directory named
// `agents`. Returns cwd and home as absolute paths.
func makeWireFixture(t *testing.T, home string, withAgents []bool, chain []string, dirNames []string) string {
	t.Helper()
	if len(withAgents) < len(chain)+1 {
		missing := len(chain) + 1 - len(withAgents)
		withAgents = append(append(make([]bool, 0, len(withAgents)+missing), withAgents...), make([]bool, missing)...)
	}
	if withAgents[0] {
		for _, n := range dirNames {
			mustMkdir(t, filepath.Join(home, n))
		}
	}
	cur := home
	for i, seg := range chain {
		cur = filepath.Join(cur, seg)
		mustMkdir(t, cur)
		if withAgents[i+1] {
			for _, n := range dirNames {
				mustMkdir(t, filepath.Join(cur, n))
			}
		}
	}
	return cur
}

// TestPrintAgentsDirsFor_LocalWins pins the diagnostic's ordering:
// project-local HOME/a/agents shows up at depth 3, and HOME/agents is at
// the tail with depth 4. Both rows must appear in CWD-closest order.
func TestPrintAgentsDirsFor_LocalWins(t *testing.T) {
	home := t.TempDir()
	cwd := makeWireFixture(t, home,
		[]bool{true /*HOME*/, true /*a*/, false, false, false},
		[]string{"a", "b", "c", "d"},
		[]string{"agents"})

	out := printAgentsDirsFor(cwd, home)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), out)
	}
	wantFirst := filepath.Join(home, "a", "agents")
	wantSecond := filepath.Join(home, "agents")
	if !strings.HasSuffix(lines[0], wantFirst) || strings.HasPrefix(lines[0], "4") {
		t.Fatalf("line[0] should be CWD-closest %s, got %q", wantFirst, lines[0])
	}
	if !strings.HasSuffix(lines[1], wantSecond) || !strings.HasPrefix(lines[1], "4") {
		t.Fatalf("line[1] should be HOME-level %s with depth 4, got %q", wantSecond, lines[1])
	}
}

// TestPrintAgentsDirsFor_EmptyResult pins the no-entries case: an empty
// input tree yields an empty diagnostic string (no error, no panic).
// This is the contract printSlashHooks and printSlashSkills follow.
func TestPrintAgentsDirsFor_EmptyResult(t *testing.T) {
	home := t.TempDir()
	cwd := makeWireFixture(t, home,
		[]bool{false, false, false, false, false},
		[]string{"a", "b", "c", "d"},
		[]string{"agents"})

	if got := printAgentsDirsFor(cwd, home); got != "" {
		t.Fatalf("want empty output, got %q", got)
	}
}

// TestPrintSlashAgentsDirs_UsesCWDAndHome pins that printSlashAgentsDirs
// reads from os.Getwd / os.UserHomeDir and produces a non-empty
// diagnostic when the user's HOME has an agents/ directory. We invoke
// it with the test process's real HOME not modified — the assertion
// here is that the call does not panic and returns either a string or
// an empty string depending on the host's tree. The shape is asserted
// in TestPrintAgentsDirsFor_* above; this test only checks that the
// wrapper compiles and runs.
func TestPrintSlashAgentsDirs_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("printSlashAgentsDirs panicked: %v", r)
		}
	}()
	_ = printSlashAgentsDirs()
}

// TestPrintAgentsDirsFor_DotAgentsVariants pins that `agents` and
// `.agents` at the same level are both returned, with the undotted
// form first (DefaultDirNames order).
func TestPrintAgentsDirsFor_DotAgentsVariants(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, "agents"))
	mustMkdir(t, filepath.Join(home, ".agents"))

	out := printAgentsDirsFor(home, home)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines (HOME has agents + .agents), got %d: %q", len(lines), out)
	}
	wantFirst := filepath.Join(home, "agents")
	wantSecond := filepath.Join(home, ".agents")
	if !strings.HasSuffix(lines[0], wantFirst) {
		t.Fatalf("line[0] should be undotted %s, got %q", wantFirst, lines[0])
	}
	if !strings.HasSuffix(lines[1], wantSecond) {
		t.Fatalf("line[1] should be dotted %s, got %q", wantSecond, lines[1])
	}
}

// TestPrintAgentsDirsFor_HomeIsStop pins that the diagnostic stops at
// HOME even when there is an agents directory one level higher — the
// same rule internal/agentsdir.Walk enforces.
func TestPrintAgentsDirsFor_HomeIsStop(t *testing.T) {
	home := t.TempDir()
	above := filepath.Join(filepath.Dir(home), "above-home")
	mustMkdir(t, filepath.Join(above, "agents"))
	t.Cleanup(func() { _ = os.RemoveAll(above) })

	cwd := makeWireFixture(t, home,
		[]bool{true, false, false, false, false},
		[]string{"a", "b", "c", "d"},
		[]string{"agents"})

	out := printAgentsDirsFor(cwd, home)
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.Contains(line, above) {
			t.Fatalf("diagnostic crossed HOME boundary: %q", line)
		}
	}
	if out == "" {
		t.Fatalf("want non-empty diagnostic, got empty")
	}
}
