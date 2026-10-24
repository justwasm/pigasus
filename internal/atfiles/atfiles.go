// Package atfiles implements the parser for `@<chars>` file-mention
// prefixes that drive the AGY "@ file-mention dropdown". The clone
// learns the AGY 1.3.6 fix "Fixed the @ file-mention dropdown not
// appearing after a newline — typing @<char> at the start of a new
// line should now show the file dropdown just like at the end of a
// line" by recognising the mention token anywhere in the input,
// not just at the end of the first / current line.
//
// The parser is pure: it operates on a rune buffer and returns the
// substring after the most recent `@` that is being typed. A token
// ends when the cursor lands on whitespace, end-of-buffer, or a
// character that cannot appear in a path snippet — the same rule
// AGY's `at_file.go` uses internally (the recovered upstream calls
// into filepath.Glob with the prefix).
//
// The package deliberately contains no UI / runtime dependencies.
// The TUI dropdown lives in cmd/pigasus and re-derives its candidate
// list on every keystroke by calling Find + Glob below.
package atfiles

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Mention is the result of Parse: the prefix substring the user has
// typed after `@` plus the offsets into the buffer the dropdown
// needs to splice the completion back.
//
//	Start   byte offset of the leading '@' (inclusive)
//	End     byte offset one past the last typed char (exclusive)
//	Prefix  the substring between Start+1 and End
//
// Empty Prefix with ok=true means the user typed a bare `@` and the
// dropdown should still open to show the full file catalog.
type Mention struct {
	Prefix string
	Start  int
	End    int
}

// MaxPrefixLen caps the prefix the dropdown accepts. 256 bytes is
// generous (filenames + a couple of path components) and prevents a
// pathological paste from filling the dropdown with every file in
// the workspace while the user pastes their way out of it.
const MaxPrefixLen = 256

// MaxCandidates caps the dropdown's result set. AGY 1.4.3's @
// picker returns at most 50 results regardless of query (verified
// against the live binary: both the empty-query workspace listing
// and a fuzzy query tail out at "↓ 45 more" with a 5-row window).
const MaxCandidates = 50

// MaxWalkEntries bounds the recursive workspace scan. A large
// checkout can contain hundreds of thousands of files; the picker
// only ever displays 50, so there is no value in walking forever.
const MaxWalkEntries = 50000

// Entry is one row in the @ file-mention dropdown. AGY 1.4.3's
// empty-query listing shows directories and files side by side,
// with the directory's relative path carrying a trailing slash
// and a separate "Directory" / "File" type column.
type Entry struct {
	// Path is the absolute path on disk.
	Path string
	// Rel is the slash-separated path relative to the workspace
	// root. Directories end in "/" (e.g. "cmd/pigasus/").
	Rel string
	// IsDir distinguishes the "Directory" and "File" type cells.
	IsDir bool
}

// skipWalkDir reports whether a directory is excluded from the
// picker's workspace scan. AGY 1.4.3's live listing omits .git and
// .crush but keeps hidden project directories such as .agents,
// .github and .antigravity.
func skipWalkDir(name string) bool {
	switch name {
	case ".git", ".crush", "node_modules":
		return true
	}
	return false
}

// Parse walks back from the end of buf looking for a `@<chars>`
// token. Returns ok=true when the cursor is positioned inside or
// immediately after such a token. Whitespace terminates the
// inner token; "@-boundary" (whitespace / start-of-buffer /
// path-separator before `@`) is the outer boundary.
//
// Rules (matching AGY 1.3.6 line 12):
//
//   - the cursor must NOT be parked on whitespace; a buffer
//     ending with `@foo ` or `@foo\n` does NOT trigger the
//     dropdown (the user has already typed past the token).
//   - the mention is `@<chars>` where the latest `@` in
//     [0, cursor) is followed (up to the cursor) by a
//     token-or-space run; chars outside that run (whitespace,
//     punctuation) terminate the mention.
//   - a token-only run between cursor and the latest `@`
//     is enough to qualify — that is the AGY 1.3.6 fix:
//     `@<char>` after a newline in a multi-line prompt shows
//     the file dropdown just like at the end of a line.
//   - mid-word `@` (e.g. the 'b' in `obj@foo`) is rejected:
//     the boundary check refuses a non-token char preceding
//     a token char that precedes `@`.
func Parse(buf string) (Mention, bool) {
	if buf == "" {
		return Mention{}, false
	}
	end := len(buf)
	if c := buf[end-1]; c == ' ' || c == '\t' || c == '\n' || c == '\r' {
		return Mention{}, false
	}
	walkStart := findWalkStart(buf, end)
	at := findValidAt(buf, walkStart, end)
	if at < 0 {
		return Mention{}, false
	}
	prefixEnd := findPrefixEnd(buf, at+1, end)
	prefix := buf[at+1 : prefixEnd]
	if len(prefix) > MaxPrefixLen || strings.ContainsRune(prefix, '\x00') {
		return Mention{}, false
	}
	return Mention{Prefix: prefix, Start: at, End: prefixEnd}, true
}

func findWalkStart(buf string, end int) int {
	walkStart := end
	for i := end - 1; i >= 0; i-- {
		c := buf[i]
		if !isTokenChar(c) && c != '@' && c != ' ' && c != '\t' {
			break
		}
		walkStart = i
	}
	return walkStart
}

func findValidAt(buf string, walkStart, end int) int {
	for i := walkStart; i < end; i++ {
		if buf[i] != '@' {
			continue
		}
		if i == 0 || !isTokenChar(buf[i-1]) {
			return i
		}
	}
	return -1
}

