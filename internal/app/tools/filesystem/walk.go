// walk.go — the workspace walker and the text/binary heuristic the search
// tools share.
//
// Cycle 379: these lived in internal/app/tools/support.go, a file that
// held five unrelated things. The walker is a *file* concern — it knows
// which directories are never descended into — so it belongs with the
// file tools, not with the shell.

package filesystem

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// ErrStopWalk ends a walk early once a result limit is reached.
var ErrStopWalk = errors.New("stop walk")

// skipDirs are never descended into by the search tools.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true,
	"__pycache__": true, ".idea": true, ".vscode": true,
}

// WalkFiles calls fn for every regular file under root. rel is root-relative
// (or the base name when root is itself a file).
func WalkFiles(root string, fn func(path, rel string) error) error {
	base := filepath.Clean(root)
	info, err := os.Stat(base)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fn(base, filepath.Base(base))
	}
	return filepath.Walk(base, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if fi.IsDir() {
			if path == base {
				return nil
			}
			if skipDirs[fi.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(base, path)
		if relErr != nil {
			return nil
		}
		return fn(path, rel)
	})
}

// LooksTextual reports whether data is plausibly text: no NUL byte and valid
// UTF-8 in the leading window.
func LooksTextual(data []byte) bool {
	if len(data) == 0 {
		return true
	}
	window := data
	if len(window) > 8192 {
		window = window[:8192]
	}
	if bytes.IndexByte(window, 0) >= 0 {
		return false
	}
	return utf8.Valid(window)
}

// TrimTo shortens s to at most n bytes without splitting a rune.
func TrimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\u2026"
}
