// Package clilog provides persistent session logging under ~/.openagy/log/.
//
// Every interactive TUI and CLI session writes diagnostic events (backend
// selection, account-pool routing, HTTP errors, retries, failover, and turn
// failures) to ~/.openagy/log/cli-<YYYYMMDD_HHMMSS>.log with a latest.log
// symlink pointing to the active session log.
//
// Environment override OPENAGY_LOG:
//   - unset:            ~/.openagy/log/cli-<YYYYMMDD_HHMMSS>.log
//   - off|none|discard: disable logging
//   - stderr|fd2:       write to os.Stderr (debugging outside TUI)
//   - <path>:           write to that explicit file path
package clilog

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/configdir"
	"github.com/justwasm/pigasus/internal/core/logts"
)

const (
	// EnvLog overrides the session log destination.
	EnvLog = "OPENAGY_LOG"

	// maxRetainedFiles bounds how many cli-*.log files are kept in ~/.openagy/log/.
	maxRetainedFiles = 30

	// maxFileBytes caps a single session log file at 16 MiB.
	maxFileBytes = 16 << 20
)

// FileInfo describes one session log file in ~/.openagy/log/.
type FileInfo struct {
	Name    string
	Path    string
	Size    int64
	ModTime time.Time
}

var (
	mu          sync.Mutex
	initialized bool
	disabled    bool
	toStderr    bool
	activePath  string
	activeFile  *os.File
	written     int64
	capped      bool
)

