// Package remotecontrol — files.go: pure helper functions + the
// FSProvider seam the two read-only filesystem JSON-RPC methods
// introduced in cycle rc-file-ops call into.
//
// The two methods are:
//
//	ReadRemoteAgentFile  — read a file's content (with size cap)
//	ReadRemoteAgentDir   — list entries of a directory
//
// Both are read-only — no write/delete methods live here (those
// land in rc-conversation-deletions, which is destructive ops
// later per the brief). Both respect the host's filesystem-
// scope policy through the FSProvider seam:
//
//   - FSProvider.ResolvePolicy — checks the path against the
//     host's filesystem scope (sandbox / --add-dir /
//     Antigravity-home restrictions) and returns the canonical
//     absolute path or a sentinel error (ErrPathNotAllowed).
//
//   - FSProvider.ReadFile — opens + reads the canonical path
//     under a hard cap; returns ErrTooLarge if the file
//     exceeds the cap.
//
//   - FSProvider.ReadDir — lists entries; entries beyond
//     maxEntries are dropped and the truncated bool is set.
//
// Why a seam (FSProvider) instead of a direct call into os /
// filepath:
//
//   - Tests inject a small in-memory MemFS (a couple of
//     map[string][]byte directories with a non-allowed
//     secret.txt). The seam keeps the dispatcher pure and
//     stdlib-free for tests; production wires a WorkdirScope
//     that consults the real cwd + --add-dir + sandbox flag.
//
//   - Future cycles (rc-list-projects / rc-update-project /
//     etc.) need to read user-controlled paths too; they
//     reuse the same FSProvider without re-implementing the
//     policy resolution.
//
//   - Cycle rc-error-status etc. live later; this seam is the
//     single place the policy lives — a future cycle that
//     grows the policy (e.g. ".gitignore" awareness) only has
//     to update WorkdirScope.ResolvePolicy.
//
// The sentinels ErrPathNotAllowed / ErrTooLarge /
// ErrNotRegularFile map to the JSON-RPC codes -32007 /
// -32603 / -32008 in dispatch.go. The dispatcher never
// returns a Go error — it builds the JSON-RPC envelope
// directly from the sentinel match.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// FSProvider is the seam the dispatcher calls for read-only
// filesystem operations. Implementations carry their own
// policy (sandbox / --add-dir / etc.); the dispatcher only
// knows about the four sentinels (ErrPathNotAllowed,
// ErrTooLarge, ErrNotRegularFile, plus any stdlib error that
// surfaces as an "internal error" -32603 envelope).
//
// The three methods:
//
//   - ResolvePolicy checks `path` against the host's
//     filesystem scope and returns the canonical absolute
//     path or an error matching one of the sentinels. The
//     canonical path is what the dispatcher surfaces in
//     the JSON-RPC `path` field so a dashboard can show
//     the user exactly what was read.
//
//   - ReadFile opens + reads the canonical path. maxBytes
//     is a hard cap; a file that exceeds it returns
//     ErrTooLarge. offset is the byte offset to start
//     reading from. The returned `total` is the file's
//     total size in bytes; `truncated` is true iff the
//     returned bytes are fewer than `total - offset` (i.e.
//     the read hit the maxBytes cap or the end of file).
//     isBinary is true iff the returned bytes contain a
//     NUL byte anywhere in the first 8 KiB.
//
//   - ReadDir lists entries of the canonical directory.
//     maxEntries is a hard cap; entries beyond the cap
//     are dropped and `truncated` is set true. The
//     returned slice is sorted by name (case-sensitive,
//     ascending) so dashboards see stable ordering.
//
// All three methods MUST be safe to call concurrently with
// one another on the same receiver (the production
// implementation is stateless beyond the cwd + allow-list
// it was built with).
type FSProvider interface {
	ResolvePolicy(path string) (canonical string, err error)
	ReadFile(canonical string, maxBytes int64, offset int64) (data []byte, total int64, truncated bool, isBinary bool, err error)
	ReadDir(canonical string, maxEntries int) (entries []DirEntry, total int, truncated bool, err error)
}

// ErrPathNotAllowed is returned by FSProvider.ResolvePolicy
// when the requested path violates the host's filesystem
// scope (outside --add-dir, blocked by sandbox, etc.). The
// dispatcher maps this sentinel to JSON-RPC code -32007
// "Path not allowed".
var ErrPathNotAllowed = errors.New("rc-file-ops: path not allowed")

// ErrTooLarge is returned by FSProvider.ReadFile when the
// file exceeds the caller's maxBytes cap. The dispatcher
// maps this sentinel to JSON-RPC code -32603 "Internal
// error" with a "file too large" message — the brief's
// "maxBytes > 16 MiB" boundary already rejected requests
// for absurd caps at the param-validation step, so this
// path only fires when a caller requested a sane cap and
// the file still doesn't fit.
var ErrTooLarge = errors.New("rc-file-ops: file too large")

