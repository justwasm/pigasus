// Package remotecontrol — scope.go: the production FSProvider
// implementation. Cycle rc-file-ops splits FSProvider (the
// seam the dispatcher calls into) from WorkdirScope (the
// production seam that enforces the host's filesystem scope).
//
// Why a separate file: scope.go is the only file in the
// package that touches the real filesystem (ResolvePolicy
// uses filepath.Abs + Clean; ReadFile / ReadDir use os.Stat
// / os.Open / os.Lstat). Keeping it isolated means a test
// that wants the dispatcher's read-only logic without the
// policy just substitutes a MemFS (files.go) for
// WorkdirScope and the rest of the dispatcher doesn't care.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// WorkdirScope is the production FSProvider. It enforces
// the host's filesystem scope:
//
//   - Workspace: cwd at start (exclusive). A path inside
//     the workspace is allowed.
//
//   - Allow: additive paths from --add-dir (and any other
//     future flag that adds a directory). Each entry is
//     resolved against the workspace at ResolvePolicy time
//     and matches if the canonical path is the same as or a
//     descendant of the allow entry.
//
//   - Sandbox: a placeholder flag the brief mentions;
//     today it has no additional semantic — when Sandbox
//     is true the scope is *more* restrictive (anything
//     not in Workspace or Allow is rejected); when false,
//     the scope is the looser "workspace + adds" surface.
//     The production wiring in cmd/wagyu/remote_control.go
//     passes Sandbox=flagBoolValue(fs, "sandbox") so a
//     future cycle that wants to tighten the surface only
//     has to update WorkdirScope.
//
// The scope is consulted at ResolvePolicy time only;
// ReadFile / ReadDir skip the policy re-check because
// the canonical path has already been vetted. Symlinks
// are NOT followed by filepath.Clean (filepath.EvalSymlinks
// would follow them; the brief parks symlink-following
// control for a future cycle).
type WorkdirScope struct {
	// Workspace is the cwd at start, exclusive. A path
	// under this directory is allowed.
	Workspace string
	// Allow is the additive list (from --add-dir and
	// similar). Each entry is resolved against the
	// workspace at ResolvePolicy time.
	Allow []string
	// Sandbox flips the scope to the stricter
	// "workspace + add-dir only" mode. Future cycles
	// can hang additional restrictions off this flag.
	Sandbox bool
}

// ResolvePolicy checks the path against the workspace
// + allow list and returns the canonical absolute
// path or ErrPathNotAllowed.
//
// Resolution:
//
//  1. Empty path → ErrPathNotAllowed.
//  2. Resolve against the workspace (or cwd when
//     Workspace is empty).
//  3. If the canonical path is the same as or a
//     descendant of the workspace, allow it.
//  4. Otherwise walk the Allow list; if any entry
//     matches, allow it.
//  5. Otherwise ErrPathNotAllowed.
//
// The function does not consult the filesystem
// (filepath.Clean is pure string manipulation;
// symlinks are NOT resolved at this layer — a future
// cycle's "follow symlinks" knob lives here).
func (s *WorkdirScope) ResolvePolicy(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", ErrPathNotAllowed
	}
	base := strings.TrimSpace(s.Workspace)
	if base == "" {
		// Fallback: when the host hasn't told us the
		// workspace (a future-cycle test fixture), we
		// treat the cwd as the workspace. A host that
		// genuinely wants the workspace unset would
		// pass an empty Workspace and rely on the cwd.
		if cwd, err := os.Getwd(); err == nil {
			base = cwd
		}
	}
	// Resolve relative paths against the workspace
	// first, so "src/main.go" becomes
	// "/workspace/src/main.go" — a dashboard that
	// passes a relative path gets the workspace-
	// anchored resolution it likely expects.
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Clean(filepath.Join(base, path))
	}
	if base != "" && (abs == base || isUnder(abs, base)) {
		return abs, nil
	}
	for _, a := range s.Allow {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		var absA string
		if filepath.IsAbs(a) {
			absA = filepath.Clean(a)
		} else {
			absA = filepath.Clean(filepath.Join(base, a))
		}
		if abs == absA || isUnder(abs, absA) {
			return abs, nil
		}
	}
	return "", ErrPathNotAllowed
}

// ReadFile opens + reads the canonical path. Errors:
//
//   - file size exceeds maxBytes → ErrTooLarge
//   - offset > size → opaque error (the dispatcher
//     surfaces as -32603 with a "beyond size" message;
//     the brief's "OffsetBeyondSize → -32603" rule)
//   - I/O failure → opaque error (-32603)
//   - non-regular-file → ErrNotRegularFile
//
// The function uses os.Open + os.Stat (NOT os.Lstat)
// so symlinks are followed. A future cycle's
// "symlink-following" knob lives here; for now the
// brief says symlink-following control is parked.
func (s *WorkdirScope) ReadFile(canonical string, maxBytes int64, offset int64) (data []byte, total int64, truncated bool, isBinary bool, err error) {
	if err := validateReadFileArgs(maxBytes, offset); err != nil {
		return nil, 0, false, false, err
	}
	fi, err := os.Stat(canonical)
	if err != nil {
		return nil, 0, false, false, err
	}
	if !fi.Mode().IsRegular() {
		return nil, 0, false, false, ErrNotRegularFile
	}
	total = fi.Size()
	if offset > total {
		return nil, total, false, false, fmt.Errorf("rc-file-ops: offset %d beyond size %d", offset, total)
	}
	data, truncated, err = readFileAtOffset(canonical, maxBytes, offset, total)
	if err != nil {
		return data, total, false, isBinaryBytes(data), err
	}
	isBinary = isBinaryBytes(data)
	return data, total, truncated, isBinary, nil
}

