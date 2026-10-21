package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The wrapper's whole reason for existing is that agy's TUI owns the terminal.
// These tests pin the property that matters: by default the proxy's chatter
// goes to a file, and it never silently falls back to fd 2.

func TestLogDefaultsToFileUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvLog, "")

	want := filepath.Join(home, ".openagy", "wrapper.log")
	if got := logPath(); got != want {
		t.Fatalf("logPath() = %q, want %q", got, want)
	}

	logf, closeLog := openLog()
	logf("hello %s", "world")
	closeLog()

	raw, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("default sink did not write a file: %v", err)
	}
	if !strings.Contains(string(raw), "hello world") {
		t.Fatalf("log body = %q, want it to contain the message", raw)
	}
	if !strings.Contains(string(raw), "agyproxy: ") {
		t.Fatalf("log body = %q, want the agyproxy prefix", raw)
	}
}

func TestLogOffDiscardsAndCreatesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvLog, "off")

	if got := logPath(); got != "" {
		t.Fatalf("logPath() = %q, want empty when the sink is off", got)
	}
	logf, closeLog := openLog()
	logf("should vanish")
	closeLog()

	if _, err := os.Stat(filepath.Join(home, ".openagy", "wrapper.log")); !os.IsNotExist(err) {
		t.Fatalf("sink off still created a file (err=%v)", err)
	}
	if got := logSink(); got != "off" {
		t.Fatalf("logSink() = %q, want %q", got, "off")
	}
}

// stderr is the debugging escape hatch, and the only path that can smear the
// TUI. It must be opt-in and it must be labelled as such in doctor.
func TestLogStderrIsExplicitAndLabelled(t *testing.T) {
	t.Setenv(EnvLog, "stderr")
	if got := logPath(); got != "" {
		t.Fatalf("logPath() = %q, want empty for the stderr sink", got)
	}
	if got := logSink(); !strings.HasPrefix(got, "stderr") || !strings.Contains(got, "smear") {
		t.Fatalf("logSink() = %q, want a warning that stderr smears the TUI", got)
	}
	// openLog must hand back a usable function rather than nil.
	logf, closeLog := openLog()
	logf("debug line")
	closeLog()
}

func TestLogPathOverrideAndDebugAlias(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	custom := filepath.Join(t.TempDir(), "nested", "custom.log")
	t.Setenv(EnvLog, custom)
	if got := logPath(); got != custom {
		t.Fatalf("logPath() = %q, want the override %q", got, custom)
	}
	logf, closeLog := openLog()
	logf("nested dirs are created")
	closeLog()
	raw, err := os.ReadFile(custom)
	if err != nil {
		t.Fatalf("override sink did not write: %v", err)
	}
	if !strings.Contains(string(raw), "nested dirs are created") {
		t.Fatalf("override log body = %q", raw)
	}

	// "debug" is a synonym for stderr.
	t.Setenv(EnvLog, "debug")
	if got := logPath(); got != "" {
		t.Fatalf("logPath() = %q, want empty for the debug alias", got)
	}
}

// An unwritable sink must degrade to discarding, never to stderr: losing log
// lines is fine, smearing the TUI is not, and an unwritable log must not stop
// agy from starting.
func TestLogUnwritableFallsBackToDiscard(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// A regular file where a directory is needed: MkdirAll fails.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvLog, filepath.Join(blocker, "sub", "wrapper.log"))

	logf, closeLog := openLog()
	if logf == nil {
		t.Fatal("openLog returned a nil logf; callers would panic")
	}
	logf("this must not panic or crash")
	closeLog()
}

func TestTailLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.log")
	if err := os.WriteFile(path, []byte("a\nb\nc\nd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		n    int
		want []string
	}{
		{2, []string{"c", "d"}},
		{10, []string{"a", "b", "c", "d"}},
		{0, nil},
	}
	for _, c := range cases {
		got, err := tailLines(path, c.n)
		if err != nil {
			t.Fatalf("tailLines(%d): %v", c.n, err)
		}
		if len(got) != len(c.want) {
			t.Fatalf("tailLines(%d) = %q, want %q", c.n, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("tailLines(%d) = %q, want %q", c.n, got, c.want)
			}
		}
	}
}

// The launcher must not pass a nil Logf through to the proxy, because the
// proxy's own default is os.Stderr — the exact bug this cycle fixes.
func TestLauncherSuppliesANonStderrLogf(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvLog, "")

	// What the launcher does at startup.
	if logSink() == "" {
		t.Fatal("logSink() is empty with the default config")
	}
	if strings.Contains(logSink(), "stderr") {
		t.Fatalf("default log sink is stderr (%q) — the TUI would be smeared", logSink())
	}
}
