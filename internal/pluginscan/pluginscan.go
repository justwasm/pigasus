// Package pluginscan walks plugin and extension source directories
// (the ~/.gemini/config/plugins/<name>/ trees the reference ships
// slash commands, skills, hooks, sub-agents, and CLAUDE.md/AGENTS.md
// inside of) while following symlinks at every level.
//
// AGY 1.2.5 changelog gap closure:
//
//	Fixed custom slash commands, skills, hooks, sub-agents, and
//	CLAUDE.md / AGENTS.md files inside a symlinked plugin or extension
//	directory being silently ignored because the loader walked real
//	paths instead of following symlinks.
//
// # Why this needs a separate package
//
// filepath.Walk and filepath.WalkDir both treat symbolic links
// opaquely: they surface the link itself, but the caller cannot tell
// whether the link targets a directory, a regular file, or nothing.
// On the reference machine several plugin sources live behind a
// symlink (for example, this machine's
// ~/.gemini/skills/terminal-browser is a symlink), and a loader that
// uses os.ReadDir + e.IsDir() silently drops any symlinked entry —
// e.IsDir() reports the link's mode, not the resolved target's.
//
// What this package does
//
//   - IsDir / IsRegularFile resolve a path through any symlink chain
//     and report the target's FileMode category. A missing target
//     (broken symlink) returns false from both — the caller treats
//     it as "not there".
//
//   - ReadEntries is the os.ReadDir equivalent the loaders actually
//     want. It returns one Entry per child, with the resolved
//     Info() filled in so callers can branch on Info().IsDir() and
//     friends without an extra stat per entry.
//
//   - Walk is filepath.WalkDir's contract but it descends through
//     symlinks. A symlink cycle (a directory whose chain of links
//     leads back to itself) is broken at the cycle entry, so the
//     walk terminates; the loader does not hang.
//
// What this package does NOT do
//
//   - Resolved-path normalization. We hand the caller the path as
//     they passed it (plus the entry's basename joined onto the
//     parent) so the existing "<plugin>/skills/<x>" string lookups
//     keep working. EvalSymlinks is the wrong primitive here — it
//     would change a "my-plugin" directory name in the result to
//     wherever the symlink points, breaking the existing on-disk
//     layout assumption every loader makes.
//
//   - Touch the existing cmd/wagyu loaders. The package is the
//     leaf helper; the call-site changes (cmd/wagyu/plugin_data.go,
//     cmd/wagyu/plugin_agents.go, internal/cli/subcommand_parity.go)
//     are kept in a single commit so the diff stays reviewable.
//
// # Cycle detection
//
// Walk maintains a stack keyed by parent path. On each directory
// descent the realpath is checked against the realpath of every
// ancestor in the descent chain; a hit is treated as a symlink
// cycle and the descent is skipped with fs.SkipDir semantics (the
// entry itself is still reported once via fn, so the caller sees
// every reachable directory at least once). The resolved realpath
// is cached per path so a deeply-nested symlinked tree does not
// stat each level N times.
package pluginscan

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IsDir reports whether path resolves to a directory, following any
// symlink chain. A path that does not exist, is unreadable, or is a
// broken symlink returns false.
//
// Equivalent to the `isDirFollowingLinks` helper the cycle-198
// printSlashSkillInfos already inlined; this package lifts it so
// every plugin/extension loader shares one implementation.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// IsRegularFile reports whether path resolves to a regular file,
// following any symlink chain. Broken symlinks return false.
//
// Equivalent to d.Info().Mode().IsRegular() applied to the entry
// after a stat — the helper exists so loaders do not need to
// capture and discard the stat error inline.
func IsRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Entry is one directory child plus its resolved FileInfo. The
// underlying os.DirEntry is wrapped so the resolved info is computed
// once per entry — loaders that previously did
// `e.IsDir()` followed by a separate `os.Stat` now have a single
// source of truth.
type Entry struct {
	Name string
	Info os.FileInfo
}

// IsDir mirrors Info().IsDir(); the wrapper exists so call sites
// read the same way the legacy e.IsDir() they replaced did.
func (e Entry) IsDir() bool { return e.Info.IsDir() }

// IsRegularFile mirrors Info().Mode().IsRegular().
func (e Entry) IsRegularFile() bool { return e.Info.Mode().IsRegular() }

