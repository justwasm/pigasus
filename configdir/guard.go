// Package configdir — guard.go
//
// The machine's own configuration, and the one rule a test must not break.
//
// # Why a runtime check
//
// Environment isolation is the first line of defence: `internal/testguard`
// points `OPENAGY_CONFIG_HOME`, `OPENAGY_ACCOUNTS_CONFIG` and `HOME` at a
// temporary directory for a whole test package. It is not enough on its own,
// because a path is resolved *late* — every reader calls Dir() and Path() when
// the write happens, not when the test started. A test that sets its own
// `HOME` has that setting restored when the test ends, so a deferred save, a
// background goroutine or a later test in the same binary resolves the real
// home again and writes the user's own file. The damage is silent: the user's
// `accounts.json` (plaintext credentials, and the pool's policy) is replaced,
// and the next run looks broken.
//
// So the writers ask this package, at the moment of the write, whether the
// path they are about to touch is the file this machine started with. When the
// running binary is a test binary, that answer is "yes" for the user's own
// configuration, and the write is refused with an error instead of performed.
//
// # What it cannot do
//
// It cannot protect a configuration that was never the startup one: a test
// that runs with `OPENAGY_CONFIG_HOME` already pointing somewhere is isolated
// by that variable, and a path outside the startup home is not this machine's
// configuration. Nor does it cover a child process that scrubs its
// environment — that process is not a test binary and resolves whatever home
// it was given, which is why `internal/testguard` exists as well.
package configdir

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// startupDir is the configuration home this process started with, resolved
// once at initialisation: before any test can repoint the environment, and
// before any flag is parsed that could make Dir() answer differently.
var startupDir = Dir()

// IsTestBinary reports whether this process is a `go test` binary.
//
// Go names the binary it runs `<package>.test`, and the `testing` package
// registers its flags at initialisation, so either signal identifies one. A
// product binary carries neither.
func IsTestBinary() bool {
	if strings.HasSuffix(os.Args[0], ".test") {
		return true
	}
	return flag.Lookup("test.timeout") != nil || flag.Lookup("test.v") != nil
}

// StartupDir returns the configuration home this process started with. It is
// empty when the process started without one.
func StartupDir() string { return startupDir }

// RefuseTestWrite returns an error when path names a file in the configuration
// home this process started with and the running binary is a test binary.
//
// A writer calls it immediately before touching the file:
//
//	if err := configdir.RefuseTestWrite(path); err != nil {
//		return err
//	}
//
// It returns nil for every other case — a product binary, a path outside the
// startup home, a process that started with no home at all — so the check
// costs one path comparison on the normal path.
func RefuseTestWrite(path string) error {
	if path == "" || startupDir == "" || !IsTestBinary() {
		return nil
	}
	if !samePath(path, startupDir) {
		return nil
	}
	return fmt.Errorf("configdir: refusing to write %s: it is this machine's own configuration and this binary is a test", path)
}

// samePath reports whether path lands inside dir (or the directory symlinks
// to).
func samePath(path, dir string) bool {
	if within(filepath.Clean(path), filepath.Clean(dir)) {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return within(filepath.Clean(path), resolved)
	}
	return false
}

// within reports whether the cleaned path is dir itself or a child of it.
func within(path, dir string) bool {
	if path == dir {
		return true
	}
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
