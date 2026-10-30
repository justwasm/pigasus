package codesearch

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// HasUpper reports whether s contains any uppercase letter.
func HasUpper(s string) bool {
	return strings.IndexFunc(s, unicode.IsUpper) >= 0
}

// CompileMatcher compiles query into a regexp.
func CompileMatcher(query string, literal bool) (*regexp.Regexp, error) {
	pat := query
	if literal {
		pat = regexp.QuoteMeta(pat)
	}
	if !HasUpper(query) {
		pat = "(?i)" + pat
	}
	return regexp.Compile(pat)
}

// Scan walks root looking for lines matching re.
func Scan(root string, re *regexp.Regexp, maxHits int) []Hit {
	if root == "" {
		return nil
	}
	var hits []Hit
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path != root && (SkipDirs[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") {
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
			return nil // binary
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		hits = AppendFileHits(hits, rel, string(data), re, maxHits)
		if len(hits) >= maxHits {
			hits = hits[:maxHits]
			return fs.SkipAll
		}
		return nil
	})
	return hits
}
