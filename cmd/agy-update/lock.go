// lock.go — the self-updater's advisory lock.
//
// 【文档】 official-docs/cli/troubleshooting.md:158 —
//
//	Warning: another background updater process is already active (update.lock)
//
// The lock is a marker file inside the updater's own directory
// (troubleshooting.md:163), and the reference's own remedy for a marker
// left behind by a crashed updater is to delete it by hand
// (troubleshooting.md:172).
//
// It is advisory, not a mutex: nothing waits for it and nothing times it
// out. The second updater finds the marker and steps aside; the first
// one removes it when it is done. A lock that survives a crash blocks
// the next update until the file is removed by hand — which is what the
// reference's own text describes.
package main

import (
	"os"
	"path/filepath"

	"github.com/justwasm/pigasus/internal/core/confhome"
)

// updaterLockWarning is the reference's own wording for the advisory
// lock the self-updater takes.
//
// The "Warning: " prefix belongs to the line: the updater writes its
// diagnostics to stderr and prefixes them itself, the way
// `internal/agyupdate` prefixes its errors and `internal/cli/install.go`
// prefixes its warnings.
const updaterLockWarning = "Warning: another background updater process is already active (update.lock)"

// updaterLockPath is the lock file's path.
//
// 【文档】 the reference keeps the lock in the updater's own directory
// inside the CLI's home (troubleshooting.md:163), so the path hangs
// under the home every other file of the CLI lives in
// (`internal/core/confhome`).
func updaterLockPath() string {
	return filepath.Join(confhome.Antigravity(), "updater", "update.lock")
}

// acquireUpdaterLock takes the advisory lock.
//
// The marker is created exclusively, so the updater that finds it there
// steps aside and says so. The returned release function removes the
// marker again — the lock is held for the length of one update.
//
// A marker that cannot be created at all (a home that cannot be
// written) is not a reason to refuse the update: the lock is advisory,
// and the reference's own failure for that case is a filesystem
// permission problem rather than a lock.
func acquireUpdaterLock(path string) (release func(), ok bool) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return func() {}, true
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return func() {}, false
		}
		return func() {}, true
	}
	_ = f.Close()
	return func() { _ = os.Remove(path) }, true
}
