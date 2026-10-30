// changes.go — the git working-copy change model the /diff surfaces share.
//
// The All Changes tab lists the working copy's modified files with their
// line counts. Recovering that list needs three git queries:
//
//	git status --porcelain -uall   → which files changed, and which are untracked
//	git diff --numstat              → added/deleted counts for unstaged changes
//	git diff --cached --numstat     → added/deleted counts for staged changes
//
// The counts are summed across the two numstat passes, so a file with staged
// and unstaged edits reports both.
//
// This model is pure: it shells out to git (through RunGit, so the pager,
// editor and locale contract holds) and reads the working tree's file
// lengths. It never touches the TUI, so it lives here rather than in
// cmd/wagyu.

package action

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Change is one working-copy file with its line counts.
type Change struct {
	// Path is the file's path relative to the repository root.
	Path string
	// Added is the number of lines the working copy adds.
	Added int
	// Deleted is the number of lines the working copy removes.
	Deleted int
	// Untracked is true for a file git does not track yet.
	Untracked bool
}

// ParseGitStatusPorcelain reads `git status --porcelain -uall` output and
// returns the tracked files (keyed by path, with no counts yet) and the
// untracked files (with their counts already measured).
//
// The porcelain v1 format is two status characters, a space, then the path:
//
//	?? path      untracked
//	 M path      modified in the work tree
//	M  path      modified in the index
//	MM path      modified in both
//
// Only the codes REF's list cares about are recognised: M (modified),
// A (added), D (deleted), R (renamed), C (copied) and T (type changed).
func ParseGitStatusPorcelain(dir, status string) (map[string]Change, []Change) {
	known := map[string]Change{}
	var untracked []Change
	for _, line := range strings.Split(status, "\n") {
		if len(line) < 4 {
			continue
		}
		code, path := line[:2], line[3:]
		if code == "??" {
			ut := Change{Path: path, Added: CountFileLines(dir, path), Untracked: true}
			untracked = append(untracked, ut)
			continue
		}
		if !strings.ContainsAny(code, "MADRCUT") {
			continue
		}
		known[path] = Change{Path: path}
	}
	return known, untracked
}

// ApplyGitNumstat runs `git <args...> --numstat` in dir and folds the
// reported added/deleted counts into the known map.
//
// The numstat format is three tab-separated fields: added, deleted, path.
// A binary file reports "-" for the counts; diffAtoi turns that into 0.
func ApplyGitNumstat(dir string, known map[string]Change, args []string) {
	numstat, err := RunGit(dir, args...)
	if err != nil {
		return
	}
	for _, line := range strings.Split(numstat, "\n") {
		fields := strings.SplitN(line, "\t", 3)
		if len(fields) != 3 {
			continue
		}
		c, exists := known[fields[2]]
		if !exists {
			continue
		}
		c.Added += Atoi(fields[0])
		c.Deleted += Atoi(fields[1])
		known[fields[2]] = c
	}
}

// GitChanges returns the tracked then untracked changes of the git working
// copy rooted (anywhere) at dir.
//
// Tracked counts come from `git diff --numstat` plus
// `git diff --cached --numstat`; untracked files are counted directly, the
// way `git diff --numstat` reports a new file.
//
// It returns ok=false when git is unavailable or dir is not a repo.
func GitChanges(dir string) (tracked, untracked []Change, ok bool) {
	status, err := RunGit(dir, "status", "--porcelain", "-uall")
	if err != nil {
		return nil, nil, false
	}
	known, untracked := ParseGitStatusPorcelain(dir, status)
	if len(known) == 0 {
		SortChanges(untracked)
		return nil, untracked, true
	}
	ApplyGitNumstat(dir, known, []string{"diff", "--numstat"})
	ApplyGitNumstat(dir, known, []string{"diff", "--cached", "--numstat"})
	for _, c := range known {
		tracked = append(tracked, c)
	}
	SortChanges(tracked)
	SortChanges(untracked)
	return tracked, untracked, true
}

// CountFileLines counts the lines git would report for a brand-new file:
// newline count, plus one when the file does not end in a newline, and zero
// for an empty file.
func CountFileLines(dir, path string) int {
	data, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil || len(data) == 0 {
		return 0
	}
	n := strings.Count(string(data), "\n")
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}

// Atoi parses a numstat count. A "-" (a binary file) and any other
// unparseable value count as zero.
func Atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// SortChanges sorts the changes by path.
func SortChanges(cs []Change) {
	sort.Slice(cs, func(i, j int) bool { return cs[i].Path < cs[j].Path })
}
