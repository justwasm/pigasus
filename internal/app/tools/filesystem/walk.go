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
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"
)

// ErrStopWalk ends a walk early once a result limit is reached.
var ErrStopWalk = errors.New("stop walk")

// skipDirs are pruned below a search root, never when explicitly named as
// the root. Do not add generated-directory names here indiscriminately: users
// may intentionally search those trees.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".venv": true, "venv": true,
	"__pycache__": true, ".idea": true, ".vscode": true,
}

// WalkFiles calls fn for every regular file under root. rel is root-relative
// (or the base name when root is itself a file). The legacy entry point keeps
// its signature; cancellable callers should use WalkFilesContext.
func WalkFiles(root string, fn func(path, rel string) error) error {
	return WalkFilesContext(context.Background(), root, fn)
}

// WalkFilesContext walks without descending into symlinks. A root that names a
// regular file is checked once to preserve the explicit-file behavior; directory
// entries use WalkDir type bits rather than a stat per file. Root errors are
// returned; unreadable descendants are skipped.
func WalkFilesContext(ctx context.Context, root string, fn func(path, rel string) error) error {
	base := filepath.Clean(root)
	if err := ctx.Err(); err != nil {
		return err
	}
	// Preserve the legacy explicit-file-root behavior (including a symlink
	// to a regular file); WalkDir still does not descend into a symlink root.
	if info, err := os.Stat(base); err != nil {
		return err
	} else if !info.IsDir() {
		if info.Mode().IsRegular() {
			return fn(base, filepath.Base(base))
		}
		return nil
	}
	err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			if path == base {
				return walkErr
			}
			return nil
		}
		if entry.IsDir() {
			if path != base && skipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		rel := filepath.Base(base)
		if path != base {
			var err error
			rel, err = filepath.Rel(base, path)
			if err != nil {
				return nil
			}
		}
		return fn(path, rel)
	})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return err
}

// LooksTextual reports whether data is plausibly text: no NUL byte and valid
// UTF-8 in the leading window. Include a rune that straddles the window edge
// when the caller supplied enough data, rather than rejecting valid UTF-8.
func LooksTextual(data []byte) bool {
	end := len(data)
	if end > 8192 {
		end = 8192
		for end < len(data) && end < 8192+utf8.UTFMax-1 && !utf8.RuneStart(data[end]) {
			end++
		}
	}
	window := data[:end]
	return bytes.IndexByte(window, 0) < 0 && utf8.Valid(window)
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
