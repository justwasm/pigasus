package agybin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandbox removes every ambient way Path could find a real agy: the well-known
// install paths, $AGY_REAL_BIN and $PATH.
func sandbox(t *testing.T) {
	t.Helper()
	oldPaths := DefaultPaths
	DefaultPaths = nil
	t.Cleanup(func() { DefaultPaths = oldPaths })
	t.Setenv(EnvRealBin, "")
	t.Setenv("PATH", t.TempDir())
}

// fakeAgy writes an executable stub that behaves like agy --version.
func fakeAgy(t *testing.T, dir, version string) string {
	t.Helper()
	p := filepath.Join(dir, "agy")
	script := "#!/bin/sh\necho '" + version + "'\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPathRefusesSelf(t *testing.T) {
	sandbox(t)
	self, err := os.Executable()
	if err != nil {
		t.Skipf("os.Executable: %v", err)
	}
	got, err := Path(self)
	if err == nil {
		t.Fatalf("Path(self) = %q, want an error", got)
	}
	if !strings.Contains(err.Error(), "refusing to exec self") {
		t.Fatalf("error should name the self guard, got %v", err)
	}
	// The same binary through a symlink must also be refused: the guard
	// resolves links before comparing.
	link := filepath.Join(t.TempDir(), "agy")
	if err := os.Symlink(self, link); err == nil {
		if got, err := Path(link); err == nil {
			t.Fatalf("Path(symlink to self) = %q, want an error", got)
		}
	}
}

func TestPathExplicitWins(t *testing.T) {
	sandbox(t)
	want := fakeAgy(t, t.TempDir(), "9.9.9")
	got, err := Path(want)
	if err != nil {
		t.Fatal(err)
	}
	if same(t, got, want) == false {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

// same compares two paths after symlink resolution: on macOS t.TempDir()
// hands back /var/... while EvalSymlinks answers /private/var/....
func same(t *testing.T, a, b string) bool {
	t.Helper()
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		t.Fatal(err)
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		t.Fatal(err)
	}
	return ra == rb
}

func TestPathResolvesSymlink(t *testing.T) {
	sandbox(t)
	dir := t.TempDir()
	target := fakeAgy(t, dir, "1.4.2")
	link := filepath.Join(dir, "agy-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	got, err := Path(link)
	if err != nil {
		t.Fatal(err)
	}
	// EvalSymlinks on macOS returns /private/var/... for /var/...; compare
	// resolved forms rather than raw strings.
	wantResolved, _ := filepath.EvalSymlinks(target)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Fatalf("Path = %q, want %q", gotResolved, wantResolved)
	}
}

func TestPathRejectsNonExecutableAndDirectory(t *testing.T) {
	sandbox(t)
	dir := t.TempDir()
	plain := filepath.Join(dir, "agy")
	if err := os.WriteFile(plain, []byte("not a program"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Path(plain); err == nil {
		t.Fatalf("Path(non-executable) = %q, want an error", got)
	}
	if got, err := Path(dir); err == nil {
		t.Fatalf("Path(directory) = %q, want an error", got)
	}
}

func TestPathUsesEnvWhenNoExplicit(t *testing.T) {
	sandbox(t)
	want := fakeAgy(t, t.TempDir(), "1.4.2")
	t.Setenv(EnvRealBin, want)
	got, err := Path("")
	if err != nil {
		t.Fatal(err)
	}
	if !same(t, got, want) {
		t.Fatalf("Path = %q, want %q (from %s)", got, want, EnvRealBin)
	}
}

func TestVersion(t *testing.T) {
	bin := fakeAgy(t, t.TempDir(), "1.4.2")
	got, err := Version(bin)
	if err != nil {
		t.Fatal(err)
	}
	if got != "1.4.2" {
		t.Fatalf("Version = %q, want 1.4.2", got)
	}
	if _, err := Version(""); err == nil {
		t.Fatal("Version(\"\") should fail")
	}
}