// ReadEntries lists the children of dir. A child that cannot be
// stat'd (broken symlink, permission error, race condition) is
// dropped silently — the loader's contract is "give me every
// entry that resolves to something on disk", and a broken link
// resolves to nothing.
//
// os.DirEntry.Info() reports the link's own FileMode (Lstat
// semantics), so it would falsely report IsDir()==false on a
// symlink that targets a directory. We deliberately resolve each
// entry with os.Stat — that follows the symlink and gives the
// target's real type. A broken link surfaces stat's ENOENT here
// and we drop it.
//
// A non-existent dir returns (nil, nil) so callers can distinguish
// "absent" from "broken", matching the convention os.ReadDir and
// the prior loaders established. Other stat errors propagate.
func ReadEntries(dir string) ([]Entry, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, errors.New("pluginscan: empty dir")
	}
	raws, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Entry, 0, len(raws))
	for _, r := range raws {
		// Resolve the symlink target's type with os.Stat
		// (follows links). r.Info() does NOT follow links
		// — see os.DirEntry's Lstat-based Info method.
		info, err := os.Stat(filepath.Join(dir, r.Name()))
		if err != nil {
			// Broken symlink or unreadable entry — skip
			// silently. The reference loader used to do
			// `e.IsDir()` which silently dropped these
			// too, so dropping here is parity.
			continue
		}
		out = append(out, Entry{Name: r.Name(), Info: info})
	}
	return out, nil
}

// WalkFunc is the legacy "func(path string, info os.FileInfo, err
// error) error" walk shape — kept here so older call sites that
// still use filepath.Walk can switch in one line. New code should
// prefer Walk (the fs.WalkDirFunc shape).
type WalkFunc func(path string, info os.FileInfo, err error) error

// Walk walks the file tree rooted at root, calling fn for each file
// or directory in the tree, including root. It mirrors
// filepath.WalkDir's contract (the fs.WalkDirFunc callback) but
// descends through symlinks.
//
// Symlink cycles are detected by comparing each directory's
// realpath against the realpath of every ancestor in the descent
// stack. A cycle entry is reported once via fn and then skipped,
// so the walk terminates without recursion overflow.
//
// Errors from fn are propagated verbatim: fs.SkipDir ends the
// current directory's descent; fs.SkipAll ends the whole walk;
// any other error aborts.
//
// root is stat'd once at entry so a non-existent root surfaces a
// single, clear error rather than a flood of "lstat: no such
// file or directory" messages during the descent.
func Walk(root string, fn fs.WalkDirFunc) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("pluginscan: empty root")
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		// WalkDir semantics: a non-directory root is
		// reported once and the walk ends.
		d := &dirEntryShim{entry: dirEntryFromInfo(info), infoErr: nil}
		if err := fn(root, d, nil); err != nil && !errors.Is(err, fs.SkipAll) {
			if errors.Is(err, fs.SkipDir) {
				return nil
			}
			return err
		}
		return nil
	}
	return walkSym(root, fn)
}

// WalkLegacy walks root using fn in the legacy filepath.Walk
// callback shape (path, info, err). It is a thin shim over Walk
// that resolves the os.DirEntry to os.FileInfo before each fn
// invocation. Prefer Walk for new code.
func WalkLegacy(root string, fn WalkFunc) error {
	return Walk(root, func(p string, d fs.DirEntry, err error) error {
		var info os.FileInfo
		var infoErr error
		if d != nil {
			info, infoErr = d.Info()
		}
		if err != nil {
			return fn(p, info, err)
		}
		return fn(p, info, infoErr)
	})
}

// walkSym is the actual stack-based walker. Separated from Walk
// so the "root is not a directory" branch does not have to know
// about the stack.
//
// os.DirEntry.Info() reports the link's own FileMode (Lstat
// semantics); we deliberately resolve every child with os.Stat
// before deciding whether to descend. A child that stat()s is a
// directory from the loader's perspective, even when it is a
// symlink to one.
type symFrame struct {
	path    string
	real    string
	entries []os.DirEntry
}

