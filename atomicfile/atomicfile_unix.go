//go:build !windows

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
