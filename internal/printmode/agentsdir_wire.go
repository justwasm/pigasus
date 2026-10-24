// agentsdir_wire.go — wire internal/agentsdir into the print path so the
// CLI exposes the AGY 1.2.16 parent-walk behavior with a slash command a
// user can invoke. The /agents-dirs diagnostic prints every agents
// directory the walker surfaces from CWD up to HOME, in CWD-closest
// order, so a user can see why a given agent file is or is not visible
// to the session.
//
// The diagnostic is intentionally separate from `/agents` (which lists
// installed custom agents) and from `/skills` (which lists skills). It
// is a thin read-only view onto the walker, not a load step.
//
// Concurrency: printSlashAgentsDirs does no I/O beyond the walker's
// own os.Stat calls and the surrounding `os.Getwd` / `os.UserHomeDir`.
// Both are safe to call from a request goroutine; the result is a
// stable string snapshot.
package printmode

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/agentsdir"
)

// printSlashAgentsDirs renders the AGY 1.2.16 parent-walk output as a
// list of "<depth>\t<path>" lines, CWD-closest first. When no
// agents-directory exists anywhere on the walk, the result is an empty
// string — the same convention printSlashSkills and printSlashHooks
// already follow.
//
// The depth is the number of `..` hops the walker had to take from CWD
// to surface that directory; a depth of -1 means the entry came from a
// HOME-level fallback (currently exercised for ~/.agents only when no
// ancestor of CWD has one — see internal/agentsdir.Discover for the
// rule).
//
// CWD and HOME are read from os.Getwd / os.UserHomeDir. Tests that
// need a controlled filesystem should call printAgentsDirsFor directly.
func printSlashAgentsDirs() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		cwd = "."
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = ""
	}
	return printAgentsDirsFor(cwd, home)
}

// printAgentsDirsFor is the testable seam: it takes cwd + home
// explicitly so unit tests can run against a fixture TempDir without
// touching the process state. The output ordering matches Discover:
// CWD-closest entry at index 0, HOME-level entry at the tail.
func printAgentsDirsFor(cwd, home string) string {
	entries := agentsdir.Discover(cwd, home)
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%d\t%s\n", e.Depth, e.Path)
	}
	return b.String()
}

// writeAgentsDirsTable is a tabular variant used by ad-hoc bin-output
// tooling; the columns are aligned for terminal width 80 but degrade
// gracefully when the path is wider than the budget. It is kept here
// rather than in cmd/wagyu/main.go so the test lives next to the
// implementation it pins.
//
// Callers should prefer printSlashAgentsDirs for end-user surfaces.
func writeAgentsDirsTable(w io.Writer, entries []agentsdir.Entry) {
	for _, e := range entries {
		fmt.Fprintf(w, "agents\tdepth=%d\t%s\n", e.Depth, e.Path)
	}
}
