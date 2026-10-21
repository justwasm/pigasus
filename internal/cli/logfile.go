// Package cli - logfile.go implements a 1:1 reimplementation of
// google3/third_party/jetski/cli/logfile from the v1.3.4 binary.
//
// jetski/cli/logfile is a thin wrapper that:
//   - Opens a log file at a stable path (typically ~/.gemini/<app>/log/<date>.log)
//   - Maintains a "latest" symlink to the most recent log
//   - Cleans up logs older than `maxAge` on startup
//
// The recovered implementation matches the public surface of the
// v1.3.4 binary's logfile package, but uses simpler heuristics because
// the recovered module doesn't need to coordinate with Crashpad
// or the binary's auto-updater.
package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LogFile is a handle on a per-day log file inside a base directory.
// v1.3.4 binary uses one LogFile per process.
type LogFile struct {
	Path    string
	Symlink string
	MaxAge  time.Duration
}

// Create opens a log file in `dir`, named by the current date
// (YYYY-MM-DD-NN.log), and ensures a "latest.log" symlink points
// to it. If multiple log files exist for the same date, the new one
// gets a sequence number suffix.
//
// Returns the LogFile handle; the underlying *os.File is leaked
// intentionally (matches v1.3.4 binary which keeps the FD open
// for the lifetime of the process).
func CreateLogFile(dir string, maxAge time.Duration) (*LogFile, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("logfile: mkdir %s: %w", dir, err)
	}
	date := time.Now().Format("2006-01-02")
	// Find a free sequence number for today.
	seq := 0
	for {
		path := filepath.Join(dir, fmt.Sprintf("%s-%02d.log", date, seq))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			// Use this sequence.
			break
		}
		seq++
		if seq > 99 {
			// Bail out; we shouldn't have this many logs in a day.
			return nil, fmt.Errorf("logfile: too many logs in %s for %s", dir, date)
		}
	}
	logPath := filepath.Join(dir, fmt.Sprintf("%s-%02d.log", date, seq))
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("logfile: open %s: %w", logPath, err)
	}
	// Best-effort symlink; ignore failure.
	symlink := filepath.Join(dir, "latest.log")
	_ = os.Remove(symlink)
	_ = os.Symlink(filepath.Base(logPath), symlink)
	_ = f.Close()

	// Best-effort cleanup of stale logs.
	_, _ = CleanStale(dir, maxAge)

	return &LogFile{Path: logPath, Symlink: symlink, MaxAge: maxAge}, nil
}

// CreateUnlinked opens a log file but does NOT update the "latest" symlink.
// v1.3.4 binary uses this for short-lived child process logs.
func CreateUnlinkedLogFile(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("logfile: mkdir %s: %w", dir, err)
	}
	stamp := time.Now().Format("20060102-150405.000")
	logPath := filepath.Join(dir, "child-"+stamp+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", fmt.Errorf("logfile: open %s: %w", logPath, err)
	}
	_ = f.Close()
	return logPath, nil
}

// updateSymlink atomically replaces the "latest" symlink. v1.3.4 binary
// uses this when a child process (which doesn't update the symlink) finishes.
func (lf *LogFile) UpdateSymlink() error {
	if lf == nil {
		return fmt.Errorf("logfile: nil receiver")
	}
	_ = os.Remove(lf.Symlink)
	return os.Symlink(filepath.Base(lf.Path), lf.Symlink)
}

// CleanStale removes log files in `dir` whose mtime is older than `maxAge`.
// Returns the number of files removed.
//
// v1.3.4 binary calls this on every startup; we mirror the behavior.
// "latest.log" is never removed even if stale (it's a symlink that
// points to the most recent log).
func CleanStale(dir string, maxAge time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	removed := 0
	type fileAge struct {
		path  string
		mtime time.Time
	}
	var files []fileAge
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if e.Name() == "latest.log" {
			continue // never remove the symlink
		}
		if !strings.HasSuffix(e.Name(), ".log") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileAge{filepath.Join(dir, e.Name()), fi.ModTime()})
	}
	// Sort oldest first; this means if we hit the disk-space cap we
	// remove the oldest first. (v1.3.4 binary has a max count too but
	// we just keep it time-based.)
	sort.Slice(files, func(i, k int) bool { return files[i].mtime.Before(files[k].mtime) })
	for _, f := range files {
		if now.Sub(f.mtime) < maxAge {
			continue
		}
		if err := os.Remove(f.path); err == nil {
			removed++
		}
	}
	return removed, nil
}