var (
	reBearer = regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._\-+/=]{8,}`)
	reAPIKey = regexp.MustCompile(`(?i)((?:x-api-key|api[_-]?key|authorization)\s*[:=]\s*)([^\s,;]+)`)
	reToken  = regexp.MustCompile(`\b(ya29\.[A-Za-z0-9._\-]+|AIza[0-9A-Za-z_\-]{16,}|sk-[A-Za-z0-9_\-]{16,})\b`)
)

// Sanitize redacts bearer tokens and API keys from log lines.
func Sanitize(s string) string {
	if s == "" {
		return ""
	}
	s = reBearer.ReplaceAllString(s, "${1}<redacted>")
	s = reAPIKey.ReplaceAllString(s, "${1}<redacted>")
	s = reToken.ReplaceAllString(s, "<redacted>")
	return s
}

// Dir returns the log directory path (~/.openagy/log).
func Dir() string {
	return configdir.Path("log")
}

// LatestPath returns the path to the latest.log symlink (~/.openagy/log/latest.log).
func LatestPath() string {
	dir := Dir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "latest.log")
}

// Init initializes the session log file if not already initialized.
// Returns the active log file path, or "" if logging is disabled or unavailable.
func Init() string {
	mu.Lock()
	defer mu.Unlock()
	if initialized {
		return activePath
	}
	initLocked("")
	return activePath
}

// InitWithPath initializes logging to an explicit file path.
func InitWithPath(path string) string {
	mu.Lock()
	defer mu.Unlock()
	closeLocked()
	initLocked(strings.TrimSpace(path))
	return activePath
}

// Path returns the active session log file path, or "" if none is open.
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return activePath
}

// ResetForTest closes any open log file and resets package state.
func ResetForTest() {
	mu.Lock()
	defer mu.Unlock()
	closeLocked()
	initialized = false
	disabled = false
	toStderr = false
	activePath = ""
	written = 0
	capped = false
}

func closeLocked() {
	if activeFile != nil {
		_ = activeFile.Close()
		activeFile = nil
	}
}

func initLocked(explicitPath string) {
	initialized = true
	disabled = false
	toStderr = false
	activePath = ""
	written = 0
	capped = false

	if explicitPath == "" {
		env := strings.TrimSpace(os.Getenv(EnvLog))
		switch strings.ToLower(env) {
		case "off", "none", "discard", "silent":
			disabled = true
			return
		case "stderr", "fd2":
			toStderr = true
			return
		}
		if env != "" {
			explicitPath = env
		}
	}

	if explicitPath != "" {
		if err := os.MkdirAll(filepath.Dir(explicitPath), 0o700); err != nil {
			disabled = true
			return
		}
		f, err := os.OpenFile(explicitPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			disabled = true
			return
		}
		if fi, err := f.Stat(); err == nil {
			written = fi.Size()
		}
		activeFile = f
		activePath = explicitPath
		return
	}

	dir := Dir()
	if dir == "" {
		disabled = true
		return
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		disabled = true
		return
	}

	ts := logts.FormatLogTS()
	candidate := filepath.Join(dir, "cli-"+ts+".log")
	f, err := os.OpenFile(candidate, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		candidate = filepath.Join(dir, fmt.Sprintf("cli-%s-%d.log", ts, os.Getpid()))
		f, err = os.OpenFile(candidate, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			disabled = true
			return
		}
	}
	activeFile = f
	activePath = candidate
	updateLatestSymlink(dir, filepath.Base(candidate))
	pruneOldLogs(dir, maxRetainedFiles)
}

func updateLatestSymlink(dir, targetBase string) {
	latest := filepath.Join(dir, "latest.log")
	tmp := filepath.Join(dir, fmt.Sprintf(".latest-%d.tmp", os.Getpid()))
	_ = os.Remove(tmp)
	if err := os.Symlink(targetBase, tmp); err == nil {
		if err := os.Rename(tmp, latest); err == nil {
			return
		}
		_ = os.Remove(tmp)
	}
	_ = os.Remove(latest)
	_ = os.Symlink(targetBase, latest)
}

func pruneOldLogs(dir string, keep int) {
	if keep <= 0 {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type item struct {
		path    string
		modTime time.Time
	}
	var files []item
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "cli-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, item{
			path:    filepath.Join(dir, name),
			modTime: info.ModTime(),
		})
	}
	if len(files) <= keep {
		return
	}
	sort.Slice(files, func(i, j int) bool {
		if !files[i].modTime.Equal(files[j].modTime) {
			return files[i].modTime.After(files[j].modTime)
		}
		return files[i].path > files[j].path
	})
	for _, old := range files[keep:] {
		_ = os.Remove(old.path)
	}
}

// Logf writes a timestamped, sanitized log line for component.
func Logf(component, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	msg = strings.TrimRight(msg, "\r\n")
	if msg == "" {
		return
	}
	msg = Sanitize(msg)
	comp := strings.TrimSpace(component)
	if comp == "" {
		comp = "cli"
	}
	line := fmt.Sprintf("[%s] [%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), comp, msg)

	mu.Lock()
	defer mu.Unlock()
	if !initialized {
		// Avoid creating files in the user's real ~/.openagy/log during unrelated unit tests
		// unless explicitly configured via OPENAGY_LOG or OPENAGY_CONFIG_HOME.
		if testing.Testing() && strings.TrimSpace(os.Getenv(EnvLog)) == "" && strings.TrimSpace(os.Getenv(configdir.EnvHome)) == "" {
			return
		}
		initLocked("")
	}
	if disabled {
		return
	}
	if toStderr {
		_, _ = io.WriteString(os.Stderr, line)
		return
	}
	if activeFile == nil || capped {
		return
	}
	if written+int64(len(line)) > maxFileBytes {
		capped = true
		_, _ = io.WriteString(activeFile, fmt.Sprintf("[%s] [clilog] log size cap (%d bytes) reached; further entries suppressed\n", time.Now().Format("2006-01-02 15:04:05.000"), maxFileBytes))
		return
	}
	n, _ := io.WriteString(activeFile, line)
	written += int64(n)
}

type lineWriter struct {
	component string
}

func (w lineWriter) Write(p []byte) (int, error) {
	for _, raw := range bytes.Split(p, []byte{'\n'}) {
		s := strings.TrimSpace(string(raw))
		if s != "" {
			Logf(w.component, "%s", s)
		}
	}
	return len(p), nil
}

// Writer returns an io.Writer that forwards lines to the session log.
func Writer() io.Writer {
	return lineWriter{component: "cli"}
}

// ComponentWriter returns an io.Writer tagged with component.
func ComponentWriter(component string) io.Writer {
	return lineWriter{component: component}
}

// ResolveReadablePath returns the active log path, or falls back to latest.log
// or the newest cli-*.log file in Dir().
func ResolveReadablePath() string {
	if p := Path(); p != "" {
		return p
	}
	if latest := LatestPath(); latest != "" {
		if resolved, err := filepath.EvalSymlinks(latest); err == nil && resolved != "" {
			if _, err := os.Stat(resolved); err == nil {
				return resolved
			}
		}
		if _, err := os.Stat(latest); err == nil {
			return latest
		}
	}
	files, err := ListFiles(1)
	if err == nil && len(files) > 0 {
		return files[0].Path
	}
	return ""
}

// ListFiles returns cli-*.log files in Dir() sorted newest-first.
func ListFiles(limit int) ([]FileInfo, error) {
	dir := Dir()
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []FileInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "cli-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, FileInfo{
			Name:    name,
			Path:    filepath.Join(dir, name),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Name > out[j].Name
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// Tail returns the last n lines of path (or ResolveReadablePath() if path == "").
func Tail(path string, n int) ([]string, error) {
	if path == "" {
		path = ResolveReadablePath()
	}
	if path == "" {
		return nil, os.ErrNotExist
	}
	if n <= 0 {
		n = 50
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if len(ring) < n {
			ring = append(ring, line)
		} else {
			copy(ring, ring[1:])
			ring[n-1] = line
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return ring, nil
}
