// Package atomicfile writes a file in one step: a sibling temp file, a
// fsync, and a rename over the target. A reader either sees the old
// file or the new one, never a half-written one.
//
// It is a thin wrapper over github.com/google/renameio/v2, and the
// wrapper earns its keep on one point: the file's mode.
//
// renameio.WriteFile copies the *existing* file's mode onto the new
// file (WithExistingPermissions is in its default option set), and a
// static mode passed alongside is overwritten by that copy. The clone's
// own writers have always written 0600 — these files hold API keys,
// OAuth tokens and account records — so the mode must not depend on
// what a file happened to be before.
package atomicfile

import (
	"os"

	"github.com/google/renameio/v2"
)

// WriteFile writes data to path, replacing the file atomically. The
// resulting file's mode is exactly perm, whatever the file's mode was
// before.
//
// The temp file is a sibling of the target (same directory, same
// filesystem), so the rename cannot cross a mount point.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	f, err := renameio.NewPendingFile(path, renameio.WithStaticPermissions(perm))
	if err != nil {
		return err
	}
	// Cleanup removes the temp file if the write or the rename failed.
	defer func() { _ = f.Cleanup() }()
	if _, err := f.Write(data); err != nil {
		return err
	}
	// CloseAtomicallyReplace fsyncs the file and renames it over the
	// target.
	return f.CloseAtomicallyReplace()
}