// ErrNotRegularFile is returned by FSProvider.ReadFile
// when the path resolves to a directory, a symlink-to-
// nowhere, a socket, a fifo, or any non-regular-file
// inode. The dispatcher maps this sentinel to JSON-RPC
// code -32008 "Not a regular file".
var ErrNotRegularFile = errors.New("rc-file-ops: not a regular file")

// DefaultReadMaxBytes is the default maxBytes cap the
// dispatcher uses when the caller omits the field.
// 1 MiB is generous for "show me a config file / source
// file" probes a dashboard issues.
const DefaultReadMaxBytes int64 = 1 << 20

// MaxReadMaxBytes is the absolute upper bound on maxBytes
// the dispatcher accepts. Anything above this returns
// -32602 (Invalid params) at the param-validation step
// before the FSProvider is called. 16 MiB is large enough
// to dump a sizable source tree, small enough that an
// accidental pull-the-whole-repository loop doesn't OOM
// the daemon.
const MaxReadMaxBytes int64 = 16 << 20

// DefaultReadDirMaxEntries is the default maxEntries cap
// the dispatcher uses when the caller omits the field.
const DefaultReadDirMaxEntries int = 1000

// MaxReadDirMaxEntries is the absolute upper bound on
// maxEntries the dispatcher accepts. Anything above this
// returns -32602 (Invalid params) before the FSProvider is
// called.
const MaxReadDirMaxEntries int = 10000

// isBinaryWindow is the byte prefix the dispatcher scans
// for a NUL byte to classify the file as binary. 8 KiB
// matches what `git` and `file -i` use as a heuristic.
// Anything past the window is irrelevant: a file that
// starts as text and switches to binary deep into the
// body is still text from a dashboard's perspective.
const isBinaryWindow = 8 * 1024

// DirEntry is one row of ReadRemoteAgentDir's `entries`
// array. The shape mirrors the agy decompiled
// `readRemoteAgentDir` envelope (cf.
// out/local-macos/dec/1018ca390_ReadRemoteAgentDir.c).
//
//   - Name is the basename (no directory component).
//   - Type is "file" | "dir" | "symlink" | "other"; a
//     symlink's target is not followed (the brief:
//     "symlink-following control" is parked for a future
//     cycle).
//   - Size is the size in bytes; 0 for directories and
//     other non-regular inodes (matches stat.st_size's
//     semantic for the regular case).
//   - MTime is the mtime in RFC3339Nano; empty when the
//     filesystem doesn't surface one (rare).
type DirEntry struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Size  int64  `json:"size"`
	MTime string `json:"mtime"`
}

// classifyType maps a fs.FileMode to the wire-side
// "file" | "dir" | "symlink" | "other" vocabulary. The
// caller passes the result of os.Lstat so a symlink is
// classified as a symlink (NOT followed). mode.IsRegular
// covers the regular-file case; mode.IsDir covers the
// directory case; the symlink check is the bit test on
// mode&os.ModeSymlink. Anything else ("other") covers
// sockets, fifos, devices, and unknown future inode types.
func classifyType(mode os.FileMode) string {
	switch {
	case mode&os.ModeSymlink != 0:
		return "symlink"
	case mode.IsDir():
		return "dir"
	case mode.IsRegular():
		return "file"
	default:
		return "other"
	}
}

// isBinary reports whether the byte slice contains a NUL
// byte in its first isBinaryWindow bytes. The dispatcher
// uses this to decide whether to base64-encode the
// content: dashboards render utf-8 strings verbatim but
// can't render raw binary, so a binary result is
// base64-wrapped so the JSON envelope stays valid utf-8.
func isBinaryBytes(data []byte) bool {
	if len(data) > isBinaryWindow {
		data = data[:isBinaryWindow]
	}
	return bytes.IndexByte(data, 0) >= 0
}

// encodeContent base64-encodes data when isBinary is true,
// otherwise returns it as a string. The dispatcher's
// result envelope puts the string into the `content`
// field verbatim; a binary result is wrapped in base64
// so the wire format is valid utf-8 JSON.
func encodeContent(data []byte, isBinary bool) string {
	if isBinary {
		return base64.StdEncoding.EncodeToString(data)
	}
	return string(data)
}

// (the production seam lives in scope.go; this file
// holds the dispatcher-facing types and the in-memory
// fixture the tests inject.)

// MemFSEntry is one row of a MemFS directory. The
// fixture used by files_test.go and (forward) any test
// that wants to drive the dispatcher without touching the
// real filesystem.
//
// Fields:
//
//   - Name: the basename. Required.
//   - IsDir: when true, the entry is a directory (no
//     children are stored; ReadDir of a sub-directory
//     requires a separate row in NewMemFS's map).
//   - Data: the file's content (ignored when IsDir).
//
// The struct is exported so tests in other packages
// (notably remotetransport/tcp_test.go, which
// exercises the wire envelope) can construct entries
// without re-implementing the unexported shape.
type MemFSEntry struct {
	Name  string
	IsDir bool
	Data  []byte // when IsDir=false
}

