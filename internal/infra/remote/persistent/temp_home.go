// temp_home.go: the guard against registering the launchd daemon
// from a temporary home.
//
// The launchd job's label is global to the login session
// (`gui/<uid>/<label>`), so registering it is not "adding a daemon"
// — it is *replacing* the one that is already registered under that
// label. A registration whose plist lives in a temporary tree
// therefore has two effects, and both of them are bad:
//
//   - the daemon the user already runs is stopped and replaced by one
//     whose stdout and stderr point into a directory that is deleted
//     with the temporary tree, so the user's own log goes silent;
//   - the replacement is registered from a *different* plist path, so
//     `launchctl print` names a file that does not exist, and the
//     next `remote-control start` has to bootstrap it again.
//
// The way this happens is a test: `go test` points $HOME at a
// temporary directory, and the /remote-control handler starts the
// official daemon. So the guard belongs to the production path: a
// daemon whose home is a temporary tree is refused, with a message
// that says why.
//
// The check is on the *path*, not on $HOME: a home that merely
// contains a temporary directory is fine, and the check has to see
// the plist the daemon would actually be registered from.

package persistent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TemporaryHomeError is the error StartOfficialDaemon returns when
// the resolved plist path lives inside the operating system's
// temporary directory.
type TemporaryHomeError struct {
	// Path is the plist path the registration would have used.
	Path string
}

func (e *TemporaryHomeError) Error() string {
	return fmt.Sprintf("refusing to register the launchd daemon at %s: the home directory points into a temporary tree, so the registration would replace the daemon the user already runs with one whose log lives in a temporary directory", e.Path)
}

// refusesTemporaryHome returns a *TemporaryHomeError when path lies
// inside the operating system's temporary directory. nil otherwise.
//
// The comparison is done on the cleaned, absolute paths and the
// temporary directory's own prefix, so `/tmp/foo` is inside `/tmp`
// and `/tmpfoo` is not.
//
// The symlinks are resolved on both sides first: on macOS the
// temporary directory is reached through `/var`, which is a symlink
// to `/private/var`, and the test's own home carries the resolved
// prefix. Without the resolution the two would not compare equal and
// the guard would miss exactly the case it exists for.
func refusesTemporaryHome(path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil
	}
	for _, tmp := range temporaryRoots() {
		if tmp == "" {
			continue
		}
		rel, err := filepath.Rel(tmp, abs)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			// Outside this root.
			continue
		}
		return &TemporaryHomeError{Path: abs}
	}
	return nil
}

// temporaryRoots returns the directories a temporary home can live
// under: the operating system's temporary directory and its
// symlink-resolved form.
func temporaryRoots() []string {
	tmp := strings.TrimSpace(os.TempDir())
	if tmp == "" {
		return nil
	}
	roots := []string{tmp}
	if resolved, err := filepath.EvalSymlinks(tmp); err == nil && resolved != tmp {
		roots = append(roots, resolved)
	}
	return roots
}