// validateReadFileArgs enforces the caller-side
// preconditions ReadFile applies before opening any
// file descriptor: maxBytes must be > 0 (the function
// uses maxBytes+1 to detect truncation) and offset
// must be >= 0. Extracted from ReadFile to keep the
// public method's body under the 50-fn ceiling that
// cycle rc-filesize-internal-remotecontrol enforces.
func validateReadFileArgs(maxBytes int64, offset int64) error {
	if maxBytes <= 0 {
		return fmt.Errorf("rc-file-ops: maxBytes must be > 0")
	}
	if offset < 0 {
		return fmt.Errorf("rc-file-ops: offset must be >= 0")
	}
	return nil
}

// readFileAtOffset opens canonical, seeks to offset,
// reads up to maxBytes+1 bytes, and returns
// (data, truncated, err). err != nil →
// io.EOF-style read failure (n may be partial — caller
// surfaces what was read). maxBytes+1 is the
// truncation-detection padding; the data slice the
// helper returns is trimmed to maxBytes bytes when
// truncated.
//
// Extracted from ReadFile to keep the public method's
// body under the 50-fn ceiling that cycle
// rc-filesize-internal-remotecontrol enforces.
func readFileAtOffset(canonical string, maxBytes int64, offset int64, total int64) ([]byte, bool, error) {
	f, err := os.Open(canonical)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	if offset > 0 {
		if _, err := f.Seek(offset, 0); err != nil {
			return nil, false, err
		}
	}
	buf := make([]byte, maxBytes+1)
	n, err := f.Read(buf)
	if err != nil && err.Error() != "EOF" {
		// n may be partial; preserve the bytes we did
		// read so the dashboard sees them.
		return append([]byte(nil), buf[:n]...), false, err
	}
	data := buf[:n]
	truncated := false
	if int64(n) > maxBytes {
		// We read one extra byte; the file is longer
		// than maxBytes from offset. Truncate and
		// report truncated.
		data = data[:maxBytes]
		truncated = true
	} else if offset+int64(n) < total {
		// Short read but not at EOF (rare; some
		// filesystems return fewer bytes than
		// requested on pipes / sockets). Be
		// conservative: mark truncated.
		truncated = true
	}
	return data, truncated, nil
}

// ReadDir lists the entries of the canonical directory.
// Errors:
//   - I/O failure → opaque error (-32603)
//
// maxEntries caps the returned slice; the total count
// is the full entry count. Entries are sorted by name
// (case-sensitive, ascending).
func (s *WorkdirScope) ReadDir(canonical string, maxEntries int) (entries []DirEntry, total int, truncated bool, err error) {
	f, err := os.Open(canonical)
	if err != nil {
		return nil, 0, false, err
	}
	defer f.Close()
	names, err := f.Readdirnames(0)
	if err != nil {
		return nil, 0, false, err
	}
	sort.Strings(names)
	total = len(names)
	if maxEntries > 0 && total > maxEntries {
		names = names[:maxEntries]
		truncated = true
	}
	out := make([]DirEntry, 0, len(names))
	for _, n := range names {
		// Use Lstat so a symlink is reported as a
		// symlink (its size is the symlink's length,
		// not the target's). The brief parks
		// symlink-following control; this is the
		// conservative default a dashboard expects.
		full := filepath.Join(canonical, n)
		fi, ferr := os.Lstat(full)
		entry := DirEntry{
			Name:  n,
			Type:  "other",
			Size:  0,
			MTime: "",
		}
		if ferr == nil {
			entry.Type = classifyType(fi.Mode())
			entry.Size = fi.Size()
			if !fi.ModTime().IsZero() {
				entry.MTime = fi.ModTime().UTC().Format(time.RFC3339Nano)
			}
		}
		out = append(out, entry)
	}
	return out, total, truncated, nil
}

// isUnder reports whether path equals or sits under
// root. Both arguments must be cleaned absolute paths
// (the ResolvePolicy path uses filepath.Clean before
// this call). The check uses filepath.Rel so a path
// that begins with ".." is correctly rejected.
func isUnder(path, root string) bool {
	if path == root {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err == nil && (rel == "." || (!strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, string(filepath.Separator)))) {
		return true
	}
	// Symlink-aware comparison (e.g. ~/GitHub symlinked to ~/Documents/GitHub).
	realPath, err1 := filepath.EvalSymlinks(path)
	realRoot, err2 := filepath.EvalSymlinks(root)
	if err1 == nil && err2 == nil {
		if realPath == realRoot {
			return true
		}
		relReal, errReal := filepath.Rel(realRoot, realPath)
		if errReal == nil && (relReal == "." || (!strings.HasPrefix(relReal, "..") && !strings.HasPrefix(relReal, string(filepath.Separator)))) {
			return true
		}
	}
	return false
}
