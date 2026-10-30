//go:build windows

package atomicfile

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// WriteFile writes data to path, replacing the file atomically on Windows.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	var randBytes [8]byte
	_, _ = io.ReadFull(rand.Reader, randBytes[:])
	tmpPath := filepath.Join(dir, fmt.Sprintf(".%s-%s.tmp", base, hex.EncodeToString(randBytes[:])))

	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	// Try atomic rename first.
	if err := os.Rename(tmpPath, path); err == nil {
		return nil
	}

	// On Windows, if path is open or running, direct rename fails with access denied.
	// Renaming the existing target to a .old sibling allows moving the new file in.
	oldPath := filepath.Join(dir, fmt.Sprintf(".%s-%s.old", base, hex.EncodeToString(randBytes[:])))
	if renErr := os.Rename(path, oldPath); renErr == nil {
		if err := os.Rename(tmpPath, path); err != nil {
			_ = os.Rename(oldPath, path) // rollback
			return err
		}
		_ = os.Remove(oldPath)
		return nil
	}

	return os.Rename(tmpPath, path)
}
