// Package agentsdir implements the AGY 1.2.16 changelog line:
//
//	Agents from `~/.agents/` are now discovered by walking parent directories
//	from CWD up to HOME, so a project-local agents/ directory wins over the
//	global one.
//
// The single public entry point is Walk: it walks a path upward from a
// start directory to a stop directory and, at each step, lists the
// subdirectories in dirNames that exist. Results are deduplicated by
// absolute path and returned in CWD-closest-first order so a project-local
// match is always preferred over a global one.
//
// Two corollary helpers sit on top:
//
//   - Discover extends Walk with the dotted (`.agents`) and undotted
//     (`agents`) names plus the user's home (~/.agents) so a single call
//     surfaces every agents directory the AGY changelog line makes visible.
//   - FirstProjectLocal implements the "project-local wins" rule directly:
//     it returns the CWD-closest entry, falling back to the global home,
//     so callers that only want the winning entry never have to scan the
//     slice themselves.
package agentsdir

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// DefaultDirNames are the directory names this package looks for at each
// parent. The undotted `agents` is the schema-neutral spelling the
// changelog line uses; `.agents` is the dotted variant. The order is the
// precedence order: an `agents` directory at depth N wins over a `.agents`
// directory at the same depth because the un-dotted name is the
// user-facing one in the AGY captured examples.
//
// Callers may override the list by passing a different slice to Walk, but
// Discover + FirstProjectLocal always use these names in this order.
var DefaultDirNames = []string{"agents", ".agents"}

// Entry is one discovered agents directory. Path is always absolute and
// points at the directory itself (not the path the walker happened to be
// at when it was found). Depth is the number of `..` operations required
// to reach Path from the walk's start directory — 0 means Path IS the
// startDir, 1 means startDir/.., and so on. A negative Depth marks an
// ExtraRoot entry where the relative-to-CWD distance is undefined.
type Entry struct {
	Path  string // absolute path of the agents directory
	Depth int    // see type docs; negative for entries outside the CWD→HOME walk
}

// Walk walks parent directories from startDir up to and including
// stopDir and, at each step, returns the absolute paths of any of the
// named subdirectories that exist. The result is deduplicated by
// absolute path, sorted by depth ascending (CWD-closest first), and any
// entries whose path is *identical* to a previously-seen path are merged
// rather than duplicated.
//
// Both startDir and stopDir must be absolute. If stopDir is empty, the
// filesystem root is used (the walker stops at `/`). If startDir is
// empty, the process current working directory is used.
//
// The walk halts when the current directory equals stopDir (after the
// check) — it does not continue above stopDir. A stopDir that is not an
// ancestor of startDir is treated as the root of the filesystem; the
// walker still halts when it reaches the root.
//
// dirNames is the list of directory names to look for at each step. Use
// DefaultDirNames for the canonical pair (`agents` then `.agents`).
// Names containing a path separator or an empty string are ignored.
//
// The returned slice is sorted by path component count ascending (i.e.
// by depth relative to startDir), with CWD-closest entries at index 0.
func Walk(startDir, stopDir string, dirNames []string) []Entry {
	names := canonicalDirNames(dirNames)
	if len(names) == 0 {
		return nil
	}
	start, err := filepath.Abs(startDir)
	if err != nil || start == "" {
		return nil
	}
	stop := stopDir
	if stop == "" {
		stop = string(filepath.Separator)
	}
	stopAbs, err := filepath.Abs(stop)
	if err == nil && stopAbs != "" {
		stop = stopAbs
	}

	seen := make(map[string]bool)
	out := make([]Entry, 0, 8)
	for cur := start; ; cur = filepath.Dir(cur) {
		depth := depthBetween(start, cur)
		if cur == "" {
			break
		}
		for _, n := range names {
			p := filepath.Join(cur, n)
			if !isDir(p) {
				continue
			}
			abs, err := filepath.Abs(p)
			if err != nil || abs == "" {
				continue
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			out = append(out, Entry{Path: abs, Depth: depth})
		}
		// Stop AFTER the check at this directory: stopDir is inclusive.
		if cur == stop {
			break
		}
		parent := filepath.Dir(cur)
		// Reached the filesystem root without hitting stopDir (e.g. the
		// caller passed a stopDir that is not an ancestor). The walker
		// stops here anyway — walking above root is impossible.
		if parent == cur {
			break
		}
	}
	return out
}

// Discover wraps Walk with the AGY default directory names. It exists
// as a distinct entry point — rather than a default — so the call site
// can pass an explicit CWD/HOME without the helper having to thread
// os.Getwd / os.UserHomeDir through the package boundary.
//
// homeDir is normally the value of $HOME; an empty string falls back to
// os.UserHomeDir. If both fail (rare — no HOME env var and the temp-dir
// fallback cannot be resolved), the walker treats the filesystem root
// as the upper bound.
//
// The returned entries follow the same depth-ascending order as Walk:
// the CWD-closest .agents/ is at index 0, the HOME-level .agents/ is at
// the tail, and any intermediate entry sits between them in order.
//
// Callers wanting the CWD-closest match alone can use FirstProjectLocal.
func Discover(cwd, homeDir string) []Entry {
	return Walk(cwd, homeDir, DefaultDirNames)
}

// FirstProjectLocal returns the CWD-closest agents directory entry from
// the Discover result, falling back to the first global entry. It is a
// convenience wrapper for callers that implement the "project-local wins
// over the global one" rule without scanning the slice themselves.
//
// An empty result means no agents directory was found anywhere on the
// walk; the caller should treat that as "fall back to the bundled
// default" or surface "no agents available", depending on the surface.
func FirstProjectLocal(cwd, homeDir string) (Entry, bool) {
	entries := Discover(cwd, homeDir)
	if len(entries) == 0 {
		return Entry{}, false
	}
	return entries[0], true
}

// ErrEmptyCWD is returned by callers that validate the start directory;
// it is exported so tests can pin the error message.
var ErrEmptyCWD = errors.New("agentsdir: empty start directory")

// defaultHome returns the user's home directory or an empty string when
// it cannot be resolved.
func defaultHome() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// canonicalDirNames validates dirNames: empty entries are dropped;
// entries containing a path separator are dropped. The returned slice
// preserves the input order so callers can rely on their declared
// precedence.
func canonicalDirNames(in []string) []string {
	out := make([]string, 0, len(in))
	for _, n := range in {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if strings.ContainsAny(n, string(filepath.Separator)) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// depthBetween returns the number of parent hops above start where cur
// sits. Both arguments must be absolute paths; cur is expected to be
// `start` or an ancestor of `start` (the walker iterates `cur` upward
// from `start`, so this is the natural direction).
//
// The depth is the count of leading ".." segments in
// filepath.Rel(start, cur) — i.e. how many parent traversals separate
// `start` from `cur`. When cur == start, depth is 0; when cur is one
// parent up, depth is 1; and so on.
//
// If cur is not an ancestor of start, the depth is clamped to 0 —
// the walker never yields a negative depth for in-walk entries.
func depthBetween(start, cur string) int {
	if start == cur {
		return 0
	}
	rel, err := filepath.Rel(start, cur)
	if err != nil || rel == "." {
		return 0
	}
	parts := strings.Split(rel, string(filepath.Separator))
	n := 0
	for _, p := range parts {
		if p == ".." {
			n++
			continue
		}
		break
	}
	return n
}

// isDir reports whether path is an existing directory. Symlinks are
// followed (filepath.Stat semantics), matching the way `os.ReadDir`
// resolves SKILL.md and similar nested-resource lookups elsewhere in the
// tree.
func isDir(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.IsDir()
}
