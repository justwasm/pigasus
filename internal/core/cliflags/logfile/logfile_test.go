package logfile

import (
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
)

// flagSet builds a flag set that carries the log file flag.
func flagSet(t *testing.T, args ...string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String(FlagLogFile, "", "Override CLI log file path")
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return fs
}

// TestApply_Default pins that a session without the flag leaves the log
// where it was.
func TestApply_Default(t *testing.T) {
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	Apply(flagSet(t))
	if log.Writer() != os.Stderr {
		t.Errorf("the log was redirected without a flag")
	}
}

// TestApply_NilFlagSet pins that a session without a flag set keeps the
// default instead of failing.
func TestApply_NilFlagSet(t *testing.T) {
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	Apply(nil)
	if log.Writer() != os.Stderr {
		t.Errorf("the log was redirected without a flag set")
	}
}

// TestApply_RoutesTheLog pins that the log goes to the file the flag
// names, and that the file is created when it is not there yet.
func TestApply_RoutesTheLog(t *testing.T) {
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	path := filepath.Join(t.TempDir(), "cli.log")

	Apply(flagSet(t, "--log-file="+path))
	if log.Writer() == os.Stderr {
		t.Fatalf("the log was not redirected")
	}
	log.Printf("a line")
	log.SetOutput(os.Stderr)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log file: %v", err)
	}
	if len(data) == 0 {
		t.Errorf("the log file is empty")
	}
}

// TestApply_Appends pins that the log file is appended to, not replaced:
// the reference opens it with O_APPEND.
func TestApply_Appends(t *testing.T) {
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	path := filepath.Join(t.TempDir(), "cli.log")
	if err := os.WriteFile(path, []byte("earlier\n"), 0o644); err != nil {
		t.Fatalf("seed the log file: %v", err)
	}

	Apply(flagSet(t, "--log-file="+path))
	log.Printf("a line")
	log.SetOutput(os.Stderr)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the log file: %v", err)
	}
	if len(data) <= len("earlier\n") {
		t.Errorf("the log file was replaced, not appended to: %q", string(data))
	}
}

// TestApply_UnwritablePath pins that a path that cannot be opened leaves
// the log where it was.
func TestApply_UnwritablePath(t *testing.T) {
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	path := filepath.Join(t.TempDir(), "missing", "cli.log")

	Apply(flagSet(t, "--log-file="+path))
	if log.Writer() != os.Stderr {
		t.Errorf("the log was redirected to a file that cannot be opened")
	}
}