// MemFS is the in-memory FSProvider implementation the
// dispatcher tests inject. It is built from a map of
// absolute-path → []MemFSEntry: the directory the map
// keys are the visible directories, and entries within
// are the visible files / sub-directories.
//
// The MemFS has no policy: every path passed to
// ResolvePolicy is returned verbatim (the absolute path
// the caller already supplied). The "rejected" case for
// path-not-allowed tests is driven by passing a path
// that the production WorkdirScope rejects — MemFS is
// the policy-free fixture, WorkdirScope is the
// production-policy implementation.
//
// Symlinks are NOT modelled (the brief parks
// symlink-following control). MemFSReadDir returns
// every entry verbatim.
type MemFS struct {
	files map[string]map[string]MemFSEntry // absDirPath → entries
}

// NewMemFS builds an in-memory FSProvider from a map of
// absolute-directory-paths to a slice of entries. The
// keys are absolute directory paths; the values are the
// entries visible inside that directory. Sub-directories
// are modelled by including an entry with isDir=true
// and an empty Data slice; ReadDir of the sub-directory
// would require its own row in the map.
//
// The map is copied by reference — a test that mutates
// it after construction sees the mutation through
// MemFS too. Tests don't race on this because they're
// single-threaded by construction.
func NewMemFS(files map[string][]MemFSEntry) *MemFS {
	m := &MemFS{files: make(map[string]map[string]MemFSEntry, len(files))}
	for dir, entries := range files {
		row := make(map[string]MemFSEntry, len(entries))
		for _, e := range entries {
			row[e.Name] = e
		}
		m.files[dir] = row
	}
	return m
}

// ResolvePolicy returns the path verbatim. MemFS has no
// policy of its own; tests pair it with a WorkdirScope
// (or substitute) for the rejection paths.
func (m *MemFS) ResolvePolicy(path string) (string, error) {
	if path == "" {
		return "", ErrPathNotAllowed
	}
	return path, nil
}

// ReadFile looks the path up in the files map (the file
// is registered under its parent directory's row).
// Errors: ErrNotRegularFile when the entry is a
// directory; ErrTooLarge when the file exceeds maxBytes;
// any other error returns an opaque read failure the
// dispatcher surfaces as -32603.
func (m *MemFS) ReadFile(canonical string, maxBytes int64, offset int64) (data []byte, total int64, truncated bool, isBinary bool, err error) {
	dir, name := splitAbs(canonical)
	if dir == "" || name == "" {
		return nil, 0, false, false, fmt.Errorf("rc-file-ops: invalid path %q", canonical)
	}
	row, ok := m.files[dir]
	if !ok {
		return nil, 0, false, false, fmt.Errorf("rc-file-ops: no such file %q", canonical)
	}
	e, ok := row[name]
	if !ok {
		return nil, 0, false, false, fmt.Errorf("rc-file-ops: no such file %q", canonical)
	}
	if e.IsDir {
		return nil, 0, false, false, ErrNotRegularFile
	}
	total = int64(len(e.Data))
	if offset < 0 {
		return nil, total, false, false, fmt.Errorf("rc-file-ops: negative offset %d", offset)
	}
	if offset >= total {
		return nil, total, false, false, fmt.Errorf("rc-file-ops: offset %d beyond size %d", offset, total)
	}
	end := offset + maxBytes
	if end > total {
		end = total
	}
	data = e.Data[offset:end]
	truncated = end < total
	isBinary = isBinaryBytes(data)
	return data, total, truncated, isBinary, nil
}

// ReadDir lists the entries of the directory. Entries
// beyond maxEntries are dropped and truncated is set.
// The returned slice is sorted by name (case-sensitive)
// so dashboards see stable ordering.
func (m *MemFS) ReadDir(canonical string, maxEntries int) (entries []DirEntry, total int, truncated bool, err error) {
	row, ok := m.files[canonical]
	if !ok {
		return nil, 0, false, fmt.Errorf("rc-file-ops: no such directory %q", canonical)
	}
	all := make([]DirEntry, 0, len(row))
	for _, e := range row {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		entry := DirEntry{
			Name:  e.Name,
			Type:  "other",
			Size:  int64(len(e.Data)),
			MTime: now,
		}
		if e.IsDir {
			entry.Type = "dir"
			entry.Size = 0
		} else {
			entry.Type = "file"
		}
		all = append(all, entry)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	total = len(all)
	if maxEntries > 0 && len(all) > maxEntries {
		all = all[:maxEntries]
		truncated = true
	}
	return all, total, truncated, nil
}

// splitAbs splits an absolute path into (dir, name).
// The helper exists so MemFS.ReadFile can look up
// the parent directory's entry row. Empty inputs yield
// ("", "").
func splitAbs(p string) (dir, name string) {
	if p == "" {
		return "", ""
	}
	dir = filepath.Dir(p)
	name = filepath.Base(p)
	if dir == p {
		// filepath.Dir("/foo") returns "/foo" when the
		// path has no separator; treat the input as the
		// basename with no parent.
		return "", p
	}
	return dir, name
}

// (WorkdirScope lives in scope.go.)
