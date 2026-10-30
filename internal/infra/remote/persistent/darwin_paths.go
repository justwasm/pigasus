//go:build darwin

// darwin_paths.go: LaunchAgent plist / pidfile / side-car path
// resolution for the macOS / launchd implementation.
//
// Split out of darwin.go in cycle `rc-filesize-internal-persistentrc`
// to keep the file below the 500-LOC ceiling.  This file owns:
//
//   - plistPath             — Spec → absolute plist path
//   - pidfilePath           — Spec → dry-run pidfile path
//   - readPidfile           — read the dry-run pidfile
//   - removePidfile         — delete the dry-run pidfile
//   - startedAtFromPlist    — side-car "<file>.started_at" reader
//   - nowFromMtime          — file-mtime → epoch seconds
//
// The non-darwin versions of pidfilePath / readPidfile /
// removePidfile live in `other.go` so the package compiles on
// every platform.

package persistent

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// plistPath returns the absolute LaunchAgent plist path the Start
// path will use, honouring Spec.PlistPath / Spec.PlistDir / the
// active plist-dir override / Spec.Name in that order.  Tests
// inject a temp dir via SetPlistDirOverride so they don't touch
// ~/Library/LaunchAgents.
func plistPath(spec Spec) string {
	if spec.PlistPath != "" {
		return spec.PlistPath
	}
	dir := spec.PlistDir
	if dir == "" {
		dir = activePlistDir()
	}
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, Label(spec.Name)+".plist")
}

// pidfilePath is the dry-run pidfile the test mode writes
// alongside the plist.  When SkipLaunchd is set, Start writes a
// fake PID here and Status reads it back.  Production never reads
// or writes this file (launchd is the source of truth).
func pidfilePath(spec Spec) string {
	p := plistPath(spec)
	if p == "" {
		return ""
	}
	return p + ".pid"
}

// readPidfile reads the dry-run pidfile and returns the PID it
// contains.  Returns (0, nil) when the file is missing.
func readPidfile(spec Spec) (int, error) {
	p := pidfilePath(spec)
	if p == "" {
		return 0, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

// removePidfile deletes the dry-run pidfile (no-op when absent).
func removePidfile(spec Spec) {
	if p := pidfilePath(spec); p != "" {
		_ = os.Remove(p)
	}
}

// startedAtFromPlist reads a side-car file `<file>.started_at`
// holding the epoch-second the plist was first written.  Tests
// pin this so the StatusReport's started_at is reproducible.
// When the side-car is absent we fall back to the plist's
// file-mtime (which matches what launchctl bootstrap would
// have set in production).
func startedAtFromPlist(plistPath string) int64 {
	if plistPath == "" {
		return 0
	}
	sc := plistPath + ".started_at"
	if data, err := os.ReadFile(sc); err == nil {
		if v, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil && v > 0 {
			return v
		}
	}
	return nowFromMtime(plistPath)
}

// nowFromMtime returns the file mtime in epoch seconds.  Zero on
// stat failure.
func nowFromMtime(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.ModTime().Unix()
}