func findPrefixEnd(buf string, start, end int) int {
	for j := start; j < end; j++ {
		c := buf[j]
		if !isTokenChar(c) && c != '/' && c != '@' && c != ' ' {
			return j
		}
	}
	return end
}

// isTokenChar reports whether c can appear in a file-path token.
// Letters / digits / `_` / `-` / `.` cover the AGY upstream's
// allowed set within a single path component; `/` is included too
// so "@src/main.go" parses as a single mention without the parser
// stopping at the slash.
func isTokenChar(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return true
	case c >= 'A' && c <= 'Z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '_' || c == '-' || c == '.' || c == '/':
		return true
	}
	return false
}

// List returns every directory and file under root, recursively.
// The result is not ranked; callers pass it through FindEntries
// (or filter it themselves) to obtain a stable @ picker order.
//
// Hidden project entries (.agents, .github, .antigravity) are
// included; only VCS/runtime noise (.git, .crush, node_modules)
// is skipped. Symlinked directories are not followed.
func List(root string) ([]Entry, error) {
	if root == "" {
		return nil, nil
	}
	var entries []Entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A workspace can contain unreadable subdirectories;
			// skip them rather than aborting the whole listing.
			if path == root {
				return err
			}
			return nil
		}
		if path == root {
			return nil
		}
		if d.IsDir() && skipWalkDir(d.Name()) {
			return filepath.SkipDir
		}
		if !d.IsDir() && d.Name() == ".DS_Store" {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			rel += "/"
		}
		entries = append(entries, Entry{Path: path, Rel: rel, IsDir: d.IsDir()})
		if len(entries) >= MaxWalkEntries {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// FindEntries ranks the @ picker candidates for a prefix. The
// three modes mirror the live 1.4.3 picker:
//
//   - prefix == ""        → workspace listing; top-level entries
//     first, then nested paths by depth. The live picker's exact
//     order comes from its language-server file index, so this is
//     a deterministic approximation (see the cycle report).
//   - prefix ends in "/"  → direct children of that directory.
//   - otherwise           → case-insensitive substring/fuzzy
//     search across the whole relative path.
//
// At most MaxCandidates rows are returned. No matches yields an
// empty slice with a nil error.
func FindEntries(root, prefix string) ([]Entry, error) {
	all, err := List(root)
	if err != nil {
		return nil, err
	}
	return FilterEntries(all, prefix), nil
}

// FilterEntries applies the picker's matching rules to an already
// scanned entry list. Exposed separately so the dropdown can cache
// List's result and re-filter on every keystroke.
func FilterEntries(all []Entry, prefix string) []Entry {
	prefix = strings.TrimPrefix(filepath.ToSlash(prefix), "./")
	lower := strings.ToLower(prefix)
	dirMode := strings.HasSuffix(prefix, "/")

	var matches []Entry
	for _, e := range all {
		relLower := strings.ToLower(e.Rel)
		switch {
		case prefix == "":
			matches = append(matches, e)
		case dirMode:
			// Direct children only: the relative path must start
			// with the directory prefix and contain no further
			// slash after it.
			if !strings.HasPrefix(relLower, lower) || len(e.Rel) <= len(prefix) {
				continue
			}
			rest := e.Rel[len(prefix):]
			if strings.Contains(rest, "/") {
				// A directory whose own trailing slash is the
				// final character is a direct child; anything
				// with another separator is one level deeper.
				if !(e.IsDir && strings.Count(rest, "/") == 1 && strings.HasSuffix(rest, "/")) {
					continue
				}
			}
			matches = append(matches, e)
		case strings.Contains(relLower, lower):
			matches = append(matches, e)
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return entryLess(matches[i], matches[j], lower, prefix == "", dirMode)
	})
	if len(matches) > MaxCandidates {
		matches = matches[:MaxCandidates]
	}
	return matches
}

// entryLess is the picker's deterministic ordering. It is a close
// structural match to 1.4.3, not an exact reconstruction of its
// language-server index order:
//
//   - root listing: shallower paths first, directories before
//     files at the same depth, then case-insensitive name.
//   - directory browse: directories before files, then name.
//   - query: basename-prefix hits first, then earlier substring
//     position, then fewer path components, then name.
func entryLess(a, b Entry, query string, rootMode, dirMode bool) bool {
	// A directory's own trailing slash does not add a level: both
	// "cmd/" and ".gitignore" are top-level entries.
	depth := func(e Entry) int {
		return strings.Count(strings.TrimSuffix(e.Rel, "/"), "/")
	}
	ad, bd := depth(a), depth(b)
	if rootMode {
		if ad != bd {
			return ad < bd
		}
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return strings.ToLower(a.Rel) < strings.ToLower(b.Rel)
	}
	if dirMode {
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return strings.ToLower(a.Rel) < strings.ToLower(b.Rel)
	}
	aBase := strings.HasPrefix(strings.ToLower(filepath.Base(strings.TrimSuffix(a.Rel, "/"))), query)
	bBase := strings.HasPrefix(strings.ToLower(filepath.Base(strings.TrimSuffix(b.Rel, "/"))), query)
	if aBase != bBase {
		return aBase
	}
	ai := strings.Index(strings.ToLower(a.Rel), query)
	bi := strings.Index(strings.ToLower(b.Rel), query)
	if ai != bi {
		return ai < bi
	}
	if ad != bd {
		return ad < bd
	}
	return strings.ToLower(a.Rel) < strings.ToLower(b.Rel)
}
