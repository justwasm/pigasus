package agyproxy

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// EnvLog names the sink for the proxy's log lines.
//
// This exists because the proxy logs roughly one line per RPC and those lines
// used to go to stderr — the very terminal agy's TUI owns. Every request
// smeared the screen. The default is therefore a file, never fd 2.
const EnvLog = "OPENAGY_WRAPPER_LOG"

// logMaxBytes bounds the default log file. Past this we truncate on open
// rather than rotate: the log is a debugging aid, and a proxy that grows an
// unbounded file in the user's home is worse than a proxy with a short
// history.
const logMaxBytes = 4 << 20

// logPath is $OPENAGY_WRAPPER_LOG when it names a file, else
// ~/.openagy/wrapper.log. It returns "" when the sink is not a file (stderr,
// off) or when there is no home directory.
func logPath() string {
	if v := strings.TrimSpace(os.Getenv(EnvLog)); v != "" {
		if _, ok := logSinkKind(v); ok {
			return ""
		}
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".openagy", "wrapper.log")
}

// logSinkKind classifies the OPENAGY_WRAPPER_LOG spelling. The second result
// is false when the value is a path rather than one of the keywords.
func logSinkKind(v string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "stderr", "fd2", "debug":
		return "stderr", true
	case "off", "none", "discard", "silent":
		return "off", true
	case "":
		return "", false
	}
	return "", false
}

// logSink is one line of `pigasus proxy doctor`: where the proxy's chatter goes.
func logSink() string {
	v := strings.TrimSpace(os.Getenv(EnvLog))
	if kind, ok := logSinkKind(v); ok {
		if kind == "stderr" {
			return "stderr (TUI will be smeared — debugging only)"
		}
		return "off"
	}
	if p := logPath(); p != "" {
		return p
	}
	return "off"
}

// openLog returns the Logf the proxy should use plus a closer to call on the
// way out.
//
//	unset             ~/.openagy/wrapper.log (append; truncated past 4 MiB)
//	stderr|fd2|debug  os.Stderr — use this when debugging outside the TUI
//	off|none|discard  discard
//	<path>            that file
//
// If the file cannot be opened we fall back to discarding, *never* to stderr:
// losing log lines is strictly better than corrupting the TUI, and an
// unwritable log must not turn a working agy into a smeared screen.
func openLog() (func(string, ...any), func()) {
	if kind, ok := logSinkKind(os.Getenv(EnvLog)); ok {
		switch kind {
		case "stderr":
			return log.New(os.Stderr, "agyproxy: ", 0).Printf, func() {}
		default:
			return func(string, ...any) {}, func() {}
		}
	}

	path := logPath()
	if path == "" {
		return func(string, ...any) {}, func() {}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return func(string, ...any) {}, func() {}
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if fi, err := os.Stat(path); err == nil && fi.Size() > logMaxBytes {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return func(string, ...any) {}, func() {}
	}
	// Timestamps matter in a file (several runs share one), and they are
	// harmless on stderr.
	return log.New(f, "agyproxy: ", log.LstdFlags).Printf, func() { _ = f.Close() }
}

// cmdLog prints the tail of the log file. It exists so moving the proxy off
// stderr does not make the logs undiscoverable.
func cmdLog(opts AdminOptions, args []string) error {
	fs := newFlagSet("log", opts.Err)
	n := fs.Int("n", 40, "how many trailing lines to print")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := logPath()
	if path == "" {
		return fmt.Errorf("log sink is %s — nothing to read (unset %s to log to a file)", logSink(), EnvLog)
	}
	lines, err := tailLines(path, *n)
	if err != nil {
		return err
	}
	fmt.Fprintf(opts.Out, "%s (last %d line%s)\n\n", path, len(lines), plural(len(lines)))
	for _, l := range lines {
		fmt.Fprintln(opts.Out, l)
	}
	return nil
}

// newFlagSet returns a flag set whose usage output goes to errOut.
func newFlagSet(name string, errOut io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(errOut)
	return fs
}

// tailLines returns up to n trailing lines of path. It reads the whole file
// rather than seeking: the file is capped at 4 MiB by openLog.
func tailLines(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if n <= 0 {
		return nil, nil
	}
	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		if len(ring) == n {
			copy(ring, ring[1:])
			ring[len(ring)-1] = sc.Text()
			continue
		}
		ring = append(ring, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return ring, nil
}
