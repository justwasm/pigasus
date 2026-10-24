// Package codesearchaction holds the *feedback* path of /codesearch: the
// one-shot workspace search that runs when the command is typed, before
// the interactive panel takes over.
//
// The interactive panel's runtime lives in internal/codesearch; this
// package holds the simpler regexp2-based walker the feedback path uses.
// The two engines differ on purpose: the panel's engine follows
// ripgrep's pattern language, while the feedback path uses regexp2 so
// the preview matches ripgrep's PCRE feature set (named groups,
// lookarounds, atomic groups).
package codesearchaction

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dlclark/regexp2"
)

// MaxHits caps how many matches the feedback path renders. Matches
// beyond the cap are counted but not stored, so the recovered
// "  | %d more" tail can report how many were withheld.
const MaxHits = 200

// SkipDirs are directories the feedback path never descends into. `.git`,
// `.hg` and `.jj` hold the VCS's own bookkeeping; the rest are dependency
// trees that are noise for a workspace search.
var SkipDirs = map[string]bool{
	".git":         true,
	".hg":          true,
	".jj":          true,
	"node_modules": true,
	".venv":        true,
	"vendor":       true,
}

// Usage is the recovered usage line.
const Usage = "Usage: /codesearch [-F|--literal] <query>"

// Matcher builds the pattern for the feedback path. Recovered usage
// `Usage: /codesearch [-F|--literal] <query>` means the default is a pattern and
// `-F` / `--literal` selects a fixed-string match (ripgrep's convention).
//
// Cycle package-simplification cycle 3: switched the in-process
// preview from stdlib `regexp` (RE2) to `regexp2` (.NET-style regex
// engine) so the preview matches ripgrep's pattern language: named
// groups, lookarounds (`(?=...)`, `(?<=...)`), atomic groups
// `(?>...)`, and the rest of the PCRE feature set ripgrep ships.
// The preview was the only place in the clone where the in-process
// search engine disagreed with the actual `rg` invocation.
//
// Literal mode still wraps the query in regexp2's equivalent of
// `regexp.QuoteMeta` — `regexp2.Escape` — so the `-F` contract is
// preserved.
func Matcher(query string, literal bool) (*regexp2.Regexp, error) {
	if literal {
		return regexp2.Compile(regexp2.Escape(query), regexp2.None)
	}
	return regexp2.Compile(query, regexp2.None)
}

// searchFileLines matches lines in data against re and appends hits or
// increments extra.
func searchFileLines(rel string, data []byte, re *regexp2.Regexp, hits *[]string, extra *int) {
	for i, line := range strings.Split(string(data), "\n") {
		m, err := re.FindStringMatch(line)
		if err != nil || m == nil {
			continue
		}
		// m.Index is [start, end) byte offsets into line;
		// /codesearch reports the start column (1-based,
		// matching the ripgrep "n:line:col: text" format).
		if len(*hits) >= MaxHits {
			*extra++
			continue
		}
		*hits = append(*hits, fmt.Sprintf("%s:%d:%d: %s",
			rel, i+1, m.Index+1, strings.TrimRight(line, "\r")))
	}
}

// SearchWorkspace walks dir and returns `relpath:line:col: text` for the
// first MaxHits lines matching re, plus the number of further matches
// that were counted but withheld.
//
// Unreadable entries are skipped rather than failing the search, and files
// larger than 1 MiB or carrying a NUL in their first 8 KiB are treated as binary
// and skipped.
//
// Cycle package-simplification cycle 3: the matcher is now
// `*regexp2.Regexp`. We use `FindStringMatch` instead of
// `FindStringIndex` (regexp2 doesn't expose the index-slice form);
// the match's `.Index`/`[a:b]` tuple gives us the start column. A
// nil error means "the pattern compiled cleanly but did not match";
// a non-nil error is the regexp2 engine reporting a runtime issue
// (very rare — only happens for catastrophic backtracking on
// non-greedy quantifiers); we treat both as "no match" so the
// search still produces a result set.
func SearchWorkspace(dir string, re *regexp2.Regexp) (hits []string, extra int) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if SkipDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 1<<20 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		head := data
		if len(head) > 8<<10 {
			head = head[:8<<10]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			rel = path
		}
		searchFileLines(rel, data, re, &hits, &extra)
		return nil
	})
	return hits, extra
}

// ResultWords are the singular/plural pair the "Found N results" line
// uses.
var ResultWords = [2]string{"result", "results"}

// CountLabel renders n with the given singular/plural pair.
func CountLabel(n int, word [2]string) string {
	if n == 1 {
		return "1 " + word[0]
	}
	return fmt.Sprintf("%d %s", n, word[1])
}

// Feedback runs the feedback path of /codesearch: a workspace-wide
// pattern search.
//
// Recovered: `Usage: /codesearch [-F|--literal] <query>`, the description
// "Search code in the workspace (usage: /codesearch <query>)", `No results` for
// an empty hit set, and `  | %d more` for the truncation tail.
//
// The reference searches through the language server's `SearchCode` RPC and
// shows the hits in an interactive picker (`+ Match all (no filter)`,
// `Keep typing to filter files`). The clone has no language server, so it walks
// the workspace itself and appends `path:line:col: text` lines to the
// transcript: the *result line format* is 【推测】, the messages are not.
//
// `Searching for %q...` is not emitted — like `Loading diff`, it is a transient
// progress label shown before the content.
func Feedback(args, workspace string) string {
	literal := false
	fields := strings.Fields(args)
	for len(fields) > 0 && strings.HasPrefix(fields[0], "-") && fields[0] != "--" {
		switch fields[0] {
		case "-F", "--literal":
			literal = true
		default:
			return Usage
		}
		fields = fields[1:]
	}
	if len(fields) > 0 && fields[0] == "--" {
		fields = fields[1:]
	}
	query := strings.TrimSpace(strings.Join(fields, " "))
	if query == "" {
		return Usage
	}
	if workspace == "" {
		return "no workspace directory"
	}
	re, err := Matcher(query, literal)
	if err != nil {
		return fmt.Sprintf("invalid pattern: %v", err)
	}
	hits, extra := SearchWorkspace(workspace, re)
	if len(hits) == 0 && extra == 0 {
		return "No results"
	}
	var b strings.Builder
	// The recovered panel header is `Found ` plus the count: the string table
	// carries both `Found %d results` and `1 result`, and the changelog records
	// "Fixed counts in tool steps and the `/codesearch` panel, which now read
	// \"Found 1 result\", \"1 match\", \"1 file\", and \"Wrote 1 line\" instead of
	// \"1 results\"".
	b.WriteString("Found " + CountLabel(len(hits)+extra, ResultWords))
	for _, h := range hits {
		b.WriteString("\n")
		b.WriteString(h)
	}
	if extra > 0 {
		fmt.Fprintf(&b, "\n  | %d more", extra)
	}
	return b.String()
}
