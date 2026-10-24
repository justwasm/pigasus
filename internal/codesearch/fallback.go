// fallback.go — local directory walk and regex search fallback.
package codesearch

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"
)

// globMatchAny reports whether path matches any of the patterns. The
// two callers have opposite defaults:
//
//   - include: an empty list means "no inclusion filter, allow all".
//   - exclude: an empty list means "no exclusions, allow all".
//
// Both return true when len(patterns)==0 because "no filter means
// nothing rejects". Use want=false for include / want=true for exclude
// to invert the sense without two callers.
func globMatchAny(path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, p := range patterns {
		if p == "" {
			continue
		}
		// doublestar.Match operates on '/' separators.
		if ok, _ := doublestar.Match(p, path); ok {
			return true
		}
		// Try matching just the basename too — rg's -g matches
		// against the full path; users tend to think in basenames.
		base := filepath.Base(path)
		if ok, _ := doublestar.Match(p, base); ok {
			return true
		}
	}
	return false
}

// hasUpper is the smart-case predicate. Lowercase-only patterns are
// case-insensitive; any uppercase rune forces a case-sensitive search.
func hasUpper(s string) bool {
	for _, r := range s {
		if unicode.IsUpper(r) {
			return true
		}
	}
	return false
}

// BytesIndexByte is bytes.IndexByte without importing the package.
func BytesIndexByte(b []byte, c byte) int {
	for i := range b {
		if b[i] == c {
			return i
		}
	}
	return -1
}

func compileSearchRegex(query string, literal bool) (*regexp.Regexp, error) {
	pat := query
	if literal {
		pat = regexp.QuoteMeta(pat)
	}
	// Smart-case: lowercase-only patterns are case-insensitive.
	if !hasUpper(query) {
		pat = "(?i)" + pat
	}
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil, fmt.Errorf("codesearch: %w", err)
	}
	return re, nil
}

func shouldSkipWalkDir(root, path string, d fs.DirEntry) (bool, error) {
	if d.IsDir() {
		name := d.Name()
		if path != root && (SkipDirs[name] || strings.HasPrefix(name, ".")) {
			return true, fs.SkipDir
		}
		return true, nil
	}
	if strings.HasPrefix(d.Name(), ".") {
		return true, nil
	}
	return false, nil
}

func filterPath(root, path string, include, exclude []string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	rel = filepath.ToSlash(rel)
	// Include filter: if the user named patterns, only let matched
	// files through. Empty include list = no filter.
	if len(include) > 0 && !globMatchAny(rel, include) {
		return "", false
	}
	// Exclude filter: if the user named patterns, skip matched
	// files. Empty exclude list = no exclusions.
	if len(exclude) > 0 && globMatchAny(rel, exclude) {
		return "", false
	}
	return rel, true
}

func readTextLines(path string, d fs.DirEntry) ([]string, bool) {
	info, err := d.Info()
	if err != nil || info.Size() > 1<<20 {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	head := data
	if len(head) > 8<<10 {
		head = head[:8<<10]
	}
	if BytesIndexByte(head, 0) >= 0 {
		return nil, false // binary
	}
	content := string(data)
	lines := strings.Split(content, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines, true
}

type fallbackSearcher struct {
	ctx       context.Context
	s         *Search
	re        *regexp.Regexp
	limit     int
	overflow  int
	seenFiles map[string]bool
}

func (fsState *fallbackSearcher) searchLines(rel string, lines []string) error {
	for i, line := range lines {
		if err := fsState.ctx.Err(); err != nil {
			return err
		}
		line = strings.TrimRight(line, "\r")
		loc := fsState.re.FindStringIndex(line)
		if loc == nil {
			continue
		}
		if countSent(fsState.s) >= fsState.limit {
			fsState.overflow++
			continue
		}
		col := utf8RuneCount(line[:loc[0]]) + 1
		if !fsState.seenFiles[rel] {
			fsState.seenFiles[rel] = true
			fsState.s.FilesHit++
		}
		if err := send(fsState.ctx, fsState.s, Match{
			File: rel,
			Line: i + 1,
			Col:  col,
			Text: line,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (fsState *fallbackSearcher) walkEntry(path string, d fs.DirEntry, walkErr error) error {
	if walkErr != nil {
		return nil
	}
	if err := fsState.ctx.Err(); err != nil {
		return err
	}
	if skip, err := shouldSkipWalkDir(fsState.s.Root, path, d); skip {
		return err
	}
	rel, ok := filterPath(fsState.s.Root, path, fsState.s.Include, fsState.s.Exclude)
	if !ok {
		return nil
	}
	lines, ok := readTextLines(path, d)
	if !ok {
		return nil
	}
	return fsState.searchLines(rel, lines)
}

// runFallback walks the workspace, applies the include/exclude globs
// (bmatcuk/doublestar/v4 — supports `**`, brace expansion, and
// character classes, matching ripgrep's `-g` semantics for `*.go` /
// `*_test.go` and the multi-segment `**/foo/*.go` case), and pushes
// matches into the channel.
func (r *Runner) runFallback(ctx context.Context, s *Search) error {
	if s.Root == "" && r.Workdir != "" {
		s.Root = r.Workdir
	}
	if s.Root == "" {
		s.closeMatches()
		return errors.New("codesearch: no root directory")
	}
	re, err := compileSearchRegex(s.Query, s.Literal)
	if err != nil {
		return err
	}
	limit := s.Limit
	if limit <= 0 {
		limit = MaxHits
	}
	fsState := &fallbackSearcher{
		ctx:       ctx,
		s:         s,
		re:        re,
		limit:     limit,
		seenFiles: make(map[string]bool),
	}
	err = filepath.WalkDir(s.Root, fsState.walkEntry)
	if fsState.overflow > 0 {
		// Mirror the reference: keep counting but don't stream past
		// the cap. The cap is internal; the overflow counter lets the
		// panel show "| %d more" if it wants to.
		s.Limit = countSent(s) + fsState.overflow
	}
	return err
}