func invokeSymEntry(topPath, child string, e os.DirEntry, fn fs.WalkDirFunc) (os.FileInfo, error) {
	childInfo, infoErr := os.Stat(child)
	if infoErr != nil {
		shim := &dirEntryShim{entry: e, resolved: false}
		werr := fn(topPath, shim, infoErr)
		if werr != nil && !errors.Is(werr, fs.SkipAll) && !errors.Is(werr, fs.SkipDir) {
			return nil, werr
		}
		return nil, nil
	}
	shim := &dirEntryShim{entry: e, resolved: true, info: childInfo}
	werr := fn(topPath, shim, nil)
	if werr != nil && !errors.Is(werr, fs.SkipAll) && !errors.Is(werr, fs.SkipDir) {
		return nil, werr
	}
	if errors.Is(werr, fs.SkipDir) || !childInfo.IsDir() {
		return nil, nil
	}
	return childInfo, nil
}

func tryDescendSymDir(stack []symFrame, child string) []symFrame {
	childReal, rerr := filepath.EvalSymlinks(child)
	if rerr != nil {
		return stack
	}
	for _, f := range stack {
		if f.real == childReal {
			return stack
		}
	}
	kids, err := os.ReadDir(child)
	if err != nil {
		return stack
	}
	return append(stack, symFrame{path: child, real: childReal, entries: kids})
}

func walkSym(root string, fn fs.WalkDirFunc) error {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootReal = root
	}
	rootEntries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	d := &dirEntryShim{entry: dirEntryFromRoot(root), resolved: false}
	if werr := fn(root, d, nil); werr != nil && !errors.Is(werr, fs.SkipAll) {
		if !errors.Is(werr, fs.SkipDir) {
			return werr
		}
		return nil
	}
	stack := []symFrame{{path: root, real: rootReal, entries: rootEntries}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if len(top.entries) == 0 {
			stack = stack[:len(stack)-1]
			continue
		}
		e := top.entries[0]
		top.entries = top.entries[1:]
		child := filepath.Join(top.path, e.Name())
		info, err := invokeSymEntry(top.path, child, e, fn)
		if err != nil {
			return err
		}
		if info != nil {
			stack = tryDescendSymDir(stack, child)
		}
	}
	return nil
}

// dirEntryFromRoot is the synthetic DirEntry used for the root of
// a walk. os.DirEntry implementations are not exposed by the
// stdlib, so we wrap the info via shim construction.
func dirEntryFromRoot(root string) os.DirEntry {
	return &nameOnlyEntry{name: filepath.Base(root), isDir: true}
}

// dirEntryFromInfo wraps an info in a DirEntry-shaped shim.
func dirEntryFromInfo(info os.FileInfo) os.DirEntry {
	return &nameOnlyEntry{name: info.Name(), isDir: info.IsDir()}
}

// nameOnlyEntry is the minimal DirEntry shape used by the
// dirEntryShim. os.ReadDir's entries already implement
// DirEntry; this fallback exists only for the synthetic root
// case.
type nameOnlyEntry struct {
	name  string
	isDir bool
}

func (n *nameOnlyEntry) Name() string      { return n.name }
func (n *nameOnlyEntry) IsDir() bool       { return n.isDir }
func (n *nameOnlyEntry) Type() os.FileMode { return modeFromDir(n.isDir) }
func (n *nameOnlyEntry) Info() (os.FileInfo, error) {
	return nil, errors.New("pluginscan: synthetic entry has no info")
}

// modeFromDir maps a boolean directory flag to the matching
// os.FileMode bits.
func modeFromDir(isDir bool) os.FileMode {
	if isDir {
		return os.ModeDir
	}
	return 0
}

// dirEntryShim adapts the resolved os.DirEntry into the form Walk
// hands to the callback. The shim wraps the underlying DirEntry
// but reports the symlink-resolved FileInfo: when resolved is true
// the shim's IsDir/Type/Info come from `info` (the stat result),
// not from the link's own Lstat-based FileMode. This is the
// single most important invariant of the package — without it the
// loader would still see the link's mode, not the target's.
type dirEntryShim struct {
	entry    os.DirEntry
	resolved bool
	info     os.FileInfo
	infoErr  error
}

func (d *dirEntryShim) Name() string { return d.entry.Name() }
func (d *dirEntryShim) IsDir() bool {
	if d.resolved {
		return d.info.IsDir()
	}
	return d.entry.IsDir()
}
func (d *dirEntryShim) Type() os.FileMode {
	if d.resolved {
		return d.info.Mode().Type()
	}
	return d.entry.Type()
}
func (d *dirEntryShim) Info() (os.FileInfo, error) {
	if d.infoErr != nil {
		return nil, d.infoErr
	}
	if d.resolved {
		return d.info, nil
	}
	return d.entry.Info()
}
