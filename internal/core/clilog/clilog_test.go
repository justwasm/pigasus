package clilog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitCreatesLogAndLatestSymlink(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	home := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", home)
	t.Setenv(EnvLog, "")

	p := Init()
	if p == "" {
		t.Fatal("Init() returned empty path")
	}
	wantDir := filepath.Join(home, "log")
	if filepath.Dir(p) != wantDir {
		t.Fatalf("log dir = %q, want %q", filepath.Dir(p), wantDir)
	}
	if !strings.HasPrefix(filepath.Base(p), "cli-") || !strings.HasSuffix(p, ".log") {
		t.Fatalf("unexpected log filename %q", filepath.Base(p))
	}

	Logf("cloudcode", "request failed Authorization: Bearer ya29.secret-token-1234567890")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "[cloudcode] request failed") {
		t.Fatalf("missing log entry in %q", text)
	}
	if strings.Contains(text, "ya29.secret-token") {
		t.Fatalf("secret token leaked into log: %q", text)
	}
	if !strings.Contains(text, "<redacted>") {
		t.Fatalf("expected <redacted> marker in %q", text)
	}

	latest := LatestPath()
	resolved, err := filepath.EvalSymlinks(latest)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", latest, err)
	}
	wantResolved, _ := filepath.EvalSymlinks(p)
	if resolved != wantResolved {
		t.Fatalf("latest.log -> %q, want %q", resolved, wantResolved)
	}

	lines, err := Tail("", 10)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "[cloudcode]") {
		t.Fatalf("Tail = %v", lines)
	}
}

func TestEnvOffDisablesLogging(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	home := t.TempDir()
	t.Setenv("OPENAGY_CONFIG_HOME", home)
	t.Setenv(EnvLog, "off")

	if p := Init(); p != "" {
		t.Fatalf("Init() with OPENAGY_LOG=off = %q, want empty", p)
	}
	Logf("test", "should not write")
	if _, err := os.Stat(filepath.Join(home, "log")); !os.IsNotExist(err) {
		t.Fatalf("expected no log dir when OPENAGY_LOG=off, got err=%v", err)
	}
}

func TestPruneOldLogs(t *testing.T) {
	ResetForTest()
	t.Cleanup(ResetForTest)

	dir := t.TempDir()
	for i := 0; i < 35; i++ {
		name := filepath.Join(dir, "cli-20260101_0000"+string(rune('a'+i/26))+string(rune('a'+i%26))+".log")
		if err := os.WriteFile(name, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pruneOldLogs(dir, 30)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 30 {
		t.Fatalf("remaining files = %d, want 30", len(entries))
	}
}
